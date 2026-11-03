package main

import (
	"context"
	"database/sql"
	"encoding/json"
	"fmt"
	"log"
	"net/http"
	"regexp"
	"sort"
	"strings"
	"sync"
	"time"

	"github.com/linbit/platform/services/go/common"
	"github.com/shopspring/decimal"
)

// tradfi.go: tokenized stocks (xStocks / Ondo) and metals (PAXG, XAUT spot;
// XAG, WTI perps). See docs/tradfi.md.
//
//	public   GET /tradfi/assets                   flag-filtered catalogue + reference/deviation
//	internal GET /internal/tradfi/assets          everything (admin)
//	internal PUT /internal/tradfi/assets/{asset}  {status?, deviationBps?, refBinance?, refOkx?, name?, updatedBy}
//	internal PUT /internal/tradfi/tokens          {asset, issuer, network, contractAddress?, decimals?, depositEnabled?, withdrawEnabled?, updatedBy}
//	internal GET/PUT /internal/tradfi/settings    {restrictedCountries, deviationBps, widenFactor, alertCooldownSec}
//	internal GET /internal/tradfi/gate            gateway snapshot: flags, restricted countries, symbol/asset kinds
//	internal GET /internal/tradfi/deviation       monitor state per spot symbol
//
// The deviation monitor compares the spot last price with the TradFi
// reference; beyond the threshold it sets widened (the market maker reads
// widenFactor from /market/index and multiplies its spread) and sends a
// Telegram alert (cooldown per symbol), plus a recovery alert.

type TradFiToken struct {
	Asset           string `json:"asset"`
	Issuer          string `json:"issuer"`
	Network         string `json:"network"`
	ContractAddress string `json:"contractAddress"`
	Decimals        int    `json:"decimals"`
	DepositEnabled  bool   `json:"depositEnabled"`
	WithdrawEnabled bool   `json:"withdrawEnabled"`
	MinWithdraw     string `json:"minWithdraw"`
	WithdrawFee     string `json:"withdrawFee"`
}

type TradFiAsset struct {
	Asset        string        `json:"asset"`
	Kind         string        `json:"kind"` // stock | metal | commodity
	Name         string        `json:"name"`
	Underlying   string        `json:"underlying"`
	Market       string        `json:"market"` // spot | futures
	Symbol       string        `json:"symbol"`
	RefBinance   string        `json:"refBinance"`
	RefOKX       string        `json:"refOkx"`
	DeviationBps *int          `json:"deviationBps"`
	Status       string        `json:"status"`
	Sort         int           `json:"sort"`
	Tokens       []TradFiToken `json:"tokens"`
}

type TradFiSettings struct {
	RestrictedCountries []string `json:"restrictedCountries"`
	DeviationBps        int      `json:"deviationBps"`
	WidenFactor         string   `json:"widenFactor"`
	AlertCooldownSec    int      `json:"alertCooldownSec"`
}

// flagFor maps an asset kind to its feature flag.
func flagFor(kind string) string {
	if kind == "stock" {
		return "tokenizedStocks"
	}
	return "metals"
}

type devState struct {
	Symbol       string    `json:"symbol"`
	LastPrice    string    `json:"lastPrice,omitempty"`
	Reference    string    `json:"referencePrice,omitempty"`
	Source       string    `json:"referenceSource,omitempty"`
	DeviationBps string    `json:"deviationBps"`
	ThresholdBps int       `json:"thresholdBps"`
	Widened      bool      `json:"widened"`
	WidenFactor  string    `json:"widenFactor"`
	CheckedAt    time.Time `json:"checkedAt"`
	lastAlert    time.Time
}

// TradFi holds the cached catalogue and the deviation monitor state.
type TradFi struct {
	db   *sql.DB
	feed *IndexFeed
	last func(string) (decimal.Decimal, bool)

	mu       sync.Mutex
	assets   []TradFiAsset
	settings TradFiSettings
	state    map[string]*devState
	alert    func(string)
}

