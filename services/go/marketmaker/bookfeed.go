package main

import (
	"context"
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"net/url"
	"strings"
	"sync"
	"time"

	"github.com/shopspring/decimal"
)

// bookRef maps a Linbit symbol onto venue instrument ids.
type bookRef struct {
	Binance string // Binance spot or USDT-M id (BTCUSDT, XAUUSDT, CLUSDT)
	OKX     string // OKX instId (BTC-USDT, BTC-USDT-SWAP, XAU-USDT-SWAP)
}

type cacheEnt struct {
	book ExtBook
	err  error
	at   time.Time
}

// bookFeed pulls external depth (Binance primary, OKX backup) and caches it.
type bookFeed struct {
	sources    []string
	binanceURL string
	fapiURL    string
	okxURL     string
	bybitURL   string
	ttl        time.Duration
	scale      decimal.Decimal
	client     *http.Client
	now        func() time.Time

	mu       sync.Mutex
	cache    map[string]cacheEnt
	inflight map[string]chan struct{}
	refs     map[string]bookRef

	ctMu  sync.Mutex
	ctVal map[string]ctEnt // OKX SWAP instId -> contract value (base units per contract)
}

type ctEnt struct {
	val decimal.Decimal
	at  time.Time
}

func newBookFeed(cfg Config) *bookFeed {
	srcs := splitCSV(cfg.BookFeed)
	if len(srcs) == 0 {
		srcs = []string{"binance", "okx", "bybit"}
	}
	ttl := cfg.BookTTL
	if ttl <= 0 {
		ttl = time.Second
	}
	scale := cfg.BookScale
	if !scale.IsPositive() {
		scale = one
	}
	return &bookFeed{
		sources:    srcs,
		binanceURL: strings.TrimRight(firstNonEmpty(cfg.BinanceURL, "https://data-api.binance.vision"), "/"),
		fapiURL:    strings.TrimRight(firstNonEmpty(cfg.BinanceFAPIURL, "https://fapi.binance.com"), "/"),
		okxURL:     strings.TrimRight(firstNonEmpty(cfg.OKXURL, "https://www.okx.com"), "/"),
		bybitURL:   strings.TrimRight(firstNonEmpty(cfg.BybitURL, "https://api.bybit.com"), "/"),
		ttl:        ttl,
		scale:      scale,
		client:     httpc,
		now:        time.Now,
		cache:      map[string]cacheEnt{},
		inflight:   map[string]chan struct{}{},
		refs:       defaultTradFiRefs(),
		ctVal:      map[string]ctEnt{},
	}
}

func splitCSV(s string) []string {
	var out []string
	for _, p := range strings.Split(s, ",") {
		p = strings.ToLower(strings.TrimSpace(p))
		if p != "" {
			out = append(out, p)
		}
	}
	return out
}

func defaultTradFiRefs() map[string]bookRef {
	return map[string]bookRef{
		"AAPLUSDT":  {Binance: "AAPLUSDT", OKX: "AAPL-USDT-SWAP"},
		"TSLAUSDT":  {Binance: "TSLAUSDT", OKX: "TSLA-USDT-SWAP"},
		"NVDAUSDT":  {Binance: "NVDAUSDT", OKX: "NVDA-USDT-SWAP"},
		"MSFTUSDT":  {Binance: "MSFTUSDT", OKX: "MSFT-USDT-SWAP"},
		"GOOGLUSDT": {Binance: "GOOGLUSDT", OKX: "GOOGL-USDT-SWAP"},
		"AMZNUSDT":  {Binance: "AMZNUSDT", OKX: "AMZN-USDT-SWAP"},
		"METAUSDT":  {Binance: "METAUSDT", OKX: "META-USDT-SWAP"},
		"COINUSDT":  {Binance: "COINUSDT", OKX: "COIN-USDT-SWAP"},
		"SPYUSDT":   {Binance: "SPYUSDT", OKX: "SPY-USDT-SWAP"},
		"PAXGUSDT":  {Binance: "XAUUSDT", OKX: "XAU-USDT-SWAP"},
		"XAUTUSDT":  {Binance: "XAUUSDT", OKX: "XAU-USDT-SWAP"},
		"XAGUSDT":   {Binance: "XAGUSDT", OKX: "XAG-USDT-SWAP"},
		"WTIUSDT":   {Binance: "CLUSDT", OKX: "CL-USDT-SWAP"},
	}
}

