// Package report builds the monthly report of a business, as data for the app and as one page of PDF
package report

import (
	"context"
	"fmt"
	"math"
	"regexp"
	"sort"
	"time"

	"github.com/jackc/pgx/v5/pgxpool"

	"github.com/aethera-deeptech/backend/internal/api"
	"github.com/aethera-deeptech/backend/internal/tips"
)

const (
	topCount = 4
	// maxPeriods is the number of months that the list offers, a little more than the 400 days that the rollups are kept
	maxPeriods = 14
)

var periodPattern = regexp.MustCompile(`^[0-9]{4}-(0[1-9]|1[0-2])$`)

// Tariff is the price that the rupiah of the report use
type Tariff struct {
	RpPerKwh  int  `json:"rpPerKwh"`
	IsDefault bool `json:"isDefault"`
}

// Verified is the saving of the tips whose check ended in the month
type Verified struct {
	Rp          int `json:"rp"`
	Count       int `json:"count"`
	CheckedDays int `json:"checkedDays"`
}

// Applied is the saving of the tips that the owner applied and that are still under check at the end of the month
type Applied struct {
	Rp         int `json:"rp"`
	Count      int `json:"count"`
	Day        int `json:"day"`
	NeededDays int `json:"neededDays"`
}

// Estimate is the saving of the tips that wait for a decision
type Estimate struct {
	Rp    int `json:"rp"`
	Count int `json:"count"`
}

// Saving holds the three sums, which are counted apart
type Saving struct {
	Verified Verified `json:"verified"`
	Applied  Applied  `json:"applied"`
	Estimate Estimate `json:"estimate"`
}

// Top is a device and its energy in the month
type Top struct {
	Name string  `json:"name"`
	Kwh  float64 `json:"kwh"`
}

// PerPiece is the energy for each piece of the whole business before and after a verified tip
type PerPiece struct {
	Status    string  `json:"status"`
	BeforeKwh float64 `json:"beforeKwh"`
	AfterKwh  float64 `json:"afterKwh"`
}

// Report is the report of one month
type Report struct {
	Phase          string     `json:"phase"`
	Period         string     `json:"period"`
	Periods        []string   `json:"periods"`
	CollectedDays  *int       `json:"collectedDays,omitempty"`
	NeededDays     *int       `json:"neededDays,omitempty"`
	Through        *time.Time `json:"through,omitempty"`
	Tariff         *Tariff    `json:"tariff,omitempty"`
	Saving         *Saving    `json:"saving,omitempty"`
	TopDevices     *[]Top     `json:"topDevices,omitempty"`
	BillRp         *int       `json:"billRp,omitempty"`
	EnergyPerPiece *PerPiece  `json:"energyPerPiece,omitempty"`

	// the fields that the PDF needs and the app does not read
	businessName string
	unit         string
}

// Service builds the reports
type Service struct {
	pool *pgxpool.Pool
	tips *tips.Service
	now  func() time.Time
}

// NewService returns the service
func NewService(pool *pgxpool.Pool, t *tips.Service) *Service {
	return &Service{pool: pool, tips: t, now: time.Now}
}

func monthLabel(t time.Time) string {
	return fmt.Sprintf("%s %d", monthNames[t.Month()-1], t.Year())
}

var monthNames = [...]string{"Januari", "Februari", "Maret", "April", "Mei", "Juni", "Juli", "Agustus", "September", "Oktober", "November", "Desember"}

