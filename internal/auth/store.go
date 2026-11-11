package auth

import (
	"bytes"
	"context"
	"errors"
	"fmt"
	"time"

	"github.com/jackc/pgx/v5"
	"github.com/jackc/pgx/v5/pgconn"
	"github.com/jackc/pgx/v5/pgxpool"
)

// The lifetimes of a session and of the links in the emails
const (
	sessionIdle        = 30 * 24 * time.Hour
	sessionAbsolute    = 90 * 24 * time.Hour
	sessionTouchAfter  = 10 * time.Minute
	maxSessionsForUser = 20
	verifyTTL          = 24 * time.Hour
	resetTTL           = time.Hour
)

// The purposes of an email token
const (
	purposeVerify = "verify"
	purposeReset  = "reset"
)

const uniqueViolation = "23505"

var (
	// ErrEmailTaken is returned when an account with the email exists
	ErrEmailTaken = errors.New("the email has an account")
	// ErrTokenInvalid is returned for a token that is unknown, used or expired
	ErrTokenInvalid = errors.New("the token is unknown, used or expired")
	// ErrNoSession is returned for a session that is unknown or ended
	ErrNoSession = errors.New("no session")
)

// User is an account with the name of its business
type User struct {
	ID           string
	BusinessID   string
	Email        string
	PasswordHash *string
	Verified     bool
	BusinessName *string
}

// Store reads and writes the accounts, the sessions and the tokens
type Store struct {
	pool *pgxpool.Pool
}

// NewStore returns a store on the pool
func NewStore(pool *pgxpool.Pool) *Store {
	return &Store{pool: pool}
}

const userColumns = `u.id, u.business_id, u.email, u.password_hash, u.email_verified_at IS NOT NULL, b.name`

// scanUser reads the columns of userColumns, and then any extra columns that the query adds after them
func scanUser(row pgx.Row, extra ...any) (*User, error) {
	var u User

	dest := append([]any{&u.ID, &u.BusinessID, &u.Email, &u.PasswordHash, &u.Verified, &u.BusinessName}, extra...)

	if err := row.Scan(dest...); err != nil {
		return nil, err //nolint:wrapcheck // the caller tests for pgx.ErrNoRows
	}

	return &u, nil
}

// CreateAccount makes a business and its owner in one transaction
func (s *Store) CreateAccount(ctx context.Context, businessName, email, passwordHash, consentVersion string) (string, error) {
	tx, err := s.pool.BeginTx(ctx, pgx.TxOptions{})
	if err != nil {
		return "", fmt.Errorf("start the sign up %w", err)
	}

	defer func() { _ = tx.Rollback(ctx) }()

	var businessID, userID string

	if err := tx.QueryRow(ctx, "INSERT INTO businesses (name) VALUES ($1) RETURNING id", businessName).Scan(&businessID); err != nil {
		return "", fmt.Errorf("create the business %w", err)
	}

	err = tx.QueryRow(ctx,
		"INSERT INTO users (business_id, email, password_hash, consent_version) VALUES ($1, $2, $3, $4) RETURNING id",
		businessID, email, passwordHash, consentVersion).Scan(&userID)
	if err != nil {
		var pgErr *pgconn.PgError
		if errors.As(err, &pgErr) && pgErr.Code == uniqueViolation {
			return "", ErrEmailTaken
		}

		return "", fmt.Errorf("create the user %w", err)
	}

	if err := tx.Commit(ctx); err != nil {
		return "", fmt.Errorf("finish the sign up %w", err)
	}

	return userID, nil
}

// UserByEmail returns the user with the email, or nil when there is none
func (s *Store) UserByEmail(ctx context.Context, email string) (*User, error) {
	u, err := scanUser(s.pool.QueryRow(ctx,
		"SELECT "+userColumns+" FROM users u JOIN businesses b ON b.id = u.business_id WHERE u.email = $1", email))
	if errors.Is(err, pgx.ErrNoRows) {
		return nil, nil //nolint:nilnil // nil means that no user has the email
	}

	if err != nil {
		return nil, fmt.Errorf("read the user %w", err)
	}

	return u, nil
}

// UserByID returns the user with the id
func (s *Store) UserByID(ctx context.Context, id string) (*User, error) {
	u, err := scanUser(s.pool.QueryRow(ctx,
		"SELECT "+userColumns+" FROM users u JOIN businesses b ON b.id = u.business_id WHERE u.id = $1", id))
	if err != nil {
		return nil, fmt.Errorf("read the user %w", err)
	}

	return u, nil
}

