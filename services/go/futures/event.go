package main

import (
	"bytes"
	"context"
	"database/sql"
	"encoding/json"
	"errors"
	"fmt"
	"log"
	"net/http"
	"regexp"
	"sort"
	"strconv"
	"strings"
	"sync"
	"time"

	"github.com/linbit/platform/services/go/common"
	"github.com/shopspring/decimal"
)

// event.go: event contracts (binary up/down predictions on the index price).
//
// Rules (approved): periods 5m/15m/30m/1h/4h/1d (admin list); BTC/ETH/SOL/BNB/
// TRX USDT; win = stake * (1 + payoutRatio) (default 0.8), lose = stake, tie
// (expiry index == open index) = refund; stake 5-1000 USDT, 10000 USDT per
// user per UTC day; open price = index at order time, settle price = first
// index sample at/after expiry (same Binance/OKX index as futures); if no
// sample exists within the grace window the stake is refunded. Platform risk:
// |sum(up) - sum(down)| of unsettled stakes per symbol+period is capped
// (default 50000 USDT, admin per symbol) -> EXPOSURE_LIMIT.
//
// Money: user free <-> system:event in the asset ledger via
// /internal/system-transfer with refs event:stake:<id> / event:payout:<id>.
// Both refs are claimed once by the asset service, and the order row walks
// pending -> open -> settling -> settled, so a crash at any point is resumed
// by RunEvents and nothing is paid twice (exactly-once).

var (
	evSymbolRe  = regexp.MustCompile(`^[A-Z0-9]{2,15}USDT$`)
	evPeriodRe  = regexp.MustCompile(`^([1-9][0-9]{0,3})([mhd])$`)
	errEvIndex  = &apiError{http.StatusServiceUnavailable, "INDEX_UNAVAILABLE", "指数价格暂不可用，请稍后再试"}
	errEvFeat   = &apiError{http.StatusForbidden, "FEATURE_DISABLED", "事件合约暂未开放"}
	errEvExpo   = bad("EXPOSURE_LIMIT", "该方向下单量已达上限，请稍后再试或选择另一方向")
	errEvPaused = bad("SYMBOL_PAUSED", "该标的暂停交易")
)

// parsePeriod accepts Nm/Nh/Nd between 1 minute and 7 days.
func parsePeriod(p string) (time.Duration, bool) {
	m := evPeriodRe.FindStringSubmatch(p)
	if m == nil {
		return 0, false
	}
	n, _ := strconv.Atoi(m[1])
	unit := map[string]time.Duration{"m": time.Minute, "h": time.Hour, "d": 24 * time.Hour}[m[2]]
	d := time.Duration(n) * unit
	return d, d >= time.Minute && d <= 7*24*time.Hour
}

type evConfig struct {
	Periods        []string        `json:"periods"`
	PayoutRatio    decimal.Decimal `json:"payoutRatio"`
	MinStake       decimal.Decimal `json:"minStake"`
	MaxStake       decimal.Decimal `json:"maxStake"`
	DailyLimit     decimal.Decimal `json:"dailyLimit"`
	ExposureLimit  decimal.Decimal `json:"exposureLimit"`
	SettleGraceSec int             `json:"settleGraceSeconds"`
	UpdatedBy      string          `json:"updatedBy"`
	UpdatedAt      time.Time       `json:"updatedAt"`
}

func (c evConfig) validate() error {
	if len(c.Periods) == 0 || len(c.Periods) > 20 {
		return bad("VALIDATION_ERROR", "periods 至少 1 个、最多 20 个")
	}
	seen := map[string]bool{}
	for _, p := range c.Periods {
		if _, ok := parsePeriod(p); !ok || seen[p] {
			return bad("VALIDATION_ERROR", "周期不合法或重复: "+p+"（格式 5m/1h/1d，1 分钟到 7 天）")
		}
		seen[p] = true
	}
	switch {
	case c.PayoutRatio.Sign() <= 0 || c.PayoutRatio.GreaterThan(decimal.NewFromInt(10)):
		return bad("VALIDATION_ERROR", "payoutRatio 需在 (0, 10]")
	case c.MinStake.Sign() <= 0 || c.MaxStake.LessThan(c.MinStake):
		return bad("VALIDATION_ERROR", "minStake/maxStake 不合法")
	case c.DailyLimit.LessThan(c.MaxStake):
		return bad("VALIDATION_ERROR", "dailyLimit 不能小于 maxStake")
	case c.ExposureLimit.Sign() <= 0:
		return bad("VALIDATION_ERROR", "exposureLimit 需大于 0")
	case c.SettleGraceSec < 5 || c.SettleGraceSec > 3600:
		return bad("VALIDATION_ERROR", "settleGraceSeconds 需在 5-3600")
	}
	return nil
}

