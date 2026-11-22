// Command chat-svc is the Linbit chat + customer-service service. It hosts the
// community trading chat rooms (moved out of market-data) and the 1:1 customer
// service (客服) surface, over REST + a WebSocket hub, backed by the shared
// PostgreSQL database and fronted by the gateway (which injects X-User-Id).
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
	common.RequireProduction("chat-svc", nil, "DATABASE_URL", "INTERNAL_TOKEN")

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

	srv := NewServer(cfg, NewStore(db))
	addr := ":" + cfg.Port
	log.Printf("chat-svc listening on %s (chat=%v cs=%v guest=%v)", addr, cfg.ChatEnabled, cfg.CSEnabled, cfg.CSGuestEnabled)
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
