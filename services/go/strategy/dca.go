package main

// DCA (dollar-cost averaging) plans. A plan is a recurring market order on a
// spot symbol at a fixed cadence: a buy spends a fixed quote amount per tick,
// a sell sells a fixed base amount. The scheduler fires every tick and opens
// one execution row per fired plan; a plan is independent of a grid bot.

import (
	"context"
	"database/sql"
	"errors"
	"fmt"
	"time"

	"github.com/shopspring/decimal"
)

// Valid plan statuses: active plans are scheduled; paused plans are skipped
// but keep their next_run_at (resume re-arms it); stopped plans are terminal
// and are hidden from the default listing.
const (
	planActive  = "active"
	planPaused  = "paused"
	planStopped = "stopped"
)

const (
	execPending  = "pending"
	execFilled   = "filled"
	execCanceled = "canceled"
	execFailed   = "failed"
)

// DCAPlan mirrors the dca_plans row.
type DCAPlan struct {
	ID             int64
	UserID         string
	Symbol         string
	Base, Quote    string
	Side           string
	AmountPerRun   decimal.Decimal
	IntervalSec    int
	TriggerType    string
	TriggerPrice   decimal.NullDecimal
	StartAt        time.Time
	EndAt          sql.NullTime
	NextRunAt      time.Time
	Status         string
	StopReason     sql.NullString
	TotalRuns      int
	TotalBase      decimal.Decimal
	TotalQuote     decimal.Decimal
	TotalFeeBase   decimal.Decimal
	TotalFeeQuote  decimal.Decimal
	LastRunAt      sql.NullTime
	CreatedAt      time.Time
	StoppedAt      sql.NullTime
}

const dcaPlanCols = `id,user_id,symbol,base_asset,quote_asset,side,amount_per_run,interval_seconds,trigger_type,
	trigger_price,start_at,end_at,next_run_at,status,stop_reason,total_runs,total_base,total_quote,
	total_fee_base,total_fee_quote,last_run_at,created_at,stopped_at`

func scanDCAPlan(sc interface{ Scan(...any) error }) (DCAPlan, error) {
	var p DCAPlan
	err := sc.Scan(&p.ID, &p.UserID, &p.Symbol, &p.Base, &p.Quote, &p.Side, &p.AmountPerRun, &p.IntervalSec,
		&p.TriggerType, &p.TriggerPrice, &p.StartAt, &p.EndAt, &p.NextRunAt, &p.Status, &p.StopReason,
		&p.TotalRuns, &p.TotalBase, &p.TotalQuote, &p.TotalFeeBase, &p.TotalFeeQuote, &p.LastRunAt,
		&p.CreatedAt, &p.StoppedAt)
	return p, err
}

func (s *Server) dcaPlan(ctx context.Context, id int64) (DCAPlan, error) {
	return scanDCAPlan(s.db.QueryRowContext(ctx, `SELECT `+dcaPlanCols+` FROM dca_plans WHERE id=$1`, id))
}

func (s *Server) dcaPlans(ctx context.Context, where string, args ...any) ([]DCAPlan, error) {
	rows, err := s.db.QueryContext(ctx, `SELECT `+dcaPlanCols+` FROM dca_plans `+where, args...)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	var out []DCAPlan
	for rows.Next() {
		p, err := scanDCAPlan(rows)
		if err != nil {
			return nil, err
		}
		out = append(out, p)
	}
	return out, rows.Err()
}

// ---- create ----

type dcaCreateReq struct {
	Symbol       string  `json:"pair"`
	SymbolAlt    string  `json:"symbol"` // accept both field names
	Side         string  `json:"side"`
	BaseAmount   string  `json:"baseAmount"`
	Interval     int     `json:"intervalSeconds"`
	TriggerType  string  `json:"triggerType"`
	TriggerPrice *string `json:"triggerPrice"`
	StartAt      *string `json:"startAt"`
	EndAt        *string `json:"endAt"`
}

func (r dcaCreateReq) symbol() string {
	if r.Symbol != "" {
		return r.Symbol
	}
	return r.SymbolAlt
}

