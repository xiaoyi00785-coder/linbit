package main

// convert.go: 闪兑 (convert / flash swap). The user requests a quote (RFQ)
// priced off the external index (market-data /market/index, Binance → OKX)
// minus a configurable spread; accepting it within the TTL moves balances
// atomically in the user's spot (funding) account against the platform
// counterparty account system:convert. There is no explicit fee — the
// spread is the platform's revenue. system:convert is a ledger-only account
// (not user funds); its net per-asset position is the platform's unhedged
// convert exposure and is reported to the solvency check.

import (
	"context"
	"crypto/rand"
	"database/sql"
	"encoding/hex"
	"encoding/json"
	"errors"
	"fmt"
	"log"
	"net/http"
	"net/url"
	"sort"
	"strconv"
	"strings"
	"time"

	"github.com/linbit/platform/services/go/common"
	"github.com/shopspring/decimal"
)

// ConvertAccount is the platform counterparty for every convert.
const ConvertAccount = "system:convert"

// ConvertDefaults seed convert_config on first start (admin can change later).
type ConvertDefaults struct {
	SpreadBps   int             // CONVERT_SPREAD_BPS (20 = 0.2%)
	MinUSDT     decimal.Decimal // CONVERT_MIN_USDT (10)
	MaxUSDT     decimal.Decimal // CONVERT_MAX_USDT (50000)
	QuoteTTL    time.Duration   // CONVERT_QUOTE_TTL_SECONDS (10)
	PriceMaxAge time.Duration   // CONVERT_PRICE_MAX_AGE_SECONDS (15): older index = stale

	DepegCheck      time.Duration // CONVERT_DEPEG_CHECK_SECONDS (5): monitor interval
	HedgeEnabled    bool          // CONVERT_HEDGE_ENABLED (default true only when WALLET_CHAIN=testnet); admin switch overrides
	HedgeUserID     string        // CONVERT_HEDGE_USER_ID (system:hedge)
	HedgeInterval   time.Duration // CONVERT_HEDGE_INTERVAL_SECONDS (5)
	HedgeBackoff    time.Duration // CONVERT_HEDGE_BACKOFF_SECONDS (5): first retry delay, doubles
	HedgeBackoffMax time.Duration // CONVERT_HEDGE_BACKOFF_MAX_SECONDS (300)
}

func envBool(k string, d bool) bool {
	if v, err := strconv.ParseBool(common.Env(k, "")); err == nil {
		return v
	}
	return d
}

func loadConvertDefaults() ConvertDefaults {
	atoi := func(k string, d int) int {
		if n, err := strconv.Atoi(common.Env(k, "")); err == nil {
			return n
		}
		return d
	}
	dec := func(k, d string) decimal.Decimal {
		if v, err := decimal.NewFromString(common.Env(k, d)); err == nil {
			return v
		}
		return decimal.RequireFromString(d)
	}
	return ConvertDefaults{
		SpreadBps:   atoi("CONVERT_SPREAD_BPS", 20),
		MinUSDT:     dec("CONVERT_MIN_USDT", "10"),
		MaxUSDT:     dec("CONVERT_MAX_USDT", "50000"),
		QuoteTTL:    time.Duration(atoi("CONVERT_QUOTE_TTL_SECONDS", 10)) * time.Second,
		PriceMaxAge: time.Duration(atoi("CONVERT_PRICE_MAX_AGE_SECONDS", 15)) * time.Second,

		DepegCheck:      time.Duration(atoi("CONVERT_DEPEG_CHECK_SECONDS", 5)) * time.Second,
		HedgeEnabled:    envBool("CONVERT_HEDGE_ENABLED", common.Env("WALLET_CHAIN", "") == "testnet"),
		HedgeUserID:     common.Env("CONVERT_HEDGE_USER_ID", "system:hedge"),
		HedgeInterval:   time.Duration(atoi("CONVERT_HEDGE_INTERVAL_SECONDS", 5)) * time.Second,
		HedgeBackoff:    time.Duration(atoi("CONVERT_HEDGE_BACKOFF_SECONDS", 5)) * time.Second,
		HedgeBackoffMax: time.Duration(atoi("CONVERT_HEDGE_BACKOFF_MAX_SECONDS", 300)) * time.Second,
	}
}

// stablecoins are valued at exactly 1 USDT; a stable↔stable convert is 1:1
// with no spread.
var stablecoins = map[string]bool{"USDT": true, "USDC": true}

var (
	ErrQuoteNotFound = errors.New("quote not found")
	ErrQuoteExpired  = errors.New("quote expired")
)

type convertErr struct {
	status    int
	code, msg string
}

func (e *convertErr) Error() string { return e.code }

func cerr(status int, code, msg string) error { return &convertErr{status, code, msg} }

type ConvertConfig struct {
	SpreadBps int                `json:"spreadBps"`
	MinUSDT   string             `json:"minUsdt"`
	MaxUSDT   string             `json:"maxUsdt"`
	UpdatedBy string             `json:"updatedBy"`
	UpdatedAt int64              `json:"updatedAt"`
	DepegBps  int                `json:"depegBps"` // USDC/USDT pause threshold (50 = 0.5%)
	Hedge     HedgeConfig        `json:"hedge"`
	Assets    []ConvertAssetConf `json:"assets"`
}

