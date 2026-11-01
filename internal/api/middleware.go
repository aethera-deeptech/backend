package api

import (
	"errors"
	"fmt"
	"log/slog"
	"net/http"
	"strings"
	"time"

	"github.com/gofiber/fiber/v3"
)

// RequestedWith is the value of the header X-Requested-With that every request which changes data must carry
const RequestedWith = "aethera"

// SecurityHeaders sets the headers that make a browser treat the answers of an API as data and never as a page
func SecurityHeaders() fiber.Handler {
	return func(c fiber.Ctx) error {
		c.Set("X-Content-Type-Options", "nosniff")
		c.Set("Referrer-Policy", "no-referrer")
		c.Set("Content-Security-Policy", "default-src 'none'; frame-ancestors 'none'")
		c.Set("Cross-Origin-Resource-Policy", "same-site")

		return c.Next()
	}
}

// NoStore keeps a proxy or a browser from storing an answer with personal data
func NoStore() fiber.Handler {
	return func(c fiber.Ctx) error {
		c.Set(fiber.HeaderCacheControl, "private, no-store")

		return c.Next()
	}
}

// CSRF refuses a request that changes data unless it carries the header X-Requested-With and, when the browser sends an Origin,
// that origin is the one of the app. A page of another site cannot add that header without a preflight, and the CORS rules refuse
// the preflight, so a forged form post or a script of another origin never reaches a route.
func CSRF(allowedOrigin string) fiber.Handler {
	return func(c fiber.Ctx) error {
		switch c.Method() {
		case http.MethodGet, http.MethodHead, http.MethodOptions:
			return c.Next()
		}

		if c.Get("X-Requested-With") != RequestedWith {
			return NewError(http.StatusForbidden, "csrf_header_missing", "The header X-Requested-With is missing")
		}

		if origin := c.Get(fiber.HeaderOrigin); origin != "" && origin != allowedOrigin {
			return NewError(http.StatusForbidden, "origin_not_allowed", "This origin is not allowed")
		}

		return c.Next()
	}
}

// Recover turns a panic of a route into a logged 500, so one bad request cannot stop the copy
func Recover() fiber.Handler {
	return func(c fiber.Ctx) (err error) {
		defer func() {
			if r := recover(); r != nil {
				slog.Error("a route panicked", "method", c.Method(), "path", c.Path(), "panic", fmt.Sprint(r))

				err = NewError(http.StatusInternalServerError, "internal", "Something went wrong")
			}
		}()

		return c.Next()
	}
}

// AccessLog writes one JSON line for each request. It logs the path without the query and never a header or a body, so a token or a
// cookie cannot reach the log.
func AccessLog() fiber.Handler {
	return func(c fiber.Ctx) error {
		start := time.Now()
		err := c.Next()

		status := c.Response().StatusCode()

		var apiErr *Error

		if err != nil {
			var fiberErr *fiber.Error

			switch {
			case errors.As(err, &apiErr):
				status = apiErr.Status
			case errors.As(err, &fiberErr):
				status = fiberErr.Code
			default:
				status = http.StatusInternalServerError
			}
		}

		attrs := []any{
			"method", c.Method(), "path", strings.SplitN(c.Path(), "?", 2)[0], "status", status,
			"duration_ms", time.Since(start).Milliseconds(), "ip", c.IP(),
		}
		if apiErr != nil {
			attrs = append(attrs, "code", apiErr.Code)
		}

		level := slog.LevelInfo
		if status >= http.StatusInternalServerError {
			level = slog.LevelError
		}

		slog.Log(c.Context(), level, "request", attrs...)

		return err
	}
}
