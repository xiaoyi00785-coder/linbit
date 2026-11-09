package main

// client.go：钱包依赖的内网客户端。
// AssetClient 调资产服务复式记账（/internal/*）；AccountClient 调 2FA 和白名单。
// 都带共享内部令牌，网关不转发这些路径。金额字段是十进制字符串。

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

// AssetClient 调资产服务的内部记账接口。baseURL 为空时调用直接失败。
type AssetClient struct {
	baseURL string
	token   string
	http    *http.Client
}

// NewAssetClient 构造客户端。baseURL 为空时 post 会失败，调用方自己处理。
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

// Freeze 把 amount 从可用划到冻结。amount 用十进制字符串传给资产服务。
func (c *AssetClient) Freeze(ctx context.Context, userID, asset string, amount decimal.Decimal, ref string) error {
	return c.post(ctx, "/internal/freeze", movement{UserID: userID, Asset: asset, Amount: amount.String(), Ref: ref})
}

// Unfreeze 把 amount 从冻结划回可用。
func (c *AssetClient) Unfreeze(ctx context.Context, userID, asset string, amount decimal.Decimal, ref string) error {
	return c.post(ctx, "/internal/unfreeze", movement{UserID: userID, Asset: asset, Amount: amount.String(), Ref: ref})
}

// Deposit 入账已确认充值。按 ref 幂等，键形如 dep:<network>:<tx>:<logIndex>。
func (c *AssetClient) Deposit(ctx context.Context, userID, asset string, amount decimal.Decimal, ref string) error {
	return c.post(ctx, "/internal/deposit", movement{UserID: userID, Asset: asset, Amount: amount.String(), Ref: ref})
}

// WithdrawComplete 从冻结扣掉已确认提现，并把 Gas 记到平台账户。按 ref 幂等，提现侧用 withdraw:<id>。
func (c *AssetClient) WithdrawComplete(ctx context.Context, userID, asset string, amount, gas decimal.Decimal, feeAccount, ref string) error {
	return c.post(ctx, "/internal/withdraw-complete", withdrawComplete{
		UserID: userID, Asset: asset, Amount: amount.String(), Gas: gas.String(),
		FeeAccount: feeAccount, Ref: ref,
	})
}

type movement struct {
	UserID  string `json:"userId"`
	Asset   string `json:"asset"`
	Amount  string `json:"amount"`
	Ref     string `json:"ref"`
	Purpose string `json:"purpose,omitempty"`
	Account string `json:"account,omitempty"`
	ToUser  string `json:"toUserId,omitempty"`
}

// FreezeWithdraw 在管理端配置的提现账户里冻结（默认资金账户）。
// 资产服务按 ref 记住账户，解冻和提现完成打到同一个桶。
func (c *AssetClient) FreezeWithdraw(ctx context.Context, userID, asset string, amount decimal.Decimal, ref string) error {
	return c.post(ctx, "/internal/freeze", movement{UserID: userID, Asset: asset, Amount: amount.String(), Ref: ref, Purpose: "withdraw"})
}

// UserTransfer 在两个用户的资金账户之间划转，按 ref 幂等。默认账户 funding。
func (c *AssetClient) UserTransfer(ctx context.Context, fromUser, toUser, asset string, amount decimal.Decimal, ref string) error {
	return c.post(ctx, "/internal/user-transfer", movement{UserID: fromUser, ToUser: toUser, Asset: asset,
		Amount: amount.String(), Ref: ref, Account: "funding"})
}

type withdrawComplete struct {
	UserID     string `json:"userId"`
	Asset      string `json:"asset"`
	Amount     string `json:"amount"`
	Gas        string `json:"gas"`
	FeeAccount string `json:"feeAccount"`
	Ref        string `json:"ref"`
}

// AccountClient 调账户服务的 2FA、白名单、KYC 等级和用户查找。
type AccountClient struct {
	baseURL string
	token   string
	http    *http.Client
}

// NewAccountClient 构造账户内网客户端。
func NewAccountClient(baseURL, token string) *AccountClient {
	return &AccountClient{baseURL: strings.TrimRight(baseURL, "/"), token: token,
		http: &http.Client{Timeout: 5 * time.Second}}
}

// VerifyResult 是提现二次验证的结果。OK 为假时看 Reason。
type VerifyResult struct {
	OK          bool    `json:"ok"`
	Reason      string  `json:"reason"`
	LockedUntil *string `json:"lockedUntil"`
}

// OutflowOpts 加上安全设置检查（提现锁定、资金密码）。Action 取 "withdraw" 或 "internal_transfer"。
type OutflowOpts struct {
	Action       string
	FundPassword string
}