// HedgeConfig: automatic spot hedging of the system:convert position.
type HedgeConfig struct {
	Enabled              bool   `json:"enabled"`       // effective global switch
	EnabledSource        string `json:"enabledSource"` // admin | env_default
	SlippageBps          int    `json:"slippageBps"`   // IOC limit = index ± slippage
	MaxOrderUSDT         string `json:"maxOrderUsdt"`
	MaxHourUSDT          string `json:"maxHourUsdt"`
	UnhedgedAlertUSDT    string `json:"unhedgedAlertUsdt"`
	UnhedgedAlertMinutes int    `json:"unhedgedAlertMinutes"`
	CancelAfterSeconds   int    `json:"cancelAfterSeconds"` // cancel hedge orders still new/partially_filled after this
	Account              string `json:"account"`            // hedge account user id
}

type ConvertAssetConf struct {
	Asset        string `json:"asset"`
	Precision    int    `json:"precision"`
	Enabled      bool   `json:"enabled"`
	HedgeEnabled bool   `json:"hedgeEnabled"`
	HedgeMinQty  string `json:"hedgeMinQty"`
}

func (s *Store) convertConfig(ctx context.Context, def ConvertDefaults) (ConvertConfig, error) {
	if _, err := s.db.ExecContext(ctx, `INSERT INTO convert_config (id, spread_bps, min_usdt, max_usdt) VALUES (1,$1,$2,$3) ON CONFLICT DO NOTHING`,
		def.SpreadBps, def.MinUSDT.String(), def.MaxUSDT.String()); err != nil {
		return ConvertConfig{}, err
	}
	var c ConvertConfig
	var mn, mx string
	var at time.Time
	var hen sql.NullBool
	var ho, hh, ha string
	h := &c.Hedge
	if err := s.db.QueryRowContext(ctx, `SELECT spread_bps, min_usdt::text, max_usdt::text, updated_by, updated_at, depeg_bps, hedge_enabled,
		hedge_slippage_bps, hedge_max_order_usdt::text, hedge_max_hour_usdt::text, hedge_unhedged_alert_usdt::text, hedge_unhedged_alert_minutes, hedge_cancel_after_seconds
		FROM convert_config WHERE id=1`).
		Scan(&c.SpreadBps, &mn, &mx, &c.UpdatedBy, &at, &c.DepegBps, &hen, &h.SlippageBps, &ho, &hh, &ha, &h.UnhedgedAlertMinutes, &h.CancelAfterSeconds); err != nil {
		return c, err
	}
	c.MinUSDT, c.MaxUSDT, c.UpdatedAt = decimal.RequireFromString(mn).String(), decimal.RequireFromString(mx).String(), at.UnixMilli()
	h.MaxOrderUSDT, h.MaxHourUSDT, h.UnhedgedAlertUSDT = decimal.RequireFromString(ho).String(), decimal.RequireFromString(hh).String(), decimal.RequireFromString(ha).String()
	h.Enabled, h.EnabledSource, h.Account = def.HedgeEnabled, "env_default", def.HedgeUserID
	if hen.Valid {
		h.Enabled, h.EnabledSource = hen.Bool, "admin"
	}
	rows, err := s.db.QueryContext(ctx, `SELECT asset, precision, enabled, hedge_enabled, hedge_min_qty::text FROM convert_assets ORDER BY sort, asset`)
	if err != nil {
		return c, err
	}
	defer rows.Close()
	c.Assets = []ConvertAssetConf{}
	for rows.Next() {
		var a ConvertAssetConf
		if err := rows.Scan(&a.Asset, &a.Precision, &a.Enabled, &a.HedgeEnabled, &a.HedgeMinQty); err != nil {
			return c, err
		}
		a.HedgeMinQty = decimal.RequireFromString(a.HedgeMinQty).String()
		c.Assets = append(c.Assets, a)
	}
	return c, rows.Err()
}

func (c ConvertConfig) asset(a string) (ConvertAssetConf, bool) {
	for _, x := range c.Assets {
		if x.Asset == a {
			return x, true
		}
	}
	return ConvertAssetConf{}, false
}

// ---- pricing ----

// PriceSource returns the USDT price of an asset and when it was observed.
type PriceSource func(ctx context.Context, asset string) (decimal.Decimal, time.Time, error)

var priceHTTP = &http.Client{Timeout: 3 * time.Second}

func marketIndexSource(base string) PriceSource {
	return func(ctx context.Context, asset string) (decimal.Decimal, time.Time, error) {
		u := strings.TrimRight(base, "/") + "/market/index?symbol=" + url.QueryEscape(asset+"USDT")
		req, err := http.NewRequestWithContext(ctx, http.MethodGet, u, nil)
		if err != nil {
			return decimal.Zero, time.Time{}, err
		}
		resp, err := priceHTTP.Do(req)
		if err != nil {
			return decimal.Zero, time.Time{}, err
		}
		defer resp.Body.Close()
		if resp.StatusCode != http.StatusOK {
			return decimal.Zero, time.Time{}, fmt.Errorf("index %s: status %d", asset, resp.StatusCode)
		}
		var t struct {
			IndexPrice string    `json:"indexPrice"`
			UpdatedAt  time.Time `json:"updatedAt"`
		}
		if err := json.NewDecoder(resp.Body).Decode(&t); err != nil {
			return decimal.Zero, time.Time{}, err
		}
		p, err := decimal.NewFromString(t.IndexPrice)
		if err != nil || p.Sign() <= 0 {
			return decimal.Zero, time.Time{}, fmt.Errorf("index %s: bad price", asset)
		}
		return p, t.UpdatedAt, nil
	}
}

