package main

import (
	"encoding/json"
	"fmt"
	"net/http"
	"net/url"
	"strconv"
	"strings"

	"github.com/linbit/platform/services/go/common"
	"github.com/shopspring/decimal"
)

// mm.go: market-maker controls. Reads need mm:view; every write needs mm:edit,
// a fresh 2FA code and is audited (high). MM account credit (mm_testnet_seed)
// is allowed on every network, including mainnet: the operator can top up the
// market-maker spot account from the admin UI. Credits remain unbacked in the
// solvency report (virtualSeed).

func (s *Server) handleMMGet(path string) http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		u, err := s.call(r.Context(), http.MethodGet, s.cfg.MMURL, path, nil)
		if err != nil {
			upstreamErr(w, "mm", err)
			return
		}
		relay(w, u)
	}
}

func resultOf(u upstream) string {
	if u.ok() {
		return "ok"
	}
	return "failed:" + u.errCode()
}

// PUT /admin/v1/mm/pairs/{market}/{symbol} {<params...>, reason, totpCode}
func (s *Server) handleMMUpdatePair(w http.ResponseWriter, r *http.Request) {
	a := actorOf(r)
	market, sym := r.PathValue("market"), strings.ToUpper(r.PathValue("symbol"))
	if market != "spot" && market != "futures" {
		common.WriteError(w, http.StatusBadRequest, "VALIDATION_ERROR", "market 必须为 spot/futures")
		return
	}
	var body map[string]json.RawMessage
	if err := common.DecodeJSON(r, &body); err != nil {
		common.WriteError(w, http.StatusBadRequest, "VALIDATION_ERROR", "请求格式错误")
		return
	}
	var code, reason string
	_ = json.Unmarshal(body["totpCode"], &code)
	_ = json.Unmarshal(body["reason"], &reason)
	delete(body, "totpCode")
	delete(body, "reason")
	delete(body, "updatedBy")
	if !s.verify2FA(w, a, code) {
		return
	}
	// batch6: 只有「停用做市」可以直接生效（应急）；其余参数变更走双人审批。
	if !mmPureDisable(body) {
		if strings.TrimSpace(reason) == "" {
			common.WriteError(w, http.StatusBadRequest, "VALIDATION_ERROR", "请填写变更原因")
			return
		}
		if err := mmPrecheck(body); err != nil {
			common.WriteError(w, http.StatusBadRequest, "INVALID_PARAM", err.Error())
			return
		}
		raw, _ := json.Marshal(body)
		fa, err := buildFundAction("mm_pair", market+":"+sym, raw)
		if err != nil {
			common.WriteError(w, http.StatusBadRequest, "VALIDATION_ERROR", err.Error())
			return
		}
		id, err := s.submitFundChange(r.Context(), a, fa, reason)
		if err != nil {
			common.WriteError(w, http.StatusConflict, "CHANGE_REFUSED", err.Error())
			return
		}
		s.audit(r, a, fundChangeModule, "submit", "mm_pair:"+market+"/"+sym, levelHigh, "ok", nil, body, reason)
		writePending(w, id, "mm_pair")
		return
	}
	body["updatedBy"], _ = json.Marshal(a.Username)
	u, err := s.call(r.Context(), http.MethodPut, s.cfg.MMURL, "/internal/mm/pairs/"+url.PathEscape(market)+"/"+url.PathEscape(sym), body)
	if err != nil {
		upstreamErr(w, "mm pair", err)
		return
	}
	delete(body, "updatedBy")
	s.audit(r, a, "mm", "update_pair", "mm:"+market+"/"+sym, levelHigh, resultOf(u), nil, body, reason)
	relay(w, u)
}

// POST /admin/v1/mm/start|stop {reason, totpCode}
func (s *Server) handleMMRun(action string) http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		a := actorOf(r)
		var req reasonReq
		_ = common.DecodeJSON(r, &req)
		if !s.verify2FA(w, a, req.TOTPCode) {
			return
		}
		u, err := s.call(r.Context(), http.MethodPost, s.cfg.MMURL, "/internal/mm/"+action, map[string]string{"by": a.Username})
		if err != nil {
			upstreamErr(w, "mm "+action, err)
			return
		}
		s.audit(r, a, "mm", action, "mm:"+s.cfg.MMUserID, levelHigh, resultOf(u), nil, nil, strings.TrimSpace(req.Reason))
		relay(w, u)
	}
}

