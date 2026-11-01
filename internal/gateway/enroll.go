// Package gateway holds the gateways of the businesses, which are the only units that talk to the service. It covers the enrollment
// and the claim of a gateway, the check of its key and the intake of the readings that it sends for its nodes.
package gateway

import (
	"context"
	"crypto/rand"
	"crypto/sha256"
	"encoding/base64"
	"encoding/hex"
	"errors"
	"fmt"
	"net/http"
	"regexp"
	"strings"
	"time"

	"github.com/jackc/pgx/v5"
	"github.com/jackc/pgx/v5/pgconn"
	"github.com/jackc/pgx/v5/pgxpool"

	"github.com/aethera-deeptech/backend/internal/api"
	"github.com/aethera-deeptech/backend/internal/auth"
	"github.com/aethera-deeptech/backend/internal/business"
)

const (
	// codeAlphabet has no 0, 1, I or O, so a code that is read from a small display and typed by hand has no look-alike characters
	codeAlphabet = "ABCDEFGHJKLMNPQRSTUVWXYZ23456789"
	codeLength   = 8
	enrollTTL    = 10 * time.Minute
	maxGateways  = 10

	uniqueViolation = "23505"
)

var (
	hardwareIDPattern = regexp.MustCompile(`^[A-Za-z0-9:_-]{6,64}$`)
	keyHashPattern    = regexp.MustCompile(`^[0-9a-f]{64}$`)
	codePattern       = regexp.MustCompile(`^[A-Z2-9]{8}$`)
)

// The limits of the routes of a gateway that is not signed in yet, and of the claim
var (
	ruleEnrollIP       = auth.Rule{Bucket: "enroll-ip", Max: 20, Window: time.Hour}
	ruleEnrollHardware = auth.Rule{Bucket: "enroll-hardware", Max: 10, Window: time.Hour}
	rulePollIP         = auth.Rule{Bucket: "enroll-poll-ip", Max: 300, Window: 15 * time.Minute}
	ruleClaim          = auth.Rule{Bucket: "claim-business", Max: 10, Window: time.Hour}
)

// Gateway is a gateway of a business as the app reads it
type Gateway struct {
	ID         string     `json:"id"`
	Name       string     `json:"name"`
	LastSeenAt *time.Time `json:"lastSeenAt"`
}

// Service holds the rules of the gateways
type Service struct {
	pool    *pgxpool.Pool
	limiter *auth.Limiter
}

// NewService returns the service
func NewService(pool *pgxpool.Pool, limiter *auth.Limiter) *Service {
	return &Service{pool: pool, limiter: limiter}
}

func hashOf(s string) []byte {
	sum := sha256.Sum256([]byte(s))

	return sum[:]
}

func (s *Service) allow(ctx context.Context, rule auth.Rule, key string) error {
	ok, wait, err := s.limiter.Allow(ctx, rule, key)
	if err != nil {
		return fmt.Errorf("check the limit %w", err)
	}

	if !ok {
		return api.RateLimited("", int(wait.Seconds()+0.5))
	}

	return nil
}

func newCode() (string, error) {
	raw := make([]byte, codeLength)
	if _, err := rand.Read(raw); err != nil {
		return "", fmt.Errorf("make a code %w", err)
	}

	var b strings.Builder

	for _, x := range raw {
		b.WriteByte(codeAlphabet[int(x)%len(codeAlphabet)])
	}

	return b.String(), nil
}

// Enrollment is what a gateway gets when it asks to be claimed
type Enrollment struct {
	ClaimCode        string `json:"claimCode"`
	PollID           string `json:"pollId"`
	ExpiresInSeconds int    `json:"expiresInSeconds"`
}