// usdtPrice: USDT is 1; everything else (USDC included) needs a fresh index or
// fails closed with PRICE_UNAVAILABLE. USDC is priced at its real USDCUSDT
// index so a depeg cannot be arbitraged through USDC<->crypto pairs.
func (s *Server) usdtPrice(ctx context.Context, asset string) (decimal.Decimal, error) {
	if asset == "USDT" {
		return decimal.NewFromInt(1), nil
	}
	p, at, err := s.prices(ctx, asset)
	if err != nil || p.Sign() <= 0 {
		return decimal.Zero, cerr(http.StatusServiceUnavailable, "PRICE_UNAVAILABLE", "价格暂不可用，请稍后再试")
	}
	if !at.IsZero() && s.now().Sub(at) > s.cfg.Convert.PriceMaxAge {
		return decimal.Zero, cerr(http.StatusServiceUnavailable, "PRICE_UNAVAILABLE", "价格已过期，请稍后再试")
	}
	return p, nil
}

// Quote is the priced RFQ result.
type Quote struct {
	QuoteID     string `json:"quoteId"`
	From        string `json:"from"`
	To          string `json:"to"`
	FromAmount  string `json:"fromAmount"`
	ToAmount    string `json:"toAmount"`
	Rate        string `json:"rate"`        // to per 1 from, after spread
	InverseRate string `json:"inverseRate"` // from per 1 to
	IndexRate   string `json:"indexRate"`   // to per 1 from, before spread
	SpreadBps   int    `json:"spreadBps"`
	Fee         string `json:"fee"` // always "0": the spread is the only cost
	FeeAsset    string `json:"feeAsset"`
	ExpiresAt   int64  `json:"expiresAt"` // unix ms
	TTLSeconds  int    `json:"ttlSeconds"`
}

type quoteReq struct {
	From       string `json:"from"`
	To         string `json:"to"`
	FromAmount string `json:"fromAmount"`
	ToAmount   string `json:"toAmount"`
}

// PriceQuote computes amounts (pure apart from price lookups; unit tested).
func PriceQuote(cfg ConvertConfig, from, to ConvertAssetConf, pFrom, pTo decimal.Decimal, fromAmt, toAmt decimal.Decimal) (fa, ta, rate, idx decimal.Decimal, spread int, err error) {
	idx = pFrom.Div(pTo)
	spread = cfg.SpreadBps
	if stablecoins[from.Asset] && stablecoins[to.Asset] {
		spread, idx = 0, decimal.NewFromInt(1)
	}
	rate = idx.Mul(decimal.NewFromInt(int64(10000 - spread))).Div(decimal.NewFromInt(10000))
	if rate.Sign() <= 0 {
		return fa, ta, rate, idx, spread, cerr(http.StatusServiceUnavailable, "PRICE_UNAVAILABLE", "价格暂不可用")
	}
	if fromAmt.Sign() > 0 {
		fa = fromAmt.Truncate(int32(from.Precision))
		ta = fa.Mul(rate).Truncate(int32(to.Precision)) // round in the platform's favour
	} else {
		ta = toAmt.Truncate(int32(to.Precision))
		fa = ta.Div(rate).RoundCeil(int32(from.Precision))
	}
	if fa.Sign() <= 0 || ta.Sign() <= 0 {
		return fa, ta, rate, idx, spread, cerr(http.StatusBadRequest, "AMOUNT_TOO_SMALL", "兑换数量过小")
	}
	v := fa.Mul(pFrom)
	if mn := decimal.RequireFromString(cfg.MinUSDT); v.LessThan(mn) {
		return fa, ta, rate, idx, spread, cerr(http.StatusBadRequest, "AMOUNT_TOO_SMALL", "单笔最少 "+mn.String()+" USDT 等值")
	}
	if mx := decimal.RequireFromString(cfg.MaxUSDT); v.GreaterThan(mx) {
		return fa, ta, rate, idx, spread, cerr(http.StatusBadRequest, "AMOUNT_TOO_LARGE", "单笔最多 "+mx.String()+" USDT 等值")
	}
	return fa, ta, rate.Round(12), idx.Round(12), spread, nil
}

func newQuoteID() string {
	b := make([]byte, 12)
	_, _ = rand.Read(b)
	return "cq_" + hex.EncodeToString(b)
}

