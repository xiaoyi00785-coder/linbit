package main

import (
	"context"
	"database/sql"
	"errors"
	"fmt"
	"log"
	"net/http"
	"regexp"
	"strings"
	"time"

	"github.com/linbit/platform/services/go/common"
	"github.com/shopspring/decimal"
)

// Internal (off-chain) user-to-user transfers: funding -> funding, free and
// instant, 2FA-gated, idempotent on (user, clientId), with an admin kill switch
// and a rolling-24h USDT-equivalent cap.

const (
	itxPending   = "pending"
	itxCompleted = "completed"
	itxFailed    = "failed"
)

var (
	itxClientRe = regexp.MustCompile(`^[A-Za-z0-9_:.\-]{1,64}$`)
	itxAssetRe  = regexp.MustCompile(`^[A-Z0-9]{2,16}$`)
)

type internalTransfer struct {
	ID          int64
	UserID      string
	ToUserID    string
	ClientID    string
	Asset       string
	Amount      decimal.Decimal
	ValueUSDT   decimal.Decimal
	Status      string
	FailReason  string
	CreatedAt   time.Time
	CompletedAt sql.NullTime
}

func itxRef(id int64) string { return fmt.Sprintf("itx:%d", id) }

type itxConfig struct {
	Enabled        bool            `json:"enabled"`
	DailyLimitUSDT decimal.Decimal `json:"-"`
}

func (s *Store) itxConfig(ctx context.Context) (itxConfig, error) {
	cfg := itxConfig{Enabled: true, DailyLimitUSDT: decimal.NewFromInt(50000)}
	rows, err := s.db.QueryContext(ctx, `SELECT key, value FROM wallet_settings
		WHERE key IN ('internal_transfer_enabled','internal_transfer_daily_limit_usdt')`)
	if err != nil {
		return cfg, err
	}
	defer rows.Close()
	for rows.Next() {
		var k, v string
		if err := rows.Scan(&k, &v); err != nil {
			return cfg, err
		}
		switch k {
		case "internal_transfer_enabled":
			cfg.Enabled = v == "true"
		case "internal_transfer_daily_limit_usdt":
			if d, err := decimal.NewFromString(v); err == nil {
				cfg.DailyLimitUSDT = d
			}
		}
	}
	return cfg, rows.Err()
}

func (s *Store) setWalletSetting(ctx context.Context, key, value, by string) error {
	_, err := s.db.ExecContext(ctx, `INSERT INTO wallet_settings (key, value, updated_by, updated_at)
		VALUES ($1,$2,$3,now()) ON CONFLICT (key) DO UPDATE SET value=EXCLUDED.value,
		updated_by=EXCLUDED.updated_by, updated_at=now()`, key, value, by)
	return err
}

const itxCols = `id, user_id, to_user_id, client_id, asset, amount, value_usdt, status, fail_reason, created_at, completed_at`

func scanITX(sc interface{ Scan(...any) error }) (*internalTransfer, error) {
	var t internalTransfer
	if err := sc.Scan(&t.ID, &t.UserID, &t.ToUserID, &t.ClientID, &t.Asset, &t.Amount, &t.ValueUSDT,
		&t.Status, &t.FailReason, &t.CreatedAt, &t.CompletedAt); err != nil {
		return nil, err
	}
	return &t, nil
}

func (s *Store) itxByClient(ctx context.Context, uid, clientID string) (*internalTransfer, error) {
	return scanITX(s.db.QueryRowContext(ctx, `SELECT `+itxCols+` FROM internal_transfers
		WHERE user_id=$1 AND client_id=$2`, uid, clientID))
}

var errITXLimit = errors.New("daily limit exceeded")