func NewTradFi(db *sql.DB, feed *IndexFeed, last func(string) (decimal.Decimal, bool)) *TradFi {
	return &TradFi{db: db, feed: feed, last: last, state: map[string]*devState{}, alert: common.AlertTelegram,
		settings: TradFiSettings{RestrictedCountries: []string{}, DeviationBps: 200, WidenFactor: "3", AlertCooldownSec: 900}}
}

func (t *TradFi) load(ctx context.Context) error {
	rows, err := t.db.QueryContext(ctx, `SELECT asset, kind, name, underlying, market, symbol, ref_binance, ref_okx, deviation_bps, status, sort
		FROM tradfi_assets ORDER BY sort, asset`)
	if err != nil {
		return err
	}
	defer rows.Close()
	var assets []TradFiAsset
	idx := map[string]int{}
	for rows.Next() {
		var a TradFiAsset
		var dev sql.NullInt64
		if err := rows.Scan(&a.Asset, &a.Kind, &a.Name, &a.Underlying, &a.Market, &a.Symbol, &a.RefBinance, &a.RefOKX, &dev, &a.Status, &a.Sort); err != nil {
			return err
		}
		if dev.Valid {
			v := int(dev.Int64)
			a.DeviationBps = &v
		}
		a.Tokens = []TradFiToken{}
		idx[a.Asset] = len(assets)
		assets = append(assets, a)
	}
	if err := rows.Err(); err != nil {
		return err
	}
	trows, err := t.db.QueryContext(ctx, `SELECT asset, issuer, network, contract_address, decimals, deposit_enabled, withdraw_enabled,
		       min_withdraw::text, withdraw_fee::text
		FROM tradfi_tokens ORDER BY asset, issuer, network`)
	if err != nil {
		return err
	}
	defer trows.Close()
	for trows.Next() {
		var k TradFiToken
		if err := trows.Scan(&k.Asset, &k.Issuer, &k.Network, &k.ContractAddress, &k.Decimals, &k.DepositEnabled, &k.WithdrawEnabled, &k.MinWithdraw, &k.WithdrawFee); err != nil {
			return err
		}
		k.MinWithdraw = decimal.RequireFromString(k.MinWithdraw).String()
		k.WithdrawFee = decimal.RequireFromString(k.WithdrawFee).String()
		if i, ok := idx[k.Asset]; ok {
			assets[i].Tokens = append(assets[i].Tokens, k)
		}
	}
	if err := trows.Err(); err != nil {
		return err
	}
	st, err := loadTradFiSettings(ctx, t.db)
	if err != nil {
		return err
	}
	refs := map[string]tradfiRef{}
	for _, a := range assets {
		refs[a.Symbol] = tradfiRef{Binance: a.RefBinance, OKX: a.RefOKX}
	}
	t.feed.SetTradFi(refs)
	t.mu.Lock()
	t.assets, t.settings = assets, st
	t.mu.Unlock()
	return nil
}

func loadTradFiSettings(ctx context.Context, q interface {
	QueryRowContext(context.Context, string, ...any) *sql.Row
}) (TradFiSettings, error) {
	var st TradFiSettings
	var wf, cc string
	err := q.QueryRowContext(ctx, `SELECT array_to_string(restricted_countries, ','), deviation_bps, widen_factor::text, alert_cooldown_sec FROM tradfi_settings WHERE id = 1`).
		Scan(&cc, &st.DeviationBps, &wf, &st.AlertCooldownSec)
	st.WidenFactor = decimal.RequireFromString(firstNonEmptyStr(wf, "1")).String()
	st.RestrictedCountries = []string{}
	for _, c := range strings.Split(cc, ",") {
		if c != "" {
			st.RestrictedCountries = append(st.RestrictedCountries, c)
		}
	}
	return st, err
}

func firstNonEmptyStr(a, b string) string {
	if a != "" {
		return a
	}
	return b
}

func (t *TradFi) snapshot() ([]TradFiAsset, TradFiSettings) {
	t.mu.Lock()
	defer t.mu.Unlock()
	return append([]TradFiAsset(nil), t.assets...), t.settings
}

