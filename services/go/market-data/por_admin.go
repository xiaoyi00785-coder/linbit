package main

import (
	"database/sql"
	"encoding/json"
	"log"
	"net/http"
	"strconv"
	"strings"
	"time"

	"github.com/linbit/platform/services/go/common"
	"github.com/shopspring/decimal"
)

// Internal (admin service only) management of manually entered
// proof-of-reserves snapshots. A snapshot = all rows sharing snapshot_at;
// its id is snapshot_at in Unix milliseconds.
func (s *Server) porRoutes(mux *http.ServeMux) {
	mux.HandleFunc("GET /internal/por/snapshots", s.internal(s.iPorList))
	mux.HandleFunc("POST /internal/por/snapshots", s.internal(s.iPorCreate))
	mux.HandleFunc("DELETE /internal/por/snapshots/{id}", s.internal(s.iPorDelete))
}

type porSnapshot struct {
	ID         int64     `json:"id"`
	SnapshotAt string    `json:"snapshotAt"`
	Note       string    `json:"note"`
	CreatedBy  string    `json:"createdBy"`
	CreatedAt  string    `json:"createdAt"`
	Items      []porItem `json:"items"`
}

func porErr(w http.ResponseWriter, err error) {
	log.Printf("por admin: %v", err)
	common.WriteError(w, http.StatusInternalServerError, "INTERNAL", "服务器内部错误")
}

func (s *Server) porLoad(r *http.Request, where string, args ...any) ([]porSnapshot, error) {
	rows, err := s.store.db.QueryContext(r.Context(), `SELECT snapshot_at, asset, liabilities::text, reserves::text, note, created_by, created_at
		FROM por_snapshots `+where+` ORDER BY snapshot_at DESC, asset`, args...)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	out := []porSnapshot{}
	for rows.Next() {
		var at, ca time.Time
		var a, l, rs, note, by string
		if err := rows.Scan(&at, &a, &l, &rs, &note, &by, &ca); err != nil {
			return nil, err
		}
		ld, _ := decimal.NewFromString(l)
		rd, _ := decimal.NewFromString(rs)
		it := porItem{Asset: a, Liabilities: ld.String(), Reserves: rd.String()}
		if ld.IsPositive() {
			it.Ratio = rd.Div(ld).Round(4).String()
		}
		if n := len(out); n == 0 || out[n-1].ID != at.UnixMilli() {
			out = append(out, porSnapshot{ID: at.UnixMilli(), SnapshotAt: at.UTC().Format(time.RFC3339), Note: note, CreatedBy: by, CreatedAt: ca.UTC().Format(time.RFC3339)})
		}
		out[len(out)-1].Items = append(out[len(out)-1].Items, it)
	}
	return out, rows.Err()
}

// GET /internal/por/snapshots?limit= (newest first, default 20, max 100 snapshots)
func (s *Server) iPorList(w http.ResponseWriter, r *http.Request) {
	lim, _ := strconv.Atoi(r.URL.Query().Get("limit"))
	if lim <= 0 || lim > 100 {
		lim = 20
	}
	out, err := s.porLoad(r, `WHERE snapshot_at IN (SELECT DISTINCT snapshot_at FROM por_snapshots ORDER BY snapshot_at DESC LIMIT $1)`, lim)
	if err != nil {
		porErr(w, err)
		return
	}
	common.WriteJSON(w, http.StatusOK, map[string]any{"items": out})
}

