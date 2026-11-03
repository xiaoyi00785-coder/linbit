package main

// convert_hedge.go: automatic hedging of the 闪兑 (convert) book on the spot
// order book.
//
// Accounts:
//   - system:convert (ledger-only) is the counterparty of every user convert;
//     its per-asset ledger net is the "convert position".
//   - CONVERT_HEDGE_USER_ID (default system:hedge) is a real spot trading
//     account owned by the platform. It is separate from the market maker
//     (MM_USER_ID, mm:main) so hedge fills never self-trade against the
//     maker's quotes being attributed to the same account, and P&L is
//     attributable. Funding moves platform-owned assets from the convert
//     position into the hedge account with a balanced journal entry
//     (system:convert credit / user:<hedge>:free debit), so no money is
//     created: net exposure per asset = convert position + hedge balance.
//
// Loop (every CONVERT_HEDGE_INTERVAL_SECONDS, one sequential worker):
//   net = position + hedge balance; if |net| ≥ per-asset min, place one
//   IOC limit at index ± slippageBps (buy when short, sell when long) capped
//   by maxOrderUsdt, the remaining maxHourUsdt budget and available funding.
//   Thin depth: whatever fills is kept, the rest stays unhedged and is retried
//   with exponential backoff. If the unhedged notional stays above
//   unhedgedAlertUsdt for more than unhedgedAlertMinutes a Telegram alert
//   fires (once per episode). Rejections / failures alert (rate-limited).

import (
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"log"
	"net/http"
	"net/url"
	"sort"
	"strings"
	"sync"
	"time"

	"github.com/linbit/platform/services/go/common"
	"github.com/shopspring/decimal"
)

// SymbolRules are the spot trading rules for a hedge symbol.
type SymbolRules struct {
	Symbol            string
	PricePrecision    int
	QuantityPrecision int
	MinQuantity       decimal.Decimal
	MinNotional       decimal.Decimal
	Trading           bool
}

// HedgeOrderResult is the synchronous outcome of an IOC order.
type HedgeOrderResult struct {
	OrderID string
	Status  string // order-service status
	Filled  decimal.Decimal
	Quote   decimal.Decimal // filled quote amount when known (0 = unknown)
}

// errHedgeRejected: the order service refused the order (4xx).
type errHedgeRejected struct{ code, msg string }

func (e *errHedgeRejected) Error() string { return e.code + ": " + e.msg }

// HedgeVenue places hedge orders (order service in prod, fakes in tests).
type HedgeVenue interface {
	Rules(ctx context.Context, symbol string) (SymbolRules, error)
	PlaceIOC(ctx context.Context, userID, symbol, side string, price, qty decimal.Decimal, clientID string) (HedgeOrderResult, error)
	// Order reads an order's current status / fills (Quote = filled quote amount).
	Order(ctx context.Context, userID, orderID string) (HedgeOrderResult, error)
	// Cancel cancels a resting order; the result is the final state.
	Cancel(ctx context.Context, userID, orderID string) (HedgeOrderResult, error)
}

func orderOpen(status string) bool { return status == "new" || status == "partially_filled" }

// ---- pure planning ----

type HedgeInput struct {
	Net           decimal.Decimal // convert position + hedge balance (negative = platform short)
	Index         decimal.Decimal
	SlippageBps   int
	Rules         SymbolRules
	MinQty        decimal.Decimal // per-asset hedge minimum
	MaxOrderUSDT  decimal.Decimal
	HourRemaining decimal.Decimal // USDT budget left in the rolling hour
	AvailBase     decimal.Decimal // base the hedge can sell (hedge free + convert long)
	AvailQuote    decimal.Decimal // USDT the hedge can spend (hedge free + convert USDT long)
}

type HedgePlan struct {
	Side  string // buy | sell | "" (no order)
	Qty   decimal.Decimal
	Price decimal.Decimal
	Skip  string // below_min | budget | funds | symbol_halted
}