// Start reloads the catalogue every 15s and runs the deviation monitor every 5s.
func (t *TradFi) Start(ctx context.Context) {
	if err := t.load(ctx); err != nil {
		log.Printf("tradfi load: %v", err)
	}
	go func() {
		reload := time.NewTicker(15 * time.Second)
		check := time.NewTicker(5 * time.Second)
		defer reload.Stop()
		defer check.Stop()
		for {
			select {
			case <-ctx.Done():
				return
			case <-reload.C:
				if err := t.load(ctx); err != nil {
					log.Printf("tradfi load: %v", err)
				}
			case <-check.C:
				t.Check()
			}
		}
	}()
}

// Check runs one deviation pass over the spot TradFi symbols.
func (t *TradFi) Check() {
	assets, st := t.snapshot()
	widen := decimal.RequireFromString(st.WidenFactor)
	cooldown := time.Duration(st.AlertCooldownSec) * time.Second
	var msgs []string
	for _, a := range assets {
		if a.Market != "spot" {
			continue
		}
		threshold := st.DeviationBps
		if a.DeviationBps != nil {
			threshold = *a.DeviationBps
		}
		now := time.Now()
		t.mu.Lock()
		s := t.state[a.Symbol]
		if s == nil {
			s = &devState{Symbol: a.Symbol, DeviationBps: "0", WidenFactor: "1"}
			t.state[a.Symbol] = s
		}
		t.mu.Unlock()
		ref, okRef := t.feed.Index(a.Symbol)
		last, okLast := t.last(a.Symbol)
		t.mu.Lock()
		s.CheckedAt, s.ThresholdBps = now.UTC(), threshold
		if !okRef || !okLast || !last.IsPositive() || ref.Source == "local" {
			s.Reference, s.Source, s.LastPrice = "", "", ""
			if okLast {
				s.LastPrice = last.String()
			}
			s.DeviationBps = "0"
			t.mu.Unlock()
			continue
		}
		rp := decimal.RequireFromString(ref.Price)
		dev := last.Sub(rp).Abs().Div(rp).Mul(decimal.NewFromInt(10000)).Round(1)
		s.LastPrice, s.Reference, s.Source, s.DeviationBps = last.String(), rp.String(), ref.Source, dev.String()
		over := dev.GreaterThan(decimal.NewFromInt(int64(threshold)))
		switch {
		case over && !s.Widened:
			s.Widened, s.WidenFactor = true, widen.String()
			if now.Sub(s.lastAlert) >= cooldown {
				s.lastAlert = now
				msgs = append(msgs, fmt.Sprintf("⚠️ Linbit TradFi 价格偏离：%s 现货 %s，参考价 %s（%s），偏离 %s bps > %d bps，做市点差已放大 %sx", a.Symbol, last, rp, ref.Source, dev, threshold, widen))
			}
		case over:
			s.WidenFactor = widen.String()
		case !over && s.Widened:
			s.Widened, s.WidenFactor = false, "1"
			msgs = append(msgs, fmt.Sprintf("✅ Linbit TradFi 价格偏离恢复：%s 现货 %s，参考价 %s，偏离 %s bps，点差恢复正常", a.Symbol, last, rp, dev))
		}
		t.mu.Unlock()
	}
	for _, m := range msgs {
		log.Print(m)
		if t.alert != nil {
			t.alert(m)
		}
	}
}

// deviation returns the monitor state for a TradFi symbol (false if not tradfi).
func (t *TradFi) deviation(sym string) (devState, bool) {
	t.mu.Lock()
	defer t.mu.Unlock()
	found := false
	for _, a := range t.assets {
		if a.Symbol == sym {
			found = true
			break
		}
	}
	if !found {
		return devState{}, false
	}
	if s := t.state[sym]; s != nil {
		return *s, true
	}
	return devState{Symbol: sym, DeviationBps: "0", WidenFactor: "1"}, true
}

// ---- HTTP ----

