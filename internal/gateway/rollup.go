package gateway

import (
	"context"
	"fmt"
	"sort"
	"time"

	"github.com/jackc/pgx/v5"

	"github.com/aethera-deeptech/backend/internal/janitor"
)

// The rules that turn samples into the energy of an hour
const (
	// maxEnergyGap is the longest gap that counts when the energy is worked out from the power, so a node that was off for an hour
	// does not get an hour of energy from the last power it reported
	maxEnergyGap = 5 * time.Minute
	// maxPlausibleKW stops a counter that jumped, because no node of this product measures more than this
	maxPlausibleKW = 25.0
)

// stored is a sample that was written to the table, which is the only kind that a rollup counts, so a retry never counts twice
type stored struct {
	t      time.Time
	watts  float64
	energy *float64
}

// bucket is the sum of one hour
type bucket struct {
	samples  int
	sumWatts float64
	maxWatts float64
	minWatts float64
	energy   float64
}

// state is what the service remembers of the last sample of a node, which it needs to find the energy between two samples
type state struct {
	at     *time.Time
	energy *float64
}

// energyBetween returns the energy in kWh between the previous sample and this one. It uses the counter of the node when the counter
// grew by a plausible amount, and the power times the time when it did not, for example after the node restarted and its counter began
// again from zero. A gap longer than maxEnergyGap counts only up to the gap.
func energyBetween(prevAt time.Time, prevEnergy *float64, s stored) float64 {
	dt := s.t.Sub(prevAt)
	if dt <= 0 {
		return 0
	}

	if prevEnergy != nil && s.energy != nil {
		delta := *s.energy - *prevEnergy
		if delta >= 0 && delta <= maxPlausibleKW*dt.Hours() {
			return delta
		}
	}

	return s.watts / 1000 * min(dt, maxEnergyGap).Hours()
}

// aggregate groups new samples by hour and works out the energy of each hour. A sample that is not newer than the last one that was
// stored adds to the power figures but not to the energy, because the energy between the samples around it was counted already.
func aggregate(prev state, samples []stored) (map[time.Time]*bucket, state) {
	sort.Slice(samples, func(i, j int) bool { return samples[i].t.Before(samples[j].t) })

	out := map[time.Time]*bucket{}
	cur := prev

	for _, s := range samples {
		hour := s.t.UTC().Truncate(time.Hour)

		b := out[hour]
		if b == nil {
			b = &bucket{maxWatts: s.watts, minWatts: s.watts}
			out[hour] = b
		}

		b.samples++
		b.sumWatts += s.watts
		b.maxWatts = max(b.maxWatts, s.watts)
		b.minWatts = min(b.minWatts, s.watts)

		if cur.at != nil && s.t.After(*cur.at) {
			b.energy += energyBetween(*cur.at, cur.energy, s)
		}

		if cur.at == nil || s.t.After(*cur.at) {
			t := s.t
			cur = state{at: &t, energy: s.energy}
		}
	}

	return out, cur
}

// apply writes the sums of new samples into the hourly table and moves the state of the node to the newest sample
func apply(ctx context.Context, tx pgx.Tx, gatewayID, nodeID string, samples []stored) error {
	if len(samples) == 0 {
		return nil
	}

	var prev state
	if err := tx.QueryRow(ctx, "SELECT last_sample_at, last_energy_kwh FROM nodes WHERE gateway_id = $1 AND node_id = $2", gatewayID, nodeID).
		Scan(&prev.at, &prev.energy); err != nil {
		return fmt.Errorf("read the state of the node %w", err)
	}

	buckets, next := aggregate(prev, samples)

	for hour, b := range buckets {
		if _, err := tx.Exec(ctx,
			`INSERT INTO readings_hourly (gateway_id, node_id, hour, samples, sum_watts, max_watts, min_watts, energy_kwh)
			 VALUES ($1, $2, $3, $4, $5, $6, $7, $8)
			 ON CONFLICT (gateway_id, node_id, hour) DO UPDATE SET
			   samples = readings_hourly.samples + EXCLUDED.samples,
			   sum_watts = readings_hourly.sum_watts + EXCLUDED.sum_watts,
			   max_watts = GREATEST(readings_hourly.max_watts, EXCLUDED.max_watts),
			   min_watts = LEAST(readings_hourly.min_watts, EXCLUDED.min_watts),
			   energy_kwh = readings_hourly.energy_kwh + EXCLUDED.energy_kwh`,
			gatewayID, nodeID, hour, b.samples, b.sumWatts, b.maxWatts, b.minWatts, b.energy); err != nil {
			return fmt.Errorf("write the hour %w", err)
		}
	}

	if _, err := tx.Exec(ctx, "UPDATE nodes SET last_sample_at = $3, last_energy_kwh = $4 WHERE gateway_id = $1 AND node_id = $2",
		gatewayID, nodeID, next.at, next.energy); err != nil {
		return fmt.Errorf("move the state of the node %w", err)
	}

	return nil
}

// PurgeTasks are the clean up tasks of the readings. The raw samples are kept 30 days and the hourly sums 400 days.
func PurgeTasks() []janitor.Task {
	return []janitor.Task{
		{
			Name:  "raw readings older than 30 days",
			Query: `DELETE FROM readings WHERE ctid IN (SELECT ctid FROM readings WHERE t < now() - interval '30 days' LIMIT $1)`,
		},
		{
			Name:  "hourly sums older than 400 days",
			Query: `DELETE FROM readings_hourly WHERE ctid IN (SELECT ctid FROM readings_hourly WHERE hour < now() - interval '400 days' LIMIT $1)`,
		},
		{
			Name:  "enrollments and pairings that ended",
			Query: `DELETE FROM gateway_enrollments WHERE ctid IN (SELECT ctid FROM gateway_enrollments WHERE expires_at < now() - interval '1 day' LIMIT $1)`,
		},
		{
			Name:  "pairings that ended a week ago",
			Query: `DELETE FROM pairings WHERE ctid IN (SELECT ctid FROM pairings WHERE status <> 'searching' AND created_at < now() - interval '7 days' LIMIT $1)`,
		},
	}
}
