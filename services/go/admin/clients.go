package main

import (
	"bytes"
	"context"
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"net/url"
	"strings"
	"time"

	"github.com/shopspring/decimal"
)

// upstream is the result of an internal call.
type upstream struct {
	Status int
	Body   []byte
	Header http.Header
}

func (u upstream) ok() bool { return u.Status >= 200 && u.Status < 300 }

// decode unmarshals the body into v.
func (u upstream) decode(v interface{}) error { return json.Unmarshal(u.Body, v) }

// errCode extracts the downstream error code, if any.
func (u upstream) errCode() string {
	var e struct {
		Code  string `json:"code"`
		Error struct {
			Code string `json:"code"`
		} `json:"error"`
	}
	_ = json.Unmarshal(u.Body, &e)
	if e.Code != "" {
		return e.Code
	}
	return e.Error.Code
}

// call performs an internal service request carrying the shared token.
func (s *Server) call(ctx context.Context, method, base, path string, body interface{}) (upstream, error) {
	var rd io.Reader
	if body != nil {
		b, err := json.Marshal(body)
		if err != nil {
			return upstream{}, err
		}
		rd = bytes.NewReader(b)
	}
	req, err := http.NewRequestWithContext(ctx, method, strings.TrimRight(base, "/")+path, rd)
	if err != nil {
		return upstream{}, err
	}
	if body != nil {
		req.Header.Set("Content-Type", "application/json")
	}
	req.Header.Set("X-Internal-Token", s.cfg.InternalToken)
	resp, err := s.http.Do(req)
	if err != nil {
		return upstream{}, err
	}
	defer resp.Body.Close()
	b, err := io.ReadAll(io.LimitReader(resp.Body, 32<<20))
	if err != nil {
		return upstream{}, err
	}
	return upstream{Status: resp.StatusCode, Body: b, Header: resp.Header}, nil
}

// relay writes a downstream response through unchanged.
func relay(w http.ResponseWriter, u upstream) {
	ct := u.Header.Get("Content-Type")
	if ct == "" {
		ct = "application/json"
	}
	w.Header().Set("Content-Type", ct)
	w.WriteHeader(u.Status)
	_, _ = w.Write(u.Body)
}

// passQuery copies the listed query parameters.
func passQuery(r *http.Request, keys ...string) string {
	q := url.Values{}
	for _, k := range keys {
		if v := r.URL.Query().Get(k); v != "" {
			q.Set(k, v)
		}
	}
	if len(q) == 0 {
		return ""
	}
	return "?" + q.Encode()
}

// stablecoins are valued 1:1 in USDT.
var stablecoins = map[string]bool{"USDT": true, "USDC": true}

// usdtValue values amount of asset in USDT via market-data. ok=false when
// there is no usable price (callers fail closed: treat as "needs review").
func (s *Server) usdtValue(ctx context.Context, asset string, amount decimal.Decimal) (decimal.Decimal, bool) {
	asset = strings.ToUpper(asset)
	if stablecoins[asset] {
		return amount, true
	}
	ctx, cancel := context.WithTimeout(ctx, 3*time.Second)
	defer cancel()
	u := fmt.Sprintf("%s/market/index?symbol=%s", strings.TrimRight(s.cfg.MarketURL, "/"), url.QueryEscape(asset+"USDT"))
	req, err := http.NewRequestWithContext(ctx, http.MethodGet, u, nil)
	if err != nil {
		return decimal.Zero, false
	}
	resp, err := s.http.Do(req)
	if err != nil {
		return decimal.Zero, false
	}
	defer resp.Body.Close()
	if resp.StatusCode != http.StatusOK {
		return decimal.Zero, false
	}
	var t struct {
		IndexPrice string `json:"indexPrice"`
	}
	if json.NewDecoder(resp.Body).Decode(&t) != nil {
		return decimal.Zero, false
	}
	p, err := decimal.NewFromString(t.IndexPrice)
	if err != nil || p.Sign() <= 0 {
		return decimal.Zero, false
	}
	return amount.Mul(p), true
}
