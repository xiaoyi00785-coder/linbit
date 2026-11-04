package main

import (
	"fmt"
	"strings"

	"github.com/shopspring/decimal"
)

// Pure order-parameter checks against a contract's trading rules (tick size,
// min/max quantity, leverage). Shared by place / amend so every entry point
// returns the same error codes and messages.

// checkQty parses and validates an order quantity: > 0, >= minQty, a multiple
// of minQty (minQty doubles as the step), <= maxQty.
func checkQty(c Contract, raw string) (decimal.Decimal, error) {
	q, err := decimal.NewFromString(strings.TrimSpace(raw))
	if err != nil || q.Sign() <= 0 {
		return decimal.Zero, bad("INVALID_QTY", "数量不合法")
	}
	if q.LessThan(c.MinQty) {
		return decimal.Zero, bad("INVALID_QTY", fmt.Sprintf("数量不能小于最小下单量 %s", c.MinQty))
	}
	if !isStep(q, c.MinQty) {
		return decimal.Zero, bad("INVALID_QTY", fmt.Sprintf("数量须为 %s 的整数倍", c.MinQty))
	}
	if c.MaxQty.Sign() > 0 && q.GreaterThan(c.MaxQty) {
		return decimal.Zero, bad("INVALID_QTY", fmt.Sprintf("数量不能超过单笔最大下单量 %s", c.MaxQty))
	}
	return q, nil
}

// checkPrice parses and validates a price: > 0 and a multiple of tickSize.
// field names the parameter in the message (价格 / 止盈价 / 止损价 / 触发价).
func checkPrice(c Contract, raw, field string) (decimal.Decimal, error) {
	p, err := decimal.NewFromString(strings.TrimSpace(raw))
	if err != nil || p.Sign() <= 0 {
		return decimal.Zero, bad("INVALID_PRICE", field+"不合法")
	}
	if !isStep(p, c.Tick) {
		return decimal.Zero, bad("INVALID_PRICE", fmt.Sprintf("%s须为最小变动价位 %s 的整数倍", field, c.Tick))
	}
	return p, nil
}

// checkOptPrice is checkPrice for optional TP/SL ("" or "0" = unset).
func checkOptPrice(c Contract, raw, field string) (decimal.NullDecimal, error) {
	if v := strings.TrimSpace(raw); v == "" || v == "0" {
		return decimal.NullDecimal{}, nil
	}
	p, err := checkPrice(c, raw, field)
	if err != nil {
		return decimal.NullDecimal{}, err
	}
	return decimal.NullDecimal{Decimal: p, Valid: true}, nil
}

// checkLeverage validates 1 <= lev <= contract max leverage (e.g. 200x).
func checkLeverage(c Contract, lev int) error {
	if lev < 1 || lev > c.MaxLeverage {
		return bad("INVALID_LEVERAGE", fmt.Sprintf("杠杆倍数范围 1-%d", c.MaxLeverage))
	}
	return nil
}
