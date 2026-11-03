package main

import (
	"context"
	"encoding/json"
	"fmt"
	"log"
	"net/http"
	"net/url"
	"strconv"
	"strings"
	"time"

	"github.com/shopspring/decimal"
)

// fundingPoint is one historical funding rate from a public source.
type fundingPoint struct {
	At   time.Time
	Rate decimal.Decimal
	Mark decimal.Decimal
}

// BackfillFunding imports public funding-rate history for every contract so
// GET /futures/market/funding-history is populated on a fresh deployment.
// Rows are inserted only strictly before the earliest locally settled row
// (or before now-interval-1h when none exists), and never overwrite, so the
// engine's own settlement (idempotent on (symbol,funding_time)) is unaffected.
// Display data only: no balances move.
func (s *Server) BackfillFunding(ctx context.Context) {
	hc := &http.Client{Timeout: 15 * time.Second}
	since := s.now().Add(-time.Duration(s.cfg.FundingBackfillDays) * 24 * time.Hour)
	for sym := range s.contracts {
		if ctx.Err() != nil {
			return
		}
		n, src, err := s.backfillSymbol(ctx, hc, sym, since)
		if err != nil {
			log.Printf("futures: funding backfill %s: %v", sym, err)
			continue
		}
		if n > 0 {
			log.Printf("futures: funding backfill %s: %d rows from %s", sym, n, src)
		}
	}
}

func (s *Server) backfillSymbol(ctx context.Context, hc *http.Client, sym string, since time.Time) (int, string, error) {
	cutoff := s.now().Add(-s.cfg.FundingInterval - time.Hour)
	var earliest *time.Time
	if err := s.db.QueryRowContext(ctx, `SELECT MIN(funding_time) FROM fut_funding_history WHERE symbol=$1 AND source='settle'`, sym).Scan(&earliest); err != nil {
		return 0, "", err
	}
	if earliest != nil && earliest.Before(cutoff) {
		cutoff = *earliest
	}
	if !since.Before(cutoff) {
		return 0, "", nil
	}
	var lastErr error
	for _, src := range strings.Split(s.cfg.FundingBackfillSources, ",") {
		src = strings.ToLower(strings.TrimSpace(src))
		var pts []fundingPoint
		var err error
		switch src {
		case "binance":
			pts, err = fetchBinanceFunding(ctx, hc, s.cfg.BinanceFAPIURL, sym, since, cutoff)
		case "okx":
			pts, err = fetchOKXFunding(ctx, hc, s.cfg.OKXAPIURL, sym, since, cutoff)
		case "":
			continue
		default:
			err = fmt.Errorf("unknown source %q", src)
		}
		if err != nil || len(pts) == 0 {
			if err == nil {
				err = fmt.Errorf("%s: no data", src)
			}
			lastErr = err
			continue
		}
		n := 0
		step := int64(s.cfg.FundingInterval / time.Second)
		for _, p := range pts {
			// keep only points on this engine's settlement grid, inside (since, cutoff)
			if !p.At.Before(cutoff) || p.At.Before(since) || (step > 0 && p.At.Unix()%step != 0) {
				continue
			}
			res, err := s.db.ExecContext(ctx, `INSERT INTO fut_funding_history (symbol, funding_time, rate, mark_price, source)
				VALUES ($1,$2,$3,$4,$5) ON CONFLICT DO NOTHING`, sym, p.At.UTC(), p.Rate, p.Mark, src)
			if err != nil {
				return n, src, err
			}
			if k, _ := res.RowsAffected(); k > 0 {
				n++
			}
		}
		return n, src, nil
	}
	return 0, "", lastErr
}

func getJSON(ctx context.Context, hc *http.Client, u string, out any) error {
	req, err := http.NewRequestWithContext(ctx, http.MethodGet, u, nil)
	if err != nil {
		return err
	}
	resp, err := hc.Do(req)
	if err != nil {
		return err
	}
	defer resp.Body.Close()
	if resp.StatusCode != http.StatusOK {
		return fmt.Errorf("GET %s: HTTP %d", req.URL.Host+req.URL.Path, resp.StatusCode)
	}
	return json.NewDecoder(resp.Body).Decode(out)
}

// fetchBinanceFunding pages GET /fapi/v1/fundingRate (max 1000 per call).
func fetchBinanceFunding(ctx context.Context, hc *http.Client, base, sym string, since, until time.Time) ([]fundingPoint, error) {
	var out []fundingPoint
	start := since.UnixMilli()
	for i := 0; i < 20 && start < until.UnixMilli(); i++ {
		var rows []struct {
			FundingTime int64  `json:"fundingTime"`
			FundingRate string `json:"fundingRate"`
			MarkPrice   string `json:"markPrice"`
		}
		q := url.Values{"symbol": {sym}, "startTime": {strconv.FormatInt(start, 10)}, "endTime": {strconv.FormatInt(until.UnixMilli(), 10)}, "limit": {"1000"}}
		if err := getJSON(ctx, hc, strings.TrimRight(base, "/")+"/fapi/v1/fundingRate?"+q.Encode(), &rows); err != nil {
			return out, err
		}
		for _, r := range rows {
			rate, err := decimal.NewFromString(r.FundingRate)
			if err != nil {
				continue
			}
			mark, _ := decimal.NewFromString(r.MarkPrice)
			// Binance stamps a few ms after the boundary; snap to the second.
			out = append(out, fundingPoint{At: time.UnixMilli(r.FundingTime).Truncate(time.Second), Rate: rate, Mark: mark})
		}
		if len(rows) < 1000 {
			break
		}
		start = rows[len(rows)-1].FundingTime + 1
	}
	return out, nil
}

// fetchOKXFunding pages GET /api/v5/public/funding-rate-history (100 per call, newest first).
func fetchOKXFunding(ctx context.Context, hc *http.Client, base, sym string, since, until time.Time) ([]fundingPoint, error) {
	if !strings.HasSuffix(sym, "USDT") {
		return nil, fmt.Errorf("okx: unsupported symbol %s", sym)
	}
	inst := strings.TrimSuffix(sym, "USDT") + "-USDT-SWAP"
	var out []fundingPoint
	after := until.UnixMilli()
	for i := 0; i < 50; i++ {
		var body struct {
			Code string `json:"code"`
			Msg  string `json:"msg"`
			Data []struct {
				FundingTime  string `json:"fundingTime"`
				FundingRate  string `json:"fundingRate"`
				RealizedRate string `json:"realizedRate"`
			} `json:"data"`
		}
		q := url.Values{"instId": {inst}, "after": {strconv.FormatInt(after, 10)}, "limit": {"100"}}
		if err := getJSON(ctx, hc, strings.TrimRight(base, "/")+"/api/v5/public/funding-rate-history?"+q.Encode(), &body); err != nil {
			return out, err
		}
		if body.Code != "0" {
			return out, fmt.Errorf("okx: code %s %s", body.Code, body.Msg)
		}
		if len(body.Data) == 0 {
			break
		}
		for _, r := range body.Data {
			ms, err := strconv.ParseInt(r.FundingTime, 10, 64)
			if err != nil {
				continue
			}
			v := r.RealizedRate
			if v == "" {
				v = r.FundingRate
			}
			rate, err := decimal.NewFromString(v)
			if err != nil {
				continue
			}
			out = append(out, fundingPoint{At: time.UnixMilli(ms).Truncate(time.Second), Rate: rate, Mark: decimal.Zero})
			after = ms
		}
		if after <= since.UnixMilli() || len(body.Data) < 100 {
			break
		}
	}
	return out, nil
}