type evSymbol struct {
	Symbol        string              `json:"symbol"`
	Paused        bool                `json:"paused"`
	ExposureLimit decimal.NullDecimal `json:"exposureLimit"` // null = global default
	Sort          int                 `json:"-"`
	UpdatedBy     string              `json:"updatedBy"`
	UpdatedAt     time.Time           `json:"updatedAt"`
}

// ensureEventConfig seeds ev_config from env on first start (later edits are
// made in the admin and win over env).
func (s *Server) ensureEventConfig(ctx context.Context) error {
	periods := splitList(common.Env("EVENT_PERIODS", "5m,15m,30m,1h,4h,1d"))
	c := evConfig{Periods: periods, PayoutRatio: decEnv("EVENT_PAYOUT_RATIO", "0.8"), MinStake: decEnv("EVENT_MIN_STAKE", "5"),
		MaxStake: decEnv("EVENT_MAX_STAKE", "1000"), DailyLimit: decEnv("EVENT_DAILY_LIMIT", "10000"),
		ExposureLimit: decEnv("EVENT_EXPOSURE_LIMIT", "50000"), SettleGraceSec: common.EnvInt("EVENT_SETTLE_GRACE_SECONDS", 120)}
	if err := c.validate(); err != nil {
		return fmt.Errorf("event config env: %w", err)
	}
	_, err := s.db.ExecContext(ctx, `INSERT INTO ev_config (id, periods, payout_ratio, min_stake, max_stake, daily_limit, exposure_limit, settle_grace_s)
		VALUES (1,$1,$2,$3,$4,$5,$6,$7) ON CONFLICT (id) DO NOTHING`, textArray(c.Periods), c.PayoutRatio, c.MinStake, c.MaxStake, c.DailyLimit, c.ExposureLimit, c.SettleGraceSec)
	return err
}

func splitList(v string) []string {
	out := []string{}
	for _, p := range strings.Split(v, ",") {
		if p = strings.TrimSpace(p); p != "" {
			out = append(out, p)
		}
	}
	return out
}

// textArray renders a Postgres text[] literal (values are validated tokens).
func textArray(v []string) string { return "{" + strings.Join(v, ",") + "}" }

func parseTextArray(v string) []string {
	v = strings.Trim(v, "{}")
	if v == "" {
		return []string{}
	}
	return strings.Split(v, ",")
}

func loadEvConfig(ctx context.Context, q queryer) (evConfig, error) {
	var c evConfig
	var periods string
	err := q.QueryRowContext(ctx, `SELECT periods::text, payout_ratio, min_stake, max_stake, daily_limit, exposure_limit, settle_grace_s, updated_by, updated_at FROM ev_config WHERE id=1`).
		Scan(&periods, &c.PayoutRatio, &c.MinStake, &c.MaxStake, &c.DailyLimit, &c.ExposureLimit, &c.SettleGraceSec, &c.UpdatedBy, &c.UpdatedAt)
	c.Periods = parseTextArray(periods)
	return c, err
}

func loadEvSymbols(ctx context.Context, q queryer) ([]evSymbol, error) {
	rows, err := q.QueryContext(ctx, `SELECT symbol, paused, exposure_limit, sort, updated_by, updated_at FROM ev_symbols ORDER BY sort, symbol`)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	out := []evSymbol{}
	for rows.Next() {
		var e evSymbol
		if err := rows.Scan(&e.Symbol, &e.Paused, &e.ExposureLimit, &e.Sort, &e.UpdatedBy, &e.UpdatedAt); err != nil {
			return nil, err
		}
		out = append(out, e)
	}
	return out, rows.Err()
}

// ---- index ----

// eventIndex returns the current index of an event symbol: the futures dev
// override when set (DEV_MODE), else market-data /market/index (Binance with
// OKX backup), the same source futures uses.
func (s *Server) eventIndex(ctx context.Context, sym string) (decimal.Decimal, bool) {
	s.pmu.RLock()
	dev, ok := s.devIndex[sym]
	s.pmu.RUnlock()
	if ok {
		return dev, dev.Sign() > 0
	}
	return s.market.LastPrice(ctx, strings.TrimSuffix(sym, "USDT"))
}

// ---- feature flag (market-data /app/features, cached) ----

type featCache struct {
	mu  sync.Mutex
	at  time.Time
	val bool
	ok  bool
}

var evFeat featCache

