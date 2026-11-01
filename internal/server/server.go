// Package server builds the HTTP application that serves the API
package server

import (
	"github.com/gofiber/fiber/v3"
)

// New returns the application with its routes. The version is the one that the binary was built with.
func New(version string) *fiber.App {
	app := fiber.New()

	// the health route stays outside /v1 because it belongs to the container and not to the API that the dashboard reads
	app.Get("/healthz", func(c fiber.Ctx) error {
		return c.JSON(fiber.Map{"status": "ok", "version": version})
	})

	return app
}