// CreateDCA validates and inserts a new plan. It does NOT fire the first run;
// the scheduler will pick it up at next_run_at (= startAt rounded forward).
func (s *Server) CreateDCA(ctx context.Context, uid string, r dcaCreateReq) (DCAPlan, error) {
	s.mu.Lock()
	defer s.mu.Unlock()

	sym := r.symbol()
	if sym == "" {
		return DCAPlan{}, bad("INVALID_SYMBOL", "交易对不能为空")
	}
	info, err := s.symbolCall(ctx, sym)
	if err != nil {
		var ae *apiErr
		if errors.As(err, &ae) && ae.Status == 404 {
			return DCAPlan{}, bad("SYMBOL_NOT_FOUND", "交易对不存在")
		}
		return DCAPlan{}, err
	}
	if info.Status != "trading" {
		return DCAPlan{}, bad("SYMBOL_NOT_TRADING", "交易对暂停交易")
	}
	side := r.Side
	if side == "" {
		side = "buy"
	}
	if side != "buy" && side != "sell" {
		return DCAPlan{}, bad("INVALID_SIDE", "方向须为 buy 或 sell")
	}
	amt, err := parsePos(r.BaseAmount, "INVALID_AMOUNT", "单次金额不合法")
	if err != nil {
		return DCAPlan{}, err
	}
	if r.Interval < s.cfg.DCAMinInterval {
		return DCAPlan{}, bad("INVALID_INTERVAL", fmt.Sprintf("间隔须 >= %d 秒", s.cfg.DCAMinInterval))
	}
	trig := r.TriggerType
	if trig == "" {
		trig = "time"
	}
	if trig != "time" && trig != "price" {
		return DCAPlan{}, bad("INVALID_TRIGGER", "触发方式须为 time 或 price")
	}
	var trigPrice decimal.NullDecimal
	if trig == "price" {
		if r.TriggerPrice == nil || *r.TriggerPrice == "" {
			return DCAPlan{}, bad("INVALID_TRIGGER", "价格触发需提供 triggerPrice")
		}
		p, err := parsePos(*r.TriggerPrice, "INVALID_TRIGGER", "触发价不合法")
		if err != nil {
			return DCAPlan{}, err
		}
		trigPrice = decimal.NullDecimal{Decimal: p, Valid: true}
	}
	start := time.Now()
	if r.StartAt != nil && *r.StartAt != "" {
		t, err := parseTime(*r.StartAt)
		if err != nil {
			return DCAPlan{}, bad("INVALID_START", "开始时间不合法")
		}
		start = t
	}
	var end sql.NullTime
	if r.EndAt != nil && *r.EndAt != "" {
		t, err := parseTime(*r.EndAt)
		if err != nil {
			return DCAPlan{}, bad("INVALID_END", "结束时间不合法")
		}
		if !t.After(start) {
			return DCAPlan{}, bad("INVALID_END", "结束时间须晚于开始时间")
		}
		end = sql.NullTime{Time: t, Valid: true}
	}
	// Enforce per-run notional limits based on symbol rules.
	if side == "buy" && amt.LessThan(info.MinNotional) {
		return DCAPlan{}, bad("AMOUNT_TOO_LOW", "单次金额低于最小下单额 "+info.MinNotional.String())
	}
	if side == "sell" && amt.LessThan(info.MinQuantity) {
		return DCAPlan{}, bad("AMOUNT_TOO_LOW", "单次数量低于最小下单量 "+info.MinQuantity.String())
	}
	// Per-user plan cap.
	var n int
	if err := s.db.QueryRowContext(ctx, `SELECT count(*) FROM dca_plans WHERE user_id=$1 AND status<>'stopped'`, uid).Scan(&n); err != nil {
		return DCAPlan{}, err
	}
	if n >= s.cfg.DCAMaxPlansPerUser {
		return DCAPlan{}, bad("TOO_MANY_PLANS", fmt.Sprintf("最多同时运行 %d 个定投", s.cfg.DCAMaxPlansPerUser))
	}

	var id int64
	err = s.db.QueryRowContext(ctx, `INSERT INTO dca_plans (user_id,symbol,base_asset,quote_asset,side,amount_per_run,
		interval_seconds,trigger_type,trigger_price,start_at,end_at,next_run_at,status)
		VALUES ($1,$2,$3,$4,$5,$6,$7,$8,$9,$10,$11,$12,'active') RETURNING id`,
		uid, info.Symbol, info.Base, info.Quote, side, amt, r.Interval, trig, trigPrice, start, end, start).Scan(&id)
	if err != nil {
		return DCAPlan{}, err
	}
	return s.dcaPlan(ctx, id)
}

