package main

import (
	"context"
	"database/sql"
	"encoding/json"
	"errors"
	"fmt"
	"log"
	"net/http"
	"net/url"
	"strings"
	"time"

	"github.com/linbit/platform/services/go/common"
	"github.com/shopspring/decimal"
)

// deletion.go: account self-deletion with a 7-day cool-off window.
//
//   POST /account/delete-request  create a pending request (TOTP/code gated,
//                                 refuses users with a non-zero asset balance)
//   POST /account/delete-cancel   withdraw a pending request during the window
//   GET  /account/delete-status   current state of the user's request
//
// The sweeper (started in main) flips due pending rows to 'completed': the
// user's row becomes status='deleted', PII is scrubbed (email/phone/nickname/
// avatar) and every session is revoked. Order and asset history stays elsewhere
// for the regulated 7-year retention.

const deletionGracePeriod = 7 * 24 * time.Hour

// deletionRequest is the persisted scheduling row.
type deletionRequest struct {
	ID          int64
	UserID      int64
	Status      string
	RequestedAt time.Time
	ScheduledAt time.Time
	CompletedAt sql.NullTime
	CancelledAt sql.NullTime
	Reason      string
}

// ---- store ----

const deletionCols = `id, user_id, status, requested_at, scheduled_at, completed_at, cancelled_at, reason`

// pendingDeletion returns the user's active pending request, or ErrNotFound.
func (s *Store) pendingDeletion(ctx context.Context, userID int64) (*deletionRequest, error) {
	row := s.db.QueryRowContext(ctx,
		`SELECT `+deletionCols+` FROM account_deletion_requests
		 WHERE user_id = $1 AND status = 'pending'`, userID)
	return scanDeletion(row)
}

// createDeletion inserts a new pending request. It returns ErrConflict-shaped
// errors by bubbling the unique-index violation up for the handler to map.
func (s *Store) createDeletion(ctx context.Context, userID int64, scheduledAt time.Time, reason string) (*deletionRequest, error) {
	row := s.db.QueryRowContext(ctx,
		`INSERT INTO account_deletion_requests (user_id, status, scheduled_at, reason)
		 VALUES ($1, 'pending', $2, $3)
		 RETURNING `+deletionCols, userID, scheduledAt, reason)
	return scanDeletion(row)
}

// cancelDeletion flips a pending row to cancelled and returns whether any row was changed.
func (s *Store) cancelDeletion(ctx context.Context, userID int64, now time.Time) (bool, error) {
	res, err := s.db.ExecContext(ctx,
		`UPDATE account_deletion_requests SET status = 'cancelled', cancelled_at = $2
		 WHERE user_id = $1 AND status = 'pending'`, userID, now)
	if err != nil {
		return false, err
	}
	n, _ := res.RowsAffected()
	return n > 0, nil
}

// dueDeletions returns pending requests whose scheduled_at has elapsed. Caller
// must process each row in order.
func (s *Store) dueDeletions(ctx context.Context, now time.Time, limit int) ([]deletionRequest, error) {
	rows, err := s.db.QueryContext(ctx,
		`SELECT `+deletionCols+` FROM account_deletion_requests
		 WHERE status = 'pending' AND scheduled_at <= $1
		 ORDER BY scheduled_at
		 LIMIT $2`, now, limit)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	var out []deletionRequest
	for rows.Next() {
		var d deletionRequest
		if err := rows.Scan(&d.ID, &d.UserID, &d.Status, &d.RequestedAt, &d.ScheduledAt,
			&d.CompletedAt, &d.CancelledAt, &d.Reason); err != nil {
			return nil, err
		}
		out = append(out, d)
	}
	return out, rows.Err()
}

// completeDeletion runs the hard delete in one transaction: scrub the user,
// mark the request completed, and drop refresh tokens. Access-token revocation
// is left to the caller (it hits Redis, not Postgres).
func (s *Store) completeDeletion(ctx context.Context, req deletionRequest, now time.Time) error {
	tx, err := s.db.BeginTx(ctx, nil)
	if err != nil {
		return err
	}
	defer tx.Rollback() //nolint:errcheck
	// Scrub PII but keep the row (foreign keys from orders/wallets/etc. must
	// stay valid for the 7-year regulated retention).
	scrubbedEmail := fmt.Sprintf("deleted-%d@removed.invalid", req.UserID)
	if _, err := tx.ExecContext(ctx,
		`UPDATE users SET status = 'deleted', status_reason = 'self_delete', status_changed_at = $2,
		    email = $3, phone = NULL, nickname = NULL, avatar_url = '',
		    totp_enabled = FALSE, totp_secret = '', fund_password_hash = '',
		    anti_phishing_code = '', telegram_id = NULL, telegram_username = ''
		  WHERE id = $1`, req.UserID, now, scrubbedEmail); err != nil {
		return err
	}
	if _, err := tx.ExecContext(ctx,
		`UPDATE account_deletion_requests SET status = 'completed', completed_at = $2
		 WHERE id = $1`, req.ID, now); err != nil {
		return err
	}
	if _, err := tx.ExecContext(ctx,
		`UPDATE refresh_tokens SET revoked = TRUE WHERE user_id = $1 AND revoked = FALSE`, req.UserID); err != nil {
		return err
	}
	return tx.Commit()
}

