package main

import (
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"testing"
	"time"
)

func TestCopyTraderClosedPositionsQueryAliases(t *testing.T) {
	s, ctx := copyServer(t, map[string]int{"trader": 1})
	mux := http.NewServeMux()
	s.copyRoutes(mux)

	nick := "Lead"
	if _, err := s.ApplyTrader(ctx, "trader", traderReq{Nickname: &nick}); err != nil {
		t.Fatal(err)
	}
	if err := s.SetTraderStatus(ctx, "trader", "active"); err != nil {
		t.Fatal(err)
	}

	opened := time.Now().UTC().Add(-time.Hour)
	closed := time.Now().UTC()
	_, err := s.db.ExecContext(ctx, `INSERT INTO fut_position_history
		(user_id, symbol, position_side, direction, margin_mode, leverage, entry_price, close_price, max_qty, closed_qty, realized_pnl, fees, funding, net_pnl, status, opened_at, closed_at)
		VALUES ($1,$2,'BOTH','long','cross',20,100,110,1,1,10,0,0,10,'closed',$3,$4)`,
		"trader", "BTCUSDT", opened, closed)
	if err != nil {
		t.Fatal(err)
	}

	get := func(path string) (int, []any) {
		t.Helper()
		rr := httptest.NewRecorder()
		mux.ServeHTTP(rr, httptest.NewRequest(http.MethodGet, path, nil))
		var body map[string]any
		_ = json.Unmarshal(rr.Body.Bytes(), &body)
		pos, _ := body["positions"].([]any)
		return rr.Code, pos
	}

	// The bug: App/compat send position_status=close, which used to miss
	// the status=closed branch and return an empty open list.
	for _, path := range []string{
		"/futures/copy/traders/trader/positions?position_status=close",
		"/futures/copy/traders/trader/positions?position_status=closed",
		"/futures/copy/traders/trader/positions?status=closed",
		"/futures/copy/traders/trader/positions?status=close",
		"/futures/copy/traders/trader/positions?positionStatus=close&page_size=20",
	} {
		st, pos := get(path)
		if st != http.StatusOK || len(pos) != 1 {
			t.Fatalf("%s: http %d n=%d", path, st, len(pos))
		}
		row, _ := pos[0].(map[string]any)
		if row["symbol"] != "BTCUSDT" || row["entryPrice"] == nil || row["closePrice"] == nil || row["openTime"] == nil {
			t.Fatalf("%s row %+v", path, row)
		}
	}

	st, pos := get("/futures/copy/traders/trader/positions")
	if st != http.StatusOK || len(pos) != 0 {
		t.Fatalf("default (open) should be empty, http %d n=%d", st, len(pos))
	}
	st, pos = get("/futures/copy/traders/trader/positions?position_status=open")
	if st != http.StatusOK || len(pos) != 0 {
		t.Fatalf("open alias http %d n=%d", st, len(pos))
	}
}
