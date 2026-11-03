package main

import (
	"bytes"
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

// client.go holds the service-to-service clients the wallet depends on:
//   - AssetClient  -> asset service double-entry bookkeeping (/internal/*)
//   - AccountClient -> account service 2FA + whitelist checks (/internal/*)
// Both authenticate with the shared internal token and are never reachable
// through the gateway.

// AssetClient calls the asset service's internal bookkeeping endpoints.
type AssetClient struct {
	baseURL string
	token   string
	http    *http.Client
}

// NewAssetClient builds an AssetClient (nil-safe: a zero baseURL disables calls).
func NewAssetClient(baseURL, token string) *AssetClient {
	return &AssetClient{baseURL: strings.TrimRight(baseURL, "/"), token: token,
		http: &http.Client{Timeout: 5 * time.Second}}
}

func (c *AssetClient) post(ctx context.Context, path string, body any) error {
	if c.baseURL == "" {
		return fmt.Errorf("asset internal url not configured")
	}
	buf, _ := json.Marshal(body)
	req, err := http.NewRequestWithContext(ctx, http.MethodPost, c.baseURL+path, bytes.NewReader(buf))
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
	if resp.StatusCode >= 300 {
		b, _ := io.ReadAll(io.LimitReader(resp.Body, 1024))
		return fmt.Errorf("asset %s: status %d: %s", path, resp.StatusCode, strings.TrimSpace(string(b)))
	}
	return nil
}

// Freeze moves amount from free to locked.
func (c *AssetClient) Freeze(ctx context.Context, userID, asset string, amount decimal.Decimal, ref string) error {
	return c.post(ctx, "/internal/freeze", movement{UserID: userID, Asset: asset, Amount: amount.String(), Ref: ref})
}

// Unfreeze moves amount from locked back to free.
func (c *AssetClient) Unfreeze(ctx context.Context, userID, asset string, amount decimal.Decimal, ref string) error {
	return c.post(ctx, "/internal/unfreeze", movement{UserID: userID, Asset: asset, Amount: amount.String(), Ref: ref})
}

// Deposit credits a confirmed deposit. Idempotent on ref (network:tx:logIndex).
func (c *AssetClient) Deposit(ctx context.Context, userID, asset string, amount decimal.Decimal, ref string) error {
	return c.post(ctx, "/internal/deposit", movement{UserID: userID, Asset: asset, Amount: amount.String(), Ref: ref})
}

// WithdrawComplete debits a confirmed withdrawal from locked and books the gas to
// the platform account. Idempotent on ref (the withdrawal id).
func (c *AssetClient) WithdrawComplete(ctx context.Context, userID, asset string, amount, gas decimal.Decimal, feeAccount, ref string) error {
	return c.post(ctx, "/internal/withdraw-complete", withdrawComplete{
		UserID: userID, Asset: asset, Amount: amount.String(), Gas: gas.String(),
		FeeAccount: feeAccount, Ref: ref,
	})
}

type movement struct {
	UserID string `json:"userId"`
	Asset  string `json:"asset"`
	Amount string `json:"amount"`
	Ref    string `json:"ref"`
}

type withdrawComplete struct {
	UserID     string `json:"userId"`
	Asset      string `json:"asset"`
	Amount     string `json:"amount"`
	Gas        string `json:"gas"`
	FeeAccount string `json:"feeAccount"`
	Ref        string `json:"ref"`
}

// AccountClient calls the account service's internal 2FA + whitelist endpoints.
type AccountClient struct {
	baseURL string
	token   string
	http    *http.Client
}

// NewAccountClient builds an AccountClient.
func NewAccountClient(baseURL, token string) *AccountClient {
	return &AccountClient{baseURL: strings.TrimRight(baseURL, "/"), token: token,
		http: &http.Client{Timeout: 5 * time.Second}}
}

// VerifyResult is the outcome of a withdrawal 2FA check.
type VerifyResult struct {
	OK     bool   `json:"ok"`
	Reason string `json:"reason"`
}

// Verify2FA validates the withdrawal's second factor (TOTP when enabled, else an
// email/SMS code with purpose=withdraw) for the user.
func (c *AccountClient) Verify2FA(ctx context.Context, uid, totpCode, code string) (VerifyResult, error) {
	if c.baseURL == "" {
		return VerifyResult{}, fmt.Errorf("account internal url not configured")
	}
	buf, _ := json.Marshal(map[string]string{"uid": uid, "totpCode": totpCode, "code": code})
	req, err := http.NewRequestWithContext(ctx, http.MethodPost, c.baseURL+"/internal/withdraw/verify", bytes.NewReader(buf))
	if err != nil {
		return VerifyResult{}, err
	}
	req.Header.Set("Content-Type", "application/json")
	req.Header.Set("X-Internal-Token", c.token)
	resp, err := c.http.Do(req)
	if err != nil {
		return VerifyResult{}, err
	}
	defer resp.Body.Close()
	var out VerifyResult
	_ = json.NewDecoder(resp.Body).Decode(&out)
	if resp.StatusCode >= 300 && out.Reason == "" {
		out.Reason = fmt.Sprintf("account verify status %d", resp.StatusCode)
	}
	return out, nil
}

// WhitelistResult reports the user's whitelist switch and whether a destination
// address is permitted under it.
type WhitelistResult struct {
	Enabled bool `json:"enabled"`
	Allowed bool `json:"allowed"`
}

// WhitelistCheck returns whether the whitelist is on and whether the (asset,
// network, address) is a whitelisted destination for the user.
func (c *AccountClient) WhitelistCheck(ctx context.Context, uid, asset, network, address string) (WhitelistResult, error) {
	if c.baseURL == "" {
		return WhitelistResult{}, fmt.Errorf("account internal url not configured")
	}
	q := url.Values{}
	q.Set("uid", uid)
	q.Set("asset", asset)
	q.Set("network", network)
	q.Set("address", address)
	req, err := http.NewRequestWithContext(ctx, http.MethodGet,
		c.baseURL+"/internal/withdraw/whitelist?"+q.Encode(), nil)
	if err != nil {
		return WhitelistResult{}, err
	}
	req.Header.Set("X-Internal-Token", c.token)
	resp, err := c.http.Do(req)
	if err != nil {
		return WhitelistResult{}, err
	}
	defer resp.Body.Close()
	if resp.StatusCode >= 300 {
		return WhitelistResult{}, fmt.Errorf("account whitelist status %d", resp.StatusCode)
	}
	var out WhitelistResult
	if err := json.NewDecoder(resp.Body).Decode(&out); err != nil {
		return WhitelistResult{}, err
	}
	return out, nil
}
