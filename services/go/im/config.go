package main

import (
	"time"

	"github.com/linbit/platform/services/go/common"
)

// Config is the im service runtime configuration.
type Config struct {
	Port        string
	DatabaseURL string
	JWTSecret   string
	RedisURL    string

	// IMTokenTTL is the TTL of the im websocket JWT (short-lived; the Flutter
	// SDK refreshes on reconnect).
	IMTokenTTL time.Duration

	// AssetInternalURL + InternalToken are used for redpacket ledger moves.
	AssetInternalURL string
	// AccountInternalURL is reserved for future user-profile lookups; the
	// service works without it.
	AccountInternalURL string
	InternalToken      string

	// RedPacketExpireHours is how long an unclaimed redpacket remains claimable
	// before the sweeper refunds the remainder to the sender.
	RedPacketExpireHours int

	// RedPacketMaxUSDT caps the amount of a single redpacket (decimal string).
	RedPacketMaxUSDT string
	// RedPacketMinUSDT is the per-redpacket floor.
	RedPacketMinUSDT string
	// RedPacketMaxShares caps the pieces of a single redpacket.
	RedPacketMaxShares int

	// RecallWindow is how long a sender may recall a message after send.
	RecallWindow time.Duration

	// DevMode exposes a couple of test-only endpoints and loosens validation
	// (for seeding fixtures). Never enable in production.
	DevMode bool
}

// LoadConfig reads configuration from the environment.
func LoadConfig() Config {
	return Config{
		Port:                 common.Env("PORT", "8096"),
		DatabaseURL:          common.MustEnv("DATABASE_URL"),
		JWTSecret:            common.MustEnv("JWT_SECRET"),
		RedisURL:             common.Env("REDIS_URL", ""),
		IMTokenTTL:           time.Duration(common.EnvInt("IM_TOKEN_TTL_SECONDS", 86400)) * time.Second,
		AssetInternalURL:     common.Env("ASSET_INTERNAL_URL", ""),
		AccountInternalURL:   common.Env("ACCOUNT_INTERNAL_URL", ""),
		InternalToken:        common.Env("INTERNAL_TOKEN", ""),
		RedPacketExpireHours: common.EnvInt("IM_REDPACKET_EXPIRE_HOURS", 24),
		RedPacketMaxUSDT:     common.Env("IM_REDPACKET_MAX_USDT", "20000"),
		RedPacketMinUSDT:     common.Env("IM_REDPACKET_MIN_USDT", "0.01"),
		RedPacketMaxShares:   common.EnvInt("IM_REDPACKET_MAX_SHARES", 200),
		RecallWindow:         time.Duration(common.EnvInt("IM_RECALL_WINDOW_SECONDS", 120)) * time.Second,
		DevMode:              common.Env("DEV_MODE", "") == "true",
	}
}
