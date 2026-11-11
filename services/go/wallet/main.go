// Command wallet 是钱包服务：充值地址、扫块入账、提现（冻结、风控审核、签名、广播、确认、结算）。
// mock 链只给开发用。main() 在 WALLET_CHAIN 不是 mock 时调用 UseRealChain；
// 主网经 RequireProduction 拒绝 WALLET_CHAIN=mock。
// 真实模式走 chainkit（RPC + 外部签名进程）。种子在签名进程里，本进程只拿公钥和签名。
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
	common.RequireProduction("wallet", append(append(append(common.MainnetVars("WALLET_NETWORKS_JSON"), mainnetChainProblems()...), common.ProblemIf(chainMode() == "mock", "WALLET_CHAIN=mock (MockChain) is not allowed")...), common.ProblemIf(os.Getenv("WALLET_HOT_SECRET") == "hot-mpc-mock-secret", "WALLET_HOT_SECRET uses the hot-mpc-mock-secret default")...), "DATABASE_URL", "INTERNAL_TOKEN", "REDIS_URL")

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

	cfg.Networks = mergeTradFiNetworks(context.Background(), db, cfg.Networks)
	srv := NewServer(cfg, NewStore(db))
	if mode := chainMode(); mode != "mock" {
		if err := srv.UseRealChain(context.Background(), mode); err != nil {
			log.Fatalf("chain: %v", err)
		}
	}
	// 真实链始终跑后台 worker（WALLET_WORKER_INTERVAL，默认 30s）。
	// mock 链默认不跑，由冒烟脚本手动驱动。
	if iv := common.Env("WALLET_WORKER_INTERVAL", ""); srv.real != nil || iv != "" {
		d, err := time.ParseDuration(iv)
		if err != nil || d < time.Second {
			d = 30 * time.Second
		}
		go srv.RunWorker(context.Background(), d)
	}
	addr := ":" + cfg.Port
	log.Printf("wallet service listening on %s (chain=%s dev=%v gasMode=%s)", addr, srv.chain.Kind(), cfg.DevMode, cfg.GasDeductMode)
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
