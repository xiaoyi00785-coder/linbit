package main

import (
	"context"
	"fmt"
	"log"
	"net/url"
	"strings"
	"sync"

	"github.com/shopspring/decimal"
)

// feed_usd.go: USD-quoted index sources (Coinbase, Kraken) for symbols that no
// USDT venue lists (e.g. TONUSDT: Binance BREAK, OKX/Bybit/Gate do not list it).
//
// Design: "coinbase" and "kraken" are ordinary INDEX_FEED entries (so failover
// order, staleness, status filtering and the unchanged-price guard apply as for
// every other source), but they only ever quote the symbols listed in
// INDEX_USD_FALLBACK_SYMBOLS (default TONUSDT): BASEUSDT is read from the
// venue's BASE/USD pair. Other symbols are never quoted from a USD venue.
//
// USD -> USDT: the index is denominated in USDT, so each venue's own live
// USDT/USD rate (Coinbase USDT-USD, Kraken USDTUSD) is fetched in the same poll
// and applied as price_usdt = price_usd / usdt_usd (USDT trades slightly below
// 1 USD, so the USDT price is slightly higher than the USD price). A missing,
// unparsable, non-positive or out-of-bounds rate (INDEX_USDT_USD_MIN/MAX,
// default 0.97..1.03), or an USDT/USD pair that is not online, makes the whole
// venue fail (no quotes), so the converted components are not fresh and the
// index fails closed. The rate is never cached across polls.
//
// The unchanged-price guard watches the venue's raw USD price, not the
// converted one: otherwise the moving USDT/USD rate would hide a frozen TON/USD.

// quoteMeta describes how a quote was derived (USD venues only).
type quoteMeta struct {
	Pair    string          // venue pair, e.g. TON-USD
	Raw     decimal.Decimal // venue USD price
	UsdtUsd decimal.Decimal // rate applied: price_usdt = Raw / UsdtUsd
}

// metaFetch is a price fetcher that also reports per-symbol derivation.
type metaFetch func(ctx context.Context) (map[string]decimal.Decimal, map[string]quoteMeta, error)

// usdBase returns the base asset of a BASEUSDT symbol ("TONUSDT" -> "TON").
func usdBase(symbol string) (string, bool) {
	symbol = strings.ToUpper(strings.TrimSpace(symbol))
	base := strings.TrimSuffix(symbol, "USDT")
	if base == "" || base == symbol || strings.ContainsAny(base, "-_/ ") {
		return "", false
	}
	return base, true
}

// usdRate validates a USDT/USD rate against [min, max].
func usdRate(r, lo, hi decimal.Decimal) (decimal.Decimal, error) {
	if !r.IsPositive() {
		return decimal.Zero, fmt.Errorf("USDT/USD rate %s not positive", r)
	}
	if r.LessThan(lo) || r.GreaterThan(hi) {
		return decimal.Zero, fmt.Errorf("USDT/USD rate %s outside [%s, %s]", r, lo, hi)
	}
	return r, nil
}

// toUSDT converts a USD price with the USDT/USD rate.
func toUSDT(usd, rate decimal.Decimal) decimal.Decimal { return usd.Div(rate) }

// ---- Coinbase (Exchange public API) ----

// coinbaseProduct is GET /products/{id}.
type coinbaseProduct struct {
	ID              string `json:"id"`
	Status          string `json:"status"`
	TradingDisabled bool   `json:"trading_disabled"`
	CancelOnly      bool   `json:"cancel_only"`
	AuctionMode     bool   `json:"auction_mode"`
}

// coinbaseOnline: status online, trading not disabled, and neither cancel-only
// nor in auction (no continuous matching => last price is not an index).
func coinbaseOnline(p coinbaseProduct) bool {
	return p.Status == "online" && !p.TradingDisabled && !p.CancelOnly && !p.AuctionMode
}

// coinbaseTrading loads the status of each product id (e.g. TON-USD, USDT-USD):
// GET /products/{id}. The result has an entry (true/false) for every id, so an
// all-halted venue is a valid (empty-of-true) set, never a stale "trading" set.
func coinbaseTrading(base string, ids []string) func(ctx context.Context) (map[string]bool, error) {
	return func(ctx context.Context) (map[string]bool, error) {
		out := make(map[string]bool, len(ids))
		for _, id := range ids {
			var p coinbaseProduct
			if err := getJSONLimit(ctx, strings.TrimRight(base, "/")+"/products/"+url.PathEscape(id), 1<<20, &p); err != nil {
				// unknown product (404) = not listed; anything else = unknown status
				if strings.Contains(err.Error(), "HTTP 404") {
					out[id] = false
					continue
				}
				return nil, fmt.Errorf("coinbase %s status: %w", id, err)
			}
			out[id] = p.ID == id && coinbaseOnline(p)
		}
		return out, nil
	}
}

