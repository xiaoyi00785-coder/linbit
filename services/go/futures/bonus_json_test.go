package main

import (
	"database/sql"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"os"
	"testing"

	_ "github.com/jackc/pgx/v5/stdlib"
	"github.com/shopspring/decimal"
)

func TestBonusAvailableJSONObject(t *testing.T) {
	url := os.Getenv("FUTURES_BONUS_TEST_URL")
	if url == "" {
		if os.Getenv("REQUIRE_MIGRATION_EVIDENCE") == "1" {
			t.Fatal("FUTURES_BONUS_TEST_URL not set")
		}
		t.Skip("FUTURES_BONUS_TEST_URL not set")
	}
	db, err := sql.Open("pgx", url)
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { db.Close() })
	if err := db.Ping(); err != nil {
		t.Fatal(err)
	}
	if err := Migrate(db); err != nil {
		t.Fatal(err)
	}
	s := &Server{db: db}
	mux := http.NewServeMux()
	s.bonusRoutes(mux)

	empty := bonusBody(t, mux, "synth-bonus-empty")
	assertBonusAvailable(t, empty, decimal.Zero, decimal.Zero)

	if _, err := db.Exec(`INSERT INTO fut_bonus (user_id, ref, kind, source, amount, remaining, status, expires_at) VALUES
		('synth-bonus-user','synth-trial','trial','synth', 1.5, 1.5, 'active', now()+interval '1 day'),
		('synth-bonus-user','synth-deduction','deduction','synth', 2.25, 2.25, 'active', now()+interval '1 day')`); err != nil {
		t.Fatal(err)
	}
	body := bonusBody(t, mux, "synth-bonus-user")
	assertBonusAvailable(t, body, decimal.RequireFromString("1.5"), decimal.RequireFromString("2.25"))
	t.Logf("bonus available json=%s", rawAvailable(t, body))
}

func bonusBody(t *testing.T, h http.Handler, uid string) []byte {
	t.Helper()
	req := httptest.NewRequest(http.MethodGet, "/futures/bonus", nil)
	req.Header.Set("X-User-Id", uid)
	w := httptest.NewRecorder()
	h.ServeHTTP(w, req)
	if w.Code != http.StatusOK {
		t.Fatalf("bonus %s: %d %s", uid, w.Code, w.Body.String())
	}
	return w.Body.Bytes()
}

func assertBonusAvailable(t *testing.T, body []byte, trial, deduction decimal.Decimal) {
	t.Helper()
	raw := rawAvailable(t, body)
	if len(raw) == 0 || raw[0] != '{' {
		t.Fatalf("available JSON type = %s, want object", raw)
	}
	var fields map[string]json.RawMessage
	if err := json.Unmarshal(raw, &fields); err != nil {
		t.Fatal(err)
	}
	if len(fields) != 2 {
		t.Fatalf("available keys = %s", raw)
	}
	gotTrial := bonusDecimal(t, fields["trial"])
	gotDeduction := bonusDecimal(t, fields["deduction"])
	if !gotTrial.Equal(trial) || !gotDeduction.Equal(deduction) {
		t.Fatalf("available trial=%s deduction=%s", gotTrial, gotDeduction)
	}
}

func rawAvailable(t *testing.T, body []byte) json.RawMessage {
	t.Helper()
	var root map[string]json.RawMessage
	if err := json.Unmarshal(body, &root); err != nil {
		t.Fatal(err)
	}
	return root["available"]
}

func bonusDecimal(t *testing.T, raw json.RawMessage) decimal.Decimal {
	t.Helper()
	if len(raw) == 0 || raw[0] != '"' {
		t.Fatalf("bonus amount JSON = %s, want string", raw)
	}
	var s string
	if err := json.Unmarshal(raw, &s); err != nil {
		t.Fatal(err)
	}
	d, err := decimal.NewFromString(s)
	if err != nil {
		t.Fatal(err)
	}
	return d
}
