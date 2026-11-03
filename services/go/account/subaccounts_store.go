package main

import (
	"context"
	"database/sql"
	"errors"
	"strings"
	"time"
)

// Sub-account persistence. A sub-account is a users row (parent_user_id set)
// plus a sub_accounts row with sub-specific settings.

var (
	ErrSubLimit       = errors.New("sub-account limit reached")
	ErrSubUsername    = errors.New("sub-account username taken")
	ErrSubNotFound    = errors.New("sub-account not found")
	ErrTransferReused = errors.New("clientId reused with different parameters")
)

// SubSettings are the admin-configurable sub-account rules.
type SubSettings struct {
	Enabled          bool      `json:"enabled"`
	MaxPerParent     int       `json:"maxPerParent"`
	RequireParent2FA bool      `json:"requireParent2fa"`
	AllowSubLogin    bool      `json:"allowSubLogin"`
	MaxAPIKeysPerSub int       `json:"maxApiKeysPerSub"`
	UpdatedAt        time.Time `json:"updatedAt"`
}

func defaultSubSettings() SubSettings {
	return SubSettings{Enabled: true, MaxPerParent: 20, RequireParent2FA: true, AllowSubLogin: true, MaxAPIKeysPerSub: 10}
}

func (s *Store) SubSettings(ctx context.Context) (SubSettings, error) {
	st := defaultSubSettings()
	err := s.db.QueryRowContext(ctx, `SELECT enabled, max_per_parent, require_parent_2fa, allow_sub_login, max_api_keys_per_sub, updated_at
		FROM sub_account_settings WHERE id = 1`).Scan(&st.Enabled, &st.MaxPerParent, &st.RequireParent2FA, &st.AllowSubLogin, &st.MaxAPIKeysPerSub, &st.UpdatedAt)
	if errors.Is(err, sql.ErrNoRows) {
		return defaultSubSettings(), nil
	}
	return st, err
}

func (s *Store) PutSubSettings(ctx context.Context, st SubSettings) error {
	_, err := s.db.ExecContext(ctx, `INSERT INTO sub_account_settings (id, enabled, max_per_parent, require_parent_2fa, allow_sub_login, max_api_keys_per_sub, updated_at)
		VALUES (1, $1, $2, $3, $4, $5, now())
		ON CONFLICT (id) DO UPDATE SET enabled=$1, max_per_parent=$2, require_parent_2fa=$3, allow_sub_login=$4, max_api_keys_per_sub=$5, updated_at=now()`,
		st.Enabled, st.MaxPerParent, st.RequireParent2FA, st.AllowSubLogin, st.MaxAPIKeysPerSub)
	return err
}

// SubAccount is a joined view of a sub-account.
type SubAccount struct {
	UserID       int64
	UID          string
	ParentID     int64
	ParentUID    string
	Username     string
	LoginEmail   string
	Nickname     string
	Note         string
	LoginEnabled bool
	Status       string
	TOTPEnabled  bool
	CreatedAt    time.Time
}

const subSelect = `SELECT u.id, u.uid, sa.parent_id, p.uid, sa.username, u.email, COALESCE(u.nickname,''), sa.note,
	sa.login_enabled, u.status, u.totp_enabled, sa.created_at
	FROM sub_accounts sa JOIN users u ON u.id = sa.user_id JOIN users p ON p.id = sa.parent_id`

func scanSub(sc interface{ Scan(...any) error }) (*SubAccount, error) {
	var a SubAccount
	err := sc.Scan(&a.UserID, &a.UID, &a.ParentID, &a.ParentUID, &a.Username, &a.LoginEmail, &a.Nickname, &a.Note,
		&a.LoginEnabled, &a.Status, &a.TOTPEnabled, &a.CreatedAt)
	if errors.Is(err, sql.ErrNoRows) {
		return nil, ErrSubNotFound
	}
	return &a, err
}

// ParentIDOf returns the parent's internal id (0 when u is not a sub-account).
func (s *Store) ParentIDOf(ctx context.Context, userID int64) (int64, error) {
	var p sql.NullInt64
	err := s.db.QueryRowContext(ctx, `SELECT parent_user_id FROM users WHERE id=$1`, userID).Scan(&p)
	if err != nil {
		return 0, err
	}
	return p.Int64, nil
}

