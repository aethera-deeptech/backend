package device

import (
	"math"
	"time"
)

// The rules that turn the last reading of a device into its status
const (
	// StandbyMinW and StandbyMaxW are the band of a device that is plugged in and waits, which is the band of the proposal
	StandbyMinW = 1.2
	StandbyMaxW = 8.5
	// workingShare is the part of the usual high power of a device that it must draw to count as working
	workingShare = 0.2
	// OfflineAfter is how long a device may be silent before it counts as offline
	OfflineAfter = 15 * time.Minute
	// AnomalyFor is how long a device counts as an anomaly after the newest anomaly
	AnomalyFor = 24 * time.Hour
)

// The statuses of a device, written as the app shows them
const (
	StatusOn      = "nyala"
	StatusOff     = "mati"
	StatusStandby = "siaga"
	StatusOffline = "offline"
	StatusAnomaly = "anomali"
)

// WorkingLevel is the power from which a device counts as working. It is a share of the usual high power of that device, which is the
// 95th percentile of its highest power of each hour in the last 7 days, and never below the top of the standby band. A device with no
// history has the top of the standby band as its level.
func WorkingLevel(p95 *float64) float64 {
	if p95 == nil {
		return StandbyMaxW
	}

	return math.Max(StandbyMaxW, workingShare**p95)
}

// Classify returns the status of a device. A device with no reading, or whose newest reading is older than OfflineAfter, is offline,
// and offline wins over everything else. A device with an anomaly in the last 24 hours is an anomaly. Otherwise the power decides.
func Classify(now time.Time, lastAt *time.Time, watts *float64, p95 *float64, anomalyAt *time.Time) string {
	if lastAt == nil || watts == nil || now.Sub(*lastAt) > OfflineAfter {
		return StatusOffline
	}

	if anomalyAt != nil && now.Sub(*anomalyAt) <= AnomalyFor {
		return StatusAnomaly
	}

	switch {
	case *watts < StandbyMinW:
		return StatusOff
	case *watts < WorkingLevel(p95):
		return StatusStandby
	default:
		return StatusOn
	}
}

// statusOrder is the order of the list, the ones that need a look first
var statusOrder = map[string]int{StatusAnomaly: 0, StatusOffline: 1, StatusStandby: 2, StatusOn: 3, StatusOff: 4}

// Signal describes how well the gateway hears the node from the age of its newest reading and the number of nodes that relayed it
func Signal(now time.Time, lastAt *time.Time, hops int) string {
	if lastAt == nil || now.Sub(*lastAt) > OfflineAfter {
		return "tidak-ada"
	}

	age := now.Sub(*lastAt)

	switch {
	case age <= 2*time.Minute && hops <= 1:
		return "baik"
	case age <= 5*time.Minute:
		return "cukup"
	default:
		return "lemah"
	}
}

// KindLabel is the text that the detail screen shows for a kind
func KindLabel(kind string) string {
	if kind == "clamp" {
		return "Penjepit arus (dijepit di kabel)"
	}

	return "Colokan pintar"
}

// niceCeiling returns the smallest round value of the form 1, 2 or 5 times a power of ten that is at least v, for the top of a chart
func niceCeiling(v float64) float64 {
	if v <= 0 {
		return 100
	}

	exp := math.Pow(10, math.Floor(math.Log10(v)))

	for _, m := range []float64{1, 2, 5, 10} {
		if m*exp >= v {
			return m * exp
		}
	}

	return 10 * exp
}
