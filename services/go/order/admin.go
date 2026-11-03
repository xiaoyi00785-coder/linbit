package main

import (
	"context"
	"crypto/subtle"

	"errors"
	"net/http"
	"strings"
	"sync"

	"github.com/linbit/platform/services/go/common"
	"github.com/shopspring/decimal"
)

// feeOverrides holds runtime fee rates loaded from fee_rates (nil = env config).
type feeOverrides struct {
	mu    sync.RWMutex
	maker *decimal.Decimal
	taker *decimal.Decimal
}

var errBadSymbol = errors.New("bad symbol")

// LoadFees refreshes the runtime fee overrides from the database.
func (s *Server) LoadFees(ctx context.Context) error {
	rows, err := s.store.db.QueryContext(ctx, `SELECT key, rate::text FROM fee_rates`)
	if err != nil {
		return err
	}
	defer rows.Close()
	s.fees.mu.Lock()
	defer s.fees.mu.Unlock()
	for rows.Next() {
		var k, v string
		if err := rows.Scan(&k, &v); err != nil {
			return err
		}
		d := decimal.RequireFromString(v)
		switch k {
		case "spot_maker":
			s.fees.maker = &d
		case "spot_taker":
			s.fees.taker = &d
		}
	}
	return rows.Err()
}

func (s *Server) currentFees() (maker, taker decimal.Decimal) {
	maker, taker = s.cfg.SpotMakerFeeRate, s.cfg.SpotTakerFeeRate
	if s.fees == nil {
		return
	}
	s.fees.mu.RLock()
	defer s.fees.mu.RUnlock()
	if s.fees.maker != nil {
		maker = *s.fees.maker
	}
	if s.fees.taker != nil {
		taker = *s.fees.taker
	}
	return
}

func (s *Server) internal(next http.HandlerFunc) http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		tok := s.cfg.InternalToken
		got := r.Header.Get("X-Internal-Token")
		if tok == "" || subtle.ConstantTimeCompare([]byte(tok), []byte(got)) != 1 {
			common.WriteError(w, http.StatusUnauthorized, "AUTH_UNAUTHORIZED", "内部令牌无效")
			return
		}
		next(w, r)
	}
}

func (s *Server) adminRoutes(mux *http.ServeMux) {
	mux.HandleFunc("GET /internal/admin/fees", s.internal(s.handleGetFees))
	mux.HandleFunc("PUT /internal/admin/fees", s.internal(s.handlePutFees))
	mux.HandleFunc("GET /internal/admin/symbols", s.internal(s.handleListSymbols))
	mux.HandleFunc("POST /internal/admin/symbols", s.internal(s.handleCreateSymbol))
	mux.HandleFunc("PATCH /internal/admin/symbols/{symbol}", s.internal(s.handlePatchSymbol))
}

func (s *Server) handleGetFees(w http.ResponseWriter, r *http.Request) {
	m, t := s.currentFees()
	common.WriteJSON(w, http.StatusOK, map[string]string{"spotMaker": m.String(), "spotTaker": t.String()})
}

type feesRequest struct {
	SpotMaker string `json:"spotMaker"`
	SpotTaker string `json:"spotTaker"`
	By        string `json:"by"`
}

func parseRate(v string) (*decimal.Decimal, bool) {
	if strings.TrimSpace(v) == "" {
		return nil, true
	}
	d, err := decimal.NewFromString(v)
	if err != nil || d.IsNegative() || d.GreaterThanOrEqual(decimal.RequireFromString("0.1")) {
		return nil, false
	}
	return &d, true
}

// PUT /internal/admin/fees {spotMaker?, spotTaker?, by}
func (s *Server) handlePutFees(w http.ResponseWriter, r *http.Request) {
	var req feesRequest
	if err := common.DecodeJSON(r, &req); err != nil {
		common.WriteError(w, http.StatusBadRequest, "VALIDATION_ERROR", "请求参数不合法")
		return
	}
	m, ok1 := parseRate(req.SpotMaker)
	t, ok2 := parseRate(req.SpotTaker)
	if !ok1 || !ok2 || (m == nil && t == nil) {
		common.WriteError(w, http.StatusBadRequest, "VALIDATION_ERROR", "费率需在 [0, 0.1) 之间")
		return
	}
	for k, v := range map[string]*decimal.Decimal{"spot_maker": m, "spot_taker": t} {
		if v == nil {
			continue
		}
		if _, err := s.store.db.ExecContext(r.Context(),
			`INSERT INTO fee_rates (key, rate, updated_by) VALUES ($1, $2, $3)
			 ON CONFLICT (key) DO UPDATE SET rate = EXCLUDED.rate, updated_by = EXCLUDED.updated_by, updated_at = now()`,
			k, v.String(), req.By); err != nil {
			common.WriteError(w, http.StatusInternalServerError, "INTERNAL", "服务器内部错误")
			return
		}
	}
	if err := s.LoadFees(r.Context()); err != nil {
		common.WriteError(w, http.StatusInternalServerError, "INTERNAL", "服务器内部错误")
		return
	}
	s.handleGetFees(w, r)
}

type symbolDTO struct {
	Symbol            string `json:"symbol"`
	Base              string `json:"base"`
	Quote             string `json:"quote"`
	PricePrecision    int    `json:"pricePrecision"`
	QuantityPrecision int    `json:"quantityPrecision"`
	MinQuantity       string `json:"minQuantity"`
	MinNotional       string `json:"minNotional"`
	Status            string `json:"status"`
}

