// Package testutil builds the whole application on a test database, so a test sends real requests through the same routes and
// middleware as production. Only tests import it.
package testutil

import (
	"context"
	"encoding/json"
	"io"
	"net/http"
	"net/http/httptest"
	"net/url"
	"strings"
	"sync"
	"testing"

	"github.com/gofiber/fiber/v3"
	"github.com/jackc/pgx/v5/pgxpool"

	"github.com/aethera-deeptech/backend/internal/app"
	"github.com/aethera-deeptech/backend/internal/config"
	"github.com/aethera-deeptech/backend/internal/database/dbtest"
)

// Origin is the origin of the app in the tests
const Origin = "https://app.example.test"

// CookieName is the name of the session cookie in the tests, which run as production
const CookieName = "__Host-aethera_session"

// Outbox keeps the links that the service would have sent by email
type Outbox struct {
	mu     sync.Mutex
	verify map[string]string
	reset  map[string]string
}

// Verification keeps the link of a verification email
func (o *Outbox) Verification(to, link string) {
	o.mu.Lock()
	defer o.mu.Unlock()

	o.verify[to] = link
}

// PasswordReset keeps the link of a reset email
func (o *Outbox) PasswordReset(to, link string) {
	o.mu.Lock()
	defer o.mu.Unlock()

	o.reset[to] = link
}

// VerifyLink returns the last link of a verification email to the address, and whether there is one
func (o *Outbox) VerifyLink(to string) (string, bool) {
	o.mu.Lock()
	defer o.mu.Unlock()

	l, ok := o.verify[to]

	return l, ok
}

// ResetLink returns the last link of a reset email to the address, and whether there is one
func (o *Outbox) ResetLink(to string) (string, bool) {
	o.mu.Lock()
	defer o.mu.Unlock()

	l, ok := o.reset[to]

	return l, ok
}

// Token returns the token in a link to the app
func Token(t *testing.T, link string, ok bool) string {
	t.Helper()

	if !ok {
		t.Fatal("no email was sent")
	}

	u, err := url.Parse(link)
	if err != nil || !strings.HasPrefix(link, Origin+"/") {
		t.Fatalf("the link %q is not a link to the app", link)
	}

	return u.Query().Get("token")
}

// Env is an application on its own database
type Env struct {
	App  *fiber.App
	Pool *pgxpool.Pool
	Mail *Outbox
}

// New builds the application on a new test database. The test skips itself when no test server is set.
func New(t *testing.T) *Env {
	t.Helper()

	pool := dbtest.New(t)
	mail := &Outbox{verify: map[string]string{}, reset: map[string]string{}}

	cfg := config.Config{AppEnv: config.EnvProduction, AppOrigin: Origin}

	return &Env{App: app.New(app.Deps{Version: "test", Config: cfg, Pool: pool, Mailer: mail}), Pool: pool, Mail: mail}
}

// Reply is an answer with its body read as a JSON object
type Reply struct {
	Status int
	Body   map[string]any
	Cookie string
	Header http.Header
	Raw    []byte
}

// Call sends a JSON request the way the app does, with the CSRF header and the origin
func (e *Env) Call(t *testing.T, method, path, body, cookie string) Reply {
	t.Helper()

	req := httptest.NewRequestWithContext(t.Context(), method, path, strings.NewReader(body))
	req.Header.Set("Content-Type", "application/json")
	req.Header.Set("X-Requested-With", "aethera")
	req.Header.Set("Origin", Origin)

	if cookie != "" {
		req.Header.Set("Cookie", CookieName+"="+cookie)
	}

	return e.Send(t, req)
}

// Send sends a request that the caller built
func (e *Env) Send(t *testing.T, req *http.Request) Reply {
	t.Helper()

	res, err := e.App.Test(req)
	if err != nil {
		t.Fatalf("request failed %v", err)
	}

	defer func() { _ = res.Body.Close() }()

	raw, _ := io.ReadAll(res.Body)
	out := Reply{Status: res.StatusCode, Header: res.Header, Raw: raw}

	if len(raw) > 0 && strings.HasPrefix(res.Header.Get("Content-Type"), "application/json") {
		if err := json.Unmarshal(raw, &out.Body); err != nil {
			t.Fatalf("the body %q is not a JSON object %v", raw, err)
		}
	}

	for _, c := range res.Cookies() {
		if c.Name == CookieName && c.MaxAge >= 0 && c.Value != "" {
			out.Cookie = c.Value
		}
	}

	return out
}

