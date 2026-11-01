package auth_test

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

	"github.com/aethera-deeptech/backend/internal/auth"
	"github.com/aethera-deeptech/backend/internal/database/dbtest"
	"github.com/aethera-deeptech/backend/internal/server"
)

const origin = "https://app.example.test"

// outbox keeps the links that the service would have sent by email
type outbox struct {
	mu     sync.Mutex
	verify map[string]string
	reset  map[string]string
}

func (o *outbox) Verification(to, link string) {
	o.mu.Lock()
	defer o.mu.Unlock()

	o.verify[to] = link
}

func (o *outbox) PasswordReset(to, link string) {
	o.mu.Lock()
	defer o.mu.Unlock()

	o.reset[to] = link
}

func (o *outbox) token(t *testing.T, links map[string]string, to string) string {
	t.Helper()

	o.mu.Lock()
	defer o.mu.Unlock()

	link, ok := links[to]
	if !ok {
		t.Fatalf("no email was sent to %s", to)
	}

	u, err := url.Parse(link)
	if err != nil || !strings.HasPrefix(link, origin+"/") {
		t.Fatalf("the link %q is not a link to the app", link)
	}

	return u.Query().Get("token")
}

type env struct {
	app  *fiber.App
	pool *pgxpool.Pool
	mail *outbox
}

func newEnv(t *testing.T) *env {
	t.Helper()

	pool := dbtest.New(t)
	mail := &outbox{verify: map[string]string{}, reset: map[string]string{}}

	app := server.New(server.Options{Version: "test", DB: pool, AppOrigin: origin})
	svc := auth.NewService(auth.NewStore(pool), auth.NewLimiter(pool), auth.NewHasher(2), mail, origin)
	auth.NewHandlers(svc, auth.CookieFor(true)).Register(app.Group("/v1"))

	return &env{app: app, pool: pool, mail: mail}
}

type reply struct {
	status int
	body   map[string]any
	cookie string
	header http.Header
}

// call sends a JSON request the way the app does, with the CSRF header and the origin
func (e *env) call(t *testing.T, method, path, body, cookie string) reply {
	t.Helper()

	req := httptest.NewRequest(method, path, strings.NewReader(body))
	req.Header.Set("Content-Type", "application/json")
	req.Header.Set("X-Requested-With", "aethera")
	req.Header.Set("Origin", origin)

	if cookie != "" {
		req.Header.Set("Cookie", "__Host-aethera_session="+cookie)
	}

	return e.send(t, req)
}

func (e *env) send(t *testing.T, req *http.Request) reply {
	t.Helper()

	res, err := e.app.Test(req)
	if err != nil {
		t.Fatalf("request failed %v", err)
	}

	defer func() { _ = res.Body.Close() }()

	raw, _ := io.ReadAll(res.Body)
	out := reply{status: res.StatusCode, header: res.Header}

	if len(raw) > 0 {
		if err := json.Unmarshal(raw, &out.body); err != nil {
			t.Fatalf("the body %q is not a JSON object %v", raw, err)
		}
	}

	for _, c := range res.Cookies() {
		if c.Name == "__Host-aethera_session" && c.MaxAge >= 0 && c.Value != "" {
			out.cookie = c.Value
		}
	}

	return out
}

func (e *env) register(t *testing.T, email string) {
	t.Helper()

	r := e.call(t, http.MethodPost, "/v1/auth/register",
		`{"businessName":"Toko Maju","email":"`+email+`","password":"kata sandi baik"}`, "")
	if r.status != http.StatusCreated {
		t.Fatalf("register answered %d %v", r.status, r.body)
	}
}

// signUp registers, verifies and returns the session cookie
func (e *env) signUp(t *testing.T, email string) string {
	t.Helper()

	e.register(t, email)

	r := e.call(t, http.MethodPost, "/v1/auth/verify-email", `{"token":"`+e.mail.token(t, e.mail.verify, email)+`"}`, "")
	if r.status != http.StatusOK || r.cookie == "" {
		t.Fatalf("verify answered %d %v", r.status, r.body)
	}

	return r.cookie
}

func code(r reply) any { return r.body["code"] }