func parseTime(v string) (time.Time, error) {
	// Accept either RFC3339 or an integer unix milliseconds timestamp.
	if t, err := time.Parse(time.RFC3339, v); err == nil {
		return t, nil
	}
	if t, err := time.Parse(time.RFC3339Nano, v); err == nil {
		return t, nil
	}
	// Last resort: unix seconds / millis.
	var sec int64
	if _, err := fmt.Sscan(v, &sec); err == nil && sec > 0 {
		if sec > 1e12 { // ms
			return time.Unix(sec/1000, (sec%1000)*int64(time.Millisecond)), nil
		}
		return time.Unix(sec, 0), nil
	}
	return time.Time{}, errors.New("bad time")
}

// ---- pause / resume / delete ----

// PauseDCA puts an active plan on hold. A paused plan keeps its stats and
// next_run_at; the scheduler just skips it.
func (s *Server) PauseDCA(ctx context.Context, uid string, id int64) (DCAPlan, error) {
	s.mu.Lock()
	defer s.mu.Unlock()
	p, err := s.ownedDCA(ctx, uid, id)
	if err != nil {
		return DCAPlan{}, err
	}
	if p.Status == planStopped {
		return DCAPlan{}, bad("PLAN_STOPPED", "计划已停止")
	}
	if p.Status == planPaused {
		return p, nil
	}
	if _, err := s.db.ExecContext(ctx, `UPDATE dca_plans SET status='paused' WHERE id=$1`, id); err != nil {
		return DCAPlan{}, err
	}
	return s.dcaPlan(ctx, id)
}

// ResumeDCA re-arms a paused plan. The next tick fires immediately (we clamp
// next_run_at down to now), then the regular cadence picks up from there —
// resume does NOT itself place an order, so there is no double-send on resume.
func (s *Server) ResumeDCA(ctx context.Context, uid string, id int64) (DCAPlan, error) {
	s.mu.Lock()
	defer s.mu.Unlock()
	p, err := s.ownedDCA(ctx, uid, id)
	if err != nil {
		return DCAPlan{}, err
	}
	if p.Status == planStopped {
		return DCAPlan{}, bad("PLAN_STOPPED", "计划已停止")
	}
	if p.Status == planActive {
		return p, nil
	}
	now := s.now()
	next := p.NextRunAt
	if next.After(now) {
		// Paused past the next slot? Fire now, don't wait.
		next = now
	}
	if _, err := s.db.ExecContext(ctx, `UPDATE dca_plans SET status='active', next_run_at=$2 WHERE id=$1`, id, next); err != nil {
		return DCAPlan{}, err
	}
	return s.dcaPlan(ctx, id)
}

// DeleteDCA marks a plan stopped. Executions stay for history; the row is
// hidden from the default listing.
func (s *Server) DeleteDCA(ctx context.Context, uid string, id int64) error {
	s.mu.Lock()
	defer s.mu.Unlock()
	p, err := s.ownedDCA(ctx, uid, id)
	if err != nil {
		return err
	}
	if p.Status == planStopped {
		return nil
	}
	_, err = s.db.ExecContext(ctx, `UPDATE dca_plans SET status='stopped', stopped_at=now(),
		stop_reason=COALESCE(stop_reason,'manual') WHERE id=$1`, id)
	return err
}

func (s *Server) ownedDCA(ctx context.Context, uid string, id int64) (DCAPlan, error) {
	p, err := s.dcaPlan(ctx, id)
	if errors.Is(err, sql.ErrNoRows) || (err == nil && p.UserID != uid) {
		return DCAPlan{}, bad("PLAN_NOT_FOUND", "定投计划不存在")
	}
	return p, err
}

// now is overridable from tests.
func (s *Server) now() time.Time {
	if s.nowFn != nil {
		return s.nowFn()
	}
	return time.Now()
}
