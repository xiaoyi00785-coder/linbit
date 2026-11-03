package main

import (
	"context"
	"errors"
	"net/http"
	"strings"
	"sync"
	"testing"
	"time"

	"github.com/shopspring/decimal"
)

// ---- depeg guard (pure) ----

func TestEvalDepeg(t *testing.T) {
	now := time.Now()
	age := 15 * time.Second
	cases := []struct {
		p      string
		at     time.Time
		err    error
		paused bool
		reason string
	}{
		{"1", now, nil, false, ""},
		{"0.995", now, nil, false, ""}, // exactly 50 bps: still in range
		{"1.004", now, nil, false, ""},
		{"0.9949", now, nil, true, "depeg"},
		{"1.006", now, nil, true, "depeg"},
		{"1", now.Add(-time.Minute), nil, true, "price_unavailable"}, // stale
		{"0", now, nil, true, "price_unavailable"},
		{"1", now, errors.New("down"), true, "price_unavailable"}, // missing
	}
	for _, c := range cases {
		st := EvalDepeg(d(c.p), c.at, now, c.err, age, 50)
		if st.Paused != c.paused || st.Reason != c.reason {
			t.Errorf("p=%s err=%v: got paused=%v reason=%q", c.p, c.err, st.Paused, st.Reason)
		}
	}
	if st := EvalDepeg(d("0.99"), now, now, nil, age, 150); st.Paused {
		t.Error("configurable threshold: 100 bps < 150 must not pause")
	}
}

func TestDepegGuardScope(t *testing.T) {
	if !isDepegGuarded("USDC", "USDT") || !isDepegGuarded("USDT", "USDC") || isDepegGuarded("USDC", "BTC") || isDepegGuarded("USDT", "BTC") {
		t.Fatal("guard scope must be exactly USDC<->USDT")
	}
}

// ---- hedge planning (pure) ----

func btcRules() SymbolRules {
	return SymbolRules{Symbol: "BTCUSDT", PricePrecision: 2, QuantityPrecision: 4, MinQuantity: d("0.0001"), MinNotional: d("5"), Trading: true}
}

func baseIn(net string) HedgeInput {
	return HedgeInput{Net: d(net), Index: d("50000"), SlippageBps: 50, Rules: btcRules(), MinQty: d("0.0002"),
		MaxOrderUSDT: d("100000"), HourRemaining: d("1000000"), AvailBase: d("100"), AvailQuote: d("10000000")}
}

func TestPlanHedgeSlippageCap(t *testing.T) {
	p := PlanHedge(baseIn("-0.1")) // platform short BTC -> buy
	if p.Side != "buy" || !p.Qty.Equal(d("0.1")) || !p.Price.Equal(d("50250")) {
		t.Fatalf("buy plan %+v", p)
	}
	p = PlanHedge(baseIn("0.1")) // long -> sell
	if p.Side != "sell" || !p.Price.Equal(d("49750")) {
		t.Fatalf("sell plan %+v", p)
	}
	// rounding never loosens the cap: buy floors, sell ceils
	in := baseIn("-0.1")
	in.Index = d("50000.123")
	if p := PlanHedge(in); !p.Price.Equal(d("50250.12")) {
		t.Fatalf("buy tick %s", p.Price)
	}
	in.Net = d("0.1")
	if p := PlanHedge(in); !p.Price.Equal(d("49750.13")) {
		t.Fatalf("sell tick %s", p.Price)
	}
	in = baseIn("-0.1")
	in.SlippageBps = 10
	if p := PlanHedge(in); !p.Price.Equal(d("50050")) {
		t.Fatalf("10bps %s", p.Price)
	}
}

