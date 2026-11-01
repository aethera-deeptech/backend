// Package auth holds the accounts of the service, which are the sign up, the email verification, the sign in with a password, the
// sessions and the reset of a password
package auth

import (
	"context"
	"errors"
	"fmt"
	"net/http"
	"net/mail"
	"net/url"
	"strings"
	"time"
	"unicode"
	"unicode/utf8"

	"github.com/aethera-deeptech/backend/internal/api"
)

// consentVersion names the text of the terms and of the privacy policy that a person accepts by signing up
const consentVersion = "2026-11"

// resendCooldown is the time before an email can be asked for again
const resendCooldown = 60 * time.Second

const (
	minPasswordRunes = 8
	maxPasswordRunes = 128
	maxNameRunes     = 80
	maxEmailBytes    = 254
	hashWait         = 3 * time.Second
)

// Mailer queues the emails of the account routes and returns at once
type Mailer interface {
	Verification(to, link string)
	PasswordReset(to, link string)
}

// The limits of the account routes. A key is an address of a client or an email.
var (
	ruleRegisterIP    = Rule{Bucket: "register-ip", Max: 10, Window: time.Hour}
	ruleRegisterEmail = Rule{Bucket: "register-email", Max: 5, Window: time.Hour}
	ruleLoginIP       = Rule{Bucket: "login-ip", Max: 40, Window: 15 * time.Minute}
	ruleLoginEmail    = Rule{Bucket: "login-email", Max: 10, Window: 15 * time.Minute}
	ruleTokenIP       = Rule{Bucket: "token-ip", Max: 30, Window: 15 * time.Minute}
	ruleResendEmail   = Rule{Bucket: "resend-email", Max: 1, Window: resendCooldown}
	ruleResendIP      = Rule{Bucket: "resend-ip", Max: 20, Window: time.Hour}
	ruleForgotEmail   = Rule{Bucket: "forgot-email", Max: 1, Window: resendCooldown}
	ruleForgotIP      = Rule{Bucket: "forgot-ip", Max: 20, Window: time.Hour}
	ruleChangeIP      = Rule{Bucket: "change-ip", Max: 10, Window: time.Hour}
	ruleChangeEmail   = Rule{Bucket: "change-email", Max: 5, Window: time.Hour}
)

// Service holds the rules of the accounts
type Service struct {
	store     *Store
	limiter   *Limiter
	hasher    *Hasher
	mailer    Mailer
	appOrigin string
}

// NewService returns the service. The origin is the one of the web app, which the links in the emails point to.
func NewService(store *Store, limiter *Limiter, hasher *Hasher, mailer Mailer, appOrigin string) *Service {
	return &Service{store: store, limiter: limiter, hasher: hasher, mailer: mailer, appOrigin: strings.TrimRight(appOrigin, "/")}
}

// Me is what the app reads about the signed in user
type Me struct {
	Email         string  `json:"email"`
	EmailVerified bool    `json:"emailVerified"`
	BusinessName  *string `json:"businessName"`
}

func meOf(u *User) Me {
	return Me{Email: u.Email, EmailVerified: u.Verified, BusinessName: u.BusinessName}
}

// normalizeEmail lowers and trims an email and reports whether it is a plain address with a domain that has a dot
func normalizeEmail(raw string) (string, bool) {
	email := strings.ToLower(strings.TrimSpace(raw))
	if email == "" || len(email) > maxEmailBytes {
		return "", false
	}

	addr, err := mail.ParseAddress(email)
	if err != nil || addr.Address != email || addr.Name != "" {
		return "", false
	}

	_, domain, ok := strings.Cut(email, "@")
	if !ok || !strings.Contains(domain, ".") || strings.HasSuffix(domain, ".") {
		return "", false
	}

	return email, true
}

func validPassword(p string) (string, bool) {
	n := utf8.RuneCountInString(p)
	switch {
	case p == "":
		return "required", false
	case n < minPasswordRunes:
		return "too_short", false
	case n > maxPasswordRunes:
		return "too_long", false
	default:
		return "", true
	}
}

// cleanName trims a name and checks its length and that it has no control characters
func cleanName(raw string, maxRunes int) (string, string) {
	name := strings.TrimSpace(raw)

	switch {
	case name == "":
		return "", "required"
	case utf8.RuneCountInString(name) > maxRunes:
		return "", "too_long"
	case strings.ContainsFunc(name, unicode.IsControl):
		return "", "invalid"
	default:
		return name, ""
	}
}

// allow checks one rule and returns the 429 error when the key is over it
func (s *Service) allow(ctx context.Context, rule Rule, key, code string) error {
	ok, wait, err := s.limiter.Allow(ctx, rule, key)
	if err != nil {
		return err
	}

	if !ok {
		return api.RateLimited(code, int(wait.Seconds()+0.5))
	}

	return nil
}

func (s *Service) hashContext(ctx context.Context) (context.Context, context.CancelFunc) {
	return context.WithTimeout(ctx, hashWait)
}

func (s *Service) link(path, token string) string {
	return s.appOrigin + path + "?token=" + url.QueryEscape(token)
}

