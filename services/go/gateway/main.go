// Command gateway：校验 JWT 后按路径反代。
// 上游是 account、asset、wallet、order、market、kyc、futures、strategy、growth、earn、paper。
// admin 只放行公告和 /site/social 的匿名 GET/HEAD；/internal 与 /admin 在网关直接 404。
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
