package main

import (
	"context"
	"crypto/subtle"
	"errors"
	"net/http"
	"sort"
	"strings"
	"time"

	"github.com/linbit/platform/services/go/common"
	"github.com/shopspring/decimal"
)

// Routes: everything is internal (X-Internal-Token); the admin service adds
// RBAC, 2FA and audit in front of the write operations.
func (m *Maker) Routes() http.Handler {
	mux := http.NewServeMux()
	mux.HandleFunc("GET /healthz", func(w http.ResponseWriter, _ *http.Request) {
		common.WriteJSON(w, 200, map[string]string{"status": "ok"})
	})
	mux.HandleFunc("GET /internal/mm/status", m.guard(m.handleStatus))
	mux.HandleFunc("GET /internal/mm/pairs", m.guard(m.handlePairs))
	mux.HandleFunc("PUT /internal/mm/pairs/{market}/{symbol}", m.guard(m.handleUpdatePair))
	mux.HandleFunc("POST /internal/mm/start", m.guard(m.handleRun(true)))
	mux.HandleFunc("POST /internal/mm/stop", m.guard(m.handleRun(false)))
	mux.HandleFunc("POST /internal/mm/seed", m.guard(m.handleSeed))
	mux.HandleFunc("GET /internal/mm/baselines", m.guard(m.handleBaselines))
	mux.HandleFunc("PUT /internal/mm/baselines/{asset}", m.guard(m.handleUpdateBaseline))
	mux.HandleFunc("POST /internal/mm/futures-transfer", m.guard(m.handleTransfer))
	return mux
}

func (m *Maker) guard(h http.HandlerFunc) http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		if m.cfg.InternalToken != "" && subtle.ConstantTimeCompare([]byte(r.Header.Get("X-Internal-Token")), []byte(m.cfg.InternalToken)) != 1 {
			common.WriteError(w, http.StatusUnauthorized, "UNAUTHORIZED", "invalid internal token")
			return
		}
		h(w, r)
	}
}

func (m *Maker) handleStatus(w http.ResponseWriter, r *http.Request) {
	running, err := m.isRunning(r.Context())
	if err != nil {
		common.WriteError(w, 500, "INTERNAL", "服务器内部错误")
		return
	}
	seed, why := common.TestnetSeedAllowed()
	m.mu.Lock()
	pairs := make([]PairState, 0, len(m.state))
	for _, st := range m.state {
		pairs = append(pairs, *st)
	}
	down := len(m.feedDown)
	last := m.lastCycle
	m.mu.Unlock()
	sort.Slice(pairs, func(i, j int) bool {
		return key(pairs[i].Market, pairs[i].Symbol) < key(pairs[j].Market, pairs[j].Symbol)
	})
	bl := []Baseline{}
	if bs, err := m.Baselines(r.Context()); err == nil {
		for _, b := range bs {
			bl = append(bl, b)
		}
		sort.Slice(bl, func(i, j int) bool { return bl[i].Asset < bl[j].Asset })
	}
	common.WriteJSON(w, 200, map[string]any{
		"baselines": bl, "positionMode": "net (max_position applies to base - baseline)",
		"enabled": m.cfg.Enabled, "network": common.Network(), "running": running, "account": m.cfg.UserID, "pairs": pairs, "feedDownPairs": down,
		"lastCycle": last, "testnetSeedAllowed": seed, "testnetSeedReason": why, "seedAmount": m.cfg.SeedAmount,
		"stopLoss": false, "priceFeed": "market-data /market/index (REST polling: Binance primary, OKX backup)",
	})
}

func (m *Maker) handlePairs(w http.ResponseWriter, r *http.Request) {
	ps, err := m.Pairs(r.Context())
	if err != nil {
		common.WriteError(w, 500, "INTERNAL", "服务器内部错误")
		return
	}
	if ps == nil {
		ps = []Pair{}
	}
	common.WriteJSON(w, 200, map[string]any{"items": ps})
}

