package device

import (
	"context"
	"errors"
	"fmt"
	"math"
	"sort"
	"strings"
	"time"

	"github.com/gofiber/fiber/v3"
	"github.com/google/uuid"
	"github.com/jackc/pgx/v5"
	"github.com/jackc/pgx/v5/pgxpool"

	"github.com/aethera-deeptech/backend/internal/api"
	"github.com/aethera-deeptech/backend/internal/auth"
	"github.com/aethera-deeptech/backend/internal/business"
)

const (
	bucketSize    = 15 * time.Minute
	patternPoints = 24 * 4
	// historyDays is how far back the expected band looks, and minDays is how many days an hour of the day needs before it counts
	historyDays = 15
	minDays     = 3
)

// ListItem is one row of the list of devices
type ListItem struct {
	ID            string     `json:"id"`
	Name          string     `json:"name"`
	Status        string     `json:"status"`
	CurrentPowerW *float64   `json:"currentPowerW"`
	TodayKwh      *float64   `json:"todayKwh"`
	AnomalyAt     *time.Time `json:"anomalyAt,omitempty"`
	LastSeenAt    *time.Time `json:"lastSeenAt,omitempty"`
}

// Anomaly is the newest anomaly of a device that still counts
type Anomaly struct {
	At    time.Time `json:"at"`
	PeakW float64   `json:"peakW"`
}

// Band is the usual range of the power at each point of the pattern
type Band struct {
	Low  []float64 `json:"low"`
	High []float64 `json:"high"`
}

// Span is a range of points of the pattern
type Span struct {
	From int `json:"from"`
	To   int `json:"to"`
}

// Pattern is the power of the last 24 hours in 15 minute points
type Pattern struct {
	Points  []float64 `json:"points"`
	YMaxW   float64   `json:"yMaxW"`
	Expect  *Band     `json:"expected,omitempty"`
	Anomaly *Span     `json:"anomaly,omitempty"`
}

// Detail is the screen of one device
type Detail struct {
	ID            string    `json:"id"`
	Name          string    `json:"name"`
	Status        string    `json:"status"`
	Room          string    `json:"room"`
	KindLabel     string    `json:"kindLabel"`
	CurrentPowerW *float64  `json:"currentPowerW"`
	TodayKwh      *float64  `json:"todayKwh"`
	MonthKwh      *float64  `json:"monthKwh"`
	OnHoursToday  *float64  `json:"onHoursToday"`
	StandbyW      *float64  `json:"standbyW"`
	LastSeenAt    time.Time `json:"lastSeenAt"`
	Signal        string    `json:"signal"`
	Anomaly       *Anomaly  `json:"anomaly,omitempty"`
	PowerPattern  *Pattern  `json:"powerPattern"`
}

// Views reads the devices of a business. Every query names the business, so a business never reads another one.
type Views struct {
	pool *pgxpool.Pool
	now  func() time.Time
}

// NewViews returns the views on the pool
func NewViews(pool *pgxpool.Pool) *Views {
	return &Views{pool: pool, now: time.Now}
}

// clock is what the views need to know about the business to cut the days and the months
type clock struct {
	now         time.Time
	dayStart    time.Time
	monthStart  time.Time
	sensitivity string
}

func (v *Views) clock(ctx context.Context, businessID string) (clock, error) {
	var zone string

	c := clock{now: v.now().UTC()}

	err := v.pool.QueryRow(ctx, "SELECT time_zone, anomaly_sensitivity FROM businesses WHERE id = $1", businessID).Scan(&zone, &c.sensitivity)
	if err != nil {
		return clock{}, fmt.Errorf("read the time zone %w", err)
	}

	loc, err := time.LoadLocation(zone)
	if err != nil {
		loc = time.UTC
	}

	local := c.now.In(loc)
	c.dayStart = time.Date(local.Year(), local.Month(), local.Day(), 0, 0, 0, 0, loc).UTC()
	c.monthStart = time.Date(local.Year(), local.Month(), 1, 0, 0, 0, 0, loc).UTC()

	return c, nil
}

