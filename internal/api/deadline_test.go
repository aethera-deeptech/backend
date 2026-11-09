package api_test

import (
	"errors"
	"net/http"
	"net/http/httptest"
	"testing"
	"time"

	"github.com/gofiber/fiber/v3"

	"github.com/aethera-deeptech/backend/internal/api"
)

func deadlineApp() *fiber.App {
	app := fiber.New(fiber.Config{ErrorHandler: api.ErrorHandler})
	app.Use(api.Deadline(30 * time.Millisecond))

	// a route that waits for a database that never answers, as a query on a busy pool does
	app.Get("/stall", func(c fiber.Ctx) error {
		<-c.Context().Done()

		return c.Context().Err()
	})
	app.Get("/wrapped", func(c fiber.Ctx) error {
		<-c.Context().Done()

		return errors.Join(errors.New("acquire a connection"), c.Context().Err())
	})
	app.Get("/other", func(fiber.Ctx) error { return errors.New("boom") })
	app.Get("/fast", func(c fiber.Ctx) error {
		if _, ok := c.Context().Deadline(); !ok {
			return errors.New("the context has no deadline")
		}

		return c.SendStatus(http.StatusNoContent)
	})

	return app
}

func callDeadline(t *testing.T, path string) *http.Response {
	t.Helper()

	res, err := deadlineApp().Test(httptest.NewRequestWithContext(t.Context(), http.MethodGet, path, http.NoBody), fiber.TestConfig{Timeout: 5 * time.Second})
	if err != nil {
		t.Fatal(err)
	}

	t.Cleanup(func() { _ = res.Body.Close() })

	return res
}

// TestARouteThatRunsOutOfTimeAnswers503Busy checks that a deadline error of the context, also when a layer wrapped it, becomes the
// answer that the app already handles with a retry time
func TestARouteThatRunsOutOfTimeAnswers503Busy(t *testing.T) {
	for _, path := range []string{"/stall", "/wrapped"} {
		res := callDeadline(t, path)

		if res.StatusCode != http.StatusServiceUnavailable || res.Header.Get("Retry-After") != "2" {
			t.Fatalf("%s answered %d with Retry-After %q, want 503 and 2", path, res.StatusCode, res.Header.Get("Retry-After"))
		}
	}
}

func TestARouteInTimeKeepsItsAnswerAndSeesTheDeadline(t *testing.T) {
	if res := callDeadline(t, "/fast"); res.StatusCode != http.StatusNoContent {
		t.Fatalf("a route in time answered %d, want 204", res.StatusCode)
	}
}

func TestAnotherErrorStaysA500(t *testing.T) {
	if res := callDeadline(t, "/other"); res.StatusCode != http.StatusInternalServerError {
		t.Fatalf("another error answered %d, want 500", res.StatusCode)
	}
}
