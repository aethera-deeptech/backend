package server

import (
	"context"
	"encoding/json"
	"errors"
	"net/http"
	"net/http/httptest"
	"testing"

	"github.com/gofiber/fiber/v3"
)

func TestHealthz(t *testing.T) {
	app := New(Options{
		AppOrigin: "https://app.example.test", Version: "v1.2.3", DB: fakeDB{}, Mail: "smtp",
		Pool: func() PoolStats { return PoolStats{Acquired: 2, Idle: 3, Max: 10} },
	})

	res, err := app.Test(httptest.NewRequest(http.MethodGet, "/healthz", http.NoBody))
	if err != nil {
		t.Fatalf("request failed %v", err)
	}

	defer func() { _ = res.Body.Close() }()

	if res.StatusCode != http.StatusOK {
		t.Fatalf("status is %d, want 200", res.StatusCode)
	}

	var body struct {
		Status        string            `json:"status"`
		Version       string            `json:"version"`
		UptimeSeconds *int64            `json:"uptimeSeconds"`
		Checks        map[string]string `json:"checks"`
		Pool          map[string]int    `json:"pool"`
	}
	if err := json.NewDecoder(res.Body).Decode(&body); err != nil {
		t.Fatalf("the body is not JSON %v", err)
	}

	if body.Status != "ok" || body.Version != "v1.2.3" || body.UptimeSeconds == nil || *body.UptimeSeconds < 0 {
		t.Fatalf("unexpected body %+v", body)
	}

	if body.Checks["database"] != "ok" || body.Checks["mail"] != "smtp" {
		t.Fatalf("unexpected checks %v", body.Checks)
	}

	if body.Pool["acquired"] != 2 || body.Pool["idle"] != 3 || body.Pool["max"] != 10 {
		t.Fatalf("unexpected pool %v", body.Pool)
	}
}

func TestUnknownRouteIs404(t *testing.T) {
	app := New(Options{AppOrigin: "https://app.example.test", Version: "dev", DB: fakeDB{}})

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
	srv := New(Options{AppOrigin: "https://app.example.test", Version: "dev", DB: fakeDB{}}).Server()

	if srv.ReadTimeout != readTimeout || srv.WriteTimeout != writeTimeout || srv.IdleTimeout != idleTimeout {
		t.Fatalf("limits are %v, %v and %v", srv.ReadTimeout, srv.WriteTimeout, srv.IdleTimeout)
	}
}

// fakeDB answers a ping with its error
type fakeDB struct{ err error }

func (f fakeDB) Ping(context.Context) error { return f.err }

func TestReadyz(t *testing.T) {
	tests := map[string]struct {
		db   fakeDB
		code int
		body string
	}{
		"database answers":  {db: fakeDB{}, code: http.StatusOK, body: "ready"},
		"database does not": {db: fakeDB{err: errors.New("down")}, code: http.StatusServiceUnavailable, body: "unavailable"},
	}

	for name, tt := range tests {
		t.Run(name, func(t *testing.T) {
			res, err := New(Options{AppOrigin: "https://app.example.test", Version: "v1", DB: tt.db}).Test(httptest.NewRequest(http.MethodGet, "/readyz", http.NoBody))
			if err != nil {
				t.Fatalf("request failed %v", err)
			}

			defer func() { _ = res.Body.Close() }()

			var body map[string]string
			if err := json.NewDecoder(res.Body).Decode(&body); err != nil {
				t.Fatalf("the body is not JSON %v", err)
			}

			if res.StatusCode != tt.code || body["status"] != tt.body {
				t.Fatalf("got %d %v, want %d %s", res.StatusCode, body, tt.code, tt.body)
			}
		})
	}
}

