package healthcheck_test

import (
	"net/http"
	"net/http/httptest"
	"net/url"
	"testing"

	"github.com/aethera-deeptech/backend/internal/healthcheck"
)

// serving answers the given status on /healthz and 418 on any other path, so a probe that calls the wrong path cannot pass
func serving(t *testing.T, status int) *httptest.Server {
	t.Helper()

	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.URL.Path != "/healthz" {
			w.WriteHeader(http.StatusTeapot)

			return
		}

		w.WriteHeader(status)
	}))
	t.Cleanup(server.Close)

	return server
}

func TestURLUsesThePortWhenItIsANumber(t *testing.T) {
	for port, want := range map[string]string{
		"9000":  "http://127.0.0.1:9000/healthz",
		"65535": "http://127.0.0.1:65535/healthz",
	} {
		if got := healthcheck.URL(port); got != want {
			t.Errorf("URL(%q) is %q, want %q", port, got, want)
		}
	}
}

func TestURLFallsBackToTheDefaultPort(t *testing.T) {
	for name, port := range map[string]string{
		"empty":        "",
		"not a number": "abc",
		"negative":     "-1",
		"too large":    "65536",
		"injection":    "80/../admin",
	} {
		t.Run(name, func(t *testing.T) {
			if got, want := healthcheck.URL(port), "http://127.0.0.1:8080/healthz"; got != want {
				t.Fatalf("URL(%q) is %q, want %q", port, got, want)
			}
		})
	}
}

func TestCheckPassesOn200(t *testing.T) {
	server := serving(t, http.StatusOK)

	if err := healthcheck.Check(t.Context(), server.Client(), server.URL+"/healthz"); err != nil {
		t.Fatalf("unexpected error %v", err)
	}
}

func TestCheckFailsOnAnyOtherStatus(t *testing.T) {
	for _, status := range []int{http.StatusNoContent, http.StatusMovedPermanently, http.StatusTooManyRequests, http.StatusServiceUnavailable} {
		server := serving(t, status)

		if err := healthcheck.Check(t.Context(), server.Client(), server.URL+"/healthz"); err == nil {
			t.Errorf("status %d passed the check", status)
		}
	}
}

func TestCheckFailsOnTheWrongPath(t *testing.T) {
	server := serving(t, http.StatusOK)

	if err := healthcheck.Check(t.Context(), server.Client(), server.URL+"/other"); err == nil {
		t.Fatal("a call to another path passed the check")
	}
}

func TestCheckFailsWhenNothingListens(t *testing.T) {
	server := serving(t, http.StatusOK)
	target := server.URL + "/healthz"
	server.Close()

	if err := healthcheck.Check(t.Context(), http.DefaultClient, target); err == nil {
		t.Fatal("a closed server passed the check")
	}
}

func TestRunReturnsTheExitCode(t *testing.T) {
	healthy := serving(t, http.StatusOK)
	parsed, err := url.Parse(healthy.URL)
	if err != nil {
		t.Fatalf("parse %v", err)
	}

	if code := healthcheck.Run(parsed.Port()); code != 0 {
		t.Errorf("exit code is %d for a healthy server", code)
	}

	down := serving(t, http.StatusServiceUnavailable)
	parsed, err = url.Parse(down.URL)
	if err != nil {
		t.Fatalf("parse %v", err)
	}

	if code := healthcheck.Run(parsed.Port()); code != 1 {
		t.Errorf("exit code is %d for an unhealthy server", code)
	}
}
