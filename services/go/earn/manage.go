package main

import (
	"context"
	"database/sql"
	"errors"
	"net/http"
	"strings"

	"github.com/linbit/platform/services/go/common"
	"github.com/shopspring/decimal"
)

// Product management for the admin console (internal token only). Rates are
// written only through the dedicated /rate endpoints, which the admin service
// calls once a rate change has passed dual approval and its notice period.

var loanTermOrder = []string{"flexible", "7d", "30d"}

type loanTerm struct {
	Term        string           `json:"term"`
	Days        int              `json:"days"`
	HourlyRate  decimal.Decimal  `json:"hourlyRate"`
	AnnualRate  decimal.Decimal  `json:"annualRate"`
	Status      string           `json:"status"`
	TotalQuota  *decimal.Decimal `json:"totalQuota,omitempty"`
	Outstanding decimal.Decimal  `json:"outstanding"`
	Remaining   *decimal.Decimal `json:"remaining,omitempty"`
}

// seedLoanProducts inserts the env defaults for terms/collaterals that have
// no row yet (never overwrites admin edits).
func (s *Server) seedLoanProducts(ctx context.Context) error {
	for _, t := range loanTermOrder {
		if _, err := s.db.ExecContext(ctx, `INSERT INTO e_loan_terms (term, hourly_rate) VALUES ($1,$2) ON CONFLICT (term) DO NOTHING`,
			t, s.cfg.Loan.Rates[t]); err != nil {
			return err
		}
	}
	for i, c := range s.cfg.Loan.Collaterals {
		if _, err := s.db.ExecContext(ctx, `INSERT INTO e_loan_collaterals (asset, user_cap, sort) VALUES ($1,$2,$3) ON CONFLICT (asset) DO NOTHING`,
			c.Asset, c.Cap, i); err != nil {
			return err
		}
	}
	return nil
}

func (s *Server) loanTerms(ctx context.Context) ([]*loanTerm, error) {
	rows, err := s.db.QueryContext(ctx, `SELECT t.term, t.hourly_rate, t.status, t.total_quota,
		COALESCE((SELECT SUM(principal) FROM e_loans l WHERE l.term=t.term AND l.status IN ('pending','active')),0)
		FROM e_loan_terms t`)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	by := map[string]*loanTerm{}
	for rows.Next() {
		t := &loanTerm{}
		var q decimal.NullDecimal
		if err := rows.Scan(&t.Term, &t.HourlyRate, &t.Status, &q, &t.Outstanding); err != nil {
			return nil, err
		}
		t.Days = s.cfg.Loan.TermDays[t.Term]
		t.AnnualRate = t.HourlyRate.Mul(decimal.NewFromInt(24 * 365)).Round(4)
		if q.Valid {
			qq := q.Decimal
			rem := decimal.Max(decimal.Zero, qq.Sub(t.Outstanding))
			t.TotalQuota, t.Remaining = &qq, &rem
		}
		by[t.Term] = t
	}
	if err := rows.Err(); err != nil {
		return nil, err
	}
	out := []*loanTerm{}
	for _, k := range loanTermOrder {
		if t, ok := by[k]; ok {
			out = append(out, t)
		}
	}
	return out, nil
}

func (s *Server) loanTerm(ctx context.Context, term string) (*loanTerm, error) {
	ts, err := s.loanTerms(ctx)
	if err != nil {
		return nil, err
	}
	for _, t := range ts {
		if t.Term == term {
			return t, nil
		}
	}
	return nil, bad("INVALID_TERM", "借款期限不合法")
}

type loanCollateral struct {
	Asset  string          `json:"asset"`
	Cap    decimal.Decimal `json:"cap"`
	Status string          `json:"status"`
	Sort   int             `json:"sort"`
}

func (s *Server) loanCollaterals(ctx context.Context, onlineOnly bool) ([]loanCollateral, error) {
	q := `SELECT asset, user_cap, status, sort FROM e_loan_collaterals`
	if onlineOnly {
		q += ` WHERE status='online'`
	}
	rows, err := s.db.QueryContext(ctx, q+` ORDER BY sort, asset`)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	out := []loanCollateral{}
	for rows.Next() {
		var c loanCollateral
		if err := rows.Scan(&c.Asset, &c.Cap, &c.Status, &c.Sort); err != nil {
			return nil, err
		}
		out = append(out, c)
	}
	return out, rows.Err()
}

