package auth

import (
	"errors"
	"net/http"
	"time"

	"github.com/gofiber/fiber/v3"

	"github.com/aethera-deeptech/backend/internal/api"
)

// CookieConfig describes the session cookie. In production the name has the prefix __Host-, which makes a browser accept the cookie
// only with Secure, with Path / and without a Domain, so a sibling site cannot set or overwrite it.
type CookieConfig struct {
	Name   string
	Secure bool
}

// CookieFor returns the cookie settings for the environment. A development setup on http://localhost cannot use a Secure cookie
// in every browser, so it uses a plain name there.
func CookieFor(production bool) CookieConfig {
	if production {
		return CookieConfig{Name: "__Host-aethera_session", Secure: true}
	}

	return CookieConfig{Name: "aethera_session"}
}

// Handlers are the routes of the account
type Handlers struct {
	svc    *Service
	cookie CookieConfig
}

// principalKey is the key of the signed in user in the locals of a request
type principalKey struct{}

// NewHandlers returns the routes of the account
func NewHandlers(svc *Service, cookie CookieConfig) *Handlers {
	return &Handlers{svc: svc, cookie: cookie}
}

// Register adds the routes under /auth of the router
func (h *Handlers) Register(r fiber.Router) {
	g := r.Group("/auth")
	g.Post("/register", h.register)
	g.Post("/login", h.login)
	g.Post("/logout", h.logout, h.Require())
	g.Get("/me", h.me, h.Require())
	g.Post("/verify-email", h.verifyEmail)
	g.Post("/resend-verification", h.resend)
	g.Post("/change-email", h.changeEmail)
	g.Post("/forgot-password", h.forgot)
	g.Post("/reset-password", h.reset)
}

func (h *Handlers) setCookie(c fiber.Ctx, token string) {
	c.Cookie(&fiber.Cookie{
		Name: h.cookie.Name, Value: token, Path: "/", MaxAge: int(sessionAbsolute / time.Second),
		HTTPOnly: true, Secure: h.cookie.Secure, SameSite: fiber.CookieSameSiteLaxMode,
	})
}

func (h *Handlers) clearCookie(c fiber.Ctx) {
	c.Cookie(&fiber.Cookie{
		Name: h.cookie.Name, Value: "", Path: "/", MaxAge: -1,
		HTTPOnly: true, Secure: h.cookie.Secure, SameSite: fiber.CookieSameSiteLaxMode,
	})
}

// Require is the middleware of every route that needs a signed in user. It puts the user in the locals of the request.
func (h *Handlers) Require() fiber.Handler {
	return func(c fiber.Ctx) error {
		user, err := h.svc.Authenticate(c.Context(), c.Cookies(h.cookie.Name))
		if err != nil {
			if errors.Is(err, ErrNoSession) {
				h.clearCookie(c)

				return api.Unauthorized()
			}

			return err
		}

		c.Locals(principalKey{}, user)

		return c.Next()
	}
}

// UserFrom returns the signed in user of a request that passed Require
func UserFrom(c fiber.Ctx) *User {
	u, _ := c.Locals(principalKey{}).(*User)

	return u
}

type verificationSent struct {
	RetryAfterSeconds int `json:"retryAfterSeconds"`
}

var sent = verificationSent{RetryAfterSeconds: int(resendCooldown / time.Second)}

func (h *Handlers) register(c fiber.Ctx) error {
	var in struct {
		BusinessName string `json:"businessName"`
		Email        string `json:"email"`
		Password     string `json:"password"`
	}

	if err := api.Decode(c, &in); err != nil {
		return err
	}

	if err := h.svc.Register(c.Context(), c.IP(), in.BusinessName, in.Email, in.Password); err != nil {
		return err
	}

	return c.Status(http.StatusCreated).JSON(sent)
}

func (h *Handlers) login(c fiber.Ctx) error {
	var in struct {
		Email    string `json:"email"`
		Password string `json:"password"`
	}

	if err := api.Decode(c, &in); err != nil {
		return err
	}

	token, me, err := h.svc.Login(c.Context(), c.IP(), in.Email, in.Password)
	if err != nil {
		return err
	}

	h.setCookie(c, token)

	return c.JSON(me)
}

func (h *Handlers) logout(c fiber.Ctx) error {
	if err := h.svc.Logout(c.Context(), c.Cookies(h.cookie.Name)); err != nil {
		return err
	}

	h.clearCookie(c)

	return c.SendStatus(http.StatusNoContent)
}

func (h *Handlers) me(c fiber.Ctx) error {
	return c.JSON(meOf(UserFrom(c)))
}

func (h *Handlers) verifyEmail(c fiber.Ctx) error {
	var in struct {
		Token string `json:"token"`
	}

	if err := api.Decode(c, &in); err != nil {
		return err
	}

	token, me, err := h.svc.VerifyEmail(c.Context(), c.IP(), in.Token)
	if err != nil {
		return err
	}

	h.setCookie(c, token)

	return c.JSON(me)
}

func (h *Handlers) resend(c fiber.Ctx) error {
	var in struct {
		Email string `json:"email"`
	}

	if err := api.Decode(c, &in); err != nil {
		return err
	}

	if err := h.svc.ResendVerification(c.Context(), c.IP(), in.Email); err != nil {
		return err
	}

	return c.Status(http.StatusAccepted).JSON(sent)
}

func (h *Handlers) changeEmail(c fiber.Ctx) error {
	var in struct {
		Email    string `json:"email"`
		Password string `json:"password"`
		NewEmail string `json:"newEmail"`
	}

	if err := api.Decode(c, &in); err != nil {
		return err
	}

	if err := h.svc.ChangeEmail(c.Context(), c.IP(), in.Email, in.Password, in.NewEmail); err != nil {
		return err
	}

	return c.Status(http.StatusAccepted).JSON(sent)
}

func (h *Handlers) forgot(c fiber.Ctx) error {
	var in struct {
		Email string `json:"email"`
	}

	if err := api.Decode(c, &in); err != nil {
		return err
	}

	if err := h.svc.ForgotPassword(c.Context(), c.IP(), in.Email); err != nil {
		return err
	}

	return c.Status(http.StatusAccepted).JSON(sent)
}

func (h *Handlers) reset(c fiber.Ctx) error {
	var in struct {
		Token    string `json:"token"`
		Password string `json:"password"`
	}

	if err := api.Decode(c, &in); err != nil {
		return err
	}

	if err := h.svc.ResetPassword(c.Context(), c.IP(), in.Token, in.Password); err != nil {
		return err
	}

	return c.SendStatus(http.StatusNoContent)
}