func (s *Server) quote(ctx context.Context, userID string, req quoteReq) (*Quote, error) {
	from, to := strings.ToUpper(strings.TrimSpace(req.From)), strings.ToUpper(strings.TrimSpace(req.To))
	cfg, err := s.store.convertConfig(ctx, s.cfg.Convert)
	if err != nil {
		return nil, err
	}
	fc, ok1 := cfg.asset(from)
	tc, ok2 := cfg.asset(to)
	if from == to || !ok1 || !ok2 || !fc.Enabled || !tc.Enabled {
		return nil, cerr(http.StatusBadRequest, "PAIR_NOT_SUPPORTED", "不支持该币对")
	}
	if err := s.pairGuard(ctx, from, to); err != nil {
		return nil, err
	}
	var fa, ta decimal.Decimal
	if req.FromAmount != "" && req.ToAmount != "" {
		return nil, cerr(http.StatusBadRequest, "VALIDATION_ERROR", "fromAmount 和 toAmount 只能填一个")
	}
	if req.FromAmount != "" {
		fa, err = decimal.NewFromString(req.FromAmount)
	} else {
		ta, err = decimal.NewFromString(req.ToAmount)
	}
	if err != nil || (fa.Sign() <= 0 && ta.Sign() <= 0) {
		return nil, cerr(http.StatusBadRequest, "VALIDATION_ERROR", "数量不合法")
	}
	pFrom, err := s.usdtPrice(ctx, from)
	if err != nil {
		return nil, err
	}
	pTo, err := s.usdtPrice(ctx, to)
	if err != nil {
		return nil, err
	}
	fa, ta, rate, idx, spread, err := PriceQuote(cfg, fc, tc, pFrom, pTo, fa, ta)
	if err != nil {
		return nil, err
	}
	id := newQuoteID()
	exp := s.now().Add(s.cfg.Convert.QuoteTTL)
	if _, err := s.store.db.ExecContext(ctx, `INSERT INTO convert_quotes (id,user_id,from_asset,to_asset,from_amount,to_amount,rate,index_rate,spread_bps,expires_at)
		VALUES ($1,$2,$3,$4,$5,$6,$7,$8,$9,$10)`, id, userID, from, to, fa.String(), ta.String(), rate.String(), idx.String(), spread, exp); err != nil {
		return nil, err
	}
	inv := decimal.NewFromInt(1).Div(rate).Round(12)
	return &Quote{QuoteID: id, From: from, To: to, FromAmount: fa.String(), ToAmount: ta.String(), Rate: rate.String(), InverseRate: inv.String(),
		IndexRate: idx.String(), SpreadBps: spread, Fee: "0", FeeAsset: to, ExpiresAt: exp.UnixMilli(), TTLSeconds: int(s.cfg.Convert.QuoteTTL / time.Second)}, nil
}

// ConvertOrder is a filled convert.
type ConvertOrder struct {
	ID         int64  `json:"id"`
	QuoteID    string `json:"quoteId"`
	UserID     string `json:"userId,omitempty"`
	From       string `json:"from"`
	To         string `json:"to"`
	FromAmount string `json:"fromAmount"`
	ToAmount   string `json:"toAmount"`
	Rate       string `json:"rate"`
	IndexRate  string `json:"indexRate"`
	SpreadBps  int    `json:"spreadBps"`
	Fee        string `json:"fee"`
	USDTValue  string `json:"usdtValue"`
	Status     string `json:"status"`
	CreatedAt  int64  `json:"createdAt"`
}

const orderCols = `id, quote_id, user_id, from_asset, to_asset, from_amount::text, to_amount::text, rate::text, index_rate::text, spread_bps, usdt_value::text, created_at`

func scanOrder(sc interface{ Scan(...any) error }) (ConvertOrder, error) {
	var o ConvertOrder
	var at time.Time
	err := sc.Scan(&o.ID, &o.QuoteID, &o.UserID, &o.From, &o.To, &o.FromAmount, &o.ToAmount, &o.Rate, &o.IndexRate, &o.SpreadBps, &o.USDTValue, &at)
	if err != nil {
		return o, err
	}
	for _, p := range []*string{&o.FromAmount, &o.ToAmount, &o.Rate, &o.IndexRate, &o.USDTValue} {
		*p = decimal.RequireFromString(*p).String()
	}
	o.Fee, o.Status, o.CreatedAt = "0", "filled", at.UnixMilli()
	return o, nil
}