// insertITX inserts a pending row after checking the rolling-24h cap under a
// per-user advisory lock (so concurrent requests cannot overshoot the cap).
func (s *Store) insertITX(ctx context.Context, t internalTransfer, limit decimal.Decimal) (*internalTransfer, error) {
	tx, err := s.db.BeginTx(ctx, nil)
	if err != nil {
		return nil, err
	}
	defer tx.Rollback()
	if _, err := tx.ExecContext(ctx, `SELECT pg_advisory_xact_lock(hashtext('itx:' || $1))`, t.UserID); err != nil {
		return nil, err
	}
	// Re-check idempotency inside the lock.
	if ex, err := scanITX(tx.QueryRowContext(ctx, `SELECT `+itxCols+` FROM internal_transfers
		WHERE user_id=$1 AND client_id=$2`, t.UserID, t.ClientID)); err == nil {
		return ex, tx.Commit()
	} else if !errors.Is(err, sql.ErrNoRows) {
		return nil, err
	}
	if limit.Sign() > 0 {
		var used decimal.Decimal
		if err := tx.QueryRowContext(ctx, `SELECT COALESCE(SUM(value_usdt),0) FROM internal_transfers
			WHERE user_id=$1 AND status <> 'failed' AND created_at > now() - interval '24 hours'`, t.UserID).Scan(&used); err != nil {
			return nil, err
		}
		if used.Add(t.ValueUSDT).GreaterThan(limit) {
			return nil, errITXLimit
		}
	}
	out, err := scanITX(tx.QueryRowContext(ctx, `INSERT INTO internal_transfers
		(user_id, to_user_id, client_id, asset, amount, value_usdt, status)
		VALUES ($1,$2,$3,$4,$5,$6,'pending') RETURNING `+itxCols,
		t.UserID, t.ToUserID, t.ClientID, t.Asset, t.Amount.String(), t.ValueUSDT.String()))
	if err != nil {
		return nil, err
	}
	return out, tx.Commit()
}

func (s *Store) finishITX(ctx context.Context, id int64, status, reason string) error {
	_, err := s.db.ExecContext(ctx, `UPDATE internal_transfers SET status=$2, fail_reason=$3,
		completed_at = CASE WHEN $2='completed' THEN now() ELSE completed_at END
		WHERE id=$1 AND status='pending'`, id, status, reason)
	return err
}

func (s *Store) listITX(ctx context.Context, where string, args []any, page, size int) ([]internalTransfer, int, error) {
	var total int
	if err := s.db.QueryRowContext(ctx, `SELECT count(*) FROM internal_transfers WHERE `+where, args...).Scan(&total); err != nil {
		return nil, 0, err
	}
	q := `SELECT ` + itxCols + ` FROM internal_transfers WHERE ` + where +
		fmt.Sprintf(` ORDER BY id DESC LIMIT %d OFFSET %d`, size, (page-1)*size)
	rows, err := s.db.QueryContext(ctx, q, args...)
	if err != nil {
		return nil, 0, err
	}
	defer rows.Close()
	var out []internalTransfer
	for rows.Next() {
		t, err := scanITX(rows)
		if err != nil {
			return nil, 0, err
		}
		out = append(out, *t)
	}
	return out, total, rows.Err()
}

type itxDTO struct {
	ID          string  `json:"id"`
	ClientID    string  `json:"clientId"`
	Direction   string  `json:"direction"` // out | in (relative to the caller)
	FromUID     string  `json:"fromUid"`
	ToUID       string  `json:"toUid"`
	ToMasked    string  `json:"toMaskedEmail,omitempty"`
	Asset       string  `json:"asset"`
	Amount      string  `json:"amount"`
	Fee         string  `json:"fee"`
	Status      string  `json:"status"`
	FailReason  *string `json:"failReason"`
	CreatedAt   string  `json:"createdAt"`
	CompletedAt *string `json:"completedAt"`
}

func itxToDTO(t internalTransfer, viewer string) itxDTO {
	d := itxDTO{ID: itoa64(t.ID), ClientID: t.ClientID, Direction: "out", FromUID: t.UserID, ToUID: t.ToUserID,
		Asset: t.Asset, Amount: fmt8(t.Amount), Fee: "0", Status: t.Status, FailReason: strptr(t.FailReason),
		CreatedAt: t.CreatedAt.UTC().Format(time.RFC3339)}
	if viewer != "" && viewer == t.ToUserID {
		d.Direction = "in"
		d.ClientID = ""
	}
	if t.CompletedAt.Valid {
		c := t.CompletedAt.Time.UTC().Format(time.RFC3339)
		d.CompletedAt = &c
	}
	return d
}

