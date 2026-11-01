package server

import (
	"context"
	"encoding/json"
	"errors"
	"net/http"
	"net/http/httptest"
	"testing"
)

func TestHealthz(t *testing.T) {
	app := New("v1.2.3", fakeDB{})

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
	app := New("dev", fakeDB{})

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
	srv := New("dev", fakeDB{}).Server()

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
			res, err := New("v1", tt.db).Test(httptest.NewRequest(http.MethodGet, "/readyz", http.NoBody))
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

func TestHealthzIgnoresTheDatabase(t *testing.T) {
	res, err := New("v1", fakeDB{err: errors.New("down")}).Test(httptest.NewRequest(http.MethodGet, "/healthz", http.NoBody))
	if err != nil {
		t.Fatalf("request failed %v", err)
	}

	defer func() { _ = res.Body.Close() }()

	if res.StatusCode != http.StatusOK {
		t.Fatalf("status is %d, want 200 while the database is down", res.StatusCode)
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
	app := New("v1", db)

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
