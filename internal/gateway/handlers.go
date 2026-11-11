package gateway

import (
	"context"
	"net/http"
	"strings"
	"time"

	"github.com/gofiber/fiber/v3"

	"github.com/aethera-deeptech/backend/internal/api"
	"github.com/aethera-deeptech/backend/internal/auth"
)

// Handlers are the routes that the app calls to manage its gateways and the routes that a gateway calls
type Handlers struct {
	svc     *Service
	confirm PasswordCheck
	now     func() time.Time
}

// PasswordCheck asks a signed in user for the password again and answers nil only for the right one
type PasswordCheck func(ctx context.Context, user *auth.User, password string) error

// NewHandlers returns the routes. The confirm function guards the routes that cannot be undone.
func NewHandlers(svc *Service, confirm PasswordCheck) *Handlers {
	return &Handlers{svc: svc, confirm: confirm, now: time.Now}
}

// RegisterApp adds the routes of the app under /v1 behind the middleware that needs a signed in user
func (h *Handlers) RegisterApp(r fiber.Router, require fiber.Handler) {
	r.Get("/gateways", require, h.list)
	r.Post("/gateways", require, h.claim)
	r.Post("/gateways/:id/revoke-key", require, h.revokeKey)
	r.Delete("/gateways/:id", api.BodyLimit(maxConfirmBody), require, h.deleteGateway)
}

// RegisterDevice adds the routes of a gateway under /device/v1. They take no cookie and need no CSRF header, because a gateway is
// not a browser. The routes for readings need the key of the gateway.
func (h *Handlers) RegisterDevice(r fiber.Router) {
	r.Use(api.NoStore(), refuseEncoding)
	r.Post("/enroll", api.BodyLimit(maxEnrollBody), h.enroll)
	r.Get("/enroll/:pollId", h.poll)
	r.Post("/readings", h.readings)
}

// refuseEncoding answers 415 to a body that is compressed. A gateway has no use for it, and the server would inflate the body before
// any limit on its size could see it, so a small request could cost a lot of memory.
func refuseEncoding(c fiber.Ctx) error {
	if enc := strings.TrimSpace(c.Get(fiber.HeaderContentEncoding)); enc != "" && !strings.EqualFold(enc, "identity") {
		return api.NewError(http.StatusUnsupportedMediaType, "unsupported_encoding", "The body must not be compressed")
	}

	return c.Next()
}

// maxConfirmBody is the largest body of a request that asks for the password again
const maxConfirmBody = 2 << 10

// maxEnrollBody is the largest body of an enroll request, which has a hardware id, a version and a hash
const maxEnrollBody = 2 << 10

func (h *Handlers) list(c fiber.Ctx) error {
	items, err := h.svc.List(c.Context(), auth.UserFrom(c).BusinessID)
	if err != nil {
		return err
	}

	return c.JSON(fiber.Map{"items": items})
}

func (h *Handlers) claim(c fiber.Ctx) error {
	var in struct {
		ClaimCode string `json:"claimCode"`
		Name      string `json:"name"`
	}

	if err := api.Decode(c, &in); err != nil {
		return err
	}

	gw, err := h.svc.Claim(c.Context(), auth.UserFrom(c).BusinessID, in.ClaimCode, in.Name)
	if err != nil {
		return err
	}

	return c.Status(http.StatusCreated).JSON(gw)
}

func (h *Handlers) revokeKey(c fiber.Ctx) error {
	if err := h.svc.RevokeKey(c.Context(), auth.UserFrom(c).BusinessID, c.Params("id")); err != nil {
		return err
	}

	return c.SendStatus(http.StatusNoContent)
}

// deleteGateway removes a gateway after a check of the password, which is the same check and the same budget as the delete of an account
func (h *Handlers) deleteGateway(c fiber.Ctx) error {
	var in struct {
		Password string `json:"password"`
	}

	if err := api.Decode(c, &in); err != nil {
		return err
	}

	user := auth.UserFrom(c)

	if err := h.confirm(c.Context(), user, in.Password); err != nil {
		return err
	}

	if err := h.svc.Delete(c.Context(), user.BusinessID, c.Params("id")); err != nil {
		return err
	}

	return c.SendStatus(http.StatusNoContent)
}

func (h *Handlers) enroll(c fiber.Ctx) error {
	var in struct {
		HardwareID string `json:"hardwareId"`
		Firmware   string `json:"firmware"`
		KeyHash    string `json:"keyHash"`
	}

	if err := api.Decode(c, &in); err != nil {
		return err
	}

	e, err := h.svc.Enroll(c.Context(), api.ClientKey(c), in.HardwareID, in.Firmware, in.KeyHash)
	if err != nil {
		return err
	}

	return c.Status(http.StatusCreated).JSON(e)
}

func (h *Handlers) poll(c fiber.Ctx) error {
	res, err := h.svc.Poll(c.Context(), api.ClientKey(c), c.Params("pollId"))
	if err != nil {
		return err
	}

	if !res.Claimed {
		return c.SendStatus(http.StatusAccepted)
	}

	return c.JSON(fiber.Map{"gatewayId": res.GatewayID})
}

func (h *Handlers) readings(c fiber.Ctx) error {
	// the key is checked before the body is read as JSON, so a stranger cannot make the service parse a large body
	p, err := h.svc.Authenticate(c.Context(), c.Get(fiber.HeaderAuthorization))
	if err != nil {
		return err
	}

	if len(c.Body()) > MaxBodyBytes {
		return api.NewError(http.StatusRequestEntityTooLarge, "payload_too_large", "The batch is larger than 256 KiB")
	}

	var b Batch
	if err := api.Decode(c, &b); err != nil {
		return err
	}

	ack, err := h.svc.Ingest(c.Context(), p, b, h.now())
	if err != nil {
		return err
	}

	return c.JSON(ack)
}
