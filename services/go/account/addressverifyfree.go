package main

import (
	"context"
	"database/sql"
	"errors"
	"log"
	"net/http"
	"strings"
	"time"

	"github.com/linbit/platform/services/go/common"
)

// addressverifyfree.go: per-address withdrawal verify-free mode.
//
// When a user confirms an address with their second factor they can mark it
// "verify-free" for 30 days or permanently. Subsequent withdrawals to the
// exact (asset, network, address) tuple then bypass the 2FA / email / SMS
// check in the wallet verify flow — the password still gates login, and the
// withdrawal whitelist still applies. The expiry sweep below runs once a day
// to reset 30-day grants that have fallen out of their window.

const (
	verifyFreeNone      = "none"
	verifyFree30Days    = "30_days"
	verifyFreePermanent = "permanent"

	verifyFreeWindow = 30 * 24 * time.Hour
)

// ---- store ----

// setVerifyFree sets the mode and expiry for an address owned by userID.
// Returns the refreshed row.
func (s *Store) setVerifyFree(ctx context.Context, userID int64, id, mode string, until sql.NullTime) (WithdrawAddress, error) {
	var a WithdrawAddress
	var vfu sql.NullTime
	err := s.db.QueryRowContext(ctx,
		`UPDATE withdraw_addresses SET verify_free_mode = $3, verify_free_until = $4
		 WHERE user_id = $1 AND id::text = $2
		 RETURNING id::text, asset, network, address, memo, label, whitelisted, created_at,
		           verify_free_mode, verify_free_until`,
		userID, id, mode, until).Scan(&a.ID, &a.Asset, &a.Network, &a.Address, &a.Memo, &a.Label,
		&a.Whitelisted, &a.CreatedAt, &a.VerifyFreeMode, &vfu)
	if vfu.Valid {
		t := vfu.Time
		a.VerifyFreeUntil = &t
	}
	return a, err
}

// IsVerifyFreeAddress reports whether the destination is marked verify-free
// AND (for 30_days) still within its window. Called from the wallet via an
// internal endpoint to decide whether to skip the 2FA step.
func (s *Store) IsVerifyFreeAddress(ctx context.Context, userID int64, asset, network, address string) (bool, error) {
	var ok bool
	err := s.db.QueryRowContext(ctx,
		`SELECT EXISTS (
		   SELECT 1 FROM withdraw_addresses
		    WHERE user_id = $1 AND asset = $2 AND network = $3 AND address = $4
		      AND (
		        verify_free_mode = 'permanent'
		        OR (verify_free_mode = '30_days' AND verify_free_until IS NOT NULL AND verify_free_until > now())
		      )
		 )`,
		userID, strings.ToUpper(asset), strings.ToUpper(network), address).Scan(&ok)
	if errors.Is(err, sql.ErrNoRows) {
		return false, nil
	}
	return ok, err
}

// ExpireVerifyFree resets expired 30-day grants to none. Returns the number
// of rows updated so the caller can log.
func (s *Store) ExpireVerifyFree(ctx context.Context, now time.Time) (int64, error) {
	res, err := s.db.ExecContext(ctx,
		`UPDATE withdraw_addresses
		    SET verify_free_mode = 'none', verify_free_until = NULL
		  WHERE verify_free_mode = '30_days'
		    AND verify_free_until IS NOT NULL
		    AND verify_free_until <= $1`, now)
	if err != nil {
		return 0, err
	}
	n, _ := res.RowsAffected()
	return n, nil
}

// ---- handler ----

type verifyFreeRequest struct {
	Mode     string `json:"mode"` // none | 30_days | permanent
	TOTPCode string `json:"totpCode"`
	Code     string `json:"code"`
}

// handleSetAddressVerifyFree marks a user's address book entry verify-free
// (or clears it). Gated by the sensitive-auth check (purposeAddress) since
// the whole point is to skip that check on later withdrawals.
//
//	POST /account/withdraw-addresses/{id}/verify-free
func (s *Server) handleSetAddressVerifyFree(w http.ResponseWriter, r *http.Request) {
	u, ok := s.mustUser(w, r)
	if !ok {
		return
	}
	var req verifyFreeRequest
	if err := common.DecodeJSON(r, &req); err != nil {
		common.WriteError(w, http.StatusBadRequest, "VALIDATION_ERROR", "请求参数不合法")
		return
	}
	mode := strings.TrimSpace(req.Mode)
	if mode == "" {
		mode = verifyFreeNone
	}
	var until sql.NullTime
	switch mode {
	case verifyFreeNone, verifyFreePermanent:
		// no expiry
	case verifyFree30Days:
		until = sql.NullTime{Valid: true, Time: s.now().Add(verifyFreeWindow)}
	default:
		common.WriteError(w, http.StatusBadRequest, "VALIDATION_ERROR", "mode 必须是 none / 30_days / permanent")
		return
	}
	// Verify the user's second factor NOW so marking an address verify-free
	// requires the same level of proof the skipped check would have asked for.
	// Clearing the flag is also gated — we don't want a stolen access token to
	// silently flip it off before an attacker-controlled address goes live.
	if !s.requireSensitiveAuth(w, r, u, req.TOTPCode, req.Code, purposeAddress) {
		return
	}
	a, err := s.store.setVerifyFree(r.Context(), u.ID, r.PathValue("id"), mode, until)
	switch {
	case errors.Is(err, sql.ErrNoRows):
		common.WriteError(w, http.StatusNotFound, "NOT_FOUND", "地址不存在")
	case err != nil:
		internalErr(w)
	default:
		msg := "将提币地址标记为免二次验证（" + mode + "）"
		if mode == verifyFreeNone {
			msg = "取消了提币地址的免二次验证"
		}
		s.securityChanged(r.Context(), u, msg, false)
		common.WriteJSON(w, http.StatusOK, a)
	}
}

// RunVerifyFreeExpiry runs ExpireVerifyFree in a loop, roughly once a day,
// until ctx is cancelled. Started by main alongside the other background
// jobs. A missed tick (process down) is picked up at the next tick: we only
// need "eventually consistent" with the user-visible expiry.
func (s *Server) RunVerifyFreeExpiry(ctx context.Context) {
	tick := time.NewTicker(24 * time.Hour)
	defer tick.Stop()
	// Also run on start-up so a process that was down past an expiry catches up.
	for {
		if n, err := s.store.ExpireVerifyFree(ctx, s.now()); err != nil {
			log.Printf("verify-free expiry: %v", err)
		} else if n > 0 {
			log.Printf("verify-free expiry: reset %d address(es)", n)
		}
		select {
		case <-ctx.Done():
			return
		case <-tick.C:
		}
	}
}
