package main

import "testing"

func TestComputeSolvency(t *testing.T) {
	liabs := []liabItem{
		{Asset: "USDT", Total: "10000500", MarketMaker: "10000000", VirtualSeed: "10000000"}, // spot incl. virtual seed
		{Asset: "USDT", Total: "200", MarketMaker: "0"},                                      // futures margin
		{Asset: "BTC", Total: "1.5", MarketMaker: "0.5"},
	}
	rep := ComputeSolvency(map[string]string{"USDT": "700", "BTC": "1.5"}, liabs, nil)
	if !rep.OK {
		t.Fatalf("expected ok: %+v", rep)
	}
	rep = ComputeSolvency(map[string]string{"USDT": "699", "BTC": "1.5"}, liabs, nil)
	if rep.OK || rep.Lines[1].Asset != "USDT" || rep.Lines[1].OK || rep.Lines[1].Surplus != "-1" {
		t.Fatalf("expected USDT shortfall: %+v", rep)
	}
	rep = ComputeSolvency(map[string]string{"USDT": "1e9", "BTC": "9"}, liabs, []string{"chain down"})
	if rep.OK {
		t.Fatal("source errors must fail the check")
	}
}

func TestSolvencyFlagsAdminAdjust(t *testing.T) {
	rep := ComputeSolvency(map[string]string{"USDT": "100"}, []liabItem{{Asset: "USDT", Total: "100", AdminAdjustNet: "40"}}, nil)
	l := rep.Lines[0]
	if !rep.OK || l.AdminAdjustNet != "40" || !l.UnbackedAdjust || l.Required != "100" {
		t.Fatalf("adjust must stay in required and be flagged: %+v", l)
	}
	rep = ComputeSolvency(map[string]string{"USDT": "100"}, []liabItem{{Asset: "USDT", Total: "100", AdminAdjustNet: "-5"}}, nil)
	if rep.Lines[0].UnbackedAdjust {
		t.Fatal("net debit is not an unbacked credit")
	}
}