type internalTransferReq struct {
	ClientID string `json:"clientId"`
	Asset    string `json:"asset"`
	Amount   string `json:"amount"`
	ToUID    string `json:"toUid"`
	ToEmail  string `json:"toEmail"`
	TOTPCode string `json:"totpCode"`
	Code     string `json:"code"`
}

func (s *Server) itxPrice(ctx context.Context, asset string) (decimal.Decimal, bool) {
	if asset == "USDT" {
		return decimal.NewFromInt(1), true
	}
	return s.usdtPrice(ctx, asset)
}

// handleInternalTransfer: POST /wallet/internal-transfer.
func (s *Server) handleInternalTransfer(w http.ResponseWriter, r *http.Request) {
	uid, ok := requireUser(w, r)
	if !ok {
		return
	}
	var req internalTransferReq
	if err := common.DecodeJSON(r, &req); err != nil {
		common.WriteError(w, http.StatusBadRequest, "VALIDATION_ERROR", "请求格式错误")
		return
	}
	t, apiErr := s.internalTransfer(r.Context(), uid, req)
	if apiErr != nil {
		common.WriteError(w, apiErr.status, apiErr.code, apiErr.msg)
		return
	}
	common.WriteJSON(w, http.StatusOK, t)
}

func (s *Server) internalTransfer(ctx context.Context, uid string, req internalTransferReq) (*itxDTO, *apiError) {
	req.ClientID = strings.TrimSpace(req.ClientID)
	req.Asset = strings.ToUpper(strings.TrimSpace(req.Asset))
	req.ToUID = strings.TrimSpace(req.ToUID)
	req.ToEmail = strings.ToLower(strings.TrimSpace(req.ToEmail))
	amt, err := decimal.NewFromString(strings.TrimSpace(req.Amount))
	switch {
	case !itxClientRe.MatchString(req.ClientID):
		return nil, errf(http.StatusBadRequest, "VALIDATION_ERROR", "clientId 不合法")
	case !itxAssetRe.MatchString(req.Asset):
		return nil, errf(http.StatusBadRequest, "VALIDATION_ERROR", "币种不合法")
	case err != nil || amt.Sign() <= 0 || amt.Exponent() < -8:
		return nil, errf(http.StatusBadRequest, "VALIDATION_ERROR", "金额不合法")
	case (req.ToUID == "") == (req.ToEmail == ""):
		return nil, errf(http.StatusBadRequest, "VALIDATION_ERROR", "toUid 与 toEmail 需且仅需提供一个")
	}

	// Idempotent replay: same clientId returns the original transfer.
	if ex, err := s.store.itxByClient(ctx, uid, req.ClientID); err == nil {
		if ex.Asset != req.Asset || !ex.Amount.Equal(amt) || (req.ToUID != "" && ex.ToUserID != req.ToUID) {
			return nil, errf(http.StatusConflict, "IDEMPOTENCY_CONFLICT", "clientId 已被其他请求使用")
		}
		if ex.Status == itxPending {
			s.completeITX(ctx, ex)
			if again, err := s.store.itxByClient(ctx, uid, req.ClientID); err == nil {
				ex = again
			}
		}
		d := itxToDTO(*ex, uid)
		return &d, nil
	} else if !errors.Is(err, sql.ErrNoRows) {
		return nil, errf(http.StatusInternalServerError, "INTERNAL", "服务器内部错误")
	}

	cfg, err := s.store.itxConfig(ctx)
	if err != nil {
		return nil, errf(http.StatusInternalServerError, "INTERNAL", "服务器内部错误")
	}
	if !cfg.Enabled {
		return nil, errf(http.StatusForbidden, "INTERNAL_TRANSFER_DISABLED", "内部转账暂未开放")
	}

	vr, err := s.account.Verify2FA(ctx, uid, req.TOTPCode, req.Code)
	if err != nil {
		return nil, errf(http.StatusInternalServerError, "INTERNAL", "二次验证服务不可用")
	}
	if !vr.OK {
		return nil, errf(http.StatusUnauthorized, "TWOFA_REQUIRED", "需要有效的二次验证（TOTP 或验证码）")
	}

	to, err := s.account.LookupUser(ctx, req.ToUID, req.ToEmail)
	switch {
	case errors.Is(err, ErrUserNotFound):
		return nil, errf(http.StatusNotFound, "RECIPIENT_NOT_FOUND", "收款用户不存在")
	case err != nil:
		return nil, errf(http.StatusInternalServerError, "INTERNAL", "无法查询收款用户")
	case to.UID == uid:
		return nil, errf(http.StatusBadRequest, "SELF_TRANSFER", "不能转账给自己")
	case to.IsSubAccount:
		return nil, errf(http.StatusBadRequest, "RECIPIENT_SUB_ACCOUNT", "不支持转账给子账户")
	case to.Status != "" && to.Status != "active":
		return nil, errf(http.StatusBadRequest, "RECIPIENT_UNAVAILABLE", "收款账户状态异常")
	}

	price, ok := s.itxPrice(ctx, req.Asset)
	if !ok && cfg.DailyLimitUSDT.Sign() > 0 {
		return nil, errf(http.StatusBadRequest, "PRICE_UNAVAILABLE", "暂无 "+req.Asset+" 的实时价格，无法计算转账额度")
	}
	value := amt.Mul(price)

	t, err := s.store.insertITX(ctx, internalTransfer{UserID: uid, ToUserID: to.UID, ClientID: req.ClientID,
		Asset: req.Asset, Amount: amt, ValueUSDT: value}, cfg.DailyLimitUSDT)
	if errors.Is(err, errITXLimit) {
		return nil, errf(http.StatusBadRequest, "INTERNAL_TRANSFER_LIMIT_EXCEEDED",
			"超过内部转账 24 小时限额 "+cfg.DailyLimitUSDT.String()+" USDT")
	}
	if err != nil {
		log.Printf("internal transfer insert: %v", err)
		return nil, errf(http.StatusInternalServerError, "INTERNAL", "服务器内部错误")
	}
	if t.Status == itxPending {
		s.completeITX(ctx, t)
		if again, err := s.store.itxByClient(ctx, uid, req.ClientID); err == nil {
			t = again
		}
	}
	if t.Status == itxFailed && t.FailReason == "INSUFFICIENT_BALANCE" {
		return nil, errf(http.StatusBadRequest, "INSUFFICIENT_BALANCE", "资金账户可用余额不足")
	}
	d := itxToDTO(*t, uid)
	d.ToMasked = to.MaskedEmail
	return &d, nil
}