const listSQL = `
SELECT d.id, d.name, n.last_sample_at,
  (SELECT r.watts FROM readings r WHERE r.gateway_id = d.gateway_id AND r.node_id = d.node_id ORDER BY r.t DESC LIMIT 1),
  (SELECT percentile_cont(0.95) WITHIN GROUP (ORDER BY h.max_watts) FROM readings_hourly h
     WHERE h.gateway_id = d.gateway_id AND h.node_id = d.node_id AND h.hour >= $2::timestamptz - interval '7 days'),
  (SELECT max(a.at) FROM anomalies a
     WHERE a.gateway_id = d.gateway_id AND a.node_id = d.node_id AND a.at >= $2::timestamptz - interval '24 hours'),
  (SELECT COALESCE(sum(h.energy_kwh), 0) FROM readings_hourly h
     WHERE h.gateway_id = d.gateway_id AND h.node_id = d.node_id AND h.hour >= $3)
FROM devices d JOIN nodes n ON n.gateway_id = d.gateway_id AND n.node_id = d.node_id
WHERE d.business_id = $1`

// List returns the devices, the ones that need a look first and then by name
func (v *Views) List(ctx context.Context, businessID string) ([]ListItem, error) {
	c, err := v.clock(ctx, businessID)
	if err != nil {
		return nil, err
	}

	rows, err := v.pool.Query(ctx, listSQL, businessID, c.now, c.dayStart)
	if err != nil {
		return nil, fmt.Errorf("list the devices %w", err)
	}
	defer rows.Close()

	out := []ListItem{}

	for rows.Next() {
		var (
			it        ListItem
			lastAt    *time.Time
			watts     *float64
			p95       *float64
			anomalyAt *time.Time
			today     float64
		)

		if err := rows.Scan(&it.ID, &it.Name, &lastAt, &watts, &p95, &anomalyAt, &today); err != nil {
			return nil, fmt.Errorf("read a device %w", err)
		}

		it.Status = Classify(c.now, lastAt, watts, p95, anomalyAt)
		watts = roundPtr(watts, round1)
		today = round3(today)

		switch it.Status {
		case StatusOffline:
			it.LastSeenAt = lastAt
		case StatusAnomaly:
			it.AnomalyAt = anomalyAt
			it.CurrentPowerW = watts
			it.TodayKwh = &today
		default:
			it.CurrentPowerW = watts
			it.TodayKwh = &today
		}

		out = append(out, it)
	}

	if err := rows.Err(); err != nil {
		return nil, fmt.Errorf("read the devices %w", err)
	}

	sort.SliceStable(out, func(i, j int) bool {
		a, b := statusOrder[out[i].Status], statusOrder[out[j].Status]
		if a != b {
			return a < b
		}

		if x, y := strings.ToLower(out[i].Name), strings.ToLower(out[j].Name); x != y {
			return x < y
		}

		return out[i].ID < out[j].ID
	})

	return out, nil
}

const detailSQL = `
SELECT d.name, d.kind, rm.name, n.hops, COALESCE(n.last_sample_at, n.last_seen_at), n.last_sample_at, d.gateway_id, d.node_id,
  (SELECT r.watts FROM readings r WHERE r.gateway_id = d.gateway_id AND r.node_id = d.node_id ORDER BY r.t DESC LIMIT 1),
  (SELECT percentile_cont(0.95) WITHIN GROUP (ORDER BY h.max_watts) FROM readings_hourly h
     WHERE h.gateway_id = d.gateway_id AND h.node_id = d.node_id AND h.hour >= $3::timestamptz - interval '7 days'),
  (SELECT COALESCE(sum(h.energy_kwh), 0) FROM readings_hourly h
     WHERE h.gateway_id = d.gateway_id AND h.node_id = d.node_id AND h.hour >= $4),
  (SELECT COALESCE(sum(h.energy_kwh), 0) FROM readings_hourly h
     WHERE h.gateway_id = d.gateway_id AND h.node_id = d.node_id AND h.hour >= $5),
  (SELECT percentile_cont(0.5) WITHIN GROUP (ORDER BY h.min_watts) FROM readings_hourly h
     WHERE h.gateway_id = d.gateway_id AND h.node_id = d.node_id AND h.hour >= $3::timestamptz - interval '7 days'
       AND h.min_watts BETWEEN $6 AND $7)
FROM devices d
JOIN nodes n ON n.gateway_id = d.gateway_id AND n.node_id = d.node_id
JOIN rooms rm ON rm.id = d.room_id
WHERE d.id = $1 AND d.business_id = $2`

