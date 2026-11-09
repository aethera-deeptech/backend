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
	// a layer that hides the deadline, as a rollback does that fails with a closed connection after the query was cut
	app.Get("/hidden", func(c fiber.Ctx) error {
		<-c.Context().Done()

		return errors.New("roll back: conn closed")
	})
	app.Get("/chosen", func(c fiber.Ctx) error {
		<-c.Context().Done()

		return api.NotFound()
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

// TestARouteThatRunsOutOfTimeAnswers503Busy checks that an error after the deadline, also when a layer hid the deadline error,
// becomes the answer that the app already handles with a retry time
func TestARouteThatRunsOutOfTimeAnswers503Busy(t *testing.T) {
	for _, path := range []string{"/stall", "/wrapped", "/hidden"} {
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

func TestAnErrorThatTheRouteChoseKeepsItsAnswerAfterTheDeadline(t *testing.T) {
	if res := callDeadline(t, "/chosen"); res.StatusCode != http.StatusNotFound {
		t.Fatalf("a chosen error answered %d, want 404", res.StatusCode)
	}
}
