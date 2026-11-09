package main

import (
	"database/sql"
	"os"
	"testing"

	"github.com/pressly/goose/v3"
)

func TestMarketUpgradeFromProductionSchema(t *testing.T) {
	db := openMarketEvidenceDB(t, "MIGRATE_UPGRADE_URL")
	defer db.Close()
	useMarketGoose(t)
	if marketGooseTableExists(t, db, "goose_market_version") {
		t.Fatal("upgrade database already has goose_market_version")
	}
	if err := goose.UpTo(db, "migrations", 9); err != nil {
		t.Fatalf("up to production market 9: %v", err)
	}
	logMarketGoose(t, db, "goose_market_version")
	if maxMarketGoose(t, db, "goose_market_version") != 9 {
		t.Fatal("market baseline is not 9")
	}
	if marketColumnExists(t, db, "por_snapshots", "status") || marketTableExists(t, db, "academy_articles") {
		t.Fatal("candidate market objects exist before 10-11")
	}
	if _, err := db.Exec(`INSERT INTO por_snapshots (snapshot_at, asset, liabilities, reserves, note, created_by) VALUES ('2020-01-01T00:00:00Z','USDT', 10, 12, 'synthetic', 'synth')`); err != nil {
		t.Fatal(err)
	}
	if err := goose.Up(db, "migrations"); err != nil {
		t.Fatalf("upgrade market 10-11: %v", err)
	}
	logMarketGoose(t, db, "goose_market_version")
	if maxMarketGoose(t, db, "goose_market_version") != 11 {
		t.Fatal("market did not reach 11")
	}
	var status string
	if err := db.QueryRow(`SELECT status FROM por_snapshots WHERE asset='USDT' AND snapshot_at='2020-01-01T00:00:00Z'`).Scan(&status); err != nil {
		t.Fatal(err)
	}
	t.Logf("historical por status=%s", status)
	if status != "published" {
		t.Fatalf("historical snapshot status=%s", status)
	}
	if _, err := db.Exec(`INSERT INTO por_snapshots (snapshot_at, asset, liabilities, reserves) VALUES ('2020-01-02T00:00:00Z','BTC', 1, 1)`); err != nil {
		t.Fatal(err)
	}
	if err := db.QueryRow(`SELECT status FROM por_snapshots WHERE asset='BTC'`).Scan(&status); err != nil {
		t.Fatal(err)
	}
	t.Logf("new por default status=%s", status)
	if status != "draft" {
		t.Fatalf("new snapshot default=%s", status)
	}
	for _, table := range []string{"academy_categories", "academy_articles"} {
		n := countMarketRows(t, db, table)
		t.Logf("upgrade %s rows=%d", table, n)
		if n != 0 {
			t.Fatalf("%s was seeded", table)
		}
	}
	logMarketGoose(t, db, "goose_account_version")
	logMarketGoose(t, db, "goose_asset_version")
}

func TestMarketCleanMigrations(t *testing.T) {
	db := openMarketEvidenceDB(t, "MIGRATE_CLEAN_URL")
	defer db.Close()
	useMarketGoose(t)
	if marketGooseTableExists(t, db, "goose_market_version") {
		t.Fatal("clean database already has goose_market_version")
	}
	if err := goose.Up(db, "migrations"); err != nil {
		t.Fatalf("clean market up: %v", err)
	}
	logMarketGoose(t, db, "goose_market_version")
	if maxMarketGoose(t, db, "goose_market_version") != 11 {
		t.Fatal("clean market is not 11")
	}
	for _, table := range []string{"por_snapshots", "academy_categories", "academy_articles"} {
		if n := countMarketRows(t, db, table); n != 0 {
			t.Fatalf("clean %s rows=%d", table, n)
		}
	}
	if err := goose.DownTo(db, "migrations", 9); err != nil {
		t.Fatalf("clean market down to 9: %v", err)
	}
	if maxMarketGoose(t, db, "goose_market_version") != 9 || marketColumnExists(t, db, "por_snapshots", "status") || marketTableExists(t, db, "academy_articles") {
		t.Fatal("down to 9 did not remove market 10-11")
	}
	if err := goose.Up(db, "migrations"); err != nil {
		t.Fatalf("clean market re-up: %v", err)
	}
	if maxMarketGoose(t, db, "goose_market_version") != 11 || !marketTableExists(t, db, "academy_articles") {
		t.Fatal("re-up did not restore market 11")
	}
	t.Log("market clean down-to-9 and re-up ok; upgrade database was not rolled back")
	logMarketGoose(t, db, "goose_account_version")
	logMarketGoose(t, db, "goose_asset_version")
}

func useMarketGoose(t *testing.T) {
	t.Helper()
	goose.SetBaseFS(migrationsFS)
	goose.SetTableName("goose_market_version")
	if err := goose.SetDialect("postgres"); err != nil {
		t.Fatal(err)
	}
}

func openMarketEvidenceDB(t *testing.T, env string) *sql.DB {
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

func marketGooseTableExists(t *testing.T, db *sql.DB, name string) bool {
	t.Helper()
	var ok bool
	if err := db.QueryRow(`SELECT EXISTS (SELECT 1 FROM information_schema.tables WHERE table_schema='public' AND table_name=$1)`, name).Scan(&ok); err != nil {
		t.Fatal(err)
	}
	return ok
}

func marketTableExists(t *testing.T, db *sql.DB, name string) bool {
	return marketGooseTableExists(t, db, name)
}

func marketColumnExists(t *testing.T, db *sql.DB, table, column string) bool {
	t.Helper()
	var ok bool
	if err := db.QueryRow(`SELECT EXISTS (SELECT 1 FROM information_schema.columns WHERE table_schema='public' AND table_name=$1 AND column_name=$2)`, table, column).Scan(&ok); err != nil {
		t.Fatal(err)
	}
	return ok
}

func maxMarketGoose(t *testing.T, db *sql.DB, table string) int64 {
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

func logMarketGoose(t *testing.T, db *sql.DB, table string) {
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

func countMarketRows(t *testing.T, db *sql.DB, table string) int {
	t.Helper()
	var n int
	if err := db.QueryRow(`SELECT count(*) FROM ` + table).Scan(&n); err != nil {
		t.Fatal(err)
	}
	return n
}