// Code returns the code of an error answer
func (r Reply) Code() any { return r.Body["code"] }

// Register signs up a business with the email and a known password
func (e *Env) Register(t *testing.T, email string) {
	t.Helper()

	r := e.Call(t, http.MethodPost, "/v1/auth/register", `{"businessName":"Toko Maju","email":"`+email+`","password":"kata sandi baik"}`, "")
	if r.Status != http.StatusCreated {
		t.Fatalf("register answered %d %v", r.Status, r.Body)
	}
}

// SignUp registers, verifies the email and returns the session cookie
func (e *Env) SignUp(t *testing.T, email string) string {
	t.Helper()

	e.Register(t, email)

	link, ok := e.Mail.VerifyLink(email)

	r := e.Call(t, http.MethodPost, "/v1/auth/verify-email", `{"token":"`+Token(t, link, ok)+`"}`, "")
	if r.Status != http.StatusOK || r.Cookie == "" {
		t.Fatalf("verify answered %d %v", r.Status, r.Body)
	}

	return r.Cookie
}

// Exec runs a statement on the test database
func (e *Env) Exec(t *testing.T, sql string, args ...any) {
	t.Helper()

	if _, err := e.Pool.Exec(context.Background(), sql, args...); err != nil {
		t.Fatalf("run %q %v", sql, err)
	}
}

// CallWith sends a JSON request with a header of the caller, such as the bearer key of a gateway. It sends no CSRF header and no
// origin, like a gateway.
func (e *Env) CallWith(t *testing.T, method, path, body string, headers map[string]string) Reply {
	t.Helper()

	req := httptest.NewRequestWithContext(t.Context(), method, path, strings.NewReader(body))
	req.Header.Set("Content-Type", "application/json")

	for k, v := range headers {
		req.Header.Set(k, v)
	}

	return e.Send(t, req)
}

// Seeded is a business that has one device on one gateway, which the tests of the views use
type Seeded struct {
	e        *Env
	Session  string
	Business string
	Device   string
}

// SeedDevice signs a business up and gives it one device named Mesin jahit 1 on one gateway, with no readings yet
func (e *Env) SeedDevice(t *testing.T, email, hardware string) *Seeded {
	t.Helper()

	g := &Seeded{e: e, Session: e.SignUp(t, email)}

	if err := e.Pool.QueryRow(t.Context(), "SELECT business_id::text FROM users WHERE email = $1", email).Scan(&g.Business); err != nil {
		t.Fatal(err)
	}

	var gateway, room string

	if err := e.Pool.QueryRow(t.Context(),
		"INSERT INTO gateways (business_id, name, hardware_id, key_hash) VALUES ($1, 'Hub', $2, sha256(convert_to($2, 'UTF8'))) RETURNING id::text",
		g.Business, hardware).Scan(&gateway); err != nil {
		t.Fatal(err)
	}

	e.Exec(t, "INSERT INTO nodes (gateway_id, node_id, kind) VALUES ($1, 'PLUG-0001', 'plug')", gateway)

	if err := e.Pool.QueryRow(t.Context(), "INSERT INTO rooms (business_id, name) VALUES ($1, 'Ruang jahit') RETURNING id::text", g.Business).Scan(&room); err != nil {
		t.Fatal(err)
	}

	if err := e.Pool.QueryRow(t.Context(),
		"INSERT INTO devices (business_id, gateway_id, node_id, kind, name, room_id) VALUES ($1, $2, 'PLUG-0001', 'plug', 'Mesin jahit 1', $3) RETURNING id::text",
		g.Business, gateway, room).Scan(&g.Device); err != nil {
		t.Fatal(err)
	}

	return g
}

// Hours writes one hour of data for every hour from a number of hours ago to a number of hours ago, with the same power and energy
// in each. An hour that has a row already is left alone.
func (g *Seeded) Hours(t *testing.T, fromAgo, toAgo int, watts, kwh float64) {
	t.Helper()

	g.e.Exec(t, "INSERT INTO readings_hourly (gateway_id, node_id, hour, samples, sum_watts, max_watts, min_watts, energy_kwh) "+
		"SELECT d.gateway_id, d.node_id, date_trunc('hour', now()) - make_interval(hours => h), 10, $4::float8 * 10, $4, $4, $5 "+
		"FROM devices d, generate_series($2::int, $3::int) h WHERE d.id = $1 ON CONFLICT DO NOTHING", g.Device, toAgo, fromAgo, watts, kwh)
}