func (s *Store) CountSubs(ctx context.Context, parentID int64) (int, error) {
	var n int
	err := s.db.QueryRowContext(ctx, `SELECT count(*) FROM sub_accounts WHERE parent_id=$1`, parentID).Scan(&n)
	return n, err
}

// CreateSub inserts the users + sub_accounts rows atomically, enforcing the
// per-parent limit under a row lock on the parent.
func (s *Store) CreateSub(ctx context.Context, parentID int64, username, loginEmail, nickname, note, passwordHash string, max int) (*SubAccount, error) {
	tx, err := s.db.BeginTx(ctx, nil)
	if err != nil {
		return nil, err
	}
	defer tx.Rollback() //nolint:errcheck
	if _, err := tx.ExecContext(ctx, `SELECT id FROM users WHERE id=$1 FOR UPDATE`, parentID); err != nil {
		return nil, err
	}
	var n int
	if err := tx.QueryRowContext(ctx, `SELECT count(*) FROM sub_accounts WHERE parent_id=$1`, parentID).Scan(&n); err != nil {
		return nil, err
	}
	if n >= max {
		return nil, ErrSubLimit
	}
	var id int64
	err = tx.QueryRowContext(ctx, `INSERT INTO users (email, password_hash, nickname, email_verified, parent_user_id)
		VALUES ($1, $2, NULLIF($3,''), FALSE, $4) RETURNING id`, loginEmail, passwordHash, nickname, parentID).Scan(&id)
	if err != nil {
		if uniqueConstraint(err) != "" {
			return nil, ErrSubUsername
		}
		return nil, err
	}
	if _, err := tx.ExecContext(ctx, `INSERT INTO sub_accounts (user_id, parent_id, username, note) VALUES ($1,$2,$3,$4)`,
		id, parentID, username, note); err != nil {
		if uniqueConstraint(err) != "" {
			return nil, ErrSubUsername
		}
		return nil, err
	}
	if err := tx.Commit(); err != nil {
		return nil, err
	}
	return s.SubByUserID(ctx, id)
}

func (s *Store) SubByUserID(ctx context.Context, id int64) (*SubAccount, error) {
	return scanSub(s.db.QueryRowContext(ctx, subSelect+` WHERE sa.user_id=$1`, id))
}

// SubOfParent loads a sub-account by uid, only if it belongs to parentID.
func (s *Store) SubOfParent(ctx context.Context, parentID int64, uid string) (*SubAccount, error) {
	return scanSub(s.db.QueryRowContext(ctx, subSelect+` WHERE sa.parent_id=$1 AND u.uid=$2`, parentID, uid))
}

func (s *Store) SubByUID(ctx context.Context, uid string) (*SubAccount, error) {
	return scanSub(s.db.QueryRowContext(ctx, subSelect+` WHERE u.uid=$1`, uid))
}

func (s *Store) ListSubs(ctx context.Context, where string, args []any, limit, offset int) ([]SubAccount, int, error) {
	var total int
	if err := s.db.QueryRowContext(ctx, `SELECT count(*) FROM sub_accounts sa JOIN users u ON u.id=sa.user_id JOIN users p ON p.id=sa.parent_id WHERE `+where, args...).Scan(&total); err != nil {
		return nil, 0, err
	}
	args = append(args, limit, offset)
	n := len(args)
	rows, err := s.db.QueryContext(ctx, subSelect+` WHERE `+where+` ORDER BY sa.created_at, u.id LIMIT $`+itoa(n-1)+` OFFSET $`+itoa(n), args...)
	if err != nil {
		return nil, 0, err
	}
	defer rows.Close()
	out := []SubAccount{}
	for rows.Next() {
		a, err := scanSub(rows)
		if err != nil {
			return nil, 0, err
		}
		out = append(out, *a)
	}
	return out, total, rows.Err()
}

func itoa(n int) string {
	if n == 0 {
		return "0"
	}
	var b []byte
	for n > 0 {
		b = append([]byte{byte('0' + n%10)}, b...)
		n /= 10
	}
	return string(b)
}

