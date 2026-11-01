// Package app wires the routes of the service together. The program and the tests both build the application here, so a test
// runs the same routes and the same middleware as production.
package app

import (
	"github.com/gofiber/fiber/v3"
	"github.com/jackc/pgx/v5/pgxpool"

	"github.com/aethera-deeptech/backend/internal/auth"
	"github.com/aethera-deeptech/backend/internal/config"
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
}

// New returns the application with every route
func New(d Deps) *fiber.App {
	cfg := d.Config

	app := server.New(server.Options{
		Version: d.Version, DB: d.Pool, AppOrigin: cfg.AppOrigin, TrustProxy: cfg.TrustProxy, ProxyHeader: cfg.ProxyHeader, TrustedProxies: cfg.TrustProxyCIDRs,
	})

	accounts := auth.NewService(auth.NewStore(d.Pool), auth.NewLimiter(d.Pool), auth.NewHasher(passwordHashSlots), d.Mailer, cfg.AppOrigin)
	accountRoutes := auth.NewHandlers(accounts, auth.CookieFor(cfg.AppEnv == config.EnvProduction))
	v1 := app.Group("/v1")
	accountRoutes.Register(v1)

	return app
}
