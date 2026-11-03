package main

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

// ErrInsufficient is returned when the asset service rejects a freeze for lack
// of available balance.
var ErrInsufficient = errors.New("insufficient balance")

// AssetClient calls the asset service's internal API to freeze/unfreeze funds
// and settle matched trades. It is the order service's view of the asset ledger.
type AssetClient struct {
	url   string
	token string
	http  *http.Client
}

// NewAssetClient constructs an asset client.
func NewAssetClient(url, token string) *AssetClient {
	return &AssetClient{
		url:   strings.TrimRight(url, "/"),
		token: token,
		http:  &http.Client{Timeout: 5 * time.Second},
	}
}

// Freeze moves amount of asset from the user's free to locked balance.
func (a *AssetClient) Freeze(ctx context.Context, userID, asset string, amount decimal.Decimal, ref string) error {
	return a.movement(ctx, "/internal/freeze", userID, asset, amount, ref)
}

// Unfreeze moves amount of asset from the user's locked back to free balance.
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

// SettleParams describes one matched trade to settle atomically in the asset
// service (double-entry, idempotent on TradeID).
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

// Settle applies one trade: buyer's locked quote is paid out, seller's locked
// base is delivered, and both fees go to the fee account. Safe to retry.
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

// errorBody is the asset service's unified error envelope (see common.APIError).
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
