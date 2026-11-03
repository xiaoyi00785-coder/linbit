package main

import (
	"bytes"
	"context"
	"encoding/json"
	"fmt"
	"net/http"
	"strings"
	"time"

	"github.com/shopspring/decimal"
)

// AssetClient moves USDT between the spot wallet (asset service) and the futures
// wallet. Calls are idempotent on ref.
type AssetClient struct {
	baseURL, token string
	http           *http.Client
}

func NewAssetClient(baseURL, token string) *AssetClient {
	return &AssetClient{baseURL: strings.TrimRight(baseURL, "/"), token: token, http: &http.Client{Timeout: 5 * time.Second}}
}

// ErrSpotInsufficient is returned when the spot wallet cannot fund a transfer-in.
var ErrSpotInsufficient = fmt.Errorf("spot balance insufficient")

// Move: toFutures=true debits spot (transfer-out of spot), false credits spot.
func (c *AssetClient) Move(ctx context.Context, userID string, amount decimal.Decimal, ref string, toFutures bool) error {
	return c.MoveWallet(ctx, "spot", userID, amount, ref, toFutures)
}

// MoveWallet is Move against the user's spot or funding wallet.
func (c *AssetClient) MoveWallet(ctx context.Context, wallet, userID string, amount decimal.Decimal, ref string, toFutures bool) error {
	path := "/internal/futures/transfer-in"
	if toFutures {
		path = "/internal/futures/transfer-out"
	}
	if wallet == "" {
		wallet = "spot"
	}
	return c.postMove(ctx, path, map[string]string{"userId": userID, "asset": "USDT", "amount": amount.String(), "ref": ref, "account": wallet})
}

// AccountTransfer moves any asset between the user's spot and funding wallets.
func (c *AssetClient) AccountTransfer(ctx context.Context, userID, asset, from, to string, amount decimal.Decimal, ref string) error {
	return c.postMove(ctx, "/internal/account-transfer", map[string]string{"userId": userID, "asset": asset, "amount": amount.String(), "ref": ref, "from": from, "to": to})
}

func (c *AssetClient) postMove(ctx context.Context, path string, payload map[string]string) error {
	body, _ := json.Marshal(payload)
	req, err := http.NewRequestWithContext(ctx, http.MethodPost, c.baseURL+path, bytes.NewReader(body))
	if err != nil {
		return err
	}
	req.Header.Set("Content-Type", "application/json")
	req.Header.Set("X-Internal-Token", c.token)
	resp, err := c.http.Do(req)
	if err != nil {
		return err
	}
	defer resp.Body.Close()
	var out struct {
		Code string `json:"code"`
	}
	_ = json.NewDecoder(resp.Body).Decode(&out)
	if resp.StatusCode == http.StatusOK {
		return nil
	}
	if out.Code == "INSUFFICIENT_BALANCE" {
		return ErrSpotInsufficient
	}
	return fmt.Errorf("asset %s: http %d %s", path, resp.StatusCode, out.Code)
}
