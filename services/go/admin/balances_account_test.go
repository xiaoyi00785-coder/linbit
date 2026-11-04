package main

import (
	"database/sql"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"testing"
)

func TestAdminBalanceAccounts(t *testing.T) {
	for in, n := range map[string]int{"": 2, "all": 2, "spot": 1, " Funding ": 1} {
		if a, ok := adminBalanceAccounts(in); !ok || len(a) != n {
			t.Fatalf("%q -> %v %v", in, a, ok)
		}
	}
	if _, ok := adminBalanceAccounts("futures"); ok {
		t.Fatal("futures must be rejected")
	}
}

func TestAdminUserBalancesBothAccounts(t *testing.T) {
	var paths []string
	up := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		paths = append(paths, r.URL.RequestURI())
		free := "1.00000000"
		if r.URL.Query().Get("account") == "funding" {
			free = "10000.00000000"
		}
		_ = json.NewEncoder(w).Encode(map[string]any{"items": []map[string]string{{"asset": "USDT", "free": free, "locked": "0", "total": free}}})
	}))
	defer up.Close()
	db, _ := sql.Open("pgx", "postgres://nobody@127.0.0.1:1/x?sslmode=disable&connect_timeout=1")
	defer db.Close()
	s := NewServer(Config{AssetURL: up.URL}, db)
	get := func(q string) (int, map[string]json.RawMessage) {
		r := httptest.NewRequest("GET", "/admin/v1/users/80000004/balances"+q, nil)
		r.SetPathValue("uid", "80000004")
		w := httptest.NewRecorder()
		s.handleUserBalances(w, r)
		var m map[string]json.RawMessage
		_ = json.Unmarshal(w.Body.Bytes(), &m)
		return w.Code, m
	}
	code, m := get("")
	if code != 200 || m["spot"] == nil || m["funding"] == nil || string(m["total"]) != "2" {
		t.Fatalf("both: %d %v", code, m)
	}
	var items []struct{ Account, Free string }
	_ = json.Unmarshal(m["items"], &items)
	if len(items) != 2 || items[1].Account != "funding" || items[1].Free != "10000.00000000" {
		t.Fatalf("items %+v", items)
	}
	paths = nil
	if code, m = get("?account=funding"); code != 200 || m["spot"] != nil || len(paths) != 1 || paths[0] != "/internal/balances?userId=80000004&account=funding" {
		t.Fatalf("funding: %d %v %v", code, m, paths)
	}
	if code, _ = get("?account=margin"); code != 400 {
		t.Fatalf("margin: %d", code)
	}
}
