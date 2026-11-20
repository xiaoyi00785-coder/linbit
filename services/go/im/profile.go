package main

import (
	"context"
	"encoding/json"
	"net/http"
	"net/url"
)

// fetchProfile returns the nickname + avatar for uid read from the account
// service. Missing configuration or a transport error returns zero values; a
// handler must fall back to a placeholder (never show an error for a profile
// card).
func (s *Server) fetchProfile(ctx context.Context, uid string) (nickname, avatar string) {
	if s.cfg.AccountInternalURL == "" || s.cfg.InternalToken == "" {
		return "", ""
	}
	req, err := http.NewRequestWithContext(ctx, http.MethodGet,
		s.cfg.AccountInternalURL+"/internal/profile?userId="+url.QueryEscape(uid), nil)
	if err != nil {
		return "", ""
	}
	req.Header.Set("X-Internal-Token", s.cfg.InternalToken)
	resp, err := s.http.Do(req)
	if err != nil {
		return "", ""
	}
	defer resp.Body.Close()
	if resp.StatusCode != http.StatusOK {
		return "", ""
	}
	var body struct {
		Nickname string `json:"nickname"`
		Avatar   string `json:"avatar"`
	}
	if err := json.NewDecoder(resp.Body).Decode(&body); err != nil {
		return "", ""
	}
	return body.Nickname, body.Avatar
}
