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

// New creates a database for the test, migrates it and returns a pool on it. The database is dropped when the test ends.
func New(t *testing.T) *pgxpool.Pool {
	t.Helper()

	adminURL, ok := serverURL("postgres")
	if !ok {
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

	if _, err := admin.Exec(ctx, "CREATE DATABASE "+name); err != nil {
		t.Fatalf("create the test database %v", err)
	}

	dbURL, _ := serverURL(name)

	if err := database.Migrate(dbURL); err != nil {
		t.Fatalf("migrate the test database %v", err)
	}

	pool, err := database.Open(ctx, dbURL, 4)
	if err != nil {
		t.Fatalf("open the test database %v", err)
	}

	t.Cleanup(func() {
		pool.Close()

		dropCtx, dropCancel := context.WithTimeout(context.Background(), setupTimeout)
		defer dropCancel()

		conn, err := pgx.Connect(dropCtx, adminURL)
		if err != nil {
			t.Errorf("connect to drop the test database %v", err)

			return
		}

		defer func() { _ = conn.Close(dropCtx) }()

		if _, err := conn.Exec(dropCtx, "DROP DATABASE IF EXISTS "+name+" WITH (FORCE)"); err != nil {
			t.Errorf("drop the test database %v", err)
		}
	})

	return pool
}
