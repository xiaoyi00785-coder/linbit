package main

import (
	"context"
	"database/sql"
	"net/http"
	"strings"
	"sync"
	"sync/atomic"
	"time"

	"github.com/linbit/platform/services/go/common"
	"github.com/shopspring/decimal"
)

// Per-user futures fee overrides (fut_fee_overrides). A NULL column falls back
// to the contract's rate. The cache refreshes at most every 30s and immediately
// after admin writes.

type futFeeOv struct {
	Maker, Taker *decimal.Decimal
	Note, By     string
	At           time.Time
}

type futFeeCache struct {
	mu        sync.RWMutex
	m         map[string]futFeeOv
	loaded    time.Time
	reloading atomic.Bool
}

var futFees = &futFeeCache{m: map[string]futFeeOv{}}

func (s *Server) reloadFeeOverrides(ctx context.Context) error {
	if s.db == nil {
		return nil
	}
	rows, err := s.db.QueryContext(ctx, `SELECT user_id, maker_fee::text, taker_fee::text, note, updated_by, updated_at FROM fut_fee_overrides`)
	if err != nil {
		return err
	}
	defer rows.Close()
	m := map[string]futFeeOv{}
	for rows.Next() {
		var uid string
		var mk, tk sql.NullString
		var o futFeeOv
		if err := rows.Scan(&uid, &mk, &tk, &o.Note, &o.By, &o.At); err != nil {
			return err
		}
		if mk.Valid {
			d := decimal.RequireFromString(mk.String)
			o.Maker = &d
		}
		if tk.Valid {
			d := decimal.RequireFromString(tk.String)
			o.Taker = &d
		}
		m[uid] = o
	}
	if err := rows.Err(); err != nil {
		return err
	}
	futFees.mu.Lock()
	futFees.m, futFees.loaded = m, time.Now()
	futFees.mu.Unlock()
	return nil
}

// userFees returns the effective maker/taker fee for uid on contract c.
func (s *Server) userFees(uid string, c Contract) (maker, taker decimal.Decimal, overridden bool) {
	maker, taker = c.MakerFee, c.TakerFee
	futFees.mu.RLock()
	stale := time.Since(futFees.loaded) > 30*time.Second
	o, ok := futFees.m[uid]
	futFees.mu.RUnlock()
	if stale && futFees.reloading.CompareAndSwap(false, true) {
		// Refresh off the hot path (callers may hold the engine lock / a tx).
		go func() {
			defer futFees.reloading.Store(false)
			ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
			defer cancel()
			_ = s.reloadFeeOverrides(ctx)
		}()
	}
	if !ok {
		return
	}
	if o.Maker != nil {
		maker, overridden = *o.Maker, true
	}
	if o.Taker != nil {
		taker, overridden = *o.Taker, true
	}
	return
}

// reserveTaker is the taker rate used for margin reservation (never below the
// contract rate, so an override can't under-reserve).
func (s *Server) reserveTaker(uid string, c Contract) decimal.Decimal {
	_, t, _ := s.userFees(uid, c)
	return decimal.Max(t, c.TakerFee)
}

func (s *Server) feeOverrideRoutes(mux *http.ServeMux) {
	mux.HandleFunc("GET /internal/fee-rates", s.internalOnly(s.hInternalFeeRates))
	mux.HandleFunc("GET /internal/admin/fee-overrides", s.internalOnly(s.hListFeeOverrides))
	mux.HandleFunc("PUT /internal/admin/fee-overrides/{userId}", s.internalOnly(s.hPutFeeOverride))
	mux.HandleFunc("DELETE /internal/admin/fee-overrides/{userId}", s.internalOnly(s.hDeleteFeeOverride))
}

// GET /internal/fee-rates?userId=&symbol= → {symbol, maker, taker, overridden}.
// Without symbol, the first listed contract's default rate is used as the base.
func (s *Server) hInternalFeeRates(w http.ResponseWriter, r *http.Request) {
	uid := r.URL.Query().Get("userId")
	sym := strings.ToUpper(r.URL.Query().Get("symbol"))
	var c Contract
	var err error
	if sym != "" {
		c, err = s.contract(sym)
	}
	if sym == "" || err != nil {
		if len(s.symbols) > 0 {
			c, _ = s.contract(s.symbols[0])
		}
		sym = c.Symbol
	}
	m, t, ov := s.userFees(uid, c)
	common.WriteJSON(w, http.StatusOK, map[string]any{"symbol": sym, "maker": m.String(), "taker": t.String(), "overridden": ov})
}

