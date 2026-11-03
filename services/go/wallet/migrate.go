package main

import (
	"database/sql"
	"embed"

	"github.com/pressly/goose/v3"
)

//go:embed migrations/*.sql
var migrationsFS embed.FS

// Migrate applies all pending goose migrations on startup. The wallet service
// shares the database with account/asset, so it uses its own version table to
// avoid racing on the default goose_db_version.
func Migrate(db *sql.DB) error {
	goose.SetBaseFS(migrationsFS)
	goose.SetTableName("goose_wallet_version")
	if err := goose.SetDialect("postgres"); err != nil {
		return err
	}
	return goose.Up(db, "migrations")
}
