// Command kyc is the Linbit KYC (identity verification) service. It accepts
// controlled document uploads, basic (level 1) and advanced (level 2) identity
// submissions, enforces the submission state machine (one pending application at
// a time, re-submit after rejection, advanced requires an approved basic), and
// exposes an admin review queue. On approval it syncs the user's kycLevel back to
// the account service so the wallet can apply KYC-tiered withdrawal limits.
//
// Documents are stored through the shared pluggable ObjectStorage (local backend
// by default) and are never publicly served: they are read back only through the
// internal-token-guarded admin route.
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

	srv := NewServer(cfg, NewStore(db))
	addr := ":" + cfg.Port
	log.Printf("kyc service listening on %s (minAge=%d docMax=%dB)", addr, cfg.MinAge, cfg.DocMaxBytes)
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
