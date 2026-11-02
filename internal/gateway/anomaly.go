package gateway

import (
	"context"
	"errors"
	"fmt"
	"log/slog"
	"math"
	"slices"
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
	// overloadCode and degradationCode are the conditions that the node itself reports for an overload and for a motor that is wearing out
	overloadCode    = 3
	degradationCode = 2
	// degradationMaxPF is the power factor under which a loaded motor is a poor one, and degradationMinW is the load that counts as
	// loaded, because the power factor of an idle motor is low anyway. A batch needs degradationMinSamples loaded samples and at least
	// degradationShare of them under the limit, so one odd reading does not raise it.
	degradationMaxPF      = 0.55
	degradationMinW       = 150.0
	degradationMinSamples = 3
	degradationShare      = 0.8
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
// power of that hour of the day, when the node says it is overloaded or wearing out, or when a loaded motor has a poor power factor. A node that no business claimed is not watched, and a node
// that raised an anomaly in the last hours does not raise another.
func detectAnomaly(ctx context.Context, tx pgx.Tx, gatewayID, nodeID string, samples []stored, now time.Time) error {
	var (
		overload *stored
		// loaded counts the samples with a power factor at a high load, poor of them are under the limit, and worst is the sample of the
		// highest power among those
		loaded, poor int
		worst        *stored
		pfSum        float64
		reported     *stored
		// peaks holds the highest sample of each UTC hour, because the usual power belongs to an hour of the day
		peaks    = map[time.Time]*stored{}
		anySurge bool
	)

	for i := range samples {
		s := &samples[i]
		if s.t.Before(now.Add(-anomalyRecent)) {
			continue
		}

		if s.nodeState != nil && *s.nodeState == overloadCode && overload == nil {
			overload = s
		}

		if s.nodeState != nil && *s.nodeState == degradationCode && reported == nil {
			reported = s
		}

		if s.pf != nil && s.watts >= degradationMinW {
			loaded++

			if *s.pf < degradationMaxPF {
				poor++
				pfSum += *s.pf

				if worst == nil || s.watts > worst.watts {
					worst = s
				}
			}
		}

		hour := s.t.UTC().Truncate(time.Hour)
		if p := peaks[hour]; p == nil || s.watts > p.watts {
			peaks[hour] = s
		}

		if s.watts > surgeMinExcessW {
			anySurge = true
		}
	}

	// the motor is poor when the node says so, or when most of the loaded samples of the batch have a poor power factor
	var degradation *stored

	switch {
	case reported != nil:
		degradation = reported
	case loaded >= degradationMinSamples && worst != nil && float64(poor) >= degradationShare*float64(loaded):
		mean := pfSum / float64(poor)
		degradation = &stored{t: worst.t, watts: worst.watts, pf: &mean}
	}

	// the cheap checks come first, because most batches have nothing to report
	if overload == nil && degradation == nil && !anySurge {
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

	// the kinds are checked on their own, so a batch that holds two of them reports both
	if overload != nil {
		if err := raise(ctx, tx, gatewayID, nodeID, "overload", overload, level); err != nil {
			return err
		}
	}

	if degradation != nil {
		if err := raise(ctx, tx, gatewayID, nodeID, "degradation", degradation, level); err != nil {
			return err
		}
	}

	// the oldest hour comes first, and the debounce of the first surge that is written holds back the rest
	hours := make([]time.Time, 0, len(peaks))
	for h := range peaks {
		hours = append(hours, h)
	}

	slices.SortFunc(hours, func(a, b time.Time) int { return a.Compare(b) })

	for _, h := range hours {
		if p := peaks[h]; p.watts > surgeMinExcessW {
			if err := raise(ctx, tx, gatewayID, nodeID, "surge", p, level); err != nil {
				return err
			}
		}
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

	// only a degradation keeps the power factor, because it is what that kind is about
	var pf *float64
	if kind == "degradation" {
		pf = s.pf
	}

	if _, err := tx.Exec(ctx,
		"INSERT INTO anomalies (gateway_id, node_id, at, peak_watts, expected_high_watts, kind, power_factor) VALUES ($1, $2, $3, $4, $5, $6, $7)",
		gatewayID, nodeID, s.t, s.watts, expected, kind, pf); err != nil {
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
