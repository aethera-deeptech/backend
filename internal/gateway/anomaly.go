package gateway

import (
	"context"
	"errors"
	"fmt"
	"math"
	"time"

	"github.com/jackc/pgx/v5"

	"github.com/aethera-deeptech/backend/internal/business"
)

// The rules that decide when a reading is an anomaly
const (
	// anomalyRecent is how old a sample may be and still raise an anomaly, so a gateway that catches up on a day of readings does not
	// raise alarms about the morning
	anomalyRecent = 2 * time.Hour
	// anomalyDebounce is the time after an anomaly in which the same node raises no new one
	anomalyDebounce = 6 * time.Hour
	// surgeMinExcessW and surgeMinShare are the smallest margin over the usual high power, so a node that is always near zero does not
	// raise an anomaly for 5 W and a node that draws 3 kW does not raise one for 100 W
	surgeMinExcessW = 100.0
	surgeMinShare   = 0.3
	// overloadCode is the condition that the node itself reports for an overload
	overloadCode = 3
	// historyDays and minHistoryDays are how far back the usual power looks and how many days the same hour of the day needs
	historyDays    = 15
	minHistoryDays = 3
)

// surgeLimit is the power above which a reading is a surge, given the usual high power of the hour and its spread
func surgeLimit(mean, std, k float64) float64 {
	high := mean + k*std

	return high + math.Max(surgeMinExcessW, surgeMinShare*high)
}

// detectAnomaly looks at the new samples of a node that belongs to a business and writes an anomaly when one is far above the usual
// power of that hour of the day, or when the node says it is overloaded. A node that no business claimed is not watched, and a node
// that raised an anomaly in the last hours does not raise another.
func detectAnomaly(ctx context.Context, tx pgx.Tx, gatewayID, nodeID string, samples []stored, now time.Time) error {
	var (
		peak     *stored
		overload *stored
	)

	for i := range samples {
		s := &samples[i]
		if s.t.Before(now.Add(-anomalyRecent)) {
			continue
		}

		if s.nodeState != nil && *s.nodeState == overloadCode && overload == nil {
			overload = s
		}

		if peak == nil || s.watts > peak.watts {
			peak = s
		}
	}

	// the cheap checks come first, because most batches have nothing to report
	if overload == nil && (peak == nil || peak.watts <= surgeMinExcessW) {
		return nil
	}

	var level string

	err := tx.QueryRow(ctx,
		"SELECT b.anomaly_sensitivity FROM devices d JOIN businesses b ON b.id = d.business_id WHERE d.gateway_id = $1 AND d.node_id = $2",
		gatewayID, nodeID).Scan(&level)
	if errors.Is(err, pgx.ErrNoRows) {
		return nil
	}

	if err != nil {
		return fmt.Errorf("read the sensitivity %w", err)
	}

	kind, at, watts := "", time.Time{}, 0.0

	var expected *float64

	switch {
	case overload != nil:
		kind, at, watts = "overload", overload.t, overload.watts
	default:
		high, ok, err := usualHigh(ctx, tx, gatewayID, nodeID, peak.t, level)
		if err != nil {
			return err
		}

		if !ok || peak.watts <= high.limit {
			return nil
		}

		kind, at, watts, expected = "surge", peak.t, peak.watts, &high.high
	}

	var recent bool
	if err := tx.QueryRow(ctx,
		"SELECT EXISTS (SELECT 1 FROM anomalies WHERE gateway_id = $1 AND node_id = $2 AND at > $3)",
		gatewayID, nodeID, at.Add(-anomalyDebounce)).Scan(&recent); err != nil {
		return fmt.Errorf("read the anomalies %w", err)
	}

	if recent {
		return nil
	}

	if _, err := tx.Exec(ctx,
		"INSERT INTO anomalies (gateway_id, node_id, at, peak_watts, expected_high_watts, kind) VALUES ($1, $2, $3, $4, $5, $6)",
		gatewayID, nodeID, at, watts, expected, kind); err != nil {
		return fmt.Errorf("store the anomaly %w", err)
	}

	return nil
}

type usual struct {
	high  float64
	limit float64
}

// usualHigh reads the usual high power of the hour of the day of a sample from the same hour on earlier days. It needs several days,
// or it says that it knows nothing and no surge is raised.
func usualHigh(ctx context.Context, tx pgx.Tx, gatewayID, nodeID string, at time.Time, level string) (usual, bool, error) {
	var (
		mean, std float64
		days      int
	)

	hour := at.UTC().Truncate(time.Hour)

	err := tx.QueryRow(ctx,
		`SELECT COALESCE(avg(max_watts), 0), COALESCE(stddev_samp(max_watts), 0), count(*) FROM readings_hourly
		 WHERE gateway_id = $1 AND node_id = $2 AND hour >= $3 AND hour < $4 AND extract(hour FROM hour AT TIME ZONE 'UTC') = $5`,
		gatewayID, nodeID, hour.Add(-historyDays*24*time.Hour), hour, hour.Hour()).Scan(&mean, &std, &days)
	if err != nil {
		return usual{}, false, fmt.Errorf("read the usual power %w", err)
	}

	if days < minHistoryDays {
		return usual{}, false, nil
	}

	k := business.SensitivityK(level)

	return usual{high: mean + k*std, limit: surgeLimit(mean, std, k)}, true, nil
}
