package common

import (
	"fmt"
	"log"
	"os"
	"strings"
)

// IsProduction 在 LINBIT_ENV=production，或被判定为主网时返回 true。主网见 IsMainnet。
func IsProduction() bool {
	if IsMainnet() {
		return true
	}
	return strings.EqualFold(strings.TrimSpace(os.Getenv("LINBIT_ENV")), "production")
}

// ProductionProblems 检查列出的环境变量是否缺失或仍是已知的默认值。
// 返回的是问题描述，不包含变量的值本身。
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

// RequireProduction 在生产环境（见 IsProduction）发现缺失或默认配置时直接退出进程。
// extra 是各服务自己的额外问题。非生产环境什么都不做。
func RequireProduction(service string, extra []string, vars ...string) {
	if !IsProduction() {
		return
	}
	probs := append(ProductionProblems(os.Getenv, vars...), extra...)
	probs = dedupe(requireMainnet(service, probs))
	if len(probs) > 0 {
		log.Fatalf("%s: refusing to start in production: %s", service, strings.Join(probs, "; "))
	}
	log.Printf("%s: production configuration checks passed", service)
}

// ProblemIf 在 cond 为真时返回只含一条说明的切片，否则返回 nil。说明里不要放密钥。
func ProblemIf(cond bool, format string, a ...any) []string {
	if cond {
		return []string{fmt.Sprintf(format, a...)}
	}
	return nil
}

func dedupe(in []string) []string {
	seen := map[string]bool{}
	out := in[:0]
	for _, p := range in {
		if !seen[p] {
			seen[p] = true
			out = append(out, p)
		}
	}
	return out
}
