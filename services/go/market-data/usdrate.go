package main

import (
	"context"
	"encoding/json"
	"net/http"
	"net/url"
	"strings"
	"sync"
	"time"

	"github.com/linbit/platform/services/go/common"
	"github.com/shopspring/decimal"
)

// USDT/USD 是交易所的实时报价：1 USDT 值多少 USD，绝不假定为 1。
// 精度 8 位，一半远离零（shopspring Round）。

const (
	usdPrecision  = 8
	usdFresh      = time.Minute
	usdStaleMax   = 15 * time.Minute
	usdConvention = "usdPrice = lastPrice * rate, rate is USD per 1 USDT from a live venue ticker; USDT is not treated as 1 USD; 8 decimal places, half away from zero"
)

type usdSnap struct {
	Rate      decimal.Decimal
	Source    string
	UpdatedAt time.Time
	Stale     bool
	OK        bool
}

type usdQuoteDTO struct {
	Available  bool       `json:"available"`
	Price      *string    `json:"price"`
	Rate       *string    `json:"rate"`
	Source     string     `json:"source"`
	UpdatedAt  *time.Time `json:"updatedAt"`
	Stale      bool       `json:"stale"`
	Precision  int        `json:"precision"`
	Rounding   string     `json:"rounding"`
	Convention string     `json:"convention"`
	Reason     string     `json:"reason,omitempty"`
	PegAssumed bool       `json:"pegAssumed"`
}

var usdCache struct {
	mu   sync.Mutex
	snap usdSnap
	at   time.Time
}

func resetUSDCache() {
	usdCache.mu.Lock()
	usdCache.snap = usdSnap{}
	usdCache.at = time.Time{}
	usdCache.mu.Unlock()
}

// instrumentUSD converts a USDT last price. ok is false when the rate or price
// cannot be used; the reason is safe to show to clients.
func instrumentUSD(symbol, last string, rate decimal.Decimal) (price, reason string) {
	if !rate.IsPositive() {
		return "", "rate_unavailable"
	}
	if !strings.HasSuffix(strings.ToUpper(strings.TrimSpace(symbol)), "USDT") {
		return "", "quote_not_usdt"
	}
	px, err := decimal.NewFromString(strings.TrimSpace(last))
	if err != nil || !px.IsPositive() {
		return "", "no_price"
	}
	return px.Mul(rate).Round(usdPrecision).StringFixed(usdPrecision), ""
}

func (s *Server) currentUSDTUSD(ctx context.Context) usdSnap {
	usdCache.mu.Lock()
	if usdCache.snap.OK && time.Since(usdCache.at) < usdFresh {
		snap := usdCache.snap
		usdCache.mu.Unlock()
		return snap
	}
	prev := usdCache.snap
	usdCache.mu.Unlock()
	snap, err := fetchUSDTUSD(ctx, s.cfg)
	usdCache.mu.Lock()
	defer usdCache.mu.Unlock()
	if err == nil {
		usdCache.snap = snap
		usdCache.at = time.Now()
		return snap
	}
	if prev.OK && time.Since(prev.UpdatedAt) < usdStaleMax {
		prev.Stale = true
		return prev
	}
	return usdSnap{}
}

func fetchUSDTUSD(ctx context.Context, cfg Config) (usdSnap, error) {
	lo, hi, err := usdBounds(cfg)
	if err != nil {
		return usdSnap{}, err
	}
	if snap, err := fetchCoinbaseUSDT(ctx, cfg.CoinbaseURL, lo, hi); err == nil {
		return snap, nil
	}
	return fetchKrakenUSDT(ctx, cfg.KrakenURL, lo, hi)
}

func usdBounds(cfg Config) (decimal.Decimal, decimal.Decimal, error) {
	lo, err := decimal.NewFromString(strings.TrimSpace(cfg.IndexUsdtUsdMin))
	if err != nil || !lo.IsPositive() {
		lo = decimal.RequireFromString("0.97")
	}
	hi, err := decimal.NewFromString(strings.TrimSpace(cfg.IndexUsdtUsdMax))
	if err != nil || !hi.IsPositive() {
		hi = decimal.RequireFromString("1.03")
	}
	if hi.LessThan(lo) {
		return decimal.Zero, decimal.Zero, errBounds
	}
	return lo, hi, nil
}

var errBounds = errString("usdt usd bounds")
var errRate = errString("usdt usd unavailable")

type errString string

func (e errString) Error() string { return string(e) }

func acceptRate(raw string, lo, hi decimal.Decimal) (decimal.Decimal, error) {
	r, err := decimal.NewFromString(strings.TrimSpace(raw))
	if err != nil || !r.IsPositive() || r.LessThan(lo) || r.GreaterThan(hi) {
		return decimal.Zero, errRate
	}
	return r.Round(usdPrecision), nil
}

