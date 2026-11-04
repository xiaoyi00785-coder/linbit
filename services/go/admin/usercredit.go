package main

import (
	"encoding/json"
	"net/http"
	"net/url"
	"strings"

	"github.com/linbit/platform/services/go/common"
	"github.com/shopspring/decimal"
)

// usercredit.go: POST /admin/v1/users/{uid}/credit — testnet-only virtual
// credit to a normal user's FUNDING account (acceptance testing).
//
//   - HARD testnet-only: 404 unless common.UserCreditAllowed()
//     (WALLET_CHAIN=testnet AND ADMIN_USER_CREDIT_ENABLED=true, refused on any
//     mainnet signal even with the flag). asset enforces the same gate.
//   - Permission reward:approve (super_admin, finance_second) + a fresh 2FA
//     code (required even when ADMIN_REQUIRE_2FA=false) + non-empty reason.
//   - Idempotent on ref (asset wallet_ops PK): same ref+params returns the
//     original result (replayed=true); different params -> 409 REF_CONFLICT.
//   - Caps (USDT-equivalent, USDT/USDC only): ADMIN_USER_CREDIT_MAX per credit
//     (default 100000), ADMIN_USER_CREDIT_DAILY_MAX per UTC day (default
//     1000000), enforced in asset.
//   - Ledger: debit user:<uid>:funding_free / credit system:testnet_treasury.
//   - Audited (high) with before/after funding balance.
func (s *Server) registerUserCreditRoutes(a func(pattern, perm string, h http.HandlerFunc)) {
	a("POST /admin/v1/users/{uid}/credit", permRewardApprove, s.handleUserCredit)
}

func (s *Server) handleUserCredit(w http.ResponseWriter, r *http.Request) {
	a := actorOf(r)
	uid := strings.TrimSpace(r.PathValue("uid"))
	if ok, why := common.UserCreditAllowed(); !ok {
		s.audit(r, a, "ledger", "user_testnet_credit", "user:"+uid, levelHigh, "refused:USER_CREDIT_DISABLED", nil, nil, why)
		common.WriteError(w, http.StatusNotFound, "NOT_FOUND", "接口不存在")
		return
	}
	var req struct {
		Asset    string `json:"asset"`
		Amount   string `json:"amount"`
		Ref      string `json:"ref"`
		Reason   string `json:"reason"`
		TOTPCode string `json:"totpCode"`
	}
	if err := common.DecodeJSON(r, &req); err != nil {
		common.WriteError(w, http.StatusBadRequest, "VALIDATION_ERROR", "请求格式错误")
		return
	}
	req.Asset = strings.ToUpper(strings.TrimSpace(req.Asset))
	req.Ref = strings.TrimSpace(req.Ref)
	req.Reason = strings.TrimSpace(req.Reason)
	if msg := validateUserCredit(uid, req.Asset, req.Amount, req.Ref); msg != "" {
		common.WriteError(w, http.StatusBadRequest, "VALIDATION_ERROR", msg)
		return
	}
	if uid == s.cfg.MMUserID {
		common.WriteError(w, http.StatusForbidden, "ACCOUNT_NOT_ALLOWED", "不能为做市账户充值")
		return
	}
	if req.Reason == "" {
		common.WriteError(w, http.StatusBadRequest, "REASON_REQUIRED", "请填写操作原因")
		return
	}
	if a.TOTP == "" {
		common.WriteError(w, http.StatusForbidden, "TWO_FACTOR_REQUIRED", "账号未绑定 2FA")
		return
	}
	if !s.verify2FA(w, a, req.TOTPCode) {
		return
	}
	// The uid must be an existing normal user.
	uu, err := s.call(r.Context(), http.MethodGet, s.cfg.AccountURL, "/internal/admin/users/"+url.PathEscape(uid), nil)
	if err != nil {
		upstreamErr(w, "user credit lookup", err)
		return
	}
	if !uu.ok() {
		relay(w, uu)
		return
	}
	amount := decimal.RequireFromString(strings.TrimSpace(req.Amount)).String()
	u, err := s.call(r.Context(), http.MethodPost, s.cfg.AssetURL, "/internal/admin/user-credit",
		map[string]string{"userId": uid, "asset": req.Asset, "amount": amount, "ref": req.Ref})
	if err != nil {
		upstreamErr(w, "user credit", err)
		return
	}
	var res struct {
		Before   string `json:"fundingFreeBefore"`
		After    string `json:"fundingFreeAfter"`
		Replayed bool   `json:"replayed"`
	}
	_ = json.Unmarshal(u.Body, &res)
	if u.ok() && res.Replayed {
		// Idempotent replay: nothing moved; record it at normal level.
		s.audit(r, a, "ledger", "user_testnet_credit_replay", "user:"+uid, levelNormal, "ok", nil,
			map[string]any{"ref": req.Ref, "asset": req.Asset, "amount": amount}, req.Reason)
		relay(w, u)
		return
	}
	var before, after any
	if u.ok() {
		before = map[string]any{"account": "funding", "asset": req.Asset, "free": res.Before}
		after = map[string]any{"account": "funding", "asset": req.Asset, "free": res.After, "credited": amount,
			"ref": req.Ref, "txType": "admin_testnet_credit", "counterparty": "system:testnet_treasury"}
	} else {
		after = map[string]any{"ref": req.Ref, "asset": req.Asset, "amount": amount}
	}
	s.audit(r, a, "ledger", "user_testnet_credit", "user:"+uid, levelHigh, resultOf(u), before, after, req.Reason)
	relay(w, u)
}

// validateUserCredit checks the request shape; "" = ok. Caps are enforced by
// asset (single source of truth for the daily total).
func validateUserCredit(uid, asset, amount, ref string) string {
	if uid == "" || len(uid) > 64 || strings.HasPrefix(uid, "system:") || strings.HasPrefix(uid, "mm:") || strings.HasPrefix(uid, "platform:") {
		return "uid 不合法"
	}
	if asset != "USDT" && asset != "USDC" {
		return "asset 仅支持 USDT/USDC"
	}
	if ref == "" || len(ref) > 128 {
		return "ref 必填（幂等键，最长 128）"
	}
	amt, err := decimal.NewFromString(strings.TrimSpace(amount))
	if err != nil || !amt.IsPositive() || amt.Exponent() < -8 {
		return "amount 不合法"
	}
	return ""
}
