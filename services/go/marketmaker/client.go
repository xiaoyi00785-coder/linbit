package main

import (
	"bytes"
	"context"
	"encoding/json"
	"fmt"
	"net/http"
	"net/url"
	"strings"
	"time"

	"github.com/shopspring/decimal"
)

var httpc = &http.Client{Timeout: 8 * time.Second}

type apiErr struct {
	Status  int
	Code    string `json:"code"`
	Message string `json:"message"`
}

func (e *apiErr) Error() string { return fmt.Sprintf("%d %s: %s", e.Status, e.Code, e.Message) }

func call(ctx context.Context, method, u string, hdr map[string]string, body, out any) error {
	var rd *bytes.Reader
	if body != nil {
		b, _ := json.Marshal(body)
		rd = bytes.NewReader(b)
	} else {
		rd = bytes.NewReader(nil)
	}
	req, err := http.NewRequestWithContext(ctx, method, u, rd)
	if err != nil {
		return err
	}
	req.Header.Set("Content-Type", "application/json")
	for k, v := range hdr {
		req.Header.Set(k, v)
	}
	resp, err := httpc.Do(req)
	if err != nil {
		return err
	}
	defer resp.Body.Close()
	if resp.StatusCode/100 != 2 {
		e := &apiErr{Status: resp.StatusCode}
		_ = json.NewDecoder(resp.Body).Decode(e)
		return e
	}
	if out != nil {
		return json.NewDecoder(resp.Body).Decode(out)
	}
	return nil
}

func (m *Maker) userHdr() map[string]string { return map[string]string{"X-User-Id": m.cfg.UserID} }
func (m *Maker) internalHdr() map[string]string {
	return map[string]string{"X-Internal-Token": m.cfg.InternalToken}
}

// index reads the external index price; any failure means "unavailable".
func (m *Maker) index(ctx context.Context, sym string) (decimal.Decimal, string, error) {
	p, src, _, err := m.indexWiden(ctx, sym)
	return p, src, err
}

// indexWiden also returns the spread multiplier market-data reports for
// TradFi symbols whose spot price deviates from the reference (1 otherwise).
func (m *Maker) indexWiden(ctx context.Context, sym string) (decimal.Decimal, string, decimal.Decimal, error) {
	var t struct {
		IndexPrice  string `json:"indexPrice"`
		Source      string `json:"source"`
		WidenFactor string `json:"widenFactor"`
	}
	one := decimal.NewFromInt(1)
	if err := call(ctx, "GET", m.cfg.MarketURL+"/market/index?symbol="+url.QueryEscape(sym), nil, nil, &t); err != nil {
		return decimal.Zero, "", one, err
	}
	p, err := decimal.NewFromString(t.IndexPrice)
	if err != nil || !p.IsPositive() {
		return decimal.Zero, "", one, fmt.Errorf("bad index for %s", sym)
	}
	w, err := decimal.NewFromString(t.WidenFactor)
	if err != nil || w.LessThan(one) {
		w = one
	}
	if w.GreaterThan(decimal.NewFromInt(20)) {
		w = decimal.NewFromInt(20)
	}
	return p, t.Source, w, nil
}

// Market rules for one tradable pair.
type Rules struct {
	Market      string
	Symbol      string
	Base, Quote string
	Tick        decimal.Decimal
	Step        decimal.Decimal
	MinQty      decimal.Decimal
	MinNotional decimal.Decimal
	Trading     bool
}

func pow10neg(n int32) decimal.Decimal { return decimal.New(1, -n) }

// spotRules lists spot pairs from market-data, then reads minNotional from the
// order service.
func (m *Maker) spotRules(ctx context.Context) ([]Rules, error) {
	var syms []struct {
		Symbol            string `json:"symbol"`
		Base              string `json:"base"`
		Quote             string `json:"quote"`
		PricePrecision    int32  `json:"pricePrecision"`
		QuantityPrecision int32  `json:"quantityPrecision"`
		MinQuantity       string `json:"minQuantity"`
		Status            string `json:"status"`
	}
	if err := call(ctx, "GET", m.cfg.MarketURL+"/market/symbols", nil, nil, &syms); err != nil {
		return nil, err
	}
	out := make([]Rules, 0, len(syms))
	for _, s := range syms {
		r := Rules{Market: "spot", Symbol: s.Symbol, Base: s.Base, Quote: s.Quote, Tick: pow10neg(s.PricePrecision),
			Step: pow10neg(s.QuantityPrecision), Trading: s.Status == "trading"}
		r.MinQty, _ = decimal.NewFromString(s.MinQuantity)
		var info struct {
			MinNotional decimal.Decimal `json:"minNotional"`
		}
		if err := call(ctx, "GET", m.cfg.OrderURL+"/internal/symbols/"+url.PathEscape(s.Symbol), m.internalHdr(), nil, &info); err == nil {
			r.MinNotional = info.MinNotional
		}
		out = append(out, r)
	}
	return out, nil
}

func (m *Maker) futuresRules(ctx context.Context) ([]Rules, error) {
	var body struct {
		Contracts []struct {
			Symbol string          `json:"symbol"`
			Base   string          `json:"baseAsset"`
			Quote  string          `json:"quoteAsset"`
			Tick   decimal.Decimal `json:"tickSize"`
			MinQty decimal.Decimal `json:"minQty"`
			Status string          `json:"status"`
		} `json:"contracts"`
	}
	if err := call(ctx, "GET", m.cfg.FuturesURL+"/futures/market/contracts", nil, nil, &body); err != nil {
		return nil, err
	}
	out := make([]Rules, 0, len(body.Contracts))
	for _, c := range body.Contracts {
		out = append(out, Rules{Market: "futures", Symbol: c.Symbol, Base: c.Base, Quote: c.Quote, Tick: c.Tick,
			Step: c.MinQty, MinQty: c.MinQty, Trading: c.Status == "" || c.Status == "trading"})
	}
	return out, nil
}