func depthLimit(levels int) int {
	switch {
	case levels <= 5:
		return 5
	case levels <= 10:
		return 10
	case levels <= 20:
		return 20
	case levels <= 50:
		return 50
	case levels <= 100:
		return 100
	case levels <= 200:
		return 200
	default:
		return 400 // OKX books sz max
	}
}

// binanceDepthLimit maps a wanted depth onto Binance's accepted limits.
func binanceDepthLimit(limit int) int {
	for _, v := range []int{5, 10, 20, 50, 100, 500, 1000} {
		if limit <= v {
			return v
		}
	}
	return 1000
}

func truncBook(b ExtBook, n int) ExtBook {
	if len(b.Bids) > n {
		b.Bids = b.Bids[:n]
	}
	if len(b.Asks) > n {
		b.Asks = b.Asks[:n]
	}
	return b
}

func okxSpotInst(sym string) string {
	sym = strings.ToUpper(sym)
	if strings.HasSuffix(sym, "USDT") {
		return strings.TrimSuffix(sym, "USDT") + "-USDT"
	}
	return sym
}

func (f *bookFeed) setRefs(refs map[string]bookRef) {
	if len(refs) == 0 {
		return
	}
	f.mu.Lock()
	if f.refs == nil {
		f.refs = map[string]bookRef{}
	}
	for k, v := range refs {
		f.refs[strings.ToUpper(k)] = v
	}
	f.mu.Unlock()
}

func (f *bookFeed) resolve(market, symbol string) (fapi bool, binance, okx string) {
	sym := strings.ToUpper(symbol)
	f.mu.Lock()
	ref, tradfi := f.refs[sym]
	f.mu.Unlock()
	if tradfi {
		return true, ref.Binance, ref.OKX
	}
	if strings.EqualFold(market, "futures") {
		return true, sym, okxSpotInst(sym) + "-SWAP"
	}
	return false, sym, okxSpotInst(sym)
}

func (f *bookFeed) get(ctx context.Context, market, symbol string, levels int) (ExtBook, error) {
	fapi, bn, ox := f.resolve(market, symbol)
	limit := depthLimit(levels)
	key := strings.ToUpper(market) + "|" + strings.ToUpper(symbol) + "|" + fmt.Sprint(limit)
	return f.cached(ctx, key, func() (ExtBook, error) {
		var last error
		for _, src := range f.sources {
			var book ExtBook
			var err error
			switch src {
			case "binance":
				if bn == "" {
					continue
				}
				book, err = f.fetchBinance(ctx, fapi, bn, limit)
			case "okx":
				if ox == "" {
					continue
				}
				book, err = f.fetchOKX(ctx, ox, limit)
				if (err != nil || !book.Valid()) && strings.HasSuffix(ox, "-SWAP") {
					book, err = f.fetchOKX(ctx, strings.TrimSuffix(ox, "-SWAP"), limit)
				}
			case "bybit":
				if bn == "" {
					continue
				}
				book, err = f.fetchBybit(ctx, fapi, bn, limit)
			default:
				continue
			}
			if err != nil {
				last = err
				continue
			}
			if !book.Valid() {
				last = fmt.Errorf("%s empty book", src)
				continue
			}
			return truncBook(book, limit), nil
		}
		if last == nil {
			last = fmt.Errorf("no book source")
		}
		return ExtBook{}, last
	})
}

