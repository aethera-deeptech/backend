package auth_test

import (
	"context"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"

	"github.com/aethera-deeptech/backend/internal/testutil"
)

func code(r testutil.Reply) any { return r.Code() }

func verifyToken(t *testing.T, e *testutil.Env, email string) string {
	t.Helper()

	link, ok := e.Mail.VerifyLink(email)

	return testutil.Token(t, link, ok)
}

func resetToken(t *testing.T, e *testutil.Env, email string) string {
	t.Helper()

	link, ok := e.Mail.ResetLink(email)

	return testutil.Token(t, link, ok)
}

func TestSignUpVerifySignInAndOut(t *testing.T) {
	e := testutil.New(t)
	email := "budi@example.com"

	e.Register(t, email)

	r := e.Call(t, http.MethodPost, "/v1/auth/login", `{"email":"budi@example.com","password":"kata sandi baik"}`, "")
	if r.Status != http.StatusForbidden || code(r) != "email_not_verified" {
		t.Fatalf("an unverified sign in answered %d %v", r.Status, r.Body)
	}

	if r.Cookie != "" {
		t.Fatal("a cookie was set for an unverified account")
	}

	r = e.Call(t, http.MethodPost, "/v1/auth/verify-email", `{"token":"`+verifyToken(t, e, email)+`"}`, "")
	if r.Status != http.StatusOK || r.Cookie == "" || r.Body["emailVerified"] != true || r.Body["businessName"] != "Toko Maju" {
		t.Fatalf("verify answered %d %v", r.Status, r.Body)
	}

	session := r.Cookie

	if r = e.Call(t, http.MethodGet, "/v1/auth/me", "", session); r.Status != http.StatusOK || r.Body["email"] != email {
		t.Fatalf("me answered %d %v", r.Status, r.Body)
	}

	if r = e.Call(t, http.MethodPost, "/v1/auth/logout", "", session); r.Status != http.StatusNoContent {
		t.Fatalf("logout answered %d", r.Status)
	}

	if r = e.Call(t, http.MethodGet, "/v1/auth/me", "", session); r.Status != http.StatusUnauthorized || code(r) != "unauthorized" {
		t.Fatalf("a session that ended answered %d %v", r.Status, r.Body)
	}

	if r = e.Call(t, http.MethodPost, "/v1/auth/login", `{"email":"BUDI@example.com","password":"kata sandi baik"}`, ""); r.Status != http.StatusOK || r.Cookie == "" {
		t.Fatalf("sign in answered %d %v", r.Status, r.Body)
	}
}

func TestTheSessionCookieIsHardened(t *testing.T) {
	e := testutil.New(t)
	e.Register(t, "budi@example.com")

	req := httptest.NewRequest(http.MethodPost, "/v1/auth/verify-email",
		strings.NewReader(`{"token":"`+verifyToken(t, e, "budi@example.com")+`"}`))
	req.Header.Set("Content-Type", "application/json")
	req.Header.Set("X-Requested-With", "aethera")

	res, err := e.App.Test(req)
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
	e := testutil.New(t)
	session := e.SignUp(t, "budi@example.com")

	var n int
	if err := e.Pool.QueryRow(context.Background(),
		"SELECT count(*) FROM sessions WHERE encode(token_hash, 'base64') LIKE '%' || $1 || '%'", session).Scan(&n); err != nil || n != 0 {
		t.Fatalf("the raw token is in the table (%d rows, error %v)", n, err)
	}
}

func TestWrongPasswordAndUnknownEmailLookTheSame(t *testing.T) {
	e := testutil.New(t)
	e.SignUp(t, "budi@example.com")

	wrong := e.Call(t, http.MethodPost, "/v1/auth/login", `{"email":"budi@example.com","password":"salah sekali"}`, "")
	unknown := e.Call(t, http.MethodPost, "/v1/auth/login", `{"email":"nobody@example.com","password":"salah sekali"}`, "")

	if wrong.Status != http.StatusUnauthorized || unknown.Status != wrong.Status || code(wrong) != "invalid_credentials" ||
		wrong.Body["message"] != unknown.Body["message"] {
		t.Fatalf("the answers differ, %d %v and %d %v", wrong.Status, wrong.Body, unknown.Status, unknown.Body)
	}
}

