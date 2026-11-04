package main

import (
	"context"
	"database/sql"
	"fmt"
	"log"
	"sort"
	"strings"
	"sync"
	"time"

	"github.com/linbit/platform/services/go/common"
	"github.com/shopspring/decimal"
)

const pairCols = `market, symbol, enabled, spread_bps, levels, level_step_bps, level_notional, size_growth, refresh_ms,
	requote_bps, target_base_ratio, max_position, max_skew_bps, leverage, updated_by, updated_at`

func scanPair(sc interface{ Scan(...any) error }) (Pair, error) {
	var p Pair
	var at time.Time
	err := sc.Scan(&p.Market, &p.Symbol, &p.Enabled, &p.SpreadBps, &p.Levels, &p.LevelStepBps, &p.LevelNotional, &p.SizeGrowth,
		&p.RefreshMs, &p.RequoteBps, &p.TargetBaseRatio, &p.MaxPosition, &p.MaxSkewBps, &p.Leverage, &p.UpdatedBy, &at)
	p.UpdatedAt = at.UTC().Format(time.RFC3339)
	return p, err
}

// PairState is the live (in-memory) status of one pair.
type PairState struct {
	Market      string          `json:"market"`
	Symbol      string          `json:"symbol"`
	Status      string          `json:"status"` // quoting | paused | disabled | stopped | error
	Reason      string          `json:"reason,omitempty"`
	Index       decimal.Decimal `json:"indexPrice"`
	Source      string          `json:"source,omitempty"`
	Mid         decimal.Decimal `json:"mid"`
	Inventory   Inventory       `json:"inventory"`
	Quotes      []Quote         `json:"quotes"`
	OpenOrders  int             `json:"openOrders"`
	Placed      int64           `json:"placed"`
	Cancelled   int64           `json:"cancelled"`
	LastRun     time.Time       `json:"lastRun"`
	LastError   string          `json:"lastError,omitempty"`
	leverageSet int
	hasOrders   bool
}

// Maker runs the quoting loop.
type Maker struct {
	cfg Config
	db  *sql.DB

	mu        sync.Mutex
	rules     map[string]Rules // key market/symbol
	rulesAt   time.Time
	state     map[string]*PairState
	running   bool
	wasRun    bool
	feedDown  map[string]bool
	lastCycle time.Time
	alert     func(string)
	seedCache map[string]decimal.Decimal
	seedAt    time.Time
	gate      *tradfiGate
	gateOK    bool
	gateAt    time.Time
}

func key(market, sym string) string { return market + "/" + sym }

func NewMaker(cfg Config, db *sql.DB) *Maker {
	return &Maker{cfg: cfg, db: db, rules: map[string]Rules{}, state: map[string]*PairState{}, feedDown: map[string]bool{},
		alert: common.AlertTelegram}
}

func (m *Maker) Pairs(ctx context.Context) ([]Pair, error) {
	rows, err := m.db.QueryContext(ctx, `SELECT `+pairCols+` FROM mm_pairs ORDER BY market, symbol`)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	var out []Pair
	for rows.Next() {
		p, err := scanPair(rows)
		if err != nil {
			return nil, err
		}
		out = append(out, p)
	}
	return out, rows.Err()
}

func (m *Maker) isRunning(ctx context.Context) (bool, error) {
	var r bool
	if !m.cfg.Enabled {
		return false, nil // MM_ENABLED=false: never quote, start is refused
	}
	err := m.db.QueryRowContext(ctx, `SELECT running FROM mm_state WHERE id=1`).Scan(&r)
	return r, err
}

// refreshRules rediscovers pairs every minute and registers new ones with defaults.
func (m *Maker) refreshRules(ctx context.Context) {
	if time.Since(m.rulesAt) < time.Minute && len(m.rules) > 0 {
		return
	}
	next := map[string]Rules{}
	sp, err1 := m.spotRules(ctx)
	fu, err2 := m.futuresRules(ctx)
	if err1 != nil {
		log.Printf("mm: spot symbols: %v", err1)
	}
	if err2 != nil {
		log.Printf("mm: futures contracts: %v", err2)
	}
	for _, r := range append(sp, fu...) {
		next[key(r.Market, r.Symbol)] = r
		if _, err := m.db.ExecContext(ctx, `INSERT INTO mm_pairs (market, symbol) VALUES ($1,$2) ON CONFLICT DO NOTHING`, r.Market, r.Symbol); err != nil {
			log.Printf("mm: register %s/%s: %v", r.Market, r.Symbol, err)
		}
	}
	m.mu.Lock()
	// keep previously known rules when one side failed to load
	if err1 != nil || err2 != nil {
		for k, v := range m.rules {
			if _, ok := next[k]; !ok {
				next[k] = v
			}
		}
	}
	m.rules = next
	m.mu.Unlock()
	if err1 == nil && err2 == nil {
		m.rulesAt = time.Now()
	}
}

