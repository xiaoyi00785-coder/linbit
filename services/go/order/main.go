// 现货订单服务：下单/撤单、当前与历史订单、成交查询。按交易对精度与最小量校验
// （超出精度的价格/数量直接拒绝，不截断），经资产服务冻结后把指令交给撮合引擎，
// 再按 tradeId 向资产服务幂等结算成交和手续费。
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
	common.RequireProduction("order", nil, "DATABASE_URL", "INTERNAL_TOKEN", "REDIS_URL")

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
	if err := srv.LoadFees(context.Background()); err != nil {
		log.Printf("load fee overrides: %v (using env defaults)", err)
	}

	if err := srv.LoadUserFees(context.Background()); err != nil {
		log.Printf("load user fee overrides: %v", err)
	}
	go srv.RunBackground(context.Background())

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
