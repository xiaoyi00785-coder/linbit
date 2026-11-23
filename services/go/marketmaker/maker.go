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
	requote_bps, target_base_ratio, max_position, max_skew_bps, leverage, depth_target_qty, depth_band_bps, updated_by, updated_at`

func scanPair(sc interface{ Scan(...any) error }) (Pair, error) {
	var p Pair
	var at time.Time
	err := sc.Scan(&p.Market, &p.Symbol, &p.Enabled, &p.SpreadBps, &p.Levels, &p.LevelStepBps, &p.LevelNotional, &p.SizeGrowth,
		&p.RefreshMs, &p.RequoteBps, &p.TargetBaseRatio, &p.MaxPosition, &p.MaxSkewBps, &p.Leverage, &p.DepthTargetQty, &p.DepthBandBps, &p.UpdatedBy, &at)
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
	BookSource  string          `json:"bookSource,omitempty"`
	Mid         decimal.Decimal `json:"mid"`
	Inventory   Inventory       `json:"inventory"`
	Quotes      []Quote         `json:"quotes"`
	OpenOrders  int             `json:"openOrders"`
	Placed      int64           `json:"placed"`
	Cancelled   int64           `json:"cancelled"`
	Refills     int64           `json:"refills"` // depth mode: re-quotes triggered by fills before refresh_ms
	Amended     int64           `json:"amended"`
	LastRun     time.Time       `json:"lastRun"`
	LastError   string          `json:"lastError,omitempty"`
	leverageSet int
	hasOrders   bool
	lastTake    time.Time
}

// Maker runs the quoting loop.
type Maker struct {
	cfg Config
	db  *sql.DB

	mu          sync.Mutex
	rules       map[string]Rules // key market/symbol
	rulesAt     time.Time
	state       map[string]*PairState
	running     bool
	wasRun      bool
	feedDown    map[string]bool
	lastCycle   time.Time
	alert       func(string)
	seedCache   map[string]decimal.Decimal
	seedAt      time.Time
	seedFailAt  time.Time
	gate        *tradfiGate
	gateOK      bool
	gateAt      time.Time
	invOnce     bool
	invTryAt    time.Time
	basesSeeded bool
	books       *bookFeed
	refsAt      time.Time

	// async scheduler (see runPair)
	wg       sync.WaitGroup
	sem      chan struct{}
	inflight map[string]bool
	pendDown []string
	pendUp   []string
	alertAt  time.Time

	cacheMu sync.Mutex
	balAt   time.Time
	bal     map[string]decimal.Decimal
	baseAt  time.Time
	base    map[string]Baseline
	posAt   time.Time
	pos     map[string]decimal.Decimal
}

func key(market, sym string) string { return market + "/" + sym }

func NewMaker(cfg Config, db *sql.DB) *Maker {
	return &Maker{cfg: cfg, db: db, rules: map[string]Rules{}, state: map[string]*PairState{}, feedDown: map[string]bool{},
		alert: common.AlertTelegram, books: newBookFeed(cfg),
		sem: make(chan struct{}, cfg.Workers), inflight: map[string]bool{}}
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
	if time.Since(m.refsAt) > 5*time.Minute {
		m.refreshBookRefs(ctx)
		m.refsAt = time.Now()
	}
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
	if running {
		m.ensureInventory(ctx)
	}
	if stopping {
		// stop: wait for in-flight pairs so nothing places after cancel.
		m.wg.Wait()
	}
	for _, p := range pairs {
		k := key(p.Market, p.Symbol)
		m.mu.Lock()
		busy := m.inflight[k]
		if !busy {
			m.inflight[k] = true
		}
		m.mu.Unlock()
		if busy {
			continue // still working on the previous pass; never stack
		}
		m.wg.Add(1)
		go func(p Pair, k string) {
			defer m.wg.Done()
			m.sem <- struct{}{}
			defer func() {
				<-m.sem
				m.mu.Lock()
				delete(m.inflight, k)
				m.mu.Unlock()
			}()
			m.runPair(ctx, p, running, stopping, gate, gateOK)
		}(p, k)
	}
	m.flushAlerts(false)
}

// runPair is one pass over one pair. Pairs run concurrently (MM_WORKERS);
// a pair never overlaps itself (inflight). Before this, Cycle walked every
// pair in sequence with ~80 HTTP order calls each, so one pass took ~100s
// and BTC spot quotes and last price trailed the market by ~200 USD.
func (m *Maker) runPair(ctx context.Context, p Pair, running, stopping bool, gate *tradfiGate, gateOK bool) {
	m.mu.Lock()
	r, known := m.rules[key(p.Market, p.Symbol)]
	st := m.state[key(p.Market, p.Symbol)]
	if st == nil {
		st = &PairState{Market: p.Market, Symbol: p.Symbol, hasOrders: true}
		m.state[key(p.Market, p.Symbol)] = st
	}
	m.mu.Unlock()
	if !known {
		return
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
		return
	}
	takeEvery := m.cfg.TakerInterval
	if !m.cfg.followsBook(p.Market, r.Symbol) && m.cfg.TakerLightInterval > takeEvery {
		takeEvery = m.cfg.TakerLightInterval // light pairs: occasional prints, not 1.25 orders/s each
	}
	dueTake := m.cfg.TakerEnabled && time.Since(st.lastTake) >= takeEvery
	dueQuote := time.Since(st.LastRun) >= time.Duration(p.RefreshMs)*time.Millisecond
	if !dueQuote && depthMode(p) && time.Since(st.LastRun) >= m.cfg.RefillMin {
		// fast refill: a large taker emptied levels; don't wait for refresh_ms
		m.mu.Lock()
		quotes := append([]Quote(nil), st.Quotes...)
		quoting := st.Status == "quoting"
		m.mu.Unlock()
		if quoting && len(quotes) > 0 {
			if resting, err := m.openOrders(ctx, r); err == nil && needsRefill(resting, quotes) {
				dueQuote = true
				m.mu.Lock()
				st.Refills++
				m.mu.Unlock()
			}
		}
	}
	if !dueQuote {
		if dueTake {
			m.mu.Lock()
			quotes := append([]Quote(nil), st.Quotes...)
			quoting := st.Status == "quoting"
			st.lastTake = time.Now()
			m.mu.Unlock()
			if quoting && len(quotes) > 0 {
				if err := m.takePair(ctx, r, quotes); err != nil {
					m.mu.Lock()
					st.LastError = "take: " + err.Error()
					m.mu.Unlock()
				}
			}
		}
		return
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
			m.pendDown = append(m.pendDown, key(p.Market, p.Symbol))
		}
		m.mu.Unlock()
		return
	}

	var inv Inventory
	if p.Market == "spot" {
		balances, err := m.cachedBalances(ctx)
		if err != nil {
			m.fail(st, "balances: "+err.Error())
			return
		}
		baselines, err := m.cachedBaselines(ctx)
		if err != nil {
			// fail closed: unknown baseline -> no quotes this pass
			m.fail(st, "baseline: "+err.Error())
			return
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
			return
		}
	} else {
		positions, err := m.cachedPositions(ctx)
		if err != nil {
			m.fail(st, "positions: "+err.Error())
			return
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
	extraBps := decimal.Zero
	if widened {
		// TradFi spot deviates from the reference: push copied bids/asks wider
		extraBps = p.SpreadBps.Mul(widen.Sub(decimal.NewFromInt(1)))
	}
	var book ExtBook
	if m.books != nil && m.cfg.followsBook(p.Market, r.Symbol) {
		book, err = m.books.get(ctx, p.Market, r.Symbol, p.Levels)
	}
	if err == nil && depthMode(p) {
		// depth mode sizes the book in base units: convert venue contracts
		var ok bool
		if book, ok = book.baseUnits(); !ok {
			err = fmt.Errorf("%s: contract value unknown", book.Source)
			log.Printf("mm: %s/%s depth: %v; quoting index ladder", p.Market, r.Symbol, err)
		}
	}
	usedBook := err == nil && book.Valid()
	var mid decimal.Decimal
	var targets []Quote
	if usedBook {
		if depthMode(p) {
			mid, targets = BuildDepthGrid(p, r, book, inv, extraBps)
		} else {
			mid, targets = BuildFromBook(p, r, book, inv, extraBps, m.books.scale)
		}
		if depthMode(p) {
			var f decimal.Decimal
			if targets, f = capMarginBudget(targets, p.Leverage, m.cfg.DepthMargin, r); f.LessThan(one) {
				log.Printf("mm: %s depth scaled x%s to stay within margin budget %s", r.Symbol, f.StringFixed(3), m.cfg.DepthMargin)
			}
		}
	} else {
		// Venue has no depth (delisted / empty). Still quote around the index
		// so the pair is tradable; mark it so ops can see it is not 跟盘.
		if extraBps.IsPositive() {
			p.SpreadBps = p.SpreadBps.Add(extraBps)
		}
		if !m.cfg.followsBook(p.Market, r.Symbol) {
			p.NotionalCap = m.cfg.LightNotionalMax
		}
		mid, targets = BuildLadder(p, r, index, inv)
	}
	m.mu.Lock()
	if m.feedDown[key(p.Market, p.Symbol)] {
		delete(m.feedDown, key(p.Market, p.Symbol))
		m.pendUp = append(m.pendUp, key(p.Market, p.Symbol))
	}
	m.mu.Unlock()
	resting, err := m.openOrders(ctx, r)
	if err != nil {
		m.fail(st, "open orders: "+err.Error())
		return
	}
	if depthMode(p) && len(resting) >= openOrdersPageMax {
		// listing is capped: we can't see every resting order, so placing
		// would duplicate. Cancel what we see and rebuild next pass.
		m.cancelAll(ctx, r, st)
		m.fail(st, "open orders listing truncated (>= 500); reduce levels")
		return
	}
	var cancel []restingOrder
	var place []Quote
	if depthMode(p) && usedBook {
		cancel, place = DiffExact(resting, targets)
	} else {
		cancel, place = Diff(resting, targets, p.RequoteBps)
	}
	// inner levels first so the top of book refills before the tail
	sort.SliceStable(place, func(i, j int) bool { return place[i].Level < place[j].Level })
	lastErr := m.applyOps(ctx, r, st, cancel, place)
	m.mu.Lock()
	st.Index, st.Source, st.Mid, st.Inventory, st.Quotes = index, src, mid, inv, targets
	if usedBook {
		st.BookSource = book.Source
	} else {
		st.BookSource = "index-ladder"
	}
	st.OpenOrders = len(resting) - len(cancel) + len(place)
	st.hasOrders = true
	st.Status, st.Reason, st.LastError = "quoting", "", lastErr
	if !usedBook && !m.cfg.followsBook(p.Market, r.Symbol) {
		st.Reason = "index ladder (light profile)"
	} else if !usedBook {
		st.Reason = "index ladder (no external book)"
	} else if widened {
		st.Reason = "spread widened x" + widen.String() + " (reference deviation)"
	}
	if dueTake {
		st.lastTake = time.Now()
	}
	m.mu.Unlock()
	if dueTake {
		if err := m.takePair(ctx, r, targets); err != nil && lastErr == "" {
			m.mu.Lock()
			st.LastError = "take: " + err.Error()
			m.mu.Unlock()
		}
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

// Shared per-account snapshots. One pass used to fetch these once for all
// pairs; with pairs running concurrently they are cached for 1s instead.
const acctTTL = time.Second

func (m *Maker) cachedBalances(ctx context.Context) (map[string]decimal.Decimal, error) {
	m.cacheMu.Lock()
	defer m.cacheMu.Unlock()
	if m.bal != nil && time.Since(m.balAt) < acctTTL {
		return m.bal, nil
	}
	b, err := m.spotBalances(ctx)
	if err != nil {
		return nil, err
	}
	m.bal, m.balAt = b, time.Now()
	return b, nil
}

func (m *Maker) cachedBaselines(ctx context.Context) (map[string]Baseline, error) {
	m.cacheMu.Lock()
	defer m.cacheMu.Unlock()
	if m.base != nil && time.Since(m.baseAt) < 30*time.Second { // baselines change only on seed/admin
		return m.base, nil
	}
	b, err := m.Baselines(ctx)
	if err != nil {
		return nil, err
	}
	m.base, m.baseAt = b, time.Now()
	return b, nil
}

func (m *Maker) cachedPositions(ctx context.Context) (map[string]decimal.Decimal, error) {
	m.cacheMu.Lock()
	defer m.cacheMu.Unlock()
	if m.pos != nil && time.Since(m.posAt) < acctTTL {
		return m.pos, nil
	}
	p, err := m.futuresPositions(ctx)
	if err != nil {
		return nil, err
	}
	m.pos, m.posAt = p, time.Now()
	return p, nil
}

// applyOps runs one pair's cancels, then its places, MM_PAIR_CONCURRENCY at
// a time. Cancels finish before any place so freed balance is available.
func (m *Maker) applyOps(ctx context.Context, r Rules, st *PairState, cancel []restingOrder, place []Quote) string {
	var mu sync.Mutex
	var lastErr string
	n := m.cfg.PairConcurrency
	if n < 1 {
		n = 1
	}
	run := func(cnt int, f func(i int)) {
		sem := make(chan struct{}, n)
		var wg sync.WaitGroup
		for i := 0; i < cnt; i++ {
			wg.Add(1)
			sem <- struct{}{}
			go func(i int) {
				defer func() { <-sem; wg.Done() }()
				f(i)
			}(i)
		}
		wg.Wait()
	}
	// Futures: turn a cancel + place on the same side into one amend
	// (MM_FUTURES_AMEND, default on). The spot order API has no amend.
	if r.Market == "futures" && m.cfg.FuturesAmend {
		var amends [][2]int
		usedP := make([]bool, len(place))
		var keepC []restingOrder
		for _, o := range cancel {
			j := -1
			for k, q := range place {
				if !usedP[k] && q.Side == o.Side {
					j = k
					break
				}
			}
			if j < 0 {
				keepC = append(keepC, o)
				continue
			}
			usedP[j] = true
			keepC = append(keepC, o)
			amends = append(amends, [2]int{len(keepC) - 1, j})
		}
		var fallbackC []restingOrder
		var fallbackP []Quote
		done := make([]bool, len(keepC))
		run(len(amends), func(i int) {
			a := amends[i]
			err := m.amend(ctx, keepC[a[0]], place[a[1]])
			mu.Lock()
			if err == nil {
				st.Amended++
				done[a[0]] = true
			} else {
				fallbackP = append(fallbackP, place[a[1]])
			}
			mu.Unlock()
		})
		for i, o := range keepC {
			if !done[i] {
				fallbackC = append(fallbackC, o)
			}
		}
		for k, q := range place {
			if !usedP[k] {
				fallbackP = append(fallbackP, q)
			}
		}
		cancel, place = fallbackC, fallbackP
	}
	run(len(cancel), func(i int) {
		err := m.cancel(ctx, r.Market, cancel[i].ID)
		mu.Lock()
		if err != nil {
			lastErr = "cancel: " + err.Error()
		} else {
			st.Cancelled++
		}
		mu.Unlock()
	})
	run(len(place), func(i int) {
		q := place[i]
		err := m.place(ctx, r, q)
		mu.Lock()
		if err != nil {
			lastErr = fmt.Sprintf("place %s L%d: %v", q.Side, q.Level, err)
		} else {
			st.Placed++
		}
		mu.Unlock()
	})
	return lastErr
}

// flushAlerts sends the batched pause/recover Telegram alerts at most every
// 30s so a feed blip over hundreds of pairs is one message, not hundreds.
func (m *Maker) flushAlerts(force bool) {
	m.mu.Lock()
	if (len(m.pendDown) == 0 && len(m.pendUp) == 0) || (!force && time.Since(m.alertAt) < 30*time.Second) {
		m.mu.Unlock()
		return
	}
	down, up := m.pendDown, m.pendUp
	m.pendDown, m.pendUp = nil, nil
	m.alertAt = time.Now()
	m.mu.Unlock()
	if len(down) > 0 {
		sort.Strings(down)
		m.alert(fmt.Sprintf("⚠️ Linbit 做市暂停：外部指数或盘口不可用，已撤销报价。交易对（%d）：%s", len(down), strings.Join(down, ", ")))
	}
	if len(up) > 0 {
		sort.Strings(up)
		m.alert(fmt.Sprintf("✅ Linbit 做市恢复：外部指数/盘口已恢复。交易对（%d）：%s", len(up), strings.Join(up, ", ")))
	}
}
