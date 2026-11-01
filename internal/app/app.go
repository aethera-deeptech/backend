// Package app wires the routes of the service together. The program and the tests both build the application here, so a test
// runs the same routes and the same middleware as production.
package app

import (
	"github.com/gofiber/fiber/v3"
	"github.com/jackc/pgx/v5/pgxpool"

	"github.com/aethera-deeptech/backend/internal/auth"
	"github.com/aethera-deeptech/backend/internal/business"
	"github.com/aethera-deeptech/backend/internal/config"
	"github.com/aethera-deeptech/backend/internal/device"
	"github.com/aethera-deeptech/backend/internal/gateway"
	"github.com/aethera-deeptech/backend/internal/metrics"
	"github.com/aethera-deeptech/backend/internal/production"
	"github.com/aethera-deeptech/backend/internal/server"
)

// passwordHashSlots is how many passwords one copy hashes at the same time. One hash needs about 19 MiB.
const passwordHashSlots = 2

// Deps are what the application needs from the outside
type Deps struct {
	Version string
	Config  config.Config
	Pool    *pgxpool.Pool
	Mailer  auth.Mailer
	// Metrics counts the requests. It is optional, and the tests leave it out.
	Metrics *metrics.Metrics
}

// New returns the application with every route
func New(d Deps) *fiber.App {
	cfg := d.Config

	opts := server.Options{
		Version: d.Version, DB: d.Pool, AppOrigin: cfg.AppOrigin, TrustProxy: cfg.TrustProxy, ProxyHeader: cfg.ProxyHeader, TrustedProxies: cfg.TrustProxyCIDRs,
	}

	if d.Metrics != nil {
		opts.Observe = d.Metrics.Middleware()
	}

	app := server.New(opts)

	limiter := auth.NewLimiter(d.Pool)
	accounts := auth.NewService(auth.NewStore(d.Pool), limiter, auth.NewHasher(passwordHashSlots), d.Mailer, cfg.AppOrigin)
	accountRoutes := auth.NewHandlers(accounts, auth.CookieFor(cfg.AppEnv == config.EnvProduction))
	require := accountRoutes.Require()

	v1 := app.Group("/v1")
	accountRoutes.Register(v1)
	business.NewHandlers(business.NewStore(d.Pool)).Register(v1, require)
	production.NewHandlers(production.NewStore(d.Pool)).Register(v1, require)

	gateways := gateway.NewHandlers(gateway.NewService(d.Pool, limiter))
	gateways.RegisterApp(v1, require)
	gateways.RegisterDevice(app.Group("/device/v1"))
	device.NewHandlers(device.NewPairings(d.Pool, limiter)).Register(v1, require)

	// the labels of the metrics are the routes that exist now, so a path that a client made up is counted as one series
	if d.Metrics != nil {
		d.Metrics.AllowRoutes(app.GetRoutes(true))
	}

	return app
}
