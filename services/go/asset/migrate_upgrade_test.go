package main

import (
	"database/sql"
	"os"
	"testing"

	"github.com/pressly/goose/v3"
)

func TestAssetUpgradeFromProductionSchema(t *testing.T) {
	db := openAssetEvidenceDB(t, "MIGRATE_UPGRADE_URL")
	defer db.Close()
	useAssetGoose(t)
	if assetGooseTableExists(t, db, "goose_asset_version") {
		t.Fatal("upgrade database already has goose_asset_version")
	}
	if err := goose.UpTo(db, "migrations", 8); err != nil {
		t.Fatalf("up to production asset 8: %v", err)
	}
	logAssetGoose(t, db, "goose_asset_version")
	if maxAssetGoose(t, db, "goose_asset_version") != 8 {
		t.Fatal("asset baseline is not 8")
	}
	if assetTableExists(t, db, "user_deposit_accounts") {
		t.Fatal("user_deposit_accounts exists before asset 9")
	}
	if _, err := db.Exec(`INSERT INTO balances (user_id, asset, free, locked) VALUES ('synth-deposit-user','USDT', 12.5, 1.25)`); err != nil {
		t.Fatal(err)
	}
	if _, err := db.Exec(`INSERT INTO funding_balances (user_id, asset, free, locked) VALUES ('synth-deposit-user','USDT', 3.5, 0)`); err != nil {
		t.Fatal(err)
	}
	var setting string
	if err := db.QueryRow(`SELECT value FROM asset_settings WHERE key='deposit_account'`).Scan(&setting); err != nil {
		t.Fatal(err)
	}
	if setting != "funding" {
		t.Fatalf("platform deposit_account=%s", setting)
	}
	if err := goose.Up(db, "migrations"); err != nil {
		t.Fatalf("upgrade asset 9: %v", err)
	}
	logAssetGoose(t, db, "goose_asset_version")
	if maxAssetGoose(t, db, "goose_asset_version") != 9 {
		t.Fatal("asset did not reach 9")
	}
	var free, locked, funding string
	if err := db.QueryRow(`SELECT free::text, locked::text FROM balances WHERE user_id='synth-deposit-user' AND asset='USDT'`).Scan(&free, &locked); err != nil {
		t.Fatal(err)
	}
	if err := db.QueryRow(`SELECT free::text FROM funding_balances WHERE user_id='synth-deposit-user' AND asset='USDT'`).Scan(&funding); err != nil {
		t.Fatal(err)
	}
	t.Logf("balances free=%s locked=%s funding_free=%s", free, locked, funding)
	if free != "12.500000000000000000" || locked != "1.250000000000000000" || funding != "3.500000000000000000" {
		t.Fatal("synthetic balances changed during asset 9")
	}
	if err := db.QueryRow(`SELECT value FROM asset_settings WHERE key='deposit_account'`).Scan(&setting); err != nil || setting != "funding" {
		t.Fatalf("deposit setting after upgrade: %s %v", setting, err)
	}
	n := countAssetRows(t, db, "user_deposit_accounts")
	t.Logf("upgrade user_deposit_accounts rows=%d", n)
	if n != 0 {
		t.Fatal("migration inserted deposit preferences")
	}
	if _, err := db.Exec(`INSERT INTO user_deposit_accounts (user_id, account) VALUES ('synth-deposit-user','futures')`); err == nil {
		t.Fatal("futures deposit preference accepted")
	}
	if _, err := db.Exec(`INSERT INTO user_deposit_accounts (user_id, account) VALUES ('synth-deposit-user','spot')`); err != nil {
		t.Fatal(err)
	}
	if _, err := db.Exec(`DELETE FROM user_deposit_accounts WHERE user_id='synth-deposit-user'`); err != nil {
		t.Fatal(err)
	}
}

