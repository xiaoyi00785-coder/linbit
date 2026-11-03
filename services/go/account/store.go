package main

import (
	"context"
	"crypto/sha256"
	"database/sql"
	"encoding/hex"
	"errors"
	"strings"
	"time"

	"github.com/jackc/pgx/v5/pgconn"
)

// ErrEmailExists is returned when registering a duplicate email.
var ErrEmailExists = errors.New("email already registered")

// ErrNotFound is returned when a user or token lookup misses.
var ErrNotFound = errors.New("not found")

// User is the persisted account record.
type User struct {
	ID           int64
	UID          string
	Email        string
	PasswordHash string
	Nickname     sql.NullString
	KYCLevel     int
	TOTPSecret   string
	TOTPEnabled  bool
	CreatedAt    time.Time
}

// Store wraps the account database.
type Store struct {
	db *sql.DB
}

// NewStore constructs a Store.
func NewStore(db *sql.DB) *Store { return &Store{db: db} }

// CreateUser inserts a new user and returns the created row (uid assigned by DB).
func (s *Store) CreateUser(ctx context.Context, email, passwordHash string) (*User, error) {
	var u User
	err := s.db.QueryRowContext(ctx,
		`INSERT INTO users (email, password_hash)
		 VALUES ($1, $2)
		 RETURNING id, uid, email, password_hash, nickname, kyc_level, totp_secret, totp_enabled, created_at`,
		strings.ToLower(email), passwordHash,
	).Scan(&u.ID, &u.UID, &u.Email, &u.PasswordHash, &u.Nickname, &u.KYCLevel, &u.TOTPSecret, &u.TOTPEnabled, &u.CreatedAt)
	if err != nil {
		if isUniqueViolation(err) {
			return nil, ErrEmailExists
		}
		return nil, err
	}
	return &u, nil
}

// UserByEmail looks up a user by email.
func (s *Store) UserByEmail(ctx context.Context, email string) (*User, error) {
	return s.scanUser(s.db.QueryRowContext(ctx, userSelectByEmail, strings.ToLower(email)))
}

// UserByUID looks up a user by public uid.
func (s *Store) UserByUID(ctx context.Context, uid string) (*User, error) {
	return s.scanUser(s.db.QueryRowContext(ctx, userSelectByUID, uid))
}

const userColumns = `id, uid, email, password_hash, nickname, kyc_level, totp_secret, totp_enabled, created_at`
const userSelectByEmail = `SELECT ` + userColumns + ` FROM users WHERE email = $1`
const userSelectByUID = `SELECT ` + userColumns + ` FROM users WHERE uid = $1`

func (s *Store) scanUser(row *sql.Row) (*User, error) {
	var u User
	err := row.Scan(&u.ID, &u.UID, &u.Email, &u.PasswordHash, &u.Nickname, &u.KYCLevel, &u.TOTPSecret, &u.TOTPEnabled, &u.CreatedAt)
	if errors.Is(err, sql.ErrNoRows) {
		return nil, ErrNotFound
	}
	if err != nil {
		return nil, err
	}
	return &u, nil
}

// SetTOTPSecret stores a pending TOTP secret (not yet enabled).
func (s *Store) SetTOTPSecret(ctx context.Context, uid, secret string) error {
	_, err := s.db.ExecContext(ctx, `UPDATE users SET totp_secret = $2 WHERE uid = $1`, uid, secret)
	return err
}

// SetTOTPEnabled toggles 2FA; when disabling it also clears the secret.
func (s *Store) SetTOTPEnabled(ctx context.Context, uid string, enabled bool) error {
	if enabled {
		_, err := s.db.ExecContext(ctx, `UPDATE users SET totp_enabled = TRUE WHERE uid = $1`, uid)
		return err
	}
	_, err := s.db.ExecContext(ctx, `UPDATE users SET totp_enabled = FALSE, totp_secret = '' WHERE uid = $1`, uid)
	return err
}

// StoreRefreshToken persists the hash of an issued refresh token.
func (s *Store) StoreRefreshToken(ctx context.Context, userID int64, token string, expiresAt time.Time) error {
	_, err := s.db.ExecContext(ctx,
		`INSERT INTO refresh_tokens (user_id, token_hash, expires_at) VALUES ($1, $2, $3)`,
		userID, hashToken(token), expiresAt)
	return err
}

// ConsumeRefreshToken validates a refresh token, revokes it, and returns its user.
// Rotation: a used token cannot be replayed.
func (s *Store) ConsumeRefreshToken(ctx context.Context, token string) (*User, error) {
	tx, err := s.db.BeginTx(ctx, nil)
	if err != nil {
		return nil, err
	}
	defer tx.Rollback() //nolint:errcheck

	var id, userID int64
	err = tx.QueryRowContext(ctx,
		`SELECT id, user_id FROM refresh_tokens
		 WHERE token_hash = $1 AND revoked = FALSE AND expires_at > now()
		 FOR UPDATE`,
		hashToken(token)).Scan(&id, &userID)
	if errors.Is(err, sql.ErrNoRows) {
		return nil, ErrNotFound
	}
	if err != nil {
		return nil, err
	}
	if _, err := tx.ExecContext(ctx, `UPDATE refresh_tokens SET revoked = TRUE WHERE id = $1`, id); err != nil {
		return nil, err
	}
	u, err := s.scanUser(tx.QueryRowContext(ctx, userSelectByID, userID))
	if err != nil {
		return nil, err
	}
	if err := tx.Commit(); err != nil {
		return nil, err
	}
	return u, nil
}

const userSelectByID = `SELECT ` + userColumns + ` FROM users WHERE id = $1`

// RevokeAllRefreshTokens revokes every active refresh token for a user (logout).
func (s *Store) RevokeAllRefreshTokens(ctx context.Context, userID int64) error {
	_, err := s.db.ExecContext(ctx,
		`UPDATE refresh_tokens SET revoked = TRUE WHERE user_id = $1 AND revoked = FALSE`, userID)
	return err
}

func hashToken(token string) string {
	sum := sha256.Sum256([]byte(token))
	return hex.EncodeToString(sum[:])
}

// isUniqueViolation reports whether err is a Postgres unique-constraint error.
func isUniqueViolation(err error) bool {
	var pgErr *pgconn.PgError
	return errors.As(err, &pgErr) && pgErr.Code == "23505" // unique_violation
}