func (f *bookFeed) cached(ctx context.Context, key string, fetch func() (ExtBook, error)) (ExtBook, error) {
	now := f.now()
	f.mu.Lock()
	if e, ok := f.cache[key]; ok && now.Sub(e.at) < f.ttl {
		f.mu.Unlock()
		return e.book, e.err
	}
	if ch, ok := f.inflight[key]; ok {
		f.mu.Unlock()
		select {
		case <-ch:
		case <-ctx.Done():
			return ExtBook{}, ctx.Err()
		}
		f.mu.Lock()
		e := f.cache[key]
		f.mu.Unlock()
		return e.book, e.err
	}
	ch := make(chan struct{})
	f.inflight[key] = ch
	f.mu.Unlock()

	book, err := fetch()
	f.mu.Lock()
	f.cache[key] = cacheEnt{book: book, err: err, at: f.now()}
	delete(f.inflight, key)
	close(ch)
	f.mu.Unlock()
	return book, err
}

func (f *bookFeed) fetchBinance(ctx context.Context, fapi bool, symbol string, limit int) (ExtBook, error) {
	base, path := f.binanceURL, "/api/v3/depth"
	if fapi {
		base, path = f.fapiURL, "/fapi/v1/depth"
	}
	u := base + path + "?symbol=" + url.QueryEscape(symbol) + "&limit=" + fmt.Sprint(binanceDepthLimit(limit))
	var raw struct {
		Bids [][]string `json:"bids"`
		Asks [][]string `json:"asks"`
	}
	if err := f.getJSON(ctx, u, &raw); err != nil {
		return ExtBook{}, err
	}
	src := "binance"
	if fapi {
		src = "binance-fapi"
	}
	return parseSides(raw.Bids, raw.Asks, src)
}

func (f *bookFeed) fetchOKX(ctx context.Context, inst string, limit int) (ExtBook, error) {
	u := f.okxURL + "/api/v5/market/books?instId=" + url.QueryEscape(inst) + "&sz=" + fmt.Sprint(limit)
	var raw struct {
		Code string `json:"code"`
		Data []struct {
			Bids [][]string `json:"bids"`
			Asks [][]string `json:"asks"`
		} `json:"data"`
	}
	if err := f.getJSON(ctx, u, &raw); err != nil {
		return ExtBook{}, err
	}
	if raw.Code != "" && raw.Code != "0" {
		return ExtBook{}, fmt.Errorf("okx code %s", raw.Code)
	}
	if len(raw.Data) == 0 {
		return ExtBook{}, fmt.Errorf("okx empty")
	}
	b, err := parseSides(raw.Data[0].Bids, raw.Data[0].Asks, "okx")
	if err != nil || !strings.HasSuffix(inst, "-SWAP") {
		return b, err
	}
	// SWAP sizes are contracts, not base units (BTC-USDT-SWAP: 1 contract =
	// 0.01 BTC). The book is returned unchanged (non-depth pairs keep their
	// old sizes); depth-mode pairs convert with ExtBook.baseUnits().
	if cv, cerr := f.okxCtVal(ctx, inst); cerr == nil {
		b.CtVal = cv
	} else {
		b.CtUnknown = true
	}
	return b, nil
}

func scaleBook(b ExtBook, k decimal.Decimal) ExtBook {
	mul := func(in []BookLevel) []BookLevel {
		out := make([]BookLevel, len(in))
		for i, lv := range in {
			out[i] = BookLevel{Price: lv.Price, Qty: lv.Qty.Mul(k)}
		}
		return out
	}
	return ExtBook{Bids: mul(b.Bids), Asks: mul(b.Asks), Source: b.Source}
}

// okxCtVal returns the base units per contract of an OKX SWAP (cached 6h).
func (f *bookFeed) okxCtVal(ctx context.Context, inst string) (decimal.Decimal, error) {
	f.ctMu.Lock()
	if e, ok := f.ctVal[inst]; ok && f.now().Sub(e.at) < 6*time.Hour {
		f.ctMu.Unlock()
		return e.val, nil
	}
	f.ctMu.Unlock()
	var raw struct {
		Code string `json:"code"`
		Data []struct {
			CtVal    string `json:"ctVal"`
			CtValCcy string `json:"ctValCcy"`
		} `json:"data"`
	}
	u := f.okxURL + "/api/v5/public/instruments?instType=SWAP&instId=" + url.QueryEscape(inst)
	if err := f.getJSON(ctx, u, &raw); err != nil {
		return decimal.Zero, fmt.Errorf("okx ctVal %s: %w", inst, err)
	}
	if (raw.Code != "" && raw.Code != "0") || len(raw.Data) == 0 {
		return decimal.Zero, fmt.Errorf("okx ctVal %s: code %s", inst, raw.Code)
	}
	v, err := decimal.NewFromString(raw.Data[0].CtVal)
	if err != nil || !v.IsPositive() {
		return decimal.Zero, fmt.Errorf("okx ctVal %s: bad %q", inst, raw.Data[0].CtVal)
	}
	f.ctMu.Lock()
	f.ctVal[inst] = ctEnt{val: v, at: f.now()}
	f.ctMu.Unlock()
	return v, nil
}