// completeITX runs the (idempotent, balanced) asset leg and finalises the row.
// Transient errors leave the row pending for the worker to retry.
func (s *Server) completeITX(ctx context.Context, t *internalTransfer) {
	err := s.asset.UserTransfer(ctx, t.UserID, t.ToUserID, t.Asset, t.Amount, itxRef(t.ID))
	switch {
	case err == nil || strings.Contains(err.Error(), "ALREADY_APPLIED"):
		_ = s.store.finishITX(ctx, t.ID, itxCompleted, "")
	case strings.Contains(err.Error(), "INSUFFICIENT_BALANCE"):
		_ = s.store.finishITX(ctx, t.ID, itxFailed, "INSUFFICIENT_BALANCE")
	case strings.Contains(err.Error(), "status 400"):
		_ = s.store.finishITX(ctx, t.ID, itxFailed, "REJECTED")
	default:
		log.Printf("internal transfer %d: %v (will retry)", t.ID, err)
	}
}

// retryPendingITX re-drives pending transfers older than 15s (worker pass).
func (s *Server) retryPendingITX(ctx context.Context) {
	rows, _, err := s.store.listITX(ctx, `status='pending' AND created_at < now() - interval '15 seconds'`, nil, 1, 100)
	if err != nil {
		log.Printf("itx retry: %v", err)
		return
	}
	for i := range rows {
		s.completeITX(ctx, &rows[i])
	}
}

