package api_test

import (
	"bytes"
	"errors"
	"fmt"
	"log/slog"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"

	"github.com/gofiber/fiber/v3"

	"github.com/aethera-deeptech/backend/internal/api"
)

// TestTheLogsNameTheRouteAndNotTheValueOfAParameter checks that the access log of a route with a parameter has the pattern and not the
// value, that a failure and a panic are logged by the type of the error and not its text, and that a route with no parameter keeps
// its path
func TestTheLogsNameTheRouteAndNotTheValueOfAParameter(t *testing.T) {
	var out bytes.Buffer

	previous := slog.Default()

	slog.SetDefault(slog.New(slog.NewJSONHandler(&out, nil)))
	t.Cleanup(func() { slog.SetDefault(previous) })

	app := fiber.New(fiber.Config{ErrorHandler: api.ErrorHandler})
	app.Use(api.AccessLog(), api.Recover())
	app.Get("/enroll/:pollId", func(fiber.Ctx) error { return nil })
	app.Get("/fail/:id", func(fiber.Ctx) error {
		return fmt.Errorf("read the row of secret-text %w", errors.New("the database is gone"))
	})
	app.Get("/boom/:id", func(fiber.Ctx) error { panic("secret-text") })
	app.Get("/healthz", func(fiber.Ctx) error { return nil })

	for _, path := range []string{"/enroll/secret-poll?x=1", "/fail/secret-id", "/boom/secret-id", "/healthz?x=1"} {
		res, err := app.Test(httptest.NewRequestWithContext(t.Context(), http.MethodGet, path, http.NoBody))
		if err != nil {
			t.Fatal(err)
		}

		_ = res.Body.Close()
	}

	logged := out.String()

	for _, secret := range []string{"secret-poll", "secret-id", "secret-text", "the database is gone"} {
		if strings.Contains(logged, secret) {
			t.Fatalf("the log holds %q\n%s", secret, logged)
		}
	}

	wants := []string{
		`"path":"/enroll/:pollId"`, `"path":"/fail/:id"`, `"path":"/boom/:id"`, `"path":"/healthz"`,
		`"error_type":"*errors.errorString"`, `"panic_type":"string"`,
	}

	for _, want := range wants {
		if !strings.Contains(logged, want) {
			t.Fatalf("the log lacks %s\n%s", want, logged)
		}
	}
}
