package main

// 模拟盘对外结构。金额展示走 money()，按交易对精度 StringFixed，不改库里的原始小数。

import (
	"github.com/shopspring/decimal"
	"time"
)

// Symbol 的精度用于拒绝多余小数。Status 不是 trading 时不能下模拟现货单。
type Symbol struct {
	Symbol            string
	Base              string
	Quote             string
	PricePrecision    int
	QuantityPrecision int
	MinQuantity       decimal.Decimal
	MinNotional       decimal.Decimal
	Status            string
}

// Quote 是指数价。Source 不是现货最新成交，模拟现货按它成交。
type Quote struct {
	Symbol     string          `json:"symbol"`
	Price      decimal.Decimal `json:"indexPrice"`
	Source     string          `json:"source"`
	ObservedAt time.Time       `json:"updatedAt"`
}

// PaperAccount 的 Generation 在重置时加一。旧 generation 的余额不再被新单使用。
type PaperAccount struct {
	AccountID           string     `json:"accountId"`
	Mode                string     `json:"mode"`
	Market              string     `json:"market"`
	Generation          int64      `json:"generation"`
	QuoteAsset          string     `json:"quoteAsset"`
	InitialBalance      string     `json:"initialBalance"`
	ResetLimitPerUTCDay int        `json:"resetLimitPerUtcDay"`
	ResetsUsedToday     int        `json:"resetsUsedToday"`
	NextResetAt         *time.Time `json:"nextResetAt"`
	OrderTypes          []string   `json:"orderTypes"`
	PriceSource         string     `json:"priceSource"`
	FeeModel            string     `json:"feeModel"`
	SlippageModel       string     `json:"slippageModel"`
	CreatedAt           time.Time  `json:"createdAt"`
	UpdatedAt           time.Time  `json:"updatedAt"`
}

// PaperBalance 的 Locked 在列表和重置里固定写成 "0"。现货模拟单即时全成，不冻结。Free/Total 用 8 位 StringFixed。
type PaperBalance struct {
	Asset        string  `json:"asset"`
	Free         string  `json:"free"`
	Locked       string  `json:"locked"`
	Total        string  `json:"total"`
	ValuationUSD *string `json:"valuationUsd"`
	PriceUSD     *string `json:"priceUsd"`
}

// PaperOrder 的 Mode 固定 paper。Price 在响应里为空，成交价在 ReferencePrice，来源 INDEX。
type PaperOrder struct {
	OrderID          string       `json:"orderId"`
	ClientOrderID    string       `json:"clientOrderId"`
	Mode             string       `json:"mode"`
	AccountID        string       `json:"accountId"`
	Generation       int64        `json:"generation"`
	Symbol           string       `json:"symbol"`
	Side             string       `json:"side"`
	Type             string       `json:"type"`
	Price            any          `json:"price"`
	Quantity         string       `json:"quantity"`
	QuoteOrderQty    *string      `json:"quoteOrderQty,omitempty"`
	FilledQuantity   string       `json:"filledQuantity"`
	Status           string       `json:"status"`
	PriceSource      string       `json:"priceSource"`
	ReferencePrice   string       `json:"referencePrice"`
	MarketObservedAt time.Time    `json:"marketObservedAt"`
	Fee              string       `json:"fee"`
	FeeAsset         string       `json:"feeAsset"`
	CreatedAt        time.Time    `json:"createdAt"`
	ExecutedAt       time.Time    `json:"executedAt"`
	Trades           []PaperTrade `json:"trades,omitempty"`
}

// PaperTrade 记在 paper_trades，不进生产 trades。
type PaperTrade struct {
	TradeID          string    `json:"tradeId"`
	OrderID          string    `json:"orderId"`
	Mode             string    `json:"mode"`
	Symbol           string    `json:"symbol"`
	Side             string    `json:"side"`
	Price            string    `json:"price"`
	Quantity         string    `json:"quantity"`
	Fee              string    `json:"fee"`
	FeeAsset         string    `json:"feeAsset"`
	Role             string    `json:"role"`
	PriceSource      string    `json:"priceSource"`
	MarketObservedAt time.Time `json:"marketObservedAt"`
	CreatedAt        time.Time `json:"createdAt"`
}

// PriceAlert 触发一次后停用。冷却和写限流是两套计数。
type PriceAlert struct {
	AlertID       string   `json:"alertId"`
	ClientAlertID string   `json:"clientAlertId"`
	Market        string   `json:"market"`
	Symbol        string   `json:"symbol"`
	Condition     string   `json:"condition"`
	Threshold     string   `json:"threshold"`
	PriceSource   string   `json:"priceSource"`
	TriggerMode   string   `json:"triggerMode"`
	Channels      []string `json:"channels"`
	Enabled       bool     `json:"enabled"`
	Status        string   `json:"status"`
	Revision      int64    `json:"revision"`
	Limit         struct {
		ActivePerUser int `json:"activePerUser"`
	} `json:"limit"`
	LastObservedPrice *string    `json:"lastObservedPrice"`
	LastEvaluatedAt   *time.Time `json:"lastEvaluatedAt"`
	TriggeredPrice    *string    `json:"triggeredPrice"`
	TriggeredAt       *time.Time `json:"triggeredAt"`
	NotificationID    *string    `json:"notificationId"`
	DeliveryStatus    string     `json:"deliveryStatus"`
	CooldownUntil     *time.Time `json:"cooldownUntil"`
	Note              string     `json:"note"`
	CreatedAt         time.Time  `json:"createdAt"`
	UpdatedAt         time.Time  `json:"updatedAt"`
}

var noPrice any = nil

// money 按给定精度固定小数位展示。现货余额调用处传 8。不改已入库的数值。
func money(d decimal.Decimal, precision int) string { return d.StringFixed(int32(precision)) }
func dptr(d decimal.Decimal) *string                { s := d.String(); return &s }
func tptr(t time.Time) *time.Time                   { return &t }