func TestSignUpVerifySignInAndOut(t *testing.T) {
	e := newEnv(t)
	email := "budi@example.com"

	e.register(t, email)

	r := e.call(t, http.MethodPost, "/v1/auth/login", `{"email":"budi@example.com","password":"kata sandi baik"}`, "")
	if r.status != http.StatusForbidden || code(r) != "email_not_verified" {
		t.Fatalf("an unverified sign in answered %d %v", r.status, r.body)
	}

	if r.cookie != "" {
		t.Fatal("a cookie was set for an unverified account")
	}

	r = e.call(t, http.MethodPost, "/v1/auth/verify-email", `{"token":"`+e.mail.token(t, e.mail.verify, email)+`"}`, "")
	if r.status != http.StatusOK || r.cookie == "" || r.body["emailVerified"] != true || r.body["businessName"] != "Toko Maju" {
		t.Fatalf("verify answered %d %v", r.status, r.body)
	}

	session := r.cookie

	if r = e.call(t, http.MethodGet, "/v1/auth/me", "", session); r.status != http.StatusOK || r.body["email"] != email {
		t.Fatalf("me answered %d %v", r.status, r.body)
	}

	if r = e.call(t, http.MethodPost, "/v1/auth/logout", "", session); r.status != http.StatusNoContent {
		t.Fatalf("logout answered %d", r.status)
	}

	if r = e.call(t, http.MethodGet, "/v1/auth/me", "", session); r.status != http.StatusUnauthorized || code(r) != "unauthorized" {
		t.Fatalf("a session that ended answered %d %v", r.status, r.body)
	}

	if r = e.call(t, http.MethodPost, "/v1/auth/login", `{"email":"BUDI@example.com","password":"kata sandi baik"}`, ""); r.status != http.StatusOK || r.cookie == "" {
		t.Fatalf("sign in answered %d %v", r.status, r.body)
	}
}

func TestTheSessionCookieIsHardened(t *testing.T) {
	e := newEnv(t)
	e.register(t, "budi@example.com")

	req := httptest.NewRequest(http.MethodPost, "/v1/auth/verify-email",
		strings.NewReader(`{"token":"`+e.mail.token(t, e.mail.verify, "budi@example.com")+`"}`))
	req.Header.Set("Content-Type", "application/json")
	req.Header.Set("X-Requested-With", "aethera")

	res, err := e.app.Test(req)
	if err != nil {
		t.Fatal(err)
	}

	defer func() { _ = res.Body.Close() }()

	set := res.Header.Get("Set-Cookie")
	for _, want := range []string{"__Host-aethera_session=", "HttpOnly", "Secure", "SameSite=Lax", "Path=/"} {
		if !strings.Contains(set, want) {
			t.Errorf("the cookie %q lacks %q", set, want)
		}
	}

	if strings.Contains(strings.ToLower(set), "domain=") {
		t.Errorf("the cookie %q has a Domain", set)
	}

	if res.Header.Get("Cache-Control") != "private, no-store" {
		t.Errorf("Cache-Control is %q", res.Header.Get("Cache-Control"))
	}
}

func TestOnlyTheHashOfASessionIsStored(t *testing.T) {
	e := newEnv(t)
	session := e.signUp(t, "budi@example.com")

	var n int
	if err := e.pool.QueryRow(context.Background(),
		"SELECT count(*) FROM sessions WHERE encode(token_hash, 'base64') LIKE '%' || $1 || '%'", session).Scan(&n); err != nil || n != 0 {
		t.Fatalf("the raw token is in the table (%d rows, error %v)", n, err)
	}
}

func TestWrongPasswordAndUnknownEmailLookTheSame(t *testing.T) {
	e := newEnv(t)
	e.signUp(t, "budi@example.com")

	wrong := e.call(t, http.MethodPost, "/v1/auth/login", `{"email":"budi@example.com","password":"salah sekali"}`, "")
	unknown := e.call(t, http.MethodPost, "/v1/auth/login", `{"email":"nobody@example.com","password":"salah sekali"}`, "")

	if wrong.status != http.StatusUnauthorized || unknown.status != wrong.status || code(wrong) != "invalid_credentials" ||
		wrong.body["message"] != unknown.body["message"] {
		t.Fatalf("the answers differ, %d %v and %d %v", wrong.status, wrong.body, unknown.status, unknown.body)
	}
}

