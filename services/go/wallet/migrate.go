package main

// migrate.go：启动时跑 goose。和 account/asset 共用库，版本表是 goose_wallet_version。

import (
	"database/sql"
	"embed"

	"github.com/pressly/goose/v3"
)

//go:embed migrations/*.sql
var migrationsFS embed.FS

// Migrate 启动时应用尚未执行的迁移。版本表 goose_wallet_version，避免和别的服务抢 goose_db_version。
func Migrate(db *sql.DB) error {
	goose.SetBaseFS(migrationsFS)
	goose.SetTableName("goose_wallet_version")
	if err := goose.SetDialect("postgres"); err != nil {
		return err
	}
	return goose.Up(db, "migrations")
}