func TestAssetCleanMigrations(t *testing.T) {
	db := openAssetEvidenceDB(t, "MIGRATE_CLEAN_URL")
	defer db.Close()
	useAssetGoose(t)
	if assetGooseTableExists(t, db, "goose_asset_version") {
		t.Fatal("clean database already has goose_asset_version")
	}
	if err := goose.Up(db, "migrations"); err != nil {
		t.Fatalf("clean asset up: %v", err)
	}
	logAssetGoose(t, db, "goose_asset_version")
	if maxAssetGoose(t, db, "goose_asset_version") != 9 {
		t.Fatal("clean asset is not 9")
	}
	if n := countAssetRows(t, db, "user_deposit_accounts"); n != 0 {
		t.Fatalf("clean user_deposit_accounts rows=%d", n)
	}
	if n := countAssetRows(t, db, "balances"); n != 0 {
		t.Fatalf("clean balances rows=%d", n)
	}
	if err := goose.DownTo(db, "migrations", 8); err != nil {
		t.Fatalf("clean asset down to 8: %v", err)
	}
	if maxAssetGoose(t, db, "goose_asset_version") != 8 || assetTableExists(t, db, "user_deposit_accounts") {
		t.Fatal("down to 8 did not remove user_deposit_accounts")
	}
	if err := goose.Up(db, "migrations"); err != nil {
		t.Fatalf("clean asset re-up: %v", err)
	}
	if maxAssetGoose(t, db, "goose_asset_version") != 9 || !assetTableExists(t, db, "user_deposit_accounts") {
		t.Fatal("re-up did not restore asset 9")
	}
	t.Log("asset clean down-to-8 and re-up ok; upgrade database was not rolled back")
}

func useAssetGoose(t *testing.T) {
	t.Helper()
	goose.SetBaseFS(migrationsFS)
	goose.SetTableName("goose_asset_version")
	if err := goose.SetDialect("postgres"); err != nil {
		t.Fatal(err)
	}
}

func openAssetEvidenceDB(t *testing.T, env string) *sql.DB {
	t.Helper()
	url := os.Getenv(env)
	if url == "" {
		if os.Getenv("REQUIRE_MIGRATION_EVIDENCE") == "1" {
			t.Fatalf("%s not set", env)
		}
		t.Skip(env + " not set")
	}
	db, err := sql.Open("pgx", url)
	if err != nil {
		t.Fatal(err)
	}
	if err := db.Ping(); err != nil {
		t.Fatal(err)
	}
	return db
}

func assetGooseTableExists(t *testing.T, db *sql.DB, name string) bool {
	t.Helper()
	var ok bool
	if err := db.QueryRow(`SELECT EXISTS (SELECT 1 FROM information_schema.tables WHERE table_schema='public' AND table_name=$1)`, name).Scan(&ok); err != nil {
		t.Fatal(err)
	}
	return ok
}

func assetTableExists(t *testing.T, db *sql.DB, name string) bool {
	return assetGooseTableExists(t, db, name)
}

func maxAssetGoose(t *testing.T, db *sql.DB, table string) int64 {
	t.Helper()
	var v sql.NullInt64
	if err := db.QueryRow(`SELECT max(version_id) FROM ` + table + ` WHERE is_applied`).Scan(&v); err != nil {
		t.Fatal(err)
	}
	if !v.Valid {
		return 0
	}
	return v.Int64
}

func logAssetGoose(t *testing.T, db *sql.DB, table string) {
	t.Helper()
	rows, err := db.Query(`SELECT version_id, is_applied FROM ` + table + ` ORDER BY version_id`)
	if err != nil {
		t.Fatal(err)
	}
	defer rows.Close()
	for rows.Next() {
		var id int64
		var applied bool
		if err := rows.Scan(&id, &applied); err != nil {
			t.Fatal(err)
		}
		t.Logf("goose %s version=%d applied=%v", table, id, applied)
	}
	if err := rows.Err(); err != nil {
		t.Fatal(err)
	}
}

func countAssetRows(t *testing.T, db *sql.DB, table string) int {
	t.Helper()
	var n int
	if err := db.QueryRow(`SELECT count(*) FROM ` + table).Scan(&n); err != nil {
		t.Fatal(err)
	}
	return n
}