// POST /internal/por/snapshots {snapshotAt?, note?, updatedBy, items:[{asset,liabilities,reserves}]}
func (s *Server) iPorCreate(w http.ResponseWriter, r *http.Request) {
	var req struct {
		SnapshotAt string `json:"snapshotAt"`
		Note       string `json:"note"`
		UpdatedBy  string `json:"updatedBy"`
		Items      []struct {
			Asset       string `json:"asset"`
			Liabilities string `json:"liabilities"`
			Reserves    string `json:"reserves"`
		} `json:"items"`
	}
	if err := common.DecodeJSON(r, &req); err != nil || len(req.Items) == 0 || len(req.Items) > 200 {
		common.WriteError(w, http.StatusBadRequest, "VALIDATION_ERROR", "items 必填（1-200 项）")
		return
	}
	at := time.Now().UTC().Truncate(time.Second)
	if req.SnapshotAt != "" {
		t, err := time.Parse(time.RFC3339, req.SnapshotAt)
		if err != nil || t.After(time.Now().Add(time.Minute)) {
			common.WriteError(w, http.StatusBadRequest, "VALIDATION_ERROR", "snapshotAt 须为不晚于当前的 RFC3339 时间")
			return
		}
		at = t.UTC().Truncate(time.Second)
	}
	seen := map[string]bool{}
	tx, err := s.store.db.BeginTx(r.Context(), nil)
	if err != nil {
		porErr(w, err)
		return
	}
	defer tx.Rollback()
	var exists int
	if err := tx.QueryRowContext(r.Context(), `SELECT COUNT(*) FROM por_snapshots WHERE snapshot_at=$1`, at).Scan(&exists); err != nil {
		porErr(w, err)
		return
	}
	if exists > 0 {
		common.WriteError(w, http.StatusConflict, "SNAPSHOT_EXISTS", "该时间点已有快照")
		return
	}
	for _, it := range req.Items {
		a := strings.ToUpper(strings.TrimSpace(it.Asset))
		l, e1 := decimal.NewFromString(strings.TrimSpace(it.Liabilities))
		rs, e2 := decimal.NewFromString(strings.TrimSpace(it.Reserves))
		if a == "" || len(a) > 20 || seen[a] || e1 != nil || e2 != nil || l.IsNegative() || rs.IsNegative() {
			common.WriteError(w, http.StatusBadRequest, "VALIDATION_ERROR", "资产不能为空或重复，负债/储备须为非负数字："+a)
			return
		}
		seen[a] = true
		if _, err := tx.ExecContext(r.Context(), `INSERT INTO por_snapshots (snapshot_at, asset, liabilities, reserves, note, created_by) VALUES ($1,$2,$3,$4,$5,$6)`,
			at, a, l, rs, strings.TrimSpace(req.Note), req.UpdatedBy); err != nil {
			porErr(w, err)
			return
		}
	}
	if err := tx.Commit(); err != nil {
		porErr(w, err)
		return
	}
	out, err := s.porLoad(r, `WHERE snapshot_at=$1`, at)
	if err != nil || len(out) == 0 {
		porErr(w, err)
		return
	}
	after, _ := json.Marshal(out[0])
	common.WriteJSON(w, http.StatusCreated, map[string]any{"snapshot": out[0], "after": json.RawMessage(after)})
}

// DELETE /internal/por/snapshots/{id}
func (s *Server) iPorDelete(w http.ResponseWriter, r *http.Request) {
	id, err := strconv.ParseInt(r.PathValue("id"), 10, 64)
	if err != nil || id <= 0 {
		common.WriteError(w, http.StatusBadRequest, "VALIDATION_ERROR", "id 不合法")
		return
	}
	at := time.UnixMilli(id).UTC()
	before, err := s.porLoad(r, `WHERE snapshot_at=$1`, at)
	if err != nil {
		porErr(w, err)
		return
	}
	if len(before) == 0 {
		common.WriteError(w, http.StatusNotFound, "NOT_FOUND", "快照不存在")
		return
	}
	if _, err := s.store.db.ExecContext(r.Context(), `DELETE FROM por_snapshots WHERE snapshot_at=$1`, at); err != nil && err != sql.ErrNoRows {
		porErr(w, err)
		return
	}
	b, _ := json.Marshal(before[0])
	common.WriteJSON(w, http.StatusOK, map[string]any{"deleted": true, "before": json.RawMessage(b)})
}
