// Package janitor runs the clean up work of the service on a timer. Only one copy of the service does a round at a time, because the
// round takes an advisory lock of PostgreSQL, so two copies never delete the same rows twice.
package janitor

import (
	"context"
	"fmt"
	"log/slog"
	"time"

	"github.com/jackc/pgx/v5"
	"github.com/jackc/pgx/v5/pgxpool"
)

const (
	// lockKey is the number of the advisory lock that a round holds
	lockKey = 7261990001
	// batchSize is how many rows one statement deletes, so a purge never holds a lock on a big table for long
	batchSize = 1000
	// maxBatches stops a round that has a lot to delete, and the next round goes on
	maxBatches = 100
)

// Task is one kind of clean up. Its query deletes at most the number of rows in $1 and the task repeats it until nothing is left.
type Task struct {
	Name string
	// Query is a DELETE statement that takes the batch size as its only argument
	Query string
}

// Janitor runs the tasks
type Janitor struct {
	pool     *pgxpool.Pool
	tasks    []Task
	interval time.Duration
}

// New returns a janitor that runs the tasks every interval
func New(pool *pgxpool.Pool, interval time.Duration, tasks ...Task) *Janitor {
	return &Janitor{pool: pool, tasks: tasks, interval: interval}
}

// Run does a round now and then once for each interval, until the context ends
func (j *Janitor) Run(ctx context.Context) {
	j.Round(ctx)

	ticker := time.NewTicker(j.interval)
	defer ticker.Stop()

	for {
		select {
		case <-ctx.Done():
			return
		case <-ticker.C:
			j.Round(ctx)
		}
	}
}

// Round does one round of every task when this copy gets the lock. It returns the number of rows that it deleted.
func (j *Janitor) Round(ctx context.Context) int64 {
	conn, err := j.pool.Acquire(ctx)
	if err != nil {
		slog.Warn("the clean up could not get a connection", "error", err)

		return 0
	}
	defer conn.Release()

	var got bool
	if err := conn.QueryRow(ctx, "SELECT pg_try_advisory_lock($1)", int64(lockKey)).Scan(&got); err != nil || !got {
		return 0
	}

	defer func() {
		// the lock belongs to the session, so it is let go before the connection goes back to the pool
		_, _ = conn.Exec(context.WithoutCancel(ctx), "SELECT pg_advisory_unlock($1)", int64(lockKey))
	}()

	var total int64

	for _, t := range j.tasks {
		n, err := runTask(ctx, conn.Conn(), t)
		total += n

		if err != nil {
			slog.Warn("a clean up task failed", "task", t.Name, "error", err)

			continue
		}

		if n > 0 {
			slog.Info("a clean up task deleted rows", "task", t.Name, "rows", n)
		}
	}

	return total
}

func runTask(ctx context.Context, conn *pgx.Conn, t Task) (int64, error) {
	var total int64

	for range maxBatches {
		tag, err := conn.Exec(ctx, t.Query, batchSize)
		if err != nil {
			return total, fmt.Errorf("run the task %w", err)
		}

		total += tag.RowsAffected()

		if tag.RowsAffected() < batchSize {
			return total, nil
		}
	}

	// the round stopped at its cap, so there is more to delete than one round can do. If this repeats, the rows come in faster than they
	// are removed and the keeping time is not held.
	slog.Warn("a clean up task stopped at the cap of a round", "task", t.Name, "rows", total)

	return total, nil
}
