// Command paper provides a completely separate virtual-spot ledger and
// server-side, once-only index price alerts. It never calls the production
// order book, matching engine, balances or ledger endpoints.
package main

import (
	"context"
	"database/sql"
	"log"
	"net/http"
	"time"

	_ "github.com/jackc/pgx/v5/stdlib"
	"github.com/linbit/platform/services/go/common"
)

func main() {
	cfg := LoadConfig()
	common.RequireProduction("paper", common.ProblemIf(cfg.IndexMaxAge <= 0, "PAPER_INDEX_MAX_AGE_SECONDS must be positive"), "DATABASE_URL", "INTERNAL_TOKEN", "JWT_SECRET")
	db, err := sql.Open("pgx", cfg.DatabaseURL)
	if err != nil {
		log.Fatalf("open db: %v", err)
	}
	defer db.Close()
	db.SetMaxOpenConns(16)
	if err := waitForDB(db); err != nil {
		log.Fatalf("db not reachable: %v", err)
	}
	if err := Migrate(db); err != nil {
		log.Fatalf("migrate: %v", err)
	}
	store := NewStore(db)

	features := NewFeatureGate(cfg, store)
	market := NewMarketClient(cfg.MarketURL, cfg.InternalToken, cfg.IndexMaxAge)
	notifier := NewNotifier(cfg.AccountURL, cfg.InternalToken)
	srv := NewServer(cfg, store, features, market, notifier)
	ctx := context.Background()
	go srv.runAlertWorker(ctx)
	go srv.runDeliveryWorker(ctx)
	addr := ":" + cfg.Port
	log.Printf("paper service listening on %s (features are remote and fail closed)", addr)
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