// collateralOf returns a collateral row (any status) for cap checks.
func (s *Server) collateralOf(ctx context.Context, asset string) (loanCollateral, error) {
	var c loanCollateral
	err := s.db.QueryRowContext(ctx, `SELECT asset, user_cap, status, sort FROM e_loan_collaterals WHERE asset=$1`, strings.ToUpper(asset)).
		Scan(&c.Asset, &c.Cap, &c.Status, &c.Sort)
	if errors.Is(err, sql.ErrNoRows) {
		return c, bad("UNSUPPORTED_COLLATERAL", "不支持的质押币种")
	}
	return c, err
}

func manageRoutes(s *Server, mux *http.ServeMux) {
	in := func(p string, h http.HandlerFunc) { mux.HandleFunc(p, s.internal(h)) }
	in("GET /internal/earn/products", s.handleAdminProducts)
	in("GET /internal/earn/products/{id}", s.handleAdminProduct)
	in("PATCH /internal/earn/products/{id}", s.handlePatchProduct)
	in("DELETE /internal/earn/products/{id}", s.handleDeleteProduct)
	in("PUT /internal/earn/products/{id}/rate", s.handleProductRate)
	in("PUT /internal/earn/products/{id}/quota", s.handleProductQuota)
	in("GET /internal/loans/terms", s.handleAdminLoanTerms)
	in("PATCH /internal/loans/terms/{term}", s.handlePatchLoanTerm)
	in("PUT /internal/loans/terms/{term}/rate", s.handleLoanTermRate)
	in("PUT /internal/loans/terms/{term}/quota", s.handleLoanTermQuota)
	in("GET /internal/loans/collaterals", s.handleAdminCollaterals)
	in("POST /internal/loans/collaterals", s.handleUpsertCollateral)
	in("DELETE /internal/loans/collaterals/{asset}", s.handleDeleteCollateral)
}

// ---- savings products ----

func (s *Server) getProduct(ctx context.Context, id string) (*product, error) {
	p, err := scanProduct(s.db.QueryRowContext(ctx, `SELECT `+productCols+` FROM e_products WHERE id=$1`, id))
	if errors.Is(err, sql.ErrNoRows) {
		return nil, notFound("PRODUCT_NOT_FOUND", "产品不存在")
	}
	return p, err
}

func (s *Server) handleAdminProducts(w http.ResponseWriter, r *http.Request) {
	q := `SELECT ` + productCols + ` FROM e_products`
	args := []any{}
	if st := r.URL.Query().Get("status"); st != "" {
		q += ` WHERE status=$1`
		args = append(args, st)
	}
	rows, err := s.db.QueryContext(r.Context(), q+` ORDER BY sort, id`, args...)
	if err != nil {
		writeErr(w, err)
		return
	}
	defer rows.Close()
	items := []*product{}
	for rows.Next() {
		p, err := scanProduct(rows)
		if err != nil {
			writeErr(w, err)
			return
		}
		items = append(items, p)
	}
	common.WriteJSON(w, http.StatusOK, map[string]any{"items": items})
}

func (s *Server) handleAdminProduct(w http.ResponseWriter, r *http.Request) {
	p, err := s.getProduct(r.Context(), r.PathValue("id"))
	if err != nil {
		writeErr(w, err)
		return
	}
	common.WriteJSON(w, http.StatusOK, p)
}

// PATCH: non-rate, non-quota fields.
func (s *Server) handlePatchProduct(w http.ResponseWriter, r *http.Request) {
	var req struct {
		Title      *string          `json:"title"`
		MinAmount  *decimal.Decimal `json:"minAmount"`
		UnlockDays *int             `json:"unlockDays"`
		Newcomer   *bool            `json:"newcomer"`
		Hot        *bool            `json:"hot"`
		Status     *string          `json:"status"`
		Sort       *int             `json:"sort"`
	}
	if err := common.DecodeJSON(r, &req); err != nil ||
		(req.Title != nil && strings.TrimSpace(*req.Title) == "") ||
		(req.MinAmount != nil && req.MinAmount.Sign() <= 0) ||
		(req.UnlockDays != nil && *req.UnlockDays < 0) ||
		(req.Status != nil && *req.Status != "online" && *req.Status != "offline") {
		writeErr(w, bad("VALIDATION_ERROR", "产品参数不合法"))
		return
	}
	ctx := r.Context()
	before, err := s.getProduct(ctx, r.PathValue("id"))
	if err != nil {
		writeErr(w, err)
		return
	}
	_, err = s.db.ExecContext(ctx, `UPDATE e_products SET title=COALESCE($2,title), min_amount=COALESCE($3,min_amount),
		unlock_days=COALESCE($4,unlock_days), newcomer=COALESCE($5,newcomer), hot=COALESCE($6,hot), status=COALESCE($7,status),
		sort=COALESCE($8,sort) WHERE id=$1`, before.ID, req.Title, req.MinAmount, req.UnlockDays, req.Newcomer, req.Hot, req.Status, req.Sort)
	if err != nil {
		writeErr(w, err)
		return
	}
	after, _ := s.getProduct(ctx, before.ID)
	common.WriteJSON(w, http.StatusOK, map[string]any{"before": before, "after": after})
}

