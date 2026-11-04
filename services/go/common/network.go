package common

import (
	"log"
	"net"
	"net/url"
	"os"
	"strings"
)

// Network is the single deployment switch: NETWORK (or legacy APP_ENV) =
// mainnet | testnet | dev. Empty means dev/testnet behaviour (unchanged).
func Network() string {
	v := strings.ToLower(strings.TrimSpace(os.Getenv("NETWORK")))
	if v == "" {
		v = strings.ToLower(strings.TrimSpace(os.Getenv("APP_ENV")))
	}
	return v
}

// IsMainnet reports NETWORK=mainnet (or APP_ENV=mainnet). Mainnet implies
// production hardening (IsProduction) and forbids every virtual/test path.
func IsMainnet() bool { return Network() == "mainnet" }

// mainnetForbiddenTrue lists flags that must not be truthy on mainnet.
var mainnetForbiddenTrue = []string{
	"DEV_MODE", "NEXT_PUBLIC_ENABLE_MOCK", "NEXT_PUBLIC_DEMO", "ENABLE_MOCK",
	"MM_TESTNET_SEED", "ADMIN_MM_SEED", "TESTNET_FAUCET", "FAUCET_ENABLED",
	"VIRTUAL_BALANCES", "TRADFI_VIRTUAL_INVENTORY", "ADMIN_USER_CREDIT_ENABLED",
}

func truthy(v string) bool {
	switch strings.ToLower(strings.TrimSpace(v)) {
	case "1", "true", "yes", "on":
		return true
	}
	return false
}

// MainnetProblems returns every virtual/test setting that is incompatible
// with mainnet (names only, never values).
func MainnetProblems(lookup func(string) string) []string {
	var out []string
	if a, n := strings.ToLower(strings.TrimSpace(lookup("APP_ENV"))), strings.ToLower(strings.TrimSpace(lookup("NETWORK"))); a != "" && n != "" && a != n {
		out = append(out, "NETWORK and APP_ENV disagree")
	}
	if c := strings.ToLower(strings.TrimSpace(lookup("WALLET_CHAIN"))); c != "mainnet" {
		out = append(out, "WALLET_CHAIN must be mainnet (mock/testnet chains and virtual assets are forbidden)")
	}
	for _, k := range mainnetForbiddenTrue {
		if truthy(lookup(k)) {
			out = append(out, k+" must not be enabled on mainnet")
		}
	}
	if le := strings.TrimSpace(lookup("LINBIT_ENV")); le != "" && !strings.EqualFold(le, "production") {
		out = append(out, "LINBIT_ENV must be production (or unset) on mainnet")
	}
	return out
}

// MainnetRequired are vars every service needs on mainnet (in addition to
// the per-service list passed to RequireProduction).
var MainnetRequired []string

// MainnetVars returns problems for service-specific vars required only on
// mainnet (empty/change-me). No-op off mainnet.
func MainnetVars(vars ...string) []string {
	if !IsMainnet() {
		return nil
	}
	return ProductionProblems(func(k string) string {
		if k == "DEV_MODE" {
			return ""
		}
		return os.Getenv(k)
	}, vars...)
}

// MainnetIf is ProblemIf that only applies on mainnet.
func MainnetIf(cond bool, format string, a ...any) []string {
	if !IsMainnet() {
		return nil
	}
	return ProblemIf(cond, format, a...)
}

func requireMainnet(service string, probs []string) []string {
	if !IsMainnet() {
		return probs
	}
	probs = append(probs, MainnetProblems(os.Getenv)...)
	probs = append(probs, DBSSLProblems(os.Getenv("DATABASE_URL"))...)
	probs = append(probs, ProductionProblems(os.Getenv, MainnetRequired...)...)
	if !TelegramConfigured() {
		probs = append(probs, "TELEGRAM_BOT_TOKEN and TELEGRAM_CHAT_ID must be set on mainnet (alerts)")
	}
	log.Printf("%s: NETWORK=mainnet — virtual seed, faucets, mock chains and demo data are disabled", service)
	return probs
}

// DBSSLProblems: on mainnet the DB connection must use TLS unless the host is
// private (loopback, RFC1918/ULA, link-local, or a docker/compose service name
// / *.internal / *.local). Unset sslmode (libpq "prefer") counts as disable.
func DBSSLProblems(dsn string) []string {
	if strings.TrimSpace(dsn) == "" {
		return nil
	}
	u, err := url.Parse(dsn)
	if err != nil || u.Host == "" {
		return []string{"DATABASE_URL must be a postgres:// URL on mainnet"}
	}
	switch strings.ToLower(u.Query().Get("sslmode")) {
	case "require", "verify-ca", "verify-full":
		return nil
	}
	if PrivateDBHost(u.Hostname()) {
		return nil
	}
	return []string{"DATABASE_URL: sslmode=require (or verify-full) is required for a non-private DB host on mainnet"}
}

// PrivateDBHost reports whether host is a loopback/private IP or an internal
// (dot-less docker service, *.internal, *.local) name.
func PrivateDBHost(host string) bool {
	h := strings.ToLower(strings.TrimSuffix(host, "."))
	if h == "" {
		return false
	}
	if ip := net.ParseIP(h); ip != nil {
		return ip.IsLoopback() || ip.IsPrivate() || ip.IsLinkLocalUnicast()
	}
	return h == "localhost" || !strings.Contains(h, ".") || strings.HasSuffix(h, ".internal") || strings.HasSuffix(h, ".local")
}