func (s *Server) tradfiRoutes(mux *http.ServeMux) {
	mux.HandleFunc("GET /tradfi/assets", s.hTradFiPublic)
	mux.HandleFunc("GET /internal/tradfi/assets", s.internal(s.hTradFiAssets))
	mux.HandleFunc("PUT /internal/tradfi/assets/{asset}", s.internal(s.hTradFiAssetPut))
	mux.HandleFunc("PUT /internal/tradfi/tokens", s.internal(s.hTradFiTokenPut))
	mux.HandleFunc("GET /internal/tradfi/settings", s.internal(s.hTradFiSettings))
	mux.HandleFunc("PUT /internal/tradfi/settings", s.internal(s.hTradFiSettingsPut))
	mux.HandleFunc("GET /internal/tradfi/gate", s.internal(s.hTradFiGate))
	mux.HandleFunc("GET /internal/tradfi/deviation", s.internal(s.hTradFiDeviation))
}

func (s *Server) currentFlags(ctx context.Context) map[string]bool {
	over, _, err := s.store.featureOverrides(ctx)
	if err != nil {
		return mergeFeatures(nil)
	}
	return mergeFeatures(over)
}

type publicAsset struct {
	TradFiAsset
	ReferencePrice  string `json:"referencePrice,omitempty"`
	ReferenceSource string `json:"referenceSource,omitempty"`
	LastPrice       string `json:"lastPrice,omitempty"`
	CurDeviationBps string `json:"currentDeviationBps,omitempty"`
	Widened         bool   `json:"widened"`
}

// GET /tradfi/assets?kind=stock|metal|commodity
func (s *Server) hTradFiPublic(w http.ResponseWriter, r *http.Request) {
	flags := s.currentFlags(r.Context())
	if !flags["tokenizedStocks"] && !flags["metals"] {
		common.WriteError(w, http.StatusForbidden, "FEATURE_DISABLED", "该功能暂未开放")
		return
	}
	kind := r.URL.Query().Get("kind")
	assets, _ := s.tradfi.snapshot()
	out := []publicAsset{}
	for _, a := range assets {
		if !flags[flagFor(a.Kind)] || a.Status != "active" || (kind != "" && a.Kind != kind) {
			continue
		}
		p := publicAsset{TradFiAsset: a}
		p.RefBinance, p.RefOKX, p.DeviationBps = "", "", nil
		toks := []TradFiToken{}
		for _, k := range a.Tokens {
			if k.ContractAddress != "" {
				toks = append(toks, k)
			}
		}
		p.Tokens = toks
		if ref, ok := s.feed.IndexNoLocal(a.Symbol); ok {
			p.ReferencePrice, p.ReferenceSource = ref.Price, ref.Source
		}
		if d, ok := s.tradfi.deviation(a.Symbol); ok && a.Market == "spot" {
			p.LastPrice, p.CurDeviationBps, p.Widened = d.LastPrice, d.DeviationBps, d.Widened
		}
		out = append(out, p)
	}
	common.WriteJSON(w, http.StatusOK, map[string]any{"items": out})
}

func (s *Server) hTradFiAssets(w http.ResponseWriter, r *http.Request) {
	if err := s.tradfi.load(r.Context()); err != nil {
		intErr(w, "tradfi load", err)
		return
	}
	assets, st := s.tradfi.snapshot()
	common.WriteJSON(w, http.StatusOK, map[string]any{"items": assets, "settings": st})
}

func (t *TradFi) find(asset string) (TradFiAsset, bool) {
	assets, _ := t.snapshot()
	for _, a := range assets {
		if a.Asset == asset {
			return a, true
		}
	}
	return TradFiAsset{}, false
}

var refRe = regexp.MustCompile(`^[A-Z0-9-]{0,30}$`)