// Enroll stores a request of a gateway to be claimed. The gateway sends the SHA-256 of a key that it made itself, so the service never
// sees the key. A newer request of the same hardware replaces the one that is waiting.
func (s *Service) Enroll(ctx context.Context, ip, hardwareID, firmware, keyHash string) (Enrollment, error) {
	fields := map[string]string{}

	if !hardwareIDPattern.MatchString(hardwareID) {
		fields["hardwareId"] = "invalid"
	}

	if !keyHashPattern.MatchString(keyHash) {
		fields["keyHash"] = "invalid"
	}

	if len(firmware) > 32 || strings.ContainsAny(firmware, "\r\n\x00") {
		fields["firmware"] = "invalid"
	}

	if len(fields) > 0 {
		return Enrollment{}, api.Validation(fields)
	}

	if err := s.allow(ctx, ruleEnrollIP, ip); err != nil {
		return Enrollment{}, err
	}

	if err := s.allow(ctx, ruleEnrollHardware, hardwareID); err != nil {
		return Enrollment{}, err
	}

	rawKeyHash, _ := hex.DecodeString(keyHash)

	poll := make([]byte, 32)
	if _, err := rand.Read(poll); err != nil {
		return Enrollment{}, fmt.Errorf("make a poll id %w", err)
	}

	pollID := base64.RawURLEncoding.EncodeToString(poll)

	// a code can collide with one that is waiting, which has 1 chance in 10^12, so a few tries are more than enough
	for range 3 {
		code, err := newCode()
		if err != nil {
			return Enrollment{}, err
		}

		err = s.storeEnrollment(ctx, hardwareID, firmware, rawKeyHash, hashOf(pollID), hashOf(code))
		if err == nil {
			return Enrollment{ClaimCode: code, PollID: pollID, ExpiresInSeconds: int(enrollTTL / time.Second)}, nil
		}

		var pgErr *pgconn.PgError
		if !errors.As(err, &pgErr) || pgErr.Code != uniqueViolation {
			return Enrollment{}, err
		}
	}

	return Enrollment{}, errors.New("no free claim code was found")
}

func (s *Service) storeEnrollment(ctx context.Context, hardwareID, firmware string, keyHash, pollHash, codeHash []byte) error {
	tx, err := s.pool.BeginTx(ctx, pgx.TxOptions{})
	if err != nil {
		return fmt.Errorf("start the enrollment %w", err)
	}

	defer func() { _ = tx.Rollback(ctx) }()

	if _, err := tx.Exec(ctx, "DELETE FROM gateway_enrollments WHERE gateway_id IS NULL AND (hardware_id = $1 OR expires_at < now())", hardwareID); err != nil {
		return fmt.Errorf("remove the older enrollments %w", err)
	}

	if _, err := tx.Exec(ctx,
		`INSERT INTO gateway_enrollments (poll_hash, code_hash, hardware_id, firmware, key_hash, expires_at)
		 VALUES ($1, $2, $3, NULLIF($4, ''), $5, now() + make_interval(secs => $6))`,
		pollHash, codeHash, hardwareID, firmware, keyHash, enrollTTL.Seconds()); err != nil {
		return fmt.Errorf("store the enrollment %w", err)
	}

	return tx.Commit(ctx) //nolint:wrapcheck // the caller reads the code of the database error
}

// PollResult is the answer to a poll of an enrollment
type PollResult struct {
	Claimed   bool
	GatewayID string
}

// Poll tells a gateway whether its code was claimed. The row is removed after the first answer that says so, so the answer comes
// once. An unknown or expired poll id answers 404.
func (s *Service) Poll(ctx context.Context, ip, pollID string) (PollResult, error) {
	if len(pollID) < 32 || len(pollID) > 64 {
		return PollResult{}, api.NotFound()
	}

	if err := s.allow(ctx, rulePollIP, ip); err != nil {
		return PollResult{}, err
	}

	var (
		gatewayID *string
		expired   bool
	)

	err := s.pool.QueryRow(ctx, "SELECT gateway_id, expires_at < now() FROM gateway_enrollments WHERE poll_hash = $1", hashOf(pollID)).Scan(&gatewayID, &expired)
	if errors.Is(err, pgx.ErrNoRows) {
		return PollResult{}, api.NotFound()
	}

	if err != nil {
		return PollResult{}, fmt.Errorf("read the enrollment %w", err)
	}

	if gatewayID != nil {
		if _, err := s.pool.Exec(ctx, "DELETE FROM gateway_enrollments WHERE poll_hash = $1", hashOf(pollID)); err != nil {
			return PollResult{}, fmt.Errorf("remove the enrollment %w", err)
		}

		return PollResult{Claimed: true, GatewayID: *gatewayID}, nil
	}

	if expired {
		return PollResult{}, api.NewError(http.StatusGone, "enrollment_expired", "The claim code expired")
	}

	return PollResult{}, nil
}

