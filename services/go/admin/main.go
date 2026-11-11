// Command admin：后台账号、会话、RBAC、只追加的审计，以及费用、大额卡券/奖励、提现双人复核、人工调账。
// 调其他服务只用 X-Internal-Token，不写它们的表。
// 登录失败记在 admin_users.failed_attempts，满 ADMIN_MAX_FAILED_LOGINS（默认 5）锁定 ADMIN_LOCKOUT_MINUTES（默认 15）。
// ADMIN_IP_ALLOWLIST 为空则不限来源 IP。
package main

import (
	"context"
	"database/sql"
	"github.com/linbit/platform/services/go/common"
	"log"
	"net/http"
	"time"

	_ "github.com/jackc/pgx/v5/stdlib"
)

func main() {
	cfg := LoadConfig()
	common.RequireProduction("admin", common.MainnetIf(cfg.SolvencyHourUTC < 0 || cfg.SolvencyHourUTC > 23, "SOLVENCY_HOUR_UTC must be 0-23 on mainnet (daily solvency check)"), "DATABASE_URL", "INTERNAL_TOKEN")
	if common.IsProduction() && len(cfg.IPAllowlist) == 0 {
		log.Printf("WARN admin: ADMIN_IP_ALLOWLIST is empty in production; every source IP can reach the back office (rely on nginx/firewall)")
	}

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

	srv := NewServer(cfg, db)
	if err := srv.Bootstrap(context.Background()); err != nil {
		log.Fatalf("bootstrap: %v", err)
	}
	go srv.RunScheduler(context.Background())
	go srv.RunSolvencyDaily(context.Background())

	addr := ":" + cfg.Port
	log.Printf("admin service listening on %s (2fa=%v idle=%s feeNotice=%s)", addr, cfg.Require2FA, cfg.SessionIdle, cfg.FeeNotice)
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
