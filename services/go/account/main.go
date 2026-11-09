// Command account：注册、登录、刷新、登出和 TOTP。
// 登出把 unix 毫秒写到 Redis auth:revoked_before:{uid}，网关和本服务用 iat 比较。
// 密码错误不计次。登录二次验证码的限流键是 vc:int:login:{target} 与 vc:hr:login:{target}。
package main

import (
	"database/sql"
	"log"
	"net/http"
	"os"
	"strings"
	"time"

	_ "github.com/jackc/pgx/v5/stdlib"
	"github.com/linbit/platform/services/go/common"
)

func main() {
	cfg := LoadConfig()
	common.RequireProduction("account", append(append(append(common.MainnetVars("GROWTH_INTERNAL_URL", "GROWTH_INVITE_BASE_URL", "WEBAUTHN_RP_ID", "WEBAUTHN_RP_ORIGINS"), common.MainnetIf(!strings.HasPrefix(os.Getenv("GROWTH_INVITE_BASE_URL"), "https://"), "GROWTH_INVITE_BASE_URL must be https on mainnet")...), common.ProblemIf(cfg.ResendAPIKey == "" || cfg.ResendFrom == "", "RESEND_API_KEY and RESEND_FROM must be set (the log email sender is not allowed)")...), common.ProblemIf(cfg.VIPEligibilitySkip, "VIP_ELIGIBILITY_CHECK=off is not allowed")...), "DATABASE_URL", "INTERNAL_TOKEN", "JWT_SECRET", "REDIS_URL")

	db, err := sql.Open("pgx", cfg.DatabaseURL)
	if err != nil {
		log.Fatalf("open db: %v", err)
	}
	defer db.Close()
	db.SetMaxOpenConns(10)

	if err := waitForDB(db); err != nil {
		log.Fatalf("db not reachable: %v", err)
	}
	if err := Migrate(db); err != nil {
		log.Fatalf("migrate: %v", err)
	}

	// Redis 可选：验证码限流，以及 access 吊销截止时间。连不上则为 nil，限流改查库，吊销检查放行。
	rdb := common.MaybeRedis(cfg.RedisURL)
	if rdb != nil {
		defer rdb.Close()
	}

	srv := NewServer(cfg, NewStore(db), rdb)
	addr := ":" + cfg.Port
	log.Printf("account service listening on %s", addr)
	if err := http.ListenAndServe(addr, srv.Routes()); err != nil {
		log.Fatalf("serve: %v", err)
	}
}

func waitForDB(db *sql.DB) error {
	var err error
	for i := 0; i < 30; i++ {
		if err = db.Ping(); err == nil {
			return nil
		}
		time.Sleep(time.Second)
	}
	return err
}
