package main

import (
	"database/sql"
	"embed"

	"github.com/pressly/goose/v3"
)

//go:embed migrations/*.sql
var migrationFS embed.FS

// Migrate applies every SQL migration in migrations/ to the open database.
// Several services share the same database in dev; a dedicated version table
// keeps our migrations from colliding with theirs on the default name.
func Migrate(db *sql.DB) error {
	goose.SetBaseFS(migrationFS)
	goose.SetTableName("goose_im_version")
	if err := goose.SetDialect("postgres"); err != nil {
		return err
	}
	return goose.Up(db, "migrations")
}