func (s *Server) eventEnabled(ctx context.Context) bool {
	if !s.cfg.EventRequireFlag {
		return true
	}
	evFeat.mu.Lock()
	defer evFeat.mu.Unlock()
	if evFeat.ok && s.now().Sub(evFeat.at) < 15*time.Second {
		return evFeat.val
	}
	req, _ := http.NewRequestWithContext(ctx, http.MethodGet, strings.TrimRight(s.cfg.MarketURL, "/")+"/app/features", nil)
	resp, err := (&http.Client{Timeout: 3 * time.Second}).Do(req)
	if err != nil {
		return evFeat.ok && evFeat.val // keep last known value; fail closed if never known
	}
	defer resp.Body.Close()
	var out struct {
		Features map[string]bool `json:"features"`
	}
	if resp.StatusCode != http.StatusOK || json.NewDecoder(resp.Body).Decode(&out) != nil {
		return evFeat.ok && evFeat.val
	}
	evFeat.val, evFeat.ok, evFeat.at = out.Features["eventContracts"], true, s.now()
	return evFeat.val
}

// ---- orders ----

type evOrder struct {
	ID           int64
	UserID       string
	ClientID     string
	Symbol       string
	Period       string
	Direction    string
	Stake        decimal.Decimal
	PayoutRatio  decimal.Decimal
	OpenPrice    decimal.Decimal
	OpenTime     time.Time
	ExpiryTime   time.Time
	Status       string
	Result       sql.NullString
	SettlePrice  decimal.NullDecimal
	Payout       decimal.Decimal
	SettledAt    sql.NullTime
	RejectReason sql.NullString
	CreatedAt    time.Time
}

const evCols = `id, user_id, client_id, symbol, period, direction, stake, payout_ratio, open_price, open_time, expiry_time, status, result, settle_price, payout, settled_at, reject_reason, created_at`

func scanEv(sc interface{ Scan(...any) error }) (evOrder, error) {
	var o evOrder
	err := sc.Scan(&o.ID, &o.UserID, &o.ClientID, &o.Symbol, &o.Period, &o.Direction, &o.Stake, &o.PayoutRatio, &o.OpenPrice,
		&o.OpenTime, &o.ExpiryTime, &o.Status, &o.Result, &o.SettlePrice, &o.Payout, &o.SettledAt, &o.RejectReason, &o.CreatedAt)
	return o, err
}

func (o evOrder) dto(admin bool) map[string]any {
	m := map[string]any{
		"id": strconv.FormatInt(o.ID, 10), "clientId": o.ClientID, "symbol": o.Symbol, "period": o.Period, "direction": o.Direction,
		"stake": o.Stake.StringFixed(2), "payoutRatio": o.PayoutRatio.String(), "openPrice": o.OpenPrice.String(),
		"openTime": o.OpenTime.UnixMilli(), "expiryTime": o.ExpiryTime.UnixMilli(), "status": o.Status,
		"result": nil, "settlePrice": nil, "payout": nil, "pnl": nil, "settledAt": nil,
		"expectedPayout": o.Stake.Mul(decimal.NewFromInt(1).Add(o.PayoutRatio)).StringFixed(2),
	}
	if o.Result.Valid {
		m["result"] = o.Result.String
		m["payout"] = o.Payout.StringFixed(2)
		m["pnl"] = o.Payout.Sub(o.Stake).StringFixed(2)
	}
	if o.SettlePrice.Valid {
		m["settlePrice"] = o.SettlePrice.Decimal.String()
	}
	if o.SettledAt.Valid {
		m["settledAt"] = o.SettledAt.Time.UnixMilli()
	}
	if o.RejectReason.Valid {
		m["rejectReason"] = o.RejectReason.String
	}
	if admin {
		m["userId"] = o.UserID
		m["createdAt"] = o.CreatedAt.UnixMilli()
	}
	return m
}

// outcome decides won/lost/tie for a settled price. Pure; unit tested.
func evOutcome(dir string, open, settle, stake, ratio decimal.Decimal) (string, decimal.Decimal) {
	c := settle.Cmp(open)
	switch {
	case c == 0:
		return "tie", stake
	case (c > 0) == (dir == "up"):
		return "won", money(stake.Mul(decimal.NewFromInt(1).Add(ratio)))
	default:
		return "lost", dZero
	}
}

// exposureBreach reports whether adding stake on dir pushes the net
// imbalance past limit (orders that reduce the imbalance always pass). Pure.
func exposureBreach(up, down, stake decimal.Decimal, dir string, limit decimal.Decimal) bool {
	net := up.Sub(down)
	next := net.Add(stake)
	if dir == "down" {
		next = net.Sub(stake)
	}
	return next.Abs().GreaterThan(limit) && next.Abs().GreaterThan(net.Abs())
}

type evPlaceReq struct {
	Symbol    string `json:"symbol"`
	Period    string `json:"period"`
	Direction string `json:"direction"`
	Amount    string `json:"stake"`
	ClientID  string `json:"clientId"`
}

