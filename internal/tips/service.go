package tips

import (
	"context"
	"errors"
	"fmt"
	"log/slog"
	"math"
	"sort"
	"time"

	"github.com/jackc/pgx/v5"
	"github.com/jackc/pgx/v5/pgconn"
	"github.com/jackc/pgx/v5/pgxpool"

	"github.com/aethera-deeptech/backend/internal/api"
	"github.com/aethera-deeptech/backend/internal/business"
)

const (
	refreshEvery     = 10 * time.Minute
	lockNotAvailable = "55P03"
	placeholderName  = "Usaha saya"
)

// The phases of a business, which the home screen, the tips and the report share
const (
	PhaseNoDevices  = "no-devices"
	PhaseCollecting = "collecting"
	PhaseReady      = "ready"
)

// The states of a tip
const (
	StatusPending  = "pending"
	StatusDeferred = "deferred"
	StatusApplied  = "applied"
	StatusVerified = "verified"
	StatusRejected = "rejected"
)

var statusRank = map[string]int{StatusPending: 0, StatusDeferred: 1, StatusApplied: 2, StatusVerified: 3, StatusRejected: 4}

// Progress is how far the check of an applied tip has come
type Progress struct {
	CollectedDays int `json:"collectedDays"`
	NeededDays    int `json:"neededDays"`
}

// Item is one row of the list of tips
type Item struct {
	ID               string    `json:"id"`
	Title            string    `json:"title"`
	SavingPerMonthRp int       `json:"savingPerMonthRp"`
	Status           string    `json:"status"`
	Progress         *Progress `json:"progress,omitempty"`
	ResultPercent    *float64  `json:"resultPercent,omitempty"`
}

// List is the answer of the list route
type List struct {
	Phase        string `json:"phase"`
	BusinessName string `json:"businessName"`
	NeededDays   *int   `json:"neededDays,omitempty"`
	// Items is a pointer, so an empty list is still written as [] and the other phases leave it out
	Items *[]Item `json:"items,omitempty"`
}

// Result is what the check of a tip found
type Result struct {
	BeforeKwh  float64 `json:"beforeKwh"`
	AfterKwh   float64 `json:"afterKwh"`
	BeforeDays int     `json:"beforeDays"`
	AfterDays  int     `json:"afterDays"`
}

// Detail is the screen of one tip
type Detail struct {
	ID               string     `json:"id"`
	Title            string     `json:"title"`
	DeviceName       string     `json:"deviceName"`
	Status           string     `json:"status"`
	SavingPerMonthRp int        `json:"savingPerMonthRp"`
	TariffIsDefault  bool       `json:"tariffIsDefault"`
	WhatToDo         string     `json:"whatToDo"`
	Why              string     `json:"why"`
	HowCalculated    []string   `json:"howCalculated"`
	AppliedAt        *time.Time `json:"appliedAt,omitempty"`
	RejectedAt       *time.Time `json:"rejectedAt,omitempty"`
	Verification     *Progress  `json:"verification,omitempty"`
	Result           *Result    `json:"result,omitempty"`
}

// Service works out the tips and keeps their states
type Service struct {
	pool *pgxpool.Pool
	now  func() time.Time
}

// NewService returns the service on the pool
func NewService(pool *pgxpool.Pool) *Service {
	return &Service{pool: pool, now: time.Now}
}

// ErrInvalidTransition is the answer for a decision that the state of the tip does not allow
func errInvalidTransition() *api.Error {
	return api.NewError(409, "invalid_transition", "The tip is not in a state that allows this decision")
}

type profile struct {
	name          string
	loc           *time.Location
	work          business.WorkHours
	tariff        int
	tariffDefault bool
	refreshedAt   *time.Time
}

func (s *Service) profile(ctx context.Context, q interface {
	QueryRow(context.Context, string, ...any) pgx.Row
}, businessID string,
) (profile, error) {
	var (
		p      profile
		zone   string
		tariff *int
	)

	err := q.QueryRow(ctx,
		`SELECT COALESCE(name, $2), time_zone, work_days, to_char(work_start, 'HH24:MI'), to_char(work_end, 'HH24:MI'),
		 tariff_rp_per_kwh, tips_refreshed_at FROM businesses WHERE id = $1`, businessID, placeholderName).
		Scan(&p.name, &zone, &p.work.Days, &p.work.Start, &p.work.End, &tariff, &p.refreshedAt)
	if err != nil {
		return profile{}, fmt.Errorf("read the business %w", err)
	}

	p.loc, err = time.LoadLocation(zone)
	if err != nil {
		p.loc = time.UTC
	}

	p.tariff, p.tariffDefault = business.DefaultTariffRpPerKwh, tariff == nil
	if tariff != nil {
		p.tariff = *tariff
	}

	return p, nil
}

