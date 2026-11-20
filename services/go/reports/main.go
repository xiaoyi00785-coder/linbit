// Command reports runs Linbit's reports / exports / notification-subscription
// service. It exposes POST /reports/exports (async job), GET /reports/exports
// and GET /reports/exports/{jobId}, POST /reports/tax (annual PDF),
// GET|POST|DELETE /notifications/subscriptions, and WS /account/events.
package main

import (
	"context"
	"database/sql"
	"log"
	"net/http"
	"os"
	"time"

	_ "github.com/jackc/pgx/v5/stdlib"
	"github.com/linbit/platform/services/go/common"
)

func main() {
	cfg := LoadConfig()
	common.RequireProduction("reports", nil, "DATABASE_URL", "INTERNAL_TOKEN", "JWT_SECRET")

	db, err := sql.Open("pgx", cfg.DatabaseURL)
	if err != nil {
		log.Fatalf("open db: %v", err)
	}
	defer db.Close()
	db.SetMaxOpenConns(10)

	for i := 0; i < 30; i++ {
		if err = db.Ping(); err == nil {
			break
		}
		time.Sleep(time.Second)
	}
	if err != nil {
		log.Fatalf("db not reachable: %v", err)
	}

	if err := Migrate(db); err != nil {
		log.Fatalf("migrate: %v", err)
	}
	if err := os.MkdirAll(cfg.ExportsDir, 0o755); err != nil {
		log.Fatalf("exports dir: %v", err)
	}

	srv := NewServer(cfg, db)
	go srv.RunExportWorker(context.Background())
	go srv.RunDepositWatcher(context.Background())
	go srv.RunExpiryJanitor(context.Background())

	addr := ":" + cfg.Port
	log.Printf("reports service listening on %s", addr)
	if err := http.ListenAndServe(addr, srv.Routes()); err != nil {
		log.Fatalf("serve: %v", err)
	}
}