// Run is the main scheduler loop.
func (m *Maker) Run(ctx context.Context) {
	t := time.NewTicker(m.cfg.Tick)
	defer t.Stop()
	for {
		select {
		case <-ctx.Done():
			return
		case <-t.C:
			m.Cycle(ctx)
		}
	}
}

// Cycle runs one scheduler pass: every due pair is refreshed.
func (m *Maker) Cycle(ctx context.Context) {
	running, err := m.isRunning(ctx)
	if err != nil {
		log.Printf("mm: state: %v", err)
		return
	}
	m.refreshRules(ctx)
	m.refreshGate(ctx)
	m.mu.Lock()
	gate, gateOK := m.gate, m.gateOK
	m.mu.Unlock()
	pairs, err := m.Pairs(ctx)
	if err != nil {
		log.Printf("mm: pairs: %v", err)
		return
	}
	m.mu.Lock()
	stopping := m.wasRun && !running
	starting := running && !m.wasRun
	m.running, m.wasRun = running, running
	m.lastCycle = time.Now()
	m.mu.Unlock()

	if starting {
		m.captureStartBaselines(ctx)
	}
	var balances map[string]decimal.Decimal
	var baselines map[string]Baseline
	var positions map[string]decimal.Decimal
	var newlyDown, recovered []string
	for _, p := range pairs {
		m.mu.Lock()
		r, known := m.rules[key(p.Market, p.Symbol)]
		st := m.state[key(p.Market, p.Symbol)]
		if st == nil {
			st = &PairState{Market: p.Market, Symbol: p.Symbol, hasOrders: true}
			m.state[key(p.Market, p.Symbol)] = st
		}
		m.mu.Unlock()
		if !known {
			continue
		}
		blocked := gateBlock(gate, gateOK, p.Market, p.Symbol)
		if !running || !p.Enabled || !r.Trading || blocked != "" {
			reason := "stopped"
			if running && blocked != "" {
				reason = blocked // flag state wins: admin must see why it's off
			} else if running && !p.Enabled {
				reason = "disabled"
			} else if running {
				reason = "symbol not trading"
			}
			if st.hasOrders || stopping {
				m.cancelAll(ctx, r, st)
			}
			m.setStatus(st, map[bool]string{true: "disabled", false: "stopped"}[running], reason)
			continue
		}
		if time.Since(st.LastRun) < time.Duration(p.RefreshMs)*time.Millisecond {
			continue
		}
		st.LastRun = time.Now()
		idxSym := r.Base + r.Quote
		if idxSym == "" {
			idxSym = r.Symbol
		}
		index, src, widen, err := m.indexWiden(ctx, idxSym)
		if err != nil {
			// fail closed: no external price -> no quotes
			m.cancelAll(ctx, r, st)
			m.setStatus(st, "paused", "index unavailable")
			m.mu.Lock()
			if !m.feedDown[key(p.Market, p.Symbol)] {
				m.feedDown[key(p.Market, p.Symbol)] = true
				newlyDown = append(newlyDown, key(p.Market, p.Symbol))
			}
			m.mu.Unlock()
			continue
		}
		m.mu.Lock()
		if m.feedDown[key(p.Market, p.Symbol)] {
			delete(m.feedDown, key(p.Market, p.Symbol))
			recovered = append(recovered, key(p.Market, p.Symbol))
		}
		m.mu.Unlock()

		var inv Inventory
		if p.Market == "spot" {
			if balances == nil {
				if balances, err = m.spotBalances(ctx); err != nil {
					m.fail(st, "balances: "+err.Error())
					balances = nil
					continue
				}
			}
			if baselines == nil {
				if baselines, err = m.Baselines(ctx); err != nil {
					// fail closed: unknown baseline -> no quotes this pass
					m.fail(st, "baseline: "+err.Error())
					baselines = nil
					continue
				}
			}
			inv = spotInventory(p, index, balances[strings.ToUpper(r.Base)], balances[strings.ToUpper(r.Quote)], baselines[strings.ToUpper(r.Base)])
			if reason := oneSidedReason(inv); reason != "" {
				// No base inventory and nothing seeded: quoting bids only would
				// just build an unhedged long book. Stand down until seeded.
				m.cancelAll(ctx, r, st)
				m.mu.Lock()
				st.Inventory = inv
				m.mu.Unlock()
				m.setStatus(st, "paused", reason)
				continue
			}
		} else {
			if positions == nil {
				if positions, err = m.futuresPositions(ctx); err != nil {
					m.fail(st, "positions: "+err.Error())
					positions = nil
					continue
				}
			}
			if st.leverageSet != p.Leverage {
				if err := m.setLeverage(ctx, r.Symbol, p.Leverage); err != nil {
					log.Printf("mm: leverage %s: %v", r.Symbol, err)
				}
				st.leverageSet = p.Leverage
			}
			inv = futuresInventory(p, index, positions[r.Symbol])
		}
		widened := widen.GreaterThan(decimal.NewFromInt(1))
		if widened {
			// TradFi spot deviates from the reference: quote wider
			p.SpreadBps = p.SpreadBps.Mul(widen)
		}
		mid, targets := BuildLadder(p, r, index, inv)
		resting, err := m.openOrders(ctx, r)
		if err != nil {
			m.fail(st, "open orders: "+err.Error())
			continue
		}
		cancel, place := Diff(resting, targets, p.RequoteBps)
		var lastErr string
		for _, o := range cancel {
			if err := m.cancel(ctx, r.Market, o.ID); err != nil {
				lastErr = "cancel: " + err.Error()
			} else {
				st.Cancelled++
			}
		}
		for _, q := range place {
			if err := m.place(ctx, r, q); err != nil {
				lastErr = fmt.Sprintf("place %s L%d: %v", q.Side, q.Level, err)
			} else {
				st.Placed++
			}
		}
		m.mu.Lock()
		st.Index, st.Source, st.Mid, st.Inventory, st.Quotes = index, src, mid, inv, targets
		st.OpenOrders = len(resting) - len(cancel) + len(place)
		st.hasOrders = true
		st.Status, st.Reason, st.LastError = "quoting", "", lastErr
		if widened {
			st.Reason = "spread widened x" + widen.String() + " (reference deviation)"
		}
		m.mu.Unlock()
	}
	if len(newlyDown) > 0 {
		sort.Strings(newlyDown)
		m.alert(fmt.Sprintf("⚠️ Linbit 做市暂停：外部指数价格不可用，已撤销报价。交易对：%s", strings.Join(newlyDown, ", ")))
	}
	if len(recovered) > 0 {
		sort.Strings(recovered)
		m.alert(fmt.Sprintf("✅ Linbit 做市恢复：外部指数价格已恢复。交易对：%s", strings.Join(recovered, ", ")))
	}
}