func toSymbolDTO(c SymbolConfig) symbolDTO {
	return symbolDTO{c.Symbol, c.Base, c.Quote, c.PricePrecision, c.QuantityPrecision,
		c.MinQuantity.String(), c.MinNotional.String(), c.Status}
}

func (s *Server) handleListSymbols(w http.ResponseWriter, r *http.Request) {
	rows, err := s.store.Symbols(r.Context())
	if err != nil {
		common.WriteError(w, http.StatusInternalServerError, "INTERNAL", "服务器内部错误")
		return
	}
	out := make([]symbolDTO, 0, len(rows))
	for _, c := range rows {
		out = append(out, toSymbolDTO(c))
	}
	common.WriteJSON(w, http.StatusOK, map[string]any{"items": out})
}

var validSymbolStatus = map[string]bool{"trading": true, "halted": true, "delisted": true}

func (s *Server) handleCreateSymbol(w http.ResponseWriter, r *http.Request) {
	var req symbolDTO
	if err := common.DecodeJSON(r, &req); err != nil {
		common.WriteError(w, http.StatusBadRequest, "VALIDATION_ERROR", "请求参数不合法")
		return
	}
	req.Base, req.Quote = strings.ToUpper(strings.TrimSpace(req.Base)), strings.ToUpper(strings.TrimSpace(req.Quote))
	if req.Status == "" {
		req.Status = "halted"
	}
	mq, e1 := decimal.NewFromString(defStr(req.MinQuantity, "0"))
	mn, e2 := decimal.NewFromString(defStr(req.MinNotional, "0"))
	if req.Base == "" || req.Quote == "" || e1 != nil || e2 != nil || mq.IsNegative() || mn.IsNegative() ||
		req.PricePrecision < 0 || req.PricePrecision > 12 || req.QuantityPrecision < 0 || req.QuantityPrecision > 12 ||
		!validSymbolStatus[req.Status] {
		common.WriteError(w, http.StatusBadRequest, "VALIDATION_ERROR", "交易对参数不合法")
		return
	}
	sym := req.Base + req.Quote
	_, err := s.store.db.ExecContext(r.Context(),
		`INSERT INTO symbols (symbol, base, quote, price_precision, quantity_precision, min_quantity, min_notional, status)
		 VALUES ($1,$2,$3,$4,$5,$6,$7,$8)`, sym, req.Base, req.Quote, req.PricePrecision, req.QuantityPrecision,
		mq.String(), mn.String(), req.Status)
	if err != nil {
		if strings.Contains(err.Error(), "duplicate key") {
			common.WriteError(w, http.StatusConflict, "SYMBOL_EXISTS", "交易对已存在")
			return
		}
		common.WriteError(w, http.StatusInternalServerError, "INTERNAL", "服务器内部错误")
		return
	}
	c, _ := s.store.Symbol(r.Context(), sym)
	common.WriteJSON(w, http.StatusCreated, toSymbolDTO(*c))
}

type symbolPatch struct {
	Status      *string `json:"status"`
	MinQuantity *string `json:"minQuantity"`
	MinNotional *string `json:"minNotional"`
}

func (s *Server) handlePatchSymbol(w http.ResponseWriter, r *http.Request) {
	sym := strings.ToUpper(r.PathValue("symbol"))
	var p symbolPatch
	if err := common.DecodeJSON(r, &p); err != nil {
		common.WriteError(w, http.StatusBadRequest, "VALIDATION_ERROR", "请求参数不合法")
		return
	}
	cur, err := s.store.Symbol(r.Context(), sym)
	if errors.Is(err, ErrSymbolNotFound) {
		common.WriteError(w, http.StatusNotFound, "NOT_FOUND", "交易对不存在")
		return
	} else if err != nil {
		common.WriteError(w, http.StatusInternalServerError, "INTERNAL", "服务器内部错误")
		return
	}
	if p.Status != nil {
		if !validSymbolStatus[*p.Status] {
			common.WriteError(w, http.StatusBadRequest, "VALIDATION_ERROR", "状态不合法")
			return
		}
		cur.Status = *p.Status
	}
	for _, f := range []struct {
		v   *string
		dst *decimal.Decimal
	}{{p.MinQuantity, &cur.MinQuantity}, {p.MinNotional, &cur.MinNotional}} {
		if f.v == nil {
			continue
		}
		d, err := decimal.NewFromString(*f.v)
		if err != nil || d.IsNegative() {
			common.WriteError(w, http.StatusBadRequest, "VALIDATION_ERROR", "最小值不合法")
			return
		}
		*f.dst = d
	}
	if _, err := s.store.db.ExecContext(r.Context(),
		`UPDATE symbols SET status=$2, min_quantity=$3, min_notional=$4 WHERE symbol=$1`,
		sym, cur.Status, cur.MinQuantity.String(), cur.MinNotional.String()); err != nil {
		common.WriteError(w, http.StatusInternalServerError, "INTERNAL", "服务器内部错误")
		return
	}
	common.WriteJSON(w, http.StatusOK, toSymbolDTO(*cur))
}

func defStr(v, d string) string {
	if strings.TrimSpace(v) == "" {
		return d
	}
	return v
}