func (s *Server) hTradFiAssetPut(w http.ResponseWriter, r *http.Request) {
	asset := strings.ToUpper(r.PathValue("asset"))
	var in struct {
		Status       *string `json:"status"`
		DeviationBps *int    `json:"deviationBps"`
		ClearDev     bool    `json:"clearDeviationBps"`
		RefBinance   *string `json:"refBinance"`
		RefOKX       *string `json:"refOkx"`
		Name         *string `json:"name"`
		UpdatedBy    string  `json:"updatedBy"`
	}
	if err := json.NewDecoder(r.Body).Decode(&in); err != nil {
		common.WriteError(w, http.StatusBadRequest, "VALIDATION_ERROR", "请求格式错误")
		return
	}
	_ = s.tradfi.load(r.Context())
	before, ok := s.tradfi.find(asset)
	if !ok {
		common.WriteError(w, http.StatusNotFound, "NOT_FOUND", "资产不存在")
		return
	}
	if in.Status != nil && *in.Status != "active" && *in.Status != "paused" {
		common.WriteError(w, http.StatusBadRequest, "VALIDATION_ERROR", "status 必须为 active|paused")
		return
	}
	if in.DeviationBps != nil && (*in.DeviationBps < 10 || *in.DeviationBps > 5000) {
		common.WriteError(w, http.StatusBadRequest, "VALIDATION_ERROR", "deviationBps 范围 10-5000")
		return
	}
	for _, p := range []*string{in.RefBinance, in.RefOKX} {
		if p != nil && !refRe.MatchString(strings.ToUpper(*p)) {
			common.WriteError(w, http.StatusBadRequest, "VALIDATION_ERROR", "参考合约代码不合法")
			return
		}
	}
	_, err := s.store.db.ExecContext(r.Context(), `UPDATE tradfi_assets SET
		status = COALESCE($2, status),
		deviation_bps = CASE WHEN $3 THEN NULL WHEN $4::int IS NOT NULL THEN $4 ELSE deviation_bps END,
		ref_binance = COALESCE(UPPER($5), ref_binance), ref_okx = COALESCE(UPPER($6), ref_okx),
		name = COALESCE($7, name), updated_by = $8, updated_at = now() WHERE asset = $1`,
		asset, in.Status, in.ClearDev, in.DeviationBps, in.RefBinance, in.RefOKX, in.Name, in.UpdatedBy)
	if err != nil {
		intErr(w, "tradfi asset put", err)
		return
	}
	if err := s.tradfi.load(r.Context()); err != nil {
		intErr(w, "tradfi load", err)
		return
	}
	after, _ := s.tradfi.find(asset)
	common.WriteJSON(w, http.StatusOK, map[string]any{"before": before, "after": after})
}

var (
	addrRe    = regexp.MustCompile(`^[A-Za-z0-9]{20,100}$`)
	networkRe = regexp.MustCompile(`^[A-Z0-9_]{2,20}$`)
)