func TestPlanHedgeLimits(t *testing.T) {
	in := baseIn("-1")
	in.MaxOrderUSDT = d("1000") // 1000/50250 = 0.0199004 -> 0.0199
	if p := PlanHedge(in); !p.Qty.Equal(d("0.0199")) {
		t.Fatalf("max order %+v", p)
	}
	in = baseIn("-1")
	in.HourRemaining = d("0")
	if p := PlanHedge(in); p.Side != "" || p.Skip != "budget" {
		t.Fatalf("hour budget %+v", p)
	}
	in = baseIn("-1")
	in.AvailQuote = d("502.5")
	if p := PlanHedge(in); !p.Qty.Equal(d("0.01")) {
		t.Fatalf("funds %+v", p)
	}
	in = baseIn("0.5")
	in.AvailBase = d("0.2")
	if p := PlanHedge(in); !p.Qty.Equal(d("0.2")) {
		t.Fatalf("sell funds %+v", p)
	}
	if p := PlanHedge(baseIn("-0.00015")); p.Side != "" || p.Skip != "below_min" {
		t.Fatalf("per-asset min %+v", p)
	}
	in = baseIn("-1")
	in.Rules.Trading = false
	if p := PlanHedge(in); p.Skip != "symbol_halted" {
		t.Fatalf("halted %+v", p)
	}
}

func TestHedgeStateBackoffAndUnhedgedAlert(t *testing.T) {
	st := &HedgeAssetState{}
	t0 := time.Now()
	for i, want := range []time.Duration{5, 10, 20, 40, 80, 160, 300, 300} {
		st.result(t0, "partial", 5*time.Second, 300*time.Second)
		if got := st.next.Sub(t0); got != want*time.Second {
			t.Fatalf("failure %d: backoff %v want %v", i+1, got, want*time.Second)
		}
	}
	st.result(t0, "filled", 5*time.Second, 300*time.Second)
	if st.Failures != 0 || !st.next.IsZero() {
		t.Fatal("fill must reset backoff")
	}
	thr := d("1000")
	if st.observe(t0, d("5000"), thr, 10) || st.observe(t0.Add(9*time.Minute), d("5000"), thr, 10) {
		t.Fatal("must not alert before N minutes")
	}
	if !st.observe(t0.Add(11*time.Minute), d("5000"), thr, 10) {
		t.Fatal("must alert after N minutes above threshold")
	}
	if st.observe(t0.Add(20*time.Minute), d("5000"), thr, 10) {
		t.Fatal("alert once per episode")
	}
	st.observe(t0.Add(21*time.Minute), d("10"), thr, 10) // back under: reset
	if st.Alerted || st.UnhedgedSince != 0 {
		t.Fatal("reset when hedged")
	}
}

// ---- DB-backed ----

type alertSink struct {
	mu   sync.Mutex
	msgs []string
}

func (a *alertSink) fn(m string) { a.mu.Lock(); a.msgs = append(a.msgs, m); a.mu.Unlock() }
func (a *alertSink) has(sub string) bool {
	a.mu.Lock()
	defer a.mu.Unlock()
	for _, m := range a.msgs {
		if strings.Contains(m, sub) {
			return true
		}
	}
	return false
}

func captureAlerts(t *testing.T) *alertSink {
	a := &alertSink{}
	old := convertAlert
	convertAlert = a.fn
	t.Cleanup(func() { convertAlert = old })
	return a
}

// fakeVenue settles IOC fills against an in-memory "book depth" directly in
// the hedge account balances.
type fakeVenue struct {
	t      *testing.T
	store  *Store
	depth  decimal.Decimal // max base fill per order
	reject string
	orders []string
	prices []decimal.Decimal
	rest   bool // leave the order "new" (resting) after its immediate fill
	open   map[string]HedgeOrderResult
}

func (f *fakeVenue) Order(ctx context.Context, uid, id string) (HedgeOrderResult, error) {
	return f.open[id], nil
}

func (f *fakeVenue) Cancel(ctx context.Context, uid, id string) (HedgeOrderResult, error) {
	o := f.open[id]
	o.Status = "canceled"
	f.open[id] = o
	return o, nil
}

func (f *fakeVenue) Rules(ctx context.Context, symbol string) (SymbolRules, error) {
	r := btcRules()
	r.Symbol = symbol
	return r, nil
}

