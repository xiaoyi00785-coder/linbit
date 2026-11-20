package main

import (
	"context"
	"database/sql"
	"encoding/json"
	"errors"
	"fmt"
	"net/http"
	"strings"
	"time"

	"github.com/linbit/platform/services/go/common"
	"github.com/shopspring/decimal"
)

type project struct {
	ID            string           `json:"id"`
	Symbol        string           `json:"symbol"`
	Name          string           `json:"name"`
	Logo          string           `json:"logo"`
	Description   string           `json:"description"`
	BaseAsset     string           `json:"baseAsset"`
	Price         decimal.Decimal  `json:"price"`
	TotalSupply   decimal.Decimal  `json:"totalSupply"`
	MinSub        decimal.Decimal  `json:"minSub"`
	MaxSub        *decimal.Decimal `json:"maxSub,omitempty"`
	StartAt       time.Time        `json:"-"`
	EndAt         time.Time        `json:"-"`
	SettleAt      time.Time        `json:"-"`
	DistributeAt  time.Time        `json:"-"`
	Status        string           `json:"status"`
	Allocated     decimal.Decimal  `json:"allocated"`
	Raised        decimal.Decimal  `json:"raised"`
	RulesMD       string           `json:"-"`
	Sort          int              `json:"-"`
	SettledAt     sql.NullTime     `json:"-"`
	DistributedAt sql.NullTime     `json:"-"`
}

const projectCols = `id, symbol, name, logo, description, base_asset, price, total_supply, min_sub, max_sub,
	start_at, end_at, settle_at, distribute_at, status, allocated, raised, rules_md, sort, settled_at, distributed_at`

type rowScanner interface{ Scan(...any) error }

func scanProject(r rowScanner) (*project, error) {
	p := &project{}
	var maxSub decimal.NullDecimal
	if err := r.Scan(&p.ID, &p.Symbol, &p.Name, &p.Logo, &p.Description, &p.BaseAsset, &p.Price, &p.TotalSupply,
		&p.MinSub, &maxSub, &p.StartAt, &p.EndAt, &p.SettleAt, &p.DistributeAt, &p.Status, &p.Allocated,
		&p.Raised, &p.RulesMD, &p.Sort, &p.SettledAt, &p.DistributedAt); err != nil {
		return nil, err
	}
	if maxSub.Valid {
		v := maxSub.Decimal
		p.MaxSub = &v
	}
	return p, nil
}

// publicView renders a project for the catalogue / detail response. It derives
// `effectiveStatus` from the clock so a row that has not yet been Tick'd still
// reads as `open` when its window started, and friendly time fields.
func (s *Server) publicView(p *project) map[string]any {
	now := s.now()
	status := p.Status
	switch {
	case status == "upcoming" && !now.Before(p.StartAt):
		status = "open"
	case (status == "upcoming" || status == "open") && !now.Before(p.EndAt):
		status = "computing"
	}
	out := map[string]any{
		"id":             p.ID,
		"symbol":         p.Symbol,
		"name":           p.Name,
		"logo":           p.Logo,
		"description":    p.Description,
		"baseAsset":      p.BaseAsset,
		"price":          p.Price,
		"totalSupply":    p.TotalSupply,
		"minSub":         p.MinSub,
		"startAt":        p.StartAt.UnixMilli(),
		"endAt":          p.EndAt.UnixMilli(),
		"settleAt":       p.SettleAt.UnixMilli(),
		"distributeAt":   p.DistributeAt.UnixMilli(),
		"status":         status,
		"allocated":      p.Allocated,
		"raised":         p.Raised,
		"totalRaiseCap":  p.TotalSupply.Mul(p.Price),
	}
	if p.MaxSub != nil {
		out["maxSub"] = *p.MaxSub
	}
	if p.SettledAt.Valid {
		out["settledAt"] = p.SettledAt.Time.UnixMilli()
	}
	if p.DistributedAt.Valid {
		out["distributedAt"] = p.DistributedAt.Time.UnixMilli()
	}
	return out
}

