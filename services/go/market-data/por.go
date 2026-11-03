package main

import (
	"log"
	"net/http"
	"time"

	"github.com/linbit/platform/services/go/common"
	"github.com/shopspring/decimal"
)

type porItem struct {
	Asset       string `json:"asset"`
	Liabilities string `json:"liabilities"` // total user assets
	Reserves    string `json:"reserves"`    // platform reserves
	Ratio       string `json:"ratio"`       // reserves / liabilities, e.g. "1.0234"; "" if liabilities = 0
}

// GET /market/proof-of-reserves — latest published reserve snapshot.
// {available:false, snapshotAt:null, items:[]} until operations publish one.
func (s *Server) handleProofOfReserves(w http.ResponseWriter, r *http.Request) {
	out := map[string]any{"available": false, "snapshotAt": nil, "items": []porItem{}}
	if s.store == nil || s.store.db == nil {
		common.WriteJSON(w, http.StatusOK, out)
		return
	}
	rows, err := s.store.db.QueryContext(r.Context(), `SELECT snapshot_at, asset, liabilities::text, reserves::text
		FROM por_snapshots WHERE snapshot_at = (SELECT MAX(snapshot_at) FROM por_snapshots) ORDER BY asset`)
	if err != nil {
		log.Printf("proof-of-reserves: %v", err)
		common.WriteError(w, http.StatusInternalServerError, "INTERNAL", "服务器内部错误")
		return
	}
	defer rows.Close()
	items := []porItem{}
	var at time.Time
	for rows.Next() {
		var a, l, rs string
		if err := rows.Scan(&at, &a, &l, &rs); err != nil {
			log.Printf("proof-of-reserves scan: %v", err)
			continue
		}
		ld, _ := decimal.NewFromString(l)
		rd, _ := decimal.NewFromString(rs)
		it := porItem{Asset: a, Liabilities: ld.String(), Reserves: rd.String()}
		if ld.IsPositive() {
			it.Ratio = rd.Div(ld).Round(4).String()
		}
		items = append(items, it)
	}
	if len(items) > 0 {
		out["available"], out["snapshotAt"], out["items"] = true, at.UTC().Format(time.RFC3339), items
	}
	common.WriteJSON(w, http.StatusOK, out)
}
