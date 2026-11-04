package main

import "testing"

// Whole-position TP/SL is replace-semantics: setting it again cancels the
// previous active pair instead of stacking a second one. Partial (qty) TP/SL
// orders are left alone.
func TestAdvancedTPSLReplace(t *testing.T) {
	s, ctx := advServer(t)
	place(t, s, ctx, "u:a", PlaceReq{Side: "sell", Type: "limit", Price: "100000", Qty: "0.02"})
	place(t, s, ctx, "u:b", PlaceReq{Side: "buy", Type: "market", Qty: "0.02"})
	active := func(qtyNull bool) int {
		var n int
		q := `SELECT count(*) FROM fut_algo_orders WHERE user_id='u:b' AND status='active' AND kind IN ('tp','sl') AND qty IS NULL`
		if !qtyNull {
			q = `SELECT count(*) FROM fut_algo_orders WHERE user_id='u:b' AND status='active' AND kind IN ('tp','sl') AND qty IS NOT NULL`
		}
		if err := s.db.QueryRowContext(ctx, q).Scan(&n); err != nil {
			t.Fatal(err)
		}
		return n
	}
	if _, err := s.CreateTPSL(ctx, "u:b", TPSLReq{Symbol: "BTCUSDT", PositionSide: "BOTH", Qty: "0.005", TPTrigger: "103000"}); err != nil {
		t.Fatal(err)
	}
	if _, err := s.CreateTPSL(ctx, "u:b", TPSLReq{Symbol: "BTCUSDT", PositionSide: "BOTH", TPTrigger: "101000", SLTrigger: "95000"}); err != nil {
		t.Fatal(err)
	}
	if _, err := s.CreateTPSL(ctx, "u:b", TPSLReq{Symbol: "BTCUSDT", PositionSide: "BOTH", TPTrigger: "102000", SLTrigger: "96000"}); err != nil {
		t.Fatal(err)
	}
	if n := active(true); n != 2 {
		t.Fatalf("whole-position tp/sl active=%d want 2 (replaced)", n)
	}
	if n := active(false); n != 1 {
		t.Fatalf("partial tp active=%d want 1 (untouched)", n)
	}
	var tp string
	if err := s.db.QueryRowContext(ctx, `SELECT trigger_price::text FROM fut_algo_orders WHERE user_id='u:b' AND status='active' AND kind='tp' AND qty IS NULL`).Scan(&tp); err != nil {
		t.Fatal(err)
	}
	if tp != "102000" && tp[:6] != "102000" {
		t.Fatalf("tp trigger=%s want 102000", tp)
	}
}
