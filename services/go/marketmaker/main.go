// Command marketmaker copies external order books (Binance depth, OKX backup)
// onto every listed spot and futures pair, under one market-maker account
// (MM_USER_ID). Index prices still come from market-data /market/index (mark,
// inventory, fail-closed). When the index or the book is unavailable it
// cancels that pair's quotes, pauses it and sends a Telegram alert. There is
// no stop-loss (owner decision). Parameters and start/stop are driven by the
// admin service (RBAC + 2FA + audit) via /internal/mm/*.
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
	common.RequireProduction("marketmaker", nil, "DATABASE_URL", "INTERNAL_TOKEN")
	db, err := sql.Open("pgx", cfg.DatabaseURL)
	if err != nil {
		log.Fatalf("open db: %v", err)
	}
	defer db.Close()
	db.SetMaxOpenConns(5)
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
	mm := NewMaker(cfg, db)
	go mm.Run(context.Background())
	seed, why := common.TestnetSeedAllowed()
	log.Printf("marketmaker listening on :%s (account=%s testnetSeed=%v %s)", cfg.Port, cfg.UserID, seed, why)
	if err := http.ListenAndServe(":"+cfg.Port, mm.Routes()); err != nil {
		log.Fatalf("serve: %v", err)
	}
}