// Claim binds the gateway that shows a claim code to a business. A gateway that the same business claimed before gets its new key
// and keeps its readings. A gateway that another business owns is refused.
func (s *Service) Claim(ctx context.Context, businessID, rawCode, rawName string) (Gateway, error) {
	code := strings.ToUpper(strings.TrimSpace(rawCode))
	fields := map[string]string{}

	if !codePattern.MatchString(code) {
		fields["claimCode"] = "invalid"
	}

	name, nameCode := business.CleanName(rawName, 40)
	if nameCode != "" {
		fields["name"] = nameCode
	}

	if len(fields) > 0 {
		return Gateway{}, api.Validation(fields)
	}

	if err := s.allow(ctx, ruleClaim, businessID); err != nil {
		return Gateway{}, err
	}

	tx, err := s.pool.BeginTx(ctx, pgx.TxOptions{})
	if err != nil {
		return Gateway{}, fmt.Errorf("start the claim %w", err)
	}

	defer func() { _ = tx.Rollback(ctx) }()

	var (
		hardwareID string
		keyHash    []byte
	)

	err = tx.QueryRow(ctx,
		"SELECT hardware_id, key_hash FROM gateway_enrollments WHERE code_hash = $1 AND gateway_id IS NULL AND expires_at > now() FOR UPDATE",
		hashOf(code)).Scan(&hardwareID, &keyHash)
	if errors.Is(err, pgx.ErrNoRows) {
		return Gateway{}, api.NewError(http.StatusNotFound, "claim_code_unknown", "The code is unknown, used or expired")
	}

	if err != nil {
		return Gateway{}, fmt.Errorf("read the code %w", err)
	}

	var count int
	if err := tx.QueryRow(ctx, "SELECT count(*) FROM gateways WHERE business_id = $1", businessID).Scan(&count); err != nil {
		return Gateway{}, fmt.Errorf("count the gateways %w", err)
	}

	var (
		gw    Gateway
		owner *string
	)

	// the hardware that is already in the table belongs to one business, and only that business may take it again
	err = tx.QueryRow(ctx, "SELECT business_id FROM gateways WHERE hardware_id = $1 FOR UPDATE", hardwareID).Scan(&owner)

	switch {
	case errors.Is(err, pgx.ErrNoRows):
		if count >= maxGateways {
			return Gateway{}, api.NewError(http.StatusConflict, "too_many_gateways", "The business has the most gateways that it can have")
		}

		err = tx.QueryRow(ctx,
			"INSERT INTO gateways (business_id, name, hardware_id, key_hash) VALUES ($1, $2, $3, $4) RETURNING id, name, last_seen_at",
			businessID, name, hardwareID, keyHash).Scan(&gw.ID, &gw.Name, &gw.LastSeenAt)
	case err != nil:
		return Gateway{}, fmt.Errorf("read the gateway %w", err)
	case *owner != businessID:
		return Gateway{}, api.NewError(http.StatusConflict, "gateway_claimed_elsewhere", "This gateway belongs to another business")
	default:
		err = tx.QueryRow(ctx,
			"UPDATE gateways SET name = $3, key_hash = $2 WHERE hardware_id = $1 RETURNING id, name, last_seen_at",
			hardwareID, keyHash, name).Scan(&gw.ID, &gw.Name, &gw.LastSeenAt)
	}

	if err != nil {
		var pgErr *pgconn.PgError
		if errors.As(err, &pgErr) && pgErr.Code == uniqueViolation {
			return Gateway{}, api.NewError(http.StatusConflict, "key_in_use", "This key belongs to another gateway")
		}

		return Gateway{}, fmt.Errorf("save the gateway %w", err)
	}

	if _, err := tx.Exec(ctx, "UPDATE gateway_enrollments SET gateway_id = $2 WHERE code_hash = $1", hashOf(code), gw.ID); err != nil {
		return Gateway{}, fmt.Errorf("mark the code as used %w", err)
	}

	if err := tx.Commit(ctx); err != nil {
		return Gateway{}, fmt.Errorf("finish the claim %w", err)
	}

	return gw, nil
}

// List returns the gateways of a business by name
func (s *Service) List(ctx context.Context, businessID string) ([]Gateway, error) {
	rows, err := s.pool.Query(ctx, "SELECT id, name, last_seen_at FROM gateways WHERE business_id = $1 ORDER BY lower(name), id", businessID)
	if err != nil {
		return nil, fmt.Errorf("list the gateways %w", err)
	}
	defer rows.Close()

	out := []Gateway{}

	for rows.Next() {
		var g Gateway
		if err := rows.Scan(&g.ID, &g.Name, &g.LastSeenAt); err != nil {
			return nil, fmt.Errorf("read a gateway %w", err)
		}

		out = append(out, g)
	}

	return out, rows.Err()
}