func (f *fakeVenue) PlaceIOC(ctx context.Context, uid, symbol, side string, price, qty decimal.Decimal, cid string) (HedgeOrderResult, error) {
	f.orders = append(f.orders, symbol+":"+side+":"+qty.String())
	f.prices = append(f.prices, price)
	if f.reject != "" {
		return HedgeOrderResult{}, &errHedgeRejected{code: "ORDER_REJECTED", msg: f.reject}
	}
	fill := decimal.Min(qty, f.depth)
	base := strings.TrimSuffix(symbol, "USDT")
	px := price // fills at the limit (worst case)
	sign := decimal.NewFromInt(1)
	if side == "sell" {
		sign = sign.Neg()
	}
	if fill.Sign() > 0 {
		for _, q := range []struct {
			a string
			v decimal.Decimal
		}{{base, fill.Mul(sign)}, {"USDT", fill.Mul(px).Mul(sign).Neg()}} {
			if _, err := f.store.db.ExecContext(ctx, `INSERT INTO balances (user_id, asset) VALUES ($1,$2) ON CONFLICT DO NOTHING`, uid, q.a); err != nil {
				f.t.Fatal(err)
			}
			if _, err := f.store.db.ExecContext(ctx, `UPDATE balances SET free=free+$3 WHERE user_id=$1 AND asset=$2`, uid, q.a, q.v.String()); err != nil {
				f.t.Fatal(err)
			}
		}
	}
	res := HedgeOrderResult{OrderID: "o" + cid, Status: "canceled", Filled: fill, Quote: fill.Mul(px)}
	if f.rest {
		res.Status = "partially_filled"
		if f.open == nil {
			f.open = map[string]HedgeOrderResult{}
		}
		f.open[res.OrderID] = res
	}
	return res, nil
}

func hedgeTestServer(t *testing.T) (*Server, *fakeVenue, *time.Time) {
	store, _ := newTestStore(t)
	ctx := context.Background()
	now := time.Now()
	clock := &now
	s := &Server{cfg: Config{Convert: ConvertDefaults{SpreadBps: 20, MinUSDT: d("10"), MaxUSDT: d("50000"), QuoteTTL: 10 * time.Second,
		PriceMaxAge: 15 * time.Second, HedgeUserID: "system:hedge-t" + testUser(), HedgeBackoff: 5 * time.Second, HedgeBackoffMax: 300 * time.Second}},
		store: store, now: func() time.Time { return *clock }}
	s.prices = func(ctx context.Context, a string) (decimal.Decimal, time.Time, error) {
		switch a {
		case "USDC":
			return d("1"), *clock, nil
		case "BTC":
			return d("50000"), *clock, nil
		}
		return d("100"), *clock, nil
	}
	fv := &fakeVenue{t: t, store: store, depth: d("1000")}
	s.hedge = &Hedger{venue: fv}
	// Neutralise other assets' global convert positions (shared test DB):
	// only BTC is hedge-enabled for these tests.
	if _, err := store.convertConfig(ctx, s.cfg.Convert); err != nil {
		t.Fatal(err)
	}
	mustExec(t, store, `UPDATE convert_config SET hedge_enabled=true, hedge_max_order_usdt=1000000, hedge_max_hour_usdt=100000000, hedge_unhedged_alert_usdt=1000, hedge_unhedged_alert_minutes=10, hedge_slippage_bps=50`)
	mustExec(t, store, `UPDATE convert_assets SET hedge_enabled=(asset='BTC')`)
	t.Cleanup(func() {
		mustExec(t, store, `UPDATE convert_config SET hedge_enabled=NULL, hedge_max_order_usdt=20000, hedge_max_hour_usdt=200000, hedge_unhedged_alert_usdt=5000`)
		mustExec(t, store, `UPDATE convert_assets SET hedge_enabled=(asset NOT IN ('USDT','USDC'))`)
	})
	return s, fv, clock
}

func mustExec(t *testing.T, s *Store, q string, args ...any) {
	t.Helper()
	if _, err := s.db.ExecContext(context.Background(), q, args...); err != nil {
		t.Fatal(err)
	}
}

// userBuysBTC makes the platform short BTC (and long USDT) via a real convert.
func userBuysBTC(t *testing.T, s *Server, usdt string) {
	ctx := context.Background()
	uid := testUser()
	if err := s.store.Credit(ctx, uid, "USDT", d(usdt), "hedge-seed-"+uid); err != nil {
		t.Fatal(err)
	}
	q, err := s.quote(ctx, uid, quoteReq{From: "USDT", To: "BTC", FromAmount: usdt})
	if err != nil {
		t.Fatal(err)
	}
	if _, _, err := s.store.AcceptQuote(ctx, uid, q.QuoteID, s.now(), func(string, decimal.Decimal) decimal.Decimal { return d(usdt) }, nil); err != nil {
		t.Fatal(err)
	}
}