// Verify2FA 校验二次因素：开了 TOTP 用 TOTP，否则用 purpose=withdraw 的邮箱/短信码。
func (c *AccountClient) Verify2FA(ctx context.Context, uid, totpCode, code string, opts ...OutflowOpts) (VerifyResult, error) {
	if c.baseURL == "" {
		return VerifyResult{}, fmt.Errorf("account internal url not configured")
	}
	body := map[string]string{"uid": uid, "totpCode": totpCode, "code": code}
	if len(opts) > 0 {
		body["action"], body["fundPassword"] = opts[0].Action, opts[0].FundPassword
	}
	buf, _ := json.Marshal(body)
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

// WhitelistResult 报告提现白名单是否打开，以及该地址是否允许。
type WhitelistResult struct {
	Enabled bool `json:"enabled"`
	Allowed bool `json:"allowed"`
}

// WhitelistCheck 查询白名单开关，以及 (asset, network, address) 是否在该用户白名单里。
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

// KYCLevel 向账户服务取当前 KYC 等级（审核通过后由 KYC 服务抬升）。用来套 24h 提现额度。
func (c *AccountClient) KYCLevel(ctx context.Context, uid string) (int, error) {
	if c.baseURL == "" {
		return 0, fmt.Errorf("account internal url not configured")
	}
	q := url.Values{}
	q.Set("uid", uid)
	req, err := http.NewRequestWithContext(ctx, http.MethodGet,
		c.baseURL+"/internal/account/kyc-level?"+q.Encode(), nil)
	if err != nil {
		return 0, err
	}
	req.Header.Set("X-Internal-Token", c.token)
	resp, err := c.http.Do(req)
	if err != nil {
		return 0, err
	}
	defer resp.Body.Close()
	if resp.StatusCode >= 300 {
		return 0, fmt.Errorf("account kyc-level status %d", resp.StatusCode)
	}
	var out struct {
		Level int `json:"level"`
	}
	if err := json.NewDecoder(resp.Body).Decode(&out); err != nil {
		return 0, err
	}
	return out.Level, nil
}

// UserLookup 是账户服务的收款人查找结果。
type UserLookup struct {
	UID          string `json:"uid"`
	Status       string `json:"status"`
	IsSubAccount bool   `json:"isSubAccount"`
	MaskedEmail  string `json:"maskedEmail"`
}

// ErrUserNotFound 表示 uid 或邮箱在账户服务里不存在。
var ErrUserNotFound = fmt.Errorf("user not found")

// LookupUser 用 uid 或邮箱向账户服务解析用户。
func (c *AccountClient) LookupUser(ctx context.Context, uid, email string) (UserLookup, error) {
	if c.baseURL == "" {
		return UserLookup{}, fmt.Errorf("account internal url not configured")
	}
	q := url.Values{}
	if uid != "" {
		q.Set("uid", uid)
	} else {
		q.Set("email", email)
	}
	req, err := http.NewRequestWithContext(ctx, http.MethodGet, c.baseURL+"/internal/users/lookup?"+q.Encode(), nil)
	if err != nil {
		return UserLookup{}, err
	}
	req.Header.Set("X-Internal-Token", c.token)
	resp, err := c.http.Do(req)
	if err != nil {
		return UserLookup{}, err
	}
	defer resp.Body.Close()
	if resp.StatusCode == http.StatusNotFound {
		return UserLookup{}, ErrUserNotFound
	}
	if resp.StatusCode >= 300 {
		return UserLookup{}, fmt.Errorf("account lookup status %d", resp.StatusCode)
	}
	var out UserLookup
	if err := json.NewDecoder(resp.Body).Decode(&out); err != nil {
		return UserLookup{}, err
	}
	return out, nil
}

// verifyError 把账户服务的 reason 映射成对外错误。
// TWOFA_REQUIRED（403）：没提供二次因素，或没绑定。
// INVALID_TOTP（401）：提供了 TOTP / 验证码但错误。
// WITHDRAW_LOCKED（403）、FUND_PASSWORD_* 见下面的分支。
func verifyError(vr VerifyResult) *apiError {
	switch vr.Reason {
	case "totp_invalid", "code_invalid":
		return errf(http.StatusUnauthorized, "INVALID_TOTP", "二次验证码错误")
	case "account_frozen":
		return errf(http.StatusForbidden, "ACCOUNT_FROZEN", "账户已冻结")
	case "withdraw_locked":
		msg := "账户安全设置近期变更，提现/转账暂时锁定"
		if vr.LockedUntil != nil {
			msg += "，解锁时间 " + *vr.LockedUntil
		}
		return errf(http.StatusForbidden, "WITHDRAW_LOCKED", msg)
	case "fund_password_required":
		return errf(http.StatusBadRequest, "FUND_PASSWORD_REQUIRED", "请输入资金密码")
	case "fund_password_invalid":
		return errf(http.StatusBadRequest, "FUND_PASSWORD_INVALID", "资金密码错误")
	case "fund_password_locked":
		msg := "资金密码错误次数过多，已暂时锁定"
		if vr.LockedUntil != nil {
			msg += "，解锁时间 " + *vr.LockedUntil
		}
		return errf(http.StatusLocked, "FUND_PASSWORD_LOCKED", msg)
	case "fund_password_not_set":
		return errf(http.StatusForbidden, "FUND_PASSWORD_NOT_SET", "请先设置资金密码")
	case "internal_error":
		return errf(http.StatusInternalServerError, "INTERNAL", "二次验证服务不可用")
	default: // totp_required, code_required, no_contact, legacy/unknown
		return errf(http.StatusForbidden, "TWOFA_REQUIRED", "需要二次验证（请绑定并提供 TOTP 或验证码）")
	}
}