type pairPatch struct {
	Enabled         *bool            `json:"enabled"`
	SpreadBps       *decimal.Decimal `json:"spreadBps"`
	Levels          *int             `json:"levels"`
	LevelStepBps    *decimal.Decimal `json:"levelStepBps"`
	LevelNotional   *decimal.Decimal `json:"levelNotional"`
	SizeGrowth      *decimal.Decimal `json:"sizeGrowth"`
	RefreshMs       *int             `json:"refreshMs"`
	RequoteBps      *decimal.Decimal `json:"requoteBps"`
	TargetBaseRatio *decimal.Decimal `json:"targetBaseRatio"`
	MaxPosition     *decimal.Decimal `json:"maxPosition"`
	MaxSkewBps      *decimal.Decimal `json:"maxSkewBps"`
	Leverage        *int             `json:"leverage"`
	UpdatedBy       string           `json:"updatedBy"`
}

func inRange(d *decimal.Decimal, lo, hi float64) bool {
	return d == nil || (d.GreaterThanOrEqual(decimal.NewFromFloat(lo)) && d.LessThanOrEqual(decimal.NewFromFloat(hi)))
}
func intIn(v *int, lo, hi int) bool { return v == nil || (*v >= lo && *v <= hi) }

// Validate checks the patch against safe bounds.
func (p pairPatch) Validate() error {
	switch {
	case !inRange(p.SpreadBps, 0.5, 1000):
		return errors.New("spreadBps 必须在 0.5–1000 之间")
	case !intIn(p.Levels, 1, 20):
		return errors.New("levels 必须在 1–20 之间")
	case !inRange(p.LevelStepBps, 0, 500):
		return errors.New("levelStepBps 必须在 0–500 之间")
	case !inRange(p.LevelNotional, 0.00000001, 10000000):
		return errors.New("levelNotional 必须为正且不超过 10,000,000")
	case !inRange(p.SizeGrowth, 1, 3):
		return errors.New("sizeGrowth 必须在 1–3 之间")
	case !intIn(p.RefreshMs, 500, 60000):
		return errors.New("refreshMs 必须在 500–60000 之间")
	case !inRange(p.RequoteBps, 0, 100):
		return errors.New("requoteBps 必须在 0–100 之间")
	case !inRange(p.TargetBaseRatio, 0, 1):
		return errors.New("targetBaseRatio 必须在 0–1 之间")
	case !inRange(p.MaxPosition, 0, 1e12):
		return errors.New("maxPosition 必须 >= 0")
	case !inRange(p.MaxSkewBps, 0, 500):
		return errors.New("maxSkewBps 必须在 0–500 之间")
	case !intIn(p.Leverage, 1, 125):
		return errors.New("leverage 必须在 1–125 之间")
	}
	return nil
}

func (m *Maker) handleUpdatePair(w http.ResponseWriter, r *http.Request) {
	market, sym := r.PathValue("market"), strings.ToUpper(r.PathValue("symbol"))
	var p pairPatch
	if err := common.DecodeJSON(r, &p); err != nil {
		common.WriteError(w, 400, "INVALID_REQUEST", "请求格式错误")
		return
	}
	if err := p.Validate(); err != nil {
		common.WriteError(w, 400, "INVALID_PARAM", err.Error())
		return
	}
	by := p.UpdatedBy
	if by == "" {
		by = "admin"
	}
	row := m.db.QueryRowContext(r.Context(), `UPDATE mm_pairs SET
		enabled=COALESCE($3,enabled), spread_bps=COALESCE($4,spread_bps), levels=COALESCE($5,levels),
		level_step_bps=COALESCE($6,level_step_bps), level_notional=COALESCE($7,level_notional), size_growth=COALESCE($8,size_growth),
		refresh_ms=COALESCE($9,refresh_ms), requote_bps=COALESCE($10,requote_bps), target_base_ratio=COALESCE($11,target_base_ratio),
		max_position=COALESCE($12,max_position), max_skew_bps=COALESCE($13,max_skew_bps), leverage=COALESCE($14,leverage),
		updated_by=$15, updated_at=now()
		WHERE market=$1 AND symbol=$2 RETURNING `+pairCols,
		market, sym, p.Enabled, nd(p.SpreadBps), p.Levels, nd(p.LevelStepBps), nd(p.LevelNotional), nd(p.SizeGrowth),
		p.RefreshMs, nd(p.RequoteBps), nd(p.TargetBaseRatio), nd(p.MaxPosition), nd(p.MaxSkewBps), p.Leverage, by)
	out, err := scanPair(row)
	if err != nil {
		common.WriteError(w, 404, "NOT_FOUND", "交易对不存在")
		return
	}
	common.WriteJSON(w, 200, out)
}

func nd(d *decimal.Decimal) any {
	if d == nil {
		return nil
	}
	return d.String()
}

