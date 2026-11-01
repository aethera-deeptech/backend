package business

import (
	"net/http"

	"github.com/gofiber/fiber/v3"

	"github.com/aethera-deeptech/backend/internal/api"
	"github.com/aethera-deeptech/backend/internal/auth"
)

// Handlers are the routes of the settings and the rooms
type Handlers struct {
	store *Store
}

// NewHandlers returns the routes
func NewHandlers(store *Store) *Handlers {
	return &Handlers{store: store}
}

// Register adds the routes to the router. The routes need a signed in user, so the caller passes the middleware that checks it.
func (h *Handlers) Register(r fiber.Router, require fiber.Handler) {
	r.Get("/settings", h.getSettings, require)
	r.Patch("/settings", h.patchSettings, require)
	r.Get("/rooms", h.listRooms, require)
	r.Post("/rooms", h.createRoom, require)
}

func (h *Handlers) getSettings(c fiber.Ctx) error {
	out, err := h.store.Settings(c.Context(), auth.UserFrom(c).BusinessID)
	if err != nil {
		return err
	}

	return c.JSON(out)
}

func (h *Handlers) patchSettings(c fiber.Ctx) error {
	var p Patch
	if err := api.Decode(c, &p); err != nil {
		return err
	}

	out, err := h.store.Update(c.Context(), auth.UserFrom(c).BusinessID, p)
	if err != nil {
		return err
	}

	return c.JSON(out)
}

func (h *Handlers) listRooms(c fiber.Ctx) error {
	rooms, err := h.store.Rooms(c.Context(), auth.UserFrom(c).BusinessID)
	if err != nil {
		return err
	}

	return c.JSON(fiber.Map{"items": rooms})
}

func (h *Handlers) createRoom(c fiber.Ctx) error {
	var in struct {
		Name string `json:"name"`
	}

	if err := api.Decode(c, &in); err != nil {
		return err
	}

	room, err := h.store.CreateRoom(c.Context(), auth.UserFrom(c).BusinessID, in.Name)
	if err != nil {
		return err
	}

	return c.Status(http.StatusCreated).JSON(room)
}