// DELETE: only products nobody has ever used; otherwise take them offline.
func (s *Server) handleDeleteProduct(w http.ResponseWriter, r *http.Request) {
	ctx := r.Context()
	p, err := s.getProduct(ctx, r.PathValue("id"))
	if err != nil {
		writeErr(w, err)
		return
	}
	var used bool
	if err := s.db.QueryRowContext(ctx, `SELECT EXISTS(SELECT 1 FROM e_holdings WHERE product_id=$1) OR EXISTS(SELECT 1 FROM e_fixed_orders WHERE product_id=$1)`,
		p.ID).Scan(&used); err != nil {
		writeErr(w, err)
		return
	}
	if used {
		writeErr(w, conflict("PRODUCT_IN_USE", "产品已有用户持仓或订单，只能下架"))
		return
	}
	if _, err := s.db.ExecContext(ctx, `DELETE FROM e_products WHERE id=$1`, p.ID); err != nil {
		writeErr(w, err)
		return
	}
	common.WriteJSON(w, http.StatusOK, map[string]any{"deleted": p.ID, "before": p})
}

func validAPR(d *decimal.Decimal) bool {
	return d == nil || (!d.IsNegative() && d.LessThanOrEqual(decimal.NewFromInt(1)) && d.Exponent() >= -6)
}

// PUT rate {apr?, bonusApr?} — applied by the admin scheduler after approval.
func (s *Server) handleProductRate(w http.ResponseWriter, r *http.Request) {
	var req struct {
		APR      *decimal.Decimal `json:"apr"`
		BonusAPR *decimal.Decimal `json:"bonusApr"`
	}
	if err := common.DecodeJSON(r, &req); err != nil || (req.APR == nil && req.BonusAPR == nil) || !validAPR(req.APR) || !validAPR(req.BonusAPR) {
		writeErr(w, bad("VALIDATION_ERROR", "年化收益率需在 [0, 1] 之间，最多 6 位小数"))
		return
	}
	ctx := r.Context()
	before, err := s.getProduct(ctx, r.PathValue("id"))
	if err != nil {
		writeErr(w, err)
		return
	}
	if req.BonusAPR != nil && before.Kind != "simple" {
		writeErr(w, bad("VALIDATION_ERROR", "只有活期产品有加息档"))
		return
	}
	s.mu.Lock() // interest settlement reads rates under the same lock
	_, err = s.db.ExecContext(ctx, `UPDATE e_products SET apr=COALESCE($2,apr), bonus_apr=COALESCE($3,bonus_apr) WHERE id=$1`, before.ID, req.APR, req.BonusAPR)
	s.mu.Unlock()
	if err != nil {
		writeErr(w, err)
		return
	}
	after, _ := s.getProduct(ctx, before.ID)
	common.WriteJSON(w, http.StatusOK, map[string]any{"before": before, "after": after})
}