// PlanHedge computes one IOC order. The limit is the slippage cap: a buy
// never pays more than index·(1+cap), a sell never receives less than
// index·(1−cap) (rounded inward to the tick).
func PlanHedge(in HedgeInput) HedgePlan {
	if !in.Rules.Trading {
		return HedgePlan{Skip: "symbol_halted"}
	}
	if in.Net.Sign() == 0 {
		return HedgePlan{Skip: "below_min"}
	}
	minQty := decimal.Max(in.MinQty, in.Rules.MinQuantity)
	need := in.Net.Abs()
	if need.LessThan(minQty) {
		return HedgePlan{Skip: "below_min"}
	}
	cap := decimal.NewFromInt(int64(in.SlippageBps)).Div(decimal.NewFromInt(10000))
	tick := in.Rules.PricePrecision
	p := HedgePlan{Side: "sell"}
	if in.Net.IsNegative() {
		p.Side = "buy"
		p.Price = in.Index.Mul(decimal.NewFromInt(1).Add(cap)).RoundFloor(int32(tick))
	} else {
		p.Price = in.Index.Mul(decimal.NewFromInt(1).Sub(cap)).RoundCeil(int32(tick))
	}
	if p.Price.Sign() <= 0 {
		return HedgePlan{Skip: "below_min"}
	}
	qty, skip := need, ""
	limit := func(maxQty decimal.Decimal, why string) {
		if maxQty.LessThan(qty) {
			qty, skip = maxQty, why
		}
	}
	limit(in.MaxOrderUSDT.Div(p.Price), "budget")
	limit(decimal.Max(in.HourRemaining, decimal.Zero).Div(p.Price), "budget")
	if p.Side == "buy" {
		limit(decimal.Max(in.AvailQuote, decimal.Zero).Div(p.Price), "funds")
	} else {
		limit(decimal.Max(in.AvailBase, decimal.Zero), "funds")
	}
	qty = qty.RoundFloor(int32(in.Rules.QuantityPrecision))
	if qty.LessThan(minQty) || qty.Mul(p.Price).LessThan(in.Rules.MinNotional) {
		if skip == "" {
			skip = "below_min"
		}
		return HedgePlan{Skip: skip}
	}
	p.Qty = qty
	return p
}

// ---- per-asset state (backoff + unhedged alerting) ----

type HedgeAssetState struct {
	Asset         string `json:"asset"`
	Net           string `json:"net"`
	UnhedgedUSDT  string `json:"unhedgedUsdt"`
	UnhedgedSince int64  `json:"unhedgedSince,omitempty"`
	Alerted       bool   `json:"alerted"`
	Failures      int    `json:"failures"`
	NextRetryAt   int64  `json:"nextRetryAt,omitempty"`
	LastStatus    string `json:"lastStatus,omitempty"`
	LastSkip      string `json:"lastSkip,omitempty"`
	LastError     string `json:"lastError,omitempty"`
	LastRunAt     int64  `json:"lastRunAt,omitempty"`
	since, next   time.Time
	lastFailAlert time.Time
}

// observe tracks unhedged exposure; returns true when an alert must fire.
func (st *HedgeAssetState) observe(now time.Time, unhedgedUSDT, threshold decimal.Decimal, minutes int) bool {
	st.UnhedgedUSDT = unhedgedUSDT.Round(2).String()
	if !unhedgedUSDT.GreaterThan(threshold) {
		st.since, st.UnhedgedSince, st.Alerted = time.Time{}, 0, false
		return false
	}
	if st.since.IsZero() {
		st.since, st.UnhedgedSince = now, now.UnixMilli()
	}
	if !st.Alerted && now.Sub(st.since) > time.Duration(minutes)*time.Minute {
		st.Alerted = true
		return true
	}
	return false
}

// result applies backoff: a full fill resets it; anything else delays the
// next attempt by base·2^(n−1), capped.
func (st *HedgeAssetState) result(now time.Time, status string, base, max time.Duration) {
	st.LastStatus = status
	if status == "filled" {
		st.Failures, st.next, st.NextRetryAt = 0, time.Time{}, 0
		return
	}
	st.Failures++
	d := base
	for i := 1; i < st.Failures && d < max; i++ {
		d *= 2
	}
	if d > max {
		d = max
	}
	st.next = now.Add(d)
	st.NextRetryAt = st.next.UnixMilli()
}

type Hedger struct {
	run    sync.Mutex // one sequential worker: never two hedge orders in flight
	mu     sync.Mutex
	venue  HedgeVenue
	states map[string]*HedgeAssetState
}

func (s *Server) hedger() *Hedger {
	s.hedgeOnce.Do(func() {
		if s.hedge == nil {
			s.hedge = &Hedger{venue: &orderVenue{url: s.cfg.OrderURL, token: s.cfg.InternalToken}}
		}
		s.hedge.states = map[string]*HedgeAssetState{}
	})
	return s.hedge
}

func (h *Hedger) state(asset string) *HedgeAssetState {
	h.mu.Lock()
	defer h.mu.Unlock()
	st, ok := h.states[asset]
	if !ok {
		st = &HedgeAssetState{Asset: asset}
		h.states[asset] = st
	}
	return st
}

