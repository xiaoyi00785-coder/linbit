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

// AssetClient talks to the asset service for USDT movements between a user's
// spot free balance and the system:options account. Idempotent on ref.
type AssetClient struct {
	baseURL, token string
	http           *http.Client
}

// ErrSpotInsufficient is returned when the spot wallet lacks the requested amount.
var ErrSpotInsufficient = fmt.Errorf("spot balance insufficient")

func NewAssetClient(baseURL, token string) *AssetClient {
	return &AssetClient{baseURL: strings.TrimRight(baseURL, "/"), token: token, http: &http.Client{Timeout: 5 * time.Second}}
}

// System moves amount between the user's spot free balance and system:options.
// direction "debit" takes from the user; "credit" pays the user. Ref is the
// idempotency key — a replay of the same ref is treated as success.
func (c *AssetClient) System(ctx context.Context, userID, direction string, amount decimal.Decimal, ref string) error {
	if amount.Sign() <= 0 {
		return nil
	}
	payload := map[string]string{
		"userId": userID, "asset": "USDT",
		"amount": amount.String(), "account": "options",
		"direction": direction, "ref": ref,
	}
	body, _ := json.Marshal(payload)
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
