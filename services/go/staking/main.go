// Command staking runs Linbit's on-chain PoS staking service: users stake a
// supported asset with a named validator, earn daily rewards and get their
// principal back after the chain's unbonding period. Funds move through the
// asset service's platform accounts (system:staking, system:staking_rewards).
package main

import (
	"context"
	"database/sql"
	"log"
	"net/http"
	"time"

	"github.com/linbit/platform/services/go/common"

	_ "github.com/jackc/pgx/v5/stdlib"
)

func main() {
	cfg := LoadConfig()
	common.RequireProduction("staking", nil, "DATABASE_URL", "INTERNAL_TOKEN")
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
	log.Printf("staking service listening on :%s", cfg.Port)
	if err := http.ListenAndServe(":"+cfg.Port, srv.Routes()); err != nil {
		log.Fatalf("serve: %v", err)
	}
}