// AcceptQuote fills a quote atomically. Idempotent: accepting an already
// filled quote returns the same order (replay=true).
//
// guard (may be nil) is re-checked for the quote's pair before filling, so a
// quote issued just before a depeg pause cannot be accepted after it.
func (s *Store) AcceptQuote(ctx context.Context, userID, quoteID string, now time.Time, usdtValue func(asset string, amt decimal.Decimal) decimal.Decimal, guard func(from, to string) error) (ConvertOrder, bool, error) {
	tx, err := s.db.BeginTx(ctx, nil)
	if err != nil {
		return ConvertOrder{}, false, err
	}
	defer tx.Rollback() //nolint:errcheck
	var from, to, fa, ta, status string
	var exp time.Time
	err = tx.QueryRowContext(ctx, `SELECT from_asset, to_asset, from_amount::text, to_amount::text, status, expires_at FROM convert_quotes
		WHERE id=$1 AND user_id=$2 FOR UPDATE`, quoteID, userID).Scan(&from, &to, &fa, &ta, &status, &exp)
	if errors.Is(err, sql.ErrNoRows) {
		return ConvertOrder{}, false, ErrQuoteNotFound
	}
	if err != nil {
		return ConvertOrder{}, false, err
	}
	if status == "filled" {
		o, err := scanOrder(tx.QueryRowContext(ctx, `SELECT `+orderCols+` FROM convert_orders WHERE quote_id=$1`, quoteID))
		return o, true, err
	}
	if !now.Before(exp) {
		return ConvertOrder{}, false, ErrQuoteExpired
	}
	if guard != nil {
		if err := guard(from, to); err != nil {
			return ConvertOrder{}, false, err
		}
	}
	fromAmt, toAmt := decimal.RequireFromString(fa), decimal.RequireFromString(ta)
	ref := "convert:" + quoteID
	if err := claimRef(ctx, tx, ref, "convert", userID, from, fromAmt); err != nil {
		return ConvertOrder{}, false, err
	}
	if err := s.applyDeltas(ctx, tx, []balanceDelta{
		{userID: userID, asset: from, free: fromAmt.Neg()},
		{userID: userID, asset: to, free: toAmt},
	}); err != nil {
		return ConvertOrder{}, false, err
	}
	// Debit = bucket increases, credit = bucket decreases (see Settle).
	lines := []ledgerLine{
		{Account: userAcct(userID, "free"), Asset: from, Direction: "credit", Amount: fromAmt},
		{Account: ConvertAccount, Asset: from, Direction: "debit", Amount: fromAmt},
		{Account: ConvertAccount, Asset: to, Direction: "credit", Amount: toAmt},
		{Account: userAcct(userID, "free"), Asset: to, Direction: "debit", Amount: toAmt},
	}
	if err := s.writeJournal(ctx, tx, userID, "convert", ref, lines); err != nil {
		return ConvertOrder{}, false, err
	}
	if _, err := tx.ExecContext(ctx, `UPDATE convert_quotes SET status='filled' WHERE id=$1`, quoteID); err != nil {
		return ConvertOrder{}, false, err
	}
	o, err := scanOrder(tx.QueryRowContext(ctx, `INSERT INTO convert_orders (quote_id,user_id,from_asset,to_asset,from_amount,to_amount,rate,index_rate,spread_bps,usdt_value)
		SELECT id,user_id,from_asset,to_asset,from_amount,to_amount,rate,index_rate,spread_bps,$2 FROM convert_quotes WHERE id=$1 RETURNING `+orderCols,
		quoteID, usdtValue(from, fromAmt).String()))
	if err != nil {
		return ConvertOrder{}, false, err
	}
	return o, false, tx.Commit()
}

// ConvertPosition: net per-asset balance of system:convert (debits - credits).
// Positive = platform received the asset from users; negative = platform paid it
// out and must hold it (hedge) — counted by the solvency check.
func (s *Store) ConvertPosition(ctx context.Context) (map[string]decimal.Decimal, error) {
	rows, err := s.db.QueryContext(ctx, `SELECT asset, COALESCE(SUM(CASE WHEN direction='debit' THEN amount ELSE -amount END),0)::text
		FROM ledger_entries WHERE account=$1 GROUP BY asset`, ConvertAccount)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	out := map[string]decimal.Decimal{}
	for rows.Next() {
		var a, v string
		if err := rows.Scan(&a, &v); err != nil {
			return nil, err
		}
		out[a] = decimal.RequireFromString(v)
	}
	return out, rows.Err()
}

// ---- HTTP ----

func (s *Server) convertRoutes(mux *http.ServeMux) {
	mux.HandleFunc("GET /convert/pairs", s.handleConvertPairs)
	mux.HandleFunc("POST /convert/quote", s.user(s.handleConvertQuote))
	mux.HandleFunc("POST /convert/accept", s.user(s.handleConvertAccept))
	mux.HandleFunc("GET /convert/orders", s.user(s.handleConvertOrders))
	mux.HandleFunc("GET /internal/convert/config", s.internal(s.handleConvertConfigGet))
	mux.HandleFunc("PUT /internal/convert/config", s.internal(s.handleConvertConfigPut))
	mux.HandleFunc("GET /internal/convert/orders", s.internal(s.handleConvertOrders))
	mux.HandleFunc("GET /internal/convert/position", s.internal(s.handleConvertPosition))
	mux.HandleFunc("GET /internal/convert/hedges", s.internal(s.handleConvertHedges))
	mux.HandleFunc("GET /internal/convert/events", s.internal(s.handleConvertEvents))
	mux.HandleFunc("POST /internal/convert/hedge/run", s.internal(s.handleHedgeRun))
}

func (s *Server) user(next http.HandlerFunc) http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		if r.Header.Get("X-User-Id") == "" {
			common.WriteError(w, http.StatusUnauthorized, "AUTH_UNAUTHORIZED", "请先登录")
			return
		}
		next(w, r)
	}
}

func writeConvertErr(w http.ResponseWriter, where string, err error) {
	var ce *convertErr
	switch {
	case errors.As(err, &ce):
		common.WriteError(w, ce.status, ce.code, ce.msg)
	case errors.Is(err, ErrQuoteNotFound):
		common.WriteError(w, http.StatusNotFound, "QUOTE_NOT_FOUND", "报价不存在")
	case errors.Is(err, ErrQuoteExpired):
		common.WriteError(w, http.StatusConflict, "QUOTE_EXPIRED", "报价已过期，请重新询价")
	case errors.Is(err, ErrInsufficient):
		common.WriteError(w, http.StatusBadRequest, "INSUFFICIENT_BALANCE", "余额不足")
	default:
		log.Printf("convert %s: %v", where, err)
		common.WriteError(w, http.StatusInternalServerError, "INTERNAL", "服务器内部错误")
	}
}

