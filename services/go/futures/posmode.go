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

// posmode.go: position mode (ONE_WAY / HEDGE), POST_ONLY helper, and the
// admin-tunable order rules (fut_order_config).

type OrderConfig struct {
	MaxAlgoPerSymbol   int             `json:"maxAlgoOrdersPerSymbol"`
	MaxAmendsPerOrder  int             `json:"maxAmendsPerOrder"`
	TPSLDefaultTrigger string          `json:"tpslDefaultTriggerType"`
	PlanDefaultTrigger string          `json:"planDefaultTriggerType"`
	PlanMaxDeviation   decimal.Decimal `json:"planMaxDeviation"`
	HedgeModeEnabled   bool            `json:"hedgeModeEnabled"`
	PostOnlyEnabled    bool            `json:"postOnlyEnabled"`
}

func defaultOrderConfig() OrderConfig {
	return OrderConfig{MaxAlgoPerSymbol: 10, MaxAmendsPerOrder: 10000, TPSLDefaultTrigger: "MARK", PlanDefaultTrigger: "LAST",
		PlanMaxDeviation: decimal.RequireFromString("0.5"), HedgeModeEnabled: true, PostOnlyEnabled: true}
}

func loadOrderConfig(ctx context.Context, q queryer) (OrderConfig, error) {
	var c OrderConfig
	err := q.QueryRowContext(ctx, `SELECT max_algo_per_symbol, max_amends_per_order, tpsl_default_trigger, plan_default_trigger, plan_max_deviation, hedge_mode_enabled, post_only_enabled FROM fut_order_config WHERE id=1`).
		Scan(&c.MaxAlgoPerSymbol, &c.MaxAmendsPerOrder, &c.TPSLDefaultTrigger, &c.PlanDefaultTrigger, &c.PlanMaxDeviation, &c.HedgeModeEnabled, &c.PostOnlyEnabled)
	if errors.Is(err, sql.ErrNoRows) {
		return defaultOrderConfig(), nil
	}
	return c, err
}

func validTriggerType(t string) bool { return t == "LAST" || t == "MARK" }

func (c OrderConfig) validate() error {
	if c.MaxAlgoPerSymbol < 1 || c.MaxAlgoPerSymbol > 200 {
		return bad("VALIDATION_ERROR", "maxAlgoOrdersPerSymbol 范围 1-200")
	}
	if c.MaxAmendsPerOrder < 1 {
		return bad("VALIDATION_ERROR", "maxAmendsPerOrder 必须大于 0")
	}
	if !validTriggerType(c.TPSLDefaultTrigger) || !validTriggerType(c.PlanDefaultTrigger) {
		return bad("VALIDATION_ERROR", "触发价类型只能是 LAST 或 MARK")
	}
	if c.PlanMaxDeviation.Sign() <= 0 || c.PlanMaxDeviation.GreaterThan(dOne) {
		return bad("VALIDATION_ERROR", "planMaxDeviation 范围 (0,1]")
	}
	return nil
}

