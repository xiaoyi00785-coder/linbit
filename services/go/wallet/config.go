package main

// config.go：钱包运行时配置，全部来自环境变量。
// 金额是十进制字符串（shopspring/decimal），不用 float。

import (
	"encoding/json"
	"strings"

	"github.com/linbit/platform/services/go/common"
	"github.com/shopspring/decimal"
)

// Config 是钱包进程的运行时配置。
type Config struct {
	Port          string
	DatabaseURL   string
	InternalToken string
	RedisURL      string
	// DevMode 打开仅开发用的 mock 链接口（注入充值、推进区块、触发扫描）。生产环境禁止。
	DevMode bool

	// AccountInternalURL 走账户服务（2FA、白名单）；AssetInternalURL 走资产服务复式记账。
	AccountInternalURL string
	AssetInternalURL   string

	// FeeAccount 是提现完成时链上 Gas 记入的平台账户，默认 platform:gas（WALLET_FEE_ACCOUNT）。
	FeeAccount string

	// GasDeductMode 决定链上 Gas 从哪扣（WALLET_GAS_DEDUCT_MODE）：
	//   "extra"  — 在提现金额之外再冻一笔 Gas。
	//   "amount" — 从提现金额里扣，用户实收 amount-gas。
	// 产品规则默认 "amount"：只冻结 amount，且 amount 必须大于 gas。
	GasDeductMode string

	// MarketURL 是 market-data 地址，非稳定币提现按 {ASSET}USDT 实时价折 USDT。
	MarketURL string

	// 提现自动放行 / 人工审核的阈值。比较时用 USDT 等值；没有价格则失败关闭，转人工。见 risk.go、withdraw.go。
	Risk RiskThresholds

	// CreditBelowMinDeposit：低于 MinDeposit 的充值是否仍入账。
	// 规则只说不够最小额不满足提现条件，并没有说丢弃，所以默认 true（WALLET_CREDIT_BELOW_MIN_DEPOSIT 非 false）。
	CreditBelowMinDeposit bool

	// Networks 是按币种的网络注册表（确认数、最小/最大、Gas）。
	Networks NetworkRegistry

	// KYC 是按认证等级的 24 小时提现额度。LoadConfig 填充；
	// 手写的零值（单测）让额度检查不生效。
	KYC KYCLimitConfig
}

// KYCLimitConfig 是按 KYC 等级的滚动 24h 提现上限，单位 USDT 等值。
// 非稳定币用 Prices 或行情折算。没有可用价格时提现直接拒绝（PRICE_UNAVAILABLE），不按 0 计入。
type KYCLimitConfig struct {
	// DailyUSDT：等级 0/1/2 → 滚动 24h 提现上限（USDT 等值）。上限 ≤ 0 表示该等级不限额。
	DailyUSDT map[int]decimal.Decimal
	// MinLevel 是允许提现的最低 KYC 等级（默认 1，未认证返回 KYC_REQUIRED）。
	// 0 关掉这道门，等级 0 再走自己的 DailyUSDT。
	MinLevel int
	// Prices 是额度折算用的固定 USDT 价。USDT、USDC 默认钉在 1；WALLET_KYC_PRICES_JSON 可覆盖。
	Prices map[string]decimal.Decimal
}

// limit 返回该等级的每日 USDT 上限，以及额度功能是否打开。
// 高于已配置最高档的等级，落到不超过它的最高一档。
func (k KYCLimitConfig) limit(level int) (decimal.Decimal, bool) {
	if len(k.DailyUSDT) == 0 {
		return decimal.Zero, false
	}
	if v, ok := k.DailyUSDT[level]; ok {
		return v, true
	}
	best, bestLvl, found := decimal.Zero, -1, false
	for lvl, v := range k.DailyUSDT {
		if lvl <= level && lvl > bestLvl {
			best, bestLvl, found = v, lvl, true
		}
	}
	return best, found
}

// price 返回配置里钉死的 USDT 价。没有则 ok=false，调用方再去行情。
func (k KYCLimitConfig) price(asset string) (decimal.Decimal, bool) {
	p, ok := k.Prices[asset]
	return p, ok
}

// RiskThresholds 决定提现自动放行还是人工审核。金额按 USDT 等值比。
// 默认：≤ WALLET_RISK_AUTO_MAX_AMOUNT（1000）且分数 ≤ WALLET_RISK_AUTO_MAX_SCORE（20）自动放行；
// ≥ WALLET_RISK_REVIEW_MIN_AMOUNT（主网 1000，否则 5000）或分数 > WALLET_RISK_REVIEW_MAX_SCORE（40）人工审核。
type RiskThresholds struct {
	AutoMaxAmount   decimal.Decimal // ≤ this (and score ≤ AutoMaxScore) auto-releases
	AutoMaxScore    int
	ReviewMinAmount decimal.Decimal // ≥ this (or score > ReviewMaxScore) forces review
	ReviewMaxScore  int
	// MiddleAction 填规则没写的中间带：WALLET_RISK_MIDDLE_ACTION，默认 "review"，也可 "auto"。
	MiddleAction string
}