// GET /convert/pairs — public. Every ordered pair of enabled assets.
func (s *Server) handleConvertPairs(w http.ResponseWriter, r *http.Request) {
	cfg, err := s.store.convertConfig(r.Context(), s.cfg.Convert)
	if err != nil {
		writeConvertErr(w, "pairs", err)
		return
	}
	type pair struct {
		From          string `json:"from"`
		To            string `json:"to"`
		FromPrecision int    `json:"fromPrecision"`
		ToPrecision   int    `json:"toPrecision"`
		SpreadBps     int    `json:"spreadBps"`
		Route         string `json:"route"` // direct | via_usdt | stable
		Paused        bool   `json:"paused"`
		PauseReason   string `json:"pauseReason,omitempty"` // depeg | price_unavailable
	}
	var depeg *DepegState
	assets := []map[string]any{}
	pairs := []pair{}
	for _, a := range cfg.Assets {
		if !a.Enabled {
			continue
		}
		assets = append(assets, map[string]any{"asset": a.Asset, "precision": a.Precision})
		for _, b := range cfg.Assets {
			if !b.Enabled || a.Asset == b.Asset {
				continue
			}
			p := pair{From: a.Asset, To: b.Asset, FromPrecision: a.Precision, ToPrecision: b.Precision, SpreadBps: cfg.SpreadBps, Route: "direct"}
			switch {
			case stablecoins[a.Asset] && stablecoins[b.Asset]:
				p.SpreadBps, p.Route = 0, "stable"
				if isDepegGuarded(a.Asset, b.Asset) {
					if depeg == nil {
						st := s.checkDepeg(r.Context())
						depeg = &st
					}
					p.Paused, p.PauseReason = depeg.Paused, depeg.Reason
				}
			case !stablecoins[a.Asset] && !stablecoins[b.Asset]:
				p.Route = "via_usdt"
			}
			pairs = append(pairs, p)
		}
	}
	common.WriteJSON(w, http.StatusOK, map[string]any{"assets": assets, "pairs": pairs, "minUsdt": cfg.MinUSDT, "maxUsdt": cfg.MaxUSDT,
		"spreadBps": cfg.SpreadBps, "fee": "0", "quoteTtlSeconds": int(s.cfg.Convert.QuoteTTL / time.Second)})
}

// POST /convert/quote {from, to, fromAmount | toAmount}
func (s *Server) handleConvertQuote(w http.ResponseWriter, r *http.Request) {
	var req quoteReq
	if err := common.DecodeJSON(r, &req); err != nil {
		common.WriteError(w, http.StatusBadRequest, "VALIDATION_ERROR", "请求参数不合法")
		return
	}
	q, err := s.quote(r.Context(), r.Header.Get("X-User-Id"), req)
	if err != nil {
		writeConvertErr(w, "quote", err)
		return
	}
	common.WriteJSON(w, http.StatusOK, q)
}

// POST /convert/accept {quoteId}
func (s *Server) handleConvertAccept(w http.ResponseWriter, r *http.Request) {
	var req struct {
		QuoteID string `json:"quoteId"`
	}
	if err := common.DecodeJSON(r, &req); err != nil || req.QuoteID == "" {
		common.WriteError(w, http.StatusBadRequest, "VALIDATION_ERROR", "缺少 quoteId")
		return
	}
	uid := r.Header.Get("X-User-Id")
	if err := s.store.EnsureAccount(r.Context(), uid); err != nil {
		writeConvertErr(w, "accept ensure", err)
		return
	}
	value := func(asset string, amt decimal.Decimal) decimal.Decimal {
		if p, err := s.usdtPrice(r.Context(), asset); err == nil {
			return amt.Mul(p).Round(8)
		}
		return decimal.Zero
	}
	o, replay, err := s.store.AcceptQuote(r.Context(), uid, req.QuoteID, s.now(), value,
		func(from, to string) error {
			if err := s.pairGuard(r.Context(), from, to); err != nil {
				return err
			}
			for _, a := range []string{from, to} {
				if a != "USDT" {
					if _, err := s.usdtPrice(r.Context(), a); err != nil {
						return err
					}
				}
			}
			return nil
		})
	if err != nil {
		writeConvertErr(w, "accept", err)
		return
	}
	o.UserID = ""
	common.WriteJSON(w, http.StatusOK, map[string]any{"order": o, "replay": replay})
}

