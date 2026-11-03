package api

import (
	"errors"
	"fmt"
	"log/slog"
	"net/http"
	"net/netip"
	"sync"
	"time"

	"github.com/gofiber/fiber/v3"

	"github.com/aethera-deeptech/backend/internal/errorreport"
)

// RequestedWith is the value of the header X-Requested-With that every request which changes data must carry
const RequestedWith = "aethera"

// SecurityHeaders sets the headers that make a browser treat the answers of an API as data and never as a page
func SecurityHeaders() fiber.Handler {
	return func(c fiber.Ctx) error {
		c.Set("X-Content-Type-Options", "nosniff")
		c.Set("Referrer-Policy", "no-referrer")
		c.Set("Content-Security-Policy", "default-src 'none'; frame-ancestors 'none'")
		c.Set("Cross-Origin-Resource-Policy", "same-origin")

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
				slog.Error("a route panicked", "method", c.Method(), "path", logPath(c), "panic_type", fmt.Sprintf("%T", r))
				errorreport.Panic(r, infoOf(c, http.StatusInternalServerError))

				err = NewError(http.StatusInternalServerError, "internal", "Something went wrong")
			}
		}()

		return c.Next()
	}
}

// StatusOf returns the status that the error handler gives an error, so a middleware can read it before the handler has run
func StatusOf(err error) int {
	var (
		apiErr   *Error
		fiberErr *fiber.Error
	)

	switch {
	case errors.As(err, &apiErr):
		return apiErr.Status
	case errors.As(err, &fiberErr):
		return fiberErr.Code
	default:
		return http.StatusInternalServerError
	}
}

// unmatchedPath is what a log line names for a request that no route answered
const unmatchedPath = "unmatched"

// handlerRoutes holds, for each app, the routes that have a handler of their own and not the middleware routes. The routes are
// registered before the first request, so the set is built once, at the first line that is logged.
var handlerRoutes sync.Map

func isHandlerRoute(c fiber.Ctx, route *fiber.Route) bool {
	set, ok := handlerRoutes.Load(c.App())
	if !ok {
		built := map[string]struct{}{}

		for _, r := range c.App().GetRoutes(true) {
			built[r.Method+" "+r.Path] = struct{}{}
		}

		set, _ = handlerRoutes.LoadOrStore(c.App(), built)
	}

	_, ok = set.(map[string]struct{})[route.Method+" "+route.Path]

	return ok
}

// logPath is the path that a log line names. A request that a route answered is named by the pattern of that route, because the value
// of a parameter can be a secret such as the poll id of an enrollment, and the pattern also keeps the lines of one route alike. A
// request that no route answered is named unmatched, because a client that sends a known secret to a wrong address would write it
// into the log otherwise. The middleware routes do not count as an answer.
func logPath(c fiber.Ctx) string {
	if route := c.Route(); route != nil && isHandlerRoute(c, route) {
		return route.Path
	}

	return unmatchedPath
}

// maxUnwrap is how many levels errorType follows, so a chain that loops cannot hold the error handler
const maxUnwrap = 16

// errorType names the type of the innermost error of a chain, which tells what failed without the text of the error. The text can
// hold a value of the request or a row of the database. An error that wraps several errors is followed along the first of them.
func errorType(err error) string {
	for range maxUnwrap {
		switch e := err.(type) { //nolint:errorlint // the loop walks the chain itself, one level at a time
		case interface{ Unwrap() error }:
			next := e.Unwrap()
			if next == nil {
				return fmt.Sprintf("%T", err)
			}

			err = next
		case interface{ Unwrap() []error }:
			all := e.Unwrap()
			if len(all) == 0 {
				return fmt.Sprintf("%T", err)
			}

			err = all[0]
		default:
			return fmt.Sprintf("%T", err)
		}
	}

	return fmt.Sprintf("%T", err)
}

// AccessLog writes one JSON line for each request. It logs the pattern of the route that answered, or unmatched, and never the query,
// a header or a body, so a token or a cookie cannot reach the log.
func AccessLog() fiber.Handler {
	return func(c fiber.Ctx) error {
		start := time.Now()
		err := c.Next()

		status := c.Response().StatusCode()

		var apiErr *Error

		if err != nil {
			status = StatusOf(err)
			_ = errors.As(err, &apiErr)
		}

		attrs := []any{
			"method", c.Method(), "path", logPath(c), "status", status,
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

// ClientKey returns the address that the limits count a client by. An IPv6 client usually owns a whole /64 and can change the
// rest of its address at no cost, so the key of an IPv6 address is its /64 prefix.
func ClientKey(c fiber.Ctx) string {
	return keyOf(c.IP())
}

func keyOf(raw string) string {
	ip, err := netip.ParseAddr(raw)
	if err != nil {
		return raw
	}

	ip = ip.Unmap()

	if ip.Is6() {
		if prefix, err := ip.Prefix(64); err == nil {
			return prefix.String()
		}
	}

	return ip.String()
}

// BodyLimit refuses a body that is larger than max bytes. The routes of the account take a few hundred bytes, so they need no more than a few KiB.
func BodyLimit(max int) fiber.Handler {
	return func(c fiber.Ctx) error {
		if len(c.Body()) > max {
			return NewError(http.StatusRequestEntityTooLarge, "payload_too_large", "The request is too large")
		}

		return c.Next()
	}
}
