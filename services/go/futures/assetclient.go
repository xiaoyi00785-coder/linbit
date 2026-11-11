package main

// 合约与资产服务之间的划转。现货/资金账户侧按 ref 幂等；资产固定 USDT。

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

// AssetClient 按 ref 幂等划转。超时 5s，重放由资产服务认 ref。
type AssetClient struct {
	baseURL, token string
	http           *http.Client
}

// NewAssetClient 去掉 baseURL 末尾斜杠，HTTP 超时 5 秒。
func NewAssetClient(baseURL, token string) *AssetClient {
	return &AssetClient{baseURL: strings.TrimRight(baseURL, "/"), token: token, http: &http.Client{Timeout: 5 * time.Second}}
}

// ErrSpotInsufficient：现货可用不足，转入合约失败。
var ErrSpotInsufficient = fmt.Errorf("spot balance insufficient")

// Move 走现货钱包。toFutures=true 扣现货（资产侧 transfer-out），false 把钱记回现货。
func (c *AssetClient) Move(ctx context.Context, userID string, amount decimal.Decimal, ref string, toFutures bool) error {
	return c.MoveWallet(ctx, "spot", userID, amount, ref, toFutures)
}

// MoveWallet 的 wallet 只能是现货或资金账户；空则按 spot。
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

// AccountTransfer 在用户现货与资金账户之间划任意资产，不经过合约钱包。
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

// System 在用户现货可用与平台账户 system:<account> 之间划转（debit 表示用户付出）。
// 按 ref 幂等：重放得到 already_applied，当作成功。
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
