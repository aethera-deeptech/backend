package tips

import (
	"context"
	"errors"
	"fmt"
	"time"

	"github.com/jackc/pgx/v5"
)

// Waiting is a tip that waits for a decision, as the home screen shows it
type Waiting struct {
	ID               string `json:"id"`
	Title            string `json:"title"`
	DeviceName       string `json:"deviceName"`
	SavingPerMonthRp int    `json:"savingPerMonthRp"`
}

// Verified is the check of the newest verified tip
type Verified struct {
	BeforeKwh float64
	AfterKwh  float64
}

// Overview is what the home screen and the report need to know about the tips and the phase
type Overview struct {
	Phase         string
	CollectedDays int
	BusinessName  string
	Location      *time.Location
	Tariff        int
	TariffDefault bool
	// Waiting holds the tips that wait for a decision, the biggest saving first. A deferred tip does not wait, the owner said later.
	Waiting []Waiting
	// Newest is the check of the tip that was verified last, or nil
	Newest *Verified
}

// Overview refreshes the tips and returns the phase and the tips that the home screen shows
func (s *Service) Overview(ctx context.Context, businessID string) (Overview, error) {
	if err := s.Refresh(ctx, businessID); err != nil {
		return Overview{}, err
	}

	p, err := s.profile(ctx, s.pool, businessID)
	if err != nil {
		return Overview{}, err
	}

	phase, days, err := s.Phase(ctx, businessID)
	if err != nil {
		return Overview{}, err
	}

	out := Overview{Phase: phase, CollectedDays: days, BusinessName: p.name, Location: p.loc, Tariff: p.tariff, TariffDefault: p.tariffDefault}
	if phase != PhaseReady {
		return out, nil
	}

	rows, err := s.pool.Query(ctx, tipSQL+" AND t.status = 'pending'", businessID)
	if err != nil {
		return Overview{}, fmt.Errorf("list the waiting tips %w", err)
	}
	defer rows.Close()

	for rows.Next() {
		r, err := scanTip(rows)
		if err != nil {
			return Overview{}, fmt.Errorf("read a waiting tip %w", err)
		}

		out.Waiting = append(out.Waiting, Waiting{ID: r.id, Title: r.facts(p).title(), DeviceName: r.deviceName, SavingPerMonthRp: r.savingRp(p)})
	}

	if err := rows.Err(); err != nil {
		return Overview{}, fmt.Errorf("read the waiting tips %w", err)
	}

	sortWaiting(out.Waiting)

	var v Verified

	err = s.pool.QueryRow(ctx,
		"SELECT before_kwh_per_piece, after_kwh_per_piece FROM tips WHERE business_id = $1 AND status = 'verified' AND before_kwh_per_piece IS NOT NULL ORDER BY verified_at DESC LIMIT 1",
		businessID).Scan(&v.BeforeKwh, &v.AfterKwh)

	switch {
	case err == nil:
		out.Newest = &v
	case !errors.Is(err, pgx.ErrNoRows):
		return Overview{}, fmt.Errorf("read the newest check %w", err)
	}

	return out, nil
}