// GET /convert/orders?page&pageSize[&asset] (user) | /internal/convert/orders?userId= (admin)
func (s *Server) handleConvertOrders(w http.ResponseWriter, r *http.Request) {
	q := r.URL.Query()
	page := atoiDefault(q.Get("page"), 1, 1, 1<<30)
	size := atoiDefault(q.Get("pageSize"), 20, 1, 100)
	internal := strings.HasPrefix(r.URL.Path, "/internal/")
	uid := r.Header.Get("X-User-Id")
	if internal {
		uid = q.Get("userId")
	}
	where, args := "WHERE ($1 = '' OR user_id = $1) AND ($2 = '' OR from_asset = $2 OR to_asset = $2)", []any{uid, strings.ToUpper(q.Get("asset"))}
	var total int
	if err := s.store.db.QueryRowContext(r.Context(), `SELECT count(*) FROM convert_orders `+where, args...).Scan(&total); err != nil {
		writeConvertErr(w, "orders count", err)
		return
	}
	rows, err := s.store.db.QueryContext(r.Context(), `SELECT `+orderCols+` FROM convert_orders `+where+` ORDER BY id DESC LIMIT $3 OFFSET $4`,
		append(args, size, (page-1)*size)...)
	if err != nil {
		writeConvertErr(w, "orders", err)
		return
	}
	defer rows.Close()
	items := []ConvertOrder{}
	for rows.Next() {
		o, err := scanOrder(rows)
		if err != nil {
			writeConvertErr(w, "orders scan", err)
			return
		}
		if !internal {
			o.UserID = ""
		}
		items = append(items, o)
	}
	common.WriteJSON(w, http.StatusOK, map[string]any{"items": items, "page": page, "pageSize": size, "total": total})
}

func (s *Server) handleConvertConfigGet(w http.ResponseWriter, r *http.Request) {
	cfg, err := s.store.convertConfig(r.Context(), s.cfg.Convert)
	if err != nil {
		writeConvertErr(w, "config", err)
		return
	}
	common.WriteJSON(w, http.StatusOK, cfg)
}

// PUT /internal/convert/config {spreadBps?, minUsdt?, maxUsdt?, assets?:[{asset, enabled}], updatedBy}
func (s *Server) handleConvertConfigPut(w http.ResponseWriter, r *http.Request) {
	var req struct {
		SpreadBps *int    `json:"spreadBps"`
		MinUSDT   *string `json:"minUsdt"`
		MaxUSDT   *string `json:"maxUsdt"`
		DepegBps  *int    `json:"depegBps"`
		Hedge     *struct {
			Enabled              *bool   `json:"enabled"`
			SlippageBps          *int    `json:"slippageBps"`
			MaxOrderUSDT         *string `json:"maxOrderUsdt"`
			MaxHourUSDT          *string `json:"maxHourUsdt"`
			UnhedgedAlertUSDT    *string `json:"unhedgedAlertUsdt"`
			UnhedgedAlertMinutes *int    `json:"unhedgedAlertMinutes"`
			CancelAfterSeconds   *int    `json:"cancelAfterSeconds"`
		} `json:"hedge"`
		Assets []struct {
			Asset        string  `json:"asset"`
			Enabled      *bool   `json:"enabled"`
			HedgeEnabled *bool   `json:"hedgeEnabled"`
			HedgeMinQty  *string `json:"hedgeMinQty"`
		} `json:"assets"`
		UpdatedBy string `json:"updatedBy"`
	}
	bad := func(m string) { common.WriteError(w, http.StatusBadRequest, "VALIDATION_ERROR", m) }
	if err := common.DecodeJSON(r, &req); err != nil {
		bad("请求参数不合法")
		return
	}
	cur, err := s.store.convertConfig(r.Context(), s.cfg.Convert)
	if err != nil {
		writeConvertErr(w, "config", err)
		return
	}
	spread, mn, mx := cur.SpreadBps, decimal.RequireFromString(cur.MinUSDT), decimal.RequireFromString(cur.MaxUSDT)
	if req.SpreadBps != nil {
		spread = *req.SpreadBps
	}
	if spread < 0 || spread > 500 {
		bad("spreadBps 必须在 0–500 之间")
		return
	}
	parse := func(p *string, into *decimal.Decimal) bool {
		if p == nil {
			return true
		}
		v, err := decimal.NewFromString(*p)
		if err != nil || v.Sign() <= 0 {
			return false
		}
		*into = v
		return true
	}
	if !parse(req.MinUSDT, &mn) || !parse(req.MaxUSDT, &mx) || mn.GreaterThan(mx) {
		bad("minUsdt/maxUsdt 不合法")
		return
	}
	depeg := cur.DepegBps
	if req.DepegBps != nil {
		depeg = *req.DepegBps
	}
	if depeg < 1 || depeg > 1000 {
		bad("depegBps 必须在 1–1000 之间")
		return
	}
	h := cur.Hedge
	hmo, hmh, hal := decimal.RequireFromString(h.MaxOrderUSDT), decimal.RequireFromString(h.MaxHourUSDT), decimal.RequireFromString(h.UnhedgedAlertUSDT)
	var hen *bool
	if req.Hedge != nil {
		hq := req.Hedge
		hen = hq.Enabled
		if hq.SlippageBps != nil {
			h.SlippageBps = *hq.SlippageBps
		}
		if hq.UnhedgedAlertMinutes != nil {
			h.UnhedgedAlertMinutes = *hq.UnhedgedAlertMinutes
		}
		if hq.CancelAfterSeconds != nil {
			h.CancelAfterSeconds = *hq.CancelAfterSeconds
		}
		if !parse(hq.MaxOrderUSDT, &hmo) || !parse(hq.MaxHourUSDT, &hmh) || !parse(hq.UnhedgedAlertUSDT, &hal) || hmo.GreaterThan(hmh) {
			bad("hedge 限额不合法（maxOrderUsdt ≤ maxHourUsdt，均 > 0）")
			return
		}
	}
	if h.SlippageBps < 1 || h.SlippageBps > 500 {
		bad("hedge.slippageBps 必须在 1–500 之间")
		return
	}
	if h.UnhedgedAlertMinutes < 1 || h.UnhedgedAlertMinutes > 1440 {
		bad("hedge.unhedgedAlertMinutes 必须在 1–1440 之间")
		return
	}
	if h.CancelAfterSeconds < 1 || h.CancelAfterSeconds > 3600 {
		bad("hedge.cancelAfterSeconds 必须在 1–3600 之间")
		return
	}
	for _, a := range req.Assets {
		if _, ok := cur.asset(strings.ToUpper(a.Asset)); !ok {
			bad("未知币种 " + a.Asset)
			return
		}
		if a.HedgeMinQty != nil {
			if v, err := decimal.NewFromString(*a.HedgeMinQty); err != nil || v.Sign() < 0 {
				bad("hedgeMinQty 不合法")
				return
			}
		}
	}
	tx, err := s.store.db.BeginTx(r.Context(), nil)
	if err != nil {
		writeConvertErr(w, "config tx", err)
		return
	}
	defer tx.Rollback() //nolint:errcheck
	by := req.UpdatedBy
	if by == "" {
		by = "internal"
	}
	if _, err := tx.ExecContext(r.Context(), `UPDATE convert_config SET spread_bps=$1, min_usdt=$2, max_usdt=$3, updated_by=$4, updated_at=now() WHERE id=1`,
		spread, mn.String(), mx.String(), by); err != nil {
		writeConvertErr(w, "config update", err)
		return
	}
	if _, err := tx.ExecContext(r.Context(), `UPDATE convert_config SET depeg_bps=$1, hedge_enabled=COALESCE($2, hedge_enabled), hedge_slippage_bps=$3,
		hedge_max_order_usdt=$4, hedge_max_hour_usdt=$5, hedge_unhedged_alert_usdt=$6, hedge_unhedged_alert_minutes=$7, hedge_cancel_after_seconds=$8 WHERE id=1`,
		depeg, hen, h.SlippageBps, hmo.String(), hmh.String(), hal.String(), h.UnhedgedAlertMinutes, h.CancelAfterSeconds); err != nil {
		writeConvertErr(w, "config update", err)
		return
	}
	for _, a := range req.Assets {
		if _, err := tx.ExecContext(r.Context(), `UPDATE convert_assets SET enabled=COALESCE($2, enabled), hedge_enabled=COALESCE($3, hedge_enabled),
			hedge_min_qty=COALESCE($4::numeric, hedge_min_qty) WHERE asset=$1`, strings.ToUpper(a.Asset), a.Enabled, a.HedgeEnabled, a.HedgeMinQty); err != nil {
			writeConvertErr(w, "config asset", err)
			return
		}
	}
	if err := tx.Commit(); err != nil {
		writeConvertErr(w, "config commit", err)
		return
	}
	s.handleConvertConfigGet(w, r)
}

