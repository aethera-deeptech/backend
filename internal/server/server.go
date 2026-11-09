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

// started is when the process started, which the health route turns into an uptime
var started = time.Now()

// The states of the database check, ok when it answers, busy when the ping timed out while every connection of the pool was in use,
// and down otherwise
const (
	dbOK   = "ok"
	dbBusy = "busy"
	dbDown = "down"
)

// readyTimeout is how long the readiness route waits for the database
const readyTimeout = 2 * time.Second

// requestDeadline is how long one request of the API may run before its context ends and the answer is 503 busy
const requestDeadline = 10 * time.Second

// readyCache is how long the answer of the readiness route is reused, so a flood of requests costs one ping and not one connection of
// the pool for each request
const readyCache = 2 * time.Second

// Pinger is the part of the database pool that the readiness route needs
type Pinger interface {
	Ping(ctx context.Context) error
}

// PoolStats is how many connections of the pool are in use, which the health route shows
type PoolStats struct {
	Acquired, Idle, Max int32
}

// Options are what the application needs from the rest of the service
type Options struct {
	// Version is the one that the binary was built with
	Version string
	// DB answers the health and readiness routes
	DB Pinger
	// Pool and Mail are optional and only shown in the health route. Mail is smtp when a mail server is set and log when the links
	// go to the log, which is the setup of development.
	Pool func() PoolStats
	Mail string
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

	// a query that waits for a free connection of a busy pool or for a stalled database ends with the request, so it cannot hold a
	// worker for ever. The health routes keep their own, shorter timeout.
	app.Use([]string{"/v1", "/device/v1"}, api.Deadline(requestDeadline))

	// the browser app calls from its own origin with its cookie. A request from any other origin gets no CORS headers.
	app.Use("/v1", cors.New(cors.Config{
		AllowOrigins:     []string{opts.AppOrigin},
		AllowMethods:     []string{fiber.MethodGet, fiber.MethodPost, fiber.MethodPut, fiber.MethodPatch, fiber.MethodDelete},
		AllowHeaders:     []string{"Content-Type", "X-Requested-With"},
		AllowCredentials: true,
		MaxAge:           600,
	}), api.NoStore(), api.CSRF(opts.AppOrigin))

	// the answer of the database check is reused for a moment, so a flood of requests costs one ping and not one connection of the
	// pool for each request. A ping takes a connection of the pool, so when every connection is in use and the ping times out, the
	// database is busy and not down. That answers 200, because a probe that took both copies out of service under load would turn a
	// slowdown into an outage.
	var (
		mu       sync.Mutex
		checked  time.Time
		lastSeen = dbDown
		validFor = readyCache
	)

	databaseState := func() string {
		mu.Lock()
		defer mu.Unlock()

		if time.Since(checked) >= validFor {
			ctx, cancel := context.WithTimeout(context.Background(), readyTimeout)
			err := db.Ping(ctx)

			cancel()

			saturated := false
			if opts.Pool != nil {
				p := opts.Pool()
				saturated = p.Max > 0 && p.Acquired >= p.Max
			}

			switch {
			case err == nil:
				lastSeen = dbOK
			case saturated:
				slog.Warn("the database check timed out while every connection of the pool is in use", "error", err)

				lastSeen = dbBusy
			default:
				slog.Warn("the database check failed", "error", err)

				lastSeen = dbDown
			}

			checked = time.Now()
		}

		return lastSeen
	}

	// the health route stays outside /v1 because it belongs to the container and not to the API that the dashboard reads. It answers
	// 503 while the database does not answer. The container health check only marks the copy unhealthy for that and nothing restarts
	// it, and the rolling update waits for it, so a deploy during a database outage stops at the first copy and says why.
	app.Get("/healthz", func(c fiber.Ctx) error {
		database := databaseState()
		status, state := fiber.StatusOK, "ok"

		if database == dbDown {
			status, state = fiber.StatusServiceUnavailable, "down"
		}

		checks := fiber.Map{"database": database}

		if opts.Mail != "" {
			checks["mail"] = opts.Mail
		}

		body := fiber.Map{
			"status":        state,
			"version":       version,
			"uptimeSeconds": int64(time.Since(started).Seconds()),
			"checks":        checks,
		}

		if opts.Pool != nil {
			p := opts.Pool()
			body["pool"] = fiber.Map{"acquired": p.Acquired, "idle": p.Idle, "max": p.Max}
		}

		return c.Status(status).JSON(body)
	})

	// readyz is the short form that a monitor reads, 200 while the database answers and 503 when it does not
	app.Get("/readyz", func(c fiber.Ctx) error {
		if databaseState() == dbDown {
			return c.Status(fiber.StatusServiceUnavailable).JSON(fiber.Map{"status": "unavailable"})
		}

		return c.JSON(fiber.Map{"status": "ready"})
	})

	return app
}