// SaveToken stores a token for a user and the address that the link goes to, and removes the unused tokens of the same purpose, so
// only the newest link works
func (s *Store) SaveToken(ctx context.Context, userID, email, purpose string, hash []byte, ttl time.Duration) error {
	tx, err := s.pool.BeginTx(ctx, pgx.TxOptions{})
	if err != nil {
		return fmt.Errorf("start the token save %w", err)
	}

	defer func() { _ = tx.Rollback(ctx) }()

	if err := saveToken(ctx, tx, userID, email, purpose, hash, ttl); err != nil {
		return err
	}

	return tx.Commit(ctx)
}

func saveToken(ctx context.Context, tx pgx.Tx, userID, email, purpose string, hash []byte, ttl time.Duration) error {
	// the row lock orders this save after a change of the address, and a save for an address that is gone does nothing, so a late
	// resend cannot delete the link that went to the new address
	var current string
	if err := tx.QueryRow(ctx, "SELECT email FROM users WHERE id = $1 FOR UPDATE", userID).Scan(&current); err != nil {
		return fmt.Errorf("lock the user %w", err)
	}

	if current != email {
		return nil
	}

	if _, err := tx.Exec(ctx, "DELETE FROM email_tokens WHERE user_id = $1 AND purpose = $2 AND used_at IS NULL", userID, purpose); err != nil {
		return fmt.Errorf("remove the older tokens %w", err)
	}

	if _, err := tx.Exec(ctx,
		"INSERT INTO email_tokens (token_hash, user_id, purpose, email, expires_at) VALUES ($1, $2, $3, $4, now() + make_interval(secs => $5))",
		hash, userID, purpose, email, ttl.Seconds()); err != nil {
		return fmt.Errorf("store the token %w", err)
	}

	return nil
}

// TokenValid reports whether a token is unused, not expired and still bound to the address of its account. It does not use the token.
func (s *Store) TokenValid(ctx context.Context, purpose string, hash []byte) (bool, error) {
	var ok bool

	err := s.pool.QueryRow(ctx,
		`SELECT EXISTS (SELECT 1 FROM email_tokens t JOIN users u ON u.id = t.user_id
		 WHERE t.token_hash = $1 AND t.purpose = $2 AND t.used_at IS NULL AND t.expires_at > now() AND u.email = t.email)`,
		hash, purpose).Scan(&ok)
	if err != nil {
		return false, fmt.Errorf("read the token %w", err)
	}

	return ok, nil
}

// consumeToken marks an unused, unexpired token as used and returns its user
func consumeToken(ctx context.Context, tx pgx.Tx, purpose string, hash []byte) (string, error) {
	var userID string

	// the user row is locked before the token row, the same order as a save of a token, so the two never wait for each other
	err := tx.QueryRow(ctx,
		"SELECT u.id FROM users u WHERE u.id = (SELECT user_id FROM email_tokens WHERE token_hash = $1 AND purpose = $2) FOR UPDATE",
		hash, purpose).Scan(&userID)
	if errors.Is(err, pgx.ErrNoRows) {
		return "", ErrTokenInvalid
	}

	if err != nil {
		return "", fmt.Errorf("lock the user %w", err)
	}

	// the address of the token must still be the address of the account, so a link that went to one mailbox never acts on an account that
	// moved to another address
	err = tx.QueryRow(ctx,
		`UPDATE email_tokens t SET used_at = now() FROM users u
		 WHERE t.token_hash = $1 AND t.purpose = $2 AND t.used_at IS NULL AND t.expires_at > now() AND u.id = t.user_id AND u.email = t.email
		 RETURNING t.user_id`, hash, purpose).Scan(&userID)
	if errors.Is(err, pgx.ErrNoRows) {
		return "", ErrTokenInvalid
	}

	if err != nil {
		return "", fmt.Errorf("use the token %w", err)
	}

	return userID, nil
}

// VerifyEmail uses a verification token and marks the email of its user as verified
func (s *Store) VerifyEmail(ctx context.Context, hash []byte) (string, error) {
	tx, err := s.pool.BeginTx(ctx, pgx.TxOptions{})
	if err != nil {
		return "", fmt.Errorf("start the verification %w", err)
	}

	defer func() { _ = tx.Rollback(ctx) }()

	userID, err := consumeToken(ctx, tx, purposeVerify, hash)
	if err != nil {
		return "", err
	}

	if _, err := tx.Exec(ctx, "UPDATE users SET email_verified_at = COALESCE(email_verified_at, now()) WHERE id = $1", userID); err != nil {
		return "", fmt.Errorf("mark the email as verified %w", err)
	}

	if err := tx.Commit(ctx); err != nil {
		return "", fmt.Errorf("finish the verification %w", err)
	}

	return userID, nil
}

