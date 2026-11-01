// Package database opens the PostgreSQL pool of the service and applies the SQL migrations that are embedded in the binary
package database

import (
	"context"
	"database/sql"
	"embed"
	"errors"
	"fmt"
	"log/slog"
	"time"

	"github.com/golang-migrate/migrate/v4"
	migratepgx "github.com/golang-migrate/migrate/v4/database/pgx/v5"
	"github.com/golang-migrate/migrate/v4/source/iofs"
	"github.com/jackc/pgx/v5/pgxpool"
	// registers the pgx driver for database/sql, which the migration library uses
	_ "github.com/jackc/pgx/v5/stdlib"
)

//go:embed migrations/*.sql
var migrationFiles embed.FS

// The limits of a connection. The database is shared with other apps, so a statement that runs for too long or a transaction that
// is left open must not hold its resources.
const (
	statementTimeout       = "15s"
	lockTimeout            = "5s"
	idleInTransactionLimit = "30s"
	connectAttempts        = 10
	connectWait            = 2 * time.Second
)

// Open returns a pool and waits until the database answers, so a restart of the database during a deploy does not stop the service
func Open(ctx context.Context, url string, maxConns uint) (*pgxpool.Pool, error) {
	cfg, err := pgxpool.ParseConfig(url)
	if err != nil {
		return nil, errors.New("the database settings are not valid")
	}

	cfg.MaxConns = int32(min(maxConns, 1<<30)) //nolint:gosec // the config loader limits it to 50
	cfg.MaxConnLifetime = 30 * time.Minute
	cfg.MaxConnIdleTime = 5 * time.Minute
	cfg.HealthCheckPeriod = time.Minute
	cfg.ConnConfig.RuntimeParams["application_name"] = "aethera-api"
	cfg.ConnConfig.RuntimeParams["timezone"] = "UTC"
	cfg.ConnConfig.RuntimeParams["statement_timeout"] = statementTimeout
	cfg.ConnConfig.RuntimeParams["lock_timeout"] = lockTimeout
	cfg.ConnConfig.RuntimeParams["idle_in_transaction_session_timeout"] = idleInTransactionLimit

	pool, err := pgxpool.NewWithConfig(ctx, cfg)
	if err != nil {
		return nil, fmt.Errorf("create the pool %w", err)
	}

	for attempt := 1; ; attempt++ {
		err = pool.Ping(ctx)
		if err == nil {
			return pool, nil
		}

		if attempt == connectAttempts || ctx.Err() != nil {
			pool.Close()

			return nil, fmt.Errorf("the database does not answer %w", err)
		}

		slog.Warn("the database does not answer yet", "attempt", attempt, "error", err)

		select {
		case <-ctx.Done():
		case <-time.After(connectWait):
		}
	}
}

// Migrate applies every migration that is not applied yet. It is safe to call from two copies at the same time, because the
// library holds an advisory lock of PostgreSQL while it runs, so the second copy waits and then finds nothing to do.
func Migrate(url string) error {
	source, err := iofs.New(migrationFiles, "migrations")
	if err != nil {
		return fmt.Errorf("read the embedded migrations %w", err)
	}

	db, err := sql.Open("pgx", url)
	if err != nil {
		return errors.New("the database settings are not valid")
	}

	driver, err := migratepgx.WithInstance(db, &migratepgx.Config{})
	if err != nil {
		_ = db.Close()

		return fmt.Errorf("open the migration driver %w", err)
	}

	migrator, err := migrate.NewWithInstance("iofs", source, "pgx5", driver)
	if err != nil {
		_ = db.Close()

		return fmt.Errorf("create the migrator %w", err)
	}

	defer func() { _, _ = migrator.Close() }()

	if err := migrator.Up(); err != nil && !errors.Is(err, migrate.ErrNoChange) {
		return fmt.Errorf("apply the migrations %w", err)
	}

	return nil
}