// Phase says whether the business has devices, has collected enough days, or is ready for tips. The days are counted from the first
// hour of data of any device of the business.
func (s *Service) Phase(ctx context.Context, businessID string) (string, int, error) {
	return s.phaseOn(ctx, s.pool, businessID)
}

// phaseOn is Phase on a connection of the caller, so a refresh does not need a second connection of the pool while it holds one
func (s *Service) phaseOn(ctx context.Context, q interface {
	QueryRow(context.Context, string, ...any) pgx.Row
}, businessID string,
) (string, int, error) {
	var (
		devices int
		first   *time.Time
	)

	// the first hour is one index probe for each device, because the table holds up to 400 days of hours for every node
	err := q.QueryRow(ctx,
		`SELECT (SELECT count(*) FROM devices WHERE business_id = $1),
		 (SELECT min(f.hour) FROM devices d CROSS JOIN LATERAL
		   (SELECT h.hour FROM readings_hourly h WHERE h.gateway_id = d.gateway_id AND h.node_id = d.node_id ORDER BY h.hour LIMIT 1) f
		  WHERE d.business_id = $1)`,
		businessID).Scan(&devices, &first)
	if err != nil {
		return "", 0, fmt.Errorf("read the phase %w", err)
	}

	if devices == 0 {
		return PhaseNoDevices, 0, nil
	}

	days := 0
	if first != nil {
		days = int(s.now().Sub(*first) / (24 * time.Hour))
	}

	if days < NeededDays {
		return PhaseCollecting, max(days, 0), nil
	}

	return PhaseReady, NeededDays, nil
}

// Refresh verifies the tips that are due and makes new ones, at most every few minutes for a business. A business that is busy with
// another refresh is left alone, because the tips it reads are only a few minutes old.
func (s *Service) Refresh(ctx context.Context, businessID string) error {
	// a plain read first, so a read inside the window takes no lock and writes nothing
	var seen *time.Time
	if err := s.pool.QueryRow(ctx, "SELECT tips_refreshed_at FROM businesses WHERE id = $1", businessID).Scan(&seen); err != nil {
		return fmt.Errorf("read the refresh time %w", err)
	}

	if seen != nil && s.now().Sub(*seen) < refreshEvery {
		return nil
	}

	tx, err := s.pool.BeginTx(ctx, pgx.TxOptions{})
	if err != nil {
		return fmt.Errorf("start the refresh %w", err)
	}

	defer func() { _ = tx.Rollback(ctx) }()

	if _, err := tx.Exec(ctx, "SET LOCAL lock_timeout = '2s'"); err != nil {
		return fmt.Errorf("set the lock time %w", err)
	}

	var locked *time.Time
	if err := tx.QueryRow(ctx, "SELECT tips_refreshed_at FROM businesses WHERE id = $1 FOR NO KEY UPDATE", businessID).Scan(&locked); err != nil {
		var pgErr *pgconn.PgError
		if errors.As(err, &pgErr) && pgErr.Code == lockNotAvailable {
			return nil
		}

		return fmt.Errorf("lock the business %w", err)
	}

	now := s.now()
	if locked != nil && now.Sub(*locked) < refreshEvery {
		return nil
	}

	p, err := s.profile(ctx, tx, businessID)
	if err != nil {
		return err
	}

	if err := s.verifyDue(ctx, tx, businessID, p, now); err != nil {
		return err
	}

	if phase, _, err := s.phaseOn(ctx, tx, businessID); err != nil {
		return err
	} else if phase == PhaseReady {
		if err := s.generate(ctx, tx, businessID, p, now); err != nil {
			return err
		}
	}

	if _, err := tx.Exec(ctx, "UPDATE businesses SET tips_refreshed_at = $2 WHERE id = $1", businessID, now); err != nil {
		return fmt.Errorf("mark the refresh %w", err)
	}

	return tx.Commit(ctx)
}

