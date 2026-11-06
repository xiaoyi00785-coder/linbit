package main

import (
	"context"
	"database/sql"
	"embed"
	"io/fs"

	"github.com/pressly/goose/v3"
	"github.com/pressly/goose/v3/database"
)

//go:embed migrations/*.sql
var migrationsFS embed.FS

func Migrate(db *sql.DB) error {
	store, err := database.NewStore(database.DialectPostgres, "goose_paper_version")
	if err != nil {
		return err
	}
	files, err := fs.Sub(migrationsFS, "migrations")
	if err != nil {
		return err
	}
	provider, err := goose.NewProvider(goose.DialectCustom, db, files, goose.WithStore(store), goose.WithDisableGlobalRegistry(true))
	if err != nil {
		return err
	}
	// Provider owns the supplied *sql.DB lifecycle. All service operations run
	// through Store, so do not close the database here; main closes it on exit.
	_, err = provider.Up(context.Background())
	return err
}
