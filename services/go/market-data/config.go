package main

import (
	"strings"

	"github.com/linbit/platform/services/go/common"
)

// Config holds the market-data service runtime configuration.
type Config struct {
	Port          string
	DatabaseURL   string
	InternalToken string
	// EngineURL is used only to bootstrap a full depth snapshot on startup
	// (restart recovery); live depth comes from the event stream.
	EngineURL string
	RedisURL  string
	// Symbols optionally overrides the tracked trading pairs; when empty the
	// list is read from the shared `symbols` table.
	Symbols []string
}

// LoadConfig reads configuration from the environment.
func LoadConfig() Config {
	return Config{
		Port:          common.Env("PORT", "8085"),
		DatabaseURL:   common.MustEnv("DATABASE_URL"),
		InternalToken: common.Env("INTERNAL_TOKEN", ""),
		EngineURL:     common.Env("ENGINE_URL", "http://localhost:8083"),
		RedisURL:      common.Env("REDIS_URL", ""),
		Symbols:       splitCSV(common.Env("SYMBOLS", "")),
	}
}

// splitCSV splits a comma list into trimmed, non-empty entries.
func splitCSV(s string) []string {
	var out []string
	for _, p := range strings.Split(s, ",") {
		if p = strings.TrimSpace(p); p != "" {
			out = append(out, p)
		}
	}
	return out
}