// PUT quota {totalQuota?, perUserMax?, bonusQuota?, clear?: ["totalQuota",...]}
func (s *Server) handleProductQuota(w http.ResponseWriter, r *http.Request) {
	var req struct {
		TotalQuota *decimal.Decimal `json:"totalQuota"`
		PerUserMax *decimal.Decimal `json:"perUserMax"`
		BonusQuota *decimal.Decimal `json:"bonusQuota"`
		Clear      []string         `json:"clear"`
	}
	pos := func(d *decimal.Decimal) bool { return d == nil || d.Sign() > 0 }
	if err := common.DecodeJSON(r, &req); err != nil || !pos(req.TotalQuota) || !pos(req.PerUserMax) || !pos(req.BonusQuota) ||
		(req.TotalQuota == nil && req.PerUserMax == nil && req.BonusQuota == nil && len(req.Clear) == 0) {
		writeErr(w, bad("VALIDATION_ERROR", "额度需大于 0"))
		return
	}
	clr := map[string]bool{}
	for _, c := range req.Clear {
		if c != "totalQuota" && c != "perUserMax" && c != "bonusQuota" {
			writeErr(w, bad("VALIDATION_ERROR", "clear 只能包含 totalQuota/perUserMax/bonusQuota"))
			return
		}
		clr[c] = true
	}
	ctx := r.Context()
	s.mu.Lock()
	defer s.mu.Unlock()
	before, err := s.getProduct(ctx, r.PathValue("id"))
	if err != nil {
		writeErr(w, err)
		return
	}
	if req.TotalQuota != nil && req.TotalQuota.LessThan(before.Sold) {
		writeErr(w, conflict("QUOTA_BELOW_SOLD", "总额度不能低于已售 "+before.Sold.String()))
		return
	}
	if req.BonusQuota != nil && before.Kind != "simple" {
		writeErr(w, bad("VALIDATION_ERROR", "只有活期产品有加息额度"))
		return
	}
	_, err = s.db.ExecContext(ctx, `UPDATE e_products SET
		total_quota = CASE WHEN $5 THEN NULL ELSE COALESCE($2,total_quota) END,
		per_user_max = CASE WHEN $6 THEN NULL ELSE COALESCE($3,per_user_max) END,
		bonus_quota = CASE WHEN $7 THEN NULL ELSE COALESCE($4,bonus_quota) END WHERE id=$1`,
		before.ID, req.TotalQuota, req.PerUserMax, req.BonusQuota, clr["totalQuota"], clr["perUserMax"], clr["bonusQuota"])
	if err != nil {
		writeErr(w, err)
		return
	}
	after, _ := s.getProduct(ctx, before.ID)
	common.WriteJSON(w, http.StatusOK, map[string]any{"before": before, "after": after})
}

// ---- loan terms ----

func (s *Server) handleAdminLoanTerms(w http.ResponseWriter, r *http.Request) {
	ts, err := s.loanTerms(r.Context())
	if err != nil {
		writeErr(w, err)
		return
	}
	common.WriteJSON(w, http.StatusOK, map[string]any{"items": ts})
}

func (s *Server) handlePatchLoanTerm(w http.ResponseWriter, r *http.Request) {
	var req struct {
		Status string `json:"status"`
	}
	if err := common.DecodeJSON(r, &req); err != nil || (req.Status != "online" && req.Status != "offline") {
		writeErr(w, bad("VALIDATION_ERROR", "status 只能是 online/offline"))
		return
	}
	s.updateTerm(w, r, `UPDATE e_loan_terms SET status=$2, updated_at=now() WHERE term=$1`, req.Status)
}

func (s *Server) handleLoanTermRate(w http.ResponseWriter, r *http.Request) {
	var req struct {
		HourlyRate *decimal.Decimal `json:"hourlyRate"`
	}
	if err := common.DecodeJSON(r, &req); err != nil || req.HourlyRate == nil || req.HourlyRate.IsNegative() ||
		req.HourlyRate.GreaterThan(decimal.RequireFromString("0.001")) || req.HourlyRate.Exponent() < -12 {
		writeErr(w, bad("VALIDATION_ERROR", "小时利率需在 [0, 0.001] 之间，最多 12 位小数"))
		return
	}
	s.updateTerm(w, r, `UPDATE e_loan_terms SET hourly_rate=$2, updated_at=now() WHERE term=$1`, *req.HourlyRate)
}

func (s *Server) handleLoanTermQuota(w http.ResponseWriter, r *http.Request) {
	var req struct {
		TotalQuota *decimal.Decimal `json:"totalQuota"`
		Clear      bool             `json:"clear"`
	}
	if err := common.DecodeJSON(r, &req); err != nil || (req.TotalQuota == nil) == !req.Clear || (req.TotalQuota != nil && req.TotalQuota.Sign() <= 0) {
		writeErr(w, bad("VALIDATION_ERROR", "需提供大于 0 的 totalQuota，或 clear=true 取消限额"))
		return
	}
	var v any
	if req.TotalQuota != nil {
		v = *req.TotalQuota
	}
	s.updateTerm(w, r, `UPDATE e_loan_terms SET total_quota=$2, updated_at=now() WHERE term=$1`, v)
}

