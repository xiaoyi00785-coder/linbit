package main

import (
	"context"
	"log"
	"strings"
	"time"
)

// TradFi feature-flag awareness. market-data still lists TradFi symbols as
// "trading" when the tokenizedStocks/metals flag is off, so the MM must check
// the flag itself and stand down (cancel + no quotes) on gated pairs.

type tradfiGate struct {
	Features       map[string]bool   `json:"features"`
	SpotSymbols    map[string]string `json:"spotSymbols"`
	FuturesSymbols map[string]string `json:"futuresSymbols"`
}

// knownTradFiSpot: seeded TradFi spot pairs (migration 00003). Used to fail
// closed when the gate has never been readable.
var knownTradFiSpot = map[string]string{
	"AAPLUSDT": "tokenizedStocks", "TSLAUSDT": "tokenizedStocks", "NVDAUSDT": "tokenizedStocks",
	"MSFTUSDT": "tokenizedStocks", "GOOGLUSDT": "tokenizedStocks", "AMZNUSDT": "tokenizedStocks",
	"METAUSDT": "tokenizedStocks", "COINUSDT": "tokenizedStocks", "SPYUSDT": "tokenizedStocks",
	"PAXGUSDT": "metals", "XAUTUSDT": "metals",
}

const gateTTL = 5 * time.Second

// refreshGate reloads the gate snapshot (cached gateTTL). On error the last
// snapshot is kept but marked stale; gateBlock then fails closed.
func (m *Maker) refreshGate(ctx context.Context) {
	m.mu.Lock()
	fresh := time.Since(m.gateAt) < gateTTL && m.gateOK
	m.mu.Unlock()
	if fresh {
		return
	}
	var g tradfiGate
	err := call(ctx, "GET", m.cfg.MarketURL+"/internal/tradfi/gate", m.internalHdr(), nil, &g)
	m.mu.Lock()
	defer m.mu.Unlock()
	if err != nil {
		if m.gateOK {
			log.Printf("mm: tradfi gate: %v (failing closed for TradFi pairs)", err)
		}
		m.gateOK = false
		return
	}
	m.gate, m.gateOK, m.gateAt = &g, true, time.Now()
}

// gateBlock returns a non-empty reason when the pair must not be quoted
// because its TradFi feature flag is off (or the flag state is unknown).
func gateBlock(g *tradfiGate, ok bool, market, symbol string) string {
	sym := strings.ToUpper(symbol)
	var flag string
	if g != nil {
		if market == "futures" {
			flag = g.FuturesSymbols[sym]
		} else {
			flag = g.SpotSymbols[sym]
		}
	}
	if flag == "" && market == "spot" {
		flag = knownTradFiSpot[sym]
	}
	if flag == "" {
		return ""
	}
	if !ok || g == nil {
		return "tradfi gate unavailable"
	}
	if !g.Features[flag] {
		return "feature flag off (" + flag + ")"
	}
	return ""
}