type feeOvDTO struct {
	UserID    string  `json:"userId"`
	Maker     *string `json:"maker"`
	Taker     *string `json:"taker"`
	Note      string  `json:"note"`
	UpdatedBy string  `json:"updatedBy"`
	UpdatedAt string  `json:"updatedAt"`
}

func ovDTO(uid string, o futFeeOv) feeOvDTO {
	d := feeOvDTO{UserID: uid, Note: o.Note, UpdatedBy: o.By, UpdatedAt: o.At.UTC().Format(time.RFC3339)}
	if o.Maker != nil {
		v := o.Maker.String()
		d.Maker = &v
	}
	if o.Taker != nil {
		v := o.Taker.String()
		d.Taker = &v
	}
	return d
}

func (s *Server) hListFeeOverrides(w http.ResponseWriter, r *http.Request) {
	if err := s.reloadFeeOverrides(r.Context()); err != nil {
		common.WriteError(w, http.StatusInternalServerError, "INTERNAL", "服务器内部错误")
		return
	}
	uid := r.URL.Query().Get("userId")
	items := []feeOvDTO{}
	futFees.mu.RLock()
	for k, o := range futFees.m {
		if uid == "" || k == uid {
			items = append(items, ovDTO(k, o))
		}
	}
	futFees.mu.RUnlock()
	common.WriteJSON(w, http.StatusOK, map[string]any{"items": items})
}

func parseFeeRate(v string) (any, bool) {
	v = strings.TrimSpace(v)
	if v == "" {
		return nil, true
	}
	d, err := decimal.NewFromString(v)
	if err != nil || d.IsNegative() || d.GreaterThanOrEqual(decimal.RequireFromString("0.1")) {
		return nil, false
	}
	return d.String(), true
}

func (s *Server) hPutFeeOverride(w http.ResponseWriter, r *http.Request) {
	uid := r.PathValue("userId")
	var req struct {
		Maker     string `json:"maker"`
		Taker     string `json:"taker"`
		Note      string `json:"note"`
		UpdatedBy string `json:"updatedBy"`
	}
	if uid == "" || common.DecodeJSON(r, &req) != nil {
		common.WriteError(w, http.StatusBadRequest, "VALIDATION_ERROR", "请求参数不合法")
		return
	}
	m, ok1 := parseFeeRate(req.Maker)
	t, ok2 := parseFeeRate(req.Taker)
	if !ok1 || !ok2 || (m == nil && t == nil) {
		common.WriteError(w, http.StatusBadRequest, "VALIDATION_ERROR", "费率需在 [0, 0.1) 之间")
		return
	}
	if _, err := s.db.ExecContext(r.Context(), `INSERT INTO fut_fee_overrides (user_id, maker_fee, taker_fee, note, updated_by)
		VALUES ($1,$2,$3,$4,$5) ON CONFLICT (user_id) DO UPDATE SET maker_fee=EXCLUDED.maker_fee, taker_fee=EXCLUDED.taker_fee,
		note=EXCLUDED.note, updated_by=EXCLUDED.updated_by, updated_at=now()`, uid, m, t, req.Note, req.UpdatedBy); err != nil {
		common.WriteError(w, http.StatusInternalServerError, "INTERNAL", "服务器内部错误")
		return
	}
	if err := s.reloadFeeOverrides(r.Context()); err != nil {
		common.WriteError(w, http.StatusInternalServerError, "INTERNAL", "服务器内部错误")
		return
	}
	futFees.mu.RLock()
	o := futFees.m[uid]
	futFees.mu.RUnlock()
	common.WriteJSON(w, http.StatusOK, ovDTO(uid, o))
}

func (s *Server) hDeleteFeeOverride(w http.ResponseWriter, r *http.Request) {
	if _, err := s.db.ExecContext(r.Context(), `DELETE FROM fut_fee_overrides WHERE user_id=$1`, r.PathValue("userId")); err != nil {
		common.WriteError(w, http.StatusInternalServerError, "INTERNAL", "服务器内部错误")
		return
	}
	_ = s.reloadFeeOverrides(r.Context())
	common.WriteJSON(w, http.StatusOK, map[string]string{"status": "deleted"})
}
