package main

import (
	"context"
	"fmt"
	"log"
	"strings"
	"time"

	"github.com/linbit/platform/services/go/common"
	"github.com/shopspring/decimal"
)

const takerSeedUSDT = "5000000"
const mmFuturesUSDT = "3000000"
const baseSeedUSDT = "300000"

// pickTakes: taker BUYs the best MM ask and SELLs the best MM bid so the
// resting maker quotes fill. Qty is min(level qty, notional/price), stepped.
func pickTakes(targets []Quote, notional decimal.Decimal, r Rules) []Quote {
	var bid, ask Quote
	for _, q := range targets {
		if !q.Price.IsPositive() || !q.Qty.IsPositive() {
			continue
		}
		if q.Side == "buy" && (bid.Price.IsZero() || q.Price.GreaterThan(bid.Price)) {
			bid = q
		}
		if q.Side == "sell" && (ask.Price.IsZero() || q.Price.LessThan(ask.Price)) {
			ask = q
		}
	}
	size := func(px, avail decimal.Decimal) decimal.Decimal {
		q := floorTo(notional.Div(px), r.Step)
		if q.GreaterThan(avail) {
			q = floorTo(avail, r.Step)
		}
		if r.MinQty.IsPositive() && q.LessThan(r.MinQty) {
			return decimal.Zero
		}
		return q
	}
	if !ask.Price.IsPositive() || !bid.Price.IsPositive() {
		return nil
	}
	bq := size(ask.Price, ask.Qty)
	sq := size(bid.Price, bid.Qty)
	q := bq
	if sq.LessThan(q) {
		q = sq
	}
	if !q.IsPositive() {
		return nil
	}
	return []Quote{
		{Side: "buy", Price: ask.Price, Qty: q},
		{Side: "sell", Price: bid.Price, Qty: q},
	}
}

func (m *Maker) takerHdr() map[string]string {
	return map[string]string{"X-User-Id": m.cfg.TakerUserID}
}

func (m *Maker) takePair(ctx context.Context, r Rules, targets []Quote) error {
	if !m.cfg.TakerEnabled || m.cfg.TakerUserID == "" || m.cfg.TakerUserID == m.cfg.UserID {
		return nil
	}
	takes := pickTakes(targets, m.cfg.TakerNotional, r)
	var last error
	for _, q := range takes {
		if err := m.placeTake(ctx, r, q); err != nil {
			last = err
			log.Printf("mm taker %s/%s %s: %v", r.Market, r.Symbol, q.Side, err)
		}
	}
	return last
}

func (m *Maker) placeTake(ctx context.Context, r Rules, q Quote) error {
	price, qty := q.Price.String(), q.Qty.String()
	if r.Market == "spot" {
		return call(ctx, "POST", m.cfg.OrderURL+"/spot/orders", m.takerHdr(), map[string]any{
			"symbol": r.Symbol, "side": q.Side, "type": "limit", "price": price, "quantity": qty, "timeInForce": "IOC"}, nil)
	}
	return call(ctx, "POST", m.cfg.FuturesURL+"/futures/orders", m.takerHdr(), map[string]any{
		"symbol": r.Symbol, "side": q.Side, "type": "limit", "timeInForce": "IOC", "price": price, "qty": qty}, nil)
}

func splitAssets(s string) []string {
	var out []string
	seen := map[string]bool{}
	for _, a := range strings.Split(s, ",") {
		a = strings.ToUpper(strings.TrimSpace(a))
		if a == "" || a == "USDT" || seen[a] {
			continue
		}
		seen[a] = true
		out = append(out, a)
	}
	return out
}

func (m *Maker) ensureInventory(ctx context.Context) {
	m.mu.Lock()
	needSeed := !m.basesSeeded
	m.mu.Unlock()
	if needSeed {
		n, err := m.seedMMBases(ctx)
		if err != nil {
			log.Printf("mm: seed bases: %v", err)
		} else {
			m.mu.Lock()
			m.basesSeeded = true
			m.mu.Unlock()
		}
		if n > 0 {
			m.invalidateSeeds()
		}
	}
	m.mu.Lock()
	if m.invOnce || time.Since(m.invTryAt) < time.Minute {
		// Failed setup is retried once a minute. Cycle now ticks every
		// MM_TICK_MS (pairs run async), and an unconditional retry spammed
		// the asset service twice a second.
		m.mu.Unlock()
		return
	}
	m.invTryAt = time.Now()
	m.mu.Unlock()
	ok := true
	if err := m.seedTaker(ctx); err != nil {
		log.Printf("mm: seed taker: %v", err)
		ok = false
	}
	if err := m.transferUSDT(ctx, m.cfg.UserID, mmFuturesUSDT, "mm-fut-in-v1"); err != nil {
		log.Printf("mm: futures transfer mm: %v", err)
		ok = false
	}
	if m.cfg.TakerEnabled && m.cfg.TakerUserID != "" && m.cfg.TakerUserID != m.cfg.UserID {
		if err := m.transferUSDT(ctx, m.cfg.TakerUserID, "2000000", "taker-fut-in-v1"); err != nil {
			log.Printf("mm: futures transfer taker: %v", err)
			ok = false
		}
	}
	if ok {
		m.mu.Lock()
		m.invOnce = true
		m.mu.Unlock()
	}
}