func (m *Maker) handleRun(run bool) http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		var body struct {
			By string `json:"by"`
		}
		_ = common.DecodeJSON(r, &body)
		if body.By == "" {
			body.By = "admin"
		}
		if run && !m.cfg.Enabled {
			common.WriteError(w, http.StatusConflict, "MM_DISABLED", "做市已通过 MM_ENABLED=false 关闭（主网默认关闭）")
			return
		}
		if _, err := m.db.ExecContext(r.Context(), `UPDATE mm_state SET running=$1, updated_by=$2, updated_at=now() WHERE id=1`, run, body.By); err != nil {
			common.WriteError(w, 500, "INTERNAL", "服务器内部错误")
			return
		}
		cancelled := false
		if !run {
			ctx, cancel := context.WithTimeout(context.Background(), 30*time.Second)
			defer cancel()
			m.CancelEverything(ctx)
			cancelled = true
		}
		common.WriteJSON(w, 200, map[string]any{"running": run, "ordersCancelled": cancelled})
	}
}

// handleSeed credits the MM account with virtual testnet funds via asset.
// Refused outright in production / on mainnet (asset enforces the same rule).
//
//	{ref}                              -> MM_SEED_AMOUNT USDT (unchanged)
//	{ref, assets:["BTC","ETH","SOL"]}  -> per asset, qty = usdtValue / index(<A>USDT)
//	                                      (usdtValue default MM_SEED_BASE_USDT=1000000),
//	                                      or a fixed qty from amounts:{"BTC":"10"}.
//
// Each asset uses its own idempotency ref mm_testnet_seed:<ref>:<ASSET>, so a
// retried call with the same ref is already_applied per asset even if the
// index moved. Virtual seeds are excluded from solvency (virtualSeed per asset).
func (m *Maker) handleSeed(w http.ResponseWriter, r *http.Request) {
	if ok, why := common.TestnetSeedAllowed(); !ok {
		common.WriteError(w, http.StatusForbidden, "SEED_DISABLED", "虚拟注资已禁用："+why)
		return
	}
	defer m.invalidateSeeds() // seeded inventory moves the testnet baseline
	var body struct {
		Ref       string            `json:"ref"`
		Assets    []string          `json:"assets"`
		USDTValue string            `json:"usdtValue"`
		Amounts   map[string]string `json:"amounts"`
		Asset     string            `json:"asset"`
		Amount    string            `json:"amount"`
	}
	if err := common.DecodeJSON(r, &body); err != nil || strings.TrimSpace(body.Ref) == "" {
		common.WriteError(w, 400, "INVALID_REQUEST", "ref 必填")
		return
	}
	if a := strings.ToUpper(strings.TrimSpace(body.Asset)); a != "" || body.Amount != "" {
		// single-asset top-up: exactly {amount} of {asset} into the MM spot
		// account; asset caps it at MM_SEED_MAX and enforces testnet-only.
		amt, err := decimal.NewFromString(strings.TrimSpace(body.Amount))
		if a == "" || err != nil || !amt.IsPositive() {
			common.WriteError(w, 400, "INVALID_REQUEST", "asset/amount 不合法")
			return
		}
		if !SeedAssetAllowed(a, m.cfg.SeedBaseAssets) {
			common.WriteError(w, 400, "ASSET_NOT_ALLOWED", "该资产不可注资")
			return
		}
		var out map[string]any
		err = call(r.Context(), "POST", m.cfg.AssetURL+"/internal/mm/testnet-seed", m.internalHdr(), map[string]any{
			"userId": m.cfg.UserID, "asset": a, "amount": amt.String(), "ref": "mm_testnet_seed:" + body.Ref + ":single:" + a}, &out)
		if err == nil && out != nil {
			out["asset"], out["amount"] = a, amt.String()
		}
		relayErr(w, err, out)
		return
	}
	if len(body.Assets) == 0 {
		var out map[string]any
		err := call(r.Context(), "POST", m.cfg.AssetURL+"/internal/mm/testnet-seed", m.internalHdr(), map[string]any{
			"userId": m.cfg.UserID, "asset": "USDT", "amount": m.cfg.SeedAmount, "ref": "mm_testnet_seed:" + body.Ref}, &out)
		relayErr(w, err, out)
		return
	}
	value, err := decimal.NewFromString(firstNonEmpty(body.USDTValue, m.cfg.SeedBaseUSDT))
	if err != nil || !value.IsPositive() {
		common.WriteError(w, 400, "INVALID_REQUEST", "usdtValue 不合法")
		return
	}
	allowed := map[string]bool{}
	for _, a := range strings.Split(m.cfg.SeedBaseAssets, ",") {
		allowed[strings.ToUpper(strings.TrimSpace(a))] = true
	}
	type result struct {
		Asset      string `json:"asset"`
		Amount     string `json:"amount,omitempty"`
		IndexPrice string `json:"indexPrice,omitempty"`
		Status     string `json:"status"`
		Error      string `json:"error,omitempty"`
	}
	results := []result{}
	seen := map[string]bool{}
	for _, raw := range body.Assets {
		a := strings.ToUpper(strings.TrimSpace(raw))
		if a == "" || seen[a] {
			continue
		}
		seen[a] = true
		if !allowed[a] {
			results = append(results, result{Asset: a, Status: "failed", Error: "ASSET_NOT_ALLOWED"})
			continue
		}
		res := result{Asset: a}
		var qty decimal.Decimal
		if fixed, ok := body.Amounts[a]; ok {
			qty, err = decimal.NewFromString(fixed)
			if err != nil || !qty.IsPositive() {
				results = append(results, result{Asset: a, Status: "failed", Error: "INVALID_AMOUNT"})
				continue
			}
		} else {
			px, _, err := m.index(r.Context(), a+"USDT")
			if err != nil || !px.IsPositive() {
				results = append(results, result{Asset: a, Status: "failed", Error: "PRICE_UNAVAILABLE"})
				continue
			}
			qty = SeedQty(value, px)
			res.IndexPrice = px.String()
		}
		res.Amount = qty.String()
		var out struct {
			Status string `json:"status"`
		}
		err := call(r.Context(), "POST", m.cfg.AssetURL+"/internal/mm/testnet-seed", m.internalHdr(), map[string]any{
			"userId": m.cfg.UserID, "asset": a, "amount": res.Amount, "ref": "mm_testnet_seed:" + body.Ref + ":" + a}, &out)
		var ae *apiErr
		switch {
		case errors.As(err, &ae):
			res.Status, res.Error = "failed", ae.Code
		case err != nil:
			res.Status, res.Error = "failed", "UPSTREAM_UNAVAILABLE"
		default:
			res.Status = out.Status // ok | already_applied
		}
		results = append(results, res)
	}
	common.WriteJSON(w, 200, map[string]any{"txType": "mm_testnet_seed", "usdtValue": value.String(), "items": results})
}