func (s *Server) placeEvent(ctx context.Context, uid string, req evPlaceReq) (evOrder, bool, error) {
	req.Symbol = strings.ToUpper(strings.TrimSpace(req.Symbol))
	req.Period = strings.ToLower(strings.TrimSpace(req.Period))
	req.Direction = strings.ToLower(strings.TrimSpace(req.Direction))
	req.ClientID = strings.TrimSpace(req.ClientID)
	if !clientIDRe.MatchString(req.ClientID) {
		return evOrder{}, false, bad("VALIDATION_ERROR", "clientId 必填，1-64 位字母数字或 _-:.")
	}
	// Idempotent replay first (even if the config changed since).
	if o, err := scanEv(s.db.QueryRowContext(ctx, `SELECT `+evCols+` FROM ev_orders WHERE user_id=$1 AND client_id=$2`, uid, req.ClientID)); err == nil {
		return o, true, nil
	} else if !errors.Is(err, sql.ErrNoRows) {
		return evOrder{}, false, err
	}
	if req.Direction != "up" && req.Direction != "down" {
		return evOrder{}, false, bad("VALIDATION_ERROR", "direction 需为 up 或 down")
	}
	amt, perr := decimal.NewFromString(strings.TrimSpace(req.Amount))
	if perr != nil || amt.Exponent() < -2 {
		return evOrder{}, false, bad("VALIDATION_ERROR", "amount 不合法（最多 2 位小数）")
	}
	cfg, err := loadEvConfig(ctx, s.db)
	if err != nil {
		return evOrder{}, false, err
	}
	if amt.LessThan(cfg.MinStake) || amt.GreaterThan(cfg.MaxStake) {
		return evOrder{}, false, bad("STAKE_OUT_OF_RANGE", "单笔金额需在 "+cfg.MinStake.String()+"-"+cfg.MaxStake.String()+" USDT")
	}
	dur, okP := parsePeriod(req.Period)
	allowed := false
	for _, p := range cfg.Periods {
		allowed = allowed || p == req.Period
	}
	if !okP || !allowed {
		return evOrder{}, false, bad("PERIOD_NOT_SUPPORTED", "不支持的周期: "+req.Period)
	}
	if !s.eventEnabled(ctx) {
		return evOrder{}, false, errEvFeat
	}
	var o evOrder
	err = s.evTx(ctx, func(tx *sql.Tx) error {
		var paused bool
		var symLimit decimal.NullDecimal
		if err := tx.QueryRowContext(ctx, `SELECT paused, exposure_limit FROM ev_symbols WHERE symbol=$1`, req.Symbol).Scan(&paused, &symLimit); errors.Is(err, sql.ErrNoRows) {
			return bad("SYMBOL_NOT_SUPPORTED", "不支持的标的: "+req.Symbol)
		} else if err != nil {
			return err
		}
		if paused {
			return errEvPaused
		}
		// Serialize per user (daily cap) and per book (exposure).
		if _, err := tx.ExecContext(ctx, `SELECT pg_advisory_xact_lock(hashtext('ev:u:'||$1))`, uid); err != nil {
			return err
		}
		if _, err := tx.ExecContext(ctx, `SELECT pg_advisory_xact_lock(hashtext('ev:b:'||$1||':'||$2))`, req.Symbol, req.Period); err != nil {
			return err
		}
		now := s.now().UTC()
		day := now.Truncate(24 * time.Hour)
		var used decimal.Decimal
		if err := tx.QueryRowContext(ctx, `SELECT COALESCE(sum(stake),0) FROM ev_orders WHERE user_id=$1 AND created_at >= $2 AND status <> 'rejected'`, uid, day).Scan(&used); err != nil {
			return err
		}
		if used.Add(amt).GreaterThan(cfg.DailyLimit) {
			return bad("DAILY_LIMIT_EXCEEDED", "今日下单额度已用 "+used.StringFixed(2)+"/"+cfg.DailyLimit.String()+" USDT")
		}
		var up, down decimal.Decimal
		if err := tx.QueryRowContext(ctx, `SELECT COALESCE(sum(stake) FILTER (WHERE direction='up'),0), COALESCE(sum(stake) FILTER (WHERE direction='down'),0)
			FROM ev_orders WHERE symbol=$1 AND period=$2 AND status IN ('pending','open')`, req.Symbol, req.Period).Scan(&up, &down); err != nil {
			return err
		}
		limit := cfg.ExposureLimit
		if symLimit.Valid {
			limit = symLimit.Decimal
		}
		if exposureBreach(up, down, amt, req.Direction, limit) {
			return errEvExpo
		}
		px, ok := s.eventIndex(ctx, req.Symbol)
		if !ok {
			return errEvIndex
		}
		now = s.now().UTC()
		var err error
		o, err = scanEv(tx.QueryRowContext(ctx, `INSERT INTO ev_orders (user_id, client_id, symbol, period, direction, stake, payout_ratio, open_price, open_time, expiry_time, status)
			VALUES ($1,$2,$3,$4,$5,$6,$7,$8,$9,$10,'pending') RETURNING `+evCols,
			uid, req.ClientID, req.Symbol, req.Period, req.Direction, amt, cfg.PayoutRatio, px, now, now.Add(dur)))
		return err
	})
	if err != nil {
		// Lost an idempotency race: return the winner.
		if strings.Contains(err.Error(), "ev_orders_user_id_client_id_key") {
			o, e2 := scanEv(s.db.QueryRowContext(ctx, `SELECT `+evCols+` FROM ev_orders WHERE user_id=$1 AND client_id=$2`, uid, req.ClientID))
			return o, true, e2
		}
		return evOrder{}, false, err
	}
	o, err = s.fundEvent(ctx, o)
	return o, false, err
}