// generate makes a tip for every device that draws power outside the work time, and works out the saving of the waiting ones again
func (s *Service) generate(ctx context.Context, tx pgx.Tx, businessID string, p profile, now time.Time) error {
	end := now.UTC().Truncate(time.Hour)
	start := end.Add(-BaselineDays * 24 * time.Hour)

	rows, err := tx.Query(ctx,
		`SELECT d.id, h.hour, h.energy_kwh, h.sum_watts / h.samples FROM devices d
		 JOIN readings_hourly h ON h.gateway_id = d.gateway_id AND h.node_id = d.node_id
		 WHERE d.business_id = $1 AND h.hour >= $2 AND h.hour < $3 ORDER BY d.id, h.hour`,
		businessID, start, end)
	if err != nil {
		return fmt.Errorf("read the hours %w", err)
	}

	byDevice := map[string][]hourRow{}

	for rows.Next() {
		var (
			id string
			r  hourRow
		)

		if err := rows.Scan(&id, &r.hour, &r.energy, &r.watts); err != nil {
			rows.Close()

			return fmt.Errorf("read an hour %w", err)
		}

		byDevice[id] = append(byDevice[id], r)
	}

	rows.Close()

	if err := rows.Err(); err != nil {
		return fmt.Errorf("read the hours %w", err)
	}

	// keep holds the device and kind of every tip that still holds, so a waiting tip of a device that no longer qualifies, or that
	// changed kind, goes away and the saving of one device is never counted twice
	keep := []string{}

	for id, list := range byDevice {
		kind, kwh := classify(list, p.loc, p.work, BaselineDays)
		if kind == "" || RoundRp(kwh, p.tariff) < minSavingRp {
			continue
		}

		keep = append(keep, id+":"+kind)

		if _, err := tx.Exec(ctx,
			`INSERT INTO tips (business_id, device_id, kind, saving_kwh_month) VALUES ($1, $2, $3, $4)
			 ON CONFLICT (device_id, kind) DO UPDATE SET saving_kwh_month = EXCLUDED.saving_kwh_month
			 WHERE tips.status IN ('pending', 'deferred')`,
			businessID, id, kind, kwh); err != nil {
			return fmt.Errorf("store the tip %w", err)
		}
	}

	// only a tip that nobody decided on is removed, the other states are the history of the business
	if _, err := tx.Exec(ctx,
		"DELETE FROM tips WHERE business_id = $1 AND status = 'pending' AND NOT (device_id::text || ':' || kind) = ANY($2)", businessID, keep); err != nil {
		return fmt.Errorf("remove the tips that no longer hold %w", err)
	}

	return nil
}

// day returns the date of a moment in the time zone of the business
func day(t time.Time, loc *time.Location) time.Time {
	l := t.In(loc)

	return time.Date(l.Year(), l.Month(), l.Day(), 0, 0, 0, 0, loc)
}

// window sums the energy of all devices of the business and the production of the days from first up to but not including last
func (s *Service) window(ctx context.Context, q interface {
	QueryRow(context.Context, string, ...any) pgx.Row
}, businessID string, loc *time.Location, first, last time.Time,
) (float64, int, error) {
	var (
		kwh    float64
		pieces int
	)

	err := q.QueryRow(ctx,
		`SELECT COALESCE((SELECT sum(h.energy_kwh) FROM devices d JOIN readings_hourly h ON h.gateway_id = d.gateway_id AND h.node_id = d.node_id
		   WHERE d.business_id = $1 AND h.hour >= $2 AND h.hour < $3
		     AND EXISTS (SELECT 1 FROM production_records r WHERE r.business_id = $1 AND r.day = (h.hour AT TIME ZONE $6)::date)), 0),
		 COALESCE((SELECT sum(quantity) FROM production_records WHERE business_id = $1 AND day >= $4::date AND day < $5::date), 0)`,
		businessID, first.UTC(), last.UTC(), first.Format("2006-01-02"), last.Format("2006-01-02"), loc.String()).Scan(&kwh, &pieces)
	if err != nil {
		return 0, 0, fmt.Errorf("read the energy for each piece %w", err)
	}

	return kwh, pieces, nil
}