// ResetPassword uses a reset token, sets the new password hash, ends every session of the user and removes the other reset tokens.
// A reset proves that the person controls the mailbox, so it also verifies the email.
func (s *Store) ResetPassword(ctx context.Context, hash []byte, passwordHash string) error {
	tx, err := s.pool.BeginTx(ctx, pgx.TxOptions{})
	if err != nil {
		return fmt.Errorf("start the reset %w", err)
	}

	defer func() { _ = tx.Rollback(ctx) }()

	userID, err := consumeToken(ctx, tx, purposeReset, hash)
	if err != nil {
		return err
	}

	if _, err := tx.Exec(ctx,
		"UPDATE users SET password_hash = $2, email_verified_at = COALESCE(email_verified_at, now()) WHERE id = $1", userID, passwordHash); err != nil {
		return fmt.Errorf("save the password %w", err)
	}

	if _, err := tx.Exec(ctx, "DELETE FROM sessions WHERE user_id = $1", userID); err != nil {
		return fmt.Errorf("end the sessions %w", err)
	}

	if _, err := tx.Exec(ctx, "DELETE FROM email_tokens WHERE user_id = $1 AND purpose = $2 AND used_at IS NULL", userID, purposeReset); err != nil {
		return fmt.Errorf("remove the other tokens %w", err)
	}

	return tx.Commit(ctx)
}

// ChangeEmail moves an account that is not verified yet to a new address and stores the verification token for that address, in one
// transaction. It removes every unused token of the account, so no link that went to the old address can act on the new one. It
// reports false for an account that is verified.
func (s *Store) ChangeEmail(ctx context.Context, userID, email string, hash []byte, ttl time.Duration) (bool, error) {
	tx, err := s.pool.BeginTx(ctx, pgx.TxOptions{})
	if err != nil {
		return false, fmt.Errorf("start the change %w", err)
	}

	defer func() { _ = tx.Rollback(ctx) }()

	// the row is locked, so a resend that read the old address cannot save its token after the change
	var verified bool
	if err := tx.QueryRow(ctx, "SELECT email_verified_at IS NOT NULL FROM users WHERE id = $1 FOR UPDATE", userID).Scan(&verified); err != nil {
		return false, fmt.Errorf("lock the user %w", err)
	}

	if verified {
		return false, nil
	}

	if _, err := tx.Exec(ctx, "UPDATE users SET email = $2 WHERE id = $1", userID, email); err != nil {
		var pgErr *pgconn.PgError
		if errors.As(err, &pgErr) && pgErr.Code == uniqueViolation {
			return false, ErrEmailTaken
		}

		return false, fmt.Errorf("change the email %w", err)
	}

	if _, err := tx.Exec(ctx, "DELETE FROM email_tokens WHERE user_id = $1 AND used_at IS NULL", userID); err != nil {
		return false, fmt.Errorf("remove the tokens %w", err)
	}

	if err := saveToken(ctx, tx, userID, email, purposeVerify, hash, ttl); err != nil {
		return false, err
	}

	return true, tx.Commit(ctx)
}

// CreateSession stores a session and keeps at most maxSessionsForUser for the user, so a stolen password cannot fill the table
func (s *Store) CreateSession(ctx context.Context, userID string, hash []byte, label string) error {
	tx, err := s.pool.BeginTx(ctx, pgx.TxOptions{})
	if err != nil {
		return fmt.Errorf("start the session save %w", err)
	}

	defer func() { _ = tx.Rollback(ctx) }()

	if _, err := tx.Exec(ctx, "INSERT INTO sessions (token_hash, user_id, label) VALUES ($1, $2, $3)", hash, userID, label); err != nil {
		return fmt.Errorf("store the session %w", err)
	}

	if _, err := tx.Exec(ctx,
		`DELETE FROM sessions WHERE user_id = $1 AND token_hash NOT IN
		 (SELECT token_hash FROM sessions WHERE user_id = $1 ORDER BY last_used_at DESC LIMIT $2)`, userID, maxSessionsForUser); err != nil {
		return fmt.Errorf("trim the sessions %w", err)
	}

	return tx.Commit(ctx)
}

