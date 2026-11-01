// Package production holds the daily production records of a business, which the service uses to work out the energy for each piece
package production

import (
	"context"
	"errors"
	"fmt"
	"time"

	"github.com/gofiber/fiber/v3"
	"github.com/jackc/pgx/v5"
	"github.com/jackc/pgx/v5/pgxpool"

	"github.com/aethera-deeptech/backend/internal/api"
	"github.com/aethera-deeptech/backend/internal/auth"
)

const (
	dateLayout  = "2006-01-02"
	recentCount = 3
	maxQuantity = 100000000
)

// Units are the units that a business can count in. The first one is the default of a new business.
var Units = []string{"potong", "pcs", "kg", "liter", "porsi", "pasang", "lusin", "meter"}

// Record is the production of one day
type Record struct {
	Date     string `json:"date"`
	Quantity int    `json:"quantity"`
	Unit     string `json:"unit"`
}

// Overview is the answer of the list route
type Overview struct {
	Unit   string   `json:"unit"`
	Units  []string `json:"units"`
	Today  string   `json:"today"`
	Recent []Record `json:"recent"`
}

// Store reads and writes the records
type Store struct {
	pool *pgxpool.Pool
	now  func() time.Time
}

// NewStore returns a store on the pool
func NewStore(pool *pgxpool.Pool) *Store {
	return &Store{pool: pool, now: time.Now}
}

// location returns the time zone of the business and its unit
func (s *Store) location(ctx context.Context, businessID string) (*time.Location, string, error) {
	var zone, unit string

	if err := s.pool.QueryRow(ctx, "SELECT time_zone, production_unit FROM businesses WHERE id = $1", businessID).Scan(&zone, &unit); err != nil {
		return nil, "", fmt.Errorf("read the business %w", err)
	}

	loc, err := time.LoadLocation(zone)
	if err != nil {
		return nil, "", fmt.Errorf("the time zone %q of the business is not known %w", zone, err)
	}

	return loc, unit, nil
}

// today returns the calendar day of the business now, as a date at midnight UTC so it compares with a date of the database
func (s *Store) today(loc *time.Location) time.Time {
	y, m, d := s.now().In(loc).Date()

	return time.Date(y, m, d, 0, 0, 0, 0, time.UTC)
}

// Overview returns the unit, the units to choose from, the day of the business now and the newest records
func (s *Store) Overview(ctx context.Context, businessID string) (Overview, error) {
	loc, unit, err := s.location(ctx, businessID)
	if err != nil {
		return Overview{}, err
	}

	rows, err := s.pool.Query(ctx,
		"SELECT to_char(day, 'YYYY-MM-DD'), quantity, unit FROM production_records WHERE business_id = $1 ORDER BY day DESC LIMIT $2",
		businessID, recentCount)
	if err != nil {
		return Overview{}, fmt.Errorf("list the records %w", err)
	}
	defer rows.Close()

	out := Overview{Unit: unit, Units: Units, Today: s.today(loc).Format(dateLayout), Recent: []Record{}}

	for rows.Next() {
		var r Record
		if err := rows.Scan(&r.Date, &r.Quantity, &r.Unit); err != nil {
			return Overview{}, fmt.Errorf("read a record %w", err)
		}

		out.Recent = append(out.Recent, r)
	}

	return out, rows.Err()
}

// Get returns the record of one day, or api.NotFound when the day has none
func (s *Store) Get(ctx context.Context, businessID string, day time.Time) (Record, error) {
	var r Record

	err := s.pool.QueryRow(ctx,
		"SELECT to_char(day, 'YYYY-MM-DD'), quantity, unit FROM production_records WHERE business_id = $1 AND day = $2", businessID, day).
		Scan(&r.Date, &r.Quantity, &r.Unit)
	if errors.Is(err, pgx.ErrNoRows) {
		return Record{}, api.NotFound()
	}

	if err != nil {
		return Record{}, fmt.Errorf("read the record %w", err)
	}

	return r, nil
}