func TestHealthzSaysWhenTheDatabaseIsDown(t *testing.T) {
	res, err := New(Options{AppOrigin: "https://app.example.test", Version: "v1", DB: fakeDB{err: errors.New("down")}}).Test(httptest.NewRequest(http.MethodGet, "/healthz", http.NoBody))
	if err != nil {
		t.Fatalf("request failed %v", err)
	}

	defer func() { _ = res.Body.Close() }()

	if res.StatusCode != http.StatusServiceUnavailable {
		t.Fatalf("status is %d, want 503 while the database is down", res.StatusCode)
	}

	var body struct {
		Status  string            `json:"status"`
		Version string            `json:"version"`
		Checks  map[string]string `json:"checks"`
	}
	if err := json.NewDecoder(res.Body).Decode(&body); err != nil {
		t.Fatalf("the body is not JSON %v", err)
	}

	if body.Status != "down" || body.Version != "v1" || body.Checks["database"] != "down" {
		t.Fatalf("unexpected body %+v", body)
	}
}

// countingDB counts the pings
type countingDB struct{ pings int }

func (c *countingDB) Ping(context.Context) error {
	c.pings++

	return nil
}

func TestReadyzReusesTheAnswerForAMoment(t *testing.T) {
	db := &countingDB{}
	app := New(Options{AppOrigin: "https://app.example.test", Version: "v1", DB: db})

	for range 5 {
		res, err := app.Test(httptest.NewRequest(http.MethodGet, "/readyz", http.NoBody))
		if err != nil {
			t.Fatalf("request failed %v", err)
		}

		_ = res.Body.Close()
	}

	if db.pings != 1 {
		t.Fatalf("the database was pinged %d times for 5 requests, want 1", db.pings)
	}
}

func healthBody(t *testing.T, app *fiber.App, path string) (int, map[string]any) {
	t.Helper()

	res, err := app.Test(httptest.NewRequest(http.MethodGet, path, http.NoBody))
	if err != nil {
		t.Fatalf("request failed %v", err)
	}

	defer func() { _ = res.Body.Close() }()

	var body map[string]any
	if err := json.NewDecoder(res.Body).Decode(&body); err != nil {
		t.Fatalf("the body is not JSON %v", err)
	}

	return res.StatusCode, body
}

func TestHealthzIsBusyNotDownWhenThePoolIsFull(t *testing.T) {
	app := New(Options{
		AppOrigin: "https://app.example.test", Version: "v1", DB: fakeDB{err: errors.New("timeout")},
		Pool: func() PoolStats { return PoolStats{Acquired: 10, Idle: 0, Max: 10} },
	})

	code, body := healthBody(t, app, "/healthz")
	checks, _ := body["checks"].(map[string]any)

	if code != http.StatusOK || body["status"] != "ok" || checks["database"] != "busy" {
		t.Fatalf("got %d %v, want 200 ok with database busy", code, body)
	}
}

func TestHealthzIsDownWhenThePingFailsAndThePoolHasRoom(t *testing.T) {
	app := New(Options{
		AppOrigin: "https://app.example.test", Version: "v1", DB: fakeDB{err: errors.New("refused")},
		Pool: func() PoolStats { return PoolStats{Acquired: 1, Idle: 2, Max: 10} },
	})

	code, body := healthBody(t, app, "/healthz")
	pool, _ := body["pool"].(map[string]any)

	if code != http.StatusServiceUnavailable || body["status"] != "down" || pool["max"] != float64(10) {
		t.Fatalf("got %d %v, want 503 down with the pool", code, body)
	}
}

func TestHealthzLeavesOutThePoolAndTheMailWhenTheyAreNotGiven(t *testing.T) {
	_, body := healthBody(t, New(Options{AppOrigin: "https://app.example.test", Version: "v1", DB: fakeDB{}}), "/healthz")
	checks, _ := body["checks"].(map[string]any)

	if _, ok := body["pool"]; ok {
		t.Fatalf("the pool is in the body %v", body)
	}

	if _, ok := checks["mail"]; ok {
		t.Fatalf("the mail is in the checks %v", checks)
	}
}

func TestHealthzAndReadyzShareOnePing(t *testing.T) {
	db := &countingDB{}
	app := New(Options{AppOrigin: "https://app.example.test", Version: "v1", DB: db})

	healthBody(t, app, "/healthz")
	healthBody(t, app, "/readyz")
	healthBody(t, app, "/healthz")

	if db.pings != 1 {
		t.Fatalf("the database was pinged %d times, want 1", db.pings)
	}
}
