package gateway

import (
	"context"
	"errors"
	"fmt"
	"math"
	"net/http"
	"regexp"
	"strings"
	"time"

	"github.com/jackc/pgx/v5"

	"github.com/aethera-deeptech/backend/internal/api"
	"github.com/aethera-deeptech/backend/internal/auth"
)

// The limits of one batch of readings
const (
	MaxBodyBytes     = 256 << 10
	maxNodes         = 16
	maxSamplesNode   = 1000
	maxSamplesBatch  = 5000
	maxAge           = 24 * time.Hour
	maxFuture        = time.Minute
	clockSlack       = 5 * time.Second
	minUnix          = 1700000000
	maxWatts         = 60000
	maxVolts         = 500
	maxAmps          = 200
	maxHertz         = 100
	maxHops          = 3
	maxKWh           = 1e9
	nodeIDMinLength  = 6
	nodeIDMaxLength  = 32
	ingestPerHourMax = 600
)

var (
	nodeIDPattern = regexp.MustCompile(`^[A-Za-z0-9:_-]{6,32}$`)
	ruleIngest    = auth.Rule{Bucket: "ingest-gateway", Max: ingestPerHourMax, Window: time.Hour}
)

// Principal is a gateway that proved its key
type Principal struct {
	GatewayID  string
	BusinessID string
}

// Authenticate finds the gateway whose key hash is the hash of the bearer token. The token is never stored and never logged.
func (s *Service) Authenticate(ctx context.Context, header string) (*Principal, error) {
	token, ok := strings.CutPrefix(header, "Bearer ")
	if !ok || len(token) < 32 || len(token) > 128 {
		return nil, badSignature()
	}

	var p Principal

	err := s.pool.QueryRow(ctx, "SELECT id, business_id FROM gateways WHERE key_hash = $1", hashOf(token)).Scan(&p.GatewayID, &p.BusinessID)
	if errors.Is(err, pgx.ErrNoRows) {
		return nil, badSignature()
	}

	if err != nil {
		return nil, fmt.Errorf("read the gateway %w", err)
	}

	return &p, nil
}

func badSignature() error {
	return api.NewError(http.StatusUnauthorized, "bad_signature", "The key of the gateway is not valid")
}

// Sample is one reading of a node. The power is the only field that must be there.
type Sample struct {
	T  int64    `json:"t"`
	V  *float64 `json:"v"`
	I  *float64 `json:"i"`
	P  *float64 `json:"p"`
	E  *float64 `json:"e"`
	F  *float64 `json:"f"`
	PF *float64 `json:"pf"`
	A  *int     `json:"a"`
}

// NodeBatch holds the samples of one node
type NodeBatch struct {
	NodeID  string   `json:"nodeId"`
	Kind    string   `json:"kind"`
	Hops    int      `json:"hops"`
	Samples []Sample `json:"samples"`
}

// Batch is what a gateway sends
type Batch struct {
	Seq    int64       `json:"seq"`
	SentAt int64       `json:"sentAt"`
	Nodes  []NodeBatch `json:"nodes"`
}

// Ack is the answer to a batch that was stored, or that was stored before
type Ack struct {
	AckSeq     int64     `json:"ackSeq"`
	ServerTime time.Time `json:"serverTime"`
	Accepted   int       `json:"accepted"`
	Rejected   int       `json:"rejected"`
}

func inRange(v *float64, lo, hi float64) bool {
	return v == nil || (!math.IsNaN(*v) && *v >= lo && *v <= hi)
}

// validSample checks one sample against the limits of the sensors. It does not look at the time.
func validSample(s Sample) bool {
	return s.P != nil && inRange(s.P, 0, maxWatts) && inRange(s.V, 0, maxVolts) && inRange(s.I, 0, maxAmps) &&
		inRange(s.E, 0, maxKWh) && inRange(s.F, 0, maxHertz) && inRange(s.PF, 0, 1) && (s.A == nil || (*s.A >= 0 && *s.A <= 3))
}

// Ingest stores a batch. A batch is stored once. A retry of the last batch is answered again without storing it, and a batch that
// skips a number is refused with the number that the service waits for. A sample that is out of range, or that is older than a day,
// is dropped and counted, because a gateway that retried a batch with one bad sample would never move on.
func (s *Service) Ingest(ctx context.Context, p *Principal, b Batch, now time.Time) (Ack, error) {
	if err := s.allow(ctx, ruleIngest, p.GatewayID); err != nil {
		return Ack{}, err
	}

	if err := checkShape(b); err != nil {
		return Ack{}, err
	}

	// a gateway has no real time clock until it has asked a time server, so every time stamp moves by the difference of the clocks
	var shift time.Duration

	if sent := time.Unix(b.SentAt, 0); now.Sub(sent) > clockSlack || sent.Sub(now) > clockSlack {
		shift = now.Sub(sent)
	}

	tx, err := s.pool.BeginTx(ctx, pgx.TxOptions{})
	if err != nil {
		return Ack{}, fmt.Errorf("start the intake %w", err)
	}

	defer func() { _ = tx.Rollback(ctx) }()

	var last int64
	if err := tx.QueryRow(ctx, "SELECT last_seq FROM gateways WHERE id = $1 FOR UPDATE", p.GatewayID).Scan(&last); err != nil {
		return Ack{}, fmt.Errorf("lock the gateway %w", err)
	}

	switch {
	case b.Seq <= last:
		return Ack{AckSeq: last, ServerTime: now}, nil
	case b.Seq > last+1:
		e := api.NewError(http.StatusConflict, "sequence_gap", "The batch skips a sequence number")
		expected := last + 1
		e.ExpectedSeq = &expected

		return Ack{}, e
	}

	ack := Ack{AckSeq: b.Seq, ServerTime: now}

	for _, n := range b.Nodes {
		if err := upsertNode(ctx, tx, p.GatewayID, n, now); err != nil {
			return Ack{}, err
		}

		accepted, rejected, err := storeSamples(ctx, tx, p.GatewayID, n, shift, now)
		if err != nil {
			return Ack{}, err
		}

		ack.Accepted += accepted
		ack.Rejected += rejected
	}

	if _, err := tx.Exec(ctx, "UPDATE gateways SET last_seq = $2, last_seen_at = $3 WHERE id = $1", p.GatewayID, b.Seq, now); err != nil {
		return Ack{}, fmt.Errorf("move the sequence number %w", err)
	}

	if err := tx.Commit(ctx); err != nil {
		return Ack{}, fmt.Errorf("finish the intake %w", err)
	}

	return ack, nil
}

