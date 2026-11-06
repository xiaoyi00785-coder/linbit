package main

import (
	"github.com/shopspring/decimal"
	"time"
)

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

type Quote struct {
	Symbol     string          `json:"symbol"`
	Price      decimal.Decimal `json:"indexPrice"`
	Source     string          `json:"source"`
	ObservedAt time.Time       `json:"updatedAt"`
}

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

type PaperBalance struct {
	Asset        string  `json:"asset"`
	Free         string  `json:"free"`
	Locked       string  `json:"locked"`
	Total        string  `json:"total"`
	ValuationUSD *string `json:"valuationUsd"`
	PriceUSD     *string `json:"priceUsd"`
}

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

func money(d decimal.Decimal, precision int) string { return d.StringFixed(int32(precision)) }
func dptr(d decimal.Decimal) *string                { s := d.String(); return &s }
func tptr(t time.Time) *time.Time                   { return &t }
