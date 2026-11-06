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

// ErrPhoneExists is returned when registering a duplicate phone.
var ErrPhoneExists = errors.New("phone already registered")

// ErrNotFound is returned when a user or token lookup misses.
var ErrNotFound = errors.New("not found")

// User is the persisted account record.
type User struct {
	ID                       int64
	UID                      string
	Email                    sql.NullString
	Phone                    sql.NullString
	CountryCode              string
	PasswordHash             string
	Nickname                 sql.NullString
	AvatarURL                string
	KYCLevel                 int
	TOTPSecret               string
	TOTPEnabled              bool
	EmailVerified            bool
	PhoneVerified            bool
	WithdrawWhitelistEnabled bool
	CreatedAt                time.Time
	Status                   string
	StatusReason             string
}

// Store wraps the account database.
type Store struct {
	db *sql.DB
}

// NewStore constructs a Store.
func NewStore(db *sql.DB) *Store { return &Store{db: db} }

// CreateUserParams carries the fields needed to register a new user. Exactly one
// of Email / Phone is set depending on the registration channel.
type CreateUserParams struct {
	Email         *string
	Phone         *string
	CountryCode   string
	PasswordHash  string
	EmailVerified bool
	PhoneVerified bool
	InviteCode    string
}

const userColumns = `id, uid, email, phone, country_code, password_hash, nickname, avatar_url,
	kyc_level, totp_secret, totp_enabled, email_verified, phone_verified,
	withdraw_whitelist_enabled, created_at, status, status_reason`

const userSelectByEmail = `SELECT ` + userColumns + ` FROM users WHERE email = $1`
const userSelectByPhone = `SELECT ` + userColumns + ` FROM users WHERE phone = $1`
const userSelectByUID = `SELECT ` + userColumns + ` FROM users WHERE uid = $1`
const userSelectByID = `SELECT ` + userColumns + ` FROM users WHERE id = $1`

// CreateUser inserts a new user and returns the created row (uid assigned by DB).
func (s *Store) CreateUser(ctx context.Context, p CreateUserParams) (*User, error) {
	var email, phone interface{}
	if p.Email != nil {
		email = strings.ToLower(*p.Email)
	}
	if p.Phone != nil {
		phone = *p.Phone
	}
	row := s.db.QueryRowContext(ctx,
		`INSERT INTO users (email, phone, country_code, password_hash, email_verified, phone_verified, invite_code)
		 VALUES ($1, $2, $3, $4, $5, $6, $7)
		 RETURNING `+userColumns,
		email, phone, p.CountryCode, p.PasswordHash, p.EmailVerified, p.PhoneVerified, normalizeInviteCode(p.InviteCode))
	u, err := s.scanUser(row)
	if err != nil {
		if constraint := uniqueConstraint(err); constraint != "" {
			if strings.Contains(constraint, "phone") {
				return nil, ErrPhoneExists
			}
			return nil, ErrEmailExists
		}
		return nil, err
	}
	return u, nil
}

// UserByEmail looks up a user by email.
func (s *Store) UserByEmail(ctx context.Context, email string) (*User, error) {
	return s.scanUser(s.db.QueryRowContext(ctx, userSelectByEmail, strings.ToLower(email)))
}

// UserByPhone looks up a user by E.164 phone.
func (s *Store) UserByPhone(ctx context.Context, phone string) (*User, error) {
	return s.scanUser(s.db.QueryRowContext(ctx, userSelectByPhone, phone))
}

// UserByUID looks up a user by public uid.
func (s *Store) UserIDByUID(ctx context.Context, uid string) (int64, error) {
	var id int64
	err := s.db.QueryRowContext(ctx, `SELECT id FROM users WHERE uid=$1 AND status='active'`, uid).Scan(&id)
	return id, err
}

func (s *Store) UserByUID(ctx context.Context, uid string) (*User, error) {
	return s.scanUser(s.db.QueryRowContext(ctx, userSelectByUID, uid))
}

// UserByID looks up a user by internal numeric id.
func (s *Store) UserByID(ctx context.Context, id int64) (*User, error) {
	return s.scanUser(s.db.QueryRowContext(ctx, userSelectByID, id))
}