func (h *Hedger) snapshot() map[string]HedgeAssetState {
	h.mu.Lock()
	defer h.mu.Unlock()
	out := map[string]HedgeAssetState{}
	for k, v := range h.states {
		out[k] = *v
	}
	return out
}

// ---- store helpers ----

// accountTotals: free+locked per asset for a user account.
func (s *Store) accountTotals(ctx context.Context, userID string) (map[string]decimal.Decimal, error) {
	tot, _, err := s.accountBalances(ctx, userID)
	return tot, err
}

func (s *Store) accountBalances(ctx context.Context, userID string) (total, free map[string]decimal.Decimal, err error) {
	total, free = map[string]decimal.Decimal{}, map[string]decimal.Decimal{}
	rows, err := s.db.QueryContext(ctx, `SELECT asset, free::text, locked::text FROM balances WHERE user_id=$1`, userID)
	if err != nil {
		return nil, nil, err
	}
	defer rows.Close()
	for rows.Next() {
		var a, f, l string
		if err := rows.Scan(&a, &f, &l); err != nil {
			return nil, nil, err
		}
		fd := decimal.RequireFromString(f)
		free[a], total[a] = fd, fd.Add(decimal.RequireFromString(l))
	}
	return total, free, rows.Err()
}

// fundHedge moves amount of asset from the convert position into the hedge
// account (idempotent on ref). Fails with ErrInsufficient if the convert
// position does not hold it — the hedge never creates balances.
func (s *Store) fundHedge(ctx context.Context, hedgeUser, asset string, amount decimal.Decimal, ref string) error {
	tx, err := s.db.BeginTx(ctx, nil)
	if err != nil {
		return err
	}
	defer tx.Rollback() //nolint:errcheck
	if _, err := tx.ExecContext(ctx, `SELECT pg_advisory_xact_lock(hashtext('convert_position'))`); err != nil {
		return err
	}
	var pos string
	if err := tx.QueryRowContext(ctx, `SELECT COALESCE(SUM(CASE WHEN direction='debit' THEN amount ELSE -amount END),0)::text
		FROM ledger_entries WHERE account=$1 AND asset=$2`, ConvertAccount, asset).Scan(&pos); err != nil {
		return err
	}
	if decimal.RequireFromString(pos).LessThan(amount) {
		return ErrInsufficient
	}
	if err := claimRef(ctx, tx, ref, "hedge_fund", hedgeUser, asset, amount); err != nil {
		if errors.Is(err, ErrAlreadyApplied) {
			return nil
		}
		return err
	}
	if err := s.applyDeltas(ctx, tx, []balanceDelta{{userID: hedgeUser, asset: asset, free: amount}}); err != nil {
		return err
	}
	if err := s.writeJournal(ctx, tx, hedgeUser, "hedge_fund", ref, []ledgerLine{
		{Account: ConvertAccount, Asset: asset, Direction: "credit", Amount: amount},
		{Account: userAcct(hedgeUser, "free"), Asset: asset, Direction: "debit", Amount: amount},
	}); err != nil {
		return err
	}
	return tx.Commit()
}

// HedgeRecord is one convert_hedges row.
type HedgeRecord struct {
	ID          int64  `json:"id"`
	Asset       string `json:"asset"`
	Symbol      string `json:"symbol"`
	Side        string `json:"side"`
	Quantity    string `json:"quantity"`
	LimitPrice  string `json:"limitPrice"`
	IndexPrice  string `json:"indexPrice"`
	NetBefore   string `json:"netBefore"`
	OrderID     string `json:"orderId"`
	FilledQty   string `json:"filledQty"`
	QuoteAmount string `json:"quoteAmount"`
	Status      string `json:"status"`
	Error       string `json:"error,omitempty"`
	CreatedAt   int64  `json:"createdAt"`
}

const hedgeCols = `id, asset, symbol, side, quantity::text, limit_price::text, index_price::text, net_before::text, order_id, filled_qty::text, quote_amount::text, status, error, created_at`

func scanHedge(sc interface{ Scan(...any) error }) (HedgeRecord, error) {
	var h HedgeRecord
	var at time.Time
	if err := sc.Scan(&h.ID, &h.Asset, &h.Symbol, &h.Side, &h.Quantity, &h.LimitPrice, &h.IndexPrice, &h.NetBefore, &h.OrderID, &h.FilledQty, &h.QuoteAmount, &h.Status, &h.Error, &at); err != nil {
		return h, err
	}
	for _, p := range []*string{&h.Quantity, &h.LimitPrice, &h.IndexPrice, &h.NetBefore, &h.FilledQty, &h.QuoteAmount} {
		*p = decimal.RequireFromString(*p).String()
	}
	h.CreatedAt = at.UnixMilli()
	return h, nil
}

