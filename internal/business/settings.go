// Package business holds what a business sets up about itself, which are its settings and its rooms
package business

import (
	// the zone names that a business can pick come from the embedded database, so a container without one still works
	_ "time/tzdata"

	"bytes"
	"context"
	"encoding/json"
	"fmt"
	"strings"
	"time"
	"unicode/utf8"

	"github.com/jackc/pgx/v5"
	"github.com/jackc/pgx/v5/pgxpool"

	"github.com/aethera-deeptech/backend/internal/api"
)

// DefaultTariffRpPerKwh is the tariff that a business uses until it sets its own, in rupiah for each kWh
const DefaultTariffRpPerKwh = 1444

const (
	maxTariff      = 100000
	maxNameRunes   = 80
	maxZoneLength  = 64
	timeLayout     = "15:04"
	minutesPerHour = 60
)

// WorkHours are the days and the hours in which the business works. The end is later than the start on the same day.
type WorkHours struct {
	Days  string `json:"days"`
	Start string `json:"start"`
	End   string `json:"end"`
}

// Tariff is the price of the electricity of the business
type Tariff struct {
	RpPerKwh        int  `json:"rpPerKwh"`
	IsDefault       bool `json:"isDefault"`
	DefaultRpPerKwh int  `json:"defaultRpPerKwh"`
}

// Settings are the settings of a business as the app reads them
type Settings struct {
	BusinessName       string    `json:"businessName"`
	WorkHours          WorkHours `json:"workHours"`
	Tariff             Tariff    `json:"tariff"`
	AnomalySensitivity string    `json:"anomalySensitivity"`
	TimeZone           string    `json:"timeZone"`
}

// Optional is a JSON field that can be missing, null or set, which a pointer cannot tell apart
type Optional[T any] struct {
	Set   bool
	Value *T
}

// UnmarshalJSON marks the field as set, and keeps a null as no value
func (o *Optional[T]) UnmarshalJSON(b []byte) error {
	o.Set = true

	if bytes.Equal(bytes.TrimSpace(b), []byte("null")) {
		o.Value = nil

		return nil
	}

	var v T
	if err := json.Unmarshal(b, &v); err != nil {
		return err //nolint:wrapcheck // the decoder reports it as an invalid body
	}

	o.Value = &v

	return nil
}

// Patch is the body of a change of the settings. Only the fields that are present change.
type Patch struct {
	BusinessName       *string       `json:"businessName"`
	WorkHours          *WorkHours    `json:"workHours"`
	TariffRpPerKwh     Optional[int] `json:"tariffRpPerKwh"`
	AnomalySensitivity *string       `json:"anomalySensitivity"`
	TimeZone           *string       `json:"timeZone"`
}

// Store reads and writes the settings and the rooms. Every query names the business, so a business never reads another one.
type Store struct {
	pool *pgxpool.Pool
}

// NewStore returns a store on the pool
func NewStore(pool *pgxpool.Pool) *Store {
	return &Store{pool: pool}
}

const settingsColumns = `COALESCE(name, 'Usaha saya'), work_days, to_char(work_start, 'HH24:MI'), to_char(work_end, 'HH24:MI'),
	tariff_rp_per_kwh, anomaly_sensitivity, time_zone`

const settingsSQL = `SELECT ` + settingsColumns + ` FROM businesses WHERE id = $1`

// Settings returns the settings of the business
func (s *Store) Settings(ctx context.Context, businessID string) (Settings, error) {
	out, err := scanSettings(s.pool.QueryRow(ctx, settingsSQL, businessID))
	if err != nil {
		return Settings{}, fmt.Errorf("read the settings %w", err)
	}

	return out, nil
}

func scanSettings(row pgx.Row) (Settings, error) {
	var (
		out    Settings
		tariff *int
	)

	err := row.Scan(&out.BusinessName, &out.WorkHours.Days, &out.WorkHours.Start, &out.WorkHours.End, &tariff, &out.AnomalySensitivity, &out.TimeZone)
	if err != nil {
		return Settings{}, err //nolint:wrapcheck // the callers add the context
	}

	out.Tariff = Tariff{RpPerKwh: DefaultTariffRpPerKwh, IsDefault: tariff == nil, DefaultRpPerKwh: DefaultTariffRpPerKwh}
	if tariff != nil {
		out.Tariff.RpPerKwh = *tariff
	}

	return out, nil
}

var (
	sensitivities = []string{"rendah", "sedang", "tinggi"}
	workDays      = []string{"mon-fri", "mon-sat", "daily"}
)

func contains(list []string, v string) bool {
	for _, x := range list {
		if x == v {
			return true
		}
	}

	return false
}

func parseClock(v string) (time.Duration, bool) {
	t, err := time.Parse(timeLayout, v)
	if err != nil || t.Format(timeLayout) != v {
		return 0, false
	}

	return time.Duration(t.Hour())*time.Hour + time.Duration(t.Minute())*time.Minute, true
}