// Get returns the report of a month, or of the running month when period is empty. A month with no data answers 404, and a malformed
// period answers 422.
func (s *Service) Get(ctx context.Context, businessID, period string) (Report, error) {
	if period != "" && !periodPattern.MatchString(period) {
		return Report{}, api.Validation(map[string]string{"period": "invalid"})
	}

	ov, err := s.tips.Overview(ctx, businessID)
	if err != nil {
		return Report{}, err
	}

	now := s.now().In(ov.Location)
	current := time.Date(now.Year(), now.Month(), 1, 0, 0, 0, 0, ov.Location)
	out := Report{Phase: ov.Phase, Period: current.Format("2006-01"), Periods: []string{}, businessName: ov.BusinessName}

	if ov.Phase != tips.PhaseReady {
		if period != "" && period != out.Period {
			return Report{}, api.NotFound()
		}

		if ov.Phase == tips.PhaseCollecting {
			needed := tips.NeededDays
			out.CollectedDays, out.NeededDays = &ov.CollectedDays, &needed
		}

		return out, nil
	}

	periods, err := s.periods(ctx, businessID, current, ov)
	if err != nil {
		return Report{}, err
	}

	out.Periods = periods

	if period == "" {
		period = out.Period
	}

	if !contains(periods, period) {
		return Report{}, api.NotFound()
	}

	start, err := time.ParseInLocation("2006-01", period, ov.Location)
	if err != nil {
		return Report{}, api.NotFound()
	}

	end := start.AddDate(0, 1, 0)
	out.Period = period
	out.Tariff = &Tariff{RpPerKwh: ov.Tariff, IsDefault: ov.TariffDefault}

	running := period == current.Format("2006-01")
	if running {
		out.Through = &now
	}

	top, err := s.top(ctx, businessID, start, end)
	if err != nil {
		return Report{}, err
	}

	out.TopDevices = &top

	kwh, err := s.energy(ctx, businessID, start, end)
	if err != nil {
		return Report{}, err
	}

	bill := int(math.Round(kwh * float64(ov.Tariff)))
	out.BillRp = &bill

	saving, err := s.saving(ctx, businessID, ov, start, end, now, running)
	if err != nil {
		return Report{}, err
	}

	out.Saving = &saving

	if running && ov.Newest != nil {
		out.EnergyPerPiece = &PerPiece{Status: "verified", BeforeKwh: round(ov.Newest.BeforeKwh, 2), AfterKwh: round(ov.Newest.AfterKwh, 2)}
	}

	if err := s.unit(ctx, businessID, &out); err != nil {
		return Report{}, err
	}

	return out, nil
}

func (s *Service) unit(ctx context.Context, businessID string, r *Report) error {
	if err := s.pool.QueryRow(ctx, "SELECT production_unit FROM businesses WHERE id = $1", businessID).Scan(&r.unit); err != nil {
		return fmt.Errorf("read the unit %w", err)
	}

	return nil
}

func contains(list []string, v string) bool {
	for _, x := range list {
		if x == v {
			return true
		}
	}

	return false
}

func round(v float64, decimals int) float64 {
	p := math.Pow(10, float64(decimals))

	return math.Round(v*p) / p
}

// periods lists the months from the first data of the business up to the running month, the newest first
func (s *Service) periods(ctx context.Context, businessID string, current time.Time, ov tips.Overview) ([]string, error) {
	var first *time.Time

	err := s.pool.QueryRow(ctx,
		`SELECT min(h.hour) FROM devices d JOIN readings_hourly h ON h.gateway_id = d.gateway_id AND h.node_id = d.node_id WHERE d.business_id = $1`,
		businessID).Scan(&first)
	if err != nil {
		return nil, fmt.Errorf("read the first data %w", err)
	}

	out := []string{current.Format("2006-01")}
	if first == nil {
		return out, nil
	}

	f := first.In(ov.Location)
	oldest := time.Date(f.Year(), f.Month(), 1, 0, 0, 0, 0, ov.Location)

	for m := current.AddDate(0, -1, 0); !m.Before(oldest) && len(out) < maxPeriods; m = m.AddDate(0, -1, 0) {
		out = append(out, m.Format("2006-01"))
	}

	return out, nil
}

func (s *Service) energy(ctx context.Context, businessID string, start, end time.Time) (float64, error) {
	var kwh float64

	err := s.pool.QueryRow(ctx,
		`SELECT COALESCE(sum(h.energy_kwh), 0) FROM devices d JOIN readings_hourly h ON h.gateway_id = d.gateway_id AND h.node_id = d.node_id
		 WHERE d.business_id = $1 AND h.hour >= $2 AND h.hour < $3`, businessID, start.UTC(), end.UTC()).Scan(&kwh)
	if err != nil {
		return 0, fmt.Errorf("read the energy %w", err)
	}

	return kwh, nil
}

