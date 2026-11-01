package gateway

import (
	"context"
	"errors"
	"fmt"
	"log/slog"
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
	// historyDays and minHistoryDays are how far back the usual power looks and how many days the same hour of the day needs. A week
	// is the least that covers every day of the week, so a business that is closed at the weekend does not look quiet at that hour.
	historyDays    = 15
	minHistoryDays = 7
)

// surgeLimit is the power above which a reading is a surge, given the usual high power of the hour and its spread
func surgeLimit(mean, std, k float64) float64 {
	high := mean + k*std

	return high + math.Max(surgeMinExcessW, surgeMinShare*high)
}

// watchNode runs detectAnomaly in a savepoint. A failure of the detection is logged and rolled back, and only a failure of the
// savepoint itself reaches the caller, because then the transaction cannot go on.
func watchNode(ctx context.Context, tx pgx.Tx, gatewayID, nodeID string, samples []stored, now time.Time) error {
	sp, err := tx.Begin(ctx)
	if err != nil {
		return fmt.Errorf("start the anomaly check %w", err)
	}

	if err := detectAnomaly(ctx, sp, gatewayID, nodeID, samples, now); err != nil {
		slog.ErrorContext(ctx, "the anomaly check failed", "error", err)

		if rbErr := sp.Rollback(ctx); rbErr != nil {
			return fmt.Errorf("roll back the anomaly check %w", rbErr)
		}

		return nil
	}

	if err := sp.Commit(ctx); err != nil {
		return fmt.Errorf("end the anomaly check %w", err)
	}

	return nil
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

	// the two kinds are checked on their own, so a batch that holds both reports both
	if overload != nil {
		if err := raise(ctx, tx, gatewayID, nodeID, "overload", overload, level); err != nil {
			return err
		}
	}

	if peak != nil && peak.watts > surgeMinExcessW {
		return raise(ctx, tx, gatewayID, nodeID, "surge", peak, level)
	}

	return nil
}

// raise writes an anomaly of one kind for a sample, unless the node raised one of that kind in the last hours, or for a surge unless
// the power is within what is usual for that hour
func raise(ctx context.Context, tx pgx.Tx, gatewayID, nodeID, kind string, s *stored, level string) error {
	// the debounce is for each kind, so a surge does not hide the overload that the node reports a little later. It is checked before the
	// usual power, so a node inside the window costs one query.
	var recent bool
	if err := tx.QueryRow(ctx,
		"SELECT EXISTS (SELECT 1 FROM anomalies WHERE gateway_id = $1 AND node_id = $2 AND kind = $3 AND at > $4)",
		gatewayID, nodeID, kind, s.t.Add(-anomalyDebounce)).Scan(&recent); err != nil {
		return fmt.Errorf("read the anomalies %w", err)
	}

	if recent {
		return nil
	}

	var expected *float64

	if kind == "surge" {
		high, ok, err := usualHigh(ctx, tx, gatewayID, nodeID, s.t, level)
		if err != nil {
			return err
		}

		if !ok || s.watts <= high.limit {
			return nil
		}

		expected = &high.high
	}

	if _, err := tx.Exec(ctx,
		"INSERT INTO anomalies (gateway_id, node_id, at, peak_watts, expected_high_watts, kind) VALUES ($1, $2, $3, $4, $5, $6)",
		gatewayID, nodeID, s.t, s.watts, expected, kind); err != nil {
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