func (s *Server) handleConvertPosition(w http.ResponseWriter, r *http.Request) {
	pos, err := s.store.ConvertPosition(r.Context())
	if err != nil {
		writeConvertErr(w, "position", err)
		return
	}
	hedgeBal, err := s.store.accountTotals(r.Context(), s.cfg.Convert.HedgeUserID)
	if err != nil {
		writeConvertErr(w, "position hedge", err)
		return
	}
	seen := map[string]bool{}
	assets := []string{}
	for a := range pos {
		seen[a] = true
		assets = append(assets, a)
	}
	for a := range hedgeBal {
		if !seen[a] {
			assets = append(assets, a)
		}
	}
	sort.Strings(assets)
	state := s.hedger().snapshot()
	items := []map[string]any{}
	for _, a := range assets {
		net := pos[a].Add(hedgeBal[a])
		// residualUsdt: USDT value of what is left unhedged (fees, dust below
		// hedgeMinQty, rounding). Accepted, never funded from other accounts.
		// For USDT itself it is the desk's accumulated USDT net (spread − fees).
		residual := ""
		if p, err := s.usdtPrice(r.Context(), a); err == nil {
			residual = net.Mul(p).Round(8).String()
		}
		it := map[string]any{"asset": a, "position": pos[a].String(), "hedgeBalance": hedgeBal[a].String(),
			"netPosition": net.String(), "residualUsdt": residual}
		if st, ok := state[a]; ok {
			it["hedge"] = st
		}
		items = append(items, it)
	}
	cfg, _ := s.store.convertConfig(r.Context(), s.cfg.Convert)
	common.WriteJSON(w, http.StatusOK, map[string]any{"account": ConvertAccount, "hedgeAccount": s.cfg.Convert.HedgeUserID,
		"hedgeEnabled": cfg.Hedge.Enabled, "depeg": s.checkDepeg(r.Context()), "items": items})
}