func scanDeletion(row *sql.Row) (*deletionRequest, error) {
	var d deletionRequest
	err := row.Scan(&d.ID, &d.UserID, &d.Status, &d.RequestedAt, &d.ScheduledAt,
		&d.CompletedAt, &d.CancelledAt, &d.Reason)
	if errors.Is(err, sql.ErrNoRows) {
		return nil, ErrNotFound
	}
	if err != nil {
		return nil, err
	}
	return &d, nil
}

// ---- handlers ----

type deleteRequestBody struct {
	Code     string `json:"code"`
	TOTPCode string `json:"totpCode"`
	Reason   string `json:"reason"`
}

type deleteStatusResponse struct {
	Pending     bool    `json:"pending"`
	Status      string  `json:"status,omitempty"`
	ScheduledAt *string `json:"scheduledAt"`
	RequestedAt *string `json:"requestedAt"`
	Cancellable bool    `json:"cancellable"`
	GraceDays   int     `json:"graceDays"`
}

// POST /account/delete-request
func (s *Server) handleAccountDeleteRequest(w http.ResponseWriter, r *http.Request) {
	u, ok := s.mustUser(w, r)
	if !ok {
		return
	}
	if u.Status == "deleted" {
		common.WriteError(w, http.StatusConflict, "ACCOUNT_DELETED", "账户已注销")
		return
	}
	var req deleteRequestBody
	if err := common.DecodeJSON(r, &req); err != nil {
		common.WriteError(w, http.StatusBadRequest, "VALIDATION_ERROR", "请求参数不合法")
		return
	}
	ctx := r.Context()
	// Refuse if the user still holds a non-zero asset balance (they must
	// withdraw first — we don't want to lose their money for them).
	if nonZero, asset, err := s.userHasBalance(ctx, u.UID); err != nil {
		log.Printf("delete-request: balance probe: %v", err)
		// Balance service unreachable: fail closed so we don't eat funds.
		common.WriteError(w, http.StatusServiceUnavailable, "BALANCE_UNAVAILABLE", "暂时无法核对余额，请稍后再试")
		return
	} else if nonZero {
		common.WriteJSON(w, http.StatusConflict, map[string]any{
			"code": "BALANCE_NOT_ZERO", "message": "账户仍有资产，请先提现后再注销", "asset": asset,
		})
		return
	}
	if !s.requireSensitiveAuth(w, r, u, req.TOTPCode, req.Code, purposeSecurity) {
		return
	}
	now := s.now()
	scheduled := now.Add(deletionGracePeriod)
	dr, err := s.store.createDeletion(ctx, u.ID, scheduled, strings.TrimSpace(req.Reason))
	if err != nil {
		if uniqueConstraint(err) != "" {
			// Idempotent: return the existing pending request.
			if existing, err := s.store.pendingDeletion(ctx, u.ID); err == nil {
				writeDeleteStatus(w, http.StatusOK, existing, now)
				return
			}
		}
		log.Printf("delete-request: insert: %v", err)
		internalErr(w)
		return
	}
	s.notifyDeletionScheduled(u, scheduled)
	writeDeleteStatus(w, http.StatusCreated, dr, now)
}

// POST /account/delete-cancel
func (s *Server) handleAccountDeleteCancel(w http.ResponseWriter, r *http.Request) {
	u, ok := s.mustUser(w, r)
	if !ok {
		return
	}
	now := s.now()
	ok2, err := s.store.cancelDeletion(r.Context(), u.ID, now)
	if err != nil {
		internalErr(w)
		return
	}
	if !ok2 {
		common.WriteError(w, http.StatusNotFound, "NO_PENDING_DELETION", "没有待取消的注销申请")
		return
	}
	s.notifyAsync(u, common.NoticeSecurity, "注销申请已取消",
		"您的账户注销申请已取消，账户恢复正常使用。如非本人操作，请立即修改登录密码并联系客服。")
	common.WriteJSON(w, http.StatusOK, map[string]any{"ok": true, "pending": false})
}

// GET /account/delete-status
func (s *Server) handleAccountDeleteStatus(w http.ResponseWriter, r *http.Request) {
	u, ok := s.mustUser(w, r)
	if !ok {
		return
	}
	dr, err := s.store.pendingDeletion(r.Context(), u.ID)
	if errors.Is(err, ErrNotFound) {
		common.WriteJSON(w, http.StatusOK, deleteStatusResponse{Pending: false, GraceDays: int(deletionGracePeriod / (24 * time.Hour))})
		return
	}
	if err != nil {
		internalErr(w)
		return
	}
	writeDeleteStatus(w, http.StatusOK, dr, s.now())
}