func (s *Store) scanUser(row *sql.Row) (*User, error) {
	var u User
	err := row.Scan(&u.ID, &u.UID, &u.Email, &u.Phone, &u.CountryCode, &u.PasswordHash,
		&u.Nickname, &u.AvatarURL, &u.KYCLevel, &u.TOTPSecret, &u.TOTPEnabled,
		&u.EmailVerified, &u.PhoneVerified, &u.WithdrawWhitelistEnabled, &u.CreatedAt, &u.Status, &u.StatusReason)
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

// UpdatePassword replaces the password hash for a user.
func (s *Store) UpdatePassword(ctx context.Context, userID int64, passwordHash string) error {
	_, err := s.db.ExecContext(ctx, `UPDATE users SET password_hash = $2 WHERE id = $1`, userID, passwordHash)
	return err
}

// UpdateProfile updates nickname and/or avatar_url when the corresponding
// pointer is non-nil.
func (s *Store) UpdateProfile(ctx context.Context, userID int64, nickname, avatarURL *string) error {
	if nickname != nil {
		if _, err := s.db.ExecContext(ctx, `UPDATE users SET nickname = $2 WHERE id = $1`, userID, *nickname); err != nil {
			return err
		}
	}
	if avatarURL != nil {
		if _, err := s.db.ExecContext(ctx, `UPDATE users SET avatar_url = $2 WHERE id = $1`, userID, *avatarURL); err != nil {
			return err
		}
	}
	return nil
}

// SetKYCLevelAtLeast raises the user's kyc_level to level, never lowering it
// (an approval for a lower tier must not downgrade an already-higher entitlement).
func (s *Store) SetKYCLevelAtLeast(ctx context.Context, uid string, level int) error {
	_, err := s.db.ExecContext(ctx,
		`UPDATE users SET kyc_level = GREATEST(kyc_level, $2) WHERE uid = $1`, uid, level)
	return err
}

// SetWithdrawWhitelistEnabled toggles the per-user withdrawal whitelist switch.
func (s *Store) SetWithdrawWhitelistEnabled(ctx context.Context, userID int64, enabled bool) error {
	_, err := s.db.ExecContext(ctx, `UPDATE users SET withdraw_whitelist_enabled = $2 WHERE id = $1`, userID, enabled)
	return err
}

// ---- refresh tokens ----

// StoreRefreshToken persists the hash of an issued refresh token, tagged with
// the device it was issued to.
func (s *Store) StoreRefreshToken(ctx context.Context, userID int64, token, deviceKey string, expiresAt time.Time) error {
	_, err := s.db.ExecContext(ctx,
		`INSERT INTO refresh_tokens (user_id, token_hash, device_key, expires_at) VALUES ($1, $2, $3, $4)`,
		userID, hashToken(token), deviceKey, expiresAt)
	return err
}

// RefreshRecord is the result of consuming a refresh token: its owner plus the
// device it was bound to (so the rotated token keeps the same device).
type RefreshRecord struct {
	User      *User
	DeviceKey string
}

// ConsumeRefreshToken validates a refresh token, revokes it, and returns its
// user and device. Rotation: a used token cannot be replayed.
func (s *Store) ConsumeRefreshToken(ctx context.Context, token string) (*RefreshRecord, error) {
	tx, err := s.db.BeginTx(ctx, nil)
	if err != nil {
		return nil, err
	}
	defer tx.Rollback() //nolint:errcheck

	var id, userID int64
	var deviceKey string
	err = tx.QueryRowContext(ctx,
		`SELECT id, user_id, device_key FROM refresh_tokens
		 WHERE token_hash = $1 AND revoked = FALSE AND expires_at > now()
		 FOR UPDATE`,
		hashToken(token)).Scan(&id, &userID, &deviceKey)
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
	return &RefreshRecord{User: u, DeviceKey: deviceKey}, nil
}

// RevokeAllRefreshTokens revokes every active refresh token for a user (logout,
// password reset).
func (s *Store) RevokeAllRefreshTokens(ctx context.Context, userID int64) error {
	_, err := s.db.ExecContext(ctx,
		`UPDATE refresh_tokens SET revoked = TRUE WHERE user_id = $1 AND revoked = FALSE`, userID)
	return err
}

// RevokeDeviceRefreshTokens revokes active refresh tokens issued to one device.
func (s *Store) RevokeDeviceRefreshTokens(ctx context.Context, userID int64, deviceKey string) error {
	_, err := s.db.ExecContext(ctx,
		`UPDATE refresh_tokens SET revoked = TRUE WHERE user_id = $1 AND device_key = $2 AND revoked = FALSE`,
		userID, deviceKey)
	return err
}

// ---- password reset tokens ----

// StorePasswordResetToken persists the hash of a short-lived, single-use reset
// token bound to a user.
func (s *Store) StorePasswordResetToken(ctx context.Context, userID int64, token string, expiresAt time.Time) error {
	_, err := s.db.ExecContext(ctx,
		`INSERT INTO password_reset_tokens (token_hash, user_id, expires_at) VALUES ($1, $2, $3)`,
		hashToken(token), userID, expiresAt)
	return err
}

// ConsumePasswordResetToken validates a reset token and marks it consumed,
// returning the owning user id. A used or expired token yields ErrNotFound.
func (s *Store) ConsumePasswordResetToken(ctx context.Context, token string) (int64, error) {
	tx, err := s.db.BeginTx(ctx, nil)
	if err != nil {
		return 0, err
	}
	defer tx.Rollback() //nolint:errcheck

	var id, userID int64
	err = tx.QueryRowContext(ctx,
		`SELECT id, user_id FROM password_reset_tokens
		 WHERE token_hash = $1 AND consumed = FALSE AND expires_at > now()
		 FOR UPDATE`,
		hashToken(token)).Scan(&id, &userID)
	if errors.Is(err, sql.ErrNoRows) {
		return 0, ErrNotFound
	}
	if err != nil {
		return 0, err
	}
	if _, err := tx.ExecContext(ctx, `UPDATE password_reset_tokens SET consumed = TRUE WHERE id = $1`, id); err != nil {
		return 0, err
	}
	if err := tx.Commit(); err != nil {
		return 0, err
	}
	return userID, nil
}

// ---- verification codes ----

// InsertCode stores a hashed verification code.
func (s *Store) InsertCode(ctx context.Context, target, channel, purpose, codeHash string, maxAttempts int, expiresAt time.Time) error {
	_, err := s.db.ExecContext(ctx,
		`INSERT INTO verification_codes (target, channel, purpose, code_hash, max_attempts, expires_at)
		 VALUES ($1, $2, $3, $4, $5, $6)`,
		target, channel, purpose, codeHash, maxAttempts, expiresAt)
	return err
}

// DeleteCode removes an issued code (used when its delivery failed).
func (s *Store) DeleteCode(ctx context.Context, target, purpose, codeHash string) error {
	_, err := s.db.ExecContext(ctx,
		`DELETE FROM verification_codes WHERE target = $1 AND purpose = $2 AND code_hash = $3`,
		target, purpose, codeHash)
	return err
}

// CountCodesSince returns how many codes were issued for target+purpose since t.
func (s *Store) CountCodesSince(ctx context.Context, target, purpose string, since time.Time) (int, error) {
	var n int
	err := s.db.QueryRowContext(ctx,
		`SELECT count(*) FROM verification_codes WHERE target = $1 AND purpose = $2 AND created_at >= $3`,
		target, purpose, since).Scan(&n)
	return n, err
}

// codeVerdict enumerates the outcome of verifying a code.
type codeVerdict int

const (
	codeOK codeVerdict = iota
	codeInvalid
	codeExpiredOrMissing
	codeTooManyAttempts
)

// VerifyAndConsumeCode checks the most recent live code for target+purpose. On a
// match it marks the code consumed and returns codeOK. A mismatch increments the
// attempt counter (and consumes the code once the cap is hit).
func (s *Store) VerifyAndConsumeCode(ctx context.Context, target, purpose, codeHash string) (codeVerdict, error) {
	tx, err := s.db.BeginTx(ctx, nil)
	if err != nil {
		return codeInvalid, err
	}
	defer tx.Rollback() //nolint:errcheck

	var id int64
	var storedHash string
	var attempts, maxAttempts int
	err = tx.QueryRowContext(ctx,
		`SELECT id, code_hash, attempts, max_attempts FROM verification_codes
		 WHERE target = $1 AND purpose = $2 AND consumed = FALSE AND expires_at > now()
		 ORDER BY created_at DESC LIMIT 1 FOR UPDATE`,
		target, purpose).Scan(&id, &storedHash, &attempts, &maxAttempts)
	if errors.Is(err, sql.ErrNoRows) {
		return codeExpiredOrMissing, nil
	}
	if err != nil {
		return codeInvalid, err
	}
	if attempts >= maxAttempts {
		if _, err := tx.ExecContext(ctx, `UPDATE verification_codes SET consumed = TRUE WHERE id = $1`, id); err != nil {
			return codeInvalid, err
		}
		if err := tx.Commit(); err != nil {
			return codeInvalid, err
		}
		return codeTooManyAttempts, nil
	}
	if storedHash != codeHash {
		attempts++
		consumed := attempts >= maxAttempts
		if _, err := tx.ExecContext(ctx,
			`UPDATE verification_codes SET attempts = $2, consumed = $3 WHERE id = $1`, id, attempts, consumed); err != nil {
			return codeInvalid, err
		}
		if err := tx.Commit(); err != nil {
			return codeInvalid, err
		}
		if consumed {
			return codeTooManyAttempts, nil
		}
		return codeInvalid, nil
	}
	if _, err := tx.ExecContext(ctx, `UPDATE verification_codes SET consumed = TRUE WHERE id = $1`, id); err != nil {
		return codeInvalid, err
	}
	if err := tx.Commit(); err != nil {
		return codeInvalid, err
	}
	return codeOK, nil
}

// ---- devices ----

// Device is a recognised login device. Current flags the device making the
// request (set by the list handler, not persisted).
type Device struct {
	DeviceKey string    `json:"deviceId"`
	Name      string    `json:"name"`
	UserAgent string    `json:"userAgent"`
	IP        string    `json:"ip"`
	Current   bool      `json:"current"`
	FirstSeen time.Time `json:"firstSeen"`
	LastSeen  time.Time `json:"lastSeen"`
}

// DeviceExists reports whether the user already has this device fingerprint.
func (s *Store) DeviceExists(ctx context.Context, userID int64, deviceKey string) (bool, error) {
	var one int
	err := s.db.QueryRowContext(ctx,
		`SELECT 1 FROM devices WHERE user_id = $1 AND device_key = $2`, userID, deviceKey).Scan(&one)
	if errors.Is(err, sql.ErrNoRows) {
		return false, nil
	}
	return err == nil, err
}

// UpsertDevice records a device login, updating last_seen (and metadata) when it
// already exists.
func (s *Store) UpsertDevice(ctx context.Context, userID int64, d Device, now time.Time) error {
	_, err := s.db.ExecContext(ctx,
		`INSERT INTO devices (user_id, device_key, name, user_agent, ip, first_seen, last_seen)
		 VALUES ($1, $2, $3, $4, $5, $6, $6)
		 ON CONFLICT (user_id, device_key)
		 DO UPDATE SET last_seen = $6,
		               name = CASE WHEN excluded.name <> '' THEN excluded.name ELSE devices.name END,
		               user_agent = excluded.user_agent,
		               ip = excluded.ip`,
		userID, d.DeviceKey, d.Name, d.UserAgent, d.IP, now)
	return err
}

// ListDevices returns the user's devices, most-recently-seen first.
func (s *Store) ListDevices(ctx context.Context, userID int64) ([]Device, error) {
	rows, err := s.db.QueryContext(ctx,
		`SELECT device_key, name, user_agent, ip, first_seen, last_seen
		 FROM devices WHERE user_id = $1 ORDER BY last_seen DESC`, userID)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	var out []Device
	for rows.Next() {
		var d Device
		if err := rows.Scan(&d.DeviceKey, &d.Name, &d.UserAgent, &d.IP, &d.FirstSeen, &d.LastSeen); err != nil {
			return nil, err
		}
		d.FirstSeen, d.LastSeen = d.FirstSeen.UTC(), d.LastSeen.UTC() // always RFC3339 "Z"
		out = append(out, d)
	}
	return out, rows.Err()
}

// DeleteDevice removes a device and reports whether a row was deleted.
func (s *Store) DeleteDevice(ctx context.Context, userID int64, deviceKey string) (bool, error) {
	res, err := s.db.ExecContext(ctx, `DELETE FROM devices WHERE user_id = $1 AND device_key = $2`, userID, deviceKey)
	if err != nil {
		return false, err
	}
	n, _ := res.RowsAffected()
	return n > 0, nil
}

// ---- login challenges ----

// StoreLoginChallenge persists a hashed challenge token.
func (s *Store) StoreLoginChallenge(ctx context.Context, userID int64, token, method string, d Device, expiresAt time.Time) error {
	_, err := s.db.ExecContext(ctx,
		`INSERT INTO login_challenges (token_hash, user_id, method, device_key, device_name, user_agent, ip, expires_at)
		 VALUES ($1, $2, $3, $4, $5, $6, $7, $8)`,
		hashToken(token), userID, method, d.DeviceKey, d.Name, d.UserAgent, d.IP, expiresAt)
	return err
}

// LoginChallenge is a pending second-factor ticket.
type LoginChallenge struct {
	ID         int64
	UserID     int64
	Method     string
	DeviceKey  string
	DeviceName string
	UserAgent  string
	IP         string
}

// LookupLoginChallenge fetches a live (non-consumed, non-expired) challenge.
func (s *Store) LookupLoginChallenge(ctx context.Context, token string) (*LoginChallenge, error) {
	var c LoginChallenge
	err := s.db.QueryRowContext(ctx,
		`SELECT id, user_id, method, device_key, device_name, user_agent, ip FROM login_challenges
		 WHERE token_hash = $1 AND consumed = FALSE AND expires_at > now()`,
		hashToken(token)).Scan(&c.ID, &c.UserID, &c.Method, &c.DeviceKey, &c.DeviceName, &c.UserAgent, &c.IP)
	if errors.Is(err, sql.ErrNoRows) {
		return nil, ErrNotFound
	}
	if err != nil {
		return nil, err
	}
	return &c, nil
}

// ConsumeLoginChallenge marks a challenge consumed.
func (s *Store) ConsumeLoginChallenge(ctx context.Context, id int64) error {
	_, err := s.db.ExecContext(ctx, `UPDATE login_challenges SET consumed = TRUE WHERE id = $1`, id)
	return err
}

// ---- withdraw address book ----

// WithdrawAddress is an address-book entry.
type WithdrawAddress struct {
	ID          string    `json:"id"`
	Asset       string    `json:"asset"`
	Network     string    `json:"network"`
	Address     string    `json:"address"`
	Memo        string    `json:"memo"`
	Label       string    `json:"label"`
	Whitelisted bool      `json:"whitelisted"`
	CreatedAt   time.Time `json:"createdAt"`
}

// ErrAddressExists is returned when inserting a duplicate address-book entry.
var ErrAddressExists = errors.New("address already exists")

// CreateWithdrawAddress inserts an address-book entry and returns its id.
func (s *Store) CreateWithdrawAddress(ctx context.Context, userID int64, a WithdrawAddress) (string, error) {
	var id string
	err := s.db.QueryRowContext(ctx,
		`INSERT INTO withdraw_addresses (user_id, asset, network, address, memo, label, whitelisted)
		 VALUES ($1, $2, $3, $4, $5, $6, $7) RETURNING id::text`,
		userID, a.Asset, a.Network, a.Address, a.Memo, a.Label, a.Whitelisted).Scan(&id)
	if err != nil {
		if uniqueConstraint(err) != "" {
			return "", ErrAddressExists
		}
		return "", err
	}
	return id, nil
}

// ListWithdrawAddresses returns the user's address book, newest first.
func (s *Store) ListWithdrawAddresses(ctx context.Context, userID int64) ([]WithdrawAddress, error) {
	rows, err := s.db.QueryContext(ctx,
		`SELECT id::text, asset, network, address, memo, label, whitelisted, created_at
		 FROM withdraw_addresses WHERE user_id = $1 ORDER BY created_at DESC`, userID)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	var out []WithdrawAddress
	for rows.Next() {
		var a WithdrawAddress
		if err := rows.Scan(&a.ID, &a.Asset, &a.Network, &a.Address, &a.Memo, &a.Label, &a.Whitelisted, &a.CreatedAt); err != nil {
			return nil, err
		}
		out = append(out, a)
	}
	return out, rows.Err()
}

// IsWhitelistedAddress reports whether (asset, network, address) is an entry in
// the user's address book flagged as whitelisted. Used by the wallet service to
// enforce the withdrawal whitelist when it is enabled.
func (s *Store) IsWhitelistedAddress(ctx context.Context, userID int64, asset, network, address string) (bool, error) {
	var one int
	err := s.db.QueryRowContext(ctx,
		`SELECT 1 FROM withdraw_addresses
		 WHERE user_id = $1 AND asset = $2 AND network = $3 AND address = $4 AND whitelisted = TRUE
		 LIMIT 1`,
		userID, strings.ToUpper(asset), strings.ToUpper(network), address).Scan(&one)
	if errors.Is(err, sql.ErrNoRows) {
		return false, nil
	}
	return err == nil, err
}

// DeleteWithdrawAddress removes an address-book entry and reports success.
func (s *Store) DeleteWithdrawAddress(ctx context.Context, userID int64, id string) (bool, error) {
	res, err := s.db.ExecContext(ctx,
		`DELETE FROM withdraw_addresses WHERE user_id = $1 AND id::text = $2`, userID, id)
	if err != nil {
		return false, err
	}
	n, _ := res.RowsAffected()
	return n > 0, nil
}

func hashToken(token string) string {
	sum := sha256.Sum256([]byte(token))
	return hex.EncodeToString(sum[:])
}

// uniqueConstraint returns the constraint name when err is a Postgres
// unique-constraint violation, or "" otherwise.
func uniqueConstraint(err error) string {
	var pgErr *pgconn.PgError
	if errors.As(err, &pgErr) && pgErr.Code == "23505" { // unique_violation
		return pgErr.ConstraintName
	}
	return ""
}
