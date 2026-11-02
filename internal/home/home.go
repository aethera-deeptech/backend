// Package home builds the home screen of a business from its devices, its tips and its energy
package home

import (
	"context"
	"fmt"
	"math"
	"time"

	"github.com/gofiber/fiber/v3"
	"github.com/jackc/pgx/v5/pgxpool"

	"github.com/aethera-deeptech/backend/internal/auth"
	"github.com/aethera-deeptech/backend/internal/device"
	"github.com/aethera-deeptech/backend/internal/tips"
)

const topCount = 4

// Counts is the number of devices in each status
type Counts struct {
	Anomali int `json:"anomali"`
	Offline int `json:"offline"`
	Siaga   int `json:"siaga"`
	Nyala   int `json:"nyala"`
	Mati    int `json:"mati"`
}

// Bill is the bill of the month so far and what it is expected to be
type Bill struct {
	DailyTotalRp     []int `json:"dailyTotalRp"`
	ProjectedTotalRp int   `json:"projectedTotalRp"`
	PotentialTotalRp int   `json:"potentialTotalRp"`
}

// Top is a device and its energy this month
type Top struct {
	Name string  `json:"name"`
	Kwh  float64 `json:"kwh"`
}

// PerPiece is the energy for each piece of the whole business. Only a verified check has numbers in it.
type PerPiece struct {
	Status    string  `json:"status"`
	BeforeKwh float64 `json:"beforeKwh"`
	AfterKwh  float64 `json:"afterKwh"`
}

// Screen is the answer of the home route
type Screen struct {
	Phase            string          `json:"phase"`
	BusinessName     string          `json:"businessName"`
	AsOf             time.Time       `json:"asOf"`
	CollectedDays    *int            `json:"collectedDays,omitempty"`
	NeededDays       *int            `json:"neededDays,omitempty"`
	SavingPerMonthRp *int            `json:"savingPerMonthRp,omitempty"`
	StatusCounts     *Counts         `json:"statusCounts,omitempty"`
	Recommendations  *[]tips.Waiting `json:"recommendations,omitempty"`
	Bill             *Bill           `json:"bill,omitempty"`
	TopDevices       *[]Top          `json:"topDevices,omitempty"`
	EnergyPerPiece   *PerPiece       `json:"energyPerPiece,omitempty"`
}

// Service builds the screen
type Service struct {
	pool    *pgxpool.Pool
	tips    *tips.Service
	devices *device.Views
	now     func() time.Time
}

// NewService returns the service
func NewService(pool *pgxpool.Pool, t *tips.Service, d *device.Views) *Service {
	return &Service{pool: pool, tips: t, devices: d, now: time.Now}
}

// Screen returns the home screen. The shape follows the phase, so a business with no devices or too little data gets no numbers that
// would be guesses.
func (s *Service) Screen(ctx context.Context, businessID string) (Screen, error) {
	ov, err := s.tips.Overview(ctx, businessID)
	if err != nil {
		return Screen{}, err
	}

	now := s.now()
	out := Screen{Phase: ov.Phase, BusinessName: ov.BusinessName, AsOf: now.In(ov.Location)}

	if ov.Phase == tips.PhaseNoDevices {
		return out, nil
	}

	monthStart := time.Date(out.AsOf.Year(), out.AsOf.Month(), 1, 0, 0, 0, 0, ov.Location)

	top, err := s.top(ctx, businessID, monthStart)
	if err != nil {
		return Screen{}, err
	}

	out.TopDevices = &top

	if ov.Phase == tips.PhaseCollecting {
		needed := tips.NeededDays
		out.CollectedDays, out.NeededDays = &ov.CollectedDays, &needed

		return out, nil
	}

	items, err := s.devices.List(ctx, businessID)
	if err != nil {
		return Screen{}, err
	}

	counts := Counts{}

	for _, it := range items {
		switch it.Status {
		case device.StatusAnomaly:
			counts.Anomali++
		case device.StatusOffline:
			counts.Offline++
		case device.StatusStandby:
			counts.Siaga++
		case device.StatusOn:
			counts.Nyala++
		default:
			counts.Mati++
		}
	}

	saving := 0
	for _, w := range ov.Waiting {
		saving += w.SavingPerMonthRp
	}

	bill, err := s.bill(ctx, businessID, ov, monthStart, saving)
	if err != nil {
		return Screen{}, err
	}

	waiting := ov.Waiting
	if waiting == nil {
		waiting = []tips.Waiting{}
	}

	out.StatusCounts, out.SavingPerMonthRp, out.Recommendations, out.Bill = &counts, &saving, &waiting, bill

	if ov.Newest != nil {
		out.EnergyPerPiece = &PerPiece{Status: "verified", BeforeKwh: round(ov.Newest.BeforeKwh, 2), AfterKwh: round(ov.Newest.AfterKwh, 2)}
	}

	return out, nil
}

