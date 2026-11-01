package server

import (
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"testing"
)

func TestHealthz(t *testing.T) {
	app := New("v1.2.3")

	res, err := app.Test(httptest.NewRequest(http.MethodGet, "/healthz", http.NoBody))
	if err != nil {
		t.Fatalf("request failed %v", err)
	}

	defer func() { _ = res.Body.Close() }()

	if res.StatusCode != http.StatusOK {
		t.Fatalf("status is %d, want 200", res.StatusCode)
	}

	var body map[string]string
	if err := json.NewDecoder(res.Body).Decode(&body); err != nil {
		t.Fatalf("the body is not JSON %v", err)
	}

	if body["status"] != "ok" || body["version"] != "v1.2.3" {
		t.Fatalf("unexpected body %v", body)
	}
}

func TestUnknownRouteIs404(t *testing.T) {
	app := New("dev")

	res, err := app.Test(httptest.NewRequest(http.MethodGet, "/nothing", http.NoBody))
	if err != nil {
		t.Fatalf("request failed %v", err)
	}

	defer func() { _ = res.Body.Close() }()

	if res.StatusCode != http.StatusNotFound {
		t.Fatalf("status is %d, want 404", res.StatusCode)
	}
}

func TestConnectionLimits(t *testing.T) {
	srv := New("dev").Server()

	if srv.ReadTimeout != readTimeout || srv.WriteTimeout != writeTimeout || srv.IdleTimeout != idleTimeout {
		t.Fatalf("limits are %v, %v and %v", srv.ReadTimeout, srv.WriteTimeout, srv.IdleTimeout)
	}
}