func TestSignUpRules(t *testing.T) {
	e := testutil.New(t)
	e.Register(t, "budi@example.com")

	r := e.Call(t, http.MethodPost, "/v1/auth/register", `{"businessName":"Lain","email":"Budi@Example.com","password":"kata sandi baik"}`, "")
	if r.Status != http.StatusConflict || code(r) != "email_taken" {
		t.Fatalf("a taken email answered %d %v", r.Status, r.Body)
	}

	r = e.Call(t, http.MethodPost, "/v1/auth/register", `{"businessName":"  ","email":"bukan-email","password":"pendek"}`, "")
	fields, _ := r.Body["fields"].(map[string]any)

	if r.Status != http.StatusUnprocessableEntity || fields["businessName"] != "required" || fields["email"] != "invalid" || fields["password"] != "too_short" {
		t.Fatalf("validation answered %d %v", r.Status, r.Body)
	}

	r = e.Call(t, http.MethodPost, "/v1/auth/register", `{"businessName":"X","email":"x@example.com","password":"kata sandi baik","admin":true}`, "")
	if r.Status != http.StatusBadRequest || code(r) != "invalid_json" {
		t.Fatalf("an unknown field answered %d %v", r.Status, r.Body)
	}

	r = e.Call(t, http.MethodPost, "/v1/auth/register", `{"businessName":"X"} {"a":1}`, "")
	if r.Status != http.StatusBadRequest {
		t.Fatalf("two values answered %d", r.Status)
	}
}

func TestVerificationTokens(t *testing.T) {
	e := testutil.New(t)
	e.Register(t, "budi@example.com")

	token := verifyToken(t, e, "budi@example.com")

	if r := e.Call(t, http.MethodPost, "/v1/auth/verify-email", `{"token":"short"}`, ""); r.Status != http.StatusUnprocessableEntity {
		t.Fatalf("a short token answered %d", r.Status)
	}

	if r := e.Call(t, http.MethodPost, "/v1/auth/verify-email", `{"token":"`+strings.Repeat("a", 43)+`"}`, ""); r.Status != http.StatusGone || code(r) != "token_invalid_or_expired" {
		t.Fatalf("an unknown token answered %d %v", r.Status, r.Body)
	}

	if r := e.Call(t, http.MethodPost, "/v1/auth/verify-email", `{"token":"`+token+`"}`, ""); r.Status != http.StatusOK {
		t.Fatalf("the token answered %d %v", r.Status, r.Body)
	}

	if r := e.Call(t, http.MethodPost, "/v1/auth/verify-email", `{"token":"`+token+`"}`, ""); r.Status != http.StatusGone {
		t.Fatalf("a used token answered %d, want 410", r.Status)
	}

	e.Register(t, "ani@example.com")

	if _, err := e.Pool.Exec(context.Background(), "UPDATE email_tokens SET expires_at = now() - interval '1 second'"); err != nil {
		t.Fatal(err)
	}

	if r := e.Call(t, http.MethodPost, "/v1/auth/verify-email", `{"token":"`+verifyToken(t, e, "ani@example.com")+`"}`, ""); r.Status != http.StatusGone {
		t.Fatalf("an expired token answered %d, want 410", r.Status)
	}
}

func TestResendHasACooldownThatIsTheSameForEveryEmail(t *testing.T) {
	e := testutil.New(t)
	e.Register(t, "budi@example.com")

	first := verifyToken(t, e, "budi@example.com")

	// the sign up did not count against the cooldown of the resend route
	if r := e.Call(t, http.MethodPost, "/v1/auth/resend-verification", `{"email":"budi@example.com"}`, ""); r.Status != http.StatusAccepted {
		t.Fatalf("the first resend answered %d %v", r.Status, r.Body)
	}

	if second := verifyToken(t, e, "budi@example.com"); second == first {
		t.Fatal("the resend did not make a new token")
	}

	if r := e.Call(t, http.MethodPost, "/v1/auth/verify-email", `{"token":"`+first+`"}`, ""); r.Status != http.StatusGone {
		t.Fatalf("the older link answered %d, want 410", r.Status)
	}

	for _, email := range []string{"budi@example.com", "nobody@example.com"} {
		r := e.Call(t, http.MethodPost, "/v1/auth/resend-verification", `{"email":"`+email+`"}`, "")
		if email == "nobody@example.com" {
			// the first request for an unknown email is accepted, and only then the cooldown starts
			if r.Status != http.StatusAccepted {
				t.Fatalf("an unknown email answered %d", r.Status)
			}

			r = e.Call(t, http.MethodPost, "/v1/auth/resend-verification", `{"email":"`+email+`"}`, "")
		}

		secs, _ := r.Body["retryAfterSeconds"].(float64)
		if r.Status != http.StatusTooManyRequests || code(r) != "resend_too_soon" || secs < 1 || secs > 60 || r.Header.Get("Retry-After") == "" {
			t.Fatalf("the second resend for %s answered %d %v", email, r.Status, r.Body)
		}
	}
}