// SessionUser returns the user of a session that is inside both its limits, or ErrNoSession. One query reads the session, the user and
// the business, because every request that is signed in pays for it and each query takes a connection of a small pool. It refreshes the
// use time at most every sessionTouchAfter, so a busy client does not write on every request.
func (s *Store) SessionUser(ctx context.Context, hash []byte) (*User, error) {
	var stale bool

	u, err := scanUser(s.pool.QueryRow(ctx,
		`SELECT `+userColumns+`, s.last_used_at < now() - make_interval(secs => $4)
		 FROM sessions s JOIN users u ON u.id = s.user_id JOIN businesses b ON b.id = u.business_id
		 WHERE s.token_hash = $1 AND s.last_used_at > now() - make_interval(secs => $2) AND s.created_at > now() - make_interval(secs => $3)`,
		hash, sessionIdle.Seconds(), sessionAbsolute.Seconds(), sessionTouchAfter.Seconds()), &stale)
	if errors.Is(err, pgx.ErrNoRows) {
		return nil, ErrNoSession
	}

	if err != nil {
		return nil, fmt.Errorf("read the session %w", err)
	}

	if stale {
		// a failed refresh does not end a request that has a valid session
		_, _ = s.pool.Exec(ctx, "UPDATE sessions SET last_used_at = now() WHERE token_hash = $1 AND last_used_at < now() - make_interval(secs => $2)",
			hash, sessionTouchAfter.Seconds())
	}

	return u, nil
}

// SessionInfo is one open session of a user as the list shows it
type SessionInfo struct {
	ID         string    `json:"id"`
	Label      string    `json:"label"`
	CreatedAt  time.Time `json:"createdAt"`
	LastUsedAt time.Time `json:"lastUsedAt"`
	Current    bool      `json:"current"`
}

// Sessions lists the sessions of a user that are inside both their limits, the one of the request first and then the others by the
// newest use. A session past a limit that the clean up has not removed yet is not shown, because it can no longer sign in.
func (s *Store) Sessions(ctx context.Context, userID string, current []byte) ([]SessionInfo, error) {
	rows, err := s.pool.Query(ctx,
		`SELECT id::text, label, created_at, last_used_at, token_hash = $2 FROM sessions
		 WHERE user_id = $1 AND last_used_at > now() - make_interval(secs => $3) AND created_at > now() - make_interval(secs => $4)
		 ORDER BY (token_hash = $2) DESC, last_used_at DESC, created_at DESC LIMIT $5`,
		userID, current, sessionIdle.Seconds(), sessionAbsolute.Seconds(), maxSessionsForUser)
	if err != nil {
		return nil, fmt.Errorf("list the sessions %w", err)
	}

	defer rows.Close()

	out := []SessionInfo{}

	for rows.Next() {
		var i SessionInfo
		if err := rows.Scan(&i.ID, &i.Label, &i.CreatedAt, &i.LastUsedAt, &i.Current); err != nil {
			return nil, fmt.Errorf("read a session %w", err)
		}

		out = append(out, i)
	}

	if err := rows.Err(); err != nil {
		return nil, fmt.Errorf("read the sessions %w", err)
	}

	return out, nil
}

// DeleteSessionByID ends one session of the user. It reports whether the session existed and whether it was the one of the request,
// and the id of another user finds nothing.
func (s *Store) DeleteSessionByID(ctx context.Context, userID, id string, current []byte) (found, wasCurrent bool, err error) {
	var hash []byte

	err = s.pool.QueryRow(ctx, "DELETE FROM sessions WHERE user_id = $1 AND id = $2 RETURNING token_hash", userID, id).Scan(&hash)
	if errors.Is(err, pgx.ErrNoRows) {
		return false, false, nil
	}

	if err != nil {
		return false, false, fmt.Errorf("end the session %w", err)
	}

	return true, bytes.Equal(hash, current), nil
}

