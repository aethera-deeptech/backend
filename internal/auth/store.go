package auth

import (
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

func scanUser(row pgx.Row) (*User, error) {
	var u User

	if err := row.Scan(&u.ID, &u.BusinessID, &u.Email, &u.PasswordHash, &u.Verified, &u.BusinessName); err != nil {
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
func (s *Store) CreateSession(ctx context.Context, userID string, hash []byte) error {
	tx, err := s.pool.BeginTx(ctx, pgx.TxOptions{})
	if err != nil {
		return fmt.Errorf("start the session save %w", err)
	}

	defer func() { _ = tx.Rollback(ctx) }()

	if _, err := tx.Exec(ctx, "INSERT INTO sessions (token_hash, user_id) VALUES ($1, $2)", hash, userID); err != nil {
		return fmt.Errorf("store the session %w", err)
	}

	if _, err := tx.Exec(ctx,
		`DELETE FROM sessions WHERE user_id = $1 AND token_hash NOT IN
		 (SELECT token_hash FROM sessions WHERE user_id = $1 ORDER BY last_used_at DESC LIMIT $2)`, userID, maxSessionsForUser); err != nil {
		return fmt.Errorf("trim the sessions %w", err)
	}

	return tx.Commit(ctx)
}

// SessionUser returns the user of a session that is inside both its limits, or ErrNoSession. It refreshes the use time at most
// every sessionTouchAfter, so a busy client does not write on every request.
func (s *Store) SessionUser(ctx context.Context, hash []byte) (*User, error) {
	var userID string

	err := s.pool.QueryRow(ctx,
		`SELECT user_id FROM sessions
		 WHERE token_hash = $1 AND last_used_at > now() - make_interval(secs => $2) AND created_at > now() - make_interval(secs => $3)`,
		hash, sessionIdle.Seconds(), sessionAbsolute.Seconds()).Scan(&userID)
	if errors.Is(err, pgx.ErrNoRows) {
		return nil, ErrNoSession
	}

	if err != nil {
		return nil, fmt.Errorf("read the session %w", err)
	}

	// a failed refresh does not end a request that has a valid session
	_, _ = s.pool.Exec(ctx, "UPDATE sessions SET last_used_at = now() WHERE token_hash = $1 AND last_used_at < now() - make_interval(secs => $2)",
		hash, sessionTouchAfter.Seconds())

	return s.UserByID(ctx, userID)
}

// DeleteSession ends one session
func (s *Store) DeleteSession(ctx context.Context, hash []byte) error {
	if _, err := s.pool.Exec(ctx, "DELETE FROM sessions WHERE token_hash = $1", hash); err != nil {
		return fmt.Errorf("end the session %w", err)
	}

	return nil
}