func TestSignUpRules(t *testing.T) {
	e := newEnv(t)
	e.register(t, "budi@example.com")

	r := e.call(t, http.MethodPost, "/v1/auth/register", `{"businessName":"Lain","email":"Budi@Example.com","password":"kata sandi baik"}`, "")
	if r.status != http.StatusConflict || code(r) != "email_taken" {
		t.Fatalf("a taken email answered %d %v", r.status, r.body)
	}

	r = e.call(t, http.MethodPost, "/v1/auth/register", `{"businessName":"  ","email":"bukan-email","password":"pendek"}`, "")
	fields, _ := r.body["fields"].(map[string]any)

	if r.status != http.StatusUnprocessableEntity || fields["businessName"] != "required" || fields["email"] != "invalid" || fields["password"] != "too_short" {
		t.Fatalf("validation answered %d %v", r.status, r.body)
	}

	r = e.call(t, http.MethodPost, "/v1/auth/register", `{"businessName":"X","email":"x@example.com","password":"kata sandi baik","admin":true}`, "")
	if r.status != http.StatusBadRequest || code(r) != "invalid_json" {
		t.Fatalf("an unknown field answered %d %v", r.status, r.body)
	}

	r = e.call(t, http.MethodPost, "/v1/auth/register", `{"businessName":"X"} {"a":1}`, "")
	if r.status != http.StatusBadRequest {
		t.Fatalf("two values answered %d", r.status)
	}
}

func TestVerificationTokens(t *testing.T) {
	e := newEnv(t)
	e.register(t, "budi@example.com")

	token := e.mail.token(t, e.mail.verify, "budi@example.com")

	if r := e.call(t, http.MethodPost, "/v1/auth/verify-email", `{"token":"short"}`, ""); r.status != http.StatusUnprocessableEntity {
		t.Fatalf("a short token answered %d", r.status)
	}

	if r := e.call(t, http.MethodPost, "/v1/auth/verify-email", `{"token":"`+strings.Repeat("a", 43)+`"}`, ""); r.status != http.StatusGone || code(r) != "token_invalid_or_expired" {
		t.Fatalf("an unknown token answered %d %v", r.status, r.body)
	}

	if r := e.call(t, http.MethodPost, "/v1/auth/verify-email", `{"token":"`+token+`"}`, ""); r.status != http.StatusOK {
		t.Fatalf("the token answered %d %v", r.status, r.body)
	}

	if r := e.call(t, http.MethodPost, "/v1/auth/verify-email", `{"token":"`+token+`"}`, ""); r.status != http.StatusGone {
		t.Fatalf("a used token answered %d, want 410", r.status)
	}

	e.register(t, "ani@example.com")

	if _, err := e.pool.Exec(context.Background(), "UPDATE email_tokens SET expires_at = now() - interval '1 second'"); err != nil {
		t.Fatal(err)
	}

	if r := e.call(t, http.MethodPost, "/v1/auth/verify-email", `{"token":"`+e.mail.token(t, e.mail.verify, "ani@example.com")+`"}`, ""); r.status != http.StatusGone {
		t.Fatalf("an expired token answered %d, want 410", r.status)
	}
}

func TestResendHasACooldownThatIsTheSameForEveryEmail(t *testing.T) {
	e := newEnv(t)
	e.register(t, "budi@example.com")

	first := e.mail.token(t, e.mail.verify, "budi@example.com")

	// the sign up did not count against the cooldown of the resend route
	if r := e.call(t, http.MethodPost, "/v1/auth/resend-verification", `{"email":"budi@example.com"}`, ""); r.status != http.StatusAccepted {
		t.Fatalf("the first resend answered %d %v", r.status, r.body)
	}

	if second := e.mail.token(t, e.mail.verify, "budi@example.com"); second == first {
		t.Fatal("the resend did not make a new token")
	}

	if r := e.call(t, http.MethodPost, "/v1/auth/verify-email", `{"token":"`+first+`"}`, ""); r.status != http.StatusGone {
		t.Fatalf("the older link answered %d, want 410", r.status)
	}

	for _, email := range []string{"budi@example.com", "nobody@example.com"} {
		r := e.call(t, http.MethodPost, "/v1/auth/resend-verification", `{"email":"`+email+`"}`, "")
		if email == "nobody@example.com" {
			// the first request for an unknown email is accepted, and only then the cooldown starts
			if r.status != http.StatusAccepted {
				t.Fatalf("an unknown email answered %d", r.status)
			}

			r = e.call(t, http.MethodPost, "/v1/auth/resend-verification", `{"email":"`+email+`"}`, "")
		}

		secs, _ := r.body["retryAfterSeconds"].(float64)
		if r.status != http.StatusTooManyRequests || code(r) != "resend_too_soon" || secs < 1 || secs > 60 || r.header.Get("Retry-After") == "" {
			t.Fatalf("the second resend for %s answered %d %v", email, r.status, r.body)
		}
	}
}