func (s *Server) evTx(ctx context.Context, fn func(tx *sql.Tx) error) error {
	tx, err := s.db.BeginTx(ctx, nil)
	if err != nil {
		return err
	}
	defer tx.Rollback() //nolint:errcheck
	if err := fn(tx); err != nil {
		return err
	}
	return tx.Commit()
}

// fundEvent debits the stake for a pending order (idempotent on
// event:stake:<id>). Insufficient balance rejects the order; any other error
// leaves it pending for RunEvents to retry.
func (s *Server) fundEvent(ctx context.Context, o evOrder) (evOrder, error) {
	err := s.asset.System(ctx, o.UserID, "USDT", "event", "debit", o.Stake, fmt.Sprintf("event:stake:%d", o.ID))
	switch {
	case err == nil:
		_, err = s.db.ExecContext(ctx, `UPDATE ev_orders SET status='open', updated_at=now() WHERE id=$1 AND status='pending'`, o.ID)
		if err == nil {
			o.Status = "open"
		}
		return o, err
	case errors.Is(err, ErrSpotInsufficient):
		if _, err := s.db.ExecContext(ctx, `UPDATE ev_orders SET status='rejected', reject_reason='INSUFFICIENT_BALANCE', updated_at=now() WHERE id=$1 AND status='pending'`, o.ID); err != nil {
			return o, err
		}
		return o, bad("INSUFFICIENT_BALANCE", "现货 USDT 可用余额不足")
	default:
		log.Printf("event: stake debit %d pending: %v", o.ID, err)
		return o, nil // stays pending; the worker retries with the same ref
	}
}

// ---- worker ----

// RunEvents samples the index and drives pending/open/settling orders. Safe to
// run in several replicas: rows are claimed with FOR UPDATE SKIP LOCKED and
// every money movement is idempotent on its ref.
func (s *Server) RunEvents(ctx context.Context) {
	if err := s.ensureEventConfig(ctx); err != nil {
		log.Printf("event: config: %v", err)
	}
	t := time.NewTicker(time.Second)
	defer t.Stop()
	n := 0
	for {
		s.EventTick(ctx)
		if n++; n%3600 == 0 {
			_, _ = s.db.ExecContext(ctx, `DELETE FROM ev_index_samples WHERE ts < now() - interval '3 days'`)
		}
		select {
		case <-ctx.Done():
			return
		case <-t.C:
		}
	}
}

func (s *Server) EventTick(ctx context.Context) {
	s.sampleEventIndex(ctx)
	s.retryPending(ctx)
	s.settleDue(ctx)
	s.payoutSettling(ctx)
}

func (s *Server) sampleEventIndex(ctx context.Context) {
	syms, err := loadEvSymbols(ctx, s.db)
	if err != nil {
		return
	}
	ts := s.now().UTC().Truncate(time.Second)
	for _, e := range syms {
		if px, ok := s.eventIndex(ctx, e.Symbol); ok {
			_, _ = s.db.ExecContext(ctx, `INSERT INTO ev_index_samples (symbol, ts, price) VALUES ($1,$2,$3) ON CONFLICT DO NOTHING`, e.Symbol, ts, px)
		}
	}
}

func (s *Server) retryPending(ctx context.Context) {
	rows, err := s.db.QueryContext(ctx, `SELECT `+evCols+` FROM ev_orders WHERE status='pending' AND updated_at < now() - interval '3 seconds' ORDER BY id LIMIT 100`)
	if err != nil {
		return
	}
	var list []evOrder
	for rows.Next() {
		if o, err := scanEv(rows); err == nil {
			list = append(list, o)
		}
	}
	rows.Close()
	for _, o := range list {
		_, _ = s.db.ExecContext(ctx, `UPDATE ev_orders SET updated_at=now() WHERE id=$1`, o.ID)
		_, _ = s.fundEvent(ctx, o) // rejection is pushed by pushSettled
	}
}

