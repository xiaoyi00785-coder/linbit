package main

import (
	"context"
	"database/sql"
	"encoding/json"
	"errors"
	"log"
	"net/http"
	"net/url"
	"strings"
	"sync"
	"time"

	"github.com/linbit/platform/services/go/common"
	"github.com/shopspring/decimal"
)

// userFee is a per-user spot fee override; nil fields fall back to the global rate.
type userFee struct {
	Maker     *decimal.Decimal
	Taker     *decimal.Decimal
	Note      string
	UpdatedBy string
	UpdatedAt time.Time
}

type userFeeCache struct {
	mu sync.RWMutex
	m  map[string]userFee
}

// LoadUserFees refreshes the per-user override cache from spot_fee_overrides.
func (s *Server) LoadUserFees(ctx context.Context) error {
	rows, err := s.store.db.QueryContext(ctx, `SELECT user_id, maker::text, taker::text, note, updated_by, updated_at FROM spot_fee_overrides`)
	if err != nil {
		return err
	}
	defer rows.Close()
	m := map[string]userFee{}
	for rows.Next() {
		var uid, note, by string
		var mk, tk sql.NullString
		var at time.Time
		if err := rows.Scan(&uid, &mk, &tk, &note, &by, &at); err != nil {
			return err
		}
		f := userFee{Note: note, UpdatedBy: by, UpdatedAt: at}
		if mk.Valid {
			d := decimal.RequireFromString(mk.String)
			f.Maker = &d
		}
		if tk.Valid {
			d := decimal.RequireFromString(tk.String)
			f.Taker = &d
		}
		m[uid] = f
	}
	if err := rows.Err(); err != nil {
		return err
	}
	if s.ufees == nil {
		s.ufees = &userFeeCache{}
	}
	s.ufees.mu.Lock()
	s.ufees.m = m
	s.ufees.mu.Unlock()
	return nil
}

// userSpotFees returns the effective maker/taker rates for a user and whether
// either is an override.
func (s *Server) userSpotFees(userID string) (maker, taker decimal.Decimal, overridden bool) {
	maker, taker = s.currentFees()
	if s.ufees == nil {
		return
	}
	s.ufees.mu.RLock()
	f, ok := s.ufees.m[userID]
	s.ufees.mu.RUnlock()
	if !ok {
		return
	}
	if f.Maker != nil {
		maker, overridden = *f.Maker, true
	}
	if f.Taker != nil {
		taker, overridden = *f.Taker, true
	}
	return
}

// userFeeRate is feeRate with the per-user override applied.
func (s *Server) userFeeRate(userID, role string) decimal.Decimal {
	m, t, _ := s.userSpotFees(userID)
	if role == "taker" {
		return t
	}
	return m
}

// ---- GET /account/fee-rates?symbol= ----

type feePair struct {
	Maker string `json:"maker"`
	Taker string `json:"taker"`
}

type feeRatesDTO struct {
	Symbol     string   `json:"symbol,omitempty"`
	VipLevel   int      `json:"vipLevel"`
	Spot       feePair  `json:"spot"`
	Futures    *feePair `json:"futures"`
	Overridden bool     `json:"overridden"`
	Source     string   `json:"source"` // default | override
	Note       string   `json:"note"`
}

func (s *Server) handleFeeRates(w http.ResponseWriter, r *http.Request, userID string) {
	symbol := strings.ToUpper(strings.TrimSpace(r.URL.Query().Get("symbol")))
	m, t, ov := s.userSpotFees(userID)
	out := feeRatesDTO{Symbol: symbol, Spot: feePair{m.String(), t.String()}, Overridden: ov, Source: "default",
		Note: "VIP 等级仅提供服务权益，手续费统一费率"}
	if fut, fov, err := s.futuresFees(r.Context(), userID, symbol); err == nil {
		out.Futures = fut
		out.Overridden = out.Overridden || fov
	} else if !errors.Is(err, errNoFutures) {
		log.Printf("fee-rates: futures: %v", err)
	}
	if out.Overridden {
		out.Source = "override"
	}
	common.WriteJSON(w, http.StatusOK, out)
}

var errNoFutures = errors.New("futures not configured")

