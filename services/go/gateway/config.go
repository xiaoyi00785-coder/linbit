package main

import "github.com/linbit/platform/services/go/common"

// Config holds the gateway runtime configuration.
type Config struct {
	Port        string
	JWTSecret   string
	AccountURL  string
	AssetURL    string
	WalletURL   string
	OrderURL    string
	MarketURL   string
	KycURL      string
	FuturesURL  string
	StrategyURL string
	GrowthURL   string
	EarnURL     string
	// AdminURL is used ONLY for the public read-only GET /announcements.
	AdminURL string
	RedisURL string
	// InternalToken authenticates the gateway to account /internal/api-keys/verify and
	// market-data/kyc internal lookups (TradFi gate).
	InternalToken string
	// ClientIPHeader names the trusted header carrying the real client IP (set by
	// nginx); used for API-key IP allowlists. Empty = TCP peer address.
	ClientIPHeader string
	// TrustedProxyCIDRs: peers whose CF-IPCountry header is trusted (TradFi
	// region gate). Default Cloudflare ranges; anything else = country unknown.
	TrustedProxyCIDRs string
	// EdgeSecret (GATEWAY_EDGE_SECRET, >=32 chars): the web/partner Next servers
	// send it as X-Linbit-Edge-Auth when calling the gateway directly; such
	// requests may assert X-Real-IP / CF-IPCountry like a trusted proxy peer.
	EdgeSecret string
	// AllowedOrigins is the CORS allow-list (empty = dev default: localhost/127.0.0.1).
	AllowedOrigins []string
}

// LoadConfig reads configuration from the environment.
func LoadConfig() Config {
	return Config{
		Port:              common.Env("PORT", "8080"),
		JWTSecret:         common.MustEnv("JWT_SECRET"),
		AccountURL:        common.Env("ACCOUNT_URL", "http://localhost:8081"),
		AssetURL:          common.Env("ASSET_URL", "http://localhost:8082"),
		WalletURL:         common.Env("WALLET_URL", "http://localhost:8086"),
		OrderURL:          common.Env("ORDER_URL", "http://localhost:8084"),
		MarketURL:         common.Env("MARKET_URL", "http://localhost:8085"),
		KycURL:            common.Env("KYC_URL", "http://localhost:8087"),
		FuturesURL:        common.Env("FUTURES_URL", "http://localhost:8089"),
		StrategyURL:       common.Env("STRATEGY_URL", "http://localhost:8090"),
		GrowthURL:         common.Env("GROWTH_URL", "http://localhost:8091"),
		EarnURL:           common.Env("EARN_URL", "http://localhost:8092"),
		AdminURL:          common.Env("ADMIN_URL", "http://localhost:8088"),
		RedisURL:          common.Env("REDIS_URL", ""),
		InternalToken:     common.Env("INTERNAL_TOKEN", ""),
		ClientIPHeader:    common.Env("GATEWAY_CLIENT_IP_HEADER", "X-Real-IP"),
		TrustedProxyCIDRs: common.Env("TRUSTED_PROXY_CIDRS", DefaultTrustedProxyCIDRs),
		EdgeSecret:        common.Env("GATEWAY_EDGE_SECRET", ""),
		AllowedOrigins:    splitOrigins(common.Env("CORS_ALLOWED_ORIGINS", "")),
	}
}
