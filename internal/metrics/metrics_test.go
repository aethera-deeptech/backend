package metrics

import (
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
	app.Get("/v1/things/:id", func(c fiber.Ctx) error { return c.SendString(c.Params("id")) })
	app.Get("/v1/limited", func(fiber.Ctx) error { return api.NewError(http.StatusTooManyRequests, "rate_limited", "slow down") })
	app.Get("/v1/boom", func(fiber.Ctx) error { panic("a nil map") })
	m.AllowRoutes(app.GetRoutes())

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