// Get returns one device with its pattern of the last 24 hours
func (v *Views) Get(ctx context.Context, businessID, id string) (Detail, error) {
	if _, err := uuid.Parse(id); err != nil {
		return Detail{}, api.NotFound()
	}

	c, err := v.clock(ctx, businessID)
	if err != nil {
		return Detail{}, err
	}

	var (
		out                  = Detail{ID: id}
		kind, gatewayID, nid string
		hops                 int
		sampleAt             *time.Time
		watts, p95, standby  *float64
		today, month         float64
	)

	err = v.pool.QueryRow(ctx, detailSQL, id, businessID, c.now, c.dayStart, c.monthStart, StandbyMinW, StandbyMaxW).Scan(
		&out.Name, &kind, &out.Room, &hops, &out.LastSeenAt, &sampleAt, &gatewayID, &nid, &watts, &p95, &today, &month, &standby)
	if errors.Is(err, pgx.ErrNoRows) {
		return Detail{}, api.NotFound()
	}

	if err != nil {
		return Detail{}, fmt.Errorf("read the device %w", err)
	}

	anomaly, err := v.newestAnomaly(ctx, gatewayID, nid, c.now)
	if err != nil {
		return Detail{}, err
	}

	var anomalyAt *time.Time
	if anomaly != nil {
		anomalyAt = &anomaly.At
	}

	out.Status = Classify(c.now, sampleAt, watts, p95, anomalyAt)
	watts, today, month = roundPtr(watts, round1), round3(today), round3(month)
	standby = roundPtr(standby, round1)
	out.KindLabel = KindLabel(kind)
	out.Signal = Signal(c.now, sampleAt, hops)
	out.MonthKwh = &month
	out.StandbyW = standby

	if out.Status == StatusOffline {
		// the day and the power are not true any more, the month is
		out.TodayKwh = nil
		out.CurrentPowerW = nil
	} else {
		out.TodayKwh = &today
		out.CurrentPowerW = watts
	}

	if out.Status == StatusAnomaly {
		out.Anomaly = anomaly
	}

	level := WorkingLevel(p95)

	onHours, err := v.onHours(ctx, gatewayID, nid, c, level)
	if err != nil {
		return Detail{}, err
	}

	// like the day and the power, the hours of the day are not true for a device that is offline
	if out.Status != StatusOffline {
		out.OnHoursToday = &onHours
	}

	// the chart marks the anomaly only when the detail shows it, so the two never disagree
	out.PowerPattern, err = v.pattern(ctx, gatewayID, nid, c, out.Anomaly)
	if err != nil {
		return Detail{}, err
	}

	return out, nil
}

func (v *Views) newestAnomaly(ctx context.Context, gatewayID, nodeID string, now time.Time) (*Anomaly, error) {
	var a Anomaly

	err := v.pool.QueryRow(ctx,
		"SELECT at, peak_watts FROM anomalies WHERE gateway_id = $1 AND node_id = $2 AND at >= $3 ORDER BY at DESC LIMIT 1",
		gatewayID, nodeID, now.Add(-AnomalyFor)).Scan(&a.At, &a.PeakW)
	a.PeakW = round1(a.PeakW)
	if errors.Is(err, pgx.ErrNoRows) {
		return nil, nil //nolint:nilnil // no anomaly is a normal answer
	}

	if err != nil {
		return nil, fmt.Errorf("read the anomaly %w", err)
	}

	return &a, nil
}

