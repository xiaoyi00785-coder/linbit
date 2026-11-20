// Command launchpad runs Linbit's 新币申购 (IEO) subsystem: users lock a base
// asset during an open window, snapshots pro-rate the allocation at settle,
// and distribute releases the new token (plus any refund) into spot wallets.
// All balance moves go through the asset service's shared platform account
// system:launchpad.
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
	common.RequireProduction("launchpad", nil, "DATABASE_URL", "INTERNAL_TOKEN")
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
	log.Printf("launchpad service listening on :%s", cfg.Port)
	if err := http.ListenAndServe(":"+cfg.Port, srv.Routes()); err != nil {
		log.Fatalf("serve: %v", err)
	}
}
