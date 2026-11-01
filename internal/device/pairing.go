// Package device holds the devices of a business, which are the nodes that the owner claimed with a pairing
package device

import (
	"context"
	"errors"
	"fmt"
	"net/http"
	"time"

	"github.com/gofiber/fiber/v3"
	"github.com/google/uuid"
	"github.com/jackc/pgx/v5"
	"github.com/jackc/pgx/v5/pgconn"
	"github.com/jackc/pgx/v5/pgxpool"

	"github.com/aethera-deeptech/backend/internal/api"
	"github.com/aethera-deeptech/backend/internal/auth"
	"github.com/aethera-deeptech/backend/internal/business"
)

const (
	pairingTTL      = 3 * time.Minute
	nodeLookback    = 5 * time.Minute
	maxDeviceName   = 40
	uniqueViolation = "23505"
)

var rulePairing = auth.Rule{Bucket: "pairing-business", Max: 30, Window: time.Hour}

// Pairing is a search for a new device
type Pairing struct {
	Status   string  `json:"status"`
	DeviceID *string `json:"deviceId"`
}

// Pairings holds the rules of the pairing of a new device
type Pairings struct {
	pool    *pgxpool.Pool
	limiter *auth.Limiter
}

// NewPairings returns the rules on the pool
func NewPairings(pool *pgxpool.Pool, limiter *auth.Limiter) *Pairings {
	return &Pairings{pool: pool, limiter: limiter}
}

// Start begins the search for the next node of a kind that a gateway of the business hears and nobody claimed. The business needs a
// gateway, and it has one search at a time.
func (p *Pairings) Start(ctx context.Context, businessID, kind, rawName, roomID string) (string, error) {
	fields := map[string]string{}

	if kind != "plug" && kind != "clamp" {
		fields["kind"] = "invalid"
	}

	name, code := business.CleanName(rawName, maxDeviceName)
	if code != "" {
		fields["name"] = code
	}

	if _, err := uuid.Parse(roomID); err != nil {
		fields["roomId"] = "invalid"
	}

	if len(fields) > 0 {
		return "", api.Validation(fields)
	}

	ok, wait, err := p.limiter.Allow(ctx, rulePairing, businessID)
	if err != nil {
		return "", fmt.Errorf("check the limit %w", err)
	}

	if !ok {
		return "", api.RateLimited("", int(wait.Seconds()+0.5))
	}

	var roomOK bool
	if err := p.pool.QueryRow(ctx, "SELECT EXISTS (SELECT 1 FROM rooms WHERE id = $1 AND business_id = $2)", roomID, businessID).Scan(&roomOK); err != nil {
		return "", fmt.Errorf("read the room %w", err)
	}

	if !roomOK {
		return "", api.Validation(map[string]string{"roomId": "invalid"})
	}

	var hasGateway bool
	if err := p.pool.QueryRow(ctx, "SELECT EXISTS (SELECT 1 FROM gateways WHERE business_id = $1)", businessID).Scan(&hasGateway); err != nil {
		return "", fmt.Errorf("read the gateways %w", err)
	}

	if !hasGateway {
		return "", api.NewError(http.StatusConflict, "no_gateway", "The business has no gateway yet")
	}

	tx, err := p.pool.BeginTx(ctx, pgx.TxOptions{})
	if err != nil {
		return "", fmt.Errorf("start the pairing %w", err)
	}

	defer func() { _ = tx.Rollback(ctx) }()

	// a search that ran out of time is closed first, so it does not block the new one
	if _, err := tx.Exec(ctx, "UPDATE pairings SET status = 'failed' WHERE business_id = $1 AND status = 'searching' AND expires_at < now()", businessID); err != nil {
		return "", fmt.Errorf("close the old search %w", err)
	}

	var id string

	err = tx.QueryRow(ctx,
		"INSERT INTO pairings (business_id, kind, name, room_id, expires_at) VALUES ($1, $2, $3, $4, now() + make_interval(secs => $5)) RETURNING id",
		businessID, kind, name, roomID, pairingTTL.Seconds()).Scan(&id)
	if err != nil {
		var pgErr *pgconn.PgError
		if errors.As(err, &pgErr) && pgErr.Code == uniqueViolation {
			return "", api.NewError(http.StatusConflict, "pairing_running", "A search for a device is running")
		}

		return "", fmt.Errorf("store the pairing %w", err)
	}

	if err := tx.Commit(ctx); err != nil {
		return "", fmt.Errorf("finish the pairing %w", err)
	}

	return id, nil
}