// ---- the cycle ----

// runHedgeCycle performs one pass over every hedgeable asset. force ignores
// the retry backoff (manual trigger).
// sweepResting resolves hedge orders that did not finish immediately: once the
// order service reports them done they are finalised; if they are still
// new/partially_filled after cfg.Hedge.CancelAfterSeconds they are canceled.
// Either way the unfilled remainder (released back to the hedge account by
// the cancel) shows up again in net = convert position + hedge balance and is
// re-hedged by later cycles. Returns the assets that still have a live order.
func (s *Server) sweepResting(ctx context.Context, h *Hedger, cfg ConvertConfig) (map[string]bool, []HedgeRecord) {
	live := map[string]bool{}
	rows, err := s.store.db.QueryContext(ctx, `SELECT `+hedgeCols+` FROM convert_hedges WHERE status='resting' ORDER BY id`)
	if err != nil {
		log.Printf("hedge sweep: %v", err)
		return live, nil
	}
	var recs []HedgeRecord
	for rows.Next() {
		r, err := scanHedge(rows)
		if err != nil {
			log.Printf("hedge sweep scan: %v", err)
			continue
		}
		recs = append(recs, r)
	}
	rows.Close()
	hedgeUser := s.cfg.Convert.HedgeUserID
	after := time.Duration(cfg.Hedge.CancelAfterSeconds) * time.Second
	var out []HedgeRecord
	for _, r := range recs {
		res, err := h.venue.Order(ctx, hedgeUser, r.OrderID)
		canceled := false
		if err == nil && orderOpen(res.Status) && s.now().Sub(time.UnixMilli(r.CreatedAt)) >= after {
			res, err = h.venue.Cancel(ctx, hedgeUser, r.OrderID)
			canceled = err == nil
		}
		if err != nil {
			log.Printf("hedge sweep %d: %v", r.ID, err)
			live[r.Asset] = true
			continue
		}
		if orderOpen(res.Status) {
			live[r.Asset] = true
			continue
		}
		qty := decimal.RequireFromString(r.Quantity)
		status := "filled"
		switch {
		case canceled:
			status = "canceled"
		case res.Filled.Sign() == 0:
			status = "unfilled"
		case res.Filled.LessThan(qty):
			status = "partial"
		}
		quote := res.Quote
		if quote.Sign() == 0 {
			quote = res.Filled.Mul(decimal.RequireFromString(r.LimitPrice))
		}
		r.Status, r.FilledQty, r.QuoteAmount = status, res.Filled.String(), quote.String()
		if _, err := s.store.db.ExecContext(ctx, `UPDATE convert_hedges SET filled_qty=$2, quote_amount=$3, status=$4, updated_at=now() WHERE id=$1 AND status='resting'`,
			r.ID, r.FilledQty, r.QuoteAmount, status); err != nil {
			log.Printf("hedge sweep update %d: %v", r.ID, err)
		}
		if canceled {
			s.store.convertEvent(ctx, "hedge_canceled", r.Asset, r)
			convertAlert(fmt.Sprintf("闪兑对冲撤单：%s 对冲单 %s 超过 %d 秒未完成已撤销，成交 %s / %s，未成交部分回到未对冲敞口",
				r.Asset, r.OrderID, cfg.Hedge.CancelAfterSeconds, r.FilledQty, r.Quantity))
		}
		out = append(out, r)
	}
	return live, out
}