func (s *Server) loadProject(ctx context.Context, q interface {
	QueryRowContext(context.Context, string, ...any) *sql.Row
}, id string, lock bool) (*project, error) {
	sfx := ""
	if lock {
		sfx = " FOR UPDATE"
	}
	p, err := scanProject(q.QueryRowContext(ctx, `SELECT `+projectCols+` FROM launchpad_projects WHERE id=$1`+sfx, id))
	if errors.Is(err, sql.ErrNoRows) {
		return nil, notFound("PROJECT_NOT_FOUND", "项目不存在")
	}
	return p, err
}

func (s *Server) handleProjects(w http.ResponseWriter, r *http.Request) {
	q := r.URL.Query()
	where := []string{"status <> 'cancelled'"}
	args := []any{}
	if st := q.Get("status"); st != "" {
		args = append(args, st)
		where = append(where, fmt.Sprintf("status=$%d", len(args)))
	}
	rows, err := s.db.QueryContext(r.Context(), `SELECT `+projectCols+` FROM launchpad_projects
		WHERE `+strings.Join(where, " AND ")+` ORDER BY sort, start_at`, args...)
	if err != nil {
		writeErr(w, err)
		return
	}
	defer rows.Close()
	items := []map[string]any{}
	for rows.Next() {
		p, err := scanProject(rows)
		if err != nil {
			writeErr(w, err)
			return
		}
		items = append(items, s.publicView(p))
	}
	common.WriteJSON(w, http.StatusOK, map[string]any{"items": items})
}

func (s *Server) handleProject(w http.ResponseWriter, r *http.Request) {
	p, err := s.loadProject(r.Context(), s.db, r.PathValue("id"), false)
	if err != nil {
		writeErr(w, err)
		return
	}
	common.WriteJSON(w, http.StatusOK, s.publicView(p))
}

func (s *Server) handleRules(w http.ResponseWriter, r *http.Request) {
	p, err := s.loadProject(r.Context(), s.db, r.PathValue("id"), false)
	if err != nil {
		writeErr(w, err)
		return
	}
	common.WriteJSON(w, http.StatusOK, map[string]any{
		"id":      p.ID,
		"symbol":  p.Symbol,
		"rulesMd": p.RulesMD,
	})
}

// ------------- admin -------------

type upsertReq struct {
	ID            string           `json:"id"`
	Symbol        string           `json:"symbol"`
	Name          string           `json:"name"`
	Logo          string           `json:"logo"`
	Description   string           `json:"description"`
	BaseAsset     string           `json:"baseAsset"`
	Price         decimal.Decimal  `json:"price"`
	TotalSupply   decimal.Decimal  `json:"totalSupply"`
	MinSub        decimal.Decimal  `json:"minSub"`
	MaxSub        *decimal.Decimal `json:"maxSub"`
	StartAt       int64            `json:"startAt"`
	EndAt         int64            `json:"endAt"`
	SettleAt      int64            `json:"settleAt"`
	DistributeAt  int64            `json:"distributeAt"`
	RulesMD       string           `json:"rulesMd"`
	Sort          int              `json:"sort"`
}

