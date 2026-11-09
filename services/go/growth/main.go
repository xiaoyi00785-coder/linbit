// growth 做邀请归因、合伙人佣金、普通邀请返佣、积分、新人礼包/活动中心和卡券。
// 用内部游标跟上用户、现货成交、合约成交和钱包流水，奖励经 asset 入账。
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
	common.RequireProduction("growth", nil, "DATABASE_URL", "INTERNAL_TOKEN")
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
	log.Printf("growth service listening on :%s", cfg.Port)
	if err := http.ListenAndServe(":"+cfg.Port, srv.Routes()); err != nil {
		log.Fatalf("serve: %v", err)
	}
}
