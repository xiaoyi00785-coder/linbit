package main

// 启动时跑 goose。版本表是 goose_futures_version，避免和 account/asset/wallet 抢默认的 goose_db_version。

import (
	"database/sql"
	"embed"

	"github.com/pressly/goose/v3"
)

//go:embed migrations/*.sql
var migrationsFS embed.FS

// Migrate 只执行尚未应用的迁移。版本表 goose_futures_version，与其他服务互不覆盖。
func Migrate(db *sql.DB) error {
	goose.SetBaseFS(migrationsFS)
	goose.SetTableName("goose_futures_version")
	if err := goose.SetDialect("postgres"); err != nil {
		return err
	}
	return goose.Up(db, "migrations")
}
