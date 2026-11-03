// Command order is the Linbit spot order service: order placement/cancel,
// current/historical orders and trade queries. It validates against per-symbol
// precision/minimum rules, freezes funds via the asset service, forwards
// commands to the matching engine, and settles fills (with fees) back through
// the asset service — all keyed for idempotency by trade id.
package main

import (
	"database/sql"
	"log"
	"net/http"
	"time"

	_ "github.com/jackc/pgx/v5/stdlib"
	"github.com/linbit/platform/services/go/common"
)

func main() {
	cfg := LoadConfig()

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

	if client := common.MaybeRedis(cfg.RedisURL); client != nil {
		defer client.Close()
	}

	store := NewStore(db)
	asset := NewAssetClient(cfg.AssetInternalURL, cfg.InternalToken)
	engine := NewHTTPMatching(cfg.EngineURL, cfg.InternalToken)
	srv := NewServer(cfg, store, asset, engine)

	addr := ":" + cfg.Port
	log.Printf("order service listening on %s (engine=%s asset=%s)", addr, cfg.EngineURL, cfg.AssetInternalURL)
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
