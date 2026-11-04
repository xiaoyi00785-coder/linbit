// Command gateway is the Linbit API gateway: JWT authentication plus reverse
// proxying to the account and asset services.
package main

import (
	"log"
	"net/http"

	"github.com/linbit/platform/services/go/common"
)

func main() {
	cfg := LoadConfig()
	common.RequireProduction("gateway", common.MainnetVars("TRUSTED_PROXY_CIDRS"), "JWT_SECRET", "REDIS_URL", "CORS_ALLOWED_ORIGINS")

	client := common.MaybeRedis(cfg.RedisURL)
	if client != nil {
		defer client.Close()
	}

	gw, err := NewGateway(cfg)
	if err != nil {
		log.Fatalf("build gateway: %v", err)
	}
	gw.rdb = client
	if accessLogEnabled() {
		gw.accessLog = newAccessLogger(nil)
	}
	addr := ":" + cfg.Port
	log.Printf("gateway listening on %s (account=%s asset=%s order=%s market=%s)", addr, cfg.AccountURL, cfg.AssetURL, cfg.OrderURL, cfg.MarketURL)
	if err := http.ListenAndServe(addr, gw.Handler()); err != nil {
		log.Fatalf("serve: %v", err)
	}
}