// coinbaseTicker reads the last trade price: GET /products/{id}/ticker.
func coinbaseTicker(ctx context.Context, base, id string) (decimal.Decimal, error) {
	var t struct {
		Price string `json:"price"`
	}
	if err := getJSON(ctx, strings.TrimRight(base, "/")+"/products/"+url.PathEscape(id)+"/ticker", &t); err != nil {
		return decimal.Zero, err
	}
	p, err := decimal.NewFromString(t.Price)
	if err != nil || !p.IsPositive() {
		return decimal.Zero, fmt.Errorf("coinbase %s: bad price %q", id, t.Price)
	}
	return p, nil
}

// fetchCoinbase quotes BASEUSDT for each symbol from BASE-USD, converted with
// the USDT-USD rate. Any rate problem fails the whole fetch.
func fetchCoinbase(ctx context.Context, base string, symbols []string, st *statusCache, lo, hi decimal.Decimal) (map[string]decimal.Decimal, map[string]quoteMeta, error) {
	trading, err := st.trading()
	if err != nil {
		return nil, nil, err
	}
	if !trading["USDT-USD"] {
		return nil, nil, fmt.Errorf("coinbase USDT-USD not online")
	}
	rawRate, err := coinbaseTicker(ctx, base, "USDT-USD")
	if err != nil {
		return nil, nil, err
	}
	rate, err := usdRate(rawRate, lo, hi)
	if err != nil {
		return nil, nil, fmt.Errorf("coinbase %w", err)
	}
	prices, metas := map[string]decimal.Decimal{}, map[string]quoteMeta{}
	for _, sym := range symbols {
		b, ok := usdBase(sym)
		id := b + "-USD"
		if !ok || !trading[id] {
			continue
		}
		raw, err := coinbaseTicker(ctx, base, id)
		if err != nil {
			continue // this symbol only; the others stay quoted
		}
		prices[sym], metas[sym] = toUSDT(raw, rate), quoteMeta{Pair: id, Raw: raw, UsdtUsd: rate}
	}
	if len(prices) == 0 {
		return nil, nil, fmt.Errorf("no online coinbase USD products quoted")
	}
	return prices, metas, nil
}

// ---- Kraken (public REST) ----

// krakenClient resolves Kraken's pair keys: AssetPairs/Ticker results may be
// keyed by the internal name (USDTZUSD) or the altname (TONUSD).
type krakenClient struct {
	base string
	alts func() map[string]string // internal key -> altname
}

// krakenAsset maps a standard ticker to Kraken's asset code where they differ.
func krakenAsset(b string) string {
	switch b {
	case "BTC":
		return "XBT"
	case "DOGE":
		return "XDG"
	}
	return b
}

// krakenTrading loads status for each altname (TONUSD, USDTUSD):
// GET /0/public/AssetPairs?pair=... (status online | cancel_only | post_only |
// limit_only | reduce_only | delisted). Only "online" is true. A pair Kraken
// does not return is false. It also records internal-key -> altname.
func krakenTrading(base string, alts []string, save func(map[string]string)) func(ctx context.Context) (map[string]bool, error) {
	return func(ctx context.Context) (map[string]bool, error) {
		var body struct {
			Error  []string `json:"error"`
			Result map[string]struct {
				Altname string `json:"altname"`
				Status  string `json:"status"`
			} `json:"result"`
		}
		u := strings.TrimRight(base, "/") + "/0/public/AssetPairs?pair=" + url.QueryEscape(strings.Join(alts, ","))
		if err := getJSONLimit(ctx, u, 4<<20, &body); err != nil {
			return nil, err
		}
		out := make(map[string]bool, len(alts))
		for _, a := range alts {
			out[a] = false
		}
		// "Unknown asset pair" for a single pair comes back as an error entry with
		// the others still present; any non-empty error with no result is a failure.
		if len(body.Error) > 0 && len(body.Result) == 0 {
			if allUnknownPair(body.Error) {
				return out, nil
			}
			return nil, fmt.Errorf("kraken: %s", strings.Join(body.Error, ";"))
		}
		keys := map[string]string{}
		for key, r := range body.Result {
			alt := r.Altname
			if alt == "" {
				alt = key
			}
			keys[key] = alt
			if _, want := out[alt]; want {
				out[alt] = r.Status == "online"
			}
		}
		if save != nil {
			save(keys)
		}
		return out, nil
	}
}

func allUnknownPair(errs []string) bool {
	for _, e := range errs {
		if !strings.Contains(e, "Unknown asset pair") {
			return false
		}
	}
	return len(errs) > 0
}

// krakenTickers reads last trade prices (field c[0]) by altname:
// GET /0/public/Ticker?pair=A,B. An error array is a failure.
func (k krakenClient) tickers(ctx context.Context, alts []string) (map[string]decimal.Decimal, error) {
	var body struct {
		Error  []string `json:"error"`
		Result map[string]struct {
			C []string `json:"c"`
		} `json:"result"`
	}
	u := strings.TrimRight(k.base, "/") + "/0/public/Ticker?pair=" + url.QueryEscape(strings.Join(alts, ","))
	if err := getJSON(ctx, u, &body); err != nil {
		return nil, err
	}
	if len(body.Error) > 0 {
		return nil, fmt.Errorf("kraken: %s", strings.Join(body.Error, ";"))
	}
	keys := k.alts()
	out := map[string]decimal.Decimal{}
	for key, r := range body.Result {
		alt := key
		if a, ok := keys[key]; ok {
			alt = a
		}
		if len(r.C) == 0 {
			continue
		}
		if p, err := decimal.NewFromString(r.C[0]); err == nil && p.IsPositive() {
			out[alt] = p
		}
	}
	return out, nil
}