func (s *Server) runHedgeCycle(ctx context.Context, force bool) ([]HedgeRecord, error) {
	h := s.hedger()
	h.run.Lock()
	defer h.run.Unlock()
	cfg, err := s.store.convertConfig(ctx, s.cfg.Convert)
	if err != nil {
		return nil, err
	}
	live, swept := s.sweepResting(ctx, h, cfg)
	hedgeUser := s.cfg.Convert.HedgeUserID
	pos, err := s.store.ConvertPosition(ctx)
	if err != nil {
		return nil, err
	}
	tot, free, err := s.store.accountBalances(ctx, hedgeUser)
	if err != nil {
		return nil, err
	}
	var used string
	if err := s.store.db.QueryRowContext(ctx, `SELECT COALESCE(SUM(GREATEST(quote_amount, filled_qty*limit_price)),0)::text FROM convert_hedges
		WHERE created_at > now() - interval '1 hour'`).Scan(&used); err != nil {
		return nil, err
	}
	hourLeft := decimal.RequireFromString(cfg.Hedge.MaxHourUSDT).Sub(decimal.RequireFromString(used))
	maxOrder := decimal.RequireFromString(cfg.Hedge.MaxOrderUSDT)
	alertUSDT := decimal.RequireFromString(cfg.Hedge.UnhedgedAlertUSDT)
	now := s.now()

	type item struct {
		a   ConvertAssetConf
		net decimal.Decimal
	}
	items := []item{}
	for _, a := range cfg.Assets {
		if stablecoins[a.Asset] {
			continue
		}
		items = append(items, item{a, pos[a.Asset].Add(tot[a.Asset])})
	}
	// Sells first: they raise USDT that later buys can use.
	sort.SliceStable(items, func(i, j int) bool { return items[i].net.Sign() > items[j].net.Sign() })

	out := append([]HedgeRecord{}, swept...)
	for _, it := range items {
		a, net := it.a, it.net
		st := h.state(a.Asset)
		h.mu.Lock()
		st.Net, st.LastRunAt = net.String(), now.UnixMilli()
		h.mu.Unlock()
		index, at, perr := s.prices(ctx, a.Asset)
		if perr == nil && (index.Sign() <= 0 || (!at.IsZero() && now.Sub(at) > s.cfg.Convert.PriceMaxAge)) {
			perr = errors.New("index stale")
		}
		if perr != nil {
			h.mu.Lock()
			st.LastSkip, st.LastError = "price_unavailable", perr.Error()
			h.mu.Unlock()
			continue
		}
		enabled := cfg.Hedge.Enabled && a.HedgeEnabled
		if live[a.Asset] {
			// one live order per asset: its locked funds are not yet reflected in net
			h.mu.Lock()
			st.LastSkip = "order_resting"
			h.mu.Unlock()
		} else if enabled && net.Sign() != 0 && (force || !now.Before(st.next)) {
			rec, filled := s.hedgeAsset(ctx, h, st, cfg, a, net, index, hourLeft, maxOrder, pos, free)
			if rec != nil {
				out = append(out, *rec)
				spent := decimal.RequireFromString(rec.QuoteAmount)
				hourLeft = hourLeft.Sub(decimal.Max(spent, filled.Mul(decimal.RequireFromString(rec.LimitPrice))))
				if rec.Side == "buy" {
					net = net.Add(filled)
				} else {
					net = net.Sub(filled)
				}
				// Re-read balances for the next asset (funding/fills changed them).
				if t2, f2, err := s.store.accountBalances(ctx, hedgeUser); err == nil {
					tot, free = t2, f2
				}
				if p2, err := s.store.ConvertPosition(ctx); err == nil {
					pos = p2
				}
			}
		} else if !enabled {
			h.mu.Lock()
			st.LastSkip = "disabled"
			h.mu.Unlock()
		}
		h.mu.Lock()
		st.Net = net.String()
		fire := st.observe(now, net.Abs().Mul(index), alertUSDT, cfg.Hedge.UnhedgedAlertMinutes)
		h.mu.Unlock()
		if fire {
			detail := map[string]any{"asset": a.Asset, "net": net.String(), "unhedgedUsdt": st.UnhedgedUSDT,
				"thresholdUsdt": cfg.Hedge.UnhedgedAlertUSDT, "minutes": cfg.Hedge.UnhedgedAlertMinutes, "hedgeEnabled": enabled, "lastSkip": st.LastSkip}
			s.store.convertEvent(ctx, "unhedged_alert", a.Asset, detail)
			sw := "对冲开启"
			if !enabled {
				sw = "对冲已关闭"
			}
			convertAlert(fmt.Sprintf("闪兑对冲告警：%s 未对冲敞口 %s USDT（净头寸 %s）已超过 %s USDT 持续 %d 分钟以上（%s，原因：%s）",
				a.Asset, st.UnhedgedUSDT, net.String(), cfg.Hedge.UnhedgedAlertUSDT, cfg.Hedge.UnhedgedAlertMinutes, sw, orDash(st.LastSkip)))
		}
	}
	return out, nil
}

func orDash(s string) string {
	if s == "" {
		return "-"
	}
	return s
}