// sendVerification stores a new verification token for the user and queues the email
func (s *Service) sendVerification(ctx context.Context, userID, email string) error {
	raw, hash, err := newToken()
	if err != nil {
		return err
	}

	if err := s.store.SaveToken(ctx, userID, purposeVerify, hash, verifyTTL); err != nil {
		return err
	}

	s.mailer.Verification(email, s.link("/verify-email", raw))

	return nil
}

// Register makes the business and its owner and sends the verification email
func (s *Service) Register(ctx context.Context, ip, businessName, rawEmail, password string) error {
	fields := map[string]string{}

	name, code := cleanName(businessName, maxNameRunes)
	if code != "" {
		fields["businessName"] = code
	}

	email, ok := normalizeEmail(rawEmail)
	if !ok {
		fields["email"] = "invalid"
		if strings.TrimSpace(rawEmail) == "" {
			fields["email"] = "required"
		}
	}

	if code, ok := validPassword(password); !ok {
		fields["password"] = code
	}

	if len(fields) > 0 {
		return api.Validation(fields)
	}

	if err := s.allow(ctx, ruleRegisterIP, ip, ""); err != nil {
		return err
	}

	if err := s.allow(ctx, ruleRegisterEmail, email, ""); err != nil {
		return err
	}

	hctx, cancel := s.hashContext(ctx)
	defer cancel()

	hash, err := s.hasher.Hash(hctx, password)
	if err != nil {
		return busyOr(err)
	}

	userID, err := s.store.CreateAccount(ctx, name, email, hash, consentVersion)
	if errors.Is(err, ErrEmailTaken) {
		return api.NewError(http.StatusConflict, "email_taken", "The email has an account")
	}

	if err != nil {
		return err
	}

	return s.sendVerification(ctx, userID, email)
}

func busyOr(err error) error {
	if errors.Is(err, ErrBusy) {
		e := api.NewError(http.StatusServiceUnavailable, "busy", "The service is busy, try again")
		e.RetryAfterSeconds = 2

		return e
	}

	return err
}

// Login checks the email and the password and starts a session. It returns the session token for the cookie.
func (s *Service) Login(ctx context.Context, ip, rawEmail, password string) (string, Me, error) {
	email, ok := normalizeEmail(rawEmail)
	if !ok || password == "" {
		fields := map[string]string{}
		if !ok {
			fields["email"] = "invalid"
		}

		if password == "" {
			fields["password"] = "required"
		}

		return "", Me{}, api.Validation(fields)
	}

	if err := s.allow(ctx, ruleLoginIP, ip, ""); err != nil {
		return "", Me{}, err
	}

	if err := s.allow(ctx, ruleLoginEmail, email, ""); err != nil {
		return "", Me{}, err
	}

	user, err := s.store.UserByEmail(ctx, email)
	if err != nil {
		return "", Me{}, err
	}

	hctx, cancel := s.hashContext(ctx)
	defer cancel()

	matches := false

	if user != nil && user.PasswordHash != nil {
		matches, err = s.hasher.Verify(hctx, password, *user.PasswordHash)
		if err != nil {
			return "", Me{}, busyOr(err)
		}
	} else if err := s.hasher.VerifyDummy(hctx); err != nil {
		return "", Me{}, busyOr(err)
	}

	if !matches {
		return "", Me{}, api.NewError(http.StatusUnauthorized, "invalid_credentials", "The email or the password is wrong")
	}

	if !user.Verified {
		return "", Me{}, api.NewError(http.StatusForbidden, "email_not_verified", "The email is not verified")
	}

	token, err := s.startSession(ctx, user.ID)
	if err != nil {
		return "", Me{}, err
	}

	return token, meOf(user), nil
}

func (s *Service) startSession(ctx context.Context, userID string) (string, error) {
	raw, hash, err := newToken()
	if err != nil {
		return "", err
	}

	if err := s.store.CreateSession(ctx, userID, hash); err != nil {
		return "", err
	}

	return raw, nil
}

// Authenticate returns the user of a session token
func (s *Service) Authenticate(ctx context.Context, token string) (*User, error) {
	if token == "" || len(token) > 128 {
		return nil, ErrNoSession
	}

	return s.store.SessionUser(ctx, hashToken(token))
}

// Logout ends the session of a token
func (s *Service) Logout(ctx context.Context, token string) error {
	return s.store.DeleteSession(ctx, hashToken(token))
}

func validToken(t string) bool { return len(t) >= 32 && len(t) <= 128 }

// VerifyEmail uses the token of the link, verifies the email and starts a session
func (s *Service) VerifyEmail(ctx context.Context, ip, token string) (string, Me, error) {
	if !validToken(token) {
		return "", Me{}, api.Validation(map[string]string{"token": "invalid"})
	}

	if err := s.allow(ctx, ruleTokenIP, ip, ""); err != nil {
		return "", Me{}, err
	}

	userID, err := s.store.VerifyEmail(ctx, hashToken(token))
	if errors.Is(err, ErrTokenInvalid) {
		return "", Me{}, tokenInvalid()
	}

	if err != nil {
		return "", Me{}, err
	}

	user, err := s.store.UserByID(ctx, userID)
	if err != nil {
		return "", Me{}, err
	}

	session, err := s.startSession(ctx, userID)
	if err != nil {
		return "", Me{}, err
	}

	return session, meOf(user), nil
}

