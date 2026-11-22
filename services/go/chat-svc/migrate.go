package main

import (
	"database/sql"
	"embed"

	"github.com/pressly/goose/v3"
)

//go:embed migrations/*.sql
var migrationsFS embed.FS

// Migrate applies chat-svc's goose migrations on startup under its own version
// table so it can share a database with the other services.
func Migrate(db *sql.DB) error {
	goose.SetBaseFS(migrationsFS)
	goose.SetTableName("goose_chat_version")
	if err := goose.SetDialect("postgres"); err != nil {
		return err
	}
	return goose.Up(db, "migrations")
}
