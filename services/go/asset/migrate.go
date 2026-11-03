package main

import (
	"database/sql"
	"embed"

	"github.com/pressly/goose/v3"
)

//go:embed migrations/*.sql
var migrationsFS embed.FS

// Migrate applies all pending goose migrations on startup.
func Migrate(db *sql.DB) error {
	goose.SetBaseFS(migrationsFS)
	// Account and asset share one database; use a service-specific version
	// table so their migrations never race on the default goose_db_version.
	goose.SetTableName("goose_asset_version")
	if err := goose.SetDialect("postgres"); err != nil {
		return err
	}
	return goose.Up(db, "migrations")
}
