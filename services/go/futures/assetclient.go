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
	path := "/internal/futures/transfer-in"
	if toFutures {
		path = "/internal/futures/transfer-out"
	}
	body, _ := json.Marshal(map[string]string{"userId": userID, "asset": "USDT", "amount": amount.String(), "ref": ref})
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

// System moves amount between the user's spot free balance and the platform
// account system:<account> (direction debit = user pays). Idempotent on ref:
// a replay answers already_applied, treated as success.
func (c *AssetClient) System(ctx context.Context, userID, asset, account, direction string, amount decimal.Decimal, ref string) error {
	body, _ := json.Marshal(map[string]string{"userId": userID, "asset": asset, "amount": amount.String(), "account": account, "direction": direction, "ref": ref})
	req, err := http.NewRequestWithContext(ctx, http.MethodPost, c.baseURL+"/internal/system-transfer", bytes.NewReader(body))
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
	return fmt.Errorf("asset system-transfer: http %d %s", resp.StatusCode, out.Code)
}
