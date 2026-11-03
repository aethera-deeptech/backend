// Package relay switches the relay of a plug on and off. The owner sends a command in the app or sets a schedule, and the gateway of the
// plug fetches the command in the answer to its next batch of readings and reports the result in a later batch. The service never calls
// a gateway, because a gateway sits behind a router and only calls out.
package relay

import (
	"context"
	"errors"
	"fmt"
	"net/http"
	"regexp"
	"slices"
	"time"

	"github.com/google/uuid"
	"github.com/jackc/pgx/v5"
	"github.com/jackc/pgx/v5/pgconn"
	"github.com/jackc/pgx/v5/pgxpool"

	"github.com/aethera-deeptech/backend/internal/api"
	"github.com/aethera-deeptech/backend/internal/auth"
	"github.com/aethera-deeptech/backend/internal/device"
)

const (
	// CommandTTL is how long a command waits for its gateway. A gateway that was off for longer does not switch a plug on an old order.
	CommandTTL = 10 * time.Minute
	// MaxResults is the number of results that one batch may carry, which is a little more than the number of nodes of a gateway
	MaxResults = 64
)

// The actions of a command and the states of a relay
const (
	On      = "on"
	Off     = "off"
	Unknown = "unknown"
)

var (
	ruleCommand = auth.Rule{Bucket: "relay-business", Max: 60, Window: time.Hour}
	clockText   = regexp.MustCompile(`^([01][0-9]|2[0-3]):[0-5][0-9]$`)
	workDays    = []string{"mon-fri", "mon-sat", "daily"}
)

// Command is what a gateway fetches. It switches the relay of one node, and the gateway reports the result with the same id.
type Command struct {
	ID        string    `json:"id"`
	NodeID    string    `json:"nodeId"`
	Action    string    `json:"action"`
	ExpiresAt time.Time `json:"expiresAt"`
}

// Result is the answer of a gateway to a command
type Result struct {
	ID string `json:"id"`
	OK bool   `json:"ok"`
}

// Pending is a command that has not ended
type Pending struct {
	ID        string    `json:"id"`
	Action    string    `json:"action"`
	Status    string    `json:"status"`
	ExpiresAt time.Time `json:"expiresAt"`
}

// Last is the newest command that ended
type Last struct {
	Action string    `json:"action"`
	Status string    `json:"status"`
	Source string    `json:"source"`
	At     time.Time `json:"at"`
}

// Schedule is the daily switch of a plug
type Schedule struct {
	Enabled bool    `json:"enabled"`
	OffAt   string  `json:"offAt"`
	OnAt    *string `json:"onAt"`
	Days    string  `json:"days"`
}

// View is what the app reads about the relay of a device
type View struct {
	State      string     `json:"state"`
	ReportedAt *time.Time `json:"reportedAt"`
	// GatewayOnline says whether the gateway of the plug sent a batch in the last device.OfflineAfter. A command for a gateway that is not
	// online is still queued, and it expires after CommandTTL if the gateway stays away.
	GatewayOnline bool      `json:"gatewayOnline"`
	Pending       *Pending  `json:"pending"`
	Last          *Last     `json:"last"`
	Schedule      *Schedule `json:"schedule"`
}

// Service holds the rules of the relay
type Service struct {
	pool    *pgxpool.Pool
	limiter *auth.Limiter
}

// NewService returns the service
func NewService(pool *pgxpool.Pool, limiter *auth.Limiter) *Service {
	return &Service{pool: pool, limiter: limiter}
}

func noRelay() *api.Error {
	return api.NewError(http.StatusConflict, "no_relay", "Only a plug has a relay")
}

// plug finds a device of the business and says that it is a plug. A device of another business is not found.
func (s *Service) plug(ctx context.Context, q querier, businessID, deviceID string) (id, gatewayID, nodeID string, err error) {
	// the parsed value goes to the query, because uuid.Parse also accepts the urn:uuid: form that PostgreSQL does not
	parsed, err := uuid.Parse(deviceID)
	if err != nil {
		return "", "", "", api.NotFound()
	}

	id = parsed.String()

	var kind string

	err = q.QueryRow(ctx, "SELECT gateway_id, node_id, kind FROM devices WHERE id = $1 AND business_id = $2", id, businessID).Scan(&gatewayID, &nodeID, &kind)
	if errors.Is(err, pgx.ErrNoRows) {
		return "", "", "", api.NotFound()
	}

	if err != nil {
		return "", "", "", fmt.Errorf("read the device %w", err)
	}

	if kind != "plug" {
		return "", "", "", noRelay()
	}

	return id, gatewayID, nodeID, nil
}