func (s *Service) top(ctx context.Context, businessID string, start, end time.Time) ([]Top, error) {
	rows, err := s.pool.Query(ctx,
		`SELECT d.name, sum(h.energy_kwh) AS kwh FROM devices d JOIN readings_hourly h ON h.gateway_id = d.gateway_id AND h.node_id = d.node_id
		 WHERE d.business_id = $1 AND h.hour >= $2 AND h.hour < $3 GROUP BY d.id, d.name ORDER BY kwh DESC, d.name LIMIT $4`,
		businessID, start.UTC(), end.UTC(), topCount)
	if err != nil {
		return nil, fmt.Errorf("read the top devices %w", err)
	}
	defer rows.Close()

	out := []Top{}

	for rows.Next() {
		var t Top
		if err := rows.Scan(&t.Name, &t.Kwh); err != nil {
			return nil, fmt.Errorf("read a top device %w", err)
		}

		t.Kwh = round(t.Kwh, 1)
		out = append(out, t)
	}

	return out, rows.Err()
}

// saving sorts the tips into the three groups. A tip is in one group only. The history of a tip is its time stamps, so a month in the
// past counts what was verified in it and what was under check at its end, and it has no waiting tips because the service does not keep
// the time that a tip was waiting.
func (s *Service) saving(ctx context.Context, businessID string, ov tips.Overview, start, end, now time.Time, running bool) (Saving, error) {
	rows, err := s.pool.Query(ctx,
		"SELECT status, applied_at, verified_at, saving_kwh_month, measured_kwh_month FROM tips WHERE business_id = $1", businessID)
	if err != nil {
		return Saving{}, fmt.Errorf("read the tips %w", err)
	}
	defer rows.Close()

	out := Saving{Verified: Verified{CheckedDays: tips.VerifyDays}, Applied: Applied{NeededDays: tips.VerifyDays}}

	var newestApplied *time.Time

	for rows.Next() {
		var (
			status            string
			applied, verified *time.Time
			estimateKwh       float64
			measured          *float64
		)

		if err := rows.Scan(&status, &applied, &verified, &estimateKwh, &measured); err != nil {
			return Saving{}, fmt.Errorf("read a tip %w", err)
		}

		switch {
		case verified != nil && !verified.Before(start) && verified.Before(end) && measured != nil:
			out.Verified.Count++
			out.Verified.Rp += tips.RoundRp(*measured, ov.Tariff)
		case applied != nil && applied.Before(end) && (verified == nil || !verified.Before(end)) && (status == tips.StatusApplied || status == tips.StatusVerified):
			out.Applied.Count++
			out.Applied.Rp += tips.RoundRp(estimateKwh, ov.Tariff)

			if newestApplied == nil || applied.After(*newestApplied) {
				newestApplied = applied
			}
		case running && status == tips.StatusPending:
			out.Estimate.Count++
			out.Estimate.Rp += tips.RoundRp(estimateKwh, ov.Tariff)
		}
	}

	if err := rows.Err(); err != nil {
		return Saving{}, fmt.Errorf("read the tips %w", err)
	}

	if newestApplied != nil {
		upTo := now
		if end.Before(now) {
			upTo = end.Add(-time.Second)
		}

		days := int(day(upTo, ov.Location).Sub(day(*newestApplied, ov.Location))/(24*time.Hour)) + 1
		out.Applied.Day = min(max(days, 1), tips.VerifyDays)
	}

	return out, nil
}

func day(t time.Time, loc *time.Location) time.Time {
	l := t.In(loc)

	return time.Date(l.Year(), l.Month(), l.Day(), 0, 0, 0, 0, loc)
}

// sortedTop is for the PDF, which draws the bars against the largest value
func sortedTop(list []Top) []Top {
	out := append([]Top(nil), list...)
	sort.SliceStable(out, func(i, j int) bool { return out[i].Kwh > out[j].Kwh })

	return out
}