func (m *Maker) setStatus(st *PairState, status, reason string) {
	m.mu.Lock()
	st.Status, st.Reason, st.Quotes = status, reason, nil
	m.mu.Unlock()
}

func (m *Maker) fail(st *PairState, msg string) {
	log.Printf("mm: %s/%s: %s", st.Market, st.Symbol, msg)
	m.mu.Lock()
	st.Status, st.LastError = "error", msg
	m.mu.Unlock()
}

// cancelAll cancels every open MM order on one pair.
func (m *Maker) cancelAll(ctx context.Context, r Rules, st *PairState) {
	resting, err := m.openOrders(ctx, r)
	if err != nil {
		log.Printf("mm: cancelAll %s/%s: %v", r.Market, r.Symbol, err)
		return
	}
	ok := true
	for _, o := range resting {
		if err := m.cancel(ctx, r.Market, o.ID); err != nil {
			ok = false
			log.Printf("mm: cancel %s/%s %s: %v", r.Market, r.Symbol, o.ID, err)
		} else {
			st.Cancelled++
		}
	}
	m.mu.Lock()
	st.hasOrders = !ok
	st.OpenOrders = 0
	m.mu.Unlock()
}

// CancelEverything is used by stop: cancels all pairs synchronously.
func (m *Maker) CancelEverything(ctx context.Context) {
	m.mu.Lock()
	rules := make([]Rules, 0, len(m.rules))
	for _, r := range m.rules {
		rules = append(rules, r)
	}
	m.mu.Unlock()
	for _, r := range rules {
		m.mu.Lock()
		st := m.state[key(r.Market, r.Symbol)]
		if st == nil {
			st = &PairState{Market: r.Market, Symbol: r.Symbol}
			m.state[key(r.Market, r.Symbol)] = st
		}
		m.mu.Unlock()
		m.cancelAll(ctx, r, st)
		m.setStatus(st, "stopped", "stopped")
	}
}