// Save stores the record of a day and replaces the one that is there. The day cannot be after today in the time zone of the
// business, and the unit must be one of Units.
func (s *Store) Save(ctx context.Context, businessID string, day time.Time, quantity int, unit string) (Record, error) {
	loc, unitOfBusiness, err := s.location(ctx, businessID)
	if err != nil {
		return Record{}, err
	}

	fields := map[string]string{}

	if day.After(s.today(loc)) {
		fields["date"] = "in_the_future"
	}

	if quantity < 1 || quantity > maxQuantity {
		fields["quantity"] = "out_of_range"
	}

	if !contains(Units, unit) {
		fields["unit"] = "invalid"
	}

	if len(fields) > 0 {
		return Record{}, api.Validation(fields)
	}

	tx, err := s.pool.BeginTx(ctx, pgx.TxOptions{})
	if err != nil {
		return Record{}, fmt.Errorf("start the save %w", err)
	}

	defer func() { _ = tx.Rollback(ctx) }()

	var r Record

	err = tx.QueryRow(ctx,
		`INSERT INTO production_records (business_id, day, quantity, unit) VALUES ($1, $2, $3, $4)
		 ON CONFLICT (business_id, day) DO UPDATE SET quantity = EXCLUDED.quantity, unit = EXCLUDED.unit, updated_at = now()
		 RETURNING to_char(day, 'YYYY-MM-DD'), quantity, unit`, businessID, day, quantity, unit).Scan(&r.Date, &r.Quantity, &r.Unit)
	if err != nil {
		return Record{}, fmt.Errorf("save the record %w", err)
	}

	// the unit of the last save becomes the unit of the business, so the form offers it first the next time
	if unit != unitOfBusiness {
		if _, err := tx.Exec(ctx, "UPDATE businesses SET production_unit = $2, updated_at = now() WHERE id = $1", businessID, unit); err != nil {
			return Record{}, fmt.Errorf("remember the unit %w", err)
		}
	}

	if err := tx.Commit(ctx); err != nil {
		return Record{}, fmt.Errorf("finish the save %w", err)
	}

	return r, nil
}

func contains(list []string, v string) bool {
	for _, x := range list {
		if x == v {
			return true
		}
	}

	return false
}

// Handlers are the routes of the production records
type Handlers struct {
	store *Store
}

// NewHandlers returns the routes
func NewHandlers(store *Store) *Handlers {
	return &Handlers{store: store}
}

// Register adds the routes to the router behind the middleware that needs a signed in user
func (h *Handlers) Register(r fiber.Router, require fiber.Handler) {
	r.Get("/production", require, h.overview)
	r.Get("/production/:date", require, h.get)
	r.Put("/production/:date", require, h.save)
}

// parseDay reads a date written YYYY-MM-DD and refuses any other form
func parseDay(raw string) (time.Time, error) {
	day, err := time.Parse(dateLayout, raw)
	if err != nil || day.Format(dateLayout) != raw {
		return time.Time{}, api.Validation(map[string]string{"date": "invalid"})
	}

	return day, nil
}

func (h *Handlers) overview(c fiber.Ctx) error {
	out, err := h.store.Overview(c.Context(), auth.UserFrom(c).BusinessID)
	if err != nil {
		return err
	}

	return c.JSON(out)
}

func (h *Handlers) get(c fiber.Ctx) error {
	day, err := parseDay(c.Params("date"))
	if err != nil {
		return api.NotFound()
	}

	r, err := h.store.Get(c.Context(), auth.UserFrom(c).BusinessID, day)
	if err != nil {
		return err
	}

	return c.JSON(r)
}

func (h *Handlers) save(c fiber.Ctx) error {
	day, err := parseDay(c.Params("date"))
	if err != nil {
		return err
	}

	var in struct {
		Quantity int    `json:"quantity"`
		Unit     string `json:"unit"`
	}

	if err := api.Decode(c, &in); err != nil {
		return err
	}

	r, err := h.store.Save(c.Context(), auth.UserFrom(c).BusinessID, day, in.Quantity, in.Unit)
	if err != nil {
		return err
	}

	return c.JSON(r)
}