// querier is what both the pool and a transaction can do
type querier interface {
	Query(ctx context.Context, sql string, args ...any) (pgx.Rows, error)
	QueryRow(ctx context.Context, sql string, args ...any) pgx.Row
	Exec(ctx context.Context, sql string, arguments ...any) (pgconn.CommandTag, error)
}

// Send queues a command of the owner. A command that waits for the same device is replaced, because the newest order is the one that
// counts.
func (s *Service) Send(ctx context.Context, businessID, deviceID, state string) (Pending, error) {
	if state != On && state != Off {
		return Pending{}, api.Validation(map[string]string{"state": "invalid"})
	}

	// the device is checked before the limit, so a bad id or a clamp does not use up the commands of the business
	id, gatewayID, nodeID, err := s.plug(ctx, s.pool, businessID, deviceID)
	if err != nil {
		return Pending{}, err
	}

	ok, wait, err := s.limiter.Allow(ctx, ruleCommand, businessID)
	if err != nil {
		return Pending{}, fmt.Errorf("check the limit %w", err)
	}

	if !ok {
		return Pending{}, api.RateLimited("", int(wait.Seconds()+0.5))
	}

	tx, err := s.pool.BeginTx(ctx, pgx.TxOptions{})
	if err != nil {
		return Pending{}, fmt.Errorf("start the command %w", err)
	}

	defer func() { _ = tx.Rollback(ctx) }()

	out, err := enqueue(ctx, tx, id, gatewayID, nodeID, state, "owner", time.Now().Add(CommandTTL))
	if err != nil {
		return Pending{}, err
	}

	if err := tx.Commit(ctx); err != nil {
		return Pending{}, fmt.Errorf("finish the command %w", err)
	}

	return out, nil
}

// enqueue ends the commands that still wait for the device and adds a new one. It takes the row of the gateway first, which is the
// lock that the intake of a batch takes first too, so the two always lock in the same order and a batch never fetches a command that
// this call is replacing.
func enqueue(ctx context.Context, q querier, deviceID, gatewayID, nodeID, action, source string, expires time.Time) (Pending, error) {
	if _, err := q.Exec(ctx, "SELECT 1 FROM gateways WHERE id = $1 FOR UPDATE", gatewayID); err != nil {
		return Pending{}, fmt.Errorf("lock the gateway %w", err)
	}

	if _, err := q.Exec(ctx,
		"UPDATE relay_commands SET status = 'expired', done_at = now() WHERE device_id = $1 AND status IN ('pending', 'sent')", deviceID); err != nil {
		return Pending{}, fmt.Errorf("end the older commands %w", err)
	}

	out := Pending{Action: action, Status: "pending", ExpiresAt: expires}

	if err := q.QueryRow(ctx,
		"INSERT INTO relay_commands (device_id, gateway_id, node_id, action, source, expires_at) VALUES ($1, $2, $3, $4, $5, $6) RETURNING id",
		deviceID, gatewayID, nodeID, action, source, expires).Scan(&out.ID); err != nil {
		return Pending{}, fmt.Errorf("store the command %w", err)
	}

	return out, nil
}

// Get reads the relay of a plug, the schedule of it included
func (s *Service) Get(ctx context.Context, businessID, deviceID string) (View, error) {
	deviceID, gatewayID, nodeID, err := s.plug(ctx, s.pool, businessID, deviceID)
	if err != nil {
		return View{}, err
	}

	out := View{State: Unknown}

	var seen *time.Time

	if err := s.pool.QueryRow(ctx, "SELECT last_seen_at FROM gateways WHERE id = $1", gatewayID).Scan(&seen); err != nil {
		return View{}, fmt.Errorf("read the gateway %w", err)
	}

	out.GatewayOnline = seen != nil && time.Since(*seen) <= device.OfflineAfter

	var on *bool

	if err := s.pool.QueryRow(ctx, "SELECT relay_on, relay_at FROM nodes WHERE gateway_id = $1 AND node_id = $2", gatewayID, nodeID).Scan(&on, &out.ReportedAt); err != nil {
		return View{}, fmt.Errorf("read the node %w", err)
	}

	if on != nil {
		out.State = Off
		if *on {
			out.State = On
		}
	}

	var p Pending

	err = s.pool.QueryRow(ctx,
		"SELECT id, action, status, expires_at FROM relay_commands WHERE device_id = $1 AND status IN ('pending', 'sent') AND expires_at > now() ORDER BY created_at DESC LIMIT 1",
		deviceID).Scan(&p.ID, &p.Action, &p.Status, &p.ExpiresAt)

	switch {
	case err == nil:
		out.Pending = &p
	case !errors.Is(err, pgx.ErrNoRows):
		return View{}, fmt.Errorf("read the pending command %w", err)
	}

	var l Last

	err = s.pool.QueryRow(ctx,
		"SELECT action, status, source, COALESCE(done_at, created_at) FROM relay_commands WHERE device_id = $1 AND status IN ('applied', 'failed', 'expired') ORDER BY created_at DESC LIMIT 1",
		deviceID).Scan(&l.Action, &l.Status, &l.Source, &l.At)

	switch {
	case err == nil:
		out.Last = &l
	case !errors.Is(err, pgx.ErrNoRows):
		return View{}, fmt.Errorf("read the last command %w", err)
	}

	sched, err := s.schedule(ctx, deviceID)
	if err != nil {
		return View{}, err
	}

	out.Schedule = sched

	return out, nil
}

