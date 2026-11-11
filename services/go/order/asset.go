package main

// 订单服务看到的资产账本：冻结、解冻，以及按 tradeId 幂等结算成交。

import (
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"net/http"
	"strings"
	"time"

	"github.com/shopspring/decimal"
)

// ErrInsufficient：资产服务因可用不足拒绝冻结。其他错误不是余额不足，不能当成拒单重试成功。
var ErrInsufficient = errors.New("insufficient balance")

// AssetClient 调资产服务内部接口。结算按 tradeId 幂等，同一笔成交重放不会划第二次。
type AssetClient struct {
	url   string
	token string
	http  *http.Client
}

// NewAssetClient 去掉 URL 末尾斜杠，超时 5 秒。
func NewAssetClient(url, token string) *AssetClient {
	return &AssetClient{
		url:   strings.TrimRight(url, "/"),
		token: token,
		http:  &http.Client{Timeout: 5 * time.Second},
	}
}

// Freeze 把 amount 从可用划到冻结。ref 由调用方给定，重放同一 ref 由资产服务认。
func (a *AssetClient) Freeze(ctx context.Context, userID, asset string, amount decimal.Decimal, ref string) error {
	return a.movement(ctx, "/internal/freeze", userID, asset, amount, ref)
}

// Unfreeze 把冻结退回可用。余额不足以外的失败不能当作已经退回。
func (a *AssetClient) Unfreeze(ctx context.Context, userID, asset string, amount decimal.Decimal, ref string) error {
	return a.movement(ctx, "/internal/unfreeze", userID, asset, amount, ref)
}

func (a *AssetClient) movement(ctx context.Context, path, userID, asset string, amount decimal.Decimal, ref string) error {
	body, _ := json.Marshal(map[string]string{
		"userId": userID,
		"asset":  asset,
		"amount": amount.String(),
		"ref":    ref,
	})
	return a.do(ctx, path, body)
}

// SettleParams 描述一笔要在资产服务里原子结算的成交。复式记账，按 TradeID 幂等。
type SettleParams struct {
	TradeID        string
	Base           string
	Quote          string
	BuyerUserID    string
	SellerUserID   string
	FeeAccount     string
	QuoteAmount    decimal.Decimal
	BaseQty        decimal.Decimal
	BuyerFeeBase   decimal.Decimal
	SellerFeeQuote decimal.Decimal
}

// Settle 结算一笔：买方冻结的计价币付出，卖方冻结的基础币交割，两边手续费进手续费账户。按 tradeId 可重试。
func (a *AssetClient) Settle(ctx context.Context, p SettleParams) error {
	body, _ := json.Marshal(map[string]string{
		"tradeId":        p.TradeID,
		"base":           p.Base,
		"quote":          p.Quote,
		"buyerUserId":    p.BuyerUserID,
		"sellerUserId":   p.SellerUserID,
		"feeAccount":     p.FeeAccount,
		"quoteAmount":    p.QuoteAmount.String(),
		"baseQty":        p.BaseQty.String(),
		"buyerFeeBase":   p.BuyerFeeBase.String(),
		"sellerFeeQuote": p.SellerFeeQuote.String(),
	})
	return a.do(ctx, "/internal/settle", body)
}

// errorBody 是资产服务的统一错误包，只取 code 判断余额不足。
type errorBody struct {
	Code string `json:"code"`
}

func (a *AssetClient) do(ctx context.Context, path string, body []byte) error {
	req, err := http.NewRequestWithContext(ctx, http.MethodPost, a.url+path, bytes.NewReader(body))
	if err != nil {
		return err
	}
	req.Header.Set("Content-Type", "application/json")
	if a.token != "" {
		req.Header.Set("X-Internal-Token", a.token)
	}
	resp, err := a.http.Do(req)
	if err != nil {
		return err
	}
	defer resp.Body.Close()
	data, _ := io.ReadAll(resp.Body)
	if resp.StatusCode == http.StatusOK || resp.StatusCode == http.StatusCreated {
		return nil
	}
	var e errorBody
	_ = json.Unmarshal(data, &e)
	if e.Code == "INSUFFICIENT_BALANCE" {
		return ErrInsufficient
	}
	return fmt.Errorf("asset %s returned %d: %s", path, resp.StatusCode, string(data))
}
