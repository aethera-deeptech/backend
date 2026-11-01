// Package database opens the PostgreSQL pool of the service and applies the SQL migrations that are embedded in the binary
package database

import (
	"context"
	"embed"
	"errors"
	"fmt"
	"io/fs"
	"log/slog"
	"strconv"
	"strings"
	"time"

	"github.com/golang-migrate/migrate/v4"
	migratepgx "github.com/golang-migrate/migrate/v4/database/pgx/v5"
	"github.com/golang-migrate/migrate/v4/source/iofs"
	"github.com/jackc/pgx/v5"
	"github.com/jackc/pgx/v5/pgxpool"
	"github.com/jackc/pgx/v5/stdlib"
)

//go:embed migrations/*.sql
var migrationFiles embed.FS

// The limits of a connection. The database is shared with other apps, so a statement that runs for too long or a transaction that
// is left open must not hold its resources.
const (
	statementTimeout       = "15s"
	lockTimeout            = "5s"
	idleInTransactionLimit = "30s"
	connectTimeout         = 5 * time.Second
	pingTimeout            = 5 * time.Second
	connectAttempts        = 10
	connectWait            = 2 * time.Second
	// migrationLockTimeout bounds the wait for a table lock in a migration. Without it an ALTER TABLE that waits behind one long
	// query blocks every other query on the table. A statement timeout is left out, because a long migration is normal.
	migrationLockTimeout = "10s"
)

// Open returns a pool and waits until the database answers, so a restart of the database during a deploy does not stop the service
func Open(ctx context.Context, url string, maxConns uint) (*pgxpool.Pool, error) {
	cfg, err := pgxpool.ParseConfig(url)
	if err != nil {
		return nil, errors.New("the database settings are not valid")
	}

	cfg.MaxConns = int32(min(maxConns, 1<<30))
	cfg.ConnConfig.ConnectTimeout = connectTimeout
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
		pingCtx, cancel := context.WithTimeout(ctx, pingTimeout)
		err = pool.Ping(pingCtx)

		cancel()

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
//
// During a rolling update the copy that still runs the old version can restart after the new copy applied a newer migration. The
// library refuses to start from a version that it has no file for, so Migrate skips the step when the database is ahead of the
// binary. That is only safe when every migration works with the code of the version before it, so a migration adds and a later one
// removes (expand and contract). Rolling back to an older version relies on the same rule.
func Migrate(url string) error {
	source, err := iofs.New(migrationFiles, "migrations")
	if err != nil {
		return fmt.Errorf("read the embedded migrations %w", err)
	}

	cfg, err := pgx.ParseConfig(url)
	if err != nil {
		return errors.New("the database settings are not valid")
	}

	cfg.ConnectTimeout = connectTimeout
	cfg.RuntimeParams["application_name"] = "aethera-migrate"
	cfg.RuntimeParams["lock_timeout"] = migrationLockTimeout

	db := stdlib.OpenDB(*cfg)

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

	current, dirty, err := migrator.Version()
	if err != nil && !errors.Is(err, migrate.ErrNilVersion) {
		return fmt.Errorf("read the schema version %w", err)
	}

	if !dirty && !errors.Is(err, migrate.ErrNilVersion) {
		newest, newestErr := newestVersion()
		if newestErr != nil {
			return newestErr
		}

		if current > newest {
			slog.Warn("the database is ahead of this version, so no migration runs", "database", current, "binary", newest)

			return nil
		}
	}

	if err := migrator.Up(); err != nil && !errors.Is(err, migrate.ErrNoChange) {
		return fmt.Errorf("apply the migrations %w", err)
	}

	return nil
}

// newestVersion returns the highest version among the embedded migrations
func newestVersion() (uint, error) {
	names, err := fs.Glob(migrationFiles, "migrations/*.up.sql")
	if err != nil {
		return 0, fmt.Errorf("list the embedded migrations %w", err)
	}

	var newest uint

	for _, name := range names {
		prefix, _, _ := strings.Cut(strings.TrimPrefix(name, "migrations/"), "_")

		version, err := strconv.ParseUint(prefix, 10, 32)
		if err != nil {
			return 0, fmt.Errorf("the migration %s has no number in front of its name", name)
		}

		newest = max(newest, uint(version))
	}

	return newest, nil
}
