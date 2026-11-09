package main

import (
	"context"
	"net/http"
	"net/http/httptest"
	"testing"
	"time"

	"github.com/shopspring/decimal"
)

type stubFut struct {
	mark decimal.Decimal
	miss bool
}

func (s stubFut) Specs(context.Context) ([]FutSpec, error) {
	// Figures match the futures seed in fut_contracts / fut_risk_tiers.
	// Production reads them over HTTP; this stub does not invent a second spec.
	sp := FutSpec{
		Symbol: "BTCUSDX", LiveSymbol: "BTCUSDT", Base: "BTC", Quote: "USDX",
		TickSize: decimal.RequireFromString("0.1"), MinQty: decimal.RequireFromString("0.0001"),
		MaxQty: decimal.RequireFromString("100"), MMR: decimal.RequireFromString("0.004"),
		MaxLeverage: 200, Available: !s.miss, PriceSource: "futures_mark",
	}
	if s.miss {
		sp.UnavailableReason = "真实标记价格不可用"
	}
	out := []FutSpec{sp}
	for _, base := range []string{"ETH", "BNB", "ARB", "XRP", "SOL"} {
		out = append(out, FutSpec{Symbol: base + "USDX", LiveSymbol: base + "USDT", Base: base, Quote: "USDX", Available: false, UnavailableReason: "真实永续合约源没有该交易对或行情服务不可达"})
	}
	return out, nil
}

func (s stubFut) Quote(context.Context, string) (decimal.Decimal, decimal.Decimal, *decimal.Decimal, bool, error) {
	if s.miss || !s.mark.IsPositive() {
		return decimal.Zero, decimal.Zero, nil, false, nil
	}
	return s.mark, s.mark, nil, true, nil
}

func TestPaperFuturesOrderLiquidationIsolation(t *testing.T) {
	store := migratedStore(t)
	ctx := context.Background()
	if _, err := store.db.Exec(`CREATE TABLE fut_accounts (user_id TEXT PRIMARY KEY, balance NUMERIC NOT NULL DEFAULT 0)`); err != nil {
		t.Fatal(err)
	}
	if _, err := store.db.Exec(`INSERT INTO fut_accounts (user_id, balance) VALUES ('80000001', 999)`); err != nil {
		t.Fatal(err)
	}
	src := stubFut{mark: decimal.RequireFromString("100000")}
	uid := "80000001"
	out, err := store.PlaceFutOrder(ctx, uid, futOrderIn{
		Symbol: "BTCUSDX", Side: "buy", Action: "open", Type: "market", SizeMode: "quantity",
		Quantity: decimal.RequireFromString("0.01"), Leverage: 10, MarginMode: "isolated", ClientOrderID: "pf-1",
	}, src)
	if err != nil {
		t.Fatal(err)
	}
	if out["status"] != "filled" {
		t.Fatalf("status %#v", out)
	}
	var bal string
	if err := store.db.QueryRow(`SELECT balance::text FROM paper_fut_accounts WHERE user_id=$1`, uid).Scan(&bal); err != nil {
		t.Fatal(err)
	}
	if bal == "10000.000000000000000000" {
		t.Fatal("isolated margin was not reserved")
	}
	var live string
	if err := store.db.QueryRow(`SELECT balance::text FROM fut_accounts WHERE user_id=$1`, uid).Scan(&live); err != nil {
		t.Fatal(err)
	}
	if live != "999" {
		t.Fatalf("live fut_accounts changed: %s", live)
	}
	hit, err := store.LiquidateAt(ctx, uid, "BTCUSDX", "BOTH", decimal.RequireFromString("1000"), decimal.RequireFromString("0.004"))
	if err != nil || !hit {
		t.Fatalf("liquidate %v %v", hit, err)
	}
	hit, err = store.LiquidateAt(ctx, uid, "BTCUSDX", "BOTH", decimal.RequireFromString("1000"), decimal.RequireFromString("0.004"))
	if err != nil || hit {
		t.Fatalf("second liquidate should no-op: %v %v", hit, err)
	}
	var n int
	if err := store.db.QueryRow(`SELECT count(*) FROM paper_fut_ledger WHERE user_id=$1 AND kind='liquidation'`, uid).Scan(&n); err != nil || n != 1 {
		t.Fatalf("liquidation ledger %d %v", n, err)
	}
	if _, err := store.FutClaim(ctx, uid, decimal.RequireFromString("100000.1")); err == nil {
		t.Fatal("claim above cap accepted")
	}
	if err := store.ApplyFunding(ctx, uid, "BTCUSDX", decimal.RequireFromString("0.0001"), time.Unix(1, 0), false); err != nil {
		t.Fatal(err)
	}
	if err := store.db.QueryRow(`SELECT count(*) FROM paper_fut_ledger WHERE kind='funding'`).Scan(&n); err != nil || n != 0 {
		t.Fatalf("unavailable funding wrote a row: %d %v", n, err)
	}
}

