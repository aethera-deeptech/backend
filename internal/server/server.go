// Package server builds the HTTP application that serves the API
package server

import (
	"context"
	"log/slog"
	"sync"
	"time"

	"github.com/gofiber/fiber/v3"
)

// The limits on a connection. Without them a client that sends its request one byte at a time, or that keeps an idle connection
// open, holds the connection and its buffers for as long as it likes.
const (
	readTimeout  = 15 * time.Second
	writeTimeout = 15 * time.Second
	idleTimeout  = 60 * time.Second
)

// readyTimeout is how long the readiness route waits for the database
const readyTimeout = 2 * time.Second

// readyCache is how long the answer of the readiness route is reused, so a flood of requests costs one ping and not one connection of
// the pool for each request
const readyCache = 2 * time.Second

// Pinger is the part of the database pool that the readiness route needs
type Pinger interface {
	Ping(ctx context.Context) error
}

// New returns the application with its routes. The version is the one that the binary was built with.
func New(version string, db Pinger) *fiber.App {
	app := fiber.New(fiber.Config{
		ReadTimeout:  readTimeout,
		WriteTimeout: writeTimeout,
		IdleTimeout:  idleTimeout,
	})

	// the health route stays outside /v1 because it belongs to the container and not to the API that the dashboard reads
	app.Get("/healthz", func(c fiber.Ctx) error {
		return c.JSON(fiber.Map{"status": "ok", "version": version})
	})

	// healthz says that the process runs and never looks at the database, because the container is restarted when it fails and a
	// restart does not fix a database that is down. readyz says that the service can do its work, and a monitor reads it.
	var (
		mu       sync.Mutex
		checked  time.Time
		lastOK   bool
		validFor = readyCache
	)

	app.Get("/readyz", func(c fiber.Ctx) error {
		mu.Lock()
		if time.Since(checked) >= validFor {
			ctx, cancel := context.WithTimeout(context.Background(), readyTimeout)
			err := db.Ping(ctx)

			cancel()

			if err != nil {
				slog.Warn("the readiness check failed", "error", err)
			}

			lastOK, checked = err == nil, time.Now()
		}

		ok := lastOK
		mu.Unlock()

		if !ok {
			return c.Status(fiber.StatusServiceUnavailable).JSON(fiber.Map{"status": "unavailable"})
		}

		return c.JSON(fiber.Map{"status": "ready"})
	})

	return app
}
