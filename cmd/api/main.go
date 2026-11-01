// Command backend starts the REST API of the energy audit dashboard
package main

import (
	"context"
	"fmt"
	"log"
	"log/slog"
	"os"
	"os/signal"
	"syscall"
	"time"
	// the time zone of a business is any IANA zone, and a container image can come without a zone database
	_ "time/tzdata"

	"github.com/gofiber/fiber/v3"

	"github.com/aethera-deeptech/backend/internal/auth"
	"github.com/aethera-deeptech/backend/internal/config"
	"github.com/aethera-deeptech/backend/internal/database"
	"github.com/aethera-deeptech/backend/internal/healthcheck"
	"github.com/aethera-deeptech/backend/internal/logging"
	"github.com/aethera-deeptech/backend/internal/mail"
	"github.com/aethera-deeptech/backend/internal/server"
)

// version is set at build time with -ldflags
var version = "dev"

// shutdownTimeout is how long the server lets requests that are running finish after a stop signal
const shutdownTimeout = 15 * time.Second

// passwordHashSlots is how many passwords one copy hashes at the same time. One hash needs about 19 MiB.
const passwordHashSlots = 2

// mailWorkers and mailBacklog size the queue of outgoing emails
const (
	mailWorkers = 2
	mailBacklog = 100
)

func main() {
	// the container health check runs this binary again, because the image has no curl
	if len(os.Args) > 1 && os.Args[1] == "healthcheck" {
		os.Exit(healthcheck.Run(os.Getenv("APP_PORT")))
	}

	// before the first line, so everything the process prints is JSON
	logging.Setup()

	// slog and not log.Fatalf, because log.Fatalf writes at the info level and an alert on the error level would miss it
	if err := run(); err != nil {
		slog.Error("stopped with an error", "error", err)
		os.Exit(1)
	}
}

func run() error {
	cfg, err := config.Load()
	if err != nil {
		return fmt.Errorf("load the settings %w", err)
	}

	log.Printf("starting backend %s in %s", version, cfg.AppEnv)

	ctx, stop := signal.NotifyContext(context.Background(), syscall.SIGINT, syscall.SIGTERM)
	defer stop()

	// the pool opens first because it waits for a database that is still starting. The migrations run before the service listens, so a
	// copy that cannot migrate never reports healthy and the rolling update stops.
	pool, err := database.Open(ctx, cfg.DatabaseURL(), cfg.DBMaxConns)
	if err != nil {
		return fmt.Errorf("open the database %w", err)
	}
	defer pool.Close()

	if err := database.Migrate(cfg.DatabaseURL()); err != nil {
		return fmt.Errorf("migrate the database %w", err)
	}

	app := server.New(server.Options{
		Version: version, DB: pool, AppOrigin: cfg.AppOrigin, TrustProxy: cfg.TrustProxy, ProxyHeader: cfg.ProxyHeader,
	})

	mailQueue, mailer := newMailer(cfg)
	defer func() {
		ctx, cancel := context.WithTimeout(context.Background(), shutdownTimeout)
		defer cancel()

		mailQueue.Close(ctx)
	}()

	accounts := auth.NewService(auth.NewStore(pool), auth.NewLimiter(pool), auth.NewHasher(passwordHashSlots), mailer, cfg.AppOrigin)
	auth.NewHandlers(accounts, auth.CookieFor(cfg.AppEnv == config.EnvProduction)).Register(app.Group("/v1"))

	serverErr := make(chan error, 1)

	go func() {
		serverErr <- app.Listen(fmt.Sprintf(":%d", cfg.AppPort), fiber.ListenConfig{DisableStartupMessage: true})
	}()

	select {
	case err := <-serverErr:
		return fmt.Errorf("server stopped %w", err)
	case <-ctx.Done():
		log.Println("gracefully shutting down")
	}

	// from here a second signal gets its default action, so an operator can stop the process without waiting for the timeout
	stop()

	if err := app.ShutdownWithTimeout(shutdownTimeout); err != nil {
		return fmt.Errorf("graceful shutdown failed %w", err)
	}

	log.Println("shutdown complete")

	return nil
}

// newMailer returns the mailer of the account routes. Without a mail server (a development setup) it logs the links instead.
func newMailer(cfg config.Config) (*mail.Queue, auth.Mailer) {
	if cfg.SMTPHost == "" {
		slog.Warn("SMTP_HOST is not set, so the emails are written to the log")

		return mail.NewQueue(nil, 0, 1), mail.Log{}
	}

	queue := mail.NewQueue(&mail.SMTP{
		Host: cfg.SMTPHost, Port: cfg.SMTPPort, Username: cfg.SMTPUsername, Password: cfg.SMTPPassword,
		Security: cfg.SMTPSecurity, FromName: cfg.SMTPFromName, FromAddress: cfg.SMTPFromAddress,
	}, mailWorkers, mailBacklog)

	return queue, mail.NewOutbox(queue)
}
