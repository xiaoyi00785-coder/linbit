package main

import (
	"database/sql"
	"os"
	"testing"

	"github.com/pressly/goose/v3"
)

func TestAccountUpgradeFromProductionSchema(t *testing.T) {
	db := openEvidenceDB(t, "MIGRATE_UPGRADE_URL")
	defer db.Close()
	useAccountGoose(t)
	if gooseTableExists(t, db, "goose_account_version") {
		t.Fatal("upgrade database already has goose_account_version; refusing a non-empty baseline")
	}
	if err := goose.UpTo(db, "migrations", 13); err != nil {
		t.Fatalf("up to production account 13: %v", err)
	}
	logGooseVersions(t, db, "goose_account_version")
	if maxGooseVersion(t, db, "goose_account_version") != 13 {
		t.Fatal("account baseline is not 13")
	}
	if columnExists(t, db, "notifications", "resource_type") || tableExists(t, db, "user_api_keys") {
		t.Fatal("candidate columns exist before account 14-17")
	}
	var userID int64
	if err := db.QueryRow(`INSERT INTO users (email, password_hash) VALUES ('synth-migrate@example.test', 'synthetic-not-a-secret') RETURNING id`).Scan(&userID); err != nil {
		t.Fatal(err)
	}
	var noteID int64
	if err := db.QueryRow(`INSERT INTO notifications (user_id, kind, title, body) VALUES ($1,'system','历史系统通知','正文提到工单 999，这不是定位') RETURNING id`, userID).Scan(&noteID); err != nil {
		t.Fatal(err)
	}
	if err := goose.Up(db, "migrations"); err != nil {
		t.Fatalf("upgrade account 14-17: %v", err)
	}
	logGooseVersions(t, db, "goose_account_version")
	if maxGooseVersion(t, db, "goose_account_version") != 17 {
		t.Fatal("account did not reach 17")
	}
	var typ, rid sql.NullString
	if err := db.QueryRow(`SELECT resource_type, resource_id FROM notifications WHERE id=$1`, noteID).Scan(&typ, &rid); err != nil {
		t.Fatal(err)
	}
	if typ.Valid || rid.Valid {
		t.Fatalf("historical notification gained a locator: %#v %#v", typ, rid)
	}
	if _, err := db.Exec(`UPDATE notifications SET resource_type='https://evil.example', resource_id='1' WHERE id=$1`, noteID); err == nil {
		t.Fatal("external resource type was accepted")
	}
	if err := db.QueryRow(`SELECT resource_type, resource_id FROM notifications WHERE id=$1`, noteID).Scan(&typ, &rid); err != nil {
		t.Fatal(err)
	}
	if typ.Valid || rid.Valid {
		t.Fatal("rejected update changed the historical row")
	}
	if _, err := db.Exec(`INSERT INTO notifications (user_id, kind, title, body, resource_type, resource_id) VALUES ($1,'system','可定位','b','support_ticket','42')`, userID); err != nil {
		t.Fatalf("support_ticket locator rejected: %v", err)
	}
	for _, table := range []string{"oauth_identities", "card_products", "card_applications", "user_api_keys"} {
		n := countRows(t, db, table)
		t.Logf("upgrade %s rows=%d", table, n)
		if n != 0 {
			t.Fatalf("%s has rows after upgrade", table)
		}
	}
	t.Logf("account upgrade synthetic user_id=%d historical_notification_id=%d locator=null", userID, noteID)
}

func TestAccountCleanMigrations(t *testing.T) {
	db := openEvidenceDB(t, "MIGRATE_CLEAN_URL")
	defer db.Close()
	useAccountGoose(t)
	if gooseTableExists(t, db, "goose_account_version") {
		t.Fatal("clean database already has goose_account_version")
	}
	if err := goose.Up(db, "migrations"); err != nil {
		t.Fatalf("clean account up: %v", err)
	}
	logGooseVersions(t, db, "goose_account_version")
	if maxGooseVersion(t, db, "goose_account_version") != 17 {
		t.Fatal("clean account is not 17")
	}
	for _, table := range []string{"oauth_identities", "card_products", "card_applications", "user_api_keys", "notifications"} {
		n := countRows(t, db, table)
		t.Logf("clean %s rows=%d", table, n)
		if n != 0 {
			t.Fatalf("clean %s is not empty", table)
		}
	}
	if !columnExists(t, db, "notifications", "resource_type") {
		t.Fatal("clean database missing resource_type")
	}
	if err := goose.DownTo(db, "migrations", 13); err != nil {
		t.Fatalf("clean account down to 13: %v", err)
	}
	logGooseVersions(t, db, "goose_account_version")
	if maxGooseVersion(t, db, "goose_account_version") != 13 || columnExists(t, db, "notifications", "resource_type") || tableExists(t, db, "user_api_keys") {
		t.Fatal("down to 13 did not remove account 14-17")
	}
	if err := goose.Up(db, "migrations"); err != nil {
		t.Fatalf("clean account re-up: %v", err)
	}
	if maxGooseVersion(t, db, "goose_account_version") != 17 || !columnExists(t, db, "notifications", "resource_type") {
		t.Fatal("re-up did not restore account 17")
	}
	t.Log("account clean down-to-13 and re-up ok; upgrade database was not rolled back")
}

func useAccountGoose(t *testing.T) {
	t.Helper()
	goose.SetBaseFS(migrationsFS)
	goose.SetTableName("goose_account_version")
	if err := goose.SetDialect("postgres"); err != nil {
		t.Fatal(err)
	}
}

func openEvidenceDB(t *testing.T, env string) *sql.DB {
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

func gooseTableExists(t *testing.T, db *sql.DB, name string) bool {
	t.Helper()
	var ok bool
	if err := db.QueryRow(`SELECT EXISTS (SELECT 1 FROM information_schema.tables WHERE table_schema='public' AND table_name=$1)`, name).Scan(&ok); err != nil {
		t.Fatal(err)
	}
	return ok
}

func tableExists(t *testing.T, db *sql.DB, name string) bool {
	return gooseTableExists(t, db, name)
}

func columnExists(t *testing.T, db *sql.DB, table, column string) bool {
	t.Helper()
	var ok bool
	if err := db.QueryRow(`SELECT EXISTS (SELECT 1 FROM information_schema.columns WHERE table_schema='public' AND table_name=$1 AND column_name=$2)`, table, column).Scan(&ok); err != nil {
		t.Fatal(err)
	}
	return ok
}

func maxGooseVersion(t *testing.T, db *sql.DB, table string) int64 {
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

func logGooseVersions(t *testing.T, db *sql.DB, table string) {
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

func countRows(t *testing.T, db *sql.DB, table string) int {
	t.Helper()
	var n int
	if err := db.QueryRow(`SELECT count(*) FROM ` + table).Scan(&n); err != nil {
		t.Fatal(err)
	}
	return n
}