// checkShape refuses a batch that is built wrong. The limits keep one request from costing more than the service plans for.
func checkShape(b Batch) error {
	fields := map[string]string{}

	if b.Seq < 1 {
		fields["seq"] = "out_of_range"
	}

	if b.SentAt < minUnix {
		fields["sentAt"] = "out_of_range"
	}

	if len(b.Nodes) == 0 || len(b.Nodes) > maxNodes {
		fields["nodes"] = "out_of_range"
	}

	total := 0
	seen := map[string]bool{}

	for _, n := range b.Nodes {
		switch {
		case !nodeIDPattern.MatchString(n.NodeID):
			fields["nodes.nodeId"] = "invalid"
		case seen[n.NodeID]:
			fields["nodes.nodeId"] = "duplicate"
		case n.Kind != "plug" && n.Kind != "clamp":
			fields["nodes.kind"] = "invalid"
		case n.Hops < 0 || n.Hops > maxHops:
			fields["nodes.hops"] = "out_of_range"
		case len(n.Samples) == 0 || len(n.Samples) > maxSamplesNode:
			fields["nodes.samples"] = "out_of_range"
		}

		seen[n.NodeID] = true
		total += len(n.Samples)
	}

	if total > maxSamplesBatch {
		fields["nodes.samples"] = "too_many"
	}

	if len(fields) > 0 {
		return api.Validation(fields)
	}

	return nil
}

func upsertNode(ctx context.Context, tx pgx.Tx, gatewayID string, n NodeBatch, now time.Time) error {
	if _, err := tx.Exec(ctx,
		`INSERT INTO nodes (gateway_id, node_id, kind, hops, first_seen_at, last_seen_at) VALUES ($1, $2, $3, $4, $5, $5)
		 ON CONFLICT (gateway_id, node_id) DO UPDATE SET hops = EXCLUDED.hops, last_seen_at = EXCLUDED.last_seen_at`,
		gatewayID, n.NodeID, n.Kind, n.Hops, now); err != nil {
		return fmt.Errorf("save the node %w", err)
	}

	return nil
}

// storeSamples writes the samples that pass the checks, in one statement, and moves the state of the node to the newest one
func storeSamples(ctx context.Context, tx pgx.Tx, gatewayID string, n NodeBatch, shift time.Duration, now time.Time) (int, int, error) {
	var (
		ts                                     []time.Time
		volts, amps, watts, energy, hertz, pfs []*float64
		states                                 []*int32
		rejected                               int
	)

	for _, s := range n.Samples {
		t := time.Unix(s.T, 0).Add(shift)

		if !validSample(s) || t.Before(now.Add(-maxAge)) || t.After(now.Add(maxFuture)) {
			rejected++

			continue
		}

		ts = append(ts, t.UTC())
		volts, amps, watts = append(volts, s.V), append(amps, s.I), append(watts, s.P)
		energy, hertz, pfs = append(energy, s.E), append(hertz, s.F), append(pfs, s.PF)

		var st *int32
		if s.A != nil {
			v := int32(*s.A) //nolint:gosec // checked to be 0 to 3
			st = &v
		}

		states = append(states, st)
	}

	if len(ts) == 0 {
		return 0, rejected, nil
	}

	tag, err := tx.Exec(ctx,
		`INSERT INTO readings (gateway_id, node_id, t, volts, amps, watts, energy_kwh, hertz, power_factor, node_state)
		 SELECT $1, $2, x.t, x.v, x.i, x.p, x.e, x.f, x.pf, x.a
		 FROM unnest($3::timestamptz[], $4::float8[], $5::float8[], $6::float8[], $7::float8[], $8::float8[], $9::float8[], $10::int4[])
		   AS x(t, v, i, p, e, f, pf, a)
		 ON CONFLICT DO NOTHING`,
		gatewayID, n.NodeID, ts, volts, amps, watts, energy, hertz, pfs, states)
	if err != nil {
		return 0, 0, fmt.Errorf("store the samples %w", err)
	}

	return int(tag.RowsAffected()), rejected, nil
}