// Get returns the state of a search. While it searches it also looks for the node, so the answer is up to date without a worker.
func (p *Pairings) Get(ctx context.Context, businessID, id string) (Pairing, error) {
	if _, err := uuid.Parse(id); err != nil {
		return Pairing{}, api.NotFound()
	}

	tx, err := p.pool.BeginTx(ctx, pgx.TxOptions{})
	if err != nil {
		return Pairing{}, fmt.Errorf("start the read %w", err)
	}

	defer func() { _ = tx.Rollback(ctx) }()

	var (
		out       Pairing
		kind      string
		name      string
		roomID    string
		createdAt time.Time
		expired   bool
	)

	err = tx.QueryRow(ctx,
		"SELECT status, device_id, kind, name, room_id, created_at, expires_at < now() FROM pairings WHERE id = $1 AND business_id = $2 FOR UPDATE",
		id, businessID).Scan(&out.Status, &out.DeviceID, &kind, &name, &roomID, &createdAt, &expired)
	if errors.Is(err, pgx.ErrNoRows) {
		return Pairing{}, api.NotFound()
	}

	if err != nil {
		return Pairing{}, fmt.Errorf("read the pairing %w", err)
	}

	if out.Status != "searching" {
		// the app has three states, so a search that the owner stopped reads as failed
		if out.Status == "cancelled" {
			out.Status = "failed"
		}

		return out, nil
	}

	deviceID, found, err := claimNode(ctx, tx, businessID, kind, name, roomID, createdAt)
	if err != nil {
		return Pairing{}, err
	}

	switch {
	case found:
		if _, err := tx.Exec(ctx, "UPDATE pairings SET status = 'connected', device_id = $2 WHERE id = $1", id, deviceID); err != nil {
			return Pairing{}, fmt.Errorf("save the pairing %w", err)
		}

		out = Pairing{Status: "connected", DeviceID: &deviceID}
	case expired:
		if _, err := tx.Exec(ctx, "UPDATE pairings SET status = 'failed' WHERE id = $1", id); err != nil {
			return Pairing{}, fmt.Errorf("close the pairing %w", err)
		}

		out.Status = "failed"
	}

	if err := tx.Commit(ctx); err != nil {
		return Pairing{}, fmt.Errorf("finish the read %w", err)
	}

	return out, nil
}

// claimNode makes a device of the unclaimed node of the kind that a gateway of the business heard most recently. A node that went
// quiet more than a few minutes before the search began is left alone, so a forgotten plug does not become the new device.
func claimNode(ctx context.Context, tx pgx.Tx, businessID, kind, name, roomID string, since time.Time) (string, bool, error) {
	var gatewayID, nodeID string

	err := tx.QueryRow(ctx,
		`SELECT n.gateway_id, n.node_id FROM nodes n JOIN gateways g ON g.id = n.gateway_id
		 WHERE g.business_id = $1 AND n.kind = $2 AND n.last_seen_at > $3
		   AND NOT EXISTS (SELECT 1 FROM devices d WHERE d.gateway_id = n.gateway_id AND d.node_id = n.node_id)
		 ORDER BY n.first_seen_at DESC LIMIT 1 FOR UPDATE OF n SKIP LOCKED`,
		businessID, kind, since.Add(-nodeLookback)).Scan(&gatewayID, &nodeID)
	if errors.Is(err, pgx.ErrNoRows) {
		return "", false, nil
	}

	if err != nil {
		return "", false, fmt.Errorf("look for the node %w", err)
	}

	var deviceID string

	err = tx.QueryRow(ctx,
		"INSERT INTO devices (business_id, gateway_id, node_id, kind, name, room_id) VALUES ($1, $2, $3, $4, $5, $6) RETURNING id",
		businessID, gatewayID, nodeID, kind, name, roomID).Scan(&deviceID)
	if err != nil {
		return "", false, fmt.Errorf("create the device %w", err)
	}

	return deviceID, true, nil
}

// Cancel stops a search that is running. A search that is over stays as it is, so the answer is the same on every call.
func (p *Pairings) Cancel(ctx context.Context, businessID, id string) error {
	if _, err := uuid.Parse(id); err != nil {
		return api.NotFound()
	}

	var exists bool
	if err := p.pool.QueryRow(ctx, "SELECT EXISTS (SELECT 1 FROM pairings WHERE id = $1 AND business_id = $2)", id, businessID).Scan(&exists); err != nil {
		return fmt.Errorf("read the pairing %w", err)
	}

	if !exists {
		return api.NotFound()
	}

	if _, err := p.pool.Exec(ctx, "UPDATE pairings SET status = 'cancelled' WHERE id = $1 AND business_id = $2 AND status = 'searching'", id, businessID); err != nil {
		return fmt.Errorf("cancel the pairing %w", err)
	}

	return nil
}

// Handlers are the routes of the pairing
type Handlers struct {
	pairings *Pairings
}

// NewHandlers returns the routes
func NewHandlers(pairings *Pairings) *Handlers {
	return &Handlers{pairings: pairings}
}

// Register adds the routes behind the middleware that needs a signed in user
func (h *Handlers) Register(r fiber.Router, require fiber.Handler) {
	r.Post("/pairings", require, h.start)
	r.Get("/pairings/:id", require, h.get)
	r.Delete("/pairings/:id", require, h.cancel)
}

func (h *Handlers) start(c fiber.Ctx) error {
	var in struct {
		Kind   string `json:"kind"`
		Name   string `json:"name"`
		RoomID string `json:"roomId"`
	}

	if err := api.Decode(c, &in); err != nil {
		return err
	}

	id, err := h.pairings.Start(c.Context(), auth.UserFrom(c).BusinessID, in.Kind, in.Name, in.RoomID)
	if err != nil {
		return err
	}

	return c.Status(http.StatusCreated).JSON(fiber.Map{"id": id})
}

func (h *Handlers) get(c fiber.Ctx) error {
	out, err := h.pairings.Get(c.Context(), auth.UserFrom(c).BusinessID, c.Params("id"))
	if err != nil {
		return err
	}

	return c.JSON(out)
}

func (h *Handlers) cancel(c fiber.Ctx) error {
	if err := h.pairings.Cancel(c.Context(), auth.UserFrom(c).BusinessID, c.Params("id")); err != nil {
		return err
	}

	return c.SendStatus(http.StatusNoContent)
}
