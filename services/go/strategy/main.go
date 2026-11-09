// strategy 跑现货网格。区间分成 N 档（等差或等比），先用一笔市价买入卖侧所需的 base，
// 再每档挂一笔限价：i 档买单成交后在 i+1 挂卖，i 档卖单成交后在 i-1 挂买。
// 订单经 order 服务，挂在机器人所属用户名下；子账（报价余额、持有 base）只按回报的成交推导。
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
	common.RequireProduction("strategy", nil, "DATABASE_URL", "INTERNAL_TOKEN")
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
	log.Printf("strategy service listening on :%s (poll=%s order=%s market=%s)", cfg.Port, cfg.Poll, cfg.OrderURL, cfg.MarketURL)
	if err := http.ListenAndServe(":"+cfg.Port, srv.Routes()); err != nil {
		log.Fatalf("serve: %v", err)
	}
}