func fetchCoinbaseUSDT(ctx context.Context, base string, lo, hi decimal.Decimal) (usdSnap, error) {
	base = strings.TrimRight(strings.TrimSpace(base), "/")
	if base == "" {
		return usdSnap{}, errRate
	}
	c, cancel := context.WithTimeout(ctx, 3*time.Second)
	defer cancel()
	req, err := http.NewRequestWithContext(c, http.MethodGet, base+"/products/USDT-USD/ticker", nil)
	if err != nil {
		return usdSnap{}, err
	}
	resp, err := http.DefaultClient.Do(req)
	if err != nil {
		return usdSnap{}, err
	}
	defer resp.Body.Close()
	if resp.StatusCode != 200 {
		return usdSnap{}, errRate
	}
	var body struct {
		Price string `json:"price"`
		Time  string `json:"time"`
	}
	if err := json.NewDecoder(resp.Body).Decode(&body); err != nil {
		return usdSnap{}, err
	}
	rate, err := acceptRate(body.Price, lo, hi)
	if err != nil {
		return usdSnap{}, err
	}
	at := time.Now().UTC()
	if t, err := time.Parse(time.RFC3339Nano, body.Time); err == nil {
		at = t.UTC()
	}
	return usdSnap{Rate: rate, Source: hostOf(base), UpdatedAt: at, OK: true}, nil
}

func fetchKrakenUSDT(ctx context.Context, base string, lo, hi decimal.Decimal) (usdSnap, error) {
	base = strings.TrimRight(strings.TrimSpace(base), "/")
	if base == "" {
		return usdSnap{}, errRate
	}
	c, cancel := context.WithTimeout(ctx, 3*time.Second)
	defer cancel()
	req, err := http.NewRequestWithContext(c, http.MethodGet, base+"/0/public/Ticker?pair=USDTUSD", nil)
	if err != nil {
		return usdSnap{}, err
	}
	resp, err := http.DefaultClient.Do(req)
	if err != nil {
		return usdSnap{}, err
	}
	defer resp.Body.Close()
	if resp.StatusCode != 200 {
		return usdSnap{}, errRate
	}
	var body struct {
		Error  []string `json:"error"`
		Result map[string]struct {
			C []string `json:"c"`
		} `json:"result"`
	}
	if err := json.NewDecoder(resp.Body).Decode(&body); err != nil || len(body.Error) > 0 {
		return usdSnap{}, errRate
	}
	var raw string
	for _, v := range body.Result {
		if len(v.C) > 0 {
			raw = v.C[0]
			break
		}
	}
	rate, err := acceptRate(raw, lo, hi)
	if err != nil {
		return usdSnap{}, err
	}
	return usdSnap{Rate: rate, Source: hostOf(base), UpdatedAt: time.Now().UTC(), OK: true}, nil
}

func hostOf(raw string) string {
	if u, err := url.Parse(raw); err == nil && u.Host != "" {
		return u.Host
	}
	return raw
}

// readUSDCache returns the last USDT/USD quote without contacting a venue.
func readUSDCache() usdSnap {
	usdCache.mu.Lock()
	defer usdCache.mu.Unlock()
	if !usdCache.snap.OK {
		return usdSnap{}
	}
	s := usdCache.snap
	if time.Since(usdCache.at) >= usdFresh {
		s.Stale = true
	}
	if time.Since(s.UpdatedAt) >= usdStaleMax && time.Since(usdCache.at) >= usdFresh {
		return usdSnap{}
	}
	return s
}

func annotateUSD(t tickerDTO, snap usdSnap) *usdQuoteDTO {
	out := &usdQuoteDTO{Precision: usdPrecision, Rounding: "half_away_from_zero", Convention: usdConvention, PegAssumed: false}
	if snap.OK {
		rate := snap.Rate.StringFixed(usdPrecision)
		out.Rate = &rate
		out.Source = snap.Source
		at := snap.UpdatedAt.UTC()
		out.UpdatedAt = &at
		out.Stale = snap.Stale
	}
	price, reason := instrumentUSD(t.Symbol, t.LastPrice, snap.Rate)
	if reason != "" || !snap.OK {
		if reason == "" {
			reason = "rate_unavailable"
		}
		out.Reason = reason
		return out
	}
	out.Available = true
	out.Price = &price
	return out
}

func (s *Server) usdForTicker(ctx context.Context, t tickerDTO) *usdQuoteDTO {
	return annotateUSD(t, s.currentUSDTUSD(ctx))
}

// StartUSDRefresh refreshes the USDT/USD cache off the ticker hot path.
func (s *Server) StartUSDRefresh(ctx context.Context) {
	go func() {
		refreshUSD(ctx, s.cfg)
		t := time.NewTicker(usdFresh)
		defer t.Stop()
		for {
			select {
			case <-ctx.Done():
				return
			case <-t.C:
				refreshUSD(ctx, s.cfg)
			}
		}
	}()
}

func refreshUSD(ctx context.Context, cfg Config) {
	snap, err := fetchUSDTUSD(ctx, cfg)
	usdCache.mu.Lock()
	defer usdCache.mu.Unlock()
	usdCache.at = time.Now()
	if err == nil {
		usdCache.snap = snap
	}
}

func (s *Server) handleUSDRate(w http.ResponseWriter, r *http.Request) {
	snap := s.currentUSDTUSD(r.Context())
	var rate any
	var at any
	if snap.OK {
		rate = snap.Rate.StringFixed(usdPrecision)
		at = snap.UpdatedAt.UTC()
	}
	w.Header().Set("Cache-Control", "public, max-age=30")
	common.WriteJSON(w, http.StatusOK, map[string]any{
		"pair": "USDTUSD", "available": snap.OK, "rate": rate, "source": snap.Source, "updatedAt": at,
		"stale": snap.Stale, "precision": usdPrecision, "rounding": "half_away_from_zero",
		"unit": "USD per 1 USDT", "pegAssumed": false,
		"convention": usdConvention,
	})
}