func (s *Server) hTradFiTokenPut(w http.ResponseWriter, r *http.Request) {
	var in struct {
		Asset           string  `json:"asset"`
		Issuer          string  `json:"issuer"`
		Network         string  `json:"network"`
		ContractAddress *string `json:"contractAddress"`
		Decimals        *int    `json:"decimals"`
		DepositEnabled  *bool   `json:"depositEnabled"`
		WithdrawEnabled *bool   `json:"withdrawEnabled"`
		MinWithdraw     *string `json:"minWithdraw"`
		WithdrawFee     *string `json:"withdrawFee"`
		UpdatedBy       string  `json:"updatedBy"`
	}
	if err := json.NewDecoder(r.Body).Decode(&in); err != nil {
		common.WriteError(w, http.StatusBadRequest, "VALIDATION_ERROR", "请求格式错误")
		return
	}
	in.Asset, in.Issuer, in.Network = strings.ToUpper(in.Asset), strings.ToLower(in.Issuer), strings.ToUpper(in.Network)
	switch in.Issuer {
	case "xstocks", "ondo", "paxos", "tether":
	default:
		common.WriteError(w, http.StatusBadRequest, "VALIDATION_ERROR", "issuer 必须为 xstocks|ondo|paxos|tether")
		return
	}
	if !networkRe.MatchString(in.Network) {
		common.WriteError(w, http.StatusBadRequest, "VALIDATION_ERROR", "network 不合法")
		return
	}
	if in.ContractAddress != nil {
		*in.ContractAddress = strings.TrimSpace(*in.ContractAddress)
		if *in.ContractAddress != "" && !addrRe.MatchString(strings.TrimPrefix(*in.ContractAddress, "0x")) {
			common.WriteError(w, http.StatusBadRequest, "VALIDATION_ERROR", "合约地址格式不合法")
			return
		}
	}
	if in.Decimals != nil && (*in.Decimals < 0 || *in.Decimals > 36) {
		common.WriteError(w, http.StatusBadRequest, "VALIDATION_ERROR", "decimals 不合法")
		return
	}
	_ = s.tradfi.load(r.Context())
	a, ok := s.tradfi.find(in.Asset)
	if !ok {
		common.WriteError(w, http.StatusNotFound, "NOT_FOUND", "资产不存在")
		return
	}
	var before any
	cur := TradFiToken{Asset: in.Asset, Issuer: in.Issuer, Network: in.Network, Decimals: 18, MinWithdraw: "0.05", WithdrawFee: "0.02"}
	for _, k := range a.Tokens {
		if k.Issuer == in.Issuer && k.Network == in.Network {
			before, cur = k, k
		}
	}
	if in.ContractAddress != nil {
		cur.ContractAddress = *in.ContractAddress
	}
	if in.Decimals != nil {
		cur.Decimals = *in.Decimals
	}
	if in.DepositEnabled != nil {
		cur.DepositEnabled = *in.DepositEnabled
	}
	if in.WithdrawEnabled != nil {
		cur.WithdrawEnabled = *in.WithdrawEnabled
	}
	for _, f := range []struct {
		in  *string
		out *string
	}{{in.MinWithdraw, &cur.MinWithdraw}, {in.WithdrawFee, &cur.WithdrawFee}} {
		if f.in == nil {
			continue
		}
		d, err := decimal.NewFromString(*f.in)
		if err != nil || d.IsNegative() {
			common.WriteError(w, http.StatusBadRequest, "VALIDATION_ERROR", "minWithdraw/withdrawFee 不合法")
			return
		}
		*f.out = d.String()
	}
	if cur.ContractAddress == "" && (cur.DepositEnabled || cur.WithdrawEnabled) {
		common.WriteError(w, http.StatusBadRequest, "ADDRESS_REQUIRED", "请先配置合约地址再开启充提")
		return
	}
	_, err := s.store.db.ExecContext(r.Context(), `INSERT INTO tradfi_tokens (asset, issuer, network, contract_address, decimals, deposit_enabled, withdraw_enabled, min_withdraw, withdraw_fee, updated_by)
		VALUES ($1,$2,$3,$4,$5,$6,$7,$8,$9,$10)
		ON CONFLICT (asset, issuer, network) DO UPDATE SET contract_address=$4, decimals=$5, deposit_enabled=$6, withdraw_enabled=$7,
		  min_withdraw=$8, withdraw_fee=$9, updated_by=$10, updated_at=now()`,
		cur.Asset, cur.Issuer, cur.Network, cur.ContractAddress, cur.Decimals, cur.DepositEnabled, cur.WithdrawEnabled, cur.MinWithdraw, cur.WithdrawFee, in.UpdatedBy)
	if err != nil {
		intErr(w, "tradfi token put", err)
		return
	}
	_ = s.tradfi.load(r.Context())
	common.WriteJSON(w, http.StatusOK, map[string]any{"before": before, "after": cur})
}

func (s *Server) hTradFiSettings(w http.ResponseWriter, r *http.Request) {
	st, err := loadTradFiSettings(r.Context(), s.store.db)
	if err != nil {
		intErr(w, "tradfi settings", err)
		return
	}
	common.WriteJSON(w, http.StatusOK, st)
}

var ccRe = regexp.MustCompile(`^[A-Z0-9]{2}$`)

