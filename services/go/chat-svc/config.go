package main

import (
	"strings"
	"time"

	"github.com/linbit/platform/services/go/common"
)

// Config is the chat-svc runtime configuration (environment-driven).
type Config struct {
	Port          string
	DatabaseURL   string
	InternalToken string
	DevMode       bool

	// ChatEnabled gates the community trading chat rooms (was a market-data
	// feature flag; here it is a simple env switch).
	ChatEnabled bool
	// CSEnabled gates the 1:1 customer-service surface.
	CSEnabled bool
	// CSGuestEnabled allows not-logged-in visitors to open CS conversations.
	CSGuestEnabled bool

	// Upstreams for room building / KYC gate (shared Postgres is read directly).
	AccountURL string
	FuturesURL string

	// CS image uploads (optional).
	UploadDir      string
	UploadMaxBytes int64

	// Guest anti-abuse.
	GuestMinIntervalMs int
	GuestMaxPerHour    int
}

// LoadConfig reads configuration from the environment.
func LoadConfig() Config {
	return Config{
		Port:               common.Env("PORT", "8095"),
		DatabaseURL:        common.MustEnv("DATABASE_URL"),
		InternalToken:      common.Env("INTERNAL_TOKEN", ""),
		DevMode:            common.Env("DEV_MODE", "") == "true",
		ChatEnabled:        common.Env("CHAT_ENABLED", "true") != "false",
		CSEnabled:          common.Env("CS_ENABLED", "true") != "false",
		CSGuestEnabled:     common.Env("CS_GUEST_ENABLED", "true") != "false",
		AccountURL:         common.Env("ACCOUNT_URL", "http://localhost:8081"),
		FuturesURL:         common.Env("FUTURES_URL", "http://localhost:8089"),
		UploadDir:          common.Env("CS_UPLOAD_DIR", "/data/cs-uploads"),
		UploadMaxBytes:     int64(common.EnvInt("CS_UPLOAD_MAX_BYTES", 5*1024*1024)),
		GuestMinIntervalMs: common.EnvInt("CS_GUEST_MIN_INTERVAL_MS", 3000),
		GuestMaxPerHour:    common.EnvInt("CS_GUEST_MAX_PER_HOUR", 60),
	}
}

func splitCSV(s string) []string {
	var out []string
	for _, p := range strings.Split(s, ",") {
		if p = strings.TrimSpace(p); p != "" {
			out = append(out, p)
		}
	}
	return out
}

var _ = splitCSV
var _ = time.Second