// LoadConfig 从环境变量组装 Config。主网 ReviewMinAmount 默认 1000，其余环境 5000。
func LoadConfig() Config {
	return Config{
		Port:               common.Env("PORT", "8086"),
		DatabaseURL:        common.MustEnv("DATABASE_URL"),
		InternalToken:      common.Env("INTERNAL_TOKEN", ""),
		RedisURL:           common.Env("REDIS_URL", ""),
		DevMode:            common.Env("DEV_MODE", "") == "true",
		AccountInternalURL: common.Env("ACCOUNT_INTERNAL_URL", ""),
		AssetInternalURL:   common.Env("ASSET_INTERNAL_URL", ""),
		MarketURL:          common.Env("MARKET_URL", "http://localhost:8085"),
		FeeAccount:         common.Env("WALLET_FEE_ACCOUNT", "platform:gas"),
		GasDeductMode:      gasMode(common.Env("WALLET_GAS_DEDUCT_MODE", "amount")),
		Risk: RiskThresholds{
			AutoMaxAmount:   decimalEnv("WALLET_RISK_AUTO_MAX_AMOUNT", "1000"),
			AutoMaxScore:    common.EnvInt("WALLET_RISK_AUTO_MAX_SCORE", 20),
			ReviewMinAmount: decimalEnv("WALLET_RISK_REVIEW_MIN_AMOUNT", map[bool]string{true: "1000", false: "5000"}[common.IsMainnet()]), /* mainnet: >1000 USDT-equivalent always manual review */
			ReviewMaxScore:  common.EnvInt("WALLET_RISK_REVIEW_MAX_SCORE", 40),
			MiddleAction:    middleAction(common.Env("WALLET_RISK_MIDDLE_ACTION", "review")),
		},
		CreditBelowMinDeposit: common.Env("WALLET_CREDIT_BELOW_MIN_DEPOSIT", "true") != "false",
		Networks:              loadNetworks(),
		KYC:                   loadKYCLimits(),
	}
}

// loadKYCLimits 从环境变量读 KYC 分档的 24h 提现额度。
// 产品规则：低于等级 1（未 KYC）不能提现（KYC_REQUIRED）；
// 等级 1 默认 50000、等级 2 默认 200000 USDT/24h（WALLET_KYC_LEVEL0/1/2_DAILY_USDT，等级 0 默认 1000）。
// 非稳定币按 market-data 的 {ASSET}USDT 最新价折算；没有价格则拒绝（PRICE_UNAVAILABLE）。
// WALLET_KYC_PRICES_JSON 钉死固定价；未设置时 USDT、USDC 为 "1"。
func loadKYCLimits() KYCLimitConfig {
	return KYCLimitConfig{
		MinLevel: common.EnvInt("WALLET_KYC_MIN_LEVEL", 1),
		DailyUSDT: map[int]decimal.Decimal{
			0: decimalEnv("WALLET_KYC_LEVEL0_DAILY_USDT", "1000"),
			1: decimalEnv("WALLET_KYC_LEVEL1_DAILY_USDT", "50000"),
			2: decimalEnv("WALLET_KYC_LEVEL2_DAILY_USDT", "200000"),
		},
		Prices: loadKYCPrices(),
	}
}

func loadKYCPrices() map[string]decimal.Decimal {
	prices := map[string]decimal.Decimal{"USDT": decimal.RequireFromString("1"), "USDC": decimal.RequireFromString("1")}
	raw := common.Env("WALLET_KYC_PRICES_JSON", "")
	if raw == "" {
		return prices
	}
	var override map[string]string
	if err := json.Unmarshal([]byte(raw), &override); err != nil {
		return prices
	}
	for asset, p := range override {
		if d, err := decimal.NewFromString(p); err == nil {
			prices[strings.ToUpper(asset)] = d
		}
	}
	return prices
}

func gasMode(v string) string {
	if v == "extra" {
		return "extra"
	}
	return "amount"
}

func middleAction(v string) string {
	if v == "auto" {
		return "auto"
	}
	return "review"
}

func decimalEnv(key, def string) decimal.Decimal {
	d, err := decimal.NewFromString(common.Env(key, def))
	if err != nil {
		return decimal.RequireFromString(def)
	}
	return d
}
