// Command options is the Linbit European options service: an instrument
// registry (BTC-30DEC26-50000-C style), one price-time order book per
// instrument, Black-Scholes mark price + Greeks on a 1-second worker,
// cash-settled expiry against the index price from market-data, and a
// SPAN-style initial margin for short positions.
//
// Money moves through system:options in the asset ledger, so every
// debit/credit is idempotent on ref and the single engine lock keeps the
// book, positions and locked margin in sync.
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
	common.RequireProduction("options", nil, "DATABASE_URL", "INTERNAL_TOKEN")
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
	ctx := context.Background()
	go srv.RunTicker(ctx)
	go srv.RunPayouts(ctx)
	addr := ":" + cfg.Port
	log.Printf("options service listening on %s", addr)
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