// hedgeAsset places at most one IOC order for asset; returns the record and
// the filled base quantity.
func (s *Server) hedgeAsset(ctx context.Context, h *Hedger, st *HedgeAssetState, cfg ConvertConfig, a ConvertAssetConf,
	net, index, hourLeft, maxOrder decimal.Decimal, pos, free map[string]decimal.Decimal) (*HedgeRecord, decimal.Decimal) {
	hedgeUser := s.cfg.Convert.HedgeUserID
	symbol := a.Asset + "USDT"
	setSkip := func(skip, e string) {
		h.mu.Lock()
		st.LastSkip, st.LastError = skip, e
		h.mu.Unlock()
	}
	rules, err := h.venue.Rules(ctx, symbol)
	if err != nil {
		setSkip("rules_unavailable", err.Error())
		return nil, decimal.Zero
	}
	minQty, _ := decimal.NewFromString(a.HedgeMinQty)
	plan := PlanHedge(HedgeInput{
		Net: net, Index: index, SlippageBps: cfg.Hedge.SlippageBps, Rules: rules, MinQty: minQty,
		MaxOrderUSDT: maxOrder, HourRemaining: hourLeft,
		AvailBase:  free[a.Asset].Add(decimal.Max(pos[a.Asset], decimal.Zero)),
		AvailQuote: free["USDT"].Add(decimal.Max(pos["USDT"], decimal.Zero)),
	})
	if plan.Side == "" {
		setSkip(plan.Skip, "")
		return nil, decimal.Zero
	}
	setSkip(plan.Skip, "") // partial-by-budget/funds is still reported
	var id int64
	var created time.Time
	if err := s.store.db.QueryRowContext(ctx, `INSERT INTO convert_hedges (asset, symbol, side, quantity, limit_price, index_price, net_before)
		VALUES ($1,$2,$3,$4,$5,$6,$7) RETURNING id, created_at`, a.Asset, symbol, plan.Side, plan.Qty.String(), plan.Price.String(), index.String(), net.String()).
		Scan(&id, &created); err != nil {
		setSkip("", err.Error())
		return nil, decimal.Zero
	}
	rec := &HedgeRecord{ID: id, Asset: a.Asset, Symbol: symbol, Side: plan.Side, Quantity: plan.Qty.String(), LimitPrice: plan.Price.String(),
		IndexPrice: index.String(), NetBefore: net.String(), FilledQty: "0", QuoteAmount: "0", CreatedAt: created.UnixMilli()}

	// Fund the hedge account from the convert position if needed.
	fundAsset, need := a.Asset, plan.Qty
	if plan.Side == "buy" {
		fundAsset, need = "USDT", plan.Qty.Mul(plan.Price)
	}
	var placeErr error
	if short := need.Sub(free[fundAsset]); short.Sign() > 0 {
		placeErr = s.store.fundHedge(ctx, hedgeUser, fundAsset, short, fmt.Sprintf("hedge_fund:%d", id))
	}
	var res HedgeOrderResult
	before, _ := s.store.accountTotals(ctx, hedgeUser)
	if placeErr == nil {
		res, placeErr = h.venue.PlaceIOC(ctx, hedgeUser, symbol, plan.Side, plan.Price, plan.Qty, fmt.Sprintf("hdg%d", id))
	}
	after, _ := s.store.accountTotals(ctx, hedgeUser)
	quote := after["USDT"].Sub(before["USDT"]).Abs()

	if orderOpen(res.Status) && res.Quote.Sign() > 0 {
		quote = res.Quote
	}
	status := "filled"
	var rej *errHedgeRejected
	switch {
	case errors.As(placeErr, &rej):
		status, rec.Error = "rejected", rej.Error()
	case placeErr != nil:
		status, rec.Error = "failed", placeErr.Error()
	case orderOpen(res.Status):
		status = "resting"
	case res.Filled.Sign() == 0:
		status = "unfilled"
	case res.Filled.LessThan(plan.Qty):
		status = "partial"
	}
	rec.Status, rec.OrderID, rec.FilledQty, rec.QuoteAmount = status, res.OrderID, res.Filled.String(), quote.String()
	if _, err := s.store.db.ExecContext(ctx, `UPDATE convert_hedges SET order_id=$2, filled_qty=$3, quote_amount=$4, status=$5, error=$6, updated_at=now() WHERE id=$1`,
		id, res.OrderID, res.Filled.String(), quote.String(), status, rec.Error); err != nil {
		log.Printf("hedge update %d: %v", id, err)
	}
	now := s.now()
	h.mu.Lock()
	st.result(now, status, s.cfg.Convert.HedgeBackoff, s.cfg.Convert.HedgeBackoffMax)
	st.LastError = rec.Error
	failAlert := (status == "rejected" || status == "failed") && now.Sub(st.lastFailAlert) >= 10*time.Minute
	if failAlert {
		st.lastFailAlert = now
	}
	h.mu.Unlock()
	if status == "rejected" || status == "failed" {
		s.store.convertEvent(ctx, "hedge_"+status, a.Asset, rec)
		if failAlert {
			convertAlert(fmt.Sprintf("闪兑对冲失败：%s %s %s @ %s（IOC）%s：%s；第 %d 次连续未完成，将退避重试",
				a.Asset, plan.Side, plan.Qty, plan.Price, map[string]string{"rejected": "被拒绝", "failed": "下单失败"}[status], rec.Error, st.Failures))
		}
	}
	// An IOC should never rest; if it does it is tracked as "resting" and
	// canceled by sweepResting after hedge.cancelAfterSeconds.
	if status == "resting" {
		s.store.convertEvent(ctx, "hedge_resting", a.Asset, rec)
	}
	return rec, res.Filled
}