// POST /admin/v1/mm/testnet-seed and POST /admin/v1/mm/credit
// {ref, assets?, usdtValue?, amounts?, asset?, amount?, reason, totpCode}
// — single-asset mode {asset:"USDT", amount:"5000000"} credits exactly that
// amount of one asset to the MM spot account (reason required, amount <=
// ADMIN_MM_SEED_MAX (legacy MM_SEED_MAX), default 10,000,000).
// — allowed on mainnet. Without assets: MM_SEED_AMOUNT USDT. With assets
// (e.g. ["BTC","ETH","SOL"]): each credited ~usdtValue (default 1,000,000) of
// the base asset at the current index, idempotent per ref+asset.
func (s *Server) handleMMSeed(w http.ResponseWriter, r *http.Request) {
	a := actorOf(r)
	var req struct {
		Ref       string            `json:"ref"`
		Assets    []string          `json:"assets"`
		USDTValue string            `json:"usdtValue"`
		Amounts   map[string]string `json:"amounts"`
		Asset     string            `json:"asset"`
		Amount    string            `json:"amount"`
		Reason    string            `json:"reason"`
		TOTPCode  string            `json:"totpCode"`
	}
	_ = common.DecodeJSON(r, &req)
	req.Ref = strings.TrimSpace(req.Ref)
	if req.Ref == "" {
		common.WriteError(w, http.StatusBadRequest, "VALIDATION_ERROR", "ref 必填（幂等键）")
		return
	}
	req.Asset = strings.ToUpper(strings.TrimSpace(req.Asset))
	single := req.Asset != "" || strings.TrimSpace(req.Amount) != ""
	if single {
		if msg := validateSingleSeed(req.Asset, req.Amount, req.Assets, req.Reason, s.cfg.MMSeedMax); msg != "" {
			common.WriteError(w, http.StatusBadRequest, "VALIDATION_ERROR", msg)
			return
		}
	}
	if !s.verify2FA(w, a, req.TOTPCode) {
		return
	}
	payload := map[string]any{"ref": req.Ref}
	if single {
		payload["asset"], payload["amount"] = req.Asset, strings.TrimSpace(req.Amount)
	} else if len(req.Assets) > 0 {
		payload["assets"] = req.Assets
		if req.USDTValue != "" {
			payload["usdtValue"] = req.USDTValue
		}
		if len(req.Amounts) > 0 {
			payload["amounts"] = req.Amounts
		}
	}
	u, err := s.call(r.Context(), http.MethodPost, s.cfg.MMURL, "/internal/mm/seed", payload)
	if err != nil {
		upstreamErr(w, "mm seed", err)
		return
	}
	after := map[string]any{"ref": req.Ref, "txType": "mm_testnet_seed", "assets": req.Assets, "usdtValue": req.USDTValue, "amounts": req.Amounts}
	if single {
		after = map[string]any{"ref": req.Ref, "txType": "mm_testnet_seed", "mode": "single", "asset": req.Asset, "amount": strings.TrimSpace(req.Amount), "account": "spot"}
	}
	if u.ok() && len(req.Assets) > 0 {
		var res struct {
			Items json.RawMessage `json:"items"`
		}
		if json.Unmarshal(u.Body, &res) == nil {
			after["items"] = res.Items
		}
	}
	s.audit(r, a, "mm", "testnet_seed", "mm:"+s.cfg.MMUserID, levelHigh, resultOf(u), nil, after, req.Reason)
	relay(w, u)
}

// POST /admin/v1/mm/futures-transfer {direction in|out, amount, reason, totpCode}
func (s *Server) handleMMTransfer(w http.ResponseWriter, r *http.Request) {
	a := actorOf(r)
	var req struct {
		Direction string `json:"direction"`
		Amount    string `json:"amount"`
		Reason    string `json:"reason"`
		TOTPCode  string `json:"totpCode"`
	}
	_ = common.DecodeJSON(r, &req)
	if !s.verify2FA(w, a, req.TOTPCode) {
		return
	}
	u, err := s.call(r.Context(), http.MethodPost, s.cfg.MMURL, "/internal/mm/futures-transfer",
		map[string]string{"direction": req.Direction, "amount": req.Amount})
	if err != nil {
		upstreamErr(w, "mm transfer", err)
		return
	}
	s.audit(r, a, "mm", "futures_transfer", "mm:"+s.cfg.MMUserID, levelHigh, resultOf(u), nil,
		map[string]string{"direction": req.Direction, "amount": req.Amount}, req.Reason)
	relay(w, u)
}