// onHours counts the hours of today in which the device drew at least its working level on average. The hour that runs now counts for
// the minutes that have passed.
func (v *Views) onHours(ctx context.Context, gatewayID, nodeID string, c clock, level float64) (float64, error) {
	rows, err := v.pool.Query(ctx,
		"SELECT hour, sum_watts / samples FROM readings_hourly WHERE gateway_id = $1 AND node_id = $2 AND hour >= $3 ORDER BY hour",
		gatewayID, nodeID, c.dayStart.Truncate(time.Hour))
	if err != nil {
		return 0, fmt.Errorf("read the hours %w", err)
	}
	defer rows.Close()

	total := 0.0

	for rows.Next() {
		var (
			hour time.Time
			avg  float64
		)

		if err := rows.Scan(&hour, &avg); err != nil {
			return 0, fmt.Errorf("read an hour %w", err)
		}

		if avg < level {
			continue
		}

		span := math.Min(time.Hour.Hours(), c.now.Sub(hour).Hours())
		total += math.Max(0, span)
	}

	return round2(total), rows.Err()
}

// pattern builds the 96 points of the last 24 hours, the highest power of each 15 minutes, and the band that the device usually stays in
func (v *Views) pattern(ctx context.Context, gatewayID, nodeID string, c clock, anomaly *Anomaly) (*Pattern, error) {
	since := c.now.Add(-24 * time.Hour)

	rows, err := v.pool.Query(ctx,
		`SELECT floor(extract(epoch FROM (t - $3::timestamptz)) / $5)::int, max(watts) FROM readings
		 WHERE gateway_id = $1 AND node_id = $2 AND t > $3 AND t <= $4 GROUP BY 1`,
		gatewayID, nodeID, since, c.now, bucketSize.Seconds())
	if err != nil {
		return nil, fmt.Errorf("read the pattern %w", err)
	}
	defer rows.Close()

	points := make([]float64, patternPoints)
	seen := 0

	for rows.Next() {
		var (
			b int
			w float64
		)

		if err := rows.Scan(&b, &w); err != nil {
			return nil, fmt.Errorf("read a point %w", err)
		}

		if b >= 0 && b < patternPoints {
			points[b] = round1(w)
			seen++
		}
	}

	if err := rows.Err(); err != nil {
		return nil, fmt.Errorf("read the points %w", err)
	}

	if seen == 0 {
		return nil, nil //nolint:nilnil // no recent data is a null pattern
	}

	out := &Pattern{Points: points}
	top := maxOf(points)

	out.Expect, err = v.expected(ctx, gatewayID, nodeID, c, since)
	if err != nil {
		return nil, err
	}

	if out.Expect != nil {
		top = math.Max(top, maxOf(out.Expect.High))
	}

	out.YMaxW = niceCeiling(top)

	if anomaly != nil && !anomaly.At.Before(since) {
		b := int(anomaly.At.Sub(since) / bucketSize)
		out.Anomaly = &Span{From: max(0, b-1), To: min(patternPoints-1, b+1)}
	}

	return out, nil
}

