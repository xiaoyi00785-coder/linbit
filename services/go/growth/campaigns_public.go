package main

import (
	"net/http"

	"github.com/linbit/platform/services/go/common"
)

// Public campaign shells for the Android profile pages.
// No campaign row is seeded. Unconfigured responses contain no prizes, winners,
// tiers or personal entries, and join does not insert a participation record.

func (s *Server) campaignRoutes(mux *http.ServeMux) {
	mux.HandleFunc("GET /campaigns/deposit-cashback", s.handleDepositCashback)
	mux.HandleFunc("POST /campaigns/deposit-cashback/tiers/{id}/join", s.user(s.handleDepositCashbackJoin))
	mux.HandleFunc("GET /campaigns/trade-lottery", s.handleTradeLottery)
	mux.HandleFunc("GET /campaigns/trade-lottery/entries", s.user(s.handleLotteryEntries))
}

func (s *Server) handleDepositCashback(w http.ResponseWriter, r *http.Request) {
	common.WriteJSON(w, http.StatusOK, map[string]any{
		"configured": false, "available": false, "eligible": false,
		"title": "", "subtitle": "", "startAt": nil, "endAt": nil,
		"tiers": []any{}, "rules": []string{},
	})
}

func (s *Server) handleDepositCashbackJoin(w http.ResponseWriter, r *http.Request, uid string) {
	common.WriteJSON(w, http.StatusConflict, map[string]any{
		"code": "CAMPAIGN_UNCONFIGURED", "message": "充值返现活动未配置，不能参加",
		"configured": false, "joined": false, "tierId": r.PathValue("id"), "requestId": common.RequestID(),
	})
	_ = uid
}

func (s *Server) handleTradeLottery(w http.ResponseWriter, r *http.Request) {
	common.WriteJSON(w, http.StatusOK, map[string]any{
		"configured": false, "available": false,
		"title": "", "startAt": nil, "endAt": nil,
		"prizes": []any{}, "winners": []string{}, "rules": []any{},
		"availableDraws": 0,
	})
}

func (s *Server) handleLotteryEntries(w http.ResponseWriter, r *http.Request, uid string) {
	st := r.URL.Query().Get("status")
	if st != "" && st != "pending" && st != "drawn" {
		common.WriteError(w, http.StatusBadRequest, "VALIDATION_ERROR", "status 仅支持 pending 或 drawn")
		return
	}
	_ = uid
	common.WriteJSON(w, http.StatusOK, map[string]any{"items": []any{}, "configured": false})
}
