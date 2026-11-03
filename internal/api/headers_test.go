package api_test

import (
	"net/http"
	"net/http/httptest"
	"testing"

	"github.com/gofiber/fiber/v3"

	"github.com/aethera-deeptech/backend/internal/api"
)

// TestEveryAnswerCarriesTheSecurityHeaders checks the headers that make a browser treat an answer as data, among them the policy that
// keeps another origin from loading the answer without CORS
func TestEveryAnswerCarriesTheSecurityHeaders(t *testing.T) {
	app := fiber.New()
	app.Use(api.SecurityHeaders())
	app.Get("/", func(fiber.Ctx) error { return nil })

	res, err := app.Test(httptest.NewRequestWithContext(t.Context(), http.MethodGet, "/", http.NoBody))
	if err != nil {
		t.Fatal(err)
	}

	_ = res.Body.Close()

	want := map[string]string{
		"X-Content-Type-Options":       "nosniff",
		"Referrer-Policy":              "no-referrer",
		"Content-Security-Policy":      "default-src 'none'; frame-ancestors 'none'",
		"Cross-Origin-Resource-Policy": "same-origin",
	}

	for name, value := range want {
		if got := res.Header.Get(name); got != value {
			t.Fatalf("%s is %q, want %q", name, got, value)
		}
	}
}
