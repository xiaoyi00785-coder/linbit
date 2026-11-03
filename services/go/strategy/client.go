package main

import (
	"bytes"
	"context"
	"encoding/json"
	"fmt"
	"net/http"
	"net/url"
	"time"

	"github.com/shopspring/decimal"
)

var httpc = &http.Client{Timeout: 10 * time.Second}

// apiErr is an error reported by a downstream service ({code,message}).
type apiErr struct {
	Status  int
	Code    string `json:"code"`
	Message string `json:"message"`
}

func (e *apiErr) Error() string { return fmt.Sprintf("%d %s: %s", e.Status, e.Code, e.Message) }

func (s *Server) call(ctx context.Context, method, u string, hdr map[string]string, body, out any) error {
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

type SymbolInfo struct {
	Symbol            string          `json:"symbol"`
	Base              string          `json:"base"`
	Quote             string          `json:"quote"`
	PricePrecision    int32           `json:"pricePrecision"`
	QuantityPrecision int32           `json:"quantityPrecision"`
	MinQuantity       decimal.Decimal `json:"minQuantity"`
	MinNotional       decimal.Decimal `json:"minNotional"`
	Status            string          `json:"status"`
}

func (s *Server) symbol(ctx context.Context, sym string) (SymbolInfo, error) {
	var out SymbolInfo
	err := s.call(ctx, "GET", s.cfg.OrderURL+"/internal/symbols/"+url.PathEscape(sym), s.internalHdr(), nil, &out)
	return out, err
}

func (s *Server) internalHdr() map[string]string {
	return map[string]string{"X-Internal-Token": s.cfg.InternalToken}
}

// lastPrice reads the market-data ticker; a zero price is an error.
func (s *Server) lastPrice(ctx context.Context, sym string) (decimal.Decimal, error) {
	var t struct {
		LastPrice string `json:"lastPrice"`
	}
	if err := s.call(ctx, "GET", s.cfg.MarketURL+"/market/ticker?symbol="+url.QueryEscape(sym), nil, nil, &t); err != nil {
		return decimal.Zero, err
	}
	p, err := decimal.NewFromString(t.LastPrice)
	if err != nil || p.Sign() <= 0 {
		return decimal.Zero, fmt.Errorf("no last price for %s", sym)
	}
	return p, nil
}

type placeReq struct {
	Symbol        string  `json:"symbol"`
	Side          string  `json:"side"`
	Type          string  `json:"type"`
	Price         *string `json:"price,omitempty"`
	Quantity      *string `json:"quantity,omitempty"`
	QuoteOrderQty *string `json:"quoteOrderQty,omitempty"`
	ClientOrderID string  `json:"clientOrderId,omitempty"`
}

type orderDTO struct {
	OrderID string `json:"orderId"`
	Status  string `json:"status"`
}

func (s *Server) place(ctx context.Context, uid string, r placeReq) (orderDTO, error) {
	var out orderDTO
	err := s.call(ctx, "POST", s.cfg.OrderURL+"/spot/orders", map[string]string{"X-User-Id": uid}, r, &out)
	return out, err
}

func (s *Server) cancel(ctx context.Context, uid, id string) error {
	return s.call(ctx, "DELETE", s.cfg.OrderURL+"/spot/orders/"+url.PathEscape(id), map[string]string{"X-User-Id": uid}, nil, nil)
}

type orderInfo struct {
	Order struct {
		Status string `json:"status"`
	} `json:"order"`
	FilledQuantity decimal.Decimal `json:"filledQuantity"`
	FilledQuote    decimal.Decimal `json:"filledQuote"`
	BaseFee        decimal.Decimal `json:"baseFee"`
	QuoteFee       decimal.Decimal `json:"quoteFee"`
}

func (o orderInfo) final() bool {
	switch o.Order.Status {
	case "filled", "canceled", "rejected", "expired":
		return true
	}
	return false
}

func (s *Server) orderInfo(ctx context.Context, uid, id string) (orderInfo, error) {
	var out orderInfo
	err := s.call(ctx, "GET", s.cfg.OrderURL+"/internal/orders/"+url.PathEscape(id)+"?userId="+url.QueryEscape(uid), s.internalHdr(), nil, &out)
	return out, err
}

// waitFinal polls a market (IOC) order until the order service reports a final state.
func (s *Server) waitFinal(ctx context.Context, uid, id string) (orderInfo, error) {
	var last orderInfo
	var err error
	for i := 0; i < 50; i++ {
		last, err = s.orderInfo(ctx, uid, id)
		if err == nil && last.final() {
			return last, nil
		}
		time.Sleep(100 * time.Millisecond)
	}
	if err == nil {
		err = fmt.Errorf("order %s not final (status %s)", id, last.Order.Status)
	}
	return last, err
}