func btcNet(t *testing.T, s *Server) decimal.Decimal {
	pos, _ := s.store.ConvertPosition(context.Background())
	tot, _ := s.store.accountTotals(context.Background(), s.cfg.Convert.HedgeUserID)
	return pos["BTC"].Add(tot["BTC"])
}

func TestHedgeCycleDB(t *testing.T) {
	s, fv, clock := hedgeTestServer(t)
	alerts := captureAlerts(t)
	ctx := context.Background()
	if err := s.store.EnsureAccount(ctx, s.cfg.Convert.HedgeUserID); err != nil {
		t.Fatal(err)
	}
	userBuysBTC(t, s, "20000")
	start := btcNet(t, s)
	if start.Sign() >= 0 {
		t.Fatalf("expected platform short BTC, net=%s", start)
	}

	// switch off: no orders
	mustExec(t, s.store, `UPDATE convert_config SET hedge_enabled=false`)
	if recs, err := s.runHedgeCycle(ctx, true); err != nil || len(recs) != 0 || len(fv.orders) != 0 {
		t.Fatalf("switch off placed orders: %v %v %v", recs, err, fv.orders)
	}
	mustExec(t, s.store, `UPDATE convert_config SET hedge_enabled=true`)
	mustExec(t, s.store, `UPDATE convert_assets SET hedge_enabled=false WHERE asset='BTC'`)
	if recs, _ := s.runHedgeCycle(ctx, true); len(recs) != 0 {
		t.Fatal("per-asset switch off placed orders")
	}
	mustExec(t, s.store, `UPDATE convert_assets SET hedge_enabled=true WHERE asset='BTC'`)

	// thin depth: partial fill, remainder unhedged, backoff set
	fv.depth = d("0.1")
	recs, err := s.runHedgeCycle(ctx, false)
	if err != nil || len(recs) != 1 {
		t.Fatalf("cycle: %v %v", recs, err)
	}
	r := recs[0]
	if r.Side != "buy" || r.Status != "partial" || !d(r.FilledQty).Equal(d("0.1")) || !d(r.LimitPrice).Equal(d("50250")) {
		t.Fatalf("partial record %+v", r)
	}
	if !d(r.QuoteAmount).Equal(d("5025")) {
		t.Fatalf("quote amount %s", r.QuoteAmount)
	}
	if got := btcNet(t, s); !got.Equal(start.Add(d("0.1"))) {
		t.Fatalf("net after partial %s want %s", got, start.Add(d("0.1")))
	}
	st := s.hedger().snapshot()["BTC"]
	if st.Failures != 1 || st.NextRetryAt == 0 || st.LastStatus != "partial" {
		t.Fatalf("state %+v", st)
	}
	// funding came from the convert USDT position — nothing created
	pos, _ := s.store.ConvertPosition(ctx)
	hb, _ := s.store.accountTotals(ctx, s.cfg.Convert.HedgeUserID)
	if hb["USDT"].IsNegative() || pos["USDT"].Add(hb["USDT"]).IsNegative() {
		t.Fatalf("funding: pos %s hedge %s", pos["USDT"], hb["USDT"])
	}
	// within backoff: no new order
	n := len(fv.orders)
	if recs, _ := s.runHedgeCycle(ctx, false); len(recs) != 0 || len(fv.orders) != n {
		t.Fatal("must respect backoff")
	}
	// unhedged above threshold for > N minutes -> one alert + event
	*clock = clock.Add(11 * time.Minute)
	fv.depth = d("0")
	s.runHedgeCycle(ctx, false) //nolint:errcheck
	if !alerts.has("未对冲敞口") {
		t.Fatalf("unhedged alert missing: %v", alerts.msgs)
	}
	// rejection -> event + alert
	fv.reject = "insufficient depth"
	*clock = clock.Add(time.Hour)
	recs, _ = s.runHedgeCycle(ctx, true)
	if len(recs) != 1 || recs[0].Status != "rejected" || !alerts.has("闪兑对冲失败") {
		t.Fatalf("reject path: %+v %v", recs, alerts.msgs)
	}
	var ev int
	s.store.db.QueryRow(`SELECT count(*) FROM convert_events WHERE kind='hedge_rejected' AND target='BTC' AND created_at > now() - interval '1 minute'`).Scan(&ev) //nolint:errcheck
	if ev < 1 {
		t.Fatalf("hedge_rejected event %d", ev)
	}
	// deep book: fully hedged
	fv.reject, fv.depth = "", d("1000")
	recs, _ = s.runHedgeCycle(ctx, true)
	if len(recs) != 1 || recs[0].Status != "filled" {
		t.Fatalf("fill %+v", recs)
	}
	// residual: buying at the cap (index+50bps) costs slightly more USDT than
	// the convert desk received, so the last ~0.5% is limited by funds.
	if got := btcNet(t, s); got.Abs().GreaterThan(d("0.002")) {
		t.Fatalf("net after full hedge %s", got)
	}
	for _, p := range fv.prices {
		if p.GreaterThan(d("50250")) {
			t.Fatalf("price above slippage cap: %s", p)
		}
	}
	// solvency nets convert vs hedge
	ls, err := s.store.Liabilities(ctx, mmUserID(), s.cfg.Convert.HedgeUserID)
	if err != nil {
		t.Fatal(err)
	}
	for _, l := range ls {
		if l.Asset == "BTC" && !d(l.NetPosition).Equal(d(l.ConvertPosition).Add(d(l.HedgeBalance))) {
			t.Fatalf("liability netting %+v", l)
		}
	}
}

