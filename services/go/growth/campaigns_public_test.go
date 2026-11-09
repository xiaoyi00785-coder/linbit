package main

import (
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
)

func TestCampaignsStayUnconfigured(t *testing.T) {
	s := &Server{}
	w := httptest.NewRecorder()
	s.handleDepositCashback(w, httptest.NewRequest(http.MethodGet, "/campaigns/deposit-cashback", nil))
	body := w.Body.String()
	if w.Code != 200 || !strings.Contains(body, `"configured":false`) || !strings.Contains(body, `"tiers":[]`) || strings.Contains(body, "bonus") {
		t.Fatalf("cashback %s", body)
	}
	w = httptest.NewRecorder()
	req := httptest.NewRequest(http.MethodPost, "/campaigns/deposit-cashback/tiers/t1/join", nil)
	req.SetPathValue("id", "t1")
	s.handleDepositCashbackJoin(w, req, "80000001")
	if w.Code != http.StatusConflict || !strings.Contains(w.Body.String(), `"joined":false`) {
		t.Fatalf("join %d %s", w.Code, w.Body.String())
	}
	w = httptest.NewRecorder()
	s.handleTradeLottery(w, httptest.NewRequest(http.MethodGet, "/campaigns/trade-lottery", nil))
	if !strings.Contains(w.Body.String(), `"winners":[]`) || !strings.Contains(w.Body.String(), `"availableDraws":0`) || strings.Contains(w.Body.String(), "BTC") {
		t.Fatalf("lottery %s", w.Body.String())
	}
	w = httptest.NewRecorder()
	s.handleLotteryEntries(w, httptest.NewRequest(http.MethodGet, "/campaigns/trade-lottery/entries?status=pending", nil), "80000001")
	if !strings.Contains(w.Body.String(), `"items":[]`) {
		t.Fatalf("entries %s", w.Body.String())
	}
}