func (s *Store) UpdateSub(ctx context.Context, userID int64, nickname, note *string, loginEnabled *bool) error {
	if nickname != nil {
		if _, err := s.db.ExecContext(ctx, `UPDATE users SET nickname=NULLIF($2,'') WHERE id=$1`, userID, *nickname); err != nil {
			return err
		}
	}
	if note != nil {
		if _, err := s.db.ExecContext(ctx, `UPDATE sub_accounts SET note=$2 WHERE user_id=$1`, userID, *note); err != nil {
			return err
		}
	}
	if loginEnabled != nil {
		if _, err := s.db.ExecContext(ctx, `UPDATE sub_accounts SET login_enabled=$2 WHERE user_id=$1`, userID, *loginEnabled); err != nil {
			return err
		}
	}
	return nil
}

// SetUserStatus sets active/frozen (shared with admin freeze).
func (s *Store) SetSubStatus(ctx context.Context, userID int64, status, reason string) error {
	_, err := s.db.ExecContext(ctx, `UPDATE users SET status=$2, status_reason=$3, status_changed_at=now() WHERE id=$1`, userID, status, reason)
	return err
}

// ---- API keys ----

type SubAPIKey struct {
	ID          int64
	APIKey      string
	SecretHash  string
	SubUserID   int64
	ParentID    int64
	Label       string
	Permissions []string
	IPAllowlist []string
	CreatedAt   time.Time
	LastUsedAt  sql.NullTime
	RevokedAt   sql.NullTime
}

func splitList(s string) []string {
	out := []string{}
	for _, p := range strings.Split(s, ",") {
		if p = strings.TrimSpace(p); p != "" {
			out = append(out, p)
		}
	}
	return out
}

const keySelect = `SELECT id, api_key, secret_hash, sub_user_id, parent_id, label, permissions, ip_allowlist, created_at, last_used_at, revoked_at FROM sub_api_keys`

func scanKey(sc interface{ Scan(...any) error }) (*SubAPIKey, error) {
	var k SubAPIKey
	var perms, ips string
	err := sc.Scan(&k.ID, &k.APIKey, &k.SecretHash, &k.SubUserID, &k.ParentID, &k.Label, &perms, &ips, &k.CreatedAt, &k.LastUsedAt, &k.RevokedAt)
	if errors.Is(err, sql.ErrNoRows) {
		return nil, ErrNotFound
	}
	k.Permissions, k.IPAllowlist = splitList(perms), splitList(ips)
	return &k, err
}

func (s *Store) CreateSubKey(ctx context.Context, k SubAPIKey) (*SubAPIKey, error) {
	return scanKey(s.db.QueryRowContext(ctx, `INSERT INTO sub_api_keys (api_key, secret_hash, sub_user_id, parent_id, label, permissions, ip_allowlist)
		VALUES ($1,$2,$3,$4,$5,$6,$7) RETURNING id, api_key, secret_hash, sub_user_id, parent_id, label, permissions, ip_allowlist, created_at, last_used_at, revoked_at`,
		k.APIKey, k.SecretHash, k.SubUserID, k.ParentID, k.Label, strings.Join(k.Permissions, ","), strings.Join(k.IPAllowlist, ",")))
}

func (s *Store) CountActiveSubKeys(ctx context.Context, subUserID int64) (int, error) {
	var n int
	err := s.db.QueryRowContext(ctx, `SELECT count(*) FROM sub_api_keys WHERE sub_user_id=$1 AND revoked_at IS NULL`, subUserID).Scan(&n)
	return n, err
}

func (s *Store) ListSubKeys(ctx context.Context, subUserID int64) ([]SubAPIKey, error) {
	rows, err := s.db.QueryContext(ctx, keySelect+` WHERE sub_user_id=$1 AND revoked_at IS NULL ORDER BY id`, subUserID)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	out := []SubAPIKey{}
	for rows.Next() {
		k, err := scanKey(rows)
		if err != nil {
			return nil, err
		}
		out = append(out, *k)
	}
	return out, rows.Err()
}

func (s *Store) RevokeSubKey(ctx context.Context, subUserID int64, apiKey string) (bool, error) {
	res, err := s.db.ExecContext(ctx, `UPDATE sub_api_keys SET revoked_at=now() WHERE sub_user_id=$1 AND api_key=$2 AND revoked_at IS NULL`, subUserID, apiKey)
	if err != nil {
		return false, err
	}
	n, _ := res.RowsAffected()
	return n > 0, nil
}

