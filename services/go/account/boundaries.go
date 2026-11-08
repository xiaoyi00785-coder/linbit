package main

import (
	"net/http"
	"net/url"
	"strings"

	"github.com/linbit/platform/services/go/common"
)

// Guest support and fiat on-ramp configuration boundaries.
// Missing vendor settings stay explicitly unavailable. No payment or chat session is created.

func publicHTTPS(raw string) (string, bool) {
	u, err := url.Parse(strings.TrimSpace(raw))
	if err != nil || u.Scheme != "https" || u.Host == "" || u.User != nil || u.RawQuery != "" || u.Fragment != "" {
		return "", false
	}
	return u.Scheme + "://" + u.Host + u.EscapedPath(), true
}

func (s *Server) handleGuestSupport(w http.ResponseWriter, r *http.Request) {
	out := map[string]any{
		"configured": false, "available": false, "mode": "unconfigured", "provider": "",
		"entryUrl": nil, "loginRequiredForTickets": true, "ticketsPath": "/support/tickets",
	}
	provider := strings.TrimSpace(s.cfg.SupportGuestProvider)
	entry, ok := publicHTTPS(s.cfg.SupportGuestEntryURL)
	if provider != "" && ok {
		out["configured"] = true
		out["available"] = true
		out["mode"] = "external"
		out["provider"] = provider
		out["entryUrl"] = entry
	} else if provider != "" || strings.TrimSpace(s.cfg.SupportGuestEntryURL) != "" {
		out["reason"] = "invalid_config"
	}
	common.WriteJSON(w, http.StatusOK, out)
}

func (s *Server) fiatBuyState() (configured bool, reason string) {
	name := strings.TrimSpace(s.cfg.FiatOnRampProvider)
	_, ok := publicHTTPS(s.cfg.FiatOnRampAPIBase)
	if name == "" && strings.TrimSpace(s.cfg.FiatOnRampAPIBase) == "" {
		return false, "unconfigured"
	}
	if name == "" || !ok {
		return false, "invalid_config"
	}
	return true, "provider_not_integrated"
}

func (s *Server) handleFiatBuyConfig(w http.ResponseWriter, r *http.Request) {
	configured, reason := s.fiatBuyState()
	common.WriteJSON(w, http.StatusOK, map[string]any{
		"configured": configured, "available": false, "provider": strings.TrimSpace(s.cfg.FiatOnRampProvider),
		"reason": reason, "quotes": false, "orders": false, "charged": false,
	})
}

func (s *Server) handleFiatBuyUnavailable(w http.ResponseWriter, r *http.Request) {
	configured, reason := s.fiatBuyState()
	common.WriteJSON(w, http.StatusServiceUnavailable, map[string]any{
		"code": "FIAT_UNAVAILABLE", "message": "法币买币暂不可用", "configured": configured,
		"available": false, "reason": reason, "charged": false, "orderCreated": false,
		"requestId": common.RequestID(),
	})
}
