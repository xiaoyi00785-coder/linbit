package main

// migrate.go：启动时跑 goose。和 account 共用一个库，版本表用 goose_asset_version，避免抢 goose_db_version。

import (
	"database/sql"
	"embed"

	"github.com/pressly/goose/v3"
)

//go:embed migrations/*.sql
var migrationsFS embed.FS

// Migrate 启动时应用尚未执行的 goose 迁移。
func Migrate(db *sql.DB) error {
	goose.SetBaseFS(migrationsFS)
	// account 和 asset 共用一个库，版本表分开，迁移不会在默认的 goose_db_version 上抢。
	goose.SetTableName("goose_asset_version")
	if err := goose.SetDialect("postgres"); err != nil {
		return err
	}
	return goose.Up(db, "migrations")
}
