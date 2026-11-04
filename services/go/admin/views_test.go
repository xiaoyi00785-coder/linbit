package main

import (
	"context"
	"database/sql"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"os"
	"strings"
	"testing"
)

func TestFilterPlaceholders(t *testing.T) {
	f := &filter{}
	f.add("a = ?", 1)
	f.where = append(f.where, "b IN ('x')")
	f.add("c BETWEEN ? AND ?", 2)
	got := f.sql()
	if got != " WHERE a = $1 AND b IN ('x') AND c BETWEEN $2 AND $2" || len(f.args) != 2 {
		t.Fatalf("got %q %v", got, f.args)
	}
	if (&filter{}).sql() != "" {
		t.Fatal("empty filter must produce no WHERE")
	}
}

// TestViewsAgainstSchema runs every read-only view against a database with
// all service migrations applied (set ADMIN_VIEWS_TEST_DSN) so a column or
// table rename breaks the build instead of the console.
func TestViewsAgainstSchema(t *testing.T) {
	dsn := os.Getenv("ADMIN_VIEWS_TEST_DSN")
	if dsn == "" {
		t.Skip("ADMIN_VIEWS_TEST_DSN not set")
	}
	db, err := sql.Open("pgx", dsn)
	if err != nil {
		t.Fatal(err)
	}
	defer db.Close()
	s := NewServer(Config{}, db)
	cases := map[string]http.HandlerFunc{
		"/?uid=u1":               s.handleRiskUsers,
		"/?status=completed":     s.handleDeposits,
		"/?status=open&side=buy": s.handleSpotOrders,
		"/?side=long":            s.handleFutPositions,
		"/?symbol=BTCUSDT":       s.handleLiquidations,
		"/":                      s.handleFutContracts,
		"/?status=running":       s.handleGrids,
		"/?status=active":        s.handleCopyTraders,
		"/?kind=spot":            s.handleCommissions,
		"/?campaignId=1":         s.handleRedemptions,
	}
	for u, h := range cases {
		rec := httptest.NewRecorder()
		h(rec, httptest.NewRequest(http.MethodGet, u, nil))
		if rec.Code != 200 || !strings.Contains(rec.Body.String(), `"items"`) {
			t.Errorf("%s: %d %s", u, rec.Code, rec.Body.String())
		}
	}
	m, err := s.queryMaps(context.Background(), `SELECT 1.50::numeric AS "camelCase"`)
	if err != nil || m[0]["camelCase"] != "1.50" {
		t.Fatalf("rowsToMaps: %v %v", m, err)
	}
	for _, h := range []http.HandlerFunc{s.handleUserDevices, s.handleUserProfile30d} {
		rec := httptest.NewRecorder()
		req := httptest.NewRequest(http.MethodGet, "/", nil)
		req.SetPathValue("uid", "80000001")
		h(rec, req)
		if rec.Code != 200 {
			t.Errorf("user view: %d %s", rec.Code, rec.Body.String())
		}
	}
	rec := httptest.NewRecorder()
	s.handleDashboard(rec, httptest.NewRequest(http.MethodGet, "/", nil))
	var out struct {
		Errors []string          `json:"errors"`
		Daily  []json.RawMessage `json:"daily"`
	}
	if err := json.Unmarshal(rec.Body.Bytes(), &out); err != nil || len(out.Errors) > 0 || len(out.Daily) != 30 {
		t.Fatalf("dashboard: %v %s", err, rec.Body.String())
	}
}
