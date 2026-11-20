package main

import (
	"context"
	"database/sql"
	"errors"
	"fmt"
	"net/http"
	"strings"

	"github.com/linbit/platform/services/go/common"
	"github.com/shopspring/decimal"
)

// Staking products: one validator per asset per row. These are account-based
// (the staking service is not a chain client) and seeded from
// migrations/00001_staking.sql; ops edit via the internal endpoints.

const dp = 8

func floor8(d decimal.Decimal) decimal.Decimal { return d.RoundFloor(dp) }

type product struct {
	ID            string           `json:"id"`
	Asset         string           `json:"asset"`
	Chain         string           `json:"chain"`
	Validator     string           `json:"validator"`
	ValidatorAddr *string          `json:"validatorAddr,omitempty"`
	APR           decimal.Decimal  `json:"apr"`
	UnbondingDays int              `json:"unbondingDays"`
	MinAmount     decimal.Decimal  `json:"minAmount"`
	MaxAmount     *decimal.Decimal `json:"maxAmount,omitempty"`
	TotalQuota    *decimal.Decimal `json:"totalQuota,omitempty"`
	Sold          decimal.Decimal  `json:"sold"`
	Remaining     *decimal.Decimal `json:"remaining,omitempty"`
	Commission    decimal.Decimal  `json:"commission"`
	LogoURL       *string          `json:"logoUrl,omitempty"`
	Status        string           `json:"status"`
	Hot           bool             `json:"hot"`
	Sort          int              `json:"sort"`
}

const productCols = `id, asset, chain, validator, validator_addr, apr, unbonding_days, min_amount, max_amount,
	total_quota, sold, commission, logo_url, status, hot, sort`

type scanner interface{ Scan(...any) error }

func scanProduct(r scanner) (*product, error) {
	p := &product{}
	var vaddr, logo sql.NullString
	var maxA, tq decimal.NullDecimal
	if err := r.Scan(&p.ID, &p.Asset, &p.Chain, &p.Validator, &vaddr, &p.APR, &p.UnbondingDays, &p.MinAmount,
		&maxA, &tq, &p.Sold, &p.Commission, &logo, &p.Status, &p.Hot, &p.Sort); err != nil {
		return nil, err
	}
	if vaddr.Valid {
		v := vaddr.String
		p.ValidatorAddr = &v
	}
	if logo.Valid {
		v := logo.String
		p.LogoURL = &v
	}
	if maxA.Valid {
		v := maxA.Decimal
		p.MaxAmount = &v
	}
	if tq.Valid {
		v := tq.Decimal
		p.TotalQuota = &v
		rem := decimal.Max(decimal.Zero, v.Sub(p.Sold))
		p.Remaining = &rem
	}
	return p, nil
}

func (s *Server) loadProduct(ctx context.Context, q interface {
	QueryRowContext(context.Context, string, ...any) *sql.Row
}, id string, lock bool) (*product, error) {
	sfx := ""
	if lock {
		sfx = " FOR UPDATE"
	}
	p, err := scanProduct(q.QueryRowContext(ctx, `SELECT `+productCols+` FROM s_products WHERE id=$1`+sfx, id))
	if errors.Is(err, sql.ErrNoRows) {
		return nil, notFound("PRODUCT_NOT_FOUND", "质押产品不存在")
	}
	return p, err
}

