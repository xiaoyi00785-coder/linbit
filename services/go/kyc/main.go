// kyc 收受控证件，以及基础（level 1）和高级（level 2）身份申请。
// 状态机：同一用户同时只能有一笔 pending；拒绝后可重提；高级必须已有通过的基础认证。
// 提供管理端审核队列。通过后把 kycLevel 同步回 account，钱包按 KYC 档位限制提现。
// 证件走可插拔 ObjectStorage（默认本地），不公开下载，只经内部令牌保护的管理接口读取。
package main

import (
	"context"
	"database/sql"
	"log"
	"net/http"
	"os"
	"strings"
	"time"

	_ "github.com/jackc/pgx/v5/stdlib"
	"github.com/linbit/platform/services/go/common"
)

func main() {
	cfg := LoadConfig()
	// 主网证件放本地盘（持久卷加备份，产品决定）；改用 S3 时要另给凭证。
	kycStore := common.MainnetVars("KYC_DOC_STORAGE_DIR")
	if strings.EqualFold(os.Getenv("STORAGE_BACKEND"), "s3") {
		kycStore = common.MainnetVars("S3_BUCKET", "S3_REGION", "S3_ACCESS_KEY_ID", "S3_SECRET_ACCESS_KEY")
	}
	common.RequireProduction("kyc", kycStore, "DATABASE_URL", "INTERNAL_TOKEN", "REDIS_URL")

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
	go srv.RunLevelSync(context.Background())
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
