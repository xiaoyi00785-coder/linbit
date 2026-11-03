// Command futures is the Linbit USDT-margined perpetual futures service: contracts,
// a futures wallet funded from spot, leverage + cross/isolated margin, a price-time
// priority order book, one-way positions, index/mark prices, 8h funding,
// liquidation into the insurance fund, position TP/SL and futures market data.
//
// All state mutations are serialized by a single engine lock and committed in one
// database transaction each, so the order book, positions and balances never
// disagree (the book itself lives in fut_orders).
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
	common.RequireProduction("futures", nil, "DATABASE_URL", "INTERNAL_TOKEN")
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
	srv, err := NewServer(cfg, db)
	if err != nil {
		log.Fatalf("init: %v", err)
	}
	if err := srv.reloadFeeOverrides(context.Background()); err != nil {
		log.Printf("load fee overrides: %v", err)
	}
	ctx := context.Background()
	if cfg.FundingBackfill {
		go srv.BackfillFunding(ctx)
	}
	go srv.RunTicker(ctx)
	go srv.RunTransferRetry(ctx)
	if cfg.EventWorker {
		go srv.RunEvents(ctx)
	} else if err := srv.ensureEventConfig(ctx); err != nil {
		log.Printf("event: config: %v", err)
	}
	addr := ":" + cfg.Port
	log.Printf("futures service listening on %s", addr)
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
