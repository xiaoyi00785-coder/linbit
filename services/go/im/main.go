// Command im is the Linbit instant-messaging service: 1v1 and group chats,
// message history, recall/read/forward, redpackets and a WebSocket fanout for
// real-time delivery. It replaces the EasiCoin / Tencent IM integration with a
// native Linbit implementation signed with the shared JWT secret.
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
	common.RequireProduction("im",
		common.ProblemIf(cfg.AssetInternalURL == "" || cfg.InternalToken == "",
			"ASSET_INTERNAL_URL and INTERNAL_TOKEN must be set (redpacket ledger moves require the asset service)"),
		"DATABASE_URL", "JWT_SECRET",
	)

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

	rdb := common.MaybeRedis(cfg.RedisURL)
	if rdb != nil {
		defer rdb.Close()
	}

	store := NewStore(db)
	srv := NewServer(cfg, store, rdb)

	// Background sweeper: refund expired redpackets to their senders. Never
	// started in tests that construct the server directly.
	go srv.sweepRedPackets(context.Background())

	addr := ":" + cfg.Port
	log.Printf("im service listening on %s", addr)
	if err := http.ListenAndServe(addr, srv.Routes()); err != nil {
		log.Fatalf("serve: %v", err)
	}
	_ = os.Stdout
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