func writeDeleteStatus(w http.ResponseWriter, code int, dr *deletionRequest, now time.Time) {
	sched := dr.ScheduledAt.UTC().Format(time.RFC3339)
	req := dr.RequestedAt.UTC().Format(time.RFC3339)
	common.WriteJSON(w, code, deleteStatusResponse{
		Pending:     dr.Status == "pending",
		Status:      dr.Status,
		ScheduledAt: &sched,
		RequestedAt: &req,
		Cancellable: dr.Status == "pending" && dr.ScheduledAt.After(now),
		GraceDays:   int(deletionGracePeriod / (24 * time.Hour)),
	})
}

// ---- balance probe ----

// userHasBalance asks the asset service whether the user still holds any
// non-zero position. Returns (true, firstAsset) when a non-zero slot is found.
// When the asset service isn't configured, we treat the user as empty rather
// than trapping them (dev stacks have no asset service wired).
func (s *Server) userHasBalance(ctx context.Context, uid string) (bool, string, error) {
	if s.cfg.AssetInternalURL == "" {
		return false, "", nil
	}
	cctx, cancel := context.WithTimeout(ctx, 3*time.Second)
	defer cancel()
	code, body, err := s.internalCall(cctx, http.MethodGet, s.cfg.AssetInternalURL,
		"/internal/balances?userId="+url.QueryEscape(uid), nil, nil)
	if err != nil {
		return false, "", err
	}
	if code != http.StatusOK {
		return false, "", fmt.Errorf("asset status %d", code)
	}
	var out struct {
		Items []balanceItem `json:"items"`
	}
	if err := json.Unmarshal(body, &out); err != nil {
		return false, "", err
	}
	for _, b := range out.Items {
		t, err := decimal.NewFromString(b.Total)
		if err == nil && t.Sign() > 0 {
			return true, b.Asset, nil
		}
	}
	return false, "", nil
}

// ---- notifications ----

func (s *Server) notifyDeletionScheduled(u *User, scheduled time.Time) {
	days := int(deletionGracePeriod / (24 * time.Hour))
	when := scheduled.UTC().Format("2006-01-02 15:04 UTC")
	body := fmt.Sprintf(
		"您已申请注销账户。为保障账户安全，设有 %d 天冷静期，到期（%s）后系统将正式关闭账户并清理个人信息。\n\n如非本人操作或希望取消注销，请在冷静期内登录并取消；超期将无法恢复。",
		days, when)
	force := true
	ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
	go func() {
		defer cancel()
		if _, err := s.notify(ctx, u, common.NoticeSecurity, "账户注销申请已受理", body, &force); err != nil {
			log.Printf("delete-request notify: %v", err)
		}
	}()
}

// ---- sweeper ----

// StartDeletionSweeper runs the deletion sweeper on a fixed interval. The
// returned stop func cancels it. Designed to live for the server's lifetime.
func (s *Server) StartDeletionSweeper(interval time.Duration) (stop func()) {
	if interval <= 0 {
		interval = 10 * time.Minute
	}
	ctx, cancel := context.WithCancel(context.Background())
	go func() {
		// First sweep right away so a restart catches anything overdue.
		s.sweepDeletions(ctx)
		t := time.NewTicker(interval)
		defer t.Stop()
		for {
			select {
			case <-ctx.Done():
				return
			case <-t.C:
				s.sweepDeletions(ctx)
			}
		}
	}()
	return cancel
}

// sweepDeletions finalises every pending request whose scheduled_at has passed.
func (s *Server) sweepDeletions(ctx context.Context) {
	now := s.now()
	rows, err := s.store.dueDeletions(ctx, now, 50)
	if err != nil {
		log.Printf("deletion sweep: list due: %v", err)
		return
	}
	for _, dr := range rows {
		// Refetch the user so we can cut access tokens by UID afterwards.
		u, err := s.store.UserByID(ctx, dr.UserID)
		if err != nil {
			log.Printf("deletion sweep: load user %d: %v", dr.UserID, err)
			continue
		}
		if u.Status == "deleted" {
			// Already scrubbed by a previous pass; just close the request.
			if _, err := s.store.db.ExecContext(ctx,
				`UPDATE account_deletion_requests SET status='completed', completed_at=$2 WHERE id=$1`,
				dr.ID, now); err != nil {
				log.Printf("deletion sweep: close stale request %d: %v", dr.ID, err)
			}
			continue
		}
		if err := s.store.completeDeletion(ctx, dr, now); err != nil {
			log.Printf("deletion sweep: complete %d: %v", dr.ID, err)
			continue
		}
		// Kick every live access token (Redis).
		if err := common.RevokeAccessTokens(ctx, s.rdb, u.UID, now, s.revocationTTL()); err != nil {
			log.Printf("deletion sweep: revoke access tokens for %s: %s", u.UID, common.RedactSecrets(err.Error()))
		}
		log.Printf("deletion sweep: account %s deleted (request %d)", u.UID, dr.ID)
	}
}
