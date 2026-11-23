package main

import (
	"encoding/json"
	"os"
	"testing"
	"time"

	"github.com/shopspring/decimal"
)

// TestAdvancedMMLargeSweep replays the market maker's depth-mode book (dumped by
// the marketmaker simulation) into the real matching engine and sends 20 and 50
// BTC market orders. Needs FUTURES_TEST_DATABASE_URL and MM_SIM_QUOTES.
func TestAdvancedMMLargeSweep(t *testing.T) {
	path := os.Getenv("MM_SIM_QUOTES")
	if path == "" {
		t.Skip("MM_SIM_QUOTES not set")
	}
	s, ctx := advServer(t)
	raw, err := os.ReadFile(path)
	if err != nil {
		t.Fatal(err)
	}
	var feed struct {
		Mark   decimal.Decimal `json:"mark"`
		Quotes []struct {
			Side  string          `json:"side"`
			Price decimal.Decimal `json:"price"`
			Qty   decimal.Decimal `json:"qty"`
		} `json:"quotes"`
	}
	if err := json.Unmarshal(raw, &feed); err != nil {
		t.Fatal(err)
	}
	s.setMark("BTCUSDT", feed.Mark.String())
	for _, u := range []string{"mm:main", "taker:buy", "taker:sell"} {
		if _, err := s.db.Exec(`INSERT INTO fut_accounts (user_id, balance) VALUES ($1, 3000000) ON CONFLICT (user_id) DO UPDATE SET balance = 3000000`, u); err != nil {
			t.Fatal(err)
		}
	}
	refill := func() time.Duration {
		t0 := time.Now()
		rows, err := s.db.Query(`SELECT id FROM fut_orders WHERE user_id='mm:main' AND status IN ('new','partially_filled')`)
		if err != nil {
			t.Fatal(err)
		}
		var ids []int64
		for rows.Next() {
			var id int64
			_ = rows.Scan(&id)
			ids = append(ids, id)
		}
		rows.Close()
		for _, id := range ids {
			if _, err := s.CancelOrder(ctx, "mm:main", id); err != nil {
				t.Fatalf("cancel %d: %v", id, err)
			}
		}
		for _, q := range feed.Quotes {
			if _, _, err := s.PlaceOrder(ctx, "mm:main", PlaceReq{Symbol: "BTCUSDT", Side: q.Side, Type: "limit", TIF: "GTC", Price: q.Price.String(), Qty: q.Qty.String()}); err != nil {
				t.Fatalf("mm place %s %s@%s: %v", q.Side, q.Qty, q.Price, err)
			}
		}
		return time.Since(t0)
	}
	market := func(uid, side, qty string) decimal.Decimal {
		t1 := time.Now()
		o, trades, err := s.PlaceOrder(ctx, uid, PlaceReq{Symbol: "BTCUSDT", Side: side, Type: "market", Qty: qty})
		dur := time.Since(t1)
		if err != nil {
			t.Fatalf("%s %s: %v", side, qty, err)
		}
		filled, cost := decimal.Zero, decimal.Zero
		for _, tr := range trades {
			filled, cost = filled.Add(tr.Qty), cost.Add(tr.Qty.Mul(tr.Price))
		}
		vwap := decimal.Zero
		if filled.IsPositive() {
			vwap = cost.Div(filled)
		}
		slip := vwap.Sub(feed.Mark).Abs().Div(feed.Mark).Mul(decimal.NewFromInt(10000))
		t.Logf("market %-4s %s BTC: filled %s in %d trades, vwap %s, slippage %s bps vs mark, status %s, engine %v",
			side, qty, filled, len(trades), vwap.StringFixed(2), slip.StringFixed(2), o.Status, dur)
		return filled
	}
	for _, c := range []struct{ uid, side, qty string }{{"taker:buy", "buy", "20"}, {"taker:sell", "sell", "20"}, {"taker:buy", "buy", "50"}, {"taker:sell", "sell", "50"}} {
		d := refill()
		t.Logf("refill (cancel + re-place %d orders, engine only) %v", len(feed.Quotes), d)
		if f := market(c.uid, c.side, c.qty); !f.Equal(decimal.RequireFromString(c.qty)) {
			t.Errorf("%s %s not fully filled after refill: %s", c.side, c.qty, f)
		}
	}
	// back-to-back with no refill in between: shows the per-side budget
	refill()
	t.Log("back-to-back, no refill between:")
	a := market("taker:buy", "buy", "20")
	b := market("taker:buy", "buy", "50")
	t.Logf("back-to-back total %s of 70 BTC (one side rests ~depth_target..1.5x target)", a.Add(b))
}
