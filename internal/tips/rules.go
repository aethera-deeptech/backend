// Package tips works out the tips of a business from its readings and keeps the decisions of its owner
package tips

import (
	"math"
	"time"

	"github.com/aethera-deeptech/backend/internal/business"
)

// The numbers behind the tips
const (
	// NeededDays is how many days of data the service wants before it gives the first tip
	NeededDays = 7
	// VerifyDays is how long the service compares after a tip was applied, and BaselineDays is how long it looks back before that
	VerifyDays   = 14
	BaselineDays = 7
	// monthDays is the length of the month in a tip, which is fixed so a tip does not change with the calendar
	monthDays = 30
	// minOffRows is the number of hours outside the work time that a device needs data for before a tip is made
	minOffRows = 24
	// minSavingRp is the smallest saving that is worth a tip
	minSavingRp = 5000
	// roundRp is the step that the money in a tip is rounded to
	roundRp = 1000
	// standbyMinW and standbyMaxW are the band of a device that waits, the same as in the device status
	standbyMinW = 1.2
	standbyMaxW = 8.5
)

// The kinds of tip
const (
	KindAfterHours = "after-hours"
	KindStandby    = "standby"
)

// IsWorkHour reports whether the hour that starts at local, in the time zone of the business, is inside its work time. An hour that
// the work time only touches counts as work, so a tip never tells the owner to switch off a device while it may be in use.
func IsWorkHour(local time.Time, wh business.WorkHours) bool {
	switch wh.Days {
	case "mon-fri":
		if local.Weekday() == time.Saturday || local.Weekday() == time.Sunday {
			return false
		}
	case "mon-sat":
		if local.Weekday() == time.Sunday {
			return false
		}
	}

	start, okStart := clock(wh.Start)
	end, okEnd := clock(wh.End)

	if !okStart || !okEnd {
		return true
	}

	at := time.Duration(local.Hour())*time.Hour + time.Duration(local.Minute())*time.Minute

	return at < end && at+time.Hour > start
}

func clock(v string) (time.Duration, bool) {
	t, err := time.Parse("15:04", v)
	if err != nil {
		return 0, false
	}

	return time.Duration(t.Hour())*time.Hour + time.Duration(t.Minute())*time.Minute, true
}

// RoundRp turns energy into money, rounded to the nearest 1000 rupiah
func RoundRp(kwh float64, tariff int) int {
	return int(math.Round(kwh*float64(tariff)/roundRp)) * roundRp
}

// hourRow is one hour of one device
type hourRow struct {
	hour   time.Time
	energy float64
	watts  float64
}

// offHours sums what a device drew outside the work time. It returns the energy in kWh, the average power in watts over the hours
// that have data, and the number of those hours.
func offHours(rows []hourRow, loc *time.Location, wh business.WorkHours) (float64, float64, int) {
	var energy, watts float64

	n := 0

	for _, r := range rows {
		if IsWorkHour(r.hour.In(loc), wh) {
			continue
		}

		energy += r.energy
		watts += r.watts
		n++
	}

	if n == 0 {
		return 0, 0, 0
	}

	return energy, watts / float64(n), n
}

// classify picks the kind of tip for a device from its energy outside the work time, or says that there is none. The energy is the sum
// over the days that the rows cover.
func classify(rows []hourRow, loc *time.Location, wh business.WorkHours, days int) (string, float64) {
	energy, avgW, n := offHours(rows, loc, wh)
	if n < minOffRows || days <= 0 {
		return "", 0
	}

	kwhMonth := energy / float64(days) * monthDays

	switch {
	case avgW >= standbyMaxW:
		return KindAfterHours, kwhMonth
	case avgW >= standbyMinW:
		return KindStandby, kwhMonth
	default:
		return "", 0
	}
}
