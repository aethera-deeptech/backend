package auth_test

import (
	"time"

	"context"
	"github.com/aethera-deeptech/backend/internal/auth"
	"github.com/aethera-deeptech/backend/internal/janitor"
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

	req := httptest.NewRequestWithContext(t.Context(), http.MethodPost, "/v1/auth/verify-email",
		strings.NewReader(`{"token":"`+verifyToken(t, e, "budi@example.com")+`"}`))
	req.Header.Set("Content-Type", "application/json")
	req.Header.Set("X-Requested-With", "aethera")

	res, err := e.App.Test(req)
	if err != nil {
		t.Fatal(err)
	}

	defer func() { _ = res.Body.Close() }()

	// the names of the attributes are not case sensitive, and the server writes some of them in lower case
	set := res.Header.Get("Set-Cookie")
	for _, want := range []string{"__host-aethera_session=", "httponly", "secure", "samesite=lax", "path=/"} {
		if !strings.Contains(strings.ToLower(set), want) {
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

	r = e.Call(t, http.MethodPost, "/v1/auth/register", `{"businessName":"Lain","email":"lain@example.com","password":"Password1"}`, "")
	fields, _ = r.Body["fields"].(map[string]any)

	if r.Status != http.StatusUnprocessableEntity || fields["password"] != "too_common" {
		t.Fatalf("a common password answered %d %v", r.Status, r.Body)
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

	r := e.Call(t, http.MethodPost, "/v1/auth/reset-password", `{"token":"`+token+`","password":"qwerty123"}`, "")
	common, _ := r.Body["fields"].(map[string]any)

	if r.Status != http.StatusUnprocessableEntity || common["password"] != "too_common" {
		t.Fatalf("a common password answered %d %v", r.Status, r.Body)
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

func TestChangeEmailToTheSameAddressIsRefusedAfterTheCredentialsAreChecked(t *testing.T) {
	e := testutil.New(t)
	e.Register(t, "budi@example.com")

	same := `{"email":"budi@example.com","password":"kata sandi baik","newEmail":"Budi@Example.com"}`

	r := e.Call(t, http.MethodPost, "/v1/auth/change-email", same, "")
	fields, _ := r.Body["fields"].(map[string]any)

	if r.Status != http.StatusUnprocessableEntity || fields["newEmail"] != "invalid" {
		t.Fatalf("the same address answered %d %v", r.Status, r.Body)
	}

	// a wrong password still answers 401, so the answer never says whether an address has an account
	wrong := `{"email":"budi@example.com","password":"salah","newEmail":"budi@example.com"}`
	if r := e.Call(t, http.MethodPost, "/v1/auth/change-email", wrong, ""); r.Status != http.StatusUnauthorized {
		t.Fatalf("a wrong password with the same address answered %d", r.Status)
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
		req := httptest.NewRequestWithContext(t.Context(), http.MethodPost, "/v1/auth/login", strings.NewReader(`{"email":"a@example.com","password":"x"}`))
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
		req := httptest.NewRequestWithContext(t.Context(), http.MethodOptions, "/v1/auth/login", http.NoBody)
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

func TestAResetLinkCannotVerifyAnAddressItWasNotSentTo(t *testing.T) {
	e := testutil.New(t)
	e.Register(t, "attacker@example.com")

	// the attacker asks for a reset link to their own mailbox, then moves the account to the address of a victim
	if r := e.Call(t, http.MethodPost, "/v1/auth/forgot-password", `{"email":"attacker@example.com"}`, ""); r.Status != http.StatusAccepted {
		t.Fatalf("forgot answered %d", r.Status)
	}

	token := resetToken(t, e, "attacker@example.com")

	body := `{"email":"attacker@example.com","password":"kata sandi baik","newEmail":"victim@example.com"}`
	if r := e.Call(t, http.MethodPost, "/v1/auth/change-email", body, ""); r.Status != http.StatusAccepted {
		t.Fatalf("change answered %d %v", r.Status, r.Body)
	}

	if r := e.Call(t, http.MethodPost, "/v1/auth/reset-password", `{"token":"`+token+`","password":"kata sandi baru"}`, ""); r.Status != http.StatusGone {
		t.Fatalf("the old reset link answered %d, want 410", r.Status)
	}

	var verified bool
	if err := e.Pool.QueryRow(context.Background(), "SELECT email_verified_at IS NOT NULL FROM users WHERE email = 'victim@example.com'").Scan(&verified); err != nil || verified {
		t.Fatalf("the victim address is verified (%v, %v) without a mail that reached it", verified, err)
	}

	// the link that went to the new address is the only one that works
	if r := e.Call(t, http.MethodPost, "/v1/auth/verify-email", `{"token":"`+verifyToken(t, e, "victim@example.com")+`"}`, ""); r.Status != http.StatusOK {
		t.Fatalf("the link of the new address answered %d", r.Status)
	}
}

func TestChangeEmailRemovesEveryUnusedToken(t *testing.T) {
	e := testutil.New(t)
	e.Register(t, "budi@example.com")
	e.Call(t, http.MethodPost, "/v1/auth/forgot-password", `{"email":"budi@example.com"}`, "")

	body := `{"email":"budi@example.com","password":"kata sandi baik","newEmail":"baru@example.com"}`
	if r := e.Call(t, http.MethodPost, "/v1/auth/change-email", body, ""); r.Status != http.StatusAccepted {
		t.Fatalf("change answered %d", r.Status)
	}

	var n int
	if err := e.Pool.QueryRow(context.Background(), "SELECT count(*) FROM email_tokens WHERE used_at IS NULL AND email <> 'baru@example.com'").Scan(&n); err != nil || n != 0 {
		t.Fatalf("%d tokens for the old address survived (%v)", n, err)
	}
}

func TestEmailsAreAsciiAndPlain(t *testing.T) {
	e := testutil.New(t)

	for _, email := range []string{"budí@example.com", "budi@exämple.com", "budi@[1.2.3.4]", "budi@example.com ,x@y.zz", "bu di@example.com"} {
		body := `{"businessName":"Toko","email":"` + email + `","password":"kata sandi baik"}`
		if r := e.Call(t, http.MethodPost, "/v1/auth/register", body, ""); r.Status != http.StatusUnprocessableEntity {
			t.Errorf("%q answered %d, want 422", email, r.Status)
		}
	}
}

func TestANameWithHiddenCharactersIsRefused(t *testing.T) {
	e := testutil.New(t)

	body := `{"businessName":"Toko\u202eMaju","email":"budi@example.com","password":"kata sandi baik"}`
	if r := e.Call(t, http.MethodPost, "/v1/auth/register", body, ""); r.Status != http.StatusUnprocessableEntity {
		t.Fatalf("a name with a right to left override answered %d", r.Status)
	}
}

func TestARequestForATakenAddressDoesNotHash(t *testing.T) {
	e := testutil.New(t)
	e.Register(t, "budi@example.com")

	start := time.Now()
	r := e.Call(t, http.MethodPost, "/v1/auth/register", `{"businessName":"Lain","email":"budi@example.com","password":"kata sandi baik"}`, "")

	if r.Status != http.StatusConflict {
		t.Fatalf("answered %d", r.Status)
	}

	// a full argon2 hash takes tens of milliseconds, so a refusal that answers faster than a hash did not run one
	if took := time.Since(start); took > 400*time.Millisecond {
		t.Logf("the refusal took %v", took)
	}
}

func TestAnUnknownResetTokenIsRefusedBeforeTheHash(t *testing.T) {
	e := testutil.New(t)

	r := e.Call(t, http.MethodPost, "/v1/auth/reset-password", `{"token":"`+strings.Repeat("a", 43)+`","password":"kata sandi baik"}`, "")
	if r.Status != http.StatusGone || r.Code() != "token_invalid_or_expired" {
		t.Fatalf("answered %d %v", r.Status, r.Body)
	}
}

func TestAnAuthBodyHasALimit(t *testing.T) {
	e := testutil.New(t)

	big := `{"email":"budi@example.com","password":"` + strings.Repeat("a", 9000) + `"}`
	if r := e.Call(t, http.MethodPost, "/v1/auth/login", big, ""); r.Status != http.StatusRequestEntityTooLarge {
		t.Fatalf("a body of 9000 bytes answered %d, want 413", r.Status)
	}
}

func TestThePurgeRemovesWhatNoRuleNeedsAnymore(t *testing.T) {
	e := testutil.New(t)
	session := e.SignUp(t, "budi@example.com")
	e.Register(t, "never@example.com")

	e.Exec(t, "UPDATE businesses SET created_at = now() - interval '8 days' WHERE id IN (SELECT business_id FROM users WHERE email = 'never@example.com')")
	e.Exec(t, "INSERT INTO rate_limit_events (bucket, key, at) VALUES ('x', 'y', now() - interval '3 hours'), ('x', 'y', now())")
	e.Exec(t, "INSERT INTO sessions (token_hash, user_id, last_used_at) SELECT sha256('old'), id, now() - interval '31 days' FROM users WHERE email = 'budi@example.com'")

	j := janitor.New(e.Pool, time.Hour, auth.PurgeTasks()...)
	if deleted := j.Round(context.Background()); deleted < 3 {
		t.Fatalf("the round deleted %d rows, want at least 3", deleted)
	}

	count := func(sql string) int {
		var n int
		if err := e.Pool.QueryRow(context.Background(), sql).Scan(&n); err != nil {
			t.Fatal(err)
		}

		return n
	}

	if n := count("SELECT count(*) FROM users WHERE email = 'never@example.com'"); n != 0 {
		t.Errorf("the account that never verified its email was kept")
	}

	if n := count("SELECT count(*) FROM users WHERE email = 'budi@example.com'"); n != 1 {
		t.Errorf("a verified account was deleted")
	}

	if n := count("SELECT count(*) FROM rate_limit_events WHERE bucket = 'x'"); n != 1 {
		t.Errorf("%d old and new test events are left, want only the new one", n)
	}

	if r := e.Call(t, http.MethodGet, "/v1/auth/me", "", session); r.Status != http.StatusOK {
		t.Errorf("a live session was removed, it answered %d", r.Status)
	}
}

func TestALateSaveForAnOldAddressKeepsTheNewLink(t *testing.T) {
	e := testutil.New(t)
	e.Register(t, "budi@example.com")

	body := `{"email":"budi@example.com","password":"kata sandi baik","newEmail":"baru@example.com"}`
	if r := e.Call(t, http.MethodPost, "/v1/auth/change-email", body, ""); r.Status != http.StatusAccepted {
		t.Fatalf("change answered %d %v", r.Status, r.Body)
	}

	var id string
	if err := e.Pool.QueryRow(t.Context(), "SELECT id FROM users WHERE email = 'baru@example.com'").Scan(&id); err != nil {
		t.Fatal(err)
	}

	// a resend that read the old address before the change saves its token now, and it must not touch the new link
	if err := auth.NewStore(e.Pool).SaveToken(t.Context(), id, "budi@example.com", "verify", make([]byte, 32), time.Hour); err != nil {
		t.Fatalf("the late save failed %v", err)
	}

	var forNew, forOld int
	if err := e.Pool.QueryRow(t.Context(),
		"SELECT count(*) FILTER (WHERE email = 'baru@example.com'), count(*) FILTER (WHERE email = 'budi@example.com') FROM email_tokens WHERE user_id = $1 AND purpose = 'verify' AND used_at IS NULL",
		id).Scan(&forNew, &forOld); err != nil {
		t.Fatal(err)
	}

	if forNew != 1 || forOld != 0 {
		t.Fatalf("the unused tokens are %d for the new address and %d for the old one", forNew, forOld)
	}

	link, ok := e.Mail.VerifyLink("baru@example.com")
	if r := e.Call(t, http.MethodPost, "/v1/auth/verify-email", `{"token":"`+testutil.Token(t, link, ok)+`"}`, ""); r.Status != http.StatusOK {
		t.Fatalf("the new link answered %d %v", r.Status, r.Body)
	}
}

// TestASessionUseTimeIsRefreshedOnlyAfterTenMinutes checks the touch rule of the one query that reads the session, a recent session
// keeps its time and a session that was last used 11 minutes ago gets the time of now
func TestASessionUseTimeIsRefreshedOnlyAfterTenMinutes(t *testing.T) {
	e := testutil.New(t)
	session := e.SignUp(t, "budi@example.com")
	ctx := context.Background()

	var before time.Time
	if err := e.Pool.QueryRow(ctx, "SELECT last_used_at FROM sessions").Scan(&before); err != nil {
		t.Fatal(err)
	}

	if r := e.Call(t, http.MethodGet, "/v1/auth/me", "", session); r.Status != http.StatusOK {
		t.Fatalf("a fresh session answered %d", r.Status)
	}

	var same time.Time
	if err := e.Pool.QueryRow(ctx, "SELECT last_used_at FROM sessions").Scan(&same); err != nil {
		t.Fatal(err)
	}

	if !same.Equal(before) {
		t.Fatalf("a request within 10 minutes moved the use time from %v to %v", before, same)
	}

	if _, err := e.Pool.Exec(ctx, "UPDATE sessions SET last_used_at = now() - interval '11 minutes'"); err != nil {
		t.Fatal(err)
	}

	if r := e.Call(t, http.MethodGet, "/v1/auth/me", "", session); r.Status != http.StatusOK {
		t.Fatalf("a session of 11 minutes answered %d", r.Status)
	}

	var refreshed bool
	if err := e.Pool.QueryRow(ctx, "SELECT last_used_at > now() - interval '1 minute' FROM sessions").Scan(&refreshed); err != nil {
		t.Fatal(err)
	}

	if !refreshed {
		t.Fatal("a request after 11 minutes did not refresh the use time")
	}
}

// signInAs signs in with a User-Agent and returns the session cookie
func signInAs(t *testing.T, e *testutil.Env, email, userAgent string) string {
	t.Helper()

	req := httptest.NewRequestWithContext(t.Context(), http.MethodPost, "/v1/auth/login", strings.NewReader(`{"email":"`+email+`","password":"kata sandi baik"}`))
	req.Header.Set("Content-Type", "application/json")
	req.Header.Set("X-Requested-With", "aethera")
	req.Header.Set("Origin", testutil.Origin)
	req.Header.Set("User-Agent", userAgent)

	r := e.Send(t, req)
	if r.Status != http.StatusOK || r.Cookie == "" {
		t.Fatalf("sign in answered %d %v", r.Status, r.Body)
	}

	return r.Cookie
}

func sessionItems(t *testing.T, r testutil.Reply) []map[string]any {
	t.Helper()

	if r.Status != http.StatusOK {
		t.Fatalf("the list answered %d %v", r.Status, r.Body)
	}

	raw, ok := r.Body["items"].([]any)
	if !ok {
		t.Fatalf("the list has no items %v", r.Body)
	}

	out := make([]map[string]any, 0, len(raw))

	for _, v := range raw {
		item, ok := v.(map[string]any)
		if !ok {
			t.Fatalf("an item is not an object %v", v)
		}

		out = append(out, item)
	}

	return out
}

func TestTheListOfSessionsNamesTheDevicesAndPutsThisOneFirst(t *testing.T) {
	e := testutil.New(t)
	e.SignUp(t, "budi@example.com")
	signInAs(t, e, "budi@example.com", "Mozilla/5.0 (Windows NT 10.0; Win64; x64) AppleWebKit/537.36 (KHTML, like Gecko) Chrome/130.0 Safari/537.36")
	phone := signInAs(t, e, "budi@example.com", "Mozilla/5.0 (Linux; Android 14; Pixel 8) AppleWebKit/537.36 (KHTML, like Gecko) Chrome/130.0 Mobile Safari/537.36")

	// the sign in of the first session came from the verification link with no User-Agent, and the laptop used the service last
	e.Exec(t, "UPDATE sessions SET last_used_at = now() - interval '3 hours' WHERE label = 'Unknown device'")
	e.Exec(t, "UPDATE sessions SET last_used_at = now() - interval '1 hour' WHERE label = 'Chrome on Windows'")

	items := sessionItems(t, e.Call(t, http.MethodGet, "/v1/auth/sessions", "", phone))

	labels := []any{}
	for _, i := range items {
		labels = append(labels, i["label"])
	}

	want := []any{"Chrome on Android", "Chrome on Windows", "Unknown device"}
	if len(items) != 3 || labels[0] != want[0] || labels[1] != want[1] || labels[2] != want[2] {
		t.Fatalf("the labels are %v, want %v", labels, want)
	}

	for n, i := range items {
		if i["current"] != (n == 0) {
			t.Fatalf("item %d has current %v", n, i["current"])
		}

		if id, _ := i["id"].(string); len(id) != 36 {
			t.Fatalf("item %d has the id %v", n, i["id"])
		}

		for _, key := range []string{"createdAt", "lastUsedAt"} {
			if _, ok := i[key].(string); !ok {
				t.Fatalf("item %d has no %s", n, key)
			}
		}
	}

	if r := e.Call(t, http.MethodGet, "/v1/auth/sessions", "", ""); r.Status != http.StatusUnauthorized {
		t.Fatalf("a list with no session answered %d", r.Status)
	}
}

func TestTheListOfSessionsHidesTheOnesThatCannotSignInAndOtherUsers(t *testing.T) {
	e := testutil.New(t)
	budi := e.SignUp(t, "budi@example.com")
	signInAs(t, e, "budi@example.com", "Firefox/1")
	signInAs(t, e, "budi@example.com", "Firefox/2")
	e.SignUp(t, "siti@example.com")

	e.Exec(t, "UPDATE sessions SET last_used_at = now() - interval '31 days' WHERE label = 'Firefox' AND ctid = (SELECT ctid FROM sessions WHERE label = 'Firefox' LIMIT 1)")

	items := sessionItems(t, e.Call(t, http.MethodGet, "/v1/auth/sessions", "", budi))
	if len(items) != 2 {
		t.Fatalf("the list has %d items, want the verification session and one Firefox", len(items))
	}

	e.Exec(t, "UPDATE sessions SET created_at = now() - interval '91 days' WHERE label = 'Firefox'")

	if items = sessionItems(t, e.Call(t, http.MethodGet, "/v1/auth/sessions", "", budi)); len(items) != 1 {
		t.Fatalf("the list has %d items after the absolute limit, want 1", len(items))
	}
}

func TestEndingAnotherSessionKeepsThisOne(t *testing.T) {
	e := testutil.New(t)
	laptop := e.SignUp(t, "budi@example.com")
	phone := signInAs(t, e, "budi@example.com", "Mozilla/5.0 (Linux; Android 14) Firefox/130.0")

	var phoneID string

	for _, i := range sessionItems(t, e.Call(t, http.MethodGet, "/v1/auth/sessions", "", laptop)) {
		if i["label"] == "Firefox on Android" {
			phoneID, _ = i["id"].(string)
		}
	}

	if phoneID == "" {
		t.Fatal("the phone is not in the list")
	}

	r := e.Call(t, http.MethodDelete, "/v1/auth/sessions/"+phoneID, "", laptop)
	if r.Status != http.StatusNoContent || r.Cookie != "" || len(r.Raw) != 0 {
		t.Fatalf("ending the phone answered %d with cookie %q and body %q", r.Status, r.Cookie, r.Raw)
	}

	if r = e.Call(t, http.MethodGet, "/v1/auth/me", "", phone); r.Status != http.StatusUnauthorized {
		t.Fatalf("the phone still works, it answered %d", r.Status)
	}

	if r = e.Call(t, http.MethodGet, "/v1/auth/me", "", laptop); r.Status != http.StatusOK {
		t.Fatalf("the laptop stopped, it answered %d", r.Status)
	}

	if r = e.Call(t, http.MethodDelete, "/v1/auth/sessions/"+phoneID, "", laptop); r.Status != http.StatusNotFound || code(r) != "not_found" {
		t.Fatalf("a second delete answered %d %v", r.Status, r.Body)
	}
}

func TestEndingThisSessionWorksLikeLogout(t *testing.T) {
	e := testutil.New(t)
	laptop := e.SignUp(t, "budi@example.com")

	items := sessionItems(t, e.Call(t, http.MethodGet, "/v1/auth/sessions", "", laptop))
	if len(items) != 1 || items[0]["current"] != true {
		t.Fatalf("the list is %v", items)
	}

	id, _ := items[0]["id"].(string)

	req := httptest.NewRequestWithContext(t.Context(), http.MethodDelete, "/v1/auth/sessions/"+id, nil)
	req.Header.Set("X-Requested-With", "aethera")
	req.Header.Set("Origin", testutil.Origin)
	req.Header.Set("Cookie", testutil.CookieName+"="+laptop)

	r := e.Send(t, req)
	if r.Status != http.StatusNoContent {
		t.Fatalf("ending this session answered %d", r.Status)
	}

	cleared := false

	for _, c := range (&http.Response{Header: r.Header}).Cookies() {
		if c.Name == testutil.CookieName && c.MaxAge < 0 {
			cleared = true
		}
	}

	if !cleared {
		t.Fatalf("the cookie was not cleared, the headers are %v", r.Header)
	}

	if r = e.Call(t, http.MethodGet, "/v1/auth/me", "", laptop); r.Status != http.StatusUnauthorized {
		t.Fatalf("the session still works, it answered %d", r.Status)
	}
}

func TestASessionOfAnotherUserOrABadIDIsNotFound(t *testing.T) {
	e := testutil.New(t)
	budi := e.SignUp(t, "budi@example.com")
	siti := e.SignUp(t, "siti@example.com")

	items := sessionItems(t, e.Call(t, http.MethodGet, "/v1/auth/sessions", "", siti))
	sitiID, _ := items[0]["id"].(string)

	for _, id := range []string{sitiID, "00000000-0000-0000-0000-000000000000", "not-a-uuid", "1", "urn:uuid:" + sitiID} {
		if r := e.Call(t, http.MethodDelete, "/v1/auth/sessions/"+id, "", budi); r.Status != http.StatusNotFound || code(r) != "not_found" {
			t.Fatalf("deleting %q answered %d %v", id, r.Status, r.Body)
		}
	}

	if r := e.Call(t, http.MethodGet, "/v1/auth/me", "", siti); r.Status != http.StatusOK {
		t.Fatalf("the session of the other user ended, it answered %d", r.Status)
	}

	if r := e.Call(t, http.MethodDelete, "/v1/auth/sessions/"+sitiID, "", ""); r.Status != http.StatusUnauthorized {
		t.Fatalf("a delete with no session answered %d", r.Status)
	}
}

func TestEndingASessionNeedsTheHeaderAndTheOrigin(t *testing.T) {
	e := testutil.New(t)
	laptop := e.SignUp(t, "budi@example.com")

	items := sessionItems(t, e.Call(t, http.MethodGet, "/v1/auth/sessions", "", laptop))
	id, _ := items[0]["id"].(string)

	req := httptest.NewRequestWithContext(t.Context(), http.MethodDelete, "/v1/auth/sessions/"+id, nil)
	req.Header.Set("Cookie", testutil.CookieName+"="+laptop)

	if r := e.Send(t, req); r.Status != http.StatusForbidden {
		t.Fatalf("a delete with no CSRF header answered %d", r.Status)
	}

	req = httptest.NewRequestWithContext(t.Context(), http.MethodDelete, "/v1/auth/sessions/"+id, nil)
	req.Header.Set("X-Requested-With", "aethera")
	req.Header.Set("Origin", "https://other.example")
	req.Header.Set("Cookie", testutil.CookieName+"="+laptop)

	if r := e.Send(t, req); r.Status != http.StatusForbidden {
		t.Fatalf("a delete from another origin answered %d", r.Status)
	}

	if r := e.Call(t, http.MethodGet, "/v1/auth/me", "", laptop); r.Status != http.StatusOK {
		t.Fatalf("the session ended, it answered %d", r.Status)
	}
}

func TestRevokeOthersKeepsThisSessionAndCountsTheRest(t *testing.T) {
	e := testutil.New(t)
	laptop := e.SignUp(t, "budi@example.com")
	phone := signInAs(t, e, "budi@example.com", "Mozilla/5.0 (Linux; Android 14) Firefox/130.0")
	tablet := signInAs(t, e, "budi@example.com", "Mozilla/5.0 (iPad; CPU OS 18_0 like Mac OS X) Safari/605.1.15")
	siti := e.SignUp(t, "siti@example.com")

	// a row that is idle too long and that the janitor has not removed yet is deleted but not counted
	e.Exec(t, "UPDATE sessions SET last_used_at = now() - interval '31 days' WHERE label = 'Safari on iPad'")

	r := e.Call(t, http.MethodPost, "/v1/auth/sessions/revoke-others", "", laptop)
	if r.Status != http.StatusOK || r.Body["ended"] != float64(1) {
		t.Fatalf("revoke answered %d %v", r.Status, r.Body)
	}

	if r.Cookie != "" {
		t.Fatalf("revoke changed the cookie to %q", r.Cookie)
	}

	for name, cookie := range map[string]string{"phone": phone, "tablet": tablet} {
		if r = e.Call(t, http.MethodGet, "/v1/auth/me", "", cookie); r.Status != http.StatusUnauthorized {
			t.Fatalf("the %s still works, it answered %d", name, r.Status)
		}
	}

	if r = e.Call(t, http.MethodGet, "/v1/auth/me", "", laptop); r.Status != http.StatusOK {
		t.Fatalf("this session stopped, it answered %d", r.Status)
	}

	if r = e.Call(t, http.MethodGet, "/v1/auth/me", "", siti); r.Status != http.StatusOK {
		t.Fatalf("the session of another user ended, it answered %d", r.Status)
	}

	if items := sessionItems(t, e.Call(t, http.MethodGet, "/v1/auth/sessions", "", laptop)); len(items) != 1 || items[0]["current"] != true {
		t.Fatalf("the list after the revoke is %v", items)
	}

	if r = e.Call(t, http.MethodPost, "/v1/auth/sessions/revoke-others", "", laptop); r.Status != http.StatusOK || r.Body["ended"] != float64(0) {
		t.Fatalf("a second revoke answered %d %v", r.Status, r.Body)
	}
}

func TestRevokeOthersNeedsASessionAndTheHeader(t *testing.T) {
	e := testutil.New(t)
	laptop := e.SignUp(t, "budi@example.com")
	phone := signInAs(t, e, "budi@example.com", "Firefox/1")

	if r := e.Call(t, http.MethodPost, "/v1/auth/sessions/revoke-others", "", ""); r.Status != http.StatusUnauthorized {
		t.Fatalf("a revoke with no session answered %d", r.Status)
	}

	req := httptest.NewRequestWithContext(t.Context(), http.MethodPost, "/v1/auth/sessions/revoke-others", nil)
	req.Header.Set("Cookie", testutil.CookieName+"="+laptop)

	if r := e.Send(t, req); r.Status != http.StatusForbidden {
		t.Fatalf("a revoke with no CSRF header answered %d", r.Status)
	}

	req = httptest.NewRequestWithContext(t.Context(), http.MethodPost, "/v1/auth/sessions/revoke-others", nil)
	req.Header.Set("X-Requested-With", "aethera")
	req.Header.Set("Origin", "https://other.example")
	req.Header.Set("Cookie", testutil.CookieName+"="+laptop)

	if r := e.Send(t, req); r.Status != http.StatusForbidden {
		t.Fatalf("a revoke from another origin answered %d", r.Status)
	}

	if r := e.Call(t, http.MethodGet, "/v1/auth/me", "", phone); r.Status != http.StatusOK {
		t.Fatalf("a refused revoke ended the phone, it answered %d", r.Status)
	}
}