// settleDue fixes the outcome of expired open orders. The settle price is the
// first index sample in [expiry, expiry+5s]; with none and the grace period
// over, the order is refunded.
func (s *Server) settleDue(ctx context.Context) {
	cfg, err := loadEvConfig(ctx, s.db)
	if err != nil {
		return
	}
	for i := 0; i < 20; i++ {
		done := 0
		err := s.evTx(ctx, func(tx *sql.Tx) error {
			rows, err := tx.QueryContext(ctx, `SELECT `+evCols+` FROM ev_orders WHERE status='open' AND expiry_time <= $1 ORDER BY expiry_time LIMIT 200 FOR UPDATE SKIP LOCKED`, s.now())
			if err != nil {
				return err
			}
			var list []evOrder
			for rows.Next() {
				o, err := scanEv(rows)
				if err != nil {
					rows.Close()
					return err
				}
				list = append(list, o)
			}
			rows.Close()
			for _, o := range list {
				var px decimal.Decimal
				err := tx.QueryRowContext(ctx, `SELECT price FROM ev_index_samples WHERE symbol=$1 AND ts >= $2 AND ts <= $3 ORDER BY ts LIMIT 1`,
					o.Symbol, o.ExpiryTime.Truncate(time.Second), o.ExpiryTime.Add(s.cfg.EventSettleTolerance)).Scan(&px)
				var result string
				var payout decimal.Decimal
				var settle any
				switch {
				case err == nil:
					result, payout = evOutcome(o.Direction, o.OpenPrice, px, o.Stake, o.PayoutRatio)
					settle = px
				case errors.Is(err, sql.ErrNoRows):
					if s.now().Before(o.ExpiryTime.Add(time.Duration(cfg.SettleGraceSec) * time.Second)) {
						continue // the sample may still arrive
					}
					result, payout = "refunded", o.Stake
				default:
					return err
				}
				status := "settling"
				if payout.IsZero() {
					status = "settled"
				}
				if _, err := tx.ExecContext(ctx, `UPDATE ev_orders SET status=$2, result=$3, settle_price=$4, payout=$5, settled_at=CASE WHEN $2='settled' THEN now() END, updated_at=now() WHERE id=$1`,
					o.ID, status, result, settle, payout); err != nil {
					return err
				}
				done++
			}
			return nil
		})
		if err != nil {
			log.Printf("event: settle: %v", err)
			return
		}
		if done == 0 {
			break
		}
	}
}

// payoutSettling credits payouts (idempotent on event:payout:<id>) and
// finalizes the orders.
func (s *Server) payoutSettling(ctx context.Context) {
	rows, err := s.db.QueryContext(ctx, `SELECT `+evCols+` FROM ev_orders WHERE status='settling' ORDER BY id LIMIT 200`)
	if err != nil {
		return
	}
	var list []evOrder
	for rows.Next() {
		if o, err := scanEv(rows); err == nil {
			list = append(list, o)
		}
	}
	rows.Close()
	for _, o := range list {
		if err := s.asset.System(ctx, o.UserID, "USDT", "event", "credit", o.Payout, fmt.Sprintf("event:payout:%d", o.ID)); err != nil {
			log.Printf("event: payout %d: %v (will retry)", o.ID, err)
			continue
		}
		if _, err := s.db.ExecContext(ctx, `UPDATE ev_orders SET status='settled', settled_at=now(), updated_at=now() WHERE id=$1 AND status='settling'`, o.ID); err != nil {
			continue
		}
	}
	s.pushSettled(ctx)
}

// pushSettled sends every newly settled order once over the user WebSocket
// (best effort; the REST order list is the source of truth).
func (s *Server) pushSettled(ctx context.Context) {
	rows, err := s.db.QueryContext(ctx, `UPDATE ev_orders SET pushed=true WHERE status IN ('settled','rejected') AND NOT pushed RETURNING `+evCols)
	if err != nil {
		return
	}
	var list []evOrder
	for rows.Next() {
		if o, err := scanEv(rows); err == nil {
			list = append(list, o)
		}
	}
	rows.Close()
	for _, o := range list {
		s.pushWS(ctx, o.UserID, "event:orders", o.dto(false))
	}
}

func (s *Server) pushWS(ctx context.Context, uid, channel string, data any) {
	if s.cfg.MarketURL == "" || s.cfg.InternalToken == "" {
		return
	}
	body, _ := json.Marshal(map[string]any{"userId": uid, "channel": channel, "data": data})
	req, err := http.NewRequestWithContext(ctx, http.MethodPost, strings.TrimRight(s.cfg.MarketURL, "/")+"/internal/ws/push", bytes.NewReader(body))
	if err != nil {
		return
	}
	req.Header.Set("Content-Type", "application/json")
	req.Header.Set("X-Internal-Token", s.cfg.InternalToken)
	resp, err := (&http.Client{Timeout: 2 * time.Second}).Do(req)
	if err != nil {
		return
	}
	resp.Body.Close()
}