// verifyDue closes the check of every applied tip whose 14 days are over. It needs production records in the 7 days before and in
// the 14 days after, because the check compares energy for each piece. A tip without them stays applied.
func (s *Service) verifyDue(ctx context.Context, tx pgx.Tx, businessID string, p profile, now time.Time) error {
	rows, err := tx.Query(ctx, "SELECT id, applied_at FROM tips WHERE business_id = $1 AND status = 'applied'", businessID)
	if err != nil {
		return fmt.Errorf("read the applied tips %w", err)
	}

	type due struct {
		id string
		at time.Time
	}

	var list []due

	for rows.Next() {
		var d due
		if err := rows.Scan(&d.id, &d.at); err != nil {
			rows.Close()

			return fmt.Errorf("read an applied tip %w", err)
		}

		list = append(list, d)
	}

	rows.Close()

	if err := rows.Err(); err != nil {
		return fmt.Errorf("read the applied tips %w", err)
	}

	today := day(now, p.loc)

	for _, d := range list {
		from := day(d.at, p.loc)
		afterEnd := from.AddDate(0, 0, VerifyDays)

		if today.Before(afterEnd) {
			continue
		}

		beforeKwh, beforePieces, err := s.window(ctx, tx, businessID, p.loc, from.AddDate(0, 0, -BaselineDays), from)
		if err != nil {
			return err
		}

		afterKwh, afterPieces, err := s.window(ctx, tx, businessID, p.loc, from, afterEnd)
		if err != nil {
			return err
		}

		if beforePieces == 0 || afterPieces == 0 || beforeKwh <= 0 {
			continue
		}

		before, after := beforeKwh/float64(beforePieces), afterKwh/float64(afterPieces)
		percent := (before - after) / before * 100
		measured := math.Max(0, percent/100*beforeKwh/BaselineDays*monthDays)

		if _, err := tx.Exec(ctx,
			`UPDATE tips SET status = 'verified', verified_at = $2, before_kwh_per_piece = $3, after_kwh_per_piece = $4, measured_kwh_month = $5
			 WHERE id = $1 AND status = 'applied'`, d.id, now, before, after, measured); err != nil {
			return fmt.Errorf("close the check %w", err)
		}
	}

	return nil
}

type tipRow struct {
	id, deviceName, deviceKind, kind, status string
	kwhMonth                                 float64
	appliedAt, rejectedAt                    *time.Time
	before, after, measured                  *float64
}

const tipSQL = `SELECT t.id, d.name, d.kind, t.kind, t.status, t.saving_kwh_month, t.applied_at, t.rejected_at,
	t.before_kwh_per_piece, t.after_kwh_per_piece, t.measured_kwh_month
	FROM tips t JOIN devices d ON d.id = t.device_id WHERE t.business_id = $1`

func scanTip(row pgx.Row) (tipRow, error) {
	var r tipRow

	err := row.Scan(&r.id, &r.deviceName, &r.deviceKind, &r.kind, &r.status, &r.kwhMonth, &r.appliedAt, &r.rejectedAt, &r.before, &r.after, &r.measured)

	return r, err
}

func (r tipRow) facts(p profile) facts {
	f := facts{kind: r.kind, deviceKind: r.deviceKind, name: r.deviceName, status: r.status, kwhMonth: r.kwhMonth,
		tariff: p.tariff, tariffDefault: p.tariffDefault}

	if r.status == StatusVerified && r.before != nil && r.after != nil && r.measured != nil && *r.before > 0 {
		f.hasResult = true
		f.beforeKwh, f.afterKwh, f.measuredKwh = *r.before, *r.after, *r.measured
		f.percent = (*r.before - *r.after) / *r.before * 100
	}

	return f
}

// savingRp is the money of a tip, the measured one after the check and the estimate before it
func (r tipRow) savingRp(p profile) int {
	if r.status == StatusVerified && r.measured != nil {
		return RoundRp(*r.measured, p.tariff)
	}

	return RoundRp(r.kwhMonth, p.tariff)
}

// elapsed is the day of the check that the business is on, where the day of the decision is day 1
func (r tipRow) elapsed(p profile, now time.Time) int {
	if r.appliedAt == nil {
		return 0
	}

	days := int(day(now, p.loc).Sub(day(*r.appliedAt, p.loc))/(24*time.Hour)) + 1

	return min(max(days, 1), VerifyDays)
}

// refresh works out the tips again, and when that fails it logs and goes on, because the tips of the last refresh can still be read
func (s *Service) refresh(ctx context.Context, businessID string) {
	if err := s.Refresh(ctx, businessID); err != nil {
		slog.ErrorContext(ctx, "the tips could not be refreshed", "error", err)
	}
}