func TestDepegPauseResumeDB(t *testing.T) {
	s, _, clock := hedgeTestServer(t)
	alerts := captureAlerts(t)
	ctx := context.Background()
	usdc := d("1")
	s.prices = func(ctx context.Context, a string) (decimal.Decimal, time.Time, error) {
		if a == "USDC" {
			return usdc, *clock, nil
		}
		return d("50000"), *clock, nil
	}
	mustExec(t, s.store, `DELETE FROM convert_pair_state`)
	uid := testUser()
	if err := s.store.Credit(ctx, uid, "USDC", d("100"), "depeg-"+uid); err != nil {
		t.Fatal(err)
	}
	q, err := s.quote(ctx, uid, quoteReq{From: "USDC", To: "USDT", FromAmount: "50"})
	if err != nil {
		t.Fatal(err)
	}
	usdc = d("0.99")
	_, err = s.quote(ctx, uid, quoteReq{From: "USDT", To: "USDC", FromAmount: "50"})
	var ce *convertErr
	if !errors.As(err, &ce) || ce.code != "PAIR_PAUSED_DEPEG" || ce.status != http.StatusServiceUnavailable {
		t.Fatalf("want PAIR_PAUSED_DEPEG, got %v", err)
	}
	// a quote issued before the pause cannot be accepted during it
	guard := func(f, to string) error { return s.pairGuard(ctx, f, to) }
	if _, _, err := s.store.AcceptQuote(ctx, uid, q.QuoteID, s.now(), func(string, decimal.Decimal) decimal.Decimal { return d("50") }, guard); !errors.As(err, &ce) || ce.code != "PAIR_PAUSED_DEPEG" {
		t.Fatalf("accept during pause: %v", err)
	}
	// other pairs unaffected
	if _, err := s.quote(ctx, uid, quoteReq{From: "USDC", To: "BTC", FromAmount: "50"}); err != nil {
		t.Fatalf("USDC->BTC should not be paused: %v", err)
	}
	if !alerts.has("USDC 脱锚") {
		t.Fatalf("pause alert: %v", alerts.msgs)
	}
	// missing price stays paused
	s.prices = func(ctx context.Context, a string) (decimal.Decimal, time.Time, error) {
		return decimal.Zero, time.Time{}, errors.New("outage")
	}
	if st := s.checkDepeg(ctx); !st.Paused || st.Reason != "price_unavailable" {
		t.Fatalf("missing price %+v", st)
	}
	usdc = d("1.001")
	s.prices = func(ctx context.Context, a string) (decimal.Decimal, time.Time, error) { return usdc, *clock, nil }
	if _, err := s.quote(ctx, uid, quoteReq{From: "USDC", To: "USDT", FromAmount: "50"}); err != nil {
		t.Fatalf("resume: %v", err)
	}
	if !alerts.has("闪兑恢复") {
		t.Fatalf("resume alert: %v", alerts.msgs)
	}
	var paused, resumed int
	s.store.db.QueryRow(`SELECT count(*) FILTER (WHERE kind='pair_paused'), count(*) FILTER (WHERE kind='pair_resumed') FROM convert_events WHERE created_at > now() - interval '1 minute'`).Scan(&paused, &resumed) //nolint:errcheck
	if paused < 1 || resumed < 1 {
		t.Fatalf("events paused=%d resumed=%d", paused, resumed)
	}
}