func (m *Maker) seedMMBases(ctx context.Context) (int, error) {
	bal, err := m.spotBalances(ctx)
	if err != nil {
		return 0, err
	}
	value, err := decimal.NewFromString(firstNonEmpty(m.cfg.SeedBaseUSDT, baseSeedUSDT))
	if err != nil || !value.IsPositive() {
		value = decimal.RequireFromString(baseSeedUSDT)
	}
	maxSeed := decimal.RequireFromString(common.Env("MM_SEED_MAX", "10000000"))
	var missing []string
	n := 0
	for _, a := range splitAssets(m.cfg.SeedBaseAssets) {
		if bal[a].IsPositive() {
			continue
		}
		px, _, err := m.index(ctx, a+"USDT")
		if err != nil || !px.IsPositive() {
			log.Printf("mm: seed %s: no index (%v)", a, err)
			missing = append(missing, a)
			continue
		}
		qty := SeedQty(value, px)
		if maxSeed.IsPositive() && qty.GreaterThan(maxSeed) {
			qty = maxSeed
		}
		if !qty.IsPositive() {
			missing = append(missing, a)
			continue
		}
		var out map[string]any
		ref := "mm_inv_" + strings.ToLower(a) + "_v1"
		err = call(ctx, "POST", m.cfg.AssetURL+"/internal/mm/testnet-seed", m.internalHdr(), map[string]any{
			"userId": m.cfg.UserID, "asset": a, "amount": qty.String(), "ref": ref}, &out)
		if err != nil {
			log.Printf("mm: seed %s %s: %v", qty, a, err)
			missing = append(missing, a)
			continue
		}
		n++
		log.Printf("mm: seeded %s %s to %s", qty, a, m.cfg.UserID)
	}
	if len(missing) > 0 {
		return n, fmt.Errorf("still empty: %s", strings.Join(missing, ","))
	}
	return n, nil
}

func (m *Maker) seedTaker(ctx context.Context) error {
	if !m.cfg.TakerEnabled || m.cfg.TakerUserID == "" || m.cfg.TakerUserID == m.cfg.UserID {
		return nil
	}
	if err := m.adjust(ctx, m.cfg.TakerUserID, "USDT", takerSeedUSDT, "taker-usdt-v1"); err != nil {
		return fmt.Errorf("taker USDT: %w", err)
	}
	value := decimal.RequireFromString(baseSeedUSDT)
	for _, a := range splitAssets(m.cfg.SeedBaseAssets) {
		px, _, err := m.index(ctx, a+"USDT")
		if err != nil || !px.IsPositive() {
			continue
		}
		qty := SeedQty(value, px)
		if !qty.IsPositive() {
			continue
		}
		if err := m.adjust(ctx, m.cfg.TakerUserID, a, qty.String(), "taker-"+strings.ToLower(a)+"-v1"); err != nil {
			log.Printf("mm: taker seed %s: %v", a, err)
		}
	}
	return nil
}

func (m *Maker) adjust(ctx context.Context, userID, asset, amount, ref string) error {
	var out map[string]any
	return call(ctx, "POST", m.cfg.AssetURL+"/internal/admin/adjust", m.internalHdr(), map[string]any{
		"userId": userID, "asset": asset, "account": "spot", "amount": amount, "ref": ref}, &out)
}

func (m *Maker) transferUSDT(ctx context.Context, userID, amount, clientID string) error {
	hdr := map[string]string{"X-User-Id": userID}
	var out map[string]any
	return call(ctx, "POST", m.cfg.FuturesURL+"/assets/transfer", hdr, map[string]any{
		"asset": "USDT", "amount": amount, "from": "spot", "to": "futures", "clientId": clientID}, &out)
}
