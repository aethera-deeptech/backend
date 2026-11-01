package metrics

import (
	"errors"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"

	"github.com/gofiber/fiber/v3"
	"github.com/prometheus/client_golang/prometheus/testutil"

	"github.com/aethera-deeptech/backend/internal/api"
)

func newApp(t *testing.T) (*fiber.App, *Metrics) {
	t.Helper()

	m := New()
	app := fiber.New(fiber.Config{ErrorHandler: api.ErrorHandler})
	app.Use(m.Middleware(), api.Recover())
	app.Use("/v1", func(c fiber.Ctx) error { return c.Next() })
	app.Get("/v1/things/:id", func(c fiber.Ctx) error { return c.SendString(c.Params("id")) })
	app.Get("/v1/limited", func(fiber.Ctx) error { return api.NewError(http.StatusTooManyRequests, "rate_limited", "slow down") })
	app.Get("/v1/boom", func(fiber.Ctx) error { panic("a nil map") })
	m.AllowRoutes(app.GetRoutes(true))

	return app, m
}

func get(t *testing.T, app *fiber.App, method, path string) int {
	t.Helper()

	res, err := app.Test(httptest.NewRequestWithContext(t.Context(), method, path, http.NoBody))
	if err != nil {
		t.Fatal(err)
	}

	_ = res.Body.Close()

	return res.StatusCode
}

func TestARequestIsCountedByRoutePatternAndNotByPath(t *testing.T) {
	app, m := newApp(t)

	get(t, app, http.MethodGet, "/v1/things/one")
	get(t, app, http.MethodGet, "/v1/things/two")

	if got := testutil.ToFloat64(m.requests.WithLabelValues("GET", "/v1/things/:id", "200")); got != 2 {
		t.Fatalf("the route was counted %v times, want 2", got)
	}
}

func TestAPathThatNoRouteOwnsIsOneSeries(t *testing.T) {
	app, m := newApp(t)

	for _, path := range []string{"/secret-1", "/secret-2", "/v1/nothing/here"} {
		get(t, app, http.MethodGet, path)
	}

	if got := testutil.ToFloat64(m.requests.WithLabelValues("GET", Unknown, "404")); got != 3 {
		t.Fatalf("the unknown paths were counted %v times, want 3", got)
	}
}

func TestAnUnknownPathUnderAGroupIsAlsoOneSeries(t *testing.T) {
	app, m := newApp(t)

	get(t, app, http.MethodGet, "/v1/nothing/here")

	if got := testutil.ToFloat64(m.requests.WithLabelValues("GET", Unknown, "404")); got != 1 {
		t.Fatalf("the unknown path under /v1 was counted %v times as unmatched, want 1", got)
	}
}

func TestTheErrorGoesOnToTheLogSoItKeepsItsCode(t *testing.T) {
	m := New()
	app := fiber.New(fiber.Config{ErrorHandler: api.ErrorHandler})

	var seen error

	app.Use(func(c fiber.Ctx) error {
		seen = c.Next()

		return seen
	}, m.Middleware())
	app.Get("/v1/limited", func(fiber.Ctx) error { return api.NewError(http.StatusTooManyRequests, "resend_too_soon", "slow down") })
	m.AllowRoutes(app.GetRoutes(true))

	if code := get(t, app, http.MethodGet, "/v1/limited"); code != http.StatusTooManyRequests {
		t.Fatalf("the answer was %d", code)
	}

	var apiErr *api.Error
	if !errors.As(seen, &apiErr) || apiErr.Code != "resend_too_soon" {
		t.Fatalf("the outer middleware saw %v, want the error with its code", seen)
	}

	// every 429 counts, whatever its code
	if got := testutil.ToFloat64(m.limited.WithLabelValues("/v1/limited")); got != 1 {
		t.Fatalf("the refusals are %v", got)
	}
}

func TestARefusalOfALimiterAndAPanicAreCounted(t *testing.T) {
	app, m := newApp(t)

	if code := get(t, app, http.MethodGet, "/v1/limited"); code != http.StatusTooManyRequests {
		t.Fatalf("the limiter answered %d", code)
	}

	if got := testutil.ToFloat64(m.limited.WithLabelValues("/v1/limited")); got != 1 {
		t.Fatalf("the refusals are %v", got)
	}

	if code := get(t, app, http.MethodGet, "/v1/boom"); code != http.StatusInternalServerError {
		t.Fatalf("the panic answered %d", code)
	}

	if got := testutil.ToFloat64(m.requests.WithLabelValues("GET", "/v1/boom", "500")); got != 1 {
		t.Fatalf("the panic was counted %v times", got)
	}

	if got := testutil.ToFloat64(m.inFlight); got != 0 {
		t.Fatalf("%v requests are still in flight", got)
	}
}

func TestTheRegistryHoldsTheRuntimeAndTheRequests(t *testing.T) {
	app, m := newApp(t)

	get(t, app, http.MethodGet, "/v1/things/one")

	families, err := m.Registry().Gather()
	if err != nil {
		t.Fatal(err)
	}

	names := []string{}
	for _, f := range families {
		names = append(names, f.GetName())
	}

	joined := strings.Join(names, " ")
	for _, want := range []string{"aethera_http_requests_total", "aethera_http_request_duration_seconds", "go_goroutines"} {
		if !strings.Contains(joined, want) {
			t.Fatalf("%s is missing from %s", want, joined)
		}
	}
}