// runHedger is the background loop.
func (s *Server) runHedger(ctx context.Context) {
	t := time.NewTicker(s.cfg.Convert.HedgeInterval)
	defer t.Stop()
	for {
		select {
		case <-ctx.Done():
			return
		case <-t.C:
		}
		cctx, cancel := context.WithTimeout(ctx, 30*time.Second)
		if _, err := s.runHedgeCycle(cctx, false); err != nil {
			log.Printf("convert hedge cycle: %v", err)
		}
		cancel()
	}
}

// ---- order-service venue ----

type orderVenue struct {
	url, token string
	client     http.Client
}

func (v *orderVenue) do(ctx context.Context, method, path string, hdr map[string]string, body any, out any) (int, []byte, error) {
	var rd io.Reader
	if body != nil {
		b, _ := json.Marshal(body)
		rd = bytes.NewReader(b)
	}
	req, err := http.NewRequestWithContext(ctx, method, strings.TrimRight(v.url, "/")+path, rd)
	if err != nil {
		return 0, nil, err
	}
	req.Header.Set("Content-Type", "application/json")
	for k, val := range hdr {
		req.Header.Set(k, val)
	}
	v.client.Timeout = 10 * time.Second
	resp, err := v.client.Do(req)
	if err != nil {
		return 0, nil, err
	}
	defer resp.Body.Close()
	b, _ := io.ReadAll(io.LimitReader(resp.Body, 1<<20))
	if resp.StatusCode < 300 && out != nil {
		if err := json.Unmarshal(b, out); err != nil {
			return resp.StatusCode, b, err
		}
	}
	return resp.StatusCode, b, nil
}

func (v *orderVenue) Rules(ctx context.Context, symbol string) (SymbolRules, error) {
	var r struct {
		Symbol            string `json:"symbol"`
		PricePrecision    int    `json:"pricePrecision"`
		QuantityPrecision int    `json:"quantityPrecision"`
		MinQuantity       string `json:"minQuantity"`
		MinNotional       string `json:"minNotional"`
		Status            string `json:"status"`
	}
	code, b, err := v.do(ctx, "GET", "/internal/symbols/"+symbol, map[string]string{"X-Internal-Token": v.token}, nil, &r)
	if err != nil {
		return SymbolRules{}, err
	}
	if code != http.StatusOK {
		return SymbolRules{}, fmt.Errorf("symbol %s: http %d %s", symbol, code, truncate(b))
	}
	mq, _ := decimal.NewFromString(r.MinQuantity)
	mn, _ := decimal.NewFromString(r.MinNotional)
	return SymbolRules{Symbol: r.Symbol, PricePrecision: r.PricePrecision, QuantityPrecision: r.QuantityPrecision, MinQuantity: mq, MinNotional: mn, Trading: r.Status == "trading"}, nil
}

func (v *orderVenue) PlaceIOC(ctx context.Context, userID, symbol, side string, price, qty decimal.Decimal, clientID string) (HedgeOrderResult, error) {
	var o struct {
		OrderID        string `json:"orderId"`
		FilledQuantity string `json:"filledQuantity"`
		Status         string `json:"status"`
	}
	body := map[string]any{"symbol": symbol, "side": side, "type": "limit", "price": price.String(), "quantity": qty.String(),
		"timeInForce": "IOC", "clientOrderId": clientID}
	code, b, err := v.do(ctx, "POST", "/spot/orders", map[string]string{"X-User-Id": userID}, body, &o)
	if err != nil {
		return HedgeOrderResult{}, err
	}
	if code >= 400 && code < 500 {
		var e struct{ Code, Message string }
		_ = json.Unmarshal(b, &e)
		return HedgeOrderResult{}, &errHedgeRejected{code: orDash(e.Code), msg: e.Message}
	}
	if code >= 300 {
		return HedgeOrderResult{}, fmt.Errorf("order service http %d %s", code, truncate(b))
	}
	f, _ := decimal.NewFromString(o.FilledQuantity)
	return HedgeOrderResult{OrderID: o.OrderID, Status: o.Status, Filled: f}, nil
}