func (s *Server) orderConfigRoutes(mux *http.ServeMux) {
	mux.HandleFunc("GET /internal/admin/order-config", s.internalOnly(func(w http.ResponseWriter, r *http.Request) {
		c, err := loadOrderConfig(r.Context(), s.db)
		if err != nil {
			writeErr(w, err)
			return
		}
		common.WriteJSON(w, http.StatusOK, c)
	}))
	mux.HandleFunc("PUT /internal/admin/order-config", s.internalOnly(func(w http.ResponseWriter, r *http.Request) {
		c, err := loadOrderConfig(r.Context(), s.db)
		if err != nil {
			writeErr(w, err)
			return
		}
		if !decode(w, r, &c) { // partial update: absent fields keep current values
			return
		}
		c.TPSLDefaultTrigger, c.PlanDefaultTrigger = strings.ToUpper(c.TPSLDefaultTrigger), strings.ToUpper(c.PlanDefaultTrigger)
		if err := c.validate(); err != nil {
			writeErr(w, err)
			return
		}
		if _, err := s.db.ExecContext(r.Context(), `INSERT INTO fut_order_config (id, max_algo_per_symbol, max_amends_per_order, tpsl_default_trigger, plan_default_trigger, plan_max_deviation, hedge_mode_enabled, post_only_enabled, updated_at)
			VALUES (1,$1,$2,$3,$4,$5,$6,$7,now()) ON CONFLICT (id) DO UPDATE SET max_algo_per_symbol=EXCLUDED.max_algo_per_symbol, max_amends_per_order=EXCLUDED.max_amends_per_order,
			tpsl_default_trigger=EXCLUDED.tpsl_default_trigger, plan_default_trigger=EXCLUDED.plan_default_trigger, plan_max_deviation=EXCLUDED.plan_max_deviation,
			hedge_mode_enabled=EXCLUDED.hedge_mode_enabled, post_only_enabled=EXCLUDED.post_only_enabled, updated_at=now()`,
			c.MaxAlgoPerSymbol, c.MaxAmendsPerOrder, c.TPSLDefaultTrigger, c.PlanDefaultTrigger, c.PlanMaxDeviation, c.HedgeModeEnabled, c.PostOnlyEnabled); err != nil {
			writeErr(w, err)
			return
		}
		common.WriteJSON(w, http.StatusOK, c)
	}))
}

// ---- position mode ----

func positionMode(ctx context.Context, q queryer, uid string) (string, error) {
	var m string
	err := q.QueryRowContext(ctx, `SELECT mode FROM fut_position_mode WHERE user_id=$1`, uid).Scan(&m)
	if errors.Is(err, sql.ErrNoRows) {
		return "ONE_WAY", nil
	}
	return m, err
}

// resolvePosSide validates positionSide against the user's mode. In hedge mode an
// order that reduces its side (sell LONG / buy SHORT) is always reduce-only, and
// reduceOnly=true on an opening order is rejected.
func (s *Server) resolvePosSide(ctx context.Context, q queryer, uid, req, side string, reduceOnly bool) (string, bool, error) {
	req = strings.ToUpper(strings.TrimSpace(req))
	mode, err := positionMode(ctx, q, uid)
	if err != nil {
		return "", false, err
	}
	if mode != "HEDGE" {
		if req != "" && req != "BOTH" {
			return "", false, bad("POSITION_SIDE_MISMATCH", "单向持仓模式下 positionSide 只能为 BOTH")
		}
		return "BOTH", reduceOnly, nil
	}
	return hedgeSide(req, side, reduceOnly)
}

func hedgeSide(req, side string, reduceOnly bool) (string, bool, error) {
	if req != "LONG" && req != "SHORT" {
		return "", false, bad("POSITION_SIDE_MISMATCH", "双向持仓模式下 positionSide 必须为 LONG 或 SHORT")
	}
	closing := (req == "LONG" && side == "sell") || (req == "SHORT" && side == "buy")
	if reduceOnly && !closing {
		return "", false, bad("VALIDATION_ERROR", "双向持仓模式下开仓单不能设为只减仓")
	}
	return req, closing, nil
}

// defaultCloseSide picks the side to close when the caller gave none: BOTH in
// one-way mode, or the only open side in hedge mode.
func (s *Server) defaultCloseSide(ctx context.Context, uid, sym string) (string, error) {
	mode, err := positionMode(ctx, s.db, uid)
	if err != nil || mode != "HEDGE" {
		return "BOTH", err
	}
	var sides []string
	rows, err := s.db.QueryContext(ctx, `SELECT position_side FROM fut_positions WHERE user_id=$1 AND symbol=$2 AND qty<>0 AND position_side<>'BOTH'`, uid, sym)
	if err != nil {
		return "", err
	}
	defer rows.Close()
	for rows.Next() {
		var x string
		if rows.Scan(&x) == nil {
			sides = append(sides, x)
		}
	}
	if len(sides) == 1 {
		return sides[0], nil
	}
	if len(sides) == 0 {
		return "", bad("NO_POSITION", "当前无持仓")
	}
	return "", bad("POSITION_SIDE_REQUIRED", "双向持仓需指定 positionSide（LONG 或 SHORT）")
}