func round(v float64, decimals int) float64 {
	p := math.Pow(10, float64(decimals))

	return math.Round(v*p) / p
}

// top returns the devices that used the most energy this month
func (s *Service) top(ctx context.Context, businessID string, monthStart time.Time) ([]Top, error) {
	rows, err := s.pool.Query(ctx,
		`SELECT d.name, sum(h.energy_kwh) AS kwh FROM devices d JOIN readings_hourly h ON h.gateway_id = d.gateway_id AND h.node_id = d.node_id
		 WHERE d.business_id = $1 AND h.hour >= $2 GROUP BY d.id, d.name ORDER BY kwh DESC, d.name LIMIT $3`,
		businessID, monthStart.UTC(), topCount)
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

// bill works out the running total of the month in rupiah for each day, the total that the month is on course for, and the same
// total if the waiting tips had been done from the first day. The energy is what the devices measured. The load that no device
// watches is not in it, because the service has no meter for it.
func (s *Service) bill(ctx context.Context, businessID string, ov tips.Overview, monthStart time.Time, saving int) (*Bill, error) {
	rows, err := s.pool.Query(ctx,
		`SELECT (h.hour AT TIME ZONE $3)::date, sum(h.energy_kwh) FROM devices d
		 JOIN readings_hourly h ON h.gateway_id = d.gateway_id AND h.node_id = d.node_id
		 WHERE d.business_id = $1 AND h.hour >= $2 GROUP BY 1 ORDER BY 1`,
		businessID, monthStart.UTC(), ov.Location.String())
	if err != nil {
		return nil, fmt.Errorf("read the daily energy %w", err)
	}
	defer rows.Close()

	perDay := map[int]float64{}

	for rows.Next() {
		var (
			d   time.Time
			kwh float64
		)

		if err := rows.Scan(&d, &kwh); err != nil {
			return nil, fmt.Errorf("read a day %w", err)
		}

		perDay[d.Day()] = kwh
	}

	if err := rows.Err(); err != nil {
		return nil, fmt.Errorf("read the days %w", err)
	}

	now := s.now().In(ov.Location)
	daysInMonth := monthStart.AddDate(0, 1, 0).Sub(monthStart).Hours() / 24
	daysInMonth = math.Round(daysInMonth)

	var (
		total float64
		daily = make([]int, 0, now.Day())
	)

	for d := 1; d <= now.Day(); d++ {
		total += perDay[d]
		daily = append(daily, int(math.Round(total*float64(ov.Tariff))))
	}

	// the run rate of the days so far, and never less than what is already spent
	elapsed := math.Max(1, now.Sub(monthStart).Hours()/24)
	projected := int(math.Round(total / elapsed * daysInMonth * float64(ov.Tariff)))
	projected = max(projected, daily[len(daily)-1])

	return &Bill{DailyTotalRp: daily, ProjectedTotalRp: projected, PotentialTotalRp: max(0, projected-saving)}, nil
}

// Handlers are the routes of the home screen
type Handlers struct {
	svc *Service
}

// NewHandlers returns the routes
func NewHandlers(svc *Service) *Handlers {
	return &Handlers{svc: svc}
}

// Register adds the routes behind the middleware that needs a signed in user
func (h *Handlers) Register(r fiber.Router, require fiber.Handler) {
	r.Get("/beranda", require, h.get)
}

func (h *Handlers) get(c fiber.Ctx) error {
	out, err := h.svc.Screen(c.Context(), auth.UserFrom(c).BusinessID)
	if err != nil {
		return err
	}

	return c.JSON(out)
}
