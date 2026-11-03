package main

import (
	"context"
	"errors"
	"net/http"
	"sync"
	"testing"
	"time"

	"github.com/shopspring/decimal"
)

func testCfg() ConvertConfig {
	return ConvertConfig{SpreadBps: 20, MinUSDT: "10", MaxUSDT: "50000"}
}

func TestPriceQuoteFromAmount(t *testing.T) {
	usdt, btc := ConvertAssetConf{"USDT", 6, true, false, "0"}, ConvertAssetConf{"BTC", 8, true, false, "0"}
	fa, ta, rate, idx, sp, err := PriceQuote(testCfg(), usdt, btc, d("1"), d("50000"), d("1000"), decimal.Zero)
	if err != nil {
		t.Fatal(err)
	}
	if !fa.Equal(d("1000")) || !idx.Equal(d("0.00002")) || sp != 20 {
		t.Fatalf("fa=%s idx=%s sp=%d", fa, idx, sp)
	}
	// 1000/50000 * 0.998 = 0.01996
	if !ta.Equal(d("0.01996")) || !rate.Equal(d("0.00001996")) {
		t.Fatalf("ta=%s rate=%s", ta, rate)
	}
}

func TestPriceQuoteToAmountAndCross(t *testing.T) {
	btc, eth := ConvertAssetConf{"BTC", 8, true, false, "0"}, ConvertAssetConf{"ETH", 8, true, false, "0"}
	fa, ta, _, idx, _, err := PriceQuote(testCfg(), btc, eth, d("50000"), d("2500"), decimal.Zero, d("1"))
	if err != nil {
		t.Fatal(err)
	}
	if !idx.Equal(d("20")) || !ta.Equal(d("1")) {
		t.Fatalf("idx=%s ta=%s", idx, ta)
	}
	// fa = 1 / (20*0.998) rounded up to 8dp
	if want := d("0.05010021"); !fa.Equal(want) {
		t.Fatalf("fa=%s want %s", fa, want)
	}
	if fa.Mul(d("19.96")).LessThan(ta) {
		t.Fatal("platform must not lose on rounding")
	}
}

func TestPriceQuoteStableAndLimits(t *testing.T) {
	usdt, usdc := ConvertAssetConf{"USDT", 6, true, false, "0"}, ConvertAssetConf{"USDC", 6, true, false, "0"}
	_, ta, _, _, sp, err := PriceQuote(testCfg(), usdc, usdt, d("1"), d("1"), d("100"), decimal.Zero)
	if err != nil || !ta.Equal(d("100")) || sp != 0 {
		t.Fatalf("stable: ta=%s sp=%d err=%v", ta, sp, err)
	}
	for amt, code := range map[string]string{"9.99": "AMOUNT_TOO_SMALL", "50000.01": "AMOUNT_TOO_LARGE"} {
		_, _, _, _, _, err := PriceQuote(testCfg(), usdc, usdt, d("1"), d("1"), d(amt), decimal.Zero)
		var ce *convertErr
		if !errors.As(err, &ce) || ce.code != code {
			t.Fatalf("%s: want %s got %v", amt, code, err)
		}
	}
}

func TestQuoteStalePrice(t *testing.T) {
	now := time.Now()
	s := &Server{cfg: Config{Convert: ConvertDefaults{PriceMaxAge: 15 * time.Second}}, now: func() time.Time { return now },
		prices: func(ctx context.Context, a string) (decimal.Decimal, time.Time, error) {
			return d("50000"), now.Add(-time.Minute), nil
		}}
	_, err := s.usdtPrice(context.Background(), "BTC")
	var ce *convertErr
	if !errors.As(err, &ce) || ce.code != "PRICE_UNAVAILABLE" || ce.status != http.StatusServiceUnavailable {
		t.Fatalf("want PRICE_UNAVAILABLE, got %v", err)
	}
}

// DB-backed: atomic accept, idempotent replay, expiry, insufficient, concurrency.
func TestConvertAcceptDB(t *testing.T) {
	store, _ := newTestStore(t)
	ctx := context.Background()
	now := time.Now()
	s := &Server{cfg: Config{Convert: ConvertDefaults{SpreadBps: 20, MinUSDT: d("10"), MaxUSDT: d("50000"), QuoteTTL: 10 * time.Second, PriceMaxAge: 15 * time.Second}},
		store: store, now: func() time.Time { return now },
		prices: func(ctx context.Context, a string) (decimal.Decimal, time.Time, error) { return d("50000"), now, nil }}
	uid := testUser()
	if err := store.Credit(ctx, uid, "USDT", d("1500"), "cv-seed-"+uid); err != nil {
		t.Fatal(err)
	}
	q, err := s.quote(ctx, uid, quoteReq{From: "USDT", To: "BTC", FromAmount: "1000"})
	if err != nil {
		t.Fatal(err)
	}
	val := func(string, decimal.Decimal) decimal.Decimal { return d("1000") }
	before, _ := store.ConvertPosition(ctx)
	var wg sync.WaitGroup
	replays := make([]bool, 5)
	for i := range replays {
		wg.Add(1)
		go func(i int) {
			defer wg.Done()
			_, rp, err := store.AcceptQuote(ctx, uid, q.QuoteID, now, val, nil)
			if err != nil {
				t.Errorf("accept %d: %v", i, err)
			}
			replays[i] = rp
		}(i)
	}
	wg.Wait()
	fresh := 0
	for _, r := range replays {
		if !r {
			fresh++
		}
	}
	if fresh != 1 {
		t.Fatalf("exactly one fill expected, got %d", fresh)
	}
	bals, _, _ := store.Balances(ctx, uid, 1, 50)
	if u := findAsset(bals, "USDT"); !u.Free.Equal(d("500")) {
		t.Fatalf("USDT %s", u.Free)
	}
	if b := findAsset(bals, "BTC"); !b.Free.Equal(d("0.01996")) {
		t.Fatalf("BTC %s", b.Free)
	}
	after, _ := store.ConvertPosition(ctx)
	if !after["USDT"].Sub(before["USDT"]).Equal(d("1000")) || !after["BTC"].Sub(before["BTC"]).Equal(d("-0.01996")) {
		t.Fatalf("position %v -> %v", before, after)
	}
	// insufficient
	q2, _ := s.quote(ctx, uid, quoteReq{From: "USDT", To: "BTC", FromAmount: "600"})
	if _, _, err := store.AcceptQuote(ctx, uid, q2.QuoteID, now, val, nil); !errors.Is(err, ErrInsufficient) {
		t.Fatalf("want insufficient, got %v", err)
	}
	// expired
	q3, _ := s.quote(ctx, uid, quoteReq{From: "USDT", To: "BTC", FromAmount: "100"})
	if _, _, err := store.AcceptQuote(ctx, uid, q3.QuoteID, now.Add(11*time.Second), val, nil); !errors.Is(err, ErrQuoteExpired) {
		t.Fatalf("want expired, got %v", err)
	}
	// someone else's quote
	if _, _, err := store.AcceptQuote(ctx, "other-"+uid, q3.QuoteID, now, val, nil); !errors.Is(err, ErrQuoteNotFound) {
		t.Fatalf("want not found, got %v", err)
	}
}
