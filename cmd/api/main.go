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

	"github.com/aethera-deeptech/backend/internal/app"
	"github.com/aethera-deeptech/backend/internal/auth"
	"github.com/aethera-deeptech/backend/internal/config"
	"github.com/aethera-deeptech/backend/internal/database"
	"github.com/aethera-deeptech/backend/internal/errorreport"
	"github.com/aethera-deeptech/backend/internal/gateway"
	"github.com/aethera-deeptech/backend/internal/healthcheck"
	"github.com/aethera-deeptech/backend/internal/janitor"
	"github.com/aethera-deeptech/backend/internal/logging"
	"github.com/aethera-deeptech/backend/internal/mail"
	"github.com/aethera-deeptech/backend/internal/metrics"
)

// version is set at build time with -ldflags
var version = "dev"

// shutdownTimeout is how long the server lets requests that are running finish after a stop signal
const shutdownTimeout = 15 * time.Second

// janitorEvery is how often a copy tries a round of the clean up. Only one copy gets the lock for a round.
const janitorEvery = time.Hour

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

func run() (err error) {
	cfg, err := config.Load()
	if err != nil {
		return fmt.Errorf("load the settings %w", err)
	}

	log.Printf("starting backend %s in %s", version, cfg.AppEnv)

	// the reports start first, so a failure while the service starts is sent too. The returned function sends what is still queued.
	stopReports := errorreport.Start(errorreport.Config{
		DSN: cfg.SentryDSN, Environment: cfg.SentryEnvironment, ServerName: cfg.SentryServerName, Release: version,
		EventsPerMinute: cfg.SentryEventsPerMinute,
	})
	defer stopReports()

	// a failure that stops the service, such as a migration that fails or a database that does not answer, is sent before the reports
	// stop, because a crash loop after a bad deploy is the case in which the report matters most
	defer func() {
		if err != nil {
			errorreport.Report(err, errorreport.Info{})
		}
	}()

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

	// the clean up stops with the signal, and a round that is running ends with its context
	go janitor.New(pool, janitorEvery, append(auth.PurgeTasks(), gateway.PurgeTasks()...)...).Run(ctx)

	mailQueue, mailer := newMailer(cfg)
	defer func() {
		ctx, cancel := context.WithTimeout(context.Background(), shutdownTimeout)
		defer cancel()

		mailQueue.Close(ctx)
	}()

	stats := metrics.New()
	stats.RegisterPool(pool)

	// the endpoint listens before the service does, so an address that is taken stops the start and not only the metrics
	listener, err := stats.Serve(cfg.MetricsAddress)
	if err != nil {
		return fmt.Errorf("start the metrics %w", err)
	}

	defer listener.Close()

	srv := app.New(app.Deps{Version: version, Config: cfg, Pool: pool, Mailer: mailer, Metrics: stats})

	serverErr := make(chan error, 1)

	go func() {
		serverErr <- srv.Listen(fmt.Sprintf(":%d", cfg.AppPort), fiber.ListenConfig{DisableStartupMessage: true})
	}()

	select {
	case err := <-serverErr:
		return fmt.Errorf("server stopped %w", err)
	case <-ctx.Done():
		log.Println("gracefully shutting down")
	}

	// from here a second signal gets its default action, so an operator can stop the process without waiting for the timeout
	stop()

	if err := srv.ShutdownWithTimeout(shutdownTimeout); err != nil {
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