// handleValidators lists online products, optionally filtered by asset. It is
// the catalogue EasiCoin's Flutter shell renders on the staking page.
func (s *Server) handleValidators(w http.ResponseWriter, r *http.Request) {
	q := r.URL.Query()
	where, args := []string{"status='online'"}, []any{}
	if a := strings.ToUpper(q.Get("asset")); a != "" {
		args = append(args, a)
		where = append(where, fmt.Sprintf("asset=$%d", len(args)))
	}
	if c := q.Get("chain"); c != "" {
		args = append(args, c)
		where = append(where, fmt.Sprintf("chain=$%d", len(args)))
	}
	rows, err := s.db.QueryContext(r.Context(), `SELECT `+productCols+` FROM s_products WHERE `+strings.Join(where, " AND ")+` ORDER BY sort, id`, args...)
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

func (s *Server) handleValidator(w http.ResponseWriter, r *http.Request) {
	p, err := s.loadProduct(r.Context(), s.db, r.PathValue("id"), false)
	if err != nil {
		writeErr(w, err)
		return
	}
	common.WriteJSON(w, http.StatusOK, p)
}

// ---- admin ----

func (s *Server) handleAdminProducts(w http.ResponseWriter, r *http.Request) {
	q := `SELECT ` + productCols + ` FROM s_products`
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

// handleUpsertProduct creates or updates a staking product (idempotent on id).
func (s *Server) handleUpsertProduct(w http.ResponseWriter, r *http.Request) {
	var p struct {
		ID            string           `json:"id"`
		Asset         string           `json:"asset"`
		Chain         string           `json:"chain"`
		Validator     string           `json:"validator"`
		ValidatorAddr *string          `json:"validatorAddr"`
		APR           decimal.Decimal  `json:"apr"`
		UnbondingDays int              `json:"unbondingDays"`
		MinAmount     decimal.Decimal  `json:"minAmount"`
		MaxAmount     *decimal.Decimal `json:"maxAmount"`
		TotalQuota    *decimal.Decimal `json:"totalQuota"`
		Commission    *decimal.Decimal `json:"commission"`
		LogoURL       *string          `json:"logoUrl"`
		Status        string           `json:"status"`
		Hot           bool             `json:"hot"`
		Sort          int              `json:"sort"`
	}
	if err := common.DecodeJSON(r, &p); err != nil || p.ID == "" || p.Asset == "" || p.Chain == "" || p.Validator == "" ||
		p.MinAmount.Sign() <= 0 || p.APR.Sign() < 0 || p.APR.GreaterThan(decimal.NewFromInt(1)) || p.UnbondingDays < 0 ||
		(p.MaxAmount != nil && p.MaxAmount.Sign() <= 0) || (p.TotalQuota != nil && p.TotalQuota.Sign() <= 0) {
		writeErr(w, bad("VALIDATION_ERROR", "产品参数不合法"))
		return
	}
	if p.Status == "" {
		p.Status = "online"
	}
	if p.Status != "online" && p.Status != "offline" {
		writeErr(w, bad("VALIDATION_ERROR", "status 只能是 online/offline"))
		return
	}
	commission := decimal.Zero
	if p.Commission != nil {
		commission = *p.Commission
	}
	_, err := s.db.ExecContext(r.Context(), `INSERT INTO s_products
		(id, asset, chain, validator, validator_addr, apr, unbonding_days, min_amount, max_amount, total_quota,
		 commission, logo_url, status, hot, sort)
		VALUES ($1,$2,$3,$4,$5,$6,$7,$8,$9,$10,$11,$12,$13,$14,$15)
		ON CONFLICT (id) DO UPDATE SET chain=$3, validator=$4, validator_addr=$5, apr=$6, unbonding_days=$7,
			min_amount=$8, max_amount=$9, total_quota=$10, commission=$11, logo_url=$12, status=$13, hot=$14, sort=$15`,
		p.ID, strings.ToUpper(p.Asset), p.Chain, p.Validator, p.ValidatorAddr, p.APR, p.UnbondingDays, p.MinAmount,
		p.MaxAmount, p.TotalQuota, commission, p.LogoURL, p.Status, p.Hot, p.Sort)
	if err != nil {
		writeErr(w, err)
		return
	}
	common.WriteJSON(w, http.StatusOK, map[string]any{"id": p.ID})
}

func (s *Server) handleDeleteProduct(w http.ResponseWriter, r *http.Request) {
	ctx := r.Context()
	id := r.PathValue("id")
	var used bool
	if err := s.db.QueryRowContext(ctx, `SELECT EXISTS(SELECT 1 FROM s_positions WHERE product_id=$1)`, id).Scan(&used); err != nil {
		writeErr(w, err)
		return
	}
	if used {
		// Keep history intact; take the product offline instead.
		if _, err := s.db.ExecContext(ctx, `UPDATE s_products SET status='offline' WHERE id=$1`, id); err != nil {
			writeErr(w, err)
			return
		}
		common.WriteJSON(w, http.StatusOK, map[string]any{"id": id, "status": "offline"})
		return
	}
	if _, err := s.db.ExecContext(ctx, `DELETE FROM s_products WHERE id=$1`, id); err != nil {
		writeErr(w, err)
		return
	}
	common.WriteJSON(w, http.StatusOK, map[string]any{"id": id, "deleted": true})
}