// restingOrder is one of our open orders.
type restingOrder struct {
	ID        string
	Side      string
	Price     decimal.Decimal
	Remaining decimal.Decimal
}

func (m *Maker) openOrders(ctx context.Context, r Rules) ([]restingOrder, error) {
	var out []restingOrder
	if r.Market == "spot" {
		var body struct {
			Items []struct {
				OrderID        string  `json:"orderId"`
				Side           string  `json:"side"`
				Price          *string `json:"price"`
				Quantity       string  `json:"quantity"`
				FilledQuantity string  `json:"filledQuantity"`
			} `json:"items"`
		}
		if err := call(ctx, "GET", m.cfg.OrderURL+"/spot/orders/open?pageSize=100&symbol="+url.QueryEscape(r.Symbol), m.userHdr(), nil, &body); err != nil {
			return nil, err
		}
		for _, o := range body.Items {
			if o.Price == nil {
				continue
			}
			p, _ := decimal.NewFromString(*o.Price)
			q, _ := decimal.NewFromString(o.Quantity)
			f, _ := decimal.NewFromString(o.FilledQuantity)
			out = append(out, restingOrder{ID: o.OrderID, Side: strings.ToLower(o.Side), Price: p, Remaining: q.Sub(f)})
		}
		return out, nil
	}
	var body struct {
		Orders []struct {
			OrderID int64               `json:"orderId"`
			Side    string              `json:"side"`
			Price   decimal.NullDecimal `json:"price"`
			Qty     decimal.Decimal     `json:"qty"`
			Filled  decimal.Decimal     `json:"filledQty"`
		} `json:"orders"`
	}
	if err := call(ctx, "GET", m.cfg.FuturesURL+"/futures/orders?status=open&limit=500&symbol="+url.QueryEscape(r.Symbol), m.userHdr(), nil, &body); err != nil {
		return nil, err
	}
	for _, o := range body.Orders {
		if !o.Price.Valid {
			continue
		}
		out = append(out, restingOrder{ID: fmt.Sprint(o.OrderID), Side: o.Side, Price: o.Price.Decimal, Remaining: o.Qty.Sub(o.Filled)})
	}
	return out, nil
}

func (m *Maker) cancel(ctx context.Context, market, id string) error {
	if market == "spot" {
		return call(ctx, "DELETE", m.cfg.OrderURL+"/spot/orders/"+url.PathEscape(id), m.userHdr(), nil, nil)
	}
	return call(ctx, "DELETE", m.cfg.FuturesURL+"/futures/orders/"+url.PathEscape(id), m.userHdr(), nil, nil)
}

func (m *Maker) place(ctx context.Context, r Rules, q Quote) error {
	price, qty := q.Price.String(), q.Qty.String()
	if r.Market == "spot" {
		tif := "PostOnly"
		return call(ctx, "POST", m.cfg.OrderURL+"/spot/orders", m.userHdr(), map[string]any{
			"symbol": r.Symbol, "side": q.Side, "type": "limit", "price": price, "quantity": qty, "timeInForce": tif}, nil)
	}
	return call(ctx, "POST", m.cfg.FuturesURL+"/futures/orders", m.userHdr(), map[string]any{
		"symbol": r.Symbol, "side": q.Side, "type": "limit", "timeInForce": "GTC", "price": price, "qty": qty}, nil)
}

func (m *Maker) setLeverage(ctx context.Context, sym string, lev int) error {
	return call(ctx, "POST", m.cfg.FuturesURL+"/futures/leverage", m.userHdr(), map[string]any{
		"symbol": sym, "leverage": lev, "marginMode": "cross"}, nil)
}

// spotBalances returns total (free+locked) per asset of the MM account.
func (m *Maker) spotBalances(ctx context.Context) (map[string]decimal.Decimal, error) {
	var body struct {
		Items []struct {
			Asset string `json:"asset"`
			Free  string `json:"free"`
			Total string `json:"total"`
		} `json:"items"`
	}
	if err := call(ctx, "GET", m.cfg.AssetURL+"/internal/balances?userId="+url.QueryEscape(m.cfg.UserID), m.internalHdr(), nil, &body); err != nil {
		return nil, err
	}
	out := map[string]decimal.Decimal{}
	for _, b := range body.Items {
		v, _ := decimal.NewFromString(b.Total)
		out[strings.ToUpper(b.Asset)] = v
	}
	return out, nil
}

// futuresPositions returns the signed position qty per symbol.
func (m *Maker) futuresPositions(ctx context.Context) (map[string]decimal.Decimal, error) {
	var body struct {
		Positions []struct {
			Symbol string `json:"symbol"`
			Side   string `json:"side"`
			Qty    string `json:"qty"`
		} `json:"positions"`
	}
	if err := call(ctx, "GET", m.cfg.FuturesURL+"/futures/positions", m.userHdr(), nil, &body); err != nil {
		return nil, err
	}
	out := map[string]decimal.Decimal{}
	for _, p := range body.Positions {
		q, _ := decimal.NewFromString(p.Qty)
		if strings.EqualFold(p.Side, "short") && q.IsPositive() {
			q = q.Neg()
		}
		out[p.Symbol] = q
	}
	return out, nil
}