// expected is the usual range of the power at each point, from the same hour of the day on the days before the window. It needs
// minDays days for every hour of the day, or it is left out.
func (v *Views) expected(ctx context.Context, gatewayID, nodeID string, c clock, since time.Time) (*Band, error) {
	rows, err := v.pool.Query(ctx,
		`SELECT extract(hour FROM hour AT TIME ZONE 'UTC')::int, avg(max_watts), COALESCE(stddev_samp(max_watts), 0), count(*)
		 FROM readings_hourly WHERE gateway_id = $1 AND node_id = $2 AND hour >= $3 AND hour < $4 GROUP BY 1`,
		gatewayID, nodeID, since.Add(-historyDays*24*time.Hour), since)
	if err != nil {
		return nil, fmt.Errorf("read the usual power %w", err)
	}
	defer rows.Close()

	k := business.SensitivityK(c.sensitivity)

	var low, high [24]float64

	have := 0

	for rows.Next() {
		var (
			h         int
			mean, std float64
			n         int
		)

		if err := rows.Scan(&h, &mean, &std, &n); err != nil {
			return nil, fmt.Errorf("read the usual hour %w", err)
		}

		if h < 0 || h > 23 || n < minDays {
			continue
		}

		low[h] = math.Max(0, mean-k*std)
		high[h] = mean + k*std
		have++
	}

	if err := rows.Err(); err != nil {
		return nil, fmt.Errorf("read the usual hours %w", err)
	}

	if have < 24 {
		return nil, nil //nolint:nilnil // too little history for a band
	}

	band := &Band{Low: make([]float64, patternPoints), High: make([]float64, patternPoints)}

	for i := range patternPoints {
		h := since.Add(time.Duration(i) * bucketSize).UTC().Hour()
		band.Low[i] = round1(low[h])
		band.High[i] = round1(high[h])
	}

	return band, nil
}

// Rename changes the name of a device and returns the device
func (v *Views) Rename(ctx context.Context, businessID, id, rawName string) (Detail, error) {
	if _, err := uuid.Parse(id); err != nil {
		return Detail{}, api.NotFound()
	}

	name, code := business.CleanName(rawName, maxDeviceName)
	if code != "" {
		return Detail{}, api.Validation(map[string]string{"name": code})
	}

	tag, err := v.pool.Exec(ctx, "UPDATE devices SET name = $3 WHERE id = $1 AND business_id = $2", id, businessID, name)
	if err != nil {
		return Detail{}, fmt.Errorf("rename the device %w", err)
	}

	if tag.RowsAffected() == 0 {
		return Detail{}, api.NotFound()
	}

	return v.Get(ctx, businessID, id)
}

func maxOf(v []float64) float64 {
	m := 0.0
	for _, x := range v {
		m = math.Max(m, x)
	}

	return m
}

func round1(v float64) float64 { return math.Round(v*10) / 10 }

func round3(v float64) float64 { return math.Round(v*1000) / 1000 }

// roundPtr rounds the value that p points to and returns a new pointer, and keeps a nil
func roundPtr(p *float64, round func(float64) float64) *float64 {
	if p == nil {
		return nil
	}

	v := round(*p)

	return &v
}

func round2(v float64) float64 { return math.Round(v*100) / 100 }

// ViewHandlers are the routes of the list and the screen of a device
type ViewHandlers struct {
	views *Views
}

// NewViewHandlers returns the routes
func NewViewHandlers(views *Views) *ViewHandlers {
	return &ViewHandlers{views: views}
}

// Register adds the routes behind the middleware that needs a signed in user
func (h *ViewHandlers) Register(r fiber.Router, require fiber.Handler) {
	r.Get("/devices", require, h.list)
	r.Get("/devices/:id", require, h.get)
	r.Patch("/devices/:id", require, h.rename)
}

func (h *ViewHandlers) list(c fiber.Ctx) error {
	items, err := h.views.List(c.Context(), auth.UserFrom(c).BusinessID)
	if err != nil {
		return err
	}

	return c.JSON(fiber.Map{"items": items})
}

func (h *ViewHandlers) get(c fiber.Ctx) error {
	out, err := h.views.Get(c.Context(), auth.UserFrom(c).BusinessID, c.Params("id"))
	if err != nil {
		return err
	}

	return c.JSON(out)
}

func (h *ViewHandlers) rename(c fiber.Ctx) error {
	var in struct {
		Name string `json:"name"`
	}

	if err := api.Decode(c, &in); err != nil {
		return err
	}

	out, err := h.views.Rename(c.Context(), auth.UserFrom(c).BusinessID, c.Params("id"), in.Name)
	if err != nil {
		return err
	}

	return c.JSON(out)
}