// List returns the tips in the order of the screen, the ones that wait first
func (s *Service) List(ctx context.Context, businessID string) (List, error) {
	s.refresh(ctx, businessID)

	p, err := s.profile(ctx, s.pool, businessID)
	if err != nil {
		return List{}, err
	}

	phase, _, err := s.Phase(ctx, businessID)
	if err != nil {
		return List{}, err
	}

	out := List{Phase: phase, BusinessName: p.name}

	switch phase {
	case PhaseNoDevices:
		return out, nil
	case PhaseCollecting:
		n := NeededDays
		out.NeededDays = &n

		return out, nil
	}

	rows, err := s.pool.Query(ctx, tipSQL, businessID)
	if err != nil {
		return List{}, fmt.Errorf("list the tips %w", err)
	}
	defer rows.Close()

	now := s.now()
	items := []tipRow{}

	for rows.Next() {
		r, err := scanTip(rows)
		if err != nil {
			return List{}, fmt.Errorf("read a tip %w", err)
		}

		items = append(items, r)
	}

	if err := rows.Err(); err != nil {
		return List{}, fmt.Errorf("read the tips %w", err)
	}

	sort.SliceStable(items, func(i, j int) bool {
		a, b := items[i], items[j]
		if statusRank[a.status] != statusRank[b.status] {
			return statusRank[a.status] < statusRank[b.status]
		}

		return a.savingRp(p) > b.savingRp(p)
	})

	list := []Item{}

	for _, r := range items {
		f := r.facts(p)
		it := Item{ID: r.id, Title: f.title(), SavingPerMonthRp: r.savingRp(p), Status: r.status}

		switch r.status {
		case StatusApplied:
			it.Progress = &Progress{CollectedDays: r.elapsed(p, now), NeededDays: VerifyDays}
		case StatusVerified:
			if f.hasResult {
				pct := math.Round(f.percent*10) / 10
				it.ResultPercent = &pct
			}
		}

		list = append(list, it)
	}

	out.Items = &list

	return out, nil
}

// Get returns one tip
func (s *Service) Get(ctx context.Context, businessID, id string) (Detail, error) {
	if !validID(id) {
		return Detail{}, api.NotFound()
	}

	s.refresh(ctx, businessID)

	return s.detail(ctx, businessID, id)
}

func (s *Service) detail(ctx context.Context, businessID, id string) (Detail, error) {
	p, err := s.profile(ctx, s.pool, businessID)
	if err != nil {
		return Detail{}, err
	}

	r, err := scanTip(s.pool.QueryRow(ctx, tipSQL+" AND t.id = $2", businessID, id))
	if errors.Is(err, pgx.ErrNoRows) {
		return Detail{}, api.NotFound()
	}

	if err != nil {
		return Detail{}, fmt.Errorf("read the tip %w", err)
	}

	f := r.facts(p)
	t := f.all()

	out := Detail{ID: r.id, Title: t.Title, DeviceName: r.deviceName, Status: r.status, SavingPerMonthRp: r.savingRp(p),
		TariffIsDefault: p.tariffDefault, WhatToDo: t.WhatToDo, Why: t.Why, HowCalculated: t.How, AppliedAt: r.appliedAt, RejectedAt: r.rejectedAt}

	if r.status == StatusApplied {
		out.Verification = &Progress{CollectedDays: r.elapsed(p, s.now()), NeededDays: VerifyDays}
	}

	if f.hasResult {
		out.Result = &Result{BeforeKwh: math.Round(f.beforeKwh*100) / 100, AfterKwh: math.Round(f.afterKwh*100) / 100,
			BeforeDays: BaselineDays, AfterDays: VerifyDays}
	}

	return out, nil
}

var transitions = map[string]struct {
	from []string
	to   string
	set  string
}{
	"apply":  {[]string{StatusPending, StatusDeferred}, StatusApplied, "applied_at = now()"},
	"defer":  {[]string{StatusPending}, StatusDeferred, ""},
	"reject": {[]string{StatusPending, StatusDeferred}, StatusRejected, "rejected_at = now()"},
	"cancel": {[]string{StatusApplied}, StatusPending, "applied_at = NULL"},
}

// Decide moves a tip to the state that the decision leads to, and refuses a decision that its state does not allow
func (s *Service) Decide(ctx context.Context, businessID, id, action string) (Detail, error) {
	tr, ok := transitions[action]
	if !ok || !validID(id) {
		return Detail{}, api.NotFound()
	}

	set := ""
	if tr.set != "" {
		set = ", " + tr.set
	}

	tag, err := s.pool.Exec(ctx, "UPDATE tips SET status = $3"+set+" WHERE id = $1 AND business_id = $2 AND status = ANY($4)",
		id, businessID, tr.to, tr.from)
	if err != nil {
		return Detail{}, fmt.Errorf("decide on the tip %w", err)
	}

	if tag.RowsAffected() == 0 {
		var exists bool
		if err := s.pool.QueryRow(ctx, "SELECT EXISTS (SELECT 1 FROM tips WHERE id = $1 AND business_id = $2)", id, businessID).Scan(&exists); err != nil {
			return Detail{}, fmt.Errorf("read the tip %w", err)
		}

		if !exists {
			return Detail{}, api.NotFound()
		}

		return Detail{}, errInvalidTransition()
	}

	return s.detail(ctx, businessID, id)
}