type venueOrder struct {
	Order struct {
		OrderID string `json:"orderId"`
		Status  string `json:"status"`
	} `json:"order"`
	FilledQuantity string `json:"filledQuantity"`
	FilledQuote    string `json:"filledQuote"`
}

func (v *orderVenue) Order(ctx context.Context, userID, orderID string) (HedgeOrderResult, error) {
	var o venueOrder
	code, b, err := v.do(ctx, "GET", "/internal/orders/"+url.PathEscape(orderID)+"?userId="+url.QueryEscape(userID),
		map[string]string{"X-Internal-Token": v.token}, nil, &o)
	if err != nil {
		return HedgeOrderResult{}, err
	}
	if code != http.StatusOK {
		return HedgeOrderResult{}, fmt.Errorf("order %s: http %d %s", orderID, code, truncate(b))
	}
	f, _ := decimal.NewFromString(o.FilledQuantity)
	q, _ := decimal.NewFromString(o.FilledQuote)
	return HedgeOrderResult{OrderID: o.Order.OrderID, Status: o.Order.Status, Filled: f, Quote: q}, nil
}

func (v *orderVenue) Cancel(ctx context.Context, userID, orderID string) (HedgeOrderResult, error) {
	code, b, err := v.do(ctx, "DELETE", "/spot/orders/"+url.PathEscape(orderID), map[string]string{"X-User-Id": userID}, nil, nil)
	if err != nil {
		return HedgeOrderResult{}, err
	}
	// ORDER_NOT_CANCELABLE (already done) is fine: read the final state either way.
	if code >= 300 && code != http.StatusBadRequest {
		return HedgeOrderResult{}, fmt.Errorf("cancel %s: http %d %s", orderID, code, truncate(b))
	}
	return v.Order(ctx, userID, orderID)
}

func truncate(b []byte) string {
	if len(b) > 200 {
		b = b[:200]
	}
	return string(b)
}

// ---- HTTP ----

// GET /internal/convert/hedges?asset=&status=&page=&pageSize=
func (s *Server) handleConvertHedges(w http.ResponseWriter, r *http.Request) {
	q := r.URL.Query()
	page := atoiDefault(q.Get("page"), 1, 1, 1<<30)
	size := atoiDefault(q.Get("pageSize"), 20, 1, 100)
	where := `WHERE ($1='' OR asset=$1) AND ($2='' OR status=$2)`
	args := []any{strings.ToUpper(q.Get("asset")), q.Get("status")}
	var total int
	if err := s.store.db.QueryRowContext(r.Context(), `SELECT count(*) FROM convert_hedges `+where, args...).Scan(&total); err != nil {
		writeConvertErr(w, "hedges", err)
		return
	}
	rows, err := s.store.db.QueryContext(r.Context(), `SELECT `+hedgeCols+` FROM convert_hedges `+where+` ORDER BY id DESC LIMIT $3 OFFSET $4`,
		append(args, size, (page-1)*size)...)
	if err != nil {
		writeConvertErr(w, "hedges", err)
		return
	}
	defer rows.Close()
	items := []HedgeRecord{}
	for rows.Next() {
		h, err := scanHedge(rows)
		if err != nil {
			writeConvertErr(w, "hedges scan", err)
			return
		}
		items = append(items, h)
	}
	common.WriteJSON(w, http.StatusOK, map[string]any{"items": items, "page": page, "pageSize": size, "total": total,
		"account": s.cfg.Convert.HedgeUserID, "state": s.hedger().snapshot()})
}

// POST /internal/convert/hedge/run — run one cycle now (ignores backoff).
func (s *Server) handleHedgeRun(w http.ResponseWriter, r *http.Request) {
	recs, err := s.runHedgeCycle(r.Context(), true)
	if err != nil {
		writeConvertErr(w, "hedge run", err)
		return
	}
	common.WriteJSON(w, http.StatusOK, map[string]any{"orders": recs, "state": s.hedger().snapshot()})
}
