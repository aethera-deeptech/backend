package api_test

import (
	"context"
	"errors"
	"net/http"
	"net/http/httptest"
	"strings"
	"sync"
	"testing"
	"time"

	"github.com/getsentry/sentry-go"
	"github.com/gofiber/fiber/v3"

	"github.com/aethera-deeptech/backend/internal/api"
	"github.com/aethera-deeptech/backend/internal/errorreport"
)

type recorder struct {
	mu     sync.Mutex
	events []*sentry.Event
}

func (r *recorder) Flush(time.Duration) bool              { return true }
func (r *recorder) FlushWithContext(context.Context) bool { return true }
func (r *recorder) Configure(sentry.ClientOptions)        {}
func (r *recorder) Close()                                {}

func (r *recorder) SendEvent(e *sentry.Event) {
	r.mu.Lock()
	defer r.mu.Unlock()

	r.events = append(r.events, e)
}

// TestOnlyAFailureOfTheServerIsReported checks that a 500 and a panic are sent with the pattern of the route and never with the
// path that was called, and that an answer that the API chose is not sent at all
func TestOnlyAFailureOfTheServerIsReported(t *testing.T) {
	rec := &recorder{}
	stop := errorreport.Start(errorreport.Config{DSN: "https://public@o0.ingest.example.invalid/1", EventsPerMinute: 30, Transport: rec})

	t.Cleanup(stop)

	app := fiber.New(fiber.Config{ErrorHandler: api.ErrorHandler})
	app.Use(api.Recover())
	app.Get("/fail/:id", func(fiber.Ctx) error { return errors.New("the database is gone") })
	app.Get("/boom/:id", func(fiber.Ctx) error { panic("a nil map") })
	app.Get("/chosen/:id", func(fiber.Ctx) error { return api.NotFound() })

	for _, path := range []string{"/fail/secret-1", "/boom/secret-2", "/chosen/secret-3"} {
		res, err := app.Test(httptest.NewRequestWithContext(t.Context(), http.MethodGet, path, http.NoBody))
		if err != nil {
			t.Fatal(err)
		}

		_ = res.Body.Close()
	}

	rec.mu.Lock()
	defer rec.mu.Unlock()

	if len(rec.events) != 2 {
		t.Fatalf("%d events were sent, want 2", len(rec.events))
	}

	routes := map[string]bool{}

	for _, e := range rec.events {
		routes[e.Tags["route"]] = true

		if e.Tags["status"] != "500" {
			t.Fatalf("the status tag is %q", e.Tags["status"])
		}

		for _, v := range e.Tags {
			if strings.Contains(v, "secret-") {
				t.Fatalf("the path leaked into a tag %q", v)
			}
		}
	}

	if !routes["/fail/:id"] || !routes["/boom/:id"] {
		t.Fatalf("the routes are %v", routes)
	}
}