// DeleteOtherSessions ends every session of the user except the one of the request. It returns how many of them could still sign in,
// which is what the list showed. A row past a limit that the clean up has not removed yet is deleted too and is not counted.
func (s *Store) DeleteOtherSessions(ctx context.Context, userID string, current []byte) (int, error) {
	var ended int

	err := s.pool.QueryRow(ctx,
		`WITH gone AS (
		   DELETE FROM sessions WHERE user_id = $1 AND token_hash <> $2 RETURNING last_used_at, created_at
		 )
		 SELECT count(*) FILTER (WHERE last_used_at > now() - make_interval(secs => $3) AND created_at > now() - make_interval(secs => $4)) FROM gone`,
		userID, current, sessionIdle.Seconds(), sessionAbsolute.Seconds()).Scan(&ended)
	if err != nil {
		return 0, fmt.Errorf("end the other sessions %w", err)
	}

	return ended, nil
}

// deleteBatch is the number of rows that one statement removes from a big table before the business row goes
const deleteBatch = 5000

// bigTablesOfABusiness are the tables that can hold millions of rows for one business, with the statement that removes one batch of them
var bigTablesOfABusiness = []string{
	"DELETE FROM readings WHERE ctid IN (SELECT ctid FROM readings WHERE gateway_id IN (SELECT id FROM gateways WHERE business_id = $1) LIMIT $2)",
	"DELETE FROM readings_hourly WHERE ctid IN (SELECT ctid FROM readings_hourly WHERE gateway_id IN (SELECT id FROM gateways WHERE business_id = $1) LIMIT $2)",
	"DELETE FROM anomalies WHERE ctid IN (SELECT ctid FROM anomalies WHERE gateway_id IN (SELECT id FROM gateways WHERE business_id = $1) LIMIT $2)",
}

// CutOffGateways stops every hub of the business from sending, as the delete of one gateway does, so no hub adds readings while the
// batches of a delete run. A code that waits for the same hardware goes too, because a claim of it would bring a cut key back. The
// rows are locked in the order of the claim, the waiting codes first and the gateways after them.
func (s *Store) CutOffGateways(ctx context.Context, businessID string) error {
	tx, err := s.pool.BeginTx(ctx, pgx.TxOptions{})
	if err != nil {
		return fmt.Errorf("start the cut off of the gateways %w", err)
	}

	defer func() { _ = tx.Rollback(ctx) }()

	if _, err := tx.Exec(ctx,
		"DELETE FROM gateway_enrollments WHERE gateway_id IS NULL AND hardware_id IN (SELECT hardware_id FROM gateways WHERE business_id = $1)", businessID); err != nil {
		return fmt.Errorf("remove the waiting codes of the gateways %w", err)
	}

	// the key hash becomes a value that no key has, and the revoked time is what stops the hub
	if _, err := tx.Exec(ctx,
		"UPDATE gateways SET key_hash = sha256(convert_to(gen_random_uuid()::text, 'UTF8')), key_revoked_at = now() WHERE business_id = $1 AND key_revoked_at IS NULL", businessID); err != nil {
		return fmt.Errorf("cut off the gateways %w", err)
	}

	if err := tx.Commit(ctx); err != nil {
		return fmt.Errorf("finish the cut off of the gateways %w", err)
	}

	return nil
}

// DeleteBusiness deletes the business, and every other table under it goes with it because the keys cascade. A business can hold millions
// of readings, and one statement for all of them could run past the deadline of the request every time, so the big tables go first in
// batches that each commit by themselves, as the janitor does. The hubs are cut off first. A call that stops half way has cut the hubs and
// deleted a part of the readings and nothing else, and the next call goes on from there. The business row goes last, in one statement.
func (s *Store) DeleteBusiness(ctx context.Context, businessID string) error {
	if err := s.CutOffGateways(ctx, businessID); err != nil {
		return err
	}

	for _, query := range bigTablesOfABusiness {
		for {
			tag, err := s.pool.Exec(ctx, query, businessID, deleteBatch)
			if err != nil {
				return fmt.Errorf("delete a batch of rows of the business %w", err)
			}

			// a batch can come back short when a concurrent writer took rows of it, so only an empty batch ends the loop
			// and any row that is left goes with the cascade of the business row
			if tag.RowsAffected() == 0 {
				break
			}
		}
	}

	if _, err := s.pool.Exec(ctx, "DELETE FROM businesses WHERE id = $1", businessID); err != nil {
		return fmt.Errorf("delete the business %w", err)
	}

	return nil
}

// DeleteSession ends one session
func (s *Store) DeleteSession(ctx context.Context, hash []byte) error {
	if _, err := s.pool.Exec(ctx, "DELETE FROM sessions WHERE token_hash = $1", hash); err != nil {
		return fmt.Errorf("end the session %w", err)
	}

	return nil
}