func (s *Server) hTradFiSettingsPut(w http.ResponseWriter, r *http.Request) {
	var in struct {
		RestrictedCountries []string `json:"restrictedCountries"`
		DeviationBps        *int     `json:"deviationBps"`
		WidenFactor         *string  `json:"widenFactor"`
		AlertCooldownSec    *int     `json:"alertCooldownSec"`
		UpdatedBy           string   `json:"updatedBy"`
	}
	if err := json.NewDecoder(r.Body).Decode(&in); err != nil {
		common.WriteError(w, http.StatusBadRequest, "VALIDATION_ERROR", "请求格式错误")
		return
	}
	before, err := loadTradFiSettings(r.Context(), s.store.db)
	if err != nil {
		intErr(w, "tradfi settings", err)
		return
	}
	after := before
	if in.RestrictedCountries != nil {
		seen := map[string]bool{}
		cc := []string{}
		for _, c := range in.RestrictedCountries {
			c = strings.ToUpper(strings.TrimSpace(c))
			if !ccRe.MatchString(c) {
				common.WriteError(w, http.StatusBadRequest, "VALIDATION_ERROR", "国家代码必须为 ISO-3166 两位字母: "+c)
				return
			}
			if !seen[c] {
				seen[c] = true
				cc = append(cc, c)
			}
		}
		sort.Strings(cc)
		after.RestrictedCountries = cc
	}
	if in.DeviationBps != nil {
		if *in.DeviationBps < 10 || *in.DeviationBps > 5000 {
			common.WriteError(w, http.StatusBadRequest, "VALIDATION_ERROR", "deviationBps 范围 10-5000")
			return
		}
		after.DeviationBps = *in.DeviationBps
	}
	if in.WidenFactor != nil {
		f, err := decimal.NewFromString(*in.WidenFactor)
		if err != nil || f.LessThan(decimal.NewFromInt(1)) || f.GreaterThan(decimal.NewFromInt(20)) {
			common.WriteError(w, http.StatusBadRequest, "VALIDATION_ERROR", "widenFactor 范围 1-20")
			return
		}
		after.WidenFactor = f.String()
	}
	if in.AlertCooldownSec != nil {
		if *in.AlertCooldownSec < 0 || *in.AlertCooldownSec > 86400 {
			common.WriteError(w, http.StatusBadRequest, "VALIDATION_ERROR", "alertCooldownSec 范围 0-86400")
			return
		}
		after.AlertCooldownSec = *in.AlertCooldownSec
	}
	if _, err := s.store.db.ExecContext(r.Context(), `UPDATE tradfi_settings SET restricted_countries=string_to_array(NULLIF($1, ''), ','), deviation_bps=$2, widen_factor=$3,
		alert_cooldown_sec=$4, updated_by=$5, updated_at=now() WHERE id=1`,
		strings.Join(after.RestrictedCountries, ","), after.DeviationBps, after.WidenFactor, after.AlertCooldownSec, in.UpdatedBy); err != nil {
		intErr(w, "tradfi settings put", err)
		return
	}
	_ = s.tradfi.load(r.Context())
	common.WriteJSON(w, http.StatusOK, map[string]any{"before": before, "after": after})
}

// GET /internal/tradfi/gate — what the gateway needs to gate TradFi routes.
func (s *Server) hTradFiGate(w http.ResponseWriter, r *http.Request) {
	if err := s.tradfi.load(r.Context()); err != nil {
		intErr(w, "tradfi gate", err) // gateway fails closed
		return
	}
	assets, st := s.tradfi.snapshot()
	over, _, err := s.store.featureOverrides(r.Context())
	if err != nil {
		intErr(w, "tradfi gate flags", err)
		return
	}
	flags := mergeFeatures(over)
	spot, fut, byAsset := map[string]string{}, map[string]string{}, map[string]string{}
	for _, a := range assets {
		f := flagFor(a.Kind)
		if a.Market == "spot" {
			spot[a.Symbol] = f
			byAsset[a.Asset] = f
		} else {
			fut[a.Symbol] = f
		}
	}
	common.WriteJSON(w, http.StatusOK, map[string]any{
		"features":            map[string]bool{"tokenizedStocks": flags["tokenizedStocks"], "metals": flags["metals"]},
		"restrictedCountries": st.RestrictedCountries,
		"spotSymbols":         spot, "futuresSymbols": fut, "assets": byAsset,
	})
}

func (s *Server) hTradFiDeviation(w http.ResponseWriter, r *http.Request) {
	if r.URL.Query().Get("check") == "1" {
		s.tradfi.Check()
	}
	s.tradfi.mu.Lock()
	out := []devState{}
	for _, v := range s.tradfi.state {
		out = append(out, *v)
	}
	s.tradfi.mu.Unlock()
	sort.Slice(out, func(i, j int) bool { return out[i].Symbol < out[j].Symbol })
	common.WriteJSON(w, http.StatusOK, map[string]any{"items": out})
}