// handleInternalTransferList: GET /wallet/internal-transfers (sent + received).
func (s *Server) handleInternalTransferList(w http.ResponseWriter, r *http.Request) {
	uid, ok := requireUser(w, r)
	if !ok {
		return
	}
	page := atoiDefault(r.URL.Query().Get("page"), 1, 1, 1<<30)
	size := atoiDefault(r.URL.Query().Get("pageSize"), 20, 1, 100)
	rows, total, err := s.store.listITX(r.Context(), `(user_id=$1 OR (to_user_id=$1 AND status='completed'))`, []any{uid}, page, size)
	if err != nil {
		common.WriteError(w, http.StatusInternalServerError, "INTERNAL", "服务器内部错误")
		return
	}
	items := make([]itxDTO, 0, len(rows))
	for _, t := range rows {
		items = append(items, itxToDTO(t, uid))
	}
	common.WriteJSON(w, http.StatusOK, paginated{Items: items, Page: page, PageSize: size, Total: total})
}

type itxConfigDTO struct {
	Enabled        bool   `json:"enabled"`
	DailyLimitUSDT string `json:"dailyLimitUsdt"`
}

func (s *Server) handleAdminITConfig(w http.ResponseWriter, r *http.Request) {
	cfg, err := s.store.itxConfig(r.Context())
	if err != nil {
		common.WriteError(w, http.StatusInternalServerError, "INTERNAL", "服务器内部错误")
		return
	}
	common.WriteJSON(w, http.StatusOK, itxConfigDTO{Enabled: cfg.Enabled, DailyLimitUSDT: cfg.DailyLimitUSDT.String()})
}

func (s *Server) handleAdminPutITConfig(w http.ResponseWriter, r *http.Request) {
	var req struct {
		Enabled        *bool   `json:"enabled"`
		DailyLimitUSDT *string `json:"dailyLimitUsdt"`
		UpdatedBy      string  `json:"updatedBy"`
	}
	if err := common.DecodeJSON(r, &req); err != nil {
		common.WriteError(w, http.StatusBadRequest, "VALIDATION_ERROR", "请求格式错误")
		return
	}
	ctx := r.Context()
	before, err := s.store.itxConfig(ctx)
	if err != nil {
		common.WriteError(w, http.StatusInternalServerError, "INTERNAL", "服务器内部错误")
		return
	}
	if req.DailyLimitUSDT != nil {
		d, err := decimal.NewFromString(strings.TrimSpace(*req.DailyLimitUSDT))
		if err != nil || d.Sign() < 0 {
			common.WriteError(w, http.StatusBadRequest, "VALIDATION_ERROR", "dailyLimitUsdt 不合法（0 表示不限）")
			return
		}
		if err := s.store.setWalletSetting(ctx, "internal_transfer_daily_limit_usdt", d.String(), req.UpdatedBy); err != nil {
			common.WriteError(w, http.StatusInternalServerError, "INTERNAL", "服务器内部错误")
			return
		}
	}
	if req.Enabled != nil {
		v := "false"
		if *req.Enabled {
			v = "true"
		}
		if err := s.store.setWalletSetting(ctx, "internal_transfer_enabled", v, req.UpdatedBy); err != nil {
			common.WriteError(w, http.StatusInternalServerError, "INTERNAL", "服务器内部错误")
			return
		}
	}
	after, _ := s.store.itxConfig(ctx)
	common.WriteJSON(w, http.StatusOK, map[string]any{
		"before": itxConfigDTO{before.Enabled, before.DailyLimitUSDT.String()},
		"after":  itxConfigDTO{after.Enabled, after.DailyLimitUSDT.String()},
	})
}

func (s *Server) handleAdminITList(w http.ResponseWriter, r *http.Request) {
	q := r.URL.Query()
	page := atoiDefault(q.Get("page"), 1, 1, 1<<30)
	size := atoiDefault(q.Get("pageSize"), 20, 1, 100)
	where, args := "TRUE", []any{}
	if u := strings.TrimSpace(q.Get("uid")); u != "" {
		args = append(args, u)
		where = "(user_id=$1 OR to_user_id=$1)"
	}
	rows, total, err := s.store.listITX(r.Context(), where, args, page, size)
	if err != nil {
		common.WriteError(w, http.StatusInternalServerError, "INTERNAL", "服务器内部错误")
		return
	}
	items := make([]itxDTO, 0, len(rows))
	for _, t := range rows {
		items = append(items, itxToDTO(t, ""))
	}
	common.WriteJSON(w, http.StatusOK, paginated{Items: items, Page: page, PageSize: size, Total: total})
}