func TestForgotAndResetPassword(t *testing.T) {
	e := testutil.New(t)
	session := e.SignUp(t, "budi@example.com")

	for _, email := range []string{"budi@example.com", "nobody@example.com"} {
		if r := e.Call(t, http.MethodPost, "/v1/auth/forgot-password", `{"email":"`+email+`"}`, ""); r.Status != http.StatusAccepted {
			t.Fatalf("forgot for %s answered %d %v", email, r.Status, r.Body)
		}
	}

	if _, ok := e.Mail.ResetLink("nobody@example.com"); ok {
		t.Fatal("a reset email went to an address without an account")
	}

	token := resetToken(t, e, "budi@example.com")

	if r := e.Call(t, http.MethodPost, "/v1/auth/reset-password", `{"token":"`+token+`","password":"pendek"}`, ""); r.Status != http.StatusUnprocessableEntity {
		t.Fatalf("a short password answered %d", r.Status)
	}

	if r := e.Call(t, http.MethodPost, "/v1/auth/reset-password", `{"token":"`+token+`","password":"kata sandi baru"}`, ""); r.Status != http.StatusNoContent {
		t.Fatalf("reset answered %d %v", r.Status, "")
	}

	if r := e.Call(t, http.MethodPost, "/v1/auth/reset-password", `{"token":"`+token+`","password":"kata sandi lagi"}`, ""); r.Status != http.StatusGone {
		t.Fatalf("a used reset token answered %d, want 410", r.Status)
	}

	if r := e.Call(t, http.MethodGet, "/v1/auth/me", "", session); r.Status != http.StatusUnauthorized {
		t.Fatalf("the old session answered %d, a reset must end every session", r.Status)
	}

	if r := e.Call(t, http.MethodPost, "/v1/auth/login", `{"email":"budi@example.com","password":"kata sandi baik"}`, ""); r.Status != http.StatusUnauthorized {
		t.Fatalf("the old password answered %d", r.Status)
	}

	if r := e.Call(t, http.MethodPost, "/v1/auth/login", `{"email":"budi@example.com","password":"kata sandi baru"}`, ""); r.Status != http.StatusOK {
		t.Fatalf("the new password answered %d", r.Status)
	}
}

func TestChangeEmailOnlyBeforeVerification(t *testing.T) {
	e := testutil.New(t)
	e.Register(t, "budi@example.com")

	old := verifyToken(t, e, "budi@example.com")

	body := `{"email":"budi@example.com","password":"kata sandi baik","newEmail":"budi.baru@example.com"}`
	if r := e.Call(t, http.MethodPost, "/v1/auth/change-email", `{"email":"budi@example.com","password":"salah","newEmail":"x@example.com"}`, ""); r.Status != http.StatusUnauthorized {
		t.Fatalf("a wrong password answered %d", r.Status)
	}

	if r := e.Call(t, http.MethodPost, "/v1/auth/change-email", body, ""); r.Status != http.StatusAccepted {
		t.Fatalf("change answered %d %v", r.Status, r.Body)
	}

	if r := e.Call(t, http.MethodPost, "/v1/auth/verify-email", `{"token":"`+old+`"}`, ""); r.Status != http.StatusGone {
		t.Fatalf("the link of the old email answered %d, want 410", r.Status)
	}

	token := verifyToken(t, e, "budi.baru@example.com")
	if r := e.Call(t, http.MethodPost, "/v1/auth/verify-email", `{"token":"`+token+`"}`, ""); r.Status != http.StatusOK || r.Body["email"] != "budi.baru@example.com" {
		t.Fatalf("verify answered %d %v", r.Status, r.Body)
	}

	r := e.Call(t, http.MethodPost, "/v1/auth/change-email", `{"email":"budi.baru@example.com","password":"kata sandi baik","newEmail":"lagi@example.com"}`, "")
	if r.Status != http.StatusConflict || code(r) != "already_verified" {
		t.Fatalf("a verified account answered %d %v", r.Status, r.Body)
	}
}

