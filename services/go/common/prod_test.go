package common

import (
	"strings"
	"testing"
)

func TestProductionProblems(t *testing.T) {
	env := map[string]string{
		"JWT_SECRET":        "change-me-to-a-long-random-secret",
		"DATABASE_URL":      "postgres://linbit:linbit@postgres:5432/linbit",
		"REDIS_URL":         "redis://redis:6379",
		"WALLET_HOT_SECRET": "hot-mpc-mock-secret",
		"DEV_MODE":          "true",
	}
	got := ProductionProblems(func(k string) string { return env[k] },
		"JWT_SECRET", "DATABASE_URL", "REDIS_URL", "WALLET_HOT_SECRET", "INTERNAL_TOKEN")
	if len(got) != 6 {
		t.Fatalf("want 6 problems, got %v", got)
	}
	for _, p := range got {
		if strings.Contains(p, "linbit:linbit@postgres") || strings.Contains(p, "hot-mpc-mock-secret\"") {
			t.Fatalf("problem leaks value: %s", p)
		}
	}
	ok := map[string]string{
		"JWT_SECRET":   "x9f2...random",
		"DATABASE_URL": "postgres://app:S3cret@postgres:5432/linbit",
		"REDIS_URL":    "redis://:S3cret@redis:6379",
	}
	if got := ProductionProblems(func(k string) string { return ok[k] }, "JWT_SECRET", "DATABASE_URL", "REDIS_URL"); len(got) != 0 {
		t.Fatalf("unexpected problems: %v", got)
	}
	if redisHasPassword("redis://user:@redis:6379") {
		t.Fatal("empty password accepted")
	}
}

func TestTestnetSeedAllowed(t *testing.T) {
	cases := []struct {
		env, chain string
		want       bool
	}{
		{"", "", true}, {"", "mock", true}, {"development", "testnet", true},
		{"", "mainnet", false}, {"production", "testnet", false}, {"production", "mock", false}, {"", "weird", false},
	}
	for _, c := range cases {
		t.Setenv("LINBIT_ENV", c.env)
		t.Setenv("WALLET_CHAIN", c.chain)
		if got, _ := TestnetSeedAllowed(); got != c.want {
			t.Errorf("env=%q chain=%q: got %v want %v", c.env, c.chain, got, c.want)
		}
	}
}