// fetchKraken quotes BASEUSDT from BASEUSD converted with USDTUSD.
func fetchKraken(ctx context.Context, k krakenClient, symbols []string, st *statusCache, lo, hi decimal.Decimal) (map[string]decimal.Decimal, map[string]quoteMeta, error) {
	trading, err := st.trading()
	if err != nil {
		return nil, nil, err
	}
	if !trading["USDTUSD"] {
		return nil, nil, fmt.Errorf("kraken USDTUSD not online")
	}
	want := []string{"USDTUSD"}
	bySym := map[string]string{}
	for _, sym := range symbols {
		if b, ok := usdBase(sym); ok {
			if alt := krakenAsset(b) + "USD"; trading[alt] {
				want = append(want, alt)
				bySym[sym] = alt
			}
		}
	}
	tk, err := k.tickers(ctx, want)
	if err != nil {
		return nil, nil, err
	}
	rate, err := usdRate(tk["USDTUSD"], lo, hi)
	if err != nil {
		return nil, nil, fmt.Errorf("kraken %w", err)
	}
	prices, metas := map[string]decimal.Decimal{}, map[string]quoteMeta{}
	for sym, alt := range bySym {
		if raw, ok := tk[alt]; ok {
			prices[sym], metas[sym] = toUSDT(raw, rate), quoteMeta{Pair: alt, Raw: raw, UsdtUsd: rate}
		}
	}
	if len(prices) == 0 {
		return nil, nil, fmt.Errorf("no online kraken USD pairs quoted")
	}
	return prices, metas, nil
}

// newUSDSources builds the Coinbase / Kraken sources for the fallback symbols.
func newUSDSources(name string, cfg Config) *indexSource {
	var syms []string
	for _, s := range cfg.IndexUSDFallbackSymbols {
		if _, ok := usdBase(s); ok {
			syms = append(syms, strings.ToUpper(strings.TrimSpace(s)))
		} else {
			log.Printf("index feed %s: ignoring INDEX_USD_FALLBACK_SYMBOLS entry %q (need BASEUSDT)", name, s)
		}
	}
	if len(syms) == 0 {
		return nil
	}
	lo, hi := cfg.usdtUSDBounds()
	switch name {
	case "coinbase":
		ids := []string{"USDT-USD"}
		for _, s := range syms {
			b, _ := usdBase(s)
			ids = append(ids, b+"-USD")
		}
		base := cfg.CoinbaseURL
		st := newStatusCache(cfg.IndexStatusRefresh, coinbaseTrading(base, ids))
		return &indexSource{name: name, quotes: map[string]indexQuote{}, usd: true,
			fetchMeta: func(ctx context.Context) (map[string]decimal.Decimal, map[string]quoteMeta, error) {
				return fetchCoinbase(ctx, base, syms, st, lo, hi)
			}}
	case "kraken":
		alts := []string{"USDTUSD"}
		for _, s := range syms {
			b, _ := usdBase(s)
			alts = append(alts, krakenAsset(b)+"USD")
		}
		kc := &krakenClient{base: cfg.KrakenURL}
		var (
			mu   sync.Mutex
			keys map[string]string
		)
		kc.alts = func() map[string]string { mu.Lock(); defer mu.Unlock(); out := keys; return out }
		st := newStatusCache(cfg.IndexStatusRefresh, krakenTrading(cfg.KrakenURL, alts, func(m map[string]string) {
			mu.Lock()
			keys = m
			mu.Unlock()
		}))
		return &indexSource{name: name, quotes: map[string]indexQuote{}, usd: true,
			fetchMeta: func(ctx context.Context) (map[string]decimal.Decimal, map[string]quoteMeta, error) {
				return fetchKraken(ctx, *kc, syms, st, lo, hi)
			}}
	}
	return nil
}


// usdtUSDBounds parses INDEX_USDT_USD_MIN/MAX (defaults 0.97..1.03).
func (c Config) usdtUSDBounds() (decimal.Decimal, decimal.Decimal) {
	lo, err := decimal.NewFromString(strings.TrimSpace(c.IndexUsdtUsdMin))
	if err != nil || !lo.IsPositive() {
		lo = decimal.RequireFromString("0.97")
	}
	hi, err := decimal.NewFromString(strings.TrimSpace(c.IndexUsdtUsdMax))
	if err != nil || !hi.IsPositive() || hi.LessThan(lo) {
		hi = decimal.RequireFromString("1.03")
	}
	return lo, hi
}
