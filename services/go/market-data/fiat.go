package main

import (
	"context"
	"encoding/json"
	"log"
	"net/http"
	"net/url"
	"strings"
	"sync"
	"time"

	"github.com/linbit/platform/services/go/common"
	"github.com/shopspring/decimal"
)

// GET /market/fiat-rates?base=USDT — fiat-per-USD rates from a live FX source.
// rates are per 1 USD, not per 1 USDT. USDT is not pegged to USD here.
// Sources (FIAT_RATES_URLS) are tried in order and cached 10 minutes. If every
// source fails we keep the last good value (stale:true). If none has ever
// succeeded, rates is null and available is false. There is no static table.

const fiatTTL = 10 * time.Minute

type fiatSnap struct {
	Rates     map[string]float64
	Source    string
	UpdatedAt time.Time
}

var fiatCache struct {
	mu      sync.Mutex
	good    *fiatSnap
	checked time.Time
}

// parseFiat accepts open.er-api.com ({result,rates,time_last_update_unix}) and
// frankfurter ({base,date,rates}) shapes; USD is always 1.
func parseFiat(b []byte) (map[string]float64, time.Time, bool) {
	var v struct {
		Result string             `json:"result"`
		Rates  map[string]float64 `json:"rates"`
		Unix   int64              `json:"time_last_update_unix"`
		Date   string             `json:"date"`
	}
	if json.Unmarshal(b, &v) != nil || len(v.Rates) < 3 || (v.Result != "" && v.Result != "success") {
		return nil, time.Time{}, false
	}
	for k, x := range v.Rates {
		if x <= 0 || len(k) != 3 {
			delete(v.Rates, k)
		}
	}
	v.Rates["USD"] = 1
	at := time.Now().UTC()
	if v.Unix > 0 {
		at = time.Unix(v.Unix, 0).UTC()
	} else if d, err := time.Parse("2006-01-02", v.Date); err == nil {
		at = d
	}
	return v.Rates, at, true
}

func (s *Server) fiatRates(ctx context.Context) (fiatSnap, bool) {
	fiatCache.mu.Lock()
	defer fiatCache.mu.Unlock()
	if fiatCache.good != nil && time.Since(fiatCache.checked) < fiatTTL {
		return *fiatCache.good, false
	}
	// Even on failure, back off for a minute rather than hammering sources.
	if fiatCache.good == nil || time.Since(fiatCache.checked) >= time.Minute {
		for i, u := range s.cfg.FiatRatesURLs {
			c, cancel := context.WithTimeout(ctx, 3*time.Second)
			req, _ := http.NewRequestWithContext(c, "GET", u, nil)
			resp, err := http.DefaultClient.Do(req)
			if err == nil {
				var raw json.RawMessage
				if resp.StatusCode == 200 && json.NewDecoder(resp.Body).Decode(&raw) == nil {
					if rates, at, ok := parseFiat(raw); ok {
						resp.Body.Close()
						cancel()
						host := u
						if p, err := url.Parse(u); err == nil {
							host = p.Host
						}
						fiatCache.good = &fiatSnap{Rates: rates, Source: host, UpdatedAt: at}
						fiatCache.checked = time.Now()
						return *fiatCache.good, false
					}
				}
				resp.Body.Close()
			}
			cancel()
			log.Printf("fiat-rates: source #%d failed", i+1)
		}
		fiatCache.checked = time.Now()
	}
	if fiatCache.good != nil {
		return *fiatCache.good, true
	}
	return fiatSnap{}, true
}

func (s *Server) handleFiatRates(w http.ResponseWriter, r *http.Request) {
	base := strings.ToUpper(strings.TrimSpace(r.URL.Query().Get("base")))
	if base == "" {
		base = "USDT"
	}
	if base != "USDT" && base != "USDC" && base != "USD" {
		common.WriteError(w, http.StatusBadRequest, "VALIDATION_ERROR", "base 仅支持 USDT/USDC/USD")
		return
	}
	snap, stale := s.fiatRates(r.Context())
	var updated any
	available := len(snap.Rates) > 0
	if !snap.UpdatedAt.IsZero() {
		updated = snap.UpdatedAt
	}
	usdt := s.currentUSDTUSD(r.Context())
	var usdtRate any
	var usdtAt any
	if usdt.OK {
		usdtRate = usdt.Rate.StringFixed(usdPrecision)
		usdtAt = usdt.UpdatedAt.UTC()
	}
	var perUsdt any
	if available && usdt.OK {
		m := map[string]string{}
		for k, v := range snap.Rates {
			if v <= 0 {
				continue
			}
			m[k] = decimal.NewFromFloat(v).Mul(usdt.Rate).Round(usdPrecision).StringFixed(usdPrecision)
		}
		perUsdt = m
	}
	w.Header().Set("Cache-Control", "public, max-age=60")
	common.WriteJSON(w, http.StatusOK, map[string]any{
		"base": base, "pegAssumed": false, "ratesBasis": "USD", "available": available,
		"rates": snap.Rates, "source": snap.Source, "updatedAt": updated, "stale": stale && available,
		"usdtUsd":      map[string]any{"available": usdt.OK, "rate": usdtRate, "source": usdt.Source, "updatedAt": usdtAt, "stale": usdt.Stale, "precision": usdPrecision, "pegAssumed": false},
		"quotePerUsdt": perUsdt,
	})
}
