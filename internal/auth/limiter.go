package auth

import (
	"context"
	"crypto/sha256"
	"encoding/hex"
	"errors"
	"fmt"
	"math/rand/v2"
	"time"

	"github.com/jackc/pgx/v5"
	"github.com/jackc/pgx/v5/pgconn"
	"github.com/jackc/pgx/v5/pgxpool"
)

// Rule is one limit, Max events in Window for each key in the bucket
type Rule struct {
	Bucket string
	Max    int
	Window time.Duration
}

// Limiter counts events in PostgreSQL, so both copies of the service share the numbers
type Limiter struct {
	pool *pgxpool.Pool
}

// NewLimiter returns a limiter on the pool
func NewLimiter(pool *pgxpool.Pool) *Limiter {
	return &Limiter{pool: pool}
}

// the longest window of a rule is one hour, so an event older than two hours counts for nothing
// lockNotAvailable is the code of PostgreSQL for a wait that ended at lock_timeout
const lockNotAvailable = "55P03"

// pruneEvery is how often a call also deletes the events that no window needs any more, one in this many
const pruneEvery = 50

// Allow counts one event for the key. It returns false and the time to wait when the key is over the rule. A refused event is not
// counted, so a client that keeps trying does not push its own wait further away. The key is stored as a hash, so the table holds
// no email and no address.
func (l *Limiter) Allow(ctx context.Context, rule Rule, key string) (bool, time.Duration, error) {
	sum := sha256.Sum256([]byte(key))
	hashed := hex.EncodeToString(sum[:16])

	tx, err := l.pool.BeginTx(ctx, pgx.TxOptions{})
	if err != nil {
		return false, 0, fmt.Errorf("start the limit check %w", err)
	}

	defer func() { _ = tx.Rollback(ctx) }()

	// a flood on one key must not hold the connections of the pool for long, so the wait for the key and for a statement is short
	if _, err := tx.Exec(ctx, "SET LOCAL lock_timeout = '2s'"); err != nil {
		return false, 0, fmt.Errorf("set the lock timeout %w", err)
	}

	// two requests for the same key take turns, so both cannot read the same count
	if _, err := tx.Exec(ctx, "SELECT pg_advisory_xact_lock(hashtextextended($1, 0))", rule.Bucket+"/"+hashed); err != nil {
		// a key that so many requests wait for is being hit in a flood, so the latecomers are told to wait and do not hold a connection
		var pgErr *pgconn.PgError
		if errors.As(err, &pgErr) && pgErr.Code == lockNotAvailable {
			return false, time.Second, nil
		}

		return false, 0, fmt.Errorf("lock the limit %w", err)
	}

	var (
		count  int
		oldest *time.Time
	)

	err = tx.QueryRow(ctx,
		"SELECT count(*), min(at) FROM rate_limit_events WHERE bucket = $1 AND key = $2 AND at > now() - make_interval(secs => $3)",
		rule.Bucket, hashed, rule.Window.Seconds()).Scan(&count, &oldest)
	if err != nil {
		return false, 0, fmt.Errorf("count the events %w", err)
	}

	if count >= rule.Max && oldest != nil {
		wait := time.Until(oldest.Add(rule.Window))

		return false, max(wait, time.Second), nil
	}

	if _, err := tx.Exec(ctx, "INSERT INTO rate_limit_events (bucket, key) VALUES ($1, $2)", rule.Bucket, hashed); err != nil {
		return false, 0, fmt.Errorf("record the event %w", err)
	}

	if rand.IntN(pruneEvery) == 0 { //nolint:gosec // this only spreads the clean up, it needs no secret
		if _, err := tx.Exec(ctx, "DELETE FROM rate_limit_events WHERE at < now() - interval '2 hours'"); err != nil {
			return false, 0, fmt.Errorf("clean up the events %w", err)
		}
	}

	if err := tx.Commit(ctx); err != nil {
		return false, 0, fmt.Errorf("finish the limit check %w", err)
	}

	return true, 0, nil
}
