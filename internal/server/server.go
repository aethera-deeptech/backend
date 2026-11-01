// Package server builds the HTTP application that serves the API
package server

import (
	"context"
	"log/slog"
	"sync"
	"time"

	"github.com/gofiber/fiber/v3"
	"github.com/gofiber/fiber/v3/middleware/cors"

	"github.com/aethera-deeptech/backend/internal/api"
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

// Options are what the application needs from the rest of the service
type Options struct {
	// Version is the one that the binary was built with
	Version string
	// DB answers the readiness route
	DB Pinger
	// AppOrigin is the only origin that CORS allows, for example https://aethera.example.com
	AppOrigin string
	// TrustProxy and ProxyHeader tell where the address of the client is when a reverse proxy on the host sits in front
	TrustProxy  bool
	ProxyHeader string
	// Observe measures every request, and it is optional. It sits inside the access log and outside the recovery.
	Observe fiber.Handler
	// TrustedProxies limits the addresses that may set the header. Empty means every private and loopback address.
	TrustedProxies []string
}

// bodyLimit is the largest request body, which is a little above the 256 KiB of a batch of readings, the largest body of any route. The
// server reads a body into memory before a route can check it, so this is the most that a stranger can make it hold.
const bodyLimit = 288 << 10

// New returns the application with its health routes and the middleware of the API. The routes under /v1 and /device/v1 are added
// by the caller, and they run behind the middleware that is set here.
func New(opts Options) *fiber.App {
	cfg := fiber.Config{
		ReadTimeout:  readTimeout,
		WriteTimeout: writeTimeout,
		IdleTimeout:  idleTimeout,
		BodyLimit:    bodyLimit,
		ErrorHandler: api.ErrorHandler,
	}

	if opts.TrustProxy {
		cfg.TrustProxy = true
		cfg.TrustProxyConfig = fiber.TrustProxyConfig{Private: true, Loopback: true}
		if len(opts.TrustedProxies) > 0 {
			cfg.TrustProxyConfig = fiber.TrustProxyConfig{Proxies: opts.TrustedProxies}
		}

		cfg.ProxyHeader = opts.ProxyHeader
		// a header that is not an IP address is ignored, so a client cannot put free text in the address that the log and the limits use
		cfg.EnableIPValidation = true
	}

	app := fiber.New(cfg)
	version, db := opts.Version, opts.DB

	// the log is outside the recovery, so a request that panicked still gets its line with the status 500
	app.Use(api.AccessLog())

	if opts.Observe != nil {
		app.Use(opts.Observe)
	}

	app.Use(api.Recover(), api.SecurityHeaders())

	// the browser app calls from its own origin with its cookie. A request from any other origin gets no CORS headers.
	app.Use("/v1", cors.New(cors.Config{
		AllowOrigins:     []string{opts.AppOrigin},
		AllowMethods:     []string{fiber.MethodGet, fiber.MethodPost, fiber.MethodPut, fiber.MethodPatch, fiber.MethodDelete},
		AllowHeaders:     []string{"Content-Type", "X-Requested-With"},
		AllowCredentials: true,
		MaxAge:           600,
	}), api.NoStore(), api.CSRF(opts.AppOrigin))

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