func TestTooManySignInsAreStopped(t *testing.T) {
	e := testutil.New(t)
	e.SignUp(t, "budi@example.com")

	var last testutil.Reply

	for range 12 {
		last = e.Call(t, http.MethodPost, "/v1/auth/login", `{"email":"budi@example.com","password":"salah sekali"}`, "")
	}

	if last.Status != http.StatusTooManyRequests || code(last) != "rate_limited" || last.Header.Get("Retry-After") == "" {
		t.Fatalf("the twelfth try answered %d %v", last.Status, last.Body)
	}

	// even the right password waits, otherwise the limit would not slow a guess that happens to be right
	if r := e.Call(t, http.MethodPost, "/v1/auth/login", `{"email":"budi@example.com","password":"kata sandi baik"}`, ""); r.Status != http.StatusTooManyRequests {
		t.Fatalf("the right password answered %d during the limit", r.Status)
	}
}

func TestASessionEndsWhenItIsIdleTooLong(t *testing.T) {
	e := testutil.New(t)
	session := e.SignUp(t, "budi@example.com")

	if _, err := e.Pool.Exec(context.Background(), "UPDATE sessions SET last_used_at = now() - interval '31 days'"); err != nil {
		t.Fatal(err)
	}

	if r := e.Call(t, http.MethodGet, "/v1/auth/me", "", session); r.Status != http.StatusUnauthorized {
		t.Fatalf("an idle session answered %d", r.Status)
	}

	session = e.SignUp(t, "ani@example.com")

	if _, err := e.Pool.Exec(context.Background(), "UPDATE sessions SET created_at = now() - interval '91 days' WHERE token_hash IN (SELECT token_hash FROM sessions ORDER BY created_at DESC LIMIT 1)"); err != nil {
		t.Fatal(err)
	}

	if r := e.Call(t, http.MethodGet, "/v1/auth/me", "", session); r.Status != http.StatusUnauthorized {
		t.Fatalf("a session older than 90 days answered %d", r.Status)
	}
}

func TestRequestsThatChangeDataNeedTheHeaderAndTheOrigin(t *testing.T) {
	e := testutil.New(t)

	post := func(setup func(*http.Request)) testutil.Reply {
		req := httptest.NewRequest(http.MethodPost, "/v1/auth/login", strings.NewReader(`{"email":"a@example.com","password":"x"}`))
		req.Header.Set("Content-Type", "application/json")
		setup(req)

		return e.Send(t, req)
	}

	if r := post(func(*http.Request) {}); r.Status != http.StatusForbidden || code(r) != "csrf_header_missing" {
		t.Fatalf("no header answered %d %v", r.Status, r.Body)
	}

	if r := post(func(req *http.Request) {
		req.Header.Set("X-Requested-With", "aethera")
		req.Header.Set("Origin", "https://evil.example")
	}); r.Status != http.StatusForbidden || code(r) != "origin_not_allowed" {
		t.Fatalf("a foreign origin answered %d %v", r.Status, r.Body)
	}

	if r := post(func(req *http.Request) { req.Header.Set("X-Requested-With", "aethera") }); r.Status == http.StatusForbidden {
		t.Fatalf("a request with the header and no Origin was refused %v", r.Body)
	}
}

func TestCORSAllowsOnlyTheApp(t *testing.T) {
	e := testutil.New(t)

	preflight := func(from string) http.Header {
		req := httptest.NewRequest(http.MethodOptions, "/v1/auth/login", http.NoBody)
		req.Header.Set("Origin", from)
		req.Header.Set("Access-Control-Request-Method", "POST")
		req.Header.Set("Access-Control-Request-Headers", "content-type,x-requested-with")

		return e.Send(t, req).Header
	}

	h := preflight(testutil.Origin)
	if h.Get("Access-Control-Allow-Origin") != testutil.Origin || h.Get("Access-Control-Allow-Credentials") != "true" {
		t.Fatalf("the app origin got %v", h)
	}

	if h = preflight("https://evil.example"); h.Get("Access-Control-Allow-Origin") != "" {
		t.Fatalf("a foreign origin got %v", h)
	}
}

func TestMeNeedsASession(t *testing.T) {
	e := testutil.New(t)

	for _, cookie := range []string{"", "nonsense", strings.Repeat("a", 500)} {
		if r := e.Call(t, http.MethodGet, "/v1/auth/me", "", cookie); r.Status != http.StatusUnauthorized {
			t.Fatalf("cookie %q answered %d", cookie, r.Status)
		}
	}
}