// updateTerm runs one UPDATE on a term under the loan lock and returns before/after.
func (s *Server) updateTerm(w http.ResponseWriter, r *http.Request, q string, v any) {
	ctx := r.Context()
	s.mu.Lock()
	defer s.mu.Unlock()
	before, err := s.loanTerm(ctx, r.PathValue("term"))
	if err != nil {
		writeErr(w, notFound("TERM_NOT_FOUND", "借款期限不存在"))
		return
	}
	if _, err := s.db.ExecContext(ctx, q, before.Term, v); err != nil {
		writeErr(w, err)
		return
	}
	after, _ := s.loanTerm(ctx, before.Term)
	common.WriteJSON(w, http.StatusOK, map[string]any{"before": before, "after": after})
}

// ---- loan collaterals ----

func (s *Server) handleAdminCollaterals(w http.ResponseWriter, r *http.Request) {
	cs, err := s.loanCollaterals(r.Context(), false)
	if err != nil {
		writeErr(w, err)
		return
	}
	common.WriteJSON(w, http.StatusOK, map[string]any{"items": cs})
}

// POST {asset, cap, status?, sort?}: create or update (cap = per-user stock limit).
func (s *Server) handleUpsertCollateral(w http.ResponseWriter, r *http.Request) {
	var req struct {
		Asset  string          `json:"asset"`
		Cap    decimal.Decimal `json:"cap"`
		Status string          `json:"status"`
		Sort   *int            `json:"sort"`
	}
	if err := common.DecodeJSON(r, &req); err != nil || req.Cap.Sign() <= 0 || !validAsset(req.Asset) ||
		(req.Status != "" && req.Status != "online" && req.Status != "offline") || strings.EqualFold(req.Asset, s.cfg.Loan.Asset) {
		writeErr(w, bad("VALIDATION_ERROR", "质押币种参数不合法"))
		return
	}
	asset := strings.ToUpper(req.Asset)
	ctx := r.Context()
	s.mu.Lock()
	defer s.mu.Unlock()
	var before any
	if c, err := s.collateralOf(ctx, asset); err == nil {
		before = c
	}
	_, err := s.db.ExecContext(ctx, `INSERT INTO e_loan_collaterals (asset, user_cap, status, sort) VALUES ($1,$2,COALESCE(NULLIF($3,''),'online'),COALESCE($4,0))
		ON CONFLICT (asset) DO UPDATE SET user_cap=$2, status=COALESCE(NULLIF($3,''),e_loan_collaterals.status),
		sort=COALESCE($4,e_loan_collaterals.sort), updated_at=now()`, asset, req.Cap, req.Status, req.Sort)
	if err != nil {
		writeErr(w, err)
		return
	}
	after, _ := s.collateralOf(ctx, asset)
	common.WriteJSON(w, http.StatusOK, map[string]any{"before": before, "after": after})
}

func (s *Server) handleDeleteCollateral(w http.ResponseWriter, r *http.Request) {
	ctx := r.Context()
	s.mu.Lock()
	defer s.mu.Unlock()
	c, err := s.collateralOf(ctx, r.PathValue("asset"))
	if err != nil {
		writeErr(w, notFound("COLLATERAL_NOT_FOUND", "质押币种不存在"))
		return
	}
	var used bool
	if err := s.db.QueryRowContext(ctx, `SELECT EXISTS(SELECT 1 FROM e_loans WHERE collateral_asset=$1)`, c.Asset).Scan(&used); err != nil {
		writeErr(w, err)
		return
	}
	if used {
		writeErr(w, conflict("COLLATERAL_IN_USE", "该币种已有借款订单，只能下架"))
		return
	}
	if _, err := s.db.ExecContext(ctx, `DELETE FROM e_loan_collaterals WHERE asset=$1`, c.Asset); err != nil {
		writeErr(w, err)
		return
	}
	common.WriteJSON(w, http.StatusOK, map[string]any{"deleted": c.Asset, "before": c})
}

func validAsset(a string) bool {
	if len(a) < 2 || len(a) > 10 {
		return false
	}
	for _, ch := range a {
		if !(ch >= 'A' && ch <= 'Z' || ch >= 'a' && ch <= 'z' || ch >= '0' && ch <= '9') {
			return false
		}
	}
	return true
}