// pushMarks relays the current mark/index/funding of every contract to the
// public WS channel "mark:{SYMBOL}" (market-data /internal/ws/broadcast; it
// re-emits each as "index:{SYMBOL}"), plus "futures-ticker:{SYMBOL}" and
// "futures-kline:{SYMBOL}:{interval}" (livefeed.go). Async, best effort, at
// most one in flight; the ticker's DB reads run off the tick loop.
func (s *Server) pushMarks(ctx context.Context) {
	if s.cfg.MarketURL == "" || s.cfg.InternalToken == "" || !s.markPushing.CompareAndSwap(false, true) {
		return
	}
	now := s.now()
	nf := s.nextFunding(now).UnixMilli()
	frames := make([]map[string]any, 0, len(s.symbols))
	for _, sym := range s.symbols {
		ps := s.snapshot(sym)
		if ps.Mark.Sign() <= 0 {
			continue
		}
		frames = append(frames, map[string]any{"channel": "mark:" + sym, "data": map[string]any{
			"symbol": sym, "markPrice": ps.Mark, "indexPrice": ps.Index,
			"fundingRate": s.predictedRate(ps), "nextFundingTime": nf}})
		s.live.addIndex(sym, ps.Index, now)
	}
	go func() {
		defer s.markPushing.Store(false)
		bctx := context.WithoutCancel(ctx)
		frames = append(frames, s.liveFrames(bctx)...)
		body, _ := json.Marshal(map[string]any{"frames": frames})
		req, err := http.NewRequestWithContext(bctx, http.MethodPost, strings.TrimRight(s.cfg.MarketURL, "/")+"/internal/ws/broadcast", bytes.NewReader(body))
		if err != nil {
			return
		}
		req.Header.Set("Content-Type", "application/json")
		req.Header.Set("X-Internal-Token", s.cfg.InternalToken)
		if resp, err := (&http.Client{Timeout: 2 * time.Second}).Do(req); err == nil {
			resp.Body.Close()
		}
	}()
}

// ---- HTTP ----

func (s *Server) eventRoutes(mux *http.ServeMux) {
	mux.HandleFunc("GET /event/products", s.hEvProducts)
	mux.HandleFunc("POST /event/orders", s.user(s.hEvPlace))
	mux.HandleFunc("GET /event/orders", s.user(s.hEvOrders))
	mux.HandleFunc("GET /event/orders/{id}", s.user(s.hEvOrder))
	mux.HandleFunc("GET /internal/event/config", s.internalOnly(s.hEvAdminConfig))
	mux.HandleFunc("PUT /internal/event/config", s.internalOnly(s.hEvAdminConfigPut))
	mux.HandleFunc("PUT /internal/event/symbols/{symbol}", s.internalOnly(s.hEvAdminSymbolPut))
	mux.HandleFunc("GET /internal/event/orders", s.internalOnly(s.hEvAdminOrders))
	mux.HandleFunc("GET /internal/event/exposure", s.internalOnly(s.hEvAdminExposure))
	mux.HandleFunc("POST /internal/dev/event/tick", s.dev(func(w http.ResponseWriter, r *http.Request) {
		s.EventTick(r.Context())
		common.WriteJSON(w, http.StatusOK, map[string]string{"status": "ok"})
	}))
	// Dev only: pull an open order's expiry to now-agoSeconds so a smoke can
	// settle it without waiting a whole period (ago > grace forces a refund).
	mux.HandleFunc("POST /internal/dev/event/expire", s.dev(func(w http.ResponseWriter, r *http.Request) {
		var in struct {
			ID         int64 `json:"id,string"`
			AgoSeconds int   `json:"agoSeconds"`
		}
		if !decode(w, r, &in) {
			return
		}
		exp := s.now().UTC().Add(-time.Duration(in.AgoSeconds) * time.Second)
		res, err := s.db.ExecContext(r.Context(), `UPDATE ev_orders SET expiry_time=$2, updated_at=now() WHERE id=$1 AND status='open'`, in.ID, exp)
		if err != nil {
			writeErr(w, err)
			return
		}
		if n, _ := res.RowsAffected(); n == 0 {
			writeErr(w, errNotFound)
			return
		}
		common.WriteJSON(w, http.StatusOK, map[string]any{"id": strconv.FormatInt(in.ID, 10), "expiryTime": exp.UnixMilli()})
	}))
}

