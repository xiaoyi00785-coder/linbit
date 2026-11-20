package main

import (
	"net/http"

	"github.com/linbit/platform/services/go/common"
)

// POST /im/token
//
// Flutter calls POST /mapi/user/login (EasiCoin) or the compat endpoint, which
// the gateway routes here. The caller is already signed in to Linbit with the
// normal account JWT (sent as Authorization: Bearer ...): we re-sign a short-
// lived access token with subject = uid so the WebSocket handshake can carry
// it as a query parameter even when the mobile stack strips headers.
//
// The response shape mirrors the Tencent IM "login" payload the Flutter SDK
// expects: {usersig, sdkAppId, userId, expireTime, wsUrl}. "usersig" IS the
// JWT (any string is accepted by the SDK; the app only verifies against our
// own gateway).
func (s *Server) handleToken(w http.ResponseWriter, r *http.Request) {
	// Explicit body { uid?: string } is accepted for parity with the legacy
	// endpoint but is ignored: the uid always comes from the validated JWT or
	// the gateway-injected X-User-Id header.
	uid := r.Header.Get("X-User-Id")
	if uid == "" {
		if claims, err := s.parseBearer(r); err == nil {
			uid = claims.Subject
		}
	}
	if uid == "" {
		common.WriteError(w, http.StatusUnauthorized, "AUTH_UNAUTHORIZED", "请先登录")
		return
	}
	now := s.now()
	token, err := common.SignAccessToken(s.cfg.JWTSecret, uid, "", s.cfg.IMTokenTTL, now)
	if err != nil {
		common.WriteError(w, http.StatusInternalServerError, "INTERNAL", "签发失败")
		return
	}
	exp := now.Add(s.cfg.IMTokenTTL).Unix()
	common.WriteJSON(w, http.StatusOK, map[string]any{
		"enabled":    true,
		"userId":     uid,
		"userID":     uid, // tencent-im casing
		"usersig":    token,
		"userSig":    token,
		"sdkAppId":   1, // placeholder: no third-party SDK in this stack
		"sdkAppID":   1,
		"expireTime": exp,
		"expiresAt":  exp * 1000,
		"wsUrl":      "/im/ws",
		"token":      token,
	})
}

// GET /im/users/{uid}/profile — a thin profile card for display beside messages.
// The fields are best-effort: uid is always returned; nickname/avatar come from
// the account service when it is configured, otherwise a fallback derived from
// the uid is returned so the UI never shows blanks.
func (s *Server) handleUserProfile(w http.ResponseWriter, r *http.Request) {
	uid := r.PathValue("uid")
	if uid == "" {
		common.WriteError(w, http.StatusBadRequest, "VALIDATION_ERROR", "缺少 uid")
		return
	}
	name, avatar := s.fetchProfile(r.Context(), uid)
	if name == "" {
		name = "User " + shortUID(uid)
	}
	common.WriteJSON(w, http.StatusOK, map[string]any{
		"userId":   uid,
		"userID":   uid,
		"nickname": name,
		"nickName": name,
		"faceUrl":  avatar,
		"avatar":   avatar,
	})
}

// shortUID returns at most the first 8 characters of uid for display fallback.
func shortUID(uid string) string {
	if len(uid) <= 8 {
		return uid
	}
	return uid[:8]
}
