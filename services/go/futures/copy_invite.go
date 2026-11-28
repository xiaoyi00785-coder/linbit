package main

import (
	"context"
	"net/http"
	"strings"

	"github.com/shopspring/decimal"

	"github.com/linbit/platform/services/go/common"
)

// traderByInvitation returns the active trader who published this invite code.
// Pending / closed / rejected rows are invisible so a not-yet-reviewed
// application cannot be followed via the code. Comparison is case-insensitive.
func (s *Server) traderByInvitation(ctx context.Context, code string) (Trader, error) {
	code = strings.TrimSpace(code)
	if code == "" {
		return Trader{}, bad("INVITATION_NOT_FOUND", "邀请码不正确或带单员不可用")
	}
	rows, err := s.db.QueryContext(ctx, `SELECT user_id FROM fut_copy_traders
		WHERE status='active' AND invitation_code <> '' AND lower(invitation_code)=lower($1) LIMIT 1`, code)
	if err != nil {
		return Trader{}, err
	}
	defer rows.Close()
	if !rows.Next() {
		return Trader{}, bad("INVITATION_NOT_FOUND", "邀请码不正确或带单员不可用")
	}
	var uid string
	if err := rows.Scan(&uid); err != nil {
		return Trader{}, err
	}
	return s.getTrader(ctx, uid)
}

// hCopyInviteLookup serves GET /futures/copy/invite?code= — the route
// t84CopyInvite (bind-invitation-code) already calls. Missing this route
// made the gateway return a raw "404 page not found" body, which compat
// mapped to business code "1" and the App showed as 服务异常 / 服务器异常.
func (s *Server) hCopyInviteLookup(w http.ResponseWriter, r *http.Request) {
	t, err := s.traderByInvitation(r.Context(), r.URL.Query().Get("code"))
	if err != nil {
		writeErr(w, err)
		return
	}
	t.InvitationCode = ""
	common.WriteJSON(w, http.StatusOK, map[string]any{
		"traderId":        t.UserID,
		"trader_id":       t.UserID,
		"id":              t.UserID,
		"nickname":        t.Nickname,
		"dealer_name":     t.Nickname,
		"dealerIdentity":  t.DealerIdentity,
		"dealer_identity": t.DealerIdentity,
		"status":          t.Status,
		"logoUrl":         t.LogoURL,
		"logo_url":        t.LogoURL,
	})
}

// hFollowPnl serves GET /futures/copy/follows/pnl. Compat ctUserPnl calls
// this; without it Go's mux returned 405 (the path collided with
// PATCH/DELETE /follows/{id} where {id}="pnl").
func (s *Server) hFollowPnl(w http.ResponseWriter, r *http.Request, uid string) {
	ctx := r.Context()
	trader := strings.TrimSpace(firstNonEmptyQuery(r, "traderId", "dealer_id", "trader_id"))
	where := `follower=$1 AND status<>'stopped'`
	args := []any{uid}
	if trader != "" {
		where += ` AND trader=$2`
		args = append(args, trader)
	}
	fs, err := s.follows(ctx, where, args...)
	if err != nil {
		writeErr(w, err)
		return
	}
	total, unreal, equity := decimal.Zero, decimal.Zero, decimal.Zero
	for _, f := range fs {
		v := s.followView(ctx, f)
		total = total.Add(decAny(v["pnl"]))
		equity = equity.Add(decAny(v["equity"]))
	}
	common.WriteJSON(w, http.StatusOK, map[string]any{
		"total_pnl":      total.StringFixed(8),
		"totalPnl":       total.StringFixed(8),
		"pnl_for_today":  "0",
		"unrealized_pnl": unreal.StringFixed(8),
		"unrealizedPnl":  unreal.StringFixed(8),
		"equity":         equity.StringFixed(8),
		"profit_days":    0,
		"profitDays":     0,
		"no_profit_days": 0,
		"noProfitDays":   0,
	})
}

func firstNonEmptyQuery(r *http.Request, keys ...string) string {
	for _, k := range keys {
		if v := strings.TrimSpace(r.URL.Query().Get(k)); v != "" {
			return v
		}
	}
	return ""
}

func decAny(v any) decimal.Decimal {
	switch x := v.(type) {
	case decimal.Decimal:
		return x
	case string:
		d, _ := decimal.NewFromString(x)
		return d
	}
	return decimal.Zero
}

// hCopyTopFollowers serves GET /futures/copy/traders/{id}/top-followers
// (the App's show-topn-users). Public, like the plaza.
func (s *Server) hCopyTopFollowers(w http.ResponseWriter, r *http.Request) {
	ctx := r.Context()
	id := r.PathValue("id")
	t, err := s.getTrader(ctx, id)
	if err != nil || t.Status != "active" {
		if err == nil {
			err = bad("TRADER_NOT_FOUND", "交易员不存在")
		}
		writeErr(w, err)
		return
	}
	fs, err := s.follows(ctx, `trader=$1 AND status<>'stopped'`, t.UserID)
	if err != nil {
		writeErr(w, err)
		return
	}
	out := []map[string]any{}
	for _, f := range fs {
		v := s.followView(ctx, f)
		out = append(out, map[string]any{
			"uid":         f.Follower,
			"user_id":     f.Follower,
			"userId":      f.Follower,
			"nickname":    f.Follower,
			"pnl":         v["pnl"],
			"principal":   f.Principal,
			"equity":      v["equity"],
			"share_paid":  f.SharePaid,
			"sharePaid":   f.SharePaid,
			"follow_id":   f.ID,
			"followId":    f.ID,
			"since":       f.CreatedAt,
			"follow_time": f.CreatedAt,
			"status":      f.Status,
		})
	}
	common.WriteJSON(w, http.StatusOK, map[string]any{"followers": out, "items": out, "list": out, "total": len(out)})
}
