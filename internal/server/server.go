// Package server builds the HTTP application that serves the API
package server

import (
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

// New returns the application with its routes. The version is the one that the binary was built with.
func New(version string) *fiber.App {
	app := fiber.New(fiber.Config{
		ReadTimeout:  readTimeout,
		WriteTimeout: writeTimeout,
		IdleTimeout:  idleTimeout,
	})

	// the health route stays outside /v1 because it belongs to the container and not to the API that the dashboard reads
	app.Get("/healthz", func(c fiber.Ctx) error {
		return c.JSON(fiber.Map{"status": "ok", "version": version})
	})

	return app
}
