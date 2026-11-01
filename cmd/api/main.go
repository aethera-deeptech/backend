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

	"github.com/aethera-deeptech/backend/internal/config"
	"github.com/aethera-deeptech/backend/internal/logging"
	"github.com/aethera-deeptech/backend/internal/server"
)

// version is set at build time with -ldflags
var version = "dev"

// shutdownTimeout is how long the server lets requests that are running finish after a stop signal
const shutdownTimeout = 15 * time.Second

func main() {
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

	app := server.New(version)

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