// validateSingleSeed checks a single-asset testnet seed request; "" = ok.
func validateSingleSeed(asset, amount string, assets []string, reason, max string) string {
	if len(assets) > 0 {
		return "asset/amount 不能与 assets 同时使用"
	}
	if asset == "" || len(asset) > 16 || strings.Trim(asset, "ABCDEFGHIJKLMNOPQRSTUVWXYZ0123456789") != "" {
		return "asset 不合法"
	}
	if strings.TrimSpace(reason) == "" {
		return "reason 必填"
	}
	amt, err := decimal.NewFromString(strings.TrimSpace(amount))
	if err != nil || !amt.IsPositive() || amt.Exponent() < -8 {
		return "amount 不合法"
	}
	if m, err := decimal.NewFromString(max); err == nil && amt.GreaterThan(m) {
		return "amount 超过单次上限 " + m.String()
	}
	return ""
}

// PUT /admin/v1/mm/baselines/{asset} {targetBase?|clearTarget?|resetStart?, reason, totpCode}
// Sets the spot inventory baseline excluded from max_position (net position cap).
func (s *Server) handleMMUpdateBaseline(w http.ResponseWriter, r *http.Request) {
	a := actorOf(r)
	asset := strings.ToUpper(r.PathValue("asset"))
	var body map[string]json.RawMessage
	if err := common.DecodeJSON(r, &body); err != nil {
		common.WriteError(w, http.StatusBadRequest, "VALIDATION_ERROR", "请求格式错误")
		return
	}
	var code, reason string
	_ = json.Unmarshal(body["totpCode"], &code)
	_ = json.Unmarshal(body["reason"], &reason)
	if strings.TrimSpace(reason) == "" {
		common.WriteError(w, http.StatusBadRequest, "VALIDATION_ERROR", "reason 必填")
		return
	}
	out := map[string]json.RawMessage{}
	for _, k := range []string{"targetBase", "clearTarget", "resetStart"} {
		if v, ok := body[k]; ok {
			out[k] = v
		}
	}
	if !s.verify2FA(w, a, code) {
		return
	}
	// batch6: 库存基线影响做市可动用资金，走双人审批。
	{
		raw, _ := json.Marshal(out)
		fa, err := buildFundAction("mm_baseline", asset, raw)
		if err != nil {
			common.WriteError(w, http.StatusBadRequest, "VALIDATION_ERROR", err.Error())
			return
		}
		id, err := s.submitFundChange(r.Context(), a, fa, reason)
		if err != nil {
			common.WriteError(w, http.StatusConflict, "CHANGE_REFUSED", err.Error())
			return
		}
		s.audit(r, a, fundChangeModule, "submit", "mm_baseline:"+asset, levelHigh, "ok", nil, out, reason)
		writePending(w, id, "mm_baseline")
		return
	}
}

// mmPureDisable: 请求体只包含 enabled=false（紧急停用某交易对做市）。
func mmPureDisable(body map[string]json.RawMessage) bool {
	if len(body) != 1 {
		return false
	}
	v, ok := body["enabled"]
	return ok && strings.TrimSpace(string(v)) == "false"
}

// mmPrecheck 与 marketmaker pairPatch.Validate 保持一致，在提交审批前拦截越界参数，
// 避免把注定失败的变更送进审批队列。最终仍以做市服务的校验为准。
func mmPrecheck(body map[string]json.RawMessage) error {
	type rng struct{ lo, hi float64 }
	bounds := map[string]rng{
		"spreadBps": {0.5, 1000}, "levels": {1, 50}, "levelStepBps": {0, 500}, "levelNotional": {0.00000001, 10000000},
		"sizeGrowth": {1, 3}, "refreshMs": {500, 60000}, "requoteBps": {0, 100}, "targetBaseRatio": {0, 1},
		"maxPosition": {0, 1e12}, "maxSkewBps": {0, 500}, "leverage": {1, 125},
	}
	for k, b := range bounds {
		v, ok := body[k]
		if !ok || string(v) == "null" {
			continue
		}
		str := strings.Trim(strings.TrimSpace(string(v)), `"`)
		f, err := strconv.ParseFloat(str, 64)
		if err != nil || f < b.lo || f > b.hi {
			return fmt.Errorf("%s 必须在 %v–%v 之间", k, b.lo, b.hi)
		}
	}
	return nil
}