// SetPositionMode switches ONE_WAY <-> HEDGE. Only allowed with no open
// positions, open orders or active TP/SL / plan orders on any contract.
func (s *Server) SetPositionMode(ctx context.Context, uid, mode string) error {
	mode = strings.ToUpper(strings.TrimSpace(mode))
	if mode != "ONE_WAY" && mode != "HEDGE" {
		return bad("VALIDATION_ERROR", "positionMode 必须为 ONE_WAY 或 HEDGE")
	}
	return s.withTx(ctx, func(tx *sql.Tx) error {
		if _, _, err := lockAccount(ctx, tx, uid); err != nil {
			return err
		}
		cfg, err := loadOrderConfig(ctx, tx)
		if err != nil {
			return err
		}
		if mode == "HEDGE" && !cfg.HedgeModeEnabled {
			return bad("HEDGE_MODE_DISABLED", "双向持仓暂未开放")
		}
		if mode == "HEDGE" && s.traders[uid] {
			return bad("COPY_TRADER_ONE_WAY", "带单交易员只能使用单向持仓")
		}
		cur, err := positionMode(ctx, tx, uid)
		if err != nil || cur == mode {
			return err
		}
		var pos, ord, algo int
		if err := tx.QueryRowContext(ctx, `SELECT (SELECT count(*) FROM fut_positions WHERE user_id=$1 AND qty<>0),
			(SELECT count(*) FROM fut_orders WHERE user_id=$1 AND status IN ('new','partially_filled')),
			(SELECT count(*) FROM fut_algo_orders WHERE user_id=$1 AND status='active')`, uid).Scan(&pos, &ord, &algo); err != nil {
			return err
		}
		if pos > 0 {
			return bad("HAS_POSITION", "有持仓时不能切换持仓模式")
		}
		if ord > 0 || algo > 0 {
			return bad("HAS_OPEN_ORDERS", "请先撤销全部挂单和止盈止损/计划委托")
		}
		_, err = tx.ExecContext(ctx, `INSERT INTO fut_position_mode (user_id, mode) VALUES ($1,$2) ON CONFLICT (user_id) DO UPDATE SET mode=EXCLUDED.mode, updated_at=now()`, uid, mode)
		return err
	})
}

func (s *Server) hGetPositionMode(w http.ResponseWriter, r *http.Request, uid string) {
	m, err := positionMode(r.Context(), s.db, uid)
	if err != nil {
		writeErr(w, err)
		return
	}
	common.WriteJSON(w, http.StatusOK, map[string]any{"positionMode": m})
}

func (s *Server) hSetPositionMode(w http.ResponseWriter, r *http.Request, uid string) {
	var req struct {
		Mode string `json:"positionMode"`
	}
	if !decode(w, r, &req) {
		return
	}
	if err := s.SetPositionMode(r.Context(), uid, req.Mode); err != nil {
		writeErr(w, err)
		return
	}
	s.hGetPositionMode(w, r, uid)
}

// wouldTake reports whether a limit order at px on side would cross the book.
func wouldTake(ctx context.Context, q queryer, sym, side string, px decimal.Decimal) (bool, error) {
	opp, cmp := "sell", "<="
	if side == "sell" {
		opp, cmp = "buy", ">="
	}
	var ok bool
	err := q.QueryRowContext(ctx, `SELECT EXISTS(SELECT 1 FROM fut_orders WHERE symbol=$1 AND side=$2 AND type='limit' AND status IN ('new','partially_filled') AND price `+cmp+` $3)`, sym, opp, px).Scan(&ok)
	return ok, err
}

// pickSide normalises an optional positionSide for position-level actions.
func (s *Server) pickSide(ctx context.Context, uid, sym, side string) (string, error) {
	side = strings.ToUpper(strings.TrimSpace(side))
	if side == "" {
		return s.defaultCloseSide(ctx, uid, sym)
	}
	mode, err := positionMode(ctx, s.db, uid)
	if err != nil {
		return "", err
	}
	if (mode == "HEDGE") != (side == "LONG" || side == "SHORT") || (side != "BOTH" && side != "LONG" && side != "SHORT") {
		return "", bad("POSITION_SIDE_MISMATCH", "positionSide 与当前持仓模式不符")
	}
	return side, nil
}
