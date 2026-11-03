package main

import (
	"context"

	"github.com/shopspring/decimal"
)

// tiers.go: tiered maintenance margin. Brackets come from fut_risk_tiers.

type Tier struct {
	Tier        int             `json:"tier"`
	MaxNotional decimal.Decimal `json:"maxNotional"`
	MMR         decimal.Decimal `json:"maintenanceMarginRate"`
	MaintAmount decimal.Decimal `json:"maintenanceAmount"`
	MaxLeverage int             `json:"maxLeverage"`
}

func loadTiers(ctx context.Context, db queryer, sym string) ([]Tier, error) {
	rows, err := db.QueryContext(ctx, `SELECT tier, max_notional, mmr, maint_amount, max_leverage FROM fut_risk_tiers WHERE symbol=$1 ORDER BY tier`, sym)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	var out []Tier
	for rows.Next() {
		var t Tier
		if err := rows.Scan(&t.Tier, &t.MaxNotional, &t.MMR, &t.MaintAmount, &t.MaxLeverage); err != nil {
			return nil, err
		}
		out = append(out, t)
	}
	return out, rows.Err()
}

// tierFor returns the bracket containing notional (the last bracket beyond the
// table). Without tiers the flat contract MMR applies.
func (c Contract) tierFor(notional decimal.Decimal) Tier {
	if len(c.Tiers) == 0 {
		return Tier{MMR: c.MMR, MaxLeverage: c.MaxLeverage}
	}
	for _, t := range c.Tiers {
		if notional.LessThanOrEqual(t.MaxNotional) {
			return t
		}
	}
	return c.Tiers[len(c.Tiers)-1]
}

// maintMargin = notional*mmr - maintAmount of its bracket (never negative).
func (c Contract) maintMargin(notional decimal.Decimal) decimal.Decimal {
	t := c.tierFor(notional)
	return dmax(dZero, notional.Mul(t.MMR).Sub(t.MaintAmount))
}

// maxNotional is the largest position value allowed at leverage lev.
func (c Contract) maxNotional(lev int) decimal.Decimal {
	if len(c.Tiers) == 0 {
		return decimal.NewFromInt(1).Shift(18)
	}
	lim := dZero
	for _, t := range c.Tiers {
		if t.MaxLeverage >= lev {
			lim = t.MaxNotional
		}
	}
	return lim
}
