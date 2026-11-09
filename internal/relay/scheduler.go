package relay

import (
	"context"
	"fmt"
	"log/slog"
	"time"

	"github.com/jackc/pgx/v5/pgxpool"

	"github.com/aethera-deeptech/backend/internal/errorreport"
)

const (
	// lockKey is the number of the advisory lock that a round of the scheduler holds, so only one copy makes the commands
	lockKey = 7261990002
	// maxLookback is how far back a round looks for a time that passed. It is the life of a command and no more, so a service that was
	// off for an hour does not switch a plug on an order that its own rule would call too old.
	maxLookback = CommandTTL
)

// Scheduler makes the commands of the schedules at the times that the owner set
type Scheduler struct {
	pool  *pgxpool.Pool
	every time.Duration
	now   func() time.Time
}

// NewScheduler returns a scheduler that looks at the schedules every interval
func NewScheduler(pool *pgxpool.Pool, every time.Duration) *Scheduler {
	return &Scheduler{pool: pool, every: every, now: time.Now}
}

// Run does a round now and then once for each interval, until the context ends
func (s *Scheduler) Run(ctx context.Context) {
	s.guardedRound(ctx)

	ticker := time.NewTicker(s.every)
	defer ticker.Stop()

	for {
		select {
		case <-ctx.Done():
			return
		case <-ticker.C:
			s.guardedRound(ctx)
		}
	}
}

// guardedRound does a round that cannot end the process, so a panic skips that round and the next one starts on time
func (s *Scheduler) guardedRound(ctx context.Context) {
	defer errorreport.Contain("relay scheduler")

	s.Round(ctx)
}

type due struct {
	deviceID, gatewayID, nodeID string
	offAt                       time.Time
	onAt                        *time.Time
	days                        string
	checked, updated            time.Time
	zone                        string
}

// Round ends the commands that nobody fetched in time, and makes a command for each schedule that has a time that passed since the
// last round. It returns the number of commands that it made.
func (s *Scheduler) Round(ctx context.Context) int {
	conn, err := s.pool.Acquire(ctx)
	if err != nil {
		slog.Warn("the relay scheduler could not get a connection", "error", err)

		return 0
	}
	defer conn.Release()

	var got bool
	if err := conn.QueryRow(ctx, "SELECT pg_try_advisory_lock($1)", int64(lockKey)).Scan(&got); err != nil || !got {
		return 0
	}

	defer func() {
		// the lock belongs to the session. When it cannot be let go, the connection is closed and not given back to the pool, because the
		// pool would keep it with the lock and no copy would run a round again.
		if _, err := conn.Exec(context.WithoutCancel(ctx), "SELECT pg_advisory_unlock($1)", int64(lockKey)); err != nil {
			_ = conn.Hijack().Close(context.WithoutCancel(ctx))
		}
	}()

	now := s.now()

	if _, err := conn.Exec(ctx, "UPDATE relay_commands SET status = 'expired', done_at = now() WHERE status IN ('pending', 'sent') AND expires_at <= now()"); err != nil {
		slog.Warn("the relay scheduler could not end the old commands", "error", err)
	}

	rows, err := conn.Query(ctx,
		`SELECT s.device_id, d.gateway_id, d.node_id, s.off_at::text, s.on_at::text, s.days, s.checked_at, s.updated_at, b.time_zone
		 FROM relay_schedules s JOIN devices d ON d.id = s.device_id JOIN businesses b ON b.id = d.business_id
		 WHERE s.enabled AND d.kind = 'plug'`)
	if err != nil {
		slog.Warn("the relay scheduler could not read the schedules", "error", err)

		return 0
	}

	var todo []due

	for rows.Next() {
		var (
			d          due
			off        string
			on         *string
			offT, onT  time.Time
			parseError error
		)

		if err := rows.Scan(&d.deviceID, &d.gatewayID, &d.nodeID, &off, &on, &d.days, &d.checked, &d.updated, &d.zone); err != nil {
			rows.Close()
			slog.Warn("the relay scheduler could not read a schedule", "error", err)

			return 0
		}

		if offT, parseError = time.Parse("15:04:05", off); parseError != nil {
			continue
		}

		d.offAt = offT

		if on != nil {
			if onT, parseError = time.Parse("15:04:05", *on); parseError == nil {
				d.onAt = &onT
			}
		}

		todo = append(todo, d)
	}

	rows.Close()

	made := 0

	for _, d := range todo {
		action, at := newest(d, now)
		if action == "" {
			continue
		}

		ok, err := s.claim(ctx, conn, d, action, now)
		if err != nil {
			slog.Warn("the relay scheduler could not make a command", "error", err, "at", at)

			continue
		}

		if ok {
			made++
		}
	}

	return made
}

// claim makes the command of a schedule and moves the time of its last look in one transaction, so a failure loses neither. It does
// nothing when the owner changed or paused the schedule after the round read it.
func (s *Scheduler) claim(ctx context.Context, conn *pgxpool.Conn, d due, action string, now time.Time) (bool, error) {
	tx, err := conn.Begin(ctx)
	if err != nil {
		return false, fmt.Errorf("start the claim %w", err)
	}

	defer func() { _ = tx.Rollback(ctx) }()

	// the gateway row comes first, as in the owner command and in the intake of a batch
	if _, err := tx.Exec(ctx, "SELECT 1 FROM gateways WHERE id = $1 FOR UPDATE", d.gatewayID); err != nil {
		return false, fmt.Errorf("lock the gateway %w", err)
	}

	tag, err := tx.Exec(ctx, "UPDATE relay_schedules SET checked_at = $2 WHERE device_id = $1 AND enabled AND updated_at = $3", d.deviceID, now, d.updated)
	if err != nil {
		return false, fmt.Errorf("move the time of the schedule %w", err)
	}

	if tag.RowsAffected() == 0 {
		return false, nil
	}

	if _, err := enqueue(ctx, tx, d.deviceID, d.gatewayID, d.nodeID, action, "schedule", now.Add(CommandTTL)); err != nil {
		return false, err
	}

	if err := tx.Commit(ctx); err != nil {
		return false, fmt.Errorf("finish the claim %w", err)
	}

	return true, nil
}

// newest finds the last switch time of a schedule in the time since it was checked, and the action of it. It returns an empty action
// when no time passed.
func newest(d due, now time.Time) (string, time.Time) {
	loc, err := time.LoadLocation(d.zone)
	if err != nil {
		loc = time.UTC
	}

	from := d.checked
	if from.Before(now.Add(-maxLookback)) {
		from = now.Add(-maxLookback)
	}

	local := now.In(loc)
	action, latest := "", time.Time{}

	// the day before is included, because a window that crosses midnight holds a time of the day before
	for back := 1; back >= 0; back-- {
		day := time.Date(local.Year(), local.Month(), local.Day()-back, 0, 0, 0, 0, loc)

		if !runsOn(d.days, day.Weekday()) {
			continue
		}

		for _, c := range []struct {
			at     *time.Time
			action string
		}{{&d.offAt, Off}, {d.onAt, On}} {
			if c.at == nil {
				continue
			}

			t := time.Date(day.Year(), day.Month(), day.Day(), c.at.Hour(), c.at.Minute(), 0, 0, loc)

			if t.After(from) && !t.After(now) && t.After(latest) {
				action, latest = c.action, t
			}
		}
	}

	return action, latest
}

func runsOn(days string, w time.Weekday) bool {
	switch days {
	case "mon-fri":
		return w != time.Saturday && w != time.Sunday
	case "mon-sat":
		return w != time.Sunday
	default:
		return true
	}
}