// SeedQty is usdtValue/price truncated to 8 decimals (pure; unit tested).
func SeedQty(value, price decimal.Decimal) decimal.Decimal {
	return value.DivRound(price, 16).Truncate(8)
}

func firstNonEmpty(v ...string) string {
	for _, s := range v {
		if strings.TrimSpace(s) != "" {
			return strings.TrimSpace(s)
		}
	}
	return ""
}

func (m *Maker) handleTransfer(w http.ResponseWriter, r *http.Request) {
	var body struct {
		Direction string `json:"direction"`
		Amount    string `json:"amount"`
	}
	if err := common.DecodeJSON(r, &body); err != nil || (body.Direction != "in" && body.Direction != "out") {
		common.WriteError(w, 400, "INVALID_REQUEST", "direction 必须为 in/out")
		return
	}
	var out map[string]any
	err := call(r.Context(), "POST", m.cfg.FuturesURL+"/futures/transfer", m.userHdr(), body, &out)
	relayErr(w, err, out)
}

func relayErr(w http.ResponseWriter, err error, out any) {
	var ae *apiErr
	if errors.As(err, &ae) {
		common.WriteError(w, ae.Status, ae.Code, ae.Message)
		return
	}
	if err != nil {
		common.WriteError(w, http.StatusBadGateway, "UPSTREAM_UNAVAILABLE", "下游服务不可用")
		return
	}
	common.WriteJSON(w, 200, out)
}

// SeedAssetAllowed: USDT or one of MM_SEED_BASE_ASSETS (pure; unit tested).
func SeedAssetAllowed(asset, base string) bool {
	if asset == "USDT" {
		return true
	}
	for _, b := range strings.Split(base, ",") {
		if strings.EqualFold(strings.TrimSpace(b), asset) {
			return true
		}
	}
	return false
}
