// marketmaker 用一个做市账户（MM_USER_ID）给每个现货和合约对挂买卖限价阶梯，
// 价格围绕 market-data 的 /market/index，按库存偏移，每隔数秒刷新。
// 指数不可用就撤掉该对报价、暂停，并发 Telegram。没有止损（产品决定）。
// 参数和启停由 admin（RBAC、2FA、审计）经 /internal/mm/* 下发。
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
	common.RequireProduction("marketmaker", nil, "DATABASE_URL", "INTERNAL_TOKEN")
	db, err := sql.Open("pgx", cfg.DatabaseURL)
	if err != nil {
		log.Fatalf("open db: %v", err)
	}
	defer db.Close()
	db.SetMaxOpenConns(5)
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
	mm := NewMaker(cfg, db)
	go mm.Run(context.Background())
	seed, why := common.TestnetSeedAllowed()
	log.Printf("marketmaker listening on :%s (account=%s testnetSeed=%v %s)", cfg.Port, cfg.UserID, seed, why)
	if err := http.ListenAndServe(":"+cfg.Port, mm.Routes()); err != nil {
		log.Fatalf("serve: %v", err)
	}
}
