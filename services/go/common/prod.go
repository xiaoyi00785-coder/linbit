package common

import (
	"fmt"
	"log"
	"os"
	"strings"
)

// IsProduction reports whether LINBIT_ENV=production.
func IsProduction() bool {
	return strings.EqualFold(strings.TrimSpace(os.Getenv("LINBIT_ENV")), "production")
}

// ProductionProblems checks the named env vars for missing or known-default
// values and returns a description of each problem (never the value itself).
func ProductionProblems(lookup func(string) string, vars ...string) []string {
	var out []string
	for _, k := range vars {
		v := strings.TrimSpace(lookup(k))
		switch {
		case v == "":
			out = append(out, k+" is empty")
		case strings.Contains(strings.ToLower(v), "change-me"):
			out = append(out, k+" still uses a change-me default")
		case k == "DATABASE_URL" && strings.Contains(v, "linbit:linbit@"):
			out = append(out, k+" uses the default linbit:linbit credentials")
		case k == "REDIS_URL" && !redisHasPassword(v):
			out = append(out, k+" has no password")
		case k == "WALLET_HOT_SECRET" && v == "hot-mpc-mock-secret":
			out = append(out, k+" uses the hot-mpc-mock-secret default")
		}
	}
	if strings.EqualFold(lookup("DEV_MODE"), "true") {
		out = append(out, "DEV_MODE=true is not allowed")
	}
	return out
}

func redisHasPassword(u string) bool {
	rest := u
	if i := strings.Index(rest, "://"); i >= 0 {
		rest = rest[i+3:]
	}
	at := strings.LastIndex(rest, "@")
	if at < 0 {
		return false
	}
	cred := rest[:at]
	if i := strings.Index(cred, ":"); i >= 0 {
		cred = cred[i+1:]
	}
	return cred != ""
}

// RequireProduction terminates the process when LINBIT_ENV=production and any
// of the given env vars is missing or a known default. extra lists additional
// service-specific problems. It is a no-op outside production.
func RequireProduction(service string, extra []string, vars ...string) {
	if !IsProduction() {
		return
	}
	probs := append(ProductionProblems(os.Getenv, vars...), extra...)
	if len(probs) > 0 {
		log.Fatalf("%s: refusing to start in production: %s", service, strings.Join(probs, "; "))
	}
	log.Printf("%s: production configuration checks passed", service)
}

// ProblemIf returns a one-element slice with msg when cond holds.
func ProblemIf(cond bool, format string, a ...any) []string {
	if cond {
		return []string{fmt.Sprintf(format, a...)}
	}
	return nil
}
