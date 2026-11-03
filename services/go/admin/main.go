// Command admin is the Linbit back-office API (prototypes/admin/DESIGN.md).
// It owns admin accounts, sessions, RBAC, the append-only audit log and the
// multi-party approval workflows (fee changes, large coupons / rewards,
// two-person withdrawal review), and calls the other services' internal
// endpoints with the shared X-Internal-Token. It never touches their tables.
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
	common.RequireProduction("admin", nil, "DATABASE_URL", "INTERNAL_TOKEN")
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