func (s *Store) SubKeyByAPIKey(ctx context.Context, apiKey string) (*SubAPIKey, error) {
	return scanKey(s.db.QueryRowContext(ctx, keySelect+` WHERE api_key=$1`, apiKey))
}

func (s *Store) TouchSubKey(ctx context.Context, id int64) {
	_, _ = s.db.ExecContext(ctx, `UPDATE sub_api_keys SET last_used_at=now() WHERE id=$1`, id)
}

// ---- transfers ----

type SubTransfer struct {
	TransferID string
	ParentID   int64
	SubUserID  int64
	SubUID     string
	ClientID   string
	Direction  string
	Asset      string
	Amount     string
	Status     string
	ErrorCode  string
	Initiator  string
	CreatedAt  time.Time
}

const xferSelect = `SELECT t.transfer_id, t.parent_id, t.sub_user_id, u.uid, t.client_id, t.direction, t.asset, t.amount::text,
	t.status, t.error_code, t.initiator, t.created_at FROM sub_transfers t JOIN users u ON u.id=t.sub_user_id`

func scanXfer(sc interface{ Scan(...any) error }) (*SubTransfer, error) {
	var t SubTransfer
	err := sc.Scan(&t.TransferID, &t.ParentID, &t.SubUserID, &t.SubUID, &t.ClientID, &t.Direction, &t.Asset, &t.Amount,
		&t.Status, &t.ErrorCode, &t.Initiator, &t.CreatedAt)
	if errors.Is(err, sql.ErrNoRows) {
		return nil, ErrNotFound
	}
	return &t, err
}

// ClaimTransfer inserts a pending transfer; on (parent, clientId) conflict it
// returns the existing row and created=false.
func (s *Store) ClaimTransfer(ctx context.Context, t SubTransfer) (*SubTransfer, bool, error) {
	res, err := s.db.ExecContext(ctx, `INSERT INTO sub_transfers (transfer_id, parent_id, sub_user_id, client_id, direction, asset, amount, initiator)
		VALUES ($1,$2,$3,$4,$5,$6,$7,$8) ON CONFLICT (parent_id, client_id) DO NOTHING`,
		t.TransferID, t.ParentID, t.SubUserID, t.ClientID, t.Direction, t.Asset, t.Amount, t.Initiator)
	if err != nil {
		return nil, false, err
	}
	n, _ := res.RowsAffected()
	cur, err := scanXfer(s.db.QueryRowContext(ctx, xferSelect+` WHERE t.parent_id=$1 AND t.client_id=$2`, t.ParentID, t.ClientID))
	return cur, n > 0, err
}

func (s *Store) FinishTransfer(ctx context.Context, transferID, status, code string) error {
	_, err := s.db.ExecContext(ctx, `UPDATE sub_transfers SET status=$2, error_code=$3, updated_at=now() WHERE transfer_id=$1`, transferID, status, code)
	return err
}

func (s *Store) ListTransfers(ctx context.Context, parentID int64, subUserID int64, limit, offset int) ([]SubTransfer, int, error) {
	where := `t.parent_id=$1`
	args := []any{parentID}
	if subUserID > 0 {
		where += ` AND t.sub_user_id=$2`
		args = append(args, subUserID)
	}
	var total int
	if err := s.db.QueryRowContext(ctx, `SELECT count(*) FROM sub_transfers t WHERE `+where, args...).Scan(&total); err != nil {
		return nil, 0, err
	}
	args = append(args, limit, offset)
	n := len(args)
	rows, err := s.db.QueryContext(ctx, xferSelect+` WHERE `+where+` ORDER BY t.id DESC LIMIT $`+itoa(n-1)+` OFFSET $`+itoa(n), args...)
	if err != nil {
		return nil, 0, err
	}
	defer rows.Close()
	out := []SubTransfer{}
	for rows.Next() {
		t, err := scanXfer(rows)
		if err != nil {
			return nil, 0, err
		}
		out = append(out, *t)
	}
	return out, total, rows.Err()
}
