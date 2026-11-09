// USDT 本位永续：合约、从现货划入的合约钱包、杠杆、全仓/逐仓、价时优先订单簿、
// 单向(BOTH)与对冲(LONG/SHORT)、指数价/标记价、默认 8 小时资金费、强平进保险基金、仓位止盈止损与行情。
// 状态变更由引擎单锁串行，每次一个数据库事务；订单簿在 fut_orders，与仓位、余额一起提交。
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
	common.RequireProduction("futures", nil, "DATABASE_URL", "INTERNAL_TOKEN")
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
	if err := srv.reloadFeeOverrides(context.Background()); err != nil {
		log.Printf("load fee overrides: %v", err)
	}
	ctx := context.Background()
	if cfg.FundingBackfill {
		go srv.BackfillFunding(ctx)
	}
	go srv.RunTicker(ctx)
	go srv.RunTransferRetry(ctx)
	if cfg.EventWorker {
		go srv.RunEvents(ctx)
	} else if err := srv.ensureEventConfig(ctx); err != nil {
		log.Printf("event: config: %v", err)
	}
	addr := ":" + cfg.Port
	log.Printf("futures service listening on %s", addr)
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
