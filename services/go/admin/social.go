package main

import (
	"context"
	"database/sql"
	"encoding/json"
	"errors"
	"net/http"
	"net/url"
	"strings"
	"time"

	"github.com/linbit/platform/services/go/common"
)

const socialSettingsKey = "site.social"

var socialChannels = []string{"x", "telegram", "facebook", "discord", "instagram", "youtube", "tiktok", "reddit", "medium"}

func emptySocialLinks() map[string]string {
	links := make(map[string]string, len(socialChannels))
	for _, channel := range socialChannels {
		links[channel] = ""
	}
	return links
}

// socialURL accepts only absolute HTTP(S) links. Public responses additionally
// exclude HTTP links so production never advertises a cleartext community URL.
func socialURL(raw string) (string, error) {
	raw = strings.TrimSpace(raw)
	if raw == "" {
		return "", nil
	}
	if len(raw) > 2048 {
		return "", errors.New("url too long")
	}
	u, err := url.Parse(raw)
	if err != nil || u.Host == "" || u.User != nil || !u.IsAbs() || (strings.ToLower(u.Scheme) != "https" && strings.ToLower(u.Scheme) != "http") {
		return "", errors.New("invalid url")
	}
	return u.String(), nil
}

func validateSocialPatch(patch map[string]*string) (map[string]string, error) {
	validated := make(map[string]string, len(patch))
	for channel, value := range patch {
		if value == nil {
			return nil, errors.New("null is not a valid social link")
		}
		known := false
		for _, candidate := range socialChannels {
			known = known || channel == candidate
		}
		if !known {
			return nil, errors.New("unknown social channel")
		}
		link, err := socialURL(*value)
		if err != nil {
			return nil, err
		}
		validated[channel] = link
	}
	return validated, nil
}

func publicSocialLinks(links map[string]string) map[string]string {
	public := emptySocialLinks()
	for channel, raw := range links {
		if _, known := public[channel]; !known {
			continue
		}
		link, err := socialURL(raw)
		if err != nil {
			continue
		}
		parsed, _ := url.Parse(link)
		if !strings.EqualFold(parsed.Scheme, "https") {
			continue
		}
		public[channel] = link
	}
	return public
}

func (s *Server) loadSocialLinks(ctx context.Context) (map[string]string, *time.Time, error) {
	links := emptySocialLinks()
	var raw string
	var updatedAt time.Time
	err := s.db.QueryRowContext(ctx, `SELECT value, updated_at FROM admin_settings WHERE key=$1`, socialSettingsKey).Scan(&raw, &updatedAt)
	if errors.Is(err, sql.ErrNoRows) {
		return links, nil, nil
	}
	if err != nil {
		return nil, nil, err
	}
	var stored map[string]string
	if err := json.Unmarshal([]byte(raw), &stored); err != nil {
		return nil, nil, err
	}
	for channel, raw := range stored {
		link, err := socialURL(raw)
		if err != nil || link == "" {
			continue
		}
		if _, ok := links[channel]; ok {
			links[channel] = link
		}
	}
	return links, &updatedAt, nil
}

// GET /admin/v1/site/social includes empty channels for editing; GET
// /site/social is anonymous and only exposes valid HTTPS links.
func (s *Server) handleSocialGet(w http.ResponseWriter, r *http.Request) {
	links, updatedAt, err := s.loadSocialLinks(r.Context())
	if err != nil {
		internalErr(w, "social settings read", err)
		return
	}
	w.Header().Set("Cache-Control", "no-store")
	common.WriteJSON(w, http.StatusOK, map[string]any{"links": links, "updatedAt": updatedAt})
}

func (s *Server) handlePublicSocial(w http.ResponseWriter, r *http.Request) {
	links, _, err := s.loadSocialLinks(r.Context())
	if err != nil {
		internalErr(w, "public social read", err)
		return
	}
	w.Header().Set("Cache-Control", "public, max-age=30, stale-while-revalidate=60")
	common.WriteJSON(w, http.StatusOK, map[string]any{"links": publicSocialLinks(links)})
}

// PUT /admin/v1/site/social is a validated partial update of one JSON setting;
// the audit record retains the entire before/after set, including removals.
func (s *Server) handleSocialPut(w http.ResponseWriter, r *http.Request) {
	var req struct {
		Links    map[string]*string `json:"links"`
		TOTPCode string             `json:"totpCode"`
		Reason   string             `json:"reason"`
	}
	if err := common.DecodeJSON(r, &req); err != nil {
		common.WriteError(w, http.StatusBadRequest, "VALIDATION_ERROR", "请求格式错误")
		return
	}
	if len(req.Links) == 0 {
		common.WriteError(w, http.StatusBadRequest, "VALIDATION_ERROR", "至少提交一个社区渠道")
		return
	}
	patch, err := validateSocialPatch(req.Links)
	if err != nil {
		common.WriteError(w, http.StatusBadRequest, "VALIDATION_ERROR", "社区链接须为合法的 http(s) 绝对 URL，渠道不能为空或未知")
		return
	}
	reason := strings.TrimSpace(req.Reason)
	if reason == "" {
		common.WriteError(w, http.StatusBadRequest, "REASON_REQUIRED", "请填写操作原因")
		return
	}
	actor := actorOf(r)
	if !s.require2FA(w, actor, req.TOTPCode) {
		return
	}
	before, _, err := s.loadSocialLinks(r.Context())
	if err != nil {
		internalErr(w, "social settings before", err)
		return
	}
	after := emptySocialLinks()
	for channel, link := range before {
		after[channel] = link
	}
	for channel, link := range patch {
		after[channel] = link
	}
	encoded, _ := json.Marshal(after)
	if _, err := s.db.ExecContext(r.Context(), `INSERT INTO admin_settings (key, value, updated_by) VALUES ($1,$2,$3)
		ON CONFLICT (key) DO UPDATE SET value=EXCLUDED.value, updated_by=EXCLUDED.updated_by, updated_at=now()`, socialSettingsKey, encoded, actor.Username); err != nil {
		internalErr(w, "social settings write", err)
		return
	}
	s.audit(r, actor, "content", "social_settings_update", "setting:"+socialSettingsKey, levelHigh, "ok", before, after, reason)
	w.Header().Set("Cache-Control", "no-store")
	common.WriteJSON(w, http.StatusOK, map[string]any{"links": after, "updatedBy": actor.Username})
}
