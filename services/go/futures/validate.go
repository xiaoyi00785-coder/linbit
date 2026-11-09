package main

// 下单与改单共用的参数校验，错误码和文案保持一致。价格、数量不符合步长则拒绝，不截断。

import (
	"fmt"
	"strings"

	"github.com/shopspring/decimal"
)

// checkQty：数量 > 0、>= minQty、<= maxQty，且是 minQty 的整数倍（minQty 即步长）。
// 多出来的小数或不足一步返回 INVALID_QTY，不截断。
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

// checkPrice：价格 > 0 且是 tickSize 的整数倍，否则 INVALID_PRICE，不截断。
// field 写入文案，取 价格 / 止盈价 / 止损价 / 触发价。
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

// checkOptPrice：空或 "0" 表示不设置止盈/止损，其余走 checkPrice。
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

// checkLeverage：杠杆必须在 1 与合约 MaxLeverage 之间，否则 INVALID_LEVERAGE。
func checkLeverage(c Contract, lev int) error {
	if lev < 1 || lev > c.MaxLeverage {
		return bad("INVALID_LEVERAGE", fmt.Sprintf("杠杆倍数范围 1-%d", c.MaxLeverage))
	}
	return nil
}
