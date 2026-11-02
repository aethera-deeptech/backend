package tips

import (
	"github.com/gofiber/fiber/v3"
	"github.com/google/uuid"

	"github.com/aethera-deeptech/backend/internal/auth"
)

func validID(id string) bool {
	_, err := uuid.Parse(id)

	return err == nil
}

// Handlers are the routes of the tips
type Handlers struct {
	svc *Service
}

// NewHandlers returns the routes
func NewHandlers(svc *Service) *Handlers {
	return &Handlers{svc: svc}
}

// Register adds the routes behind the middleware that needs a signed in user
func (h *Handlers) Register(r fiber.Router, require fiber.Handler) {
	r.Get("/recommendations", require, h.list)
	r.Get("/recommendations/:id", require, h.get)

	for _, action := range []string{"apply", "defer", "reject", "cancel"} {
		r.Post("/recommendations/:id/"+action, require, h.decide(action))
	}
}

func (h *Handlers) list(c fiber.Ctx) error {
	out, err := h.svc.List(c.Context(), auth.UserFrom(c).BusinessID)
	if err != nil {
		return err
	}

	return c.JSON(out)
}

func (h *Handlers) get(c fiber.Ctx) error {
	out, err := h.svc.Get(c.Context(), auth.UserFrom(c).BusinessID, c.Params("id"))
	if err != nil {
		return err
	}

	return c.JSON(out)
}

func (h *Handlers) decide(action string) fiber.Handler {
	return func(c fiber.Ctx) error {
		out, err := h.svc.Decide(c.Context(), auth.UserFrom(c).BusinessID, c.Params("id"), action)
		if err != nil {
			return err
		}

		return c.JSON(out)
	}
}