func tokenInvalid() error {
	return api.NewError(http.StatusGone, "token_invalid_or_expired", "The link is unknown, used or expired")
}

// ResendVerification sends the verification email again. The answer is the same for every email.
func (s *Service) ResendVerification(ctx context.Context, ip, rawEmail string) error {
	email, ok := normalizeEmail(rawEmail)
	if !ok {
		return api.Validation(map[string]string{"email": "invalid"})
	}

	if err := s.allow(ctx, ruleResendIP, ip, ""); err != nil {
		return err
	}

	// the cooldown counts for every email, so a refusal does not tell which emails have an account
	if err := s.allow(ctx, ruleResendEmail, email, "resend_too_soon"); err != nil {
		return err
	}

	user, err := s.store.UserByEmail(ctx, email)
	if err != nil || user == nil || user.Verified {
		return err
	}

	return s.sendVerification(ctx, user.ID, email)
}

// ChangeEmail moves an account that is not verified to a new email and sends the verification email there
func (s *Service) ChangeEmail(ctx context.Context, ip, rawEmail, password, rawNew string) error {
	email, ok := normalizeEmail(rawEmail)
	fields := map[string]string{}

	if !ok {
		fields["email"] = "invalid"
	}

	newEmail, okNew := normalizeEmail(rawNew)
	if !okNew {
		fields["newEmail"] = "invalid"
	}

	if password == "" {
		fields["password"] = "required"
	}

	if len(fields) > 0 {
		return api.Validation(fields)
	}

	if err := s.allow(ctx, ruleChangeIP, ip, ""); err != nil {
		return err
	}

	if err := s.allow(ctx, ruleChangeEmail, email, ""); err != nil {
		return err
	}

	user, err := s.store.UserByEmail(ctx, email)
	if err != nil {
		return err
	}

	hctx, cancel := s.hashContext(ctx)
	defer cancel()

	matches := false

	if user != nil && user.PasswordHash != nil {
		matches, err = s.hasher.Verify(hctx, password, *user.PasswordHash)
		if err != nil {
			return busyOr(err)
		}
	} else if err := s.hasher.VerifyDummy(hctx); err != nil {
		return busyOr(err)
	}

	if !matches {
		return api.NewError(http.StatusUnauthorized, "invalid_credentials", "The email or the password is wrong")
	}

	changed, err := s.store.ChangeEmail(ctx, user.ID, newEmail)
	if errors.Is(err, ErrEmailTaken) {
		return api.NewError(http.StatusConflict, "email_taken", "The email has an account")
	}

	if err != nil {
		return err
	}

	if !changed {
		return api.NewError(http.StatusConflict, "already_verified", "The email is verified, so it cannot be changed here")
	}

	return s.sendVerification(ctx, user.ID, newEmail)
}

// ForgotPassword sends the reset link when the email has an account with a password. The answer is the same for every email.
func (s *Service) ForgotPassword(ctx context.Context, ip, rawEmail string) error {
	email, ok := normalizeEmail(rawEmail)
	if !ok {
		return api.Validation(map[string]string{"email": "invalid"})
	}

	if err := s.allow(ctx, ruleForgotIP, ip, ""); err != nil {
		return err
	}

	if err := s.allow(ctx, ruleForgotEmail, email, "resend_too_soon"); err != nil {
		return err
	}

	user, err := s.store.UserByEmail(ctx, email)
	if err != nil || user == nil || user.PasswordHash == nil {
		return err
	}

	raw, hash, err := newToken()
	if err != nil {
		return err
	}

	if err := s.store.SaveToken(ctx, user.ID, purposeReset, hash, resetTTL); err != nil {
		return err
	}

	s.mailer.PasswordReset(email, s.link("/reset-password", raw))

	return nil
}

// ResetPassword sets a new password with the token of the link and ends every session of the user
func (s *Service) ResetPassword(ctx context.Context, ip, token, password string) error {
	fields := map[string]string{}

	if !validToken(token) {
		fields["token"] = "invalid"
	}

	if code, ok := validPassword(password); !ok {
		fields["password"] = code
	}

	if len(fields) > 0 {
		return api.Validation(fields)
	}

	if err := s.allow(ctx, ruleTokenIP, ip, ""); err != nil {
		return err
	}

	hctx, cancel := s.hashContext(ctx)
	defer cancel()

	hash, err := s.hasher.Hash(hctx, password)
	if err != nil {
		return busyOr(err)
	}

	err = s.store.ResetPassword(ctx, hashToken(token), hash)
	if errors.Is(err, ErrTokenInvalid) {
		return tokenInvalid()
	}

	if err != nil {
		return fmt.Errorf("reset the password %w", err)
	}

	return nil
}
