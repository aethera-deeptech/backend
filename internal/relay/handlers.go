package relay

import (
	"net/http"

	"github.com/gofiber/fiber/v3"

	"github.com/aethera-deeptech/backend/internal/api"
	"github.com/aethera-deeptech/backend/internal/auth"
)

// Handlers are the routes of the relay of a device
type Handlers struct {
	svc *Service
}

// NewHandlers returns the routes
func NewHandlers(svc *Service) *Handlers {
	return &Handlers{svc: svc}
}

// Register adds the routes behind the middleware that needs a signed in user
func (h *Handlers) Register(r fiber.Router, require fiber.Handler) {
	r.Get("/devices/:id/relay", require, h.get)
	r.Post("/devices/:id/relay", require, h.send)
	r.Put("/devices/:id/schedule", require, h.setSchedule)
	r.Delete("/devices/:id/schedule", require, h.deleteSchedule)
}

func (h *Handlers) get(c fiber.Ctx) error {
	out, err := h.svc.Get(c.Context(), auth.UserFrom(c).BusinessID, c.Params("id"))
	if err != nil {
		return err
	}

	return c.JSON(out)
}

func (h *Handlers) send(c fiber.Ctx) error {
	var in struct {
		State string `json:"state"`
	}

	if err := api.Decode(c, &in); err != nil {
		return err
	}

	out, err := h.svc.Send(c.Context(), auth.UserFrom(c).BusinessID, c.Params("id"), in.State)
	if err != nil {
		return err
	}

	return c.Status(http.StatusAccepted).JSON(out)
}

func (h *Handlers) setSchedule(c fiber.Ctx) error {
	var in SchedulePatch

	if err := api.Decode(c, &in); err != nil {
		return err
	}

	out, err := h.svc.SetSchedule(c.Context(), auth.UserFrom(c).BusinessID, c.Params("id"), in)
	if err != nil {
		return err
	}

	return c.JSON(out)
}

func (h *Handlers) deleteSchedule(c fiber.Ctx) error {
	if err := h.svc.DeleteSchedule(c.Context(), auth.UserFrom(c).BusinessID, c.Params("id")); err != nil {
		return err
	}

	return c.SendStatus(http.StatusNoContent)
}