func (f *bookFeed) fetchBybit(ctx context.Context, linear bool, symbol string, limit int) (ExtBook, error) {
	cat := "spot"
	if linear {
		cat = "linear"
	}
	if limit > 50 {
		limit = 50
	}
	u := f.bybitURL + "/v5/market/orderbook?category=" + cat + "&symbol=" + url.QueryEscape(symbol) + "&limit=" + fmt.Sprint(limit)
	var raw struct {
		RetCode int `json:"retCode"`
		Result  struct {
			B [][]string `json:"b"`
			A [][]string `json:"a"`
		} `json:"result"`
	}
	if err := f.getJSON(ctx, u, &raw); err != nil {
		return ExtBook{}, err
	}
	if raw.RetCode != 0 {
		return ExtBook{}, fmt.Errorf("bybit retCode %d", raw.RetCode)
	}
	src := "bybit"
	if linear {
		src = "bybit-linear"
	}
	return parseSides(raw.Result.B, raw.Result.A, src)
}

func parseSides(bids, asks [][]string, src string) (ExtBook, error) {
	parse := func(rows [][]string) []BookLevel {
		out := make([]BookLevel, 0, len(rows))
		for _, row := range rows {
			if len(row) < 2 {
				continue
			}
			px, err1 := decimal.NewFromString(row[0])
			qty, err2 := decimal.NewFromString(row[1])
			if err1 != nil || err2 != nil || !px.IsPositive() || !qty.IsPositive() {
				continue
			}
			out = append(out, BookLevel{Price: px, Qty: qty})
		}
		return out
	}
	b := ExtBook{Bids: parse(bids), Asks: parse(asks), Source: src}
	if !b.Valid() {
		return ExtBook{}, fmt.Errorf("%s crossed or empty", src)
	}
	return b, nil
}

func (f *bookFeed) getJSON(ctx context.Context, u string, v any) error {
	req, err := http.NewRequestWithContext(ctx, http.MethodGet, u, nil)
	if err != nil {
		return err
	}
	req.Header.Set("User-Agent", "linbit-mm/1.0")
	req.Header.Set("Accept", "application/json")
	c := f.client
	if c == nil {
		c = httpc
	}
	resp, err := c.Do(req)
	if err != nil {
		return err
	}
	defer resp.Body.Close()
	if resp.StatusCode/100 != 2 {
		slurp, _ := io.ReadAll(io.LimitReader(resp.Body, 512))
		return fmt.Errorf("HTTP %d %s", resp.StatusCode, strings.TrimSpace(string(slurp)))
	}
	return json.NewDecoder(io.LimitReader(resp.Body, 4<<20)).Decode(v)
}

func (m *Maker) refreshBookRefs(ctx context.Context) {
	if m.books == nil {
		return
	}
	var body struct {
		Items []struct {
			Symbol     string `json:"symbol"`
			RefBinance string `json:"refBinance"`
			RefOKX     string `json:"refOkx"`
		} `json:"items"`
	}
	if err := call(ctx, "GET", m.cfg.MarketURL+"/internal/tradfi/assets", m.internalHdr(), nil, &body); err != nil {
		return
	}
	refs := map[string]bookRef{}
	for _, a := range body.Items {
		if a.Symbol == "" {
			continue
		}
		refs[strings.ToUpper(a.Symbol)] = bookRef{Binance: strings.ToUpper(a.RefBinance), OKX: a.RefOKX}
	}
	m.books.setRefs(refs)
}