func (s *Server) handleUpsertProject(w http.ResponseWriter, r *http.Request) {
	var req upsertReq
	if err := common.DecodeJSON(r, &req); err != nil {
		writeErr(w, bad("VALIDATION_ERROR", "请求参数不合法"))
		return
	}
	if req.ID == "" || req.Symbol == "" || req.Name == "" || req.BaseAsset == "" ||
		req.Price.Sign() <= 0 || req.TotalSupply.Sign() <= 0 || req.MinSub.Sign() < 0 ||
		req.StartAt == 0 || req.EndAt <= req.StartAt || req.SettleAt < req.EndAt || req.DistributeAt < req.SettleAt {
		writeErr(w, bad("VALIDATION_ERROR", "项目参数不合法"))
		return
	}
	start := time.UnixMilli(req.StartAt).UTC()
	end := time.UnixMilli(req.EndAt).UTC()
	settle := time.UnixMilli(req.SettleAt).UTC()
	dist := time.UnixMilli(req.DistributeAt).UTC()
	initial := "upcoming"
	now := s.now()
	if !now.Before(start) {
		initial = "open"
	}
	if !now.Before(end) {
		initial = "computing"
	}
	_, err := s.db.ExecContext(r.Context(), `INSERT INTO launchpad_projects
		(id, symbol, name, logo, description, base_asset, price, total_supply, min_sub, max_sub,
		 start_at, end_at, settle_at, distribute_at, status, rules_md, sort)
		VALUES ($1,$2,$3,$4,$5,$6,$7,$8,$9,$10,$11,$12,$13,$14,$15,$16,$17)
		ON CONFLICT (id) DO UPDATE SET symbol=$2, name=$3, logo=$4, description=$5, base_asset=$6,
			price=$7, total_supply=$8, min_sub=$9, max_sub=$10, start_at=$11, end_at=$12,
			settle_at=$13, distribute_at=$14, rules_md=$16, sort=$17`,
		req.ID, strings.ToUpper(req.Symbol), req.Name, req.Logo, req.Description,
		strings.ToUpper(req.BaseAsset), req.Price, req.TotalSupply, req.MinSub, req.MaxSub,
		start, end, settle, dist, initial, req.RulesMD, req.Sort)
	if err != nil {
		writeErr(w, err)
		return
	}
	common.WriteJSON(w, http.StatusOK, map[string]any{"id": req.ID, "status": initial})
}

func (s *Server) handleAdminProjects(w http.ResponseWriter, r *http.Request) {
	rows, err := s.db.QueryContext(r.Context(), `SELECT `+projectCols+` FROM launchpad_projects ORDER BY sort, start_at DESC`)
	if err != nil {
		writeErr(w, err)
		return
	}
	defer rows.Close()
	items := []map[string]any{}
	for rows.Next() {
		p, err := scanProject(rows)
		if err != nil {
			writeErr(w, err)
			return
		}
		m := s.publicView(p)
		m["rulesMd"] = p.RulesMD
		m["rawStatus"] = p.Status
		items = append(items, m)
	}
	common.WriteJSON(w, http.StatusOK, map[string]any{"items": items})
}

func (s *Server) handleForceSettle(w http.ResponseWriter, r *http.Request) {
	s.mu.Lock()
	defer s.mu.Unlock()
	if err := s.settleOne(r.Context(), r.PathValue("id")); err != nil {
		writeErr(w, err)
		return
	}
	common.WriteJSON(w, http.StatusOK, map[string]any{"status": "ok"})
}

func (s *Server) handleForceDistribute(w http.ResponseWriter, r *http.Request) {
	s.mu.Lock()
	defer s.mu.Unlock()
	if err := s.distributeOne(r.Context(), r.PathValue("id")); err != nil {
		writeErr(w, err)
		return
	}
	common.WriteJSON(w, http.StatusOK, map[string]any{"status": "ok"})
}

// Admin cancel: refund every live subscription and mark the project cancelled.
func (s *Server) handleAdminCancel(w http.ResponseWriter, r *http.Request) {
	s.mu.Lock()
	defer s.mu.Unlock()
	if err := s.cancelProject(r.Context(), r.PathValue("id")); err != nil {
		writeErr(w, err)
		return
	}
	common.WriteJSON(w, http.StatusOK, map[string]any{"status": "cancelled"})
}

func (s *Server) event(ctx context.Context, tx *sql.Tx, uid, pid, kind, asset string, amount decimal.Decimal, detail map[string]any) error {
	d := []byte("{}")
	if detail != nil {
		d, _ = json.Marshal(detail)
	}
	_, err := tx.ExecContext(ctx, `INSERT INTO launchpad_events (uid, project_id, kind, asset, amount, detail, at)
		VALUES ($1,$2,$3,$4,$5,$6,$7)`, uid, pid, kind, asset, amount, d, s.now())
	return err
}

