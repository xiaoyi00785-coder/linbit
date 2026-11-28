package main

import (
	"context"
	"net/http"
	"sort"
	"strings"
	"time"

	"github.com/shopspring/decimal"

	"github.com/linbit/platform/services/go/common"
)

// ---- per-area (公域/私域) configuration helpers ----

// maxFollowersFor returns the follower cap for a trader's area. PRIVATE_AREA
// (invite-only) traders get a smaller cap than the public plaza. A zero config
// value falls back to the global CopyMaxFollowers.
func (s *Server) maxFollowersFor(identity string) int {
	if strings.EqualFold(identity, "PRIVATE_AREA") && s.cfg.CopyMaxFollowersPrivate > 0 {
		return s.cfg.CopyMaxFollowersPrivate
	}
	if s.cfg.CopyMaxFollowersPublic > 0 {
		return s.cfg.CopyMaxFollowersPublic
	}
	return s.cfg.CopyMaxFollowers
}

// maxShareFor returns the maximum profit-share rate allowed for a trader's area.
func (s *Server) maxShareFor(identity string) decimal.Decimal {
	if strings.EqualFold(identity, "PRIVATE_AREA") && s.cfg.CopyMaxSharePrivate.Sign() > 0 {
		return s.cfg.CopyMaxSharePrivate
	}
	if s.cfg.CopyMaxSharePublic.Sign() > 0 {
		return s.cfg.CopyMaxSharePublic
	}
	return decimal.RequireFromString(maxProfitShare)
}

// ---- blacklist storage ----

func (s *Server) isBlacklisted(ctx context.Context, trader, user string) (bool, error) {
	if trader == "" || user == "" {
		return false, nil
	}
	var n int
	err := s.db.QueryRowContext(ctx, `SELECT count(*) FROM fut_copy_blacklist WHERE trader=$1 AND blocked_user=$2`, trader, user).Scan(&n)
	return n > 0, err
}

// AddBlacklist blocks user from copying trader. Any live follow of this trader
// by that user is stopped (settled + refunded) so the block takes effect now.
func (s *Server) AddBlacklist(ctx context.Context, trader, user, reason string) error {
	user = strings.TrimSpace(user)
	if user == "" {
		return bad("INVALID_USER", "请提供要拉黑的用户")
	}
	if user == trader {
		return bad("CANNOT_BLACKLIST_SELF", "不能拉黑自己")
	}
	if _, err := s.db.ExecContext(ctx, `INSERT INTO fut_copy_blacklist (trader, blocked_user, reason) VALUES ($1,$2,$3)
		ON CONFLICT (trader, blocked_user) DO UPDATE SET reason=$3`, trader, user, reason); err != nil {
		return err
	}
	fs, err := s.follows(ctx, `trader=$1 AND follower=$2 AND status<>'stopped'`, trader, user)
	if err != nil {
		return err
	}
	for _, f := range fs {
		if _, err := s.StopFollower(ctx, trader, f.ID); err != nil {
			return err
		}
	}
	return nil
}

func (s *Server) RemoveBlacklist(ctx context.Context, trader, user string) error {
	_, err := s.db.ExecContext(ctx, `DELETE FROM fut_copy_blacklist WHERE trader=$1 AND blocked_user=$2`, trader, strings.TrimSpace(user))
	return err
}

func (s *Server) listBlacklist(ctx context.Context, trader string) ([]map[string]any, error) {
	rows, err := s.db.QueryContext(ctx, `SELECT blocked_user, reason, created_at FROM fut_copy_blacklist WHERE trader=$1 ORDER BY created_at DESC`, trader)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	out := []map[string]any{}
	for rows.Next() {
		var u, reason string
		var ts time.Time
		if err := rows.Scan(&u, &reason, &ts); err != nil {
			return nil, err
		}
		out = append(out, map[string]any{"userId": u, "user": u, "reason": reason, "createdAt": ts})
	}
	return out, rows.Err()
}

// ---- HTTP handlers (trader-authenticated) ----

func (s *Server) hAddBlacklist(w http.ResponseWriter, r *http.Request, uid string) {
	var req struct {
		UserID string `json:"userId"`
		User   string `json:"user"`
		Reason string `json:"reason"`
	}
	if !decode(w, r, &req) {
		return
	}
	target := req.UserID
	if target == "" {
		target = req.User
	}
	if err := s.AddBlacklist(r.Context(), uid, target, strings.TrimSpace(req.Reason)); err != nil {
		writeErr(w, err)
		return
	}
	s.writeBlacklist(w, r, uid)
}

func (s *Server) hRemoveBlacklist(w http.ResponseWriter, r *http.Request, uid string) {
	if err := s.RemoveBlacklist(r.Context(), uid, r.PathValue("uid")); err != nil {
		writeErr(w, err)
		return
	}
	s.writeBlacklist(w, r, uid)
}

func (s *Server) hListBlacklist(w http.ResponseWriter, r *http.Request, uid string) {
	s.writeBlacklist(w, r, uid)
}

func (s *Server) writeBlacklist(w http.ResponseWriter, r *http.Request, uid string) {
	list, err := s.listBlacklist(r.Context(), uid)
	if err != nil {
		writeErr(w, err)
		return
	}
	common.WriteJSON(w, http.StatusOK, map[string]any{"blacklist": list, "total": len(list)})
}

// hCopyTopN serves GET /futures/copy/topn: highest-ranked active traders by
// score, optionally filtered to one area. Public (no auth), like the plaza.
func (s *Server) hCopyTopN(w http.ResponseWriter, r *http.Request) {
	ctx := r.Context()
	ts, err := s.activeTraders(ctx)
	if err != nil {
		writeErr(w, err)
		return
	}
	ident := strings.ToUpper(strings.TrimSpace(r.URL.Query().Get("dealer_identity")))
	if ident == "" {
		ident = strings.ToUpper(strings.TrimSpace(r.URL.Query().Get("identity")))
	}
	days := statDays(r)
	out := []TraderStats{}
	for _, t := range ts {
		if ident != "" && ident != "ALL" && !strings.EqualFold(t.DealerIdentity, ident) {
			continue
		}
		st, err := s.traderStats(ctx, t, days, false)
		if err != nil {
			writeErr(w, err)
			return
		}
		st.InvitationCode = ""
		out = append(out, st)
	}
	sort.SliceStable(out, func(i, j int) bool { return out[i].Score > out[j].Score })
	n := limitParam(r, 10, 100)
	if len(out) > n {
		out = out[:n]
	}
	common.WriteJSON(w, http.StatusOK, map[string]any{"traders": out, "topn": out, "days": days})
}