func TestForgotAndResetPassword(t *testing.T) {
	e := newEnv(t)
	session := e.signUp(t, "budi@example.com")

	for _, email := range []string{"budi@example.com", "nobody@example.com"} {
		if r := e.call(t, http.MethodPost, "/v1/auth/forgot-password", `{"email":"`+email+`"}`, ""); r.status != http.StatusAccepted {
			t.Fatalf("forgot for %s answered %d %v", email, r.status, r.body)
		}
	}

	if _, ok := e.mail.reset["nobody@example.com"]; ok {
		t.Fatal("a reset email went to an address without an account")
	}

	token := e.mail.token(t, e.mail.reset, "budi@example.com")

	if r := e.call(t, http.MethodPost, "/v1/auth/reset-password", `{"token":"`+token+`","password":"pendek"}`, ""); r.status != http.StatusUnprocessableEntity {
		t.Fatalf("a short password answered %d", r.status)
	}

	if r := e.call(t, http.MethodPost, "/v1/auth/reset-password", `{"token":"`+token+`","password":"kata sandi baru"}`, ""); r.status != http.StatusNoContent {
		t.Fatalf("reset answered %d %v", r.status, "")
	}

	if r := e.call(t, http.MethodPost, "/v1/auth/reset-password", `{"token":"`+token+`","password":"kata sandi lagi"}`, ""); r.status != http.StatusGone {
		t.Fatalf("a used reset token answered %d, want 410", r.status)
	}

	if r := e.call(t, http.MethodGet, "/v1/auth/me", "", session); r.status != http.StatusUnauthorized {
		t.Fatalf("the old session answered %d, a reset must end every session", r.status)
	}

	if r := e.call(t, http.MethodPost, "/v1/auth/login", `{"email":"budi@example.com","password":"kata sandi baik"}`, ""); r.status != http.StatusUnauthorized {
		t.Fatalf("the old password answered %d", r.status)
	}

	if r := e.call(t, http.MethodPost, "/v1/auth/login", `{"email":"budi@example.com","password":"kata sandi baru"}`, ""); r.status != http.StatusOK {
		t.Fatalf("the new password answered %d", r.status)
	}
}

func TestChangeEmailOnlyBeforeVerification(t *testing.T) {
	e := newEnv(t)
	e.register(t, "budi@example.com")

	old := e.mail.token(t, e.mail.verify, "budi@example.com")

	body := `{"email":"budi@example.com","password":"kata sandi baik","newEmail":"budi.baru@example.com"}`
	if r := e.call(t, http.MethodPost, "/v1/auth/change-email", `{"email":"budi@example.com","password":"salah","newEmail":"x@example.com"}`, ""); r.status != http.StatusUnauthorized {
		t.Fatalf("a wrong password answered %d", r.status)
	}

	if r := e.call(t, http.MethodPost, "/v1/auth/change-email", body, ""); r.status != http.StatusAccepted {
		t.Fatalf("change answered %d %v", r.status, r.body)
	}

	if r := e.call(t, http.MethodPost, "/v1/auth/verify-email", `{"token":"`+old+`"}`, ""); r.status != http.StatusGone {
		t.Fatalf("the link of the old email answered %d, want 410", r.status)
	}

	token := e.mail.token(t, e.mail.verify, "budi.baru@example.com")
	if r := e.call(t, http.MethodPost, "/v1/auth/verify-email", `{"token":"`+token+`"}`, ""); r.status != http.StatusOK || r.body["email"] != "budi.baru@example.com" {
		t.Fatalf("verify answered %d %v", r.status, r.body)
	}

	r := e.call(t, http.MethodPost, "/v1/auth/change-email", `{"email":"budi.baru@example.com","password":"kata sandi baik","newEmail":"lagi@example.com"}`, "")
	if r.status != http.StatusConflict || code(r) != "already_verified" {
		t.Fatalf("a verified account answered %d %v", r.status, r.body)
	}
}