func TestPaperFuturesLimitRestsUntilMarkCrosses(t *testing.T) {
	store := migratedStore(t)
	ctx := context.Background()
	src := stubFut{mark: decimal.RequireFromString("100000")}
	out, err := store.PlaceFutOrder(ctx, "80000001", futOrderIn{
		Symbol: "BTCUSDX", Side: "buy", Action: "open", Type: "limit", SizeMode: "quantity",
		Quantity: decimal.RequireFromString("0.01"), Price: decimal.RequireFromString("90000"),
		Leverage: 10, MarginMode: "cross", ClientOrderID: "pf-limit",
	}, src)
	if err != nil || out["status"] != "open" {
		t.Fatalf("rest %#v %v", out, err)
	}
	src.mark = decimal.RequireFromString("89000")
	sp, err := store.specBySymbol(ctx, src, "BTCUSDX")
	if err != nil {
		t.Fatal(err)
	}
	if err := store.matchResting(ctx, "80000001", "BTCUSDX", src.mark, sp); err != nil {
		t.Fatal(err)
	}
	var status string
	if err := store.db.QueryRow(`SELECT status FROM paper_fut_orders WHERE client_order_id='pf-limit'`).Scan(&status); err != nil || status != "filled" {
		t.Fatalf("status %s %v", status, err)
	}
}

func TestLiveFuturesMarksMissingSymbolUnavailable(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		switch r.URL.Path {
		case "/futures/market/contracts":
			w.Write([]byte(`{"contracts":[{"symbol":"BTCUSDT","baseAsset":"BTC","tickSize":"0.1","minQty":"0.0001","maxQty":"100","maintenanceMarginRate":"0.004","maxLeverage":200,"makerFeeRate":"0.0005","takerFeeRate":"0.0005","fundingIntervalHours":8,"status":"trading"}]}`))
		case "/futures/market/risk-tiers":
			w.Write([]byte(`{"contracts":[{"symbol":"BTCUSDT","tiers":[{"tier":1,"maxNotional":"50000","maintenanceMarginRate":"0.004","maintenanceAmount":"0","maxLeverage":200}]}]}`))
		case "/futures/market/ticker":
			w.Write([]byte(`{"markPrice":"100000","indexPrice":"100010","priceAvailable":true}`))
		case "/futures/market/funding-history":
			w.Write([]byte(`{"symbol":"BTCUSDT","history":[]}`))
		default:
			http.NotFound(w, r)
		}
	}))
	defer srv.Close()
	src := NewLiveFutures(srv.URL)
	specs, err := src.Specs(context.Background())
	if err != nil {
		t.Fatal(err)
	}
	if len(specs) != 6 {
		t.Fatalf("specs %d", len(specs))
	}
	if !specs[0].Available || specs[0].Symbol != "BTCUSDX" || specs[0].FundingAvailable {
		t.Fatalf("btc %#v", specs[0])
	}
	if specs[0].Tiers == nil {
		t.Fatal("tiers dropped")
	}
	for _, sp := range specs[1:] {
		if sp.Available || sp.UnavailableReason == "" {
			t.Fatalf("missing source invented: %#v", sp)
		}
	}
}
