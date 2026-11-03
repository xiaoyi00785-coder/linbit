package main

import (
	"testing"

	"github.com/shopspring/decimal"
)

func TestPointsRules(t *testing.T) {
	p := PointsRules{VolumeTiers: parseVolumeTiers(defaultVolumeTiers), DepositUnit: decimal.NewFromInt(100), DepositCap: 10, InvitePoints: 8, InviteCap: 80,
		Redeem: parseRedeem(defaultRedeem)}
	for _, c := range []struct {
		vol  string
		want int
	}{{"1999", 0}, {"2000", 1}, {"100000", 5}, {"1000000", 33}, {"10000000", 168}, {"99999999", 168}} {
		if got := p.volumePoints(decimal.RequireFromString(c.vol)); got != c.want {
			t.Errorf("volume %s: %d want %d", c.vol, got, c.want)
		}
	}
	if p.depositPoints(decimal.NewFromInt(350)) != 3 || p.depositPoints(decimal.NewFromInt(5000)) != 10 {
		t.Error("deposit points")
	}
	if p.invitePoints(3) != 24 || p.invitePoints(11) != 80 {
		t.Error("invite points")
	}
	if len(p.Redeem) != 8 || p.Redeem[7].Points != 300 || p.Redeem[7].Kind != "trial" || !p.Redeem[7].Amount.Equal(decimal.NewFromInt(100)) {
		t.Errorf("redeem: %+v", p.Redeem)
	}
}
