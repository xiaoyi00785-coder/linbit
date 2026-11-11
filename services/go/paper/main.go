// 模拟盘：独立虚拟现货账本（paper_balances），以及服务端只触发一次的指数价预警。
// 不调用生产订单簿、撮合、真实余额或账本接口。合约模拟只读写 paper_fut_*，不写 fut_accounts。
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
	common.RequireProduction("paper", append(common.ProblemIf(cfg.IndexMaxAge <= 0, "PAPER_INDEX_MAX_AGE_SECONDS must be positive"), common.ProblemIf(cfg.AlertWriteLimit <= 0 || cfg.AlertWriteWindow <= 0, "PRICE_ALERT_WRITE_LIMIT and PRICE_ALERT_WRITE_WINDOW_SECONDS must be positive")...), "DATABASE_URL", "INTERNAL_TOKEN", "JWT_SECRET")
	db, err := sql.Open("pgx", cfg.DatabaseURL)
	if err != nil {
		log.Fatalf("open db: %v", err)
	}
	defer db.Close()
	db.SetMaxOpenConns(16)
	if err := waitForDB(db); err != nil {
		log.Fatalf("db not reachable: %v", err)
	}
	if err := Migrate(db); err != nil {
		log.Fatalf("migrate: %v", err)
	}
	store := NewStore(db)

	features := NewFeatureGate(cfg, store)
	market := NewMarketClient(cfg.MarketURL, cfg.InternalToken, cfg.IndexMaxAge)
	notifier := NewNotifier(cfg.AccountURL, cfg.InternalToken)
	srv := NewServer(cfg, store, features, market, notifier)
	ctx := context.Background()
	go srv.runAlertWorker(ctx)
	go srv.runDeliveryWorker(ctx)
	go func() {
		t := time.NewTicker(2 * time.Second)
		defer t.Stop()
		for {
			select {
			case <-ctx.Done():
				return
			case <-t.C:
				if err := store.LiquidationSweep(ctx, srv.futSource()); err != nil {
					log.Printf("paper futures liquidation: %v", err)
				}
			}
		}
	}()
	addr := ":" + cfg.Port
	log.Printf("paper service listening on %s (features are remote and fail closed)", addr)
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
