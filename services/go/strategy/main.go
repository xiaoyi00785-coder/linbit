// Command strategy runs Linbit's spot grid trading bots. A bot splits a price
// range into N levels (arithmetic or geometric), buys the base needed for the
// sell side with one market order, then keeps one limit order per level: a buy
// filled at level i is answered with a sell at i+1 and vice versa. All orders go
// through the order service under the bot owner's account; the bot's own
// sub-ledger (quote balance / base held) is derived only from reported fills.
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
	common.RequireProduction("strategy", nil, "DATABASE_URL", "INTERNAL_TOKEN")
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
	srv := NewServer(cfg, db)
	go srv.Run(context.Background())
	log.Printf("strategy service listening on :%s (poll=%s order=%s market=%s)", cfg.Port, cfg.Poll, cfg.OrderURL, cfg.MarketURL)
	if err := http.ListenAndServe(":"+cfg.Port, srv.Routes()); err != nil {
		log.Fatalf("serve: %v", err)
	}
}
