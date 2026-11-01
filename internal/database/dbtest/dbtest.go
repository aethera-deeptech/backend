// Package dbtest gives a test its own empty PostgreSQL database with every migration applied. A test skips itself when no test
// server is set, so a run on a machine without a database still passes. CI sets the server with the TEST_DB_ variables.
package dbtest

import (
	"context"
	"crypto/rand"
	"encoding/hex"
	"net"
	"net/url"
	"os"
	"testing"
	"time"

	"github.com/jackc/pgx/v5"
	"github.com/jackc/pgx/v5/pgxpool"

	"github.com/aethera-deeptech/backend/internal/database"
)

const setupTimeout = 30 * time.Second

// serverURL builds the URL of one database on the test server, or returns false when no server is set
func serverURL(name string) (string, bool) {
	host := os.Getenv("TEST_DB_HOST")
	if host == "" {
		return "", false
	}

	port := os.Getenv("TEST_DB_PORT")
	if port == "" {
		port = "5432"
	}

	u := url.URL{
		Scheme:   "postgres",
		User:     url.UserPassword(os.Getenv("TEST_DB_USER"), os.Getenv("TEST_DB_PASSWORD")),
		Host:     net.JoinHostPort(host, port),
		Path:     "/" + name,
		RawQuery: "sslmode=disable",
	}

	return u.String(), true
}

// Empty creates a database with no tables for the test and returns its URL. The database is dropped when the test ends. Point the
// TEST_DB_ variables at a throwaway server only, because a run that is killed leaves its database behind.
func Empty(t *testing.T) string {
	t.Helper()

	adminURL, ok := serverURL("postgres")
	if !ok {
		// a CI run without a server would pass with every database test skipped, so it fails instead
		if os.Getenv("CI") != "" {
			t.Fatal("TEST_DB_HOST is not set in CI, so the database tests would not run")
		}

		t.Skip("TEST_DB_HOST is not set, so the test has no database")
	}

	ctx, cancel := context.WithTimeout(context.Background(), setupTimeout)
	defer cancel()

	admin, err := pgx.Connect(ctx, adminURL)
	if err != nil {
		t.Fatalf("connect to the test server %v", err)
	}

	defer func() { _ = admin.Close(ctx) }()

	raw := make([]byte, 8)
	if _, err := rand.Read(raw); err != nil {
		t.Fatalf("make a name %v", err)
	}

	name := "t_" + hex.EncodeToString(raw)
	quoted := pgx.Identifier{name}.Sanitize()

	// the cleanup is registered before the database exists, so a failure further on still drops it
	t.Cleanup(func() {
		dropCtx, dropCancel := context.WithTimeout(context.Background(), setupTimeout)
		defer dropCancel()

		conn, err := pgx.Connect(dropCtx, adminURL)
		if err != nil {
			t.Errorf("connect to drop the test database %v", err)

			return
		}

		defer func() { _ = conn.Close(dropCtx) }()

		if _, err := conn.Exec(dropCtx, "DROP DATABASE IF EXISTS "+quoted+" WITH (FORCE)"); err != nil {
			t.Errorf("drop the test database %v", err)
		}
	})

	if _, err := admin.Exec(ctx, "CREATE DATABASE "+quoted); err != nil {
		t.Fatalf("create the test database %v", err)
	}

	dbURL, _ := serverURL(name)

	return dbURL
}

// New creates a database for the test, migrates it and returns a pool on it
func New(t *testing.T) *pgxpool.Pool {
	t.Helper()

	dbURL := Empty(t)

	if err := database.Migrate(dbURL); err != nil {
		t.Fatalf("migrate the test database %v", err)
	}

	ctx, cancel := context.WithTimeout(context.Background(), setupTimeout)
	defer cancel()

	pool, err := database.Open(ctx, dbURL, 4)
	if err != nil {
		t.Fatalf("open the test database %v", err)
	}

	t.Cleanup(pool.Close)

	return pool
}