func (s *Server) futuresFees(ctx context.Context, userID, symbol string) (*feePair, bool, error) {
	if s.cfg.FuturesInternalURL == "" {
		return nil, false, errNoFutures
	}
	q := url.Values{"userId": {userID}}
	if symbol != "" {
		q.Set("symbol", symbol)
	}
	ctx, cancel := context.WithTimeout(ctx, 3*time.Second)
	defer cancel()
	req, _ := http.NewRequestWithContext(ctx, http.MethodGet, strings.TrimRight(s.cfg.FuturesInternalURL, "/")+"/internal/fee-rates?"+q.Encode(), nil)
	req.Header.Set("X-Internal-Token", s.cfg.InternalToken)
	resp, err := http.DefaultClient.Do(req)
	if err != nil {
		return nil, false, err
	}
	defer resp.Body.Close()
	if resp.StatusCode != http.StatusOK {
		return nil, false, errors.New("futures fee-rates: " + resp.Status)
	}
	var body struct {
		Maker      string `json:"maker"`
		Taker      string `json:"taker"`
		Overridden bool   `json:"overridden"`
	}
	if err := json.NewDecoder(resp.Body).Decode(&body); err != nil {
		return nil, false, err
	}
	return &feePair{body.Maker, body.Taker}, body.Overridden, nil
}

// ---- admin: per-user spot fee overrides ----

type userFeeDTO struct {
	UserID    string  `json:"userId"`
	Maker     *string `json:"maker"`
	Taker     *string `json:"taker"`
	Note      string  `json:"note"`
	UpdatedBy string  `json:"updatedBy"`
	UpdatedAt string  `json:"updatedAt"`
}

func toUserFeeDTO(uid string, f userFee) userFeeDTO {
	d := userFeeDTO{UserID: uid, Note: f.Note, UpdatedBy: f.UpdatedBy, UpdatedAt: f.UpdatedAt.UTC().Format(time.RFC3339)}
	if f.Maker != nil {
		v := f.Maker.String()
		d.Maker = &v
	}
	if f.Taker != nil {
		v := f.Taker.String()
		d.Taker = &v
	}
	return d
}

func (s *Server) handleListUserFees(w http.ResponseWriter, r *http.Request) {
	if err := s.LoadUserFees(r.Context()); err != nil {
		common.WriteError(w, http.StatusInternalServerError, "INTERNAL", "服务器内部错误")
		return
	}
	uid := r.URL.Query().Get("userId")
	s.ufees.mu.RLock()
	items := []userFeeDTO{}
	for k, f := range s.ufees.m {
		if uid == "" || uid == k {
			items = append(items, toUserFeeDTO(k, f))
		}
	}
	s.ufees.mu.RUnlock()
	common.WriteJSON(w, http.StatusOK, map[string]any{"items": items})
}

type putUserFeeReq struct {
	Maker     string `json:"maker"`
	Taker     string `json:"taker"`
	Note      string `json:"note"`
	UpdatedBy string `json:"updatedBy"`
}

// PUT /internal/admin/fee-overrides/{userId} {maker?, taker?, note, updatedBy}
func (s *Server) handlePutUserFee(w http.ResponseWriter, r *http.Request) {
	uid := r.PathValue("userId")
	var req putUserFeeReq
	if uid == "" || common.DecodeJSON(r, &req) != nil {
		common.WriteError(w, http.StatusBadRequest, "VALIDATION_ERROR", "请求参数不合法")
		return
	}
	m, ok1 := parseRate(req.Maker)
	t, ok2 := parseRate(req.Taker)
	if !ok1 || !ok2 || (m == nil && t == nil) {
		common.WriteError(w, http.StatusBadRequest, "VALIDATION_ERROR", "费率需在 [0, 0.1) 之间")
		return
	}
	var ms, ts any
	if m != nil {
		ms = m.String()
	}
	if t != nil {
		ts = t.String()
	}
	if _, err := s.store.db.ExecContext(r.Context(), `INSERT INTO spot_fee_overrides (user_id, maker, taker, note, updated_by)
		VALUES ($1,$2,$3,$4,$5) ON CONFLICT (user_id) DO UPDATE SET maker=EXCLUDED.maker, taker=EXCLUDED.taker,
		note=EXCLUDED.note, updated_by=EXCLUDED.updated_by, updated_at=now()`, uid, ms, ts, req.Note, req.UpdatedBy); err != nil {
		common.WriteError(w, http.StatusInternalServerError, "INTERNAL", "服务器内部错误")
		return
	}
	if err := s.LoadUserFees(r.Context()); err != nil {
		common.WriteError(w, http.StatusInternalServerError, "INTERNAL", "服务器内部错误")
		return
	}
	s.ufees.mu.RLock()
	f := s.ufees.m[uid]
	s.ufees.mu.RUnlock()
	common.WriteJSON(w, http.StatusOK, toUserFeeDTO(uid, f))
}

func (s *Server) handleDeleteUserFee(w http.ResponseWriter, r *http.Request) {
	uid := r.PathValue("userId")
	if _, err := s.store.db.ExecContext(r.Context(), `DELETE FROM spot_fee_overrides WHERE user_id=$1`, uid); err != nil {
		common.WriteError(w, http.StatusInternalServerError, "INTERNAL", "服务器内部错误")
		return
	}
	_ = s.LoadUserFees(r.Context())
	common.WriteJSON(w, http.StatusOK, map[string]string{"status": "deleted"})
}
