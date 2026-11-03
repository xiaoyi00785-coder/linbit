package main

import (
	"testing"
	"time"

	"github.com/shopspring/decimal"
)

var testSym = &SymbolConfig{
	Symbol: "BTCUSDT", Base: "BTC", Quote: "USDT",
	PricePrecision: 2, QuantityPrecision: 6,
}

// TestToOrderDTOPrecision checks price/quantity are rendered as fixed-precision
// decimal strings scaled to the symbol rules (price_precision=2, qty=6).
func TestToOrderDTOPrecision(t *testing.T) {
	o := &Order{
		OrderID: "1", Symbol: "BTCUSDT", Side: "buy", Type: "limit", TimeInForce: "GTC",
		Price: decimal.RequireFromString("70000"), HasPrice: true,
		Quantity:       decimal.RequireFromString("0.01"),
		FilledQuantity: decimal.RequireFromString("0.004"),
		Status:         "partially_filled", CreatedAt: time.Unix(0, 0).UTC(),
	}
	d := toOrderDTO(o, testSym)
	if d.Quantity != "0.010000" {
		t.Fatalf("quantity = %q, want 0.010000", d.Quantity)
	}
	if d.FilledQuantity != "0.004000" {
		t.Fatalf("filledQuantity = %q, want 0.004000", d.FilledQuantity)
	}
	if d.Price == nil || *d.Price != "70000.00" {
		t.Fatalf("price = %v, want 70000.00", d.Price)
	}
}

// TestToOrderDTOQuoteBudget checks a market-buy quote budget is scaled to
// price_precision (it is quote-denominated).
func TestToOrderDTOQuoteBudget(t *testing.T) {
	o := &Order{
		OrderID: "2", Symbol: "BTCUSDT", Side: "buy", Type: "market", TimeInForce: "IOC",
		Quantity: decimal.Zero, FilledQuantity: decimal.Zero,
		QuoteOrderQty: decimal.RequireFromString("100.5"), HasQuoteQty: true,
		Status: "filled", CreatedAt: time.Unix(0, 0).UTC(),
	}
	d := toOrderDTO(o, testSym)
	if d.QuoteOrderQty == nil || *d.QuoteOrderQty != "100.50" {
		t.Fatalf("quoteOrderQty = %v, want 100.50", d.QuoteOrderQty)
	}
}

// TestToTradeDTOPrecision checks trade price/quantity/fee scaling, including
// fee scaled to the precision of its charging asset (base vs quote).
func TestToTradeDTOPrecision(t *testing.T) {
	baseFee := toTradeDTO(TradeRow{
		TradeID: "t1", OrderID: "1", Symbol: "BTCUSDT", Side: "buy",
		Price: decimal.RequireFromString("70000"), Quantity: decimal.RequireFromString("0.01"),
		Fee: decimal.RequireFromString("0.00001"), FeeAsset: "BTC", Role: "taker",
		CreatedAt: time.Unix(0, 0).UTC(),
	}, testSym)
	if baseFee.Price != "70000.00" || baseFee.Quantity != "0.010000" {
		t.Fatalf("price/qty = %q/%q", baseFee.Price, baseFee.Quantity)
	}
	if baseFee.Fee == nil || *baseFee.Fee != "0.000010" { // BTC -> qty precision 6
		t.Fatalf("base fee = %v, want 0.000010", baseFee.Fee)
	}

	quoteFee := toTradeDTO(TradeRow{
		TradeID: "t2", OrderID: "2", Symbol: "BTCUSDT", Side: "sell",
		Price: decimal.RequireFromString("70000"), Quantity: decimal.RequireFromString("0.01"),
		Fee: decimal.RequireFromString("0.7"), FeeAsset: "USDT", Role: "maker",
		CreatedAt: time.Unix(0, 0).UTC(),
	}, testSym)
	if quoteFee.Fee == nil || *quoteFee.Fee != "0.70" { // USDT -> price precision 2
		t.Fatalf("quote fee = %v, want 0.70", quoteFee.Fee)
	}
}

// TestFmtFallbackNilSymbol ensures a nil symbol config falls back to raw strings.
func TestFmtFallbackNilSymbol(t *testing.T) {
	if got := fmtQty(decimal.RequireFromString("1.23"), nil); got != "1.23" {
		t.Fatalf("fmtQty nil = %q, want 1.23", got)
	}
	if got := fmtPrice(decimal.RequireFromString("1.23"), nil); got != "1.23" {
		t.Fatalf("fmtPrice nil = %q, want 1.23", got)
	}
}
