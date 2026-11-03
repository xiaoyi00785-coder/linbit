package main

import (
	"net/http"
	"strconv"
	"strings"
	"time"
	"unicode"

	"github.com/linbit/platform/services/go/common"
)

// growthfeed.go: internal reads for the growth service (invite attribution,
// newbie tasks). Invite codes are stored as typed (normalized); an unknown
// code simply attributes nobody.

func normalizeInviteCode(c string) string {
	c = strings.ToUpper(strings.TrimSpace(c))
	if len(c) == 0 || len(c) > 32 {
		return ""
	}
	for _, r := range c {
		if !unicode.IsLetter(r) && !unicode.IsDigit(r) || r > unicode.MaxASCII {
			return ""
		}
	}
	return c
}

type growthUser struct {
	ID            int64  `json:"id"`
	UID           string `json:"uid"`
	InviteCode    string `json:"inviteCode"`
	KYCLevel      int    `json:"kycLevel"`
	EmailVerified bool   `json:"emailVerified"`
	PhoneVerified bool   `json:"phoneVerified"`
	CreatedAt     int64  `json:"createdAt"`
}

const growthUserCols = `id, uid, invite_code, kyc_level, email_verified, phone_verified, created_at`

func scanGrowthUser(sc interface{ Scan(...any) error }) (growthUser, error) {
	var u growthUser
	var at time.Time
	err := sc.Scan(&u.ID, &u.UID, &u.InviteCode, &u.KYCLevel, &u.EmailVerified, &u.PhoneVerified, &at)
	u.CreatedAt = at.UnixMilli()
	return u, err
}

// GET /internal/users/feed?after=<id>&limit=
func (s *Server) handleInternalUsersFeed(w http.ResponseWriter, r *http.Request) {
	if !s.authedInternal(r) {
		common.WriteError(w, http.StatusUnauthorized, "AUTH_UNAUTHORIZED", "内部令牌无效")
		return
	}
	after, _ := strconv.ParseInt(r.URL.Query().Get("after"), 10, 64)
	limit, _ := strconv.Atoi(r.URL.Query().Get("limit"))
	if limit <= 0 || limit > 1000 {
		limit = 500
	}
	rows, err := s.store.db.QueryContext(r.Context(), `SELECT `+growthUserCols+` FROM users WHERE id > $1 ORDER BY id LIMIT $2`, after, limit)
	if err != nil {
		common.WriteError(w, http.StatusInternalServerError, "INTERNAL", "服务器内部错误")
		return
	}
	defer rows.Close()
	items := []growthUser{}
	for rows.Next() {
		u, err := scanGrowthUser(rows)
		if err != nil {
			common.WriteError(w, http.StatusInternalServerError, "INTERNAL", "服务器内部错误")
			return
		}
		items = append(items, u)
	}
	common.WriteJSON(w, http.StatusOK, map[string]any{"items": items})
}

// GET /internal/users/profile?uid=a,b,c — current KYC / bind status.
func (s *Server) handleInternalUserProfile(w http.ResponseWriter, r *http.Request) {
	if !s.authedInternal(r) {
		common.WriteError(w, http.StatusUnauthorized, "AUTH_UNAUTHORIZED", "内部令牌无效")
		return
	}
	uids := strings.Split(r.URL.Query().Get("uid"), ",")
	if len(uids) > 500 {
		common.WriteError(w, http.StatusBadRequest, "VALIDATION_ERROR", "uid 过多")
		return
	}
	items := []growthUser{}
	for _, uid := range uids {
		if uid = strings.TrimSpace(uid); uid == "" {
			continue
		}
		u, err := scanGrowthUser(s.store.db.QueryRowContext(r.Context(), `SELECT `+growthUserCols+` FROM users WHERE uid = $1`, uid))
		if err == nil {
			items = append(items, u)
		}
	}
	common.WriteJSON(w, http.StatusOK, map[string]any{"items": items})
}
