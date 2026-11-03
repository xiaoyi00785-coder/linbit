package main

import (
	"strings"
	"time"

	"github.com/linbit/platform/services/go/common"
	"github.com/shopspring/decimal"
)

// Config holds the admin service configuration. Every business threshold not
// fixed by the product rules is configurable here (see the defaults list in
// the commit / openapi).
type Config struct {
	Port          string
	DatabaseURL   string
	InternalToken string
	DevMode       bool

	AccountURL string
	AssetURL   string
	WalletURL  string
	KYCURL     string
	FuturesURL string
	OrderURL   string
	MarketURL  string

	// Security.
	Require2FA      bool          // ADMIN_REQUIRE_2FA (default true)
	SessionIdle     time.Duration // ADMIN_SESSION_IDLE_MINUTES (default 30)
	MaxFailedLogins int           // ADMIN_MAX_FAILED_LOGINS (default 5)
	LockoutDuration time.Duration // ADMIN_LOCKOUT_MINUTES (default 15)
	IPAllowlist     []string      // ADMIN_IP_ALLOWLIST (comma list of IPs/CIDRs; empty = all)
	TrustProxy      bool          // ADMIN_TRUST_PROXY: use X-Forwarded-For for the client IP
	MinPasswordLen  int           // ADMIN_MIN_PASSWORD_LEN (default 10)

	// Bootstrap super admin, created on first start when no admin exists.
	BootstrapUser string
	BootstrapPass string
	BootstrapTOTP string

	// Approval rules.
	FeeNotice             time.Duration   // ADMIN_FEE_NOTICE_HOURS (default 24)
	CouponBudgetThreshold decimal.Decimal // ADMIN_COUPON_FINANCE_THRESHOLD (default 10000 USDT)
	RewardReviewThreshold decimal.Decimal // ADMIN_REWARD_FINANCE_THRESHOLD (default 1000 USDT)
	WithdrawSecondAmount  decimal.Decimal // ADMIN_WITHDRAW_SECOND_REVIEW_USDT (default 5000)
	WithdrawSecondRisk    int             // ADMIN_WITHDRAW_SECOND_REVIEW_RISK (risk > this; default 40)
	SchedulerInterval     time.Duration   // ADMIN_SCHEDULER_SECONDS (default 30)
}

func decEnv(key, def string) decimal.Decimal {
	d, err := decimal.NewFromString(common.Env(key, def))
	if err != nil {
		return decimal.RequireFromString(def)
	}
	return d
}

// LoadConfig reads configuration from the environment.
func LoadConfig() Config {
	var allow []string
	for _, p := range strings.Split(common.Env("ADMIN_IP_ALLOWLIST", ""), ",") {
		if p = strings.TrimSpace(p); p != "" {
			allow = append(allow, p)
		}
	}
	return Config{
		Port:          common.Env("PORT", "8088"),
		DatabaseURL:   common.MustEnv("DATABASE_URL"),
		InternalToken: common.Env("INTERNAL_TOKEN", ""),
		DevMode:       common.Env("DEV_MODE", "") == "true",

		AccountURL: common.Env("ACCOUNT_INTERNAL_URL", "http://localhost:8081"),
		AssetURL:   common.Env("ASSET_INTERNAL_URL", "http://localhost:8082"),
		WalletURL:  common.Env("WALLET_INTERNAL_URL", "http://localhost:8086"),
		KYCURL:     common.Env("KYC_INTERNAL_URL", "http://localhost:8087"),
		FuturesURL: common.Env("FUTURES_INTERNAL_URL", "http://localhost:8089"),
		OrderURL:   common.Env("ORDER_INTERNAL_URL", "http://localhost:8084"),
		MarketURL:  common.Env("MARKET_URL", "http://localhost:8085"),

		Require2FA:      common.Env("ADMIN_REQUIRE_2FA", "true") != "false",
		SessionIdle:     time.Duration(common.EnvInt("ADMIN_SESSION_IDLE_MINUTES", 30)) * time.Minute,
		MaxFailedLogins: common.EnvInt("ADMIN_MAX_FAILED_LOGINS", 5),
		LockoutDuration: time.Duration(common.EnvInt("ADMIN_LOCKOUT_MINUTES", 15)) * time.Minute,
		IPAllowlist:     allow,
		TrustProxy:      common.Env("ADMIN_TRUST_PROXY", "") == "true",
		MinPasswordLen:  common.EnvInt("ADMIN_MIN_PASSWORD_LEN", 10),

		BootstrapUser: common.Env("ADMIN_BOOTSTRAP_USERNAME", ""),
		BootstrapPass: common.Env("ADMIN_BOOTSTRAP_PASSWORD", ""),
		BootstrapTOTP: common.Env("ADMIN_BOOTSTRAP_TOTP_SECRET", ""),

		FeeNotice:             time.Duration(common.EnvInt("ADMIN_FEE_NOTICE_HOURS", 24)) * time.Hour,
		CouponBudgetThreshold: decEnv("ADMIN_COUPON_FINANCE_THRESHOLD", "10000"),
		RewardReviewThreshold: decEnv("ADMIN_REWARD_FINANCE_THRESHOLD", "1000"),
		WithdrawSecondAmount:  decEnv("ADMIN_WITHDRAW_SECOND_REVIEW_USDT", "5000"),
		WithdrawSecondRisk:    common.EnvInt("ADMIN_WITHDRAW_SECOND_REVIEW_RISK", 40),
		SchedulerInterval:     time.Duration(common.EnvInt("ADMIN_SCHEDULER_SECONDS", 30)) * time.Second,
	}
}