// Update applies a patch and returns the new settings. The hours are checked against the ones that are stored when the patch has
// only one of them, so the end is always later than the start.
func (s *Store) Update(ctx context.Context, businessID string, p Patch) (Settings, error) {
	fields := map[string]string{}
	sets := []string{}
	args := []any{businessID}

	add := func(column string, value any) {
		args = append(args, value)
		sets = append(sets, fmt.Sprintf("%s = $%d", column, len(args)))
	}

	if p.BusinessName != nil {
		name := strings.TrimSpace(*p.BusinessName)

		switch {
		case name == "":
			fields["businessName"] = "required"
		case utf8.RuneCountInString(name) > maxNameRunes:
			fields["businessName"] = "too_long"
		case strings.ContainsFunc(name, isHidden):
			fields["businessName"] = "invalid"
		default:
			add("name", name)
		}
	}

	if p.WorkHours != nil {
		start, okStart := parseClock(p.WorkHours.Start)
		end, okEnd := parseClock(p.WorkHours.End)

		switch {
		case !contains(workDays, p.WorkHours.Days):
			fields["workHours.days"] = "invalid"
		case !okStart:
			fields["workHours.start"] = "invalid"
		case !okEnd:
			fields["workHours.end"] = "invalid"
		case end <= start:
			fields["workHours.end"] = "end_before_start"
		default:
			add("work_days", p.WorkHours.Days)
			add("work_start", p.WorkHours.Start)
			add("work_end", p.WorkHours.End)
		}
	}

	if p.TariffRpPerKwh.Set {
		switch v := p.TariffRpPerKwh.Value; {
		case v == nil:
			add("tariff_rp_per_kwh", nil)
		case *v < 1 || *v > maxTariff:
			fields["tariffRpPerKwh"] = "out_of_range"
		default:
			add("tariff_rp_per_kwh", *v)
		}
	}

	if p.AnomalySensitivity != nil {
		if contains(sensitivities, *p.AnomalySensitivity) {
			add("anomaly_sensitivity", *p.AnomalySensitivity)
		} else {
			fields["anomalySensitivity"] = "invalid"
		}
	}

	if p.TimeZone != nil {
		zone := *p.TimeZone

		known, err := s.zoneKnown(ctx, zone)
		if err != nil {
			return Settings{}, err
		}

		if known {
			add("time_zone", zone)
		} else {
			fields["timeZone"] = "invalid"
		}
	}

	if len(fields) == 0 && len(sets) == 0 {
		fields["body"] = "empty"
	}

	if len(fields) > 0 {
		return Settings{}, api.Validation(fields)
	}

	// the work hours always come as a whole (days, start and end), so the pair is checked above and the database check is a last guard
	query := "UPDATE businesses SET " + strings.Join(sets, ", ") + ", updated_at = now() WHERE id = $1 RETURNING " + settingsColumns

	// the answer is what this update wrote, so a second update at the same time cannot show up in it
	out, err := scanSettings(s.pool.QueryRow(ctx, query, args...))
	if err != nil {
		if isCheckViolation(err) {
			return Settings{}, api.Validation(map[string]string{"workHours.end": "end_before_start"})
		}

		return Settings{}, fmt.Errorf("save the settings %w", err)
	}

	return out, nil
}

// zoneKnown reports whether a time zone name is one that the service can use. The name must be a zone of a place, such as
// Asia/Jakarta, and both Go and the database must know it, because Go cuts the days and PostgreSQL groups the readings by the same name.
// Names such as UTC, GMT, Etc/UTC, posix/... and right/... are refused, because they are not a place and the lists of Go and PostgreSQL
// differ for them.
func (s *Store) zoneKnown(ctx context.Context, zone string) (bool, error) {
	if zone == "" || len(zone) > maxZoneLength || !strings.Contains(zone, "/") {
		return false, nil
	}

	for _, prefix := range []string{"Etc/", "posix/", "right/", "SystemV/"} {
		if strings.HasPrefix(zone, prefix) {
			return false, nil
		}
	}

	loc, err := time.LoadLocation(zone)
	if err != nil {
		return false, nil //nolint:nilerr // an unknown name is a validation answer
	}

	// the readings are summed in whole hours of UTC, so a zone whose offset is not a whole hour (Asia/Kolkata is 5 hours 30 minutes) would
	// cut the days and the months in the middle of an hour. Both ends of the year are checked, because the offset can change with the season.
	for _, month := range []time.Month{time.January, time.July} {
		if _, offset := time.Date(time.Now().Year(), month, 1, 12, 0, 0, 0, loc).Zone(); offset%3600 != 0 {
			return false, nil
		}
	}

	var known bool
	if err := s.pool.QueryRow(ctx, "SELECT EXISTS (SELECT 1 FROM pg_timezone_names WHERE name = $1)", zone).Scan(&known); err != nil {
		return false, fmt.Errorf("check the time zone %w", err)
	}

	return known, nil
}

// SensitivityK is how many standard deviations the usual band of a device is wide. A business that wants more alerts has a narrower band.
func SensitivityK(level string) float64 {
	switch level {
	case "tinggi":
		return 2
	case "rendah":
		return 4
	default:
		return 3
	}
}
