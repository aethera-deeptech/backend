package report

import (
	"github.com/gofiber/fiber/v3"

	"github.com/aethera-deeptech/backend/internal/auth"
)

// Handlers are the routes of the report
type Handlers struct {
	svc *Service
}

// NewHandlers returns the routes
func NewHandlers(svc *Service) *Handlers {
	return &Handlers{svc: svc}
}

// Register adds the routes behind the middleware that needs a signed in user
func (h *Handlers) Register(r fiber.Router, require fiber.Handler) {
	r.Get("/reports", require, h.get)
	r.Get("/reports/:period/pdf", require, h.pdf)
}

func (h *Handlers) get(c fiber.Ctx) error {
	out, err := h.svc.Get(c.Context(), auth.UserFrom(c).BusinessID, c.Query("period"))
	if err != nil {
		return err
	}

	return c.JSON(out)
}

func (h *Handlers) pdf(c fiber.Ctx) error {
	body, name, err := h.svc.PDF(c.Context(), auth.UserFrom(c).BusinessID, c.Params("period"))
	if err != nil {
		return err
	}

	c.Set(fiber.HeaderContentType, "application/pdf")
	c.Set(fiber.HeaderContentDisposition, `attachment; filename="`+name+`"`)

	return c.Send(body)
}