func (s *Service) schedule(ctx context.Context, deviceID string) (*Schedule, error) {
	var (
		sc    Schedule
		onAt  *string
		offAt string
	)

	err := s.pool.QueryRow(ctx,
		"SELECT enabled, to_char(off_at, 'HH24:MI'), to_char(on_at, 'HH24:MI'), days FROM relay_schedules WHERE device_id = $1", deviceID).
		Scan(&sc.Enabled, &offAt, &onAt, &sc.Days)
	if errors.Is(err, pgx.ErrNoRows) {
		return nil, nil //nolint:nilnil // a device with no schedule is a normal answer
	}

	if err != nil {
		return nil, fmt.Errorf("read the schedule %w", err)
	}

	sc.OffAt, sc.OnAt = offAt, onAt

	return &sc, nil
}

// SchedulePatch is what the owner sends to set a schedule. The times are local times of the business.
type SchedulePatch struct {
	Enabled *bool   `json:"enabled"`
	OffAt   string  `json:"offAt"`
	OnAt    *string `json:"onAt"`
	Days    string  `json:"days"`
}

// SetSchedule writes the schedule of a plug. The service starts to look for a time that passed from now on, so a schedule never
// switches the plug for a time before it was set.
func (s *Service) SetSchedule(ctx context.Context, businessID, deviceID string, p SchedulePatch) (Schedule, error) {
	deviceID, _, _, err := s.plug(ctx, s.pool, businessID, deviceID)
	if err != nil {
		return Schedule{}, err
	}

	fields := map[string]string{}

	if !clockText.MatchString(p.OffAt) {
		fields["offAt"] = "invalid"
	}

	if p.OnAt != nil && (!clockText.MatchString(*p.OnAt) || *p.OnAt == p.OffAt) {
		fields["onAt"] = "invalid"
	}

	if !slices.Contains(workDays, p.Days) {
		fields["days"] = "invalid"
	}

	if len(fields) > 0 {
		return Schedule{}, api.Validation(fields)
	}

	enabled := true
	if p.Enabled != nil {
		enabled = *p.Enabled
	}

	if _, err := s.pool.Exec(ctx,
		`INSERT INTO relay_schedules (device_id, off_at, on_at, days, enabled) VALUES ($1, $2::time, $3::time, $4, $5)
		 ON CONFLICT (device_id) DO UPDATE SET off_at = EXCLUDED.off_at, on_at = EXCLUDED.on_at, days = EXCLUDED.days,
		   enabled = EXCLUDED.enabled, checked_at = now(), updated_at = now()`,
		deviceID, p.OffAt, p.OnAt, p.Days, enabled); err != nil {
		return Schedule{}, fmt.Errorf("store the schedule %w", err)
	}

	return Schedule{Enabled: enabled, OffAt: p.OffAt, OnAt: p.OnAt, Days: p.Days}, nil
}

// DeleteSchedule removes the schedule of a plug, and a plug with none is not an error
func (s *Service) DeleteSchedule(ctx context.Context, businessID, deviceID string) error {
	deviceID, _, _, err := s.plug(ctx, s.pool, businessID, deviceID)
	if err != nil {
		return err
	}

	if _, err := s.pool.Exec(ctx, "DELETE FROM relay_schedules WHERE device_id = $1", deviceID); err != nil {
		return fmt.Errorf("delete the schedule %w", err)
	}

	return nil
}
