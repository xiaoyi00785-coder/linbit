package main

import (
	"testing"

	"github.com/shopspring/decimal"
)

func TestCloseOrderSide(t *testing.T) {
	if closeOrderSide(decimal.NewFromInt(1)) != "sell" {
		t.Fatal("long")
	}
	if closeOrderSide(decimal.NewFromInt(-1)) != "buy" {
		t.Fatal("short")
	}
}

func TestSettleMirrorCloseDelta(t *testing.T) {
	rp := decimal.RequireFromString("-80")
	fees := decimal.RequireFromString("10")
	trueNet := money(rp.Sub(fees))
	userNet := mirrorUserNet(rp, fees, decimal.Zero)
	adj := money(userNet.Sub(trueNet))
	if !userNet.Equal(decimal.RequireFromString("70")) {
		t.Fatalf("userNet %s", userNet)
	}
	if !adj.Equal(decimal.RequireFromString("160")) {
		t.Fatalf("adj %s", adj)
	}
}

func TestApplyHistoryMirrorNet(t *testing.T) {
	h := PosHistory{Direction: "long", Realized: "-80", Fees: "10", Funding: "0", Net: "-90"}
	applyHistoryMirror(&h)
	if h.Direction != "short" || !h.Mirrored {
		t.Fatalf("dir %+v", h)
	}
	if feeStr(decimal.RequireFromString("80")) != h.Realized || feeStr(decimal.RequireFromString("70")) != h.Net {
		t.Fatalf("%+v", h)
	}
}

func TestFlipPosViewCloseSide(t *testing.T) {
	v := PosView{
		Symbol: "BTCUSDT", PosSide: "BOTH", Side: "long", UPnL: "-50", ROE: "-0.05",
		pos: Position{Qty: decimal.NewFromInt(2)},
		upnl: decimal.RequireFromString("-50"),
	}
	flipPosView(&v)
	if !v.Mirrored || v.CloseSide != "sell" || v.Side != "short" || v.UPnL != "50" {
		t.Fatalf("%+v", v)
	}
}