func TestHedgeRestingCancelDB(t *testing.T) {
	s, fv, clock := hedgeTestServer(t)
	alerts := captureAlerts(t)
	ctx := context.Background()
	mustExec(t, s.store, `UPDATE convert_hedges SET status='canceled' WHERE status='resting'`)
	if err := s.store.EnsureAccount(ctx, s.cfg.Convert.HedgeUserID); err != nil {
		t.Fatal(err)
	}
	userBuysBTC(t, s, "5000")
	fv.rest, fv.depth = true, d("0.02")
	recs, _ := s.runHedgeCycle(ctx, true)
	if len(recs) != 1 || recs[0].Status != "resting" {
		t.Fatalf("want resting, got %+v", recs)
	}
	n := len(fv.orders)
	// before cancelAfterSeconds (30): no cancel, no second order for the asset
	*clock = clock.Add(10 * time.Second)
	if recs, _ := s.runHedgeCycle(ctx, true); len(recs) != 0 || len(fv.orders) != n {
		t.Fatalf("must wait on the live order: %+v", recs)
	}
	if st := s.hedger().snapshot()["BTC"]; st.LastSkip != "order_resting" {
		t.Fatalf("skip %q", st.LastSkip)
	}
	// after 30s: canceled, remainder re-hedged in the same cycle
	*clock = clock.Add(25 * time.Second)
	fv.rest, fv.depth = false, d("1000")
	recs, _ = s.runHedgeCycle(ctx, true)
	if len(recs) != 2 || recs[0].Status != "canceled" || !d(recs[0].FilledQty).Equal(d("0.02")) || recs[1].Status != "filled" {
		t.Fatalf("cancel + rehedge: %+v", recs)
	}
	if !alerts.has("闪兑对冲撤单") {
		t.Fatalf("cancel alert: %v", alerts.msgs)
	}
	var st string
	s.store.db.QueryRow(`SELECT status FROM convert_hedges WHERE id=$1`, recs[0].ID).Scan(&st) //nolint:errcheck
	if st != "canceled" {
		t.Fatalf("db status %q", st)
	}
}

func TestUSDCRealIndexPricingDB(t *testing.T) {
	s, _, clock := hedgeTestServer(t)
	ctx := context.Background()
	mustExec(t, s.store, `DELETE FROM convert_pair_state`)
	s.depeg = depegGuard{}
	usdc, at := d("0.98"), *clock
	s.prices = func(ctx context.Context, a string) (decimal.Decimal, time.Time, error) {
		if a == "USDC" {
			return usdc, at, nil
		}
		return d("50000"), *clock, nil
	}
	uid := testUser()
	q, err := s.quote(ctx, uid, quoteReq{From: "USDC", To: "BTC", FromAmount: "1000"})
	if err != nil {
		t.Fatal(err)
	}
	// 1000 USDC * 0.98 / 50000 * (1 - 20bps) = 0.0195608
	if !d(q.IndexRate).Equal(d("0.0000196")) || !d(q.ToAmount).Equal(d("0.0195608")) {
		t.Fatalf("USDC must use its index: %+v", q)
	}
	at = clock.Add(-time.Minute) // stale
	for _, p := range [][2]string{{"USDC", "BTC"}, {"BTC", "USDC"}} {
		_, err := s.quote(ctx, uid, quoteReq{From: p[0], To: p[1], FromAmount: "1"})
		var ce *convertErr
		if !errors.As(err, &ce) || ce.code != "PRICE_UNAVAILABLE" {
			t.Fatalf("%v stale USDC: %v", p, err)
		}
	}
	_, err = s.quote(ctx, uid, quoteReq{From: "USDC", To: "USDT", FromAmount: "100"})
	var ce *convertErr
	if !errors.As(err, &ce) || ce.code != "PAIR_PAUSED_DEPEG" {
		t.Fatalf("USDC/USDT keeps depeg pause: %v", err)
	}
}
