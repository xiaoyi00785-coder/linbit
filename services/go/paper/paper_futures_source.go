package main

// LiveFutures 只 GET 真实合约的公开行情：/futures/market/contracts、risk-tiers、ticker、funding-history。
// 不下单，不写 fut_accounts，也不读用户余额。

import (
	"context"
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"net/url"
	"strings"
	"time"

	"github.com/shopspring/decimal"
)

// LiveFutures 的 get 只用 HTTP GET。base 为空直接失败，不回落到本地价。
type LiveFutures struct {
	base   string
	client *http.Client
}

// NewLiveFutures 去掉地址末尾斜杠，超时 5 秒。空地址留到请求时失败。
func NewLiveFutures(base string) *LiveFutures {
	return &LiveFutures{base: strings.TrimRight(base, "/"), client: &http.Client{Timeout: 5 * time.Second}}
}

func (l *LiveFutures) get(ctx context.Context, path string, out any) error {
	if l.base == "" {
		return fmt.Errorf("futures url unset")
	}
	req, err := http.NewRequestWithContext(ctx, http.MethodGet, l.base+path, nil)
	if err != nil {
		return err
	}
	resp, err := l.client.Do(req)
	if err != nil {
		return err
	}
	defer resp.Body.Close()
	if resp.StatusCode != http.StatusOK {
		return fmt.Errorf("futures %s status %d", path, resp.StatusCode)
	}
	return json.NewDecoder(io.LimitReader(resp.Body, 4<<20)).Decode(out)
}

// Specs 只映射 BTC/ETH/BNB/ARB/XRP/SOL。真实合约 halted、行情不可达或标记价不可用则 Available=false，不编价格。
func (l *LiveFutures) Specs(ctx context.Context) ([]FutSpec, error) {
	var contracts struct {
		Contracts []struct {
			Symbol      string          `json:"symbol"`
			Base        string          `json:"baseAsset"`
			Tick        decimal.Decimal `json:"tickSize"`
			MinQty      decimal.Decimal `json:"minQty"`
			MaxQty      decimal.Decimal `json:"maxQty"`
			MMR         decimal.Decimal `json:"maintenanceMarginRate"`
			MaxLeverage int             `json:"maxLeverage"`
			Maker       decimal.Decimal `json:"makerFeeRate"`
			Taker       decimal.Decimal `json:"takerFeeRate"`
			FundingH    int             `json:"fundingIntervalHours"`
			Status      string          `json:"status"`
		} `json:"contracts"`
	}
	err := l.get(ctx, "/futures/market/contracts", &contracts)
	byBase := map[string]int{}
	if err == nil {
		for i, c := range contracts.Contracts {
			byBase[strings.ToUpper(c.Base)] = i
		}
	}
	var tiers struct {
		Contracts []struct {
			Symbol string `json:"symbol"`
			Tiers  any    `json:"tiers"`
		} `json:"contracts"`
	}
	tiersOK := l.get(ctx, "/futures/market/risk-tiers", &tiers) == nil
	tierBy := map[string]any{}
	if tiersOK {
		for _, c := range tiers.Contracts {
			tierBy[c.Symbol] = c.Tiers
		}
	}
	out := make([]FutSpec, 0, len(paperFutBases))
	for _, base := range paperFutBases {
		sp := FutSpec{Symbol: base + "USDX", LiveSymbol: base + "USDT", Base: base, Quote: "USDX", PriceSource: "futures_mark"}
		idx, ok := byBase[base]
		if err != nil || !ok || contracts.Contracts[idx].Status == "halted" {
			sp.Available = false
			sp.UnavailableReason = "真实永续合约源没有该交易对或行情服务不可达"
			sp.FundingAvailable = false
			out = append(out, sp)
			continue
		}
		c := contracts.Contracts[idx]
		sp.TickSize, sp.MinQty, sp.MaxQty, sp.MMR = c.Tick, c.MinQty, c.MaxQty, c.MMR
		sp.MaxLeverage, sp.MakerFee, sp.TakerFee, sp.FundingIntervalH = c.MaxLeverage, c.Maker, c.Taker, c.FundingH
		sp.Available = true
		if tiersOK {
			sp.Tiers = tierBy[c.Symbol]
		}
		mark, index, fund, qok, _ := l.Quote(ctx, c.Symbol)
		sp.Mark, sp.Index = mark, index
		if qok {
			sp.PriceSource = "futures_mark"
		} else {
			sp.Available = false
			sp.UnavailableReason = "真实标记价格不可用"
		}
		if fund != nil {
			s := fund.String()
			sp.FundingRate = &s
			sp.FundingAvailable = true
		}
		out = append(out, sp)
	}
	return out, nil
}

// Quote 读真实 ticker 的 markPrice 和 indexPrice。priceAvailable 为假或标记价不是正数则 ok=false。资金费取历史最近一条，失败不影响标记价。
func (l *LiveFutures) Quote(ctx context.Context, liveSymbol string) (mark, index decimal.Decimal, funding *decimal.Decimal, ok bool, err error) {
	var tk struct {
		Mark           decimal.Decimal `json:"markPrice"`
		Index          decimal.Decimal `json:"indexPrice"`
		PriceAvailable bool            `json:"priceAvailable"`
	}
	if err = l.get(ctx, "/futures/market/ticker?symbol="+url.QueryEscape(liveSymbol), &tk); err != nil {
		return
	}
	if !tk.PriceAvailable || !tk.Mark.IsPositive() {
		return tk.Mark, tk.Index, nil, false, nil
	}
	var hist struct {
		History []struct {
			Rate string `json:"fundingRate"`
		} `json:"history"`
	}
	if l.get(ctx, "/futures/market/funding-history?symbol="+url.QueryEscape(liveSymbol)+"&limit=1", &hist) == nil && len(hist.History) > 0 {
		if r, e := decimal.NewFromString(hist.History[0].Rate); e == nil {
			funding = &r
		}
	}
	return tk.Mark, tk.Index, funding, true, nil
}

// Settlements 读真实资金费历史，最多 50 条。非法费率或时间戳丢掉，不补一条。
func (l *LiveFutures) Settlements(ctx context.Context, liveSymbol string) ([]FundingSettlement, error) {
	var hist struct {
		History []struct {
			Rate string `json:"fundingRate"`
			At   int64  `json:"fundingTime"`
		} `json:"history"`
	}
	if err := l.get(ctx, "/futures/market/funding-history?symbol="+url.QueryEscape(liveSymbol)+"&limit=50", &hist); err != nil {
		return nil, err
	}
	out := make([]FundingSettlement, 0, len(hist.History))
	for _, row := range hist.History {
		rate, err := decimal.NewFromString(row.Rate)
		if err != nil || row.At <= 0 {
			continue
		}
		out = append(out, FundingSettlement{Time: time.UnixMilli(row.At).UTC(), Rate: rate})
	}
	return out, nil
}