func TestTooManySignInsAreStopped(t *testing.T) {
	e := newEnv(t)
	e.signUp(t, "budi@example.com")

	var last reply

	for range 12 {
		last = e.call(t, http.MethodPost, "/v1/auth/login", `{"email":"budi@example.com","password":"salah sekali"}`, "")
	}

	if last.status != http.StatusTooManyRequests || code(last) != "rate_limited" || last.header.Get("Retry-After") == "" {
		t.Fatalf("the twelfth try answered %d %v", last.status, last.body)
	}

	// even the right password waits, otherwise the limit would not slow a guess that happens to be right
	if r := e.call(t, http.MethodPost, "/v1/auth/login", `{"email":"budi@example.com","password":"kata sandi baik"}`, ""); r.status != http.StatusTooManyRequests {
		t.Fatalf("the right password answered %d during the limit", r.status)
	}
}

func TestASessionEndsWhenItIsIdleTooLong(t *testing.T) {
	e := newEnv(t)
	session := e.signUp(t, "budi@example.com")

	if _, err := e.pool.Exec(context.Background(), "UPDATE sessions SET last_used_at = now() - interval '31 days'"); err != nil {
		t.Fatal(err)
	}

	if r := e.call(t, http.MethodGet, "/v1/auth/me", "", session); r.status != http.StatusUnauthorized {
		t.Fatalf("an idle session answered %d", r.status)
	}

	session = e.signUp(t, "ani@example.com")

	if _, err := e.pool.Exec(context.Background(), "UPDATE sessions SET created_at = now() - interval '91 days' WHERE token_hash IN (SELECT token_hash FROM sessions ORDER BY created_at DESC LIMIT 1)"); err != nil {
		t.Fatal(err)
	}

	if r := e.call(t, http.MethodGet, "/v1/auth/me", "", session); r.status != http.StatusUnauthorized {
		t.Fatalf("a session older than 90 days answered %d", r.status)
	}
}

func TestRequestsThatChangeDataNeedTheHeaderAndTheOrigin(t *testing.T) {
	e := newEnv(t)

	post := func(setup func(*http.Request)) reply {
		req := httptest.NewRequest(http.MethodPost, "/v1/auth/login", strings.NewReader(`{"email":"a@example.com","password":"x"}`))
		req.Header.Set("Content-Type", "application/json")
		setup(req)

		return e.send(t, req)
	}

	if r := post(func(*http.Request) {}); r.status != http.StatusForbidden || code(r) != "csrf_header_missing" {
		t.Fatalf("no header answered %d %v", r.status, r.body)
	}

	if r := post(func(req *http.Request) {
		req.Header.Set("X-Requested-With", "aethera")
		req.Header.Set("Origin", "https://evil.example")
	}); r.status != http.StatusForbidden || code(r) != "origin_not_allowed" {
		t.Fatalf("a foreign origin answered %d %v", r.status, r.body)
	}

	if r := post(func(req *http.Request) { req.Header.Set("X-Requested-With", "aethera") }); r.status == http.StatusForbidden {
		t.Fatalf("a request with the header and no Origin was refused %v", r.body)
	}
}

func TestCORSAllowsOnlyTheApp(t *testing.T) {
	e := newEnv(t)

	preflight := func(from string) http.Header {
		req := httptest.NewRequest(http.MethodOptions, "/v1/auth/login", http.NoBody)
		req.Header.Set("Origin", from)
		req.Header.Set("Access-Control-Request-Method", "POST")
		req.Header.Set("Access-Control-Request-Headers", "content-type,x-requested-with")

		return e.send(t, req).header
	}

	h := preflight(origin)
	if h.Get("Access-Control-Allow-Origin") != origin || h.Get("Access-Control-Allow-Credentials") != "true" {
		t.Fatalf("the app origin got %v", h)
	}

	if h = preflight("https://evil.example"); h.Get("Access-Control-Allow-Origin") != "" {
		t.Fatalf("a foreign origin got %v", h)
	}
}

func TestMeNeedsASession(t *testing.T) {
	e := newEnv(t)

	for _, cookie := range []string{"", "nonsense", strings.Repeat("a", 500)} {
		if r := e.call(t, http.MethodGet, "/v1/auth/me", "", cookie); r.status != http.StatusUnauthorized {
			t.Fatalf("cookie %q answered %d", cookie, r.status)
		}
	}
}