func (s *Server) hEvProducts(w http.ResponseWriter, r *http.Request) {
	cfg, err := loadEvConfig(r.Context(), s.db)
	if err != nil {
		writeErr(w, err)
		return
	}
	syms, err := loadEvSymbols(r.Context(), s.db)
	if err != nil {
		writeErr(w, err)
		return
	}
	periods := []map[string]any{}
	for _, p := range cfg.Periods {
		d, _ := parsePeriod(p)
		periods = append(periods, map[string]any{"period": p, "seconds": int64(d / time.Second)})
	}
	sort.SliceStable(periods, func(i, j int) bool { return periods[i]["seconds"].(int64) < periods[j]["seconds"].(int64) })
	list := []map[string]any{}
	for _, e := range syms {
		px, ok := s.eventIndex(r.Context(), e.Symbol)
		var pv any
		if ok {
			pv = px.String()
		}
		list = append(list, map[string]any{"symbol": e.Symbol, "baseAsset": strings.TrimSuffix(e.Symbol, "USDT"), "quoteAsset": "USDT",
			"paused": e.Paused, "indexPrice": pv, "priceAvailable": ok})
	}
	common.WriteJSON(w, http.StatusOK, map[string]any{
		"products": list, "periods": periods, "payoutRatio": cfg.PayoutRatio.String(),
		"minStake": cfg.MinStake.String(), "maxStake": cfg.MaxStake.String(), "dailyLimit": cfg.DailyLimit.String(),
		"stakeAsset": "USDT", "tieRule": "refund", "enabled": s.eventEnabled(r.Context()),
	})
}

func (s *Server) hEvPlace(w http.ResponseWriter, r *http.Request, uid string) {
	var req evPlaceReq
	if !decode(w, r, &req) {
		return
	}
	o, replay, err := s.placeEvent(r.Context(), uid, req)
	if err != nil {
		writeErr(w, err)
		return
	}
	out := o.dto(false)
	out["idempotentReplay"] = replay
	common.WriteJSON(w, http.StatusOK, out)
}

func (s *Server) hEvOrders(w http.ResponseWriter, r *http.Request, uid string) {
	q := `SELECT ` + evCols + ` FROM ev_orders WHERE user_id=$1`
	args := []any{uid}
	switch st := r.URL.Query().Get("status"); st {
	case "", "all":
	case "open":
		q += ` AND status IN ('pending','open','settling')`
	case "settled":
		q += ` AND status = 'settled'`
	case "rejected":
		q += ` AND status = 'rejected'`
	default:
		writeErr(w, bad("VALIDATION_ERROR", "status 需为 open|settled|rejected|all"))
		return
	}
	if b, err := strconv.ParseInt(r.URL.Query().Get("before"), 10, 64); err == nil && b > 0 {
		args = append(args, b)
		q += fmt.Sprintf(` AND id < $%d`, len(args))
	}
	if sym := strings.ToUpper(r.URL.Query().Get("symbol")); sym != "" {
		args = append(args, sym)
		q += fmt.Sprintf(` AND symbol = $%d`, len(args))
	}
	lim := limitParam(r, 50, 200)
	q += fmt.Sprintf(` ORDER BY id DESC LIMIT %d`, lim)
	list, err := s.queryEv(r.Context(), q, args...)
	if err != nil {
		writeErr(w, err)
		return
	}
	out := make([]map[string]any, 0, len(list))
	for _, o := range list {
		out = append(out, o.dto(false))
	}
	var used decimal.Decimal
	_ = s.db.QueryRowContext(r.Context(), `SELECT COALESCE(sum(stake),0) FROM ev_orders WHERE user_id=$1 AND created_at >= $2 AND status <> 'rejected'`, uid, s.now().UTC().Truncate(24*time.Hour)).Scan(&used)
	next := ""
	if len(list) == lim {
		next = strconv.FormatInt(list[len(list)-1].ID, 10)
	}
	common.WriteJSON(w, http.StatusOK, map[string]any{"orders": out, "nextBefore": next, "dailyUsed": used.StringFixed(2)})
}

func (s *Server) queryEv(ctx context.Context, q string, args ...any) ([]evOrder, error) {
	rows, err := s.db.QueryContext(ctx, q, args...)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	var list []evOrder
	for rows.Next() {
		o, err := scanEv(rows)
		if err != nil {
			return nil, err
		}
		list = append(list, o)
	}
	return list, rows.Err()
}

func (s *Server) hEvOrder(w http.ResponseWriter, r *http.Request, uid string) {
	id, err := strconv.ParseInt(r.PathValue("id"), 10, 64)
	if err != nil {
		writeErr(w, errNotFound)
		return
	}
	o, err := scanEv(s.db.QueryRowContext(r.Context(), `SELECT `+evCols+` FROM ev_orders WHERE id=$1 AND user_id=$2`, id, uid))
	if errors.Is(err, sql.ErrNoRows) {
		writeErr(w, errNotFound)
		return
	} else if err != nil {
		writeErr(w, err)
		return
	}
	common.WriteJSON(w, http.StatusOK, o.dto(false))
}
