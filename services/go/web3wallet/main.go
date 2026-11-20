// Command web3wallet runs Linbit's non-custodial (server-side signed) Web3
// wallet subsystem. It manages BIP39 mnemonics (encrypted at rest with
// secretbox + scrypt(PIN+pepper)), derives addresses for EVM / BTC / Solana /
// TRON, broadcasts signed transactions to each chain's RPC, and proxies swap /
// bridge / NFT / history queries to external APIs. Kept DELIBERATELY separate
// from the exchange-ledger wallet service (`services/go/wallet`): this one
// touches real chains, that one is pure accounting.
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
	// In production the pepper is mandatory; without it a database dump is
	// enough to brute-force any 6-digit PIN offline.
	var extra []string
	if cfg.KeystorePepper == "" {
		extra = append(extra, "WEB3_KEYSTORE_PEPPER is required in production")
	}
	common.RequireProduction("web3wallet", extra, "DATABASE_URL", "INTERNAL_TOKEN")
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
	go srv.runBackground(context.Background())
	log.Printf("web3wallet service listening on :%s", cfg.Port)
	if err := http.ListenAndServe(":"+cfg.Port, srv.Routes()); err != nil {
		log.Fatalf("serve: %v", err)
	}
}
