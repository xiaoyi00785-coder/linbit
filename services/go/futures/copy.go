package main

// Copy trading (futures only).
//
// A trader is an ordinary futures user with a fut_copy_traders row. Each
// follow owns a sub-account "copy:<follow id>" funded from the follower's
// futures wallet. Every fill that changes an active trader's position records
// a fut_copy_events row in the same transaction; processCopy (run after each
// order, close and tick, under s.mu) mirrors it into each live follow:
//
//   - position grows (open/add): margin = trader margin share of trader equity
//     × follower equity × ratio (mode=ratio) or fixed_margin (mode=fixed),
//     capped by max_per_trade and by what the sub-account can afford;
//     qty = margin × leverage / price, floored to the lot size. Leverage and
//     margin mode follow the trader. Optional TP/SL are ROE ratios.
//   - position shrinks: the follower closes the same fraction (all of it when
//     the trader goes flat or flips, then the flip is opened as a new open).
//
// Profit share: once per day (FUTURES_COPY_SETTLE_HOUR_UTC, default 16:00 UTC
// = 00:00 UTC+8) and on unfollow, realized net of the sub-account (realized
// PnL, fees, funding, liquidation fees) above its high-water mark is charged
// at the trader's rate (0–50%) and paid to the trader's spot account. Losses
// are never charged; earlier losses must be recovered before a new charge.
// The payout is booked as copy_profit_share and is not rebate-eligible.

import (
	"context"
	"database/sql"
	"errors"
	"fmt"
	"log"
	"math"
	"net/http"
	"sort"
	"strconv"
	"strings"
	"time"

	"github.com/shopspring/decimal"

	"github.com/linbit/platform/services/go/common"
)

const maxProfitShare = "0.5"

func copyAcct(id int64) string { return "copy:" + strconv.FormatInt(id, 10) }

// rebate-relevant: none of the copy kinds generate fee rebates; copy_profit_share
// is a transfer between users, not a trading fee.
var copyNetKinds = `('realized_pnl','fee','funding','liquidation_fee','insurance_cover')`

type Trader struct {
	UserID      string          `json:"traderId"`
	Nickname    string          `json:"nickname"`
	Bio         string          `json:"bio"`
	ProfitShare decimal.Decimal `json:"profitShare"`
	Status      string          `json:"status"`
	ReviewNote  string          `json:"reviewNote,omitempty"` // rejection reason
	CreatedAt   time.Time       `json:"createdAt"`
}

type Follow struct {
	ID          int64           `json:"id"`
	Follower    string          `json:"-"`
	Trader      string          `json:"traderId"`
	Status      string          `json:"status"`
	Mode        string          `json:"mode"`
	FixedMargin decimal.Decimal `json:"fixedMargin"`
	Ratio       decimal.Decimal `json:"ratio"`
	MaxPerTrade decimal.Decimal `json:"maxPerTrade"`
	TPRatio     decimal.Decimal `json:"tpRatio"`
	SLRatio     decimal.Decimal `json:"slRatio"`
	Principal   decimal.Decimal `json:"principal"`
	Returned    decimal.Decimal `json:"returned"`
	HWM         decimal.Decimal `json:"-"`
	SharePaid   decimal.Decimal `json:"sharePaid"`
	CreatedAt   time.Time       `json:"createdAt"`
	StoppedAt   *time.Time      `json:"stoppedAt,omitempty"`
}

const followCols = `id, follower, trader, status, mode, fixed_margin, ratio, max_per_trade, tp_ratio, sl_ratio, principal, returned, hwm, share_paid, created_at, stopped_at`

func scanFollow(sc interface{ Scan(...any) error }) (Follow, error) {
	var f Follow
	var st sql.NullTime
	err := sc.Scan(&f.ID, &f.Follower, &f.Trader, &f.Status, &f.Mode, &f.FixedMargin, &f.Ratio, &f.MaxPerTrade, &f.TPRatio, &f.SLRatio, &f.Principal, &f.Returned, &f.HWM, &f.SharePaid, &f.CreatedAt, &st)
	if st.Valid {
		f.StoppedAt = &st.Time
	}
	return f, err
}

func (s *Server) follows(ctx context.Context, where string, args ...any) ([]Follow, error) {
	rows, err := s.db.QueryContext(ctx, `SELECT `+followCols+` FROM fut_copy_follows WHERE `+where+` ORDER BY id`, args...)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	var out []Follow
	for rows.Next() {
		f, err := scanFollow(rows)
		if err != nil {
			return nil, err
		}
		out = append(out, f)
	}
	return out, rows.Err()
}

func (s *Server) loadTraders(ctx context.Context) error {
	rows, err := s.db.QueryContext(ctx, `SELECT user_id FROM fut_copy_traders WHERE status='active'`)
	if err != nil {
		return err
	}
	defer rows.Close()
	m := map[string]bool{}
	for rows.Next() {
		var u string
		if err := rows.Scan(&u); err != nil {
			return err
		}
		m[u] = true
	}
	s.traders = m
	return rows.Err()
}

func (s *Server) getTrader(ctx context.Context, uid string) (Trader, error) {
	var t Trader
	err := s.db.QueryRowContext(ctx, `SELECT user_id, nickname, bio, profit_share, status, review_note, created_at FROM fut_copy_traders WHERE user_id=$1`, uid).
		Scan(&t.UserID, &t.Nickname, &t.Bio, &t.ProfitShare, &t.Status, &t.ReviewNote, &t.CreatedAt)
	if errors.Is(err, sql.ErrNoRows) {
		return t, bad("TRADER_NOT_FOUND", "交易员不存在")
	}
	return t, err
}

func parseShare(v *string, def decimal.Decimal) (decimal.Decimal, error) {
	if v == nil || strings.TrimSpace(*v) == "" {
		return def, nil
	}
	d, err := decimal.NewFromString(strings.TrimSpace(*v))
	if err != nil || d.Sign() < 0 || d.GreaterThan(decimal.RequireFromString(maxProfitShare)) || d.Exponent() < -4 {
		return d, bad("INVALID_PROFIT_SHARE", "分润比例须在 0%–50% 之间")
	}
	return d, nil
}

// ---- trader lifecycle ----

type traderReq struct {
	Nickname    *string `json:"nickname"`
	Bio         *string `json:"bio"`
	ProfitShare *string `json:"profitShare"`
}

func (s *Server) ApplyTrader(ctx context.Context, uid string, r traderReq) (Trader, error) {
	s.mu.Lock()
	defer s.mu.Unlock()
	share, err := parseShare(r.ProfitShare, s.cfg.CopyDefaultShare)
	if err != nil {
		return Trader{}, err
	}
	nick := ""
	if r.Nickname != nil {
		nick = strings.TrimSpace(*r.Nickname)
	}
	if nick == "" || len([]rune(nick)) > 32 {
		return Trader{}, bad("INVALID_NICKNAME", "昵称须为 1–32 个字符")
	}
	bio := ""
	if r.Bio != nil {
		bio = strings.TrimSpace(*r.Bio)
	}
	if len([]rune(bio)) > 280 {
		return Trader{}, bad("INVALID_BIO", "简介最多 280 个字符")
	}
	if t, err := s.getTrader(ctx, uid); err == nil && t.Status != "closed" && t.Status != "rejected" {
		return t, bad("ALREADY_TRADER", "已是交易员或申请审核中")
	}
	st, err := s.state(ctx, s.db, uid)
	if err != nil {
		return Trader{}, err
	}
	if st.Equity.LessThan(s.cfg.CopyTraderMinEquity) {
		return Trader{}, bad("TRADER_EQUITY_TOO_LOW", fmt.Sprintf("合约账户权益须不低于 %s USDT", s.cfg.CopyTraderMinEquity))
	}
	status := "pending"
	if s.cfg.CopyAutoApprove {
		status = "active"
	}
	if _, err := s.db.ExecContext(ctx, `INSERT INTO fut_copy_traders (user_id, nickname, bio, profit_share, status) VALUES ($1,$2,$3,$4,$5)
		ON CONFLICT (user_id) DO UPDATE SET nickname=$2, bio=$3, profit_share=$4, status=$5, review_note='', reviewed_by='', reviewed_at=NULL, created_at=now(), updated_at=now()`, uid, nick, bio, share, status); err != nil {
		return Trader{}, err
	}
	if status == "active" {
		s.traders[uid] = true
	}
	return s.getTrader(ctx, uid)
}

func (s *Server) UpdateTrader(ctx context.Context, uid string, r traderReq) (Trader, error) {
	t, err := s.getTrader(ctx, uid)
	if err != nil {
		return t, err
	}
	if t.Status == "closed" {
		return t, bad("TRADER_CLOSED", "带单已关闭")
	}
	if r.ProfitShare != nil {
		if t.ProfitShare, err = parseShare(r.ProfitShare, t.ProfitShare); err != nil {
			return t, err
		}
	}
	if r.Nickname != nil {
		n := strings.TrimSpace(*r.Nickname)
		if n == "" || len([]rune(n)) > 32 {
			return t, bad("INVALID_NICKNAME", "昵称须为 1–32 个字符")
		}
		t.Nickname = n
	}
	if r.Bio != nil {
		if len([]rune(*r.Bio)) > 280 {
			return t, bad("INVALID_BIO", "简介最多 280 个字符")
		}
		t.Bio = strings.TrimSpace(*r.Bio)
	}
	// A new rate applies from the next settlement onwards.
	_, err = s.db.ExecContext(ctx, `UPDATE fut_copy_traders SET nickname=$2, bio=$3, profit_share=$4, updated_at=now() WHERE user_id=$1`, uid, t.Nickname, t.Bio, t.ProfitShare)
	return t, err
}

func (s *Server) SetTraderStatus(ctx context.Context, uid, status string) error {
	s.mu.Lock()
	defer s.mu.Unlock()
	res, err := s.db.ExecContext(ctx, `UPDATE fut_copy_traders SET status=$2, updated_at=now() WHERE user_id=$1`, uid, status)
	if err != nil {
		return err
	}
	if n, _ := res.RowsAffected(); n == 0 {
		return bad("TRADER_NOT_FOUND", "交易员不存在")
	}
	s.traders[uid] = status == "active"
	if status == "closed" {
		if _, err := s.db.ExecContext(ctx, `UPDATE fut_copy_follows SET status='stopping' WHERE trader=$1 AND status='active'`, uid); err != nil {
			return err
		}
		s.copyHousekeepingLocked(ctx)
	}
	return nil
}

// ---- follow lifecycle ----

type followReq struct {
	TraderID    string `json:"traderId"`
	Amount      string `json:"amount"`
	Mode        string `json:"mode"`
	FixedMargin string `json:"fixedMargin"`
	Ratio       string `json:"ratio"`
	MaxPerTrade string `json:"maxPerTrade"`
	TPRatio     string `json:"tpRatio"`
	SLRatio     string `json:"slRatio"`
}

func nonNeg(v string, def decimal.Decimal, code, msg string) (decimal.Decimal, error) {
	v = strings.TrimSpace(strings.TrimSuffix(strings.TrimSpace(v), "%"))
	if v == "" {
		return def, nil
	}
	d, err := decimal.NewFromString(v)
	if err != nil || d.Sign() < 0 || d.Exponent() < -8 {
		return def, bad(code, msg)
	}
	return d, nil
}

func (s *Server) applyParams(f *Follow, r followReq, create bool) error {
	var err error
	if r.Mode != "" || create {
		m := strings.ToLower(strings.TrimSpace(r.Mode))
		if m == "" {
			m = "ratio"
		}
		if m != "ratio" && m != "fixed" {
			return bad("INVALID_COPY_MODE", "跟单模式须为 ratio 或 fixed")
		}
		f.Mode = m
	}
	if f.FixedMargin, err = nonNeg(r.FixedMargin, f.FixedMargin, "INVALID_FIXED_MARGIN", "固定保证金不合法"); err != nil {
		return err
	}
	if f.Ratio, err = nonNeg(r.Ratio, f.Ratio, "INVALID_RATIO", "跟单比例不合法"); err != nil {
		return err
	}
	if f.MaxPerTrade, err = nonNeg(r.MaxPerTrade, f.MaxPerTrade, "INVALID_MAX_PER_TRADE", "单笔上限不合法"); err != nil {
		return err
	}
	if f.TPRatio, err = nonNeg(r.TPRatio, f.TPRatio, "INVALID_TP_RATIO", "止盈比例不合法"); err != nil {
		return err
	}
	if f.SLRatio, err = nonNeg(r.SLRatio, f.SLRatio, "INVALID_SL_RATIO", "止损比例不合法"); err != nil {
		return err
	}
	if f.Mode == "fixed" && f.FixedMargin.Sign() <= 0 {
		return bad("INVALID_FIXED_MARGIN", "固定模式须设置每笔保证金")
	}
	if f.Mode == "ratio" && (f.Ratio.Sign() <= 0 || f.Ratio.GreaterThan(s.cfg.CopyMaxRatio)) {
		return bad("INVALID_RATIO", fmt.Sprintf("跟单比例须在 (0, %s] 之间", s.cfg.CopyMaxRatio))
	}
	if f.SLRatio.GreaterThan(dOne) {
		return bad("INVALID_SL_RATIO", "止损比例不能超过 100%")
	}
	return nil
}

func (s *Server) Follow(ctx context.Context, uid string, r followReq) (Follow, error) {
	s.mu.Lock()
	defer s.mu.Unlock()
	if r.TraderID == uid {
		return Follow{}, bad("CANNOT_FOLLOW_SELF", "不能跟随自己")
	}
	t, err := s.getTrader(ctx, r.TraderID)
	if err != nil {
		return Follow{}, err
	}
	if t.Status != "active" {
		return Follow{}, bad("TRADER_NOT_ACTIVE", "该交易员暂不可跟随")
	}
	amt, err := decimal.NewFromString(strings.TrimSpace(r.Amount))
	if err != nil || amt.Exponent() < -8 || amt.LessThan(s.cfg.CopyMinAmount) {
		return Follow{}, bad("INVALID_AMOUNT", fmt.Sprintf("跟单金额最低 %s USDT", s.cfg.CopyMinAmount))
	}
	f := Follow{Follower: uid, Trader: t.UserID, Ratio: dOne}
	if err := s.applyParams(&f, r, true); err != nil {
		return f, err
	}
	err = s.txLocked(ctx, func(tx *sql.Tx) error {
		var n int
		if err := tx.QueryRowContext(ctx, `SELECT count(*) FROM fut_copy_follows WHERE trader=$1 AND status<>'stopped'`, t.UserID).Scan(&n); err != nil {
			return err
		}
		if n >= s.cfg.CopyMaxFollowers {
			return bad("TRADER_FULL", "该交易员跟单人数已满")
		}
		if _, _, err := lockAccount(ctx, tx, uid); err != nil {
			return err
		}
		st, err := s.state(ctx, tx, uid)
		if err != nil {
			return err
		}
		if st.Available.LessThan(amt) {
			return bad("INSUFFICIENT_BALANCE", "合约账户可用余额不足")
		}
		row := tx.QueryRowContext(ctx, `INSERT INTO fut_copy_follows (follower, trader, status, mode, fixed_margin, ratio, max_per_trade, tp_ratio, sl_ratio, principal)
			VALUES ($1,$2,'active',$3,$4,$5,$6,$7,$8,$9) RETURNING `+followCols, uid, t.UserID, f.Mode, f.FixedMargin, f.Ratio, f.MaxPerTrade, f.TPRatio, f.SLRatio, amt)
		if f, err = scanFollow(row); err != nil {
			if strings.Contains(err.Error(), "fut_copy_follows_live") {
				return bad("ALREADY_FOLLOWING", "已在跟随该交易员")
			}
			return err
		}
		ref := "copy-in-" + strconv.FormatInt(f.ID, 10)
		if err := addBalance(ctx, tx, uid, "", "copy_transfer_out", amt.Neg(), ref); err != nil {
			return err
		}
		return addBalance(ctx, tx, copyAcct(f.ID), "", "copy_transfer_in", amt, ref)
	})
	return f, err
}

func (s *Server) UpdateFollow(ctx context.Context, uid string, id int64, r followReq) (Follow, error) {
	s.mu.Lock()
	defer s.mu.Unlock()
	fs, err := s.follows(ctx, `id=$1 AND follower=$2`, id, uid)
	if err != nil {
		return Follow{}, err
	}
	if len(fs) == 0 {
		return Follow{}, bad("FOLLOW_NOT_FOUND", "跟单不存在")
	}
	f := fs[0]
	if f.Status != "active" {
		return f, bad("FOLLOW_NOT_ACTIVE", "跟单已停止")
	}
	if err := s.applyParams(&f, r, false); err != nil {
		return f, err
	}
	_, err = s.db.ExecContext(ctx, `UPDATE fut_copy_follows SET mode=$2, fixed_margin=$3, ratio=$4, max_per_trade=$5, tp_ratio=$6, sl_ratio=$7 WHERE id=$1`,
		f.ID, f.Mode, f.FixedMargin, f.Ratio, f.MaxPerTrade, f.TPRatio, f.SLRatio)
	return f, err
}

// Unfollow closes the mirrored positions, settles the final profit share and
// returns the remaining balance to the follower's futures wallet.
func (s *Server) Unfollow(ctx context.Context, uid string, id int64) (Follow, error) {
	s.mu.Lock()
	defer s.mu.Unlock()
	res, err := s.db.ExecContext(ctx, `UPDATE fut_copy_follows SET status='stopping' WHERE id=$1 AND follower=$2 AND status='active'`, id, uid)
	if err != nil {
		return Follow{}, err
	}
	if n, _ := res.RowsAffected(); n == 0 {
		fs, err := s.follows(ctx, `id=$1 AND follower=$2`, id, uid)
		if err != nil {
			return Follow{}, err
		}
		if len(fs) == 0 {
			return Follow{}, bad("FOLLOW_NOT_FOUND", "跟单不存在")
		}
		if fs[0].Status == "stopped" {
			return fs[0], bad("FOLLOW_NOT_ACTIVE", "跟单已停止")
		}
	}
	s.copyHousekeepingLocked(ctx)
	fs, err := s.follows(ctx, `id=$1`, id)
	if err != nil || len(fs) == 0 {
		return Follow{}, err
	}
	return fs[0], nil
}

// copyHousekeeping finishes stopping follows and runs the daily settlement.
func (s *Server) copyHousekeeping(ctx context.Context) {
	s.copyHousekeepingLocked(ctx)
	now := s.now().UTC()
	cut := time.Date(now.Year(), now.Month(), now.Day(), s.cfg.CopySettleHourUTC, 0, 0, 0, time.UTC)
	if now.Before(cut) {
		cut = cut.AddDate(0, 0, -1)
	}
	// The period that ended at cut, labelled by its (UTC+settle offset) day.
	period := cut.AddDate(0, 0, -1).Format("2006-01-02")
	if s.lastCopySettle != period {
		if err := s.settleAll(ctx, period); err != nil {
			log.Printf("futures: copy settle %s: %v", period, err)
			return
		}
		s.lastCopySettle = period
	}
}

func (s *Server) copyHousekeepingLocked(ctx context.Context) {
	fs, err := s.follows(ctx, `status='stopping'`)
	if err != nil {
		log.Printf("futures: copy stopping: %v", err)
		return
	}
	for _, f := range fs {
		if err := s.finishStop(ctx, f); err != nil {
			log.Printf("futures: copy stop %d: %v", f.ID, err)
		}
	}
}

func (s *Server) finishStop(ctx context.Context, f Follow) error {
	acct := copyAcct(f.ID)
	if err := s.txLocked(ctx, func(tx *sql.Tx) error {
		if _, _, err := lockAccount(ctx, tx, acct); err != nil {
			return err
		}
		return cancelUserOrders(ctx, tx, acct, "", "copy_stopped")
	}); err != nil {
		return err
	}
	ps, err := openPositions(ctx, s.db, acct)
	if err != nil {
		return err
	}
	for _, p := range ps {
		o, _, err := s.closeLocked(ctx, acct, p.Symbol, "copy_stop")
		s.logCopy(ctx, f.ID, 0, p.Symbol, "stop", o, p.Qty.Abs(), err)
	}
	if ps, err = openPositions(ctx, s.db, acct); err != nil || len(ps) > 0 {
		return err // retried on the next tick
	}
	if err := s.settleFollow(ctx, f, "final"); err != nil {
		return err
	}
	return s.txLocked(ctx, func(tx *sql.Tx) error {
		bal, _, err := lockAccount(ctx, tx, acct)
		if err != nil {
			return err
		}
		if _, _, err := lockAccount(ctx, tx, f.Follower); err != nil {
			return err
		}
		ref := "copy-out-" + strconv.FormatInt(f.ID, 10)
		if bal.Sign() > 0 {
			if err := addBalance(ctx, tx, acct, "", "copy_transfer_out", bal.Neg(), ref); err != nil {
				return err
			}
			if err := addBalance(ctx, tx, f.Follower, "", "copy_transfer_in", bal, ref); err != nil {
				return err
			}
		}
		_, err = tx.ExecContext(ctx, `UPDATE fut_copy_follows SET status='stopped', returned=returned+$2, stopped_at=now() WHERE id=$1`, f.ID, dmax(bal, dZero))
		return err
	})
}

// ---- profit share ----

func (s *Server) settleAll(ctx context.Context, period string) error {
	fs, err := s.follows(ctx, `status<>'stopped'`)
	if err != nil {
		return err
	}
	for _, f := range fs {
		if err := s.settleFollow(ctx, f, period); err != nil {
			return err
		}
	}
	return nil
}

func (s *Server) settleFollow(ctx context.Context, f Follow, period string) error {
	acct := copyAcct(f.ID)
	var ref string
	var share decimal.Decimal
	err := s.txLocked(ctx, func(tx *sql.Tx) error {
		var done bool
		if err := tx.QueryRowContext(ctx, `SELECT EXISTS(SELECT 1 FROM fut_copy_settlements WHERE follow_id=$1 AND period=$2)`, f.ID, period).Scan(&done); err != nil || done {
			return err
		}
		if _, _, err := lockAccount(ctx, tx, acct); err != nil {
			return err
		}
		var hwm, rate decimal.Decimal
		if err := tx.QueryRowContext(ctx, `SELECT f.hwm, t.profit_share FROM fut_copy_follows f JOIN fut_copy_traders t ON t.user_id=f.trader WHERE f.id=$1 FOR UPDATE OF f`, f.ID).Scan(&hwm, &rate); err != nil {
			return err
		}
		var net decimal.Decimal
		if err := tx.QueryRowContext(ctx, `SELECT COALESCE(SUM(amount),0) FROM fut_bills WHERE user_id=$1 AND kind IN `+copyNetKinds, acct).Scan(&net); err != nil {
			return err
		}
		share = dZero
		if net.GreaterThan(hwm) {
			share = money(net.Sub(hwm).Mul(rate))
			st, err := s.state(ctx, tx, acct)
			if err != nil {
				return err
			}
			share = dmax(dZero, dmin(share, st.Available))
		}
		if share.Sign() > 0 {
			ref = newRef("cps")
			if err := addBalance(ctx, tx, acct, "", "copy_profit_share", share.Neg(), ref); err != nil {
				return err
			}
			// Paid straight to the trader's spot (funding) account.
			if _, err := tx.ExecContext(ctx, `INSERT INTO fut_transfers (ref, user_id, direction, amount, status, kind) VALUES ($1,$2,'out',$3,'pending','copy_profit_share')`, ref, f.Trader, share); err != nil {
				return err
			}
		}
		if _, err := tx.ExecContext(ctx, `INSERT INTO fut_copy_settlements (follow_id, period, trader, realized, hwm_before, rate, share, ref) VALUES ($1,$2,$3,$4,$5,$6,$7,NULLIF($8,''))`,
			f.ID, period, f.Trader, net, hwm, rate, share, ref); err != nil {
			return err
		}
		_, err := tx.ExecContext(ctx, `UPDATE fut_copy_follows SET hwm=GREATEST(hwm,$2), share_paid=share_paid+$3 WHERE id=$1`, f.ID, net, share)
		return err
	})
	if err == nil && ref != "" {
		s.completeOut(ctx, ref, f.Trader, "spot", share)
	}
	return err
}

// ---- mirroring ----

type copyEvent struct {
	ID             int64
	Trader, Symbol string
	Old, New, Px   decimal.Decimal
}

func (s *Server) processCopy(ctx context.Context) {
	for round := 0; round < 20; round++ {
		rows, err := s.db.QueryContext(ctx, `SELECT id, trader, symbol, old_qty, new_qty, price FROM fut_copy_events WHERE NOT processed ORDER BY id LIMIT 100`)
		if err != nil {
			log.Printf("futures: copy events: %v", err)
			return
		}
		var evs []copyEvent
		for rows.Next() {
			var e copyEvent
			if rows.Scan(&e.ID, &e.Trader, &e.Symbol, &e.Old, &e.New, &e.Px) == nil {
				evs = append(evs, e)
			}
		}
		rows.Close()
		if len(evs) == 0 {
			return
		}
		for _, e := range evs {
			// Mark first: a follower order never re-emits (sub-accounts are not
			// traders), and a crash mid-way must not double-mirror.
			if _, err := s.db.ExecContext(ctx, `UPDATE fut_copy_events SET processed=true WHERE id=$1`, e.ID); err != nil {
				log.Printf("futures: copy event %d: %v", e.ID, err)
				return
			}
			fs, err := s.follows(ctx, `trader=$1 AND status='active'`, e.Trader)
			if err != nil {
				log.Printf("futures: copy follows: %v", err)
				continue
			}
			for _, f := range fs {
				s.mirror(ctx, f, e)
			}
		}
	}
}

func (s *Server) logCopy(ctx context.Context, fid, eid int64, sym, action string, o Order, qty decimal.Decimal, err error) {
	status, reason := o.Status, ""
	var oid any
	if o.ID != 0 {
		oid = o.ID
	}
	if err != nil {
		status, reason = "failed", err.Error()
		var ae *apiError
		if errors.As(err, &ae) {
			reason = ae.Code
		}
	}
	var ev any
	if eid != 0 {
		ev = eid
	}
	_, _ = s.db.ExecContext(ctx, `INSERT INTO fut_copy_trades (follow_id, event_id, symbol, action, side, qty, order_id, status, reason) VALUES ($1,$2,$3,$4,$5,$6,$7,$8,NULLIF($9,''))`,
		fid, ev, sym, action, o.Side, qty, oid, status, reason)
}

func (s *Server) skipCopy(ctx context.Context, fid, eid int64, sym, action, side, reason string) {
	_, _ = s.db.ExecContext(ctx, `INSERT INTO fut_copy_trades (follow_id, event_id, symbol, action, side, qty, status, reason) VALUES ($1,$2,$3,$4,$5,0,'skipped',$6)`,
		fid, eid, sym, action, side, reason)
}

func (s *Server) mirror(ctx context.Context, f Follow, e copyEvent) {
	c, err := s.contract(e.Symbol)
	if err != nil {
		return
	}
	acct := copyAcct(f.ID)
	p, _, err := loadPosition(ctx, s.db, acct, c.Symbol, false)
	if err != nil {
		return
	}
	// 1) reduce / close
	flip := e.Old.Sign() != 0 && e.New.Sign() != e.Old.Sign()
	if e.Old.Sign() != 0 && p.Qty.Sign() == e.Old.Sign() && (flip || e.New.Abs().LessThan(e.Old.Abs())) {
		side := "sell"
		if p.Qty.Sign() < 0 {
			side = "buy"
		}
		q := p.Qty.Abs()
		if !flip && !e.New.IsZero() {
			frac := e.Old.Abs().Sub(e.New.Abs()).Div(e.Old.Abs())
			q = floorStep(q.Mul(frac), c.MinQty)
		}
		if q.Sign() > 0 {
			o, _, err := s.placeLocked(ctx, acct, PlaceReq{Symbol: c.Symbol, Side: side, Type: "market", Qty: q.String(), ReduceOnly: true, source: "copy"})
			s.logCopy(ctx, f.ID, e.ID, c.Symbol, "close", o, q, err)
		}
	}
	// 2) open / add
	var opened decimal.Decimal
	switch {
	case flip:
		opened = e.New.Abs()
	case e.New.Abs().GreaterThan(e.Old.Abs()):
		opened = e.New.Abs().Sub(e.Old.Abs())
	}
	if opened.Sign() <= 0 {
		return
	}
	side := "buy"
	if e.New.Sign() < 0 {
		side = "sell"
	}
	lev, mode, err := s.settings(ctx, s.db, e.Trader, c)
	if err != nil {
		return
	}
	if p2, _, _ := loadPosition(ctx, s.db, acct, c.Symbol, false); p2.Qty.Sign() != 0 && p2.Qty.Sign() != e.New.Sign() {
		s.skipCopy(ctx, f.ID, e.ID, c.Symbol, "open", side, "OPPOSITE_POSITION")
		return
	}
	// Sync leverage / margin mode from the trader (only possible while the
	// sub-account has nothing open in this symbol; otherwise keep its own).
	if err := s.syncSettings(ctx, acct, c, lev, mode); err != nil {
		if l2, m2, err2 := s.settings(ctx, s.db, acct, c); err2 == nil {
			lev, mode = l2, m2
		}
	}
	st, err := s.state(ctx, s.db, acct)
	if err != nil {
		return
	}
	var margin decimal.Decimal
	if f.Mode == "fixed" {
		margin = f.FixedMargin
	} else {
		tst, err := s.state(ctx, s.db, e.Trader)
		if err != nil || tst.Equity.Sign() <= 0 {
			s.skipCopy(ctx, f.ID, e.ID, c.Symbol, "open", side, "TRADER_EQUITY")
			return
		}
		tm := opened.Mul(e.Px).Div(decimal.NewFromInt(int64(lev)))
		margin = tm.Div(tst.Equity).Mul(st.Equity).Mul(f.Ratio)
	}
	if f.MaxPerTrade.Sign() > 0 {
		margin = dmin(margin, f.MaxPerTrade)
	}
	mark, _ := s.mark(c.Symbol)
	if mark.Sign() <= 0 {
		mark = e.Px
	}
	unit := mark.Mul(dOne.Add(s.cfg.MarketBand))
	perQty := unit.Div(decimal.NewFromInt(int64(lev))).Add(unit.Mul(s.reserveTaker(f.Follower, c)))
	q := floorStep(margin.Mul(decimal.NewFromInt(int64(lev))).Div(e.Px), c.MinQty)
	if afford := floorStep(st.Available.Div(perQty), c.MinQty); afford.LessThan(q) {
		q = afford
	}
	if q.LessThan(c.MinQty) {
		s.skipCopy(ctx, f.ID, e.ID, c.Symbol, "open", side, "AMOUNT_TOO_SMALL")
		return
	}
	req := PlaceReq{Symbol: c.Symbol, Side: side, Type: "market", Qty: q.String(), source: "copy"}
	levD := decimal.NewFromInt(int64(lev))
	sign := decimal.NewFromInt(int64(e.New.Sign()))
	if f.TPRatio.Sign() > 0 {
		req.TP = e.Px.Mul(dOne.Add(sign.Mul(f.TPRatio).Div(levD))).Round(int32(-c.Tick.Exponent())).String()
	}
	if f.SLRatio.Sign() > 0 && f.SLRatio.Div(levD).LessThan(dOne) {
		req.SL = e.Px.Mul(dOne.Sub(sign.Mul(f.SLRatio).Div(levD))).Round(int32(-c.Tick.Exponent())).String()
	}
	o, _, err := s.placeLocked(ctx, acct, req)
	s.logCopy(ctx, f.ID, e.ID, c.Symbol, "open", o, q, err)
}

func floorStep(v, step decimal.Decimal) decimal.Decimal {
	if step.Sign() <= 0 {
		return v
	}
	return v.Div(step).Floor().Mul(step)
}

func (s *Server) syncSettings(ctx context.Context, acct string, c Contract, lev int, mode string) error {
	var busy bool
	if err := s.db.QueryRowContext(ctx, `SELECT EXISTS(SELECT 1 FROM fut_positions WHERE user_id=$1 AND symbol=$2 AND qty<>0)
		OR EXISTS(SELECT 1 FROM fut_orders WHERE user_id=$1 AND symbol=$2 AND status IN ('new','partially_filled'))`, acct, c.Symbol).Scan(&busy); err != nil {
		return err
	}
	if busy {
		return errors.New("busy")
	}
	_, err := s.db.ExecContext(ctx, `INSERT INTO fut_settings (user_id, symbol, leverage, margin_mode) VALUES ($1,$2,$3,$4)
		ON CONFLICT (user_id, symbol) DO UPDATE SET leverage=$3, margin_mode=$4`, acct, c.Symbol, lev, mode)
	return err
}

// ---- stats ----

type TraderStats struct {
	Trader
	Days           int             `json:"days"`
	PnL            decimal.Decimal `json:"pnl"`
	ROI            decimal.Decimal `json:"roi"`
	WinRate        decimal.Decimal `json:"winRate"`
	Trades         int             `json:"closedTrades"`
	MaxDrawdown    decimal.Decimal `json:"maxDrawdown"`
	Followers      int             `json:"followers"`
	MaxFollowers   int             `json:"maxFollowers"`
	AUM            decimal.Decimal `json:"aum"`
	FollowerProfit decimal.Decimal `json:"followerProfit"`
	ShareEarned    decimal.Decimal `json:"profitShareEarned"`
	Equity         decimal.Decimal `json:"equity"`
	Score          float64         `json:"score"`
	Curve          []CurvePoint    `json:"curve,omitempty"`
}

type CurvePoint struct {
	Day string          `json:"day"`
	PnL decimal.Decimal `json:"pnl"`
	Cum decimal.Decimal `json:"cumPnl"`
}

func (s *Server) followerProfit(ctx context.Context, fs []Follow) (aum, profit decimal.Decimal) {
	for _, f := range fs {
		eq := dZero
		if f.Status != "stopped" {
			if st, err := s.state(ctx, s.db, copyAcct(f.ID)); err == nil {
				eq = st.Equity
				aum = aum.Add(eq)
			}
		}
		profit = profit.Add(eq.Add(f.Returned).Sub(f.Principal))
	}
	return
}

func (s *Server) traderStats(ctx context.Context, t Trader, days int, curve bool) (TraderStats, error) {
	ts := TraderStats{Trader: t, Days: days, MaxFollowers: s.cfg.CopyMaxFollowers}
	since := s.now().AddDate(0, 0, -days)
	rows, err := s.db.QueryContext(ctx, `SELECT to_char(date_trunc('day', created_at AT TIME ZONE 'UTC'), 'YYYY-MM-DD'), kind, amount FROM fut_bills
		WHERE user_id=$1 AND created_at >= $2 AND kind IN `+copyNetKinds+` ORDER BY created_at`, t.UserID, since)
	if err != nil {
		return ts, err
	}
	byDay := map[string]decimal.Decimal{}
	var order []string
	wins := 0
	var cum, peak decimal.Decimal
	for rows.Next() {
		var day, kind string
		var amt decimal.Decimal
		if rows.Scan(&day, &kind, &amt) != nil {
			continue
		}
		if _, ok := byDay[day]; !ok {
			order = append(order, day)
		}
		byDay[day] = byDay[day].Add(amt)
		ts.PnL = ts.PnL.Add(amt)
		cum = cum.Add(amt)
		peak = dmax(peak, cum)
		ts.MaxDrawdown = dmax(ts.MaxDrawdown, peak.Sub(cum))
		if kind == "realized_pnl" {
			ts.Trades++
			if amt.Sign() > 0 {
				wins++
			}
		}
	}
	rows.Close()
	st, err := s.state(ctx, s.db, t.UserID)
	if err != nil {
		return ts, err
	}
	ts.Equity = st.Equity
	if base := st.Equity.Sub(ts.PnL); base.Sign() > 0 {
		ts.ROI = ts.PnL.Div(base).Round(6)
	}
	if ts.Trades > 0 {
		ts.WinRate = decimal.NewFromInt(int64(wins)).Div(decimal.NewFromInt(int64(ts.Trades))).Round(4)
	}
	fs, err := s.follows(ctx, `trader=$1`, t.UserID)
	if err != nil {
		return ts, err
	}
	for _, f := range fs {
		if f.Status != "stopped" {
			ts.Followers++
		}
		ts.ShareEarned = ts.ShareEarned.Add(f.SharePaid)
	}
	ts.AUM, ts.FollowerProfit = s.followerProfit(ctx, fs)
	roi, _ := ts.ROI.Float64()
	wr, _ := ts.WinRate.Float64()
	ts.Score = math.Round((0.5*roi+0.3*wr+0.2*math.Log10(1+float64(ts.Followers)))*1e6) / 1e6
	if curve {
		c := dZero
		for _, d := range order {
			c = c.Add(byDay[d])
			ts.Curve = append(ts.Curve, CurvePoint{Day: d, PnL: byDay[d], Cum: c})
		}
	}
	return ts, nil
}

func statDays(r *http.Request) int {
	switch r.URL.Query().Get("days") {
	case "7":
		return 7
	case "90":
		return 90
	}
	return 30
}

// ---- HTTP ----

func (s *Server) copyRoutes(mux *http.ServeMux) {
	mux.HandleFunc("GET /futures/copy/summary", s.hCopySummary)
	mux.HandleFunc("GET /futures/copy/traders", s.hCopyTraders)
	mux.HandleFunc("GET /futures/copy/traders/{id}", s.hCopyTrader)
	mux.HandleFunc("GET /futures/copy/trader", s.user(s.hMyTrader))
	mux.HandleFunc("POST /futures/copy/trader", s.user(s.hApplyTrader))
	mux.HandleFunc("PATCH /futures/copy/trader", s.user(s.hUpdateTrader))
	mux.HandleFunc("POST /futures/copy/trader/close", s.user(s.hCloseTrader))
	mux.HandleFunc("GET /futures/copy/trader/followers", s.user(s.hMyFollowers))
	mux.HandleFunc("GET /futures/copy/follows", s.user(s.hMyFollows))
	mux.HandleFunc("POST /futures/copy/follows", s.user(s.hFollow))
	mux.HandleFunc("PATCH /futures/copy/follows/{id}", s.user(s.hUpdateFollow))
	mux.HandleFunc("DELETE /futures/copy/follows/{id}", s.user(s.hUnfollow))
	mux.HandleFunc("GET /futures/copy/follows/{id}/trades", s.user(s.hFollowTrades))
	mux.HandleFunc("POST /internal/copy/traders/{id}/status", s.internalOnly(s.hTraderStatus))
	mux.HandleFunc("GET /internal/copy/applications", s.internalOnly(s.hTraderApplications))
	mux.HandleFunc("POST /internal/copy/applications/{id}/review", s.internalOnly(s.hTraderReview))
	mux.HandleFunc("POST /internal/dev/copy/settle", s.dev(s.hDevCopySettle))
}

func (s *Server) internalOnly(next http.HandlerFunc) http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		if s.cfg.InternalToken == "" || r.Header.Get("X-Internal-Token") != s.cfg.InternalToken {
			common.WriteError(w, http.StatusUnauthorized, "AUTH_UNAUTHORIZED", "内部令牌无效")
			return
		}
		next(w, r)
	}
}

func (s *Server) activeTraders(ctx context.Context) ([]Trader, error) {
	rows, err := s.db.QueryContext(ctx, `SELECT user_id, nickname, bio, profit_share, status, created_at FROM fut_copy_traders WHERE status='active' ORDER BY created_at`)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	var out []Trader
	for rows.Next() {
		var t Trader
		if err := rows.Scan(&t.UserID, &t.Nickname, &t.Bio, &t.ProfitShare, &t.Status, &t.CreatedAt); err != nil {
			return nil, err
		}
		out = append(out, t)
	}
	return out, rows.Err()
}

func (s *Server) hCopySummary(w http.ResponseWriter, r *http.Request) {
	ctx := r.Context()
	var traders, followers int
	if err := s.db.QueryRowContext(ctx, `SELECT (SELECT count(*) FROM fut_copy_traders WHERE status='active'), (SELECT count(DISTINCT follower) FROM fut_copy_follows)`).Scan(&traders, &followers); err != nil {
		writeErr(w, err)
		return
	}
	fs, err := s.follows(ctx, `true`)
	if err != nil {
		writeErr(w, err)
		return
	}
	aum, profit := s.followerProfit(ctx, fs)
	common.WriteJSON(w, http.StatusOK, map[string]any{"traders": traders, "totalFollowers": followers, "followerProfit": profit, "aum": aum,
		"maxProfitShare": maxProfitShare, "minAmount": s.cfg.CopyMinAmount, "maxFollowers": s.cfg.CopyMaxFollowers, "settleHourUtc": s.cfg.CopySettleHourUTC})
}

func (s *Server) hCopyTraders(w http.ResponseWriter, r *http.Request) {
	ctx := r.Context()
	ts, err := s.activeTraders(ctx)
	if err != nil {
		writeErr(w, err)
		return
	}
	days := statDays(r)
	out := []TraderStats{}
	for _, t := range ts {
		st, err := s.traderStats(ctx, t, days, false)
		if err != nil {
			writeErr(w, err)
			return
		}
		out = append(out, st)
	}
	key := r.URL.Query().Get("sort")
	sort.SliceStable(out, func(i, j int) bool {
		a, b := out[i], out[j]
		switch key {
		case "roi":
			return a.ROI.GreaterThan(b.ROI)
		case "followers":
			return a.Followers > b.Followers
		case "winrate":
			return a.WinRate.GreaterThan(b.WinRate)
		case "aum":
			return a.AUM.GreaterThan(b.AUM)
		}
		return a.Score > b.Score
	})
	lim := limitParam(r, 50, 200)
	if len(out) > lim {
		out = out[:lim]
	}
	common.WriteJSON(w, http.StatusOK, map[string]any{"traders": out, "days": days, "sort": key})
}

func (s *Server) hCopyTrader(w http.ResponseWriter, r *http.Request) {
	ctx := r.Context()
	t, err := s.getTrader(ctx, r.PathValue("id"))
	if err == nil && t.Status == "pending" {
		err = bad("TRADER_NOT_FOUND", "交易员不存在")
	}
	if err != nil {
		writeErr(w, err)
		return
	}
	st, err := s.traderStats(ctx, t, statDays(r), true)
	if err != nil {
		writeErr(w, err)
		return
	}
	ps, err := openPositions(ctx, s.db, t.UserID)
	if err != nil {
		writeErr(w, err)
		return
	}
	pos := []map[string]any{}
	for _, p := range ps {
		side := "long"
		if p.Qty.Sign() < 0 {
			side = "short"
		}
		pos = append(pos, map[string]any{"symbol": p.Symbol, "side": side, "entryPrice": p.Entry, "leverage": p.Leverage, "marginMode": p.Mode, "openedAt": p.UpdatedAt})
	}
	common.WriteJSON(w, http.StatusOK, map[string]any{"trader": st, "positions": pos})
}

func (s *Server) hMyTrader(w http.ResponseWriter, r *http.Request, uid string) {
	t, err := s.getTrader(r.Context(), uid)
	if err != nil {
		writeErr(w, err)
		return
	}
	st, err := s.traderStats(r.Context(), t, statDays(r), true)
	if err != nil {
		writeErr(w, err)
		return
	}
	common.WriteJSON(w, http.StatusOK, st)
}

func (s *Server) hApplyTrader(w http.ResponseWriter, r *http.Request, uid string) {
	var req traderReq
	if !decode(w, r, &req) {
		return
	}
	t, err := s.ApplyTrader(r.Context(), uid, req)
	if err != nil {
		writeErr(w, err)
		return
	}
	common.WriteJSON(w, http.StatusCreated, t)
}

func (s *Server) hUpdateTrader(w http.ResponseWriter, r *http.Request, uid string) {
	var req traderReq
	if !decode(w, r, &req) {
		return
	}
	t, err := s.UpdateTrader(r.Context(), uid, req)
	if err != nil {
		writeErr(w, err)
		return
	}
	common.WriteJSON(w, http.StatusOK, t)
}

func (s *Server) hCloseTrader(w http.ResponseWriter, r *http.Request, uid string) {
	if err := s.SetTraderStatus(r.Context(), uid, "closed"); err != nil {
		writeErr(w, err)
		return
	}
	common.WriteJSON(w, http.StatusOK, map[string]string{"status": "closed"})
}

func (s *Server) hMyFollowers(w http.ResponseWriter, r *http.Request, uid string) {
	fs, err := s.follows(r.Context(), `trader=$1 AND status<>'stopped'`, uid)
	if err != nil {
		writeErr(w, err)
		return
	}
	out := []map[string]any{}
	for _, f := range fs {
		eq := dZero
		if st, err := s.state(r.Context(), s.db, copyAcct(f.ID)); err == nil {
			eq = st.Equity
		}
		// Follower identity is masked; only the follow id is shown.
		out = append(out, map[string]any{"followId": f.ID, "principal": f.Principal, "equity": eq, "sharePaid": f.SharePaid, "since": f.CreatedAt})
	}
	common.WriteJSON(w, http.StatusOK, map[string]any{"followers": out})
}

func (s *Server) followView(ctx context.Context, f Follow) map[string]any {
	acct := copyAcct(f.ID)
	st, _ := s.state(ctx, s.db, acct)
	eq := dZero
	if f.Status != "stopped" {
		eq = st.Equity
	}
	nick := ""
	_ = s.db.QueryRowContext(ctx, `SELECT nickname FROM fut_copy_traders WHERE user_id=$1`, f.Trader).Scan(&nick)
	var unsettled decimal.Decimal
	_ = s.db.QueryRowContext(ctx, `SELECT COALESCE(SUM(amount),0) FROM fut_bills WHERE user_id=$1 AND kind IN `+copyNetKinds, acct).Scan(&unsettled)
	ps, _ := openPositions(ctx, s.db, acct)
	pos := []map[string]any{}
	for _, p := range ps {
		pos = append(pos, map[string]any{"symbol": p.Symbol, "qty": p.Qty, "entryPrice": p.Entry, "leverage": p.Leverage, "marginMode": p.Mode, "tpPrice": p.TP, "slPrice": p.SL})
	}
	return map[string]any{"follow": f, "traderNickname": nick, "equity": eq, "available": st.Available,
		"pnl": eq.Add(f.Returned).Sub(f.Principal), "realizedNet": unsettled, "positions": pos}
}

func (s *Server) hMyFollows(w http.ResponseWriter, r *http.Request, uid string) {
	where := `follower=$1 AND status<>'stopped'`
	if r.URL.Query().Get("status") == "all" {
		where = `follower=$1`
	}
	fs, err := s.follows(r.Context(), where, uid)
	if err != nil {
		writeErr(w, err)
		return
	}
	out := []map[string]any{}
	for _, f := range fs {
		out = append(out, s.followView(r.Context(), f))
	}
	common.WriteJSON(w, http.StatusOK, map[string]any{"follows": out})
}

func (s *Server) hFollow(w http.ResponseWriter, r *http.Request, uid string) {
	var req followReq
	if !decode(w, r, &req) {
		return
	}
	f, err := s.Follow(r.Context(), uid, req)
	if err != nil {
		writeErr(w, err)
		return
	}
	common.WriteJSON(w, http.StatusCreated, s.followView(r.Context(), f))
}

func pathID(r *http.Request) (int64, error) {
	id, err := strconv.ParseInt(r.PathValue("id"), 10, 64)
	if err != nil {
		return 0, bad("FOLLOW_NOT_FOUND", "跟单不存在")
	}
	return id, nil
}

func (s *Server) hUpdateFollow(w http.ResponseWriter, r *http.Request, uid string) {
	id, err := pathID(r)
	if err != nil {
		writeErr(w, err)
		return
	}
	var req followReq
	if !decode(w, r, &req) {
		return
	}
	f, err := s.UpdateFollow(r.Context(), uid, id, req)
	if err != nil {
		writeErr(w, err)
		return
	}
	common.WriteJSON(w, http.StatusOK, s.followView(r.Context(), f))
}

func (s *Server) hUnfollow(w http.ResponseWriter, r *http.Request, uid string) {
	id, err := pathID(r)
	if err != nil {
		writeErr(w, err)
		return
	}
	f, err := s.Unfollow(r.Context(), uid, id)
	if err != nil {
		writeErr(w, err)
		return
	}
	common.WriteJSON(w, http.StatusOK, s.followView(r.Context(), f))
}

func (s *Server) hFollowTrades(w http.ResponseWriter, r *http.Request, uid string) {
	id, err := pathID(r)
	if err != nil {
		writeErr(w, err)
		return
	}
	rows, err := s.db.QueryContext(r.Context(), `SELECT t.event_id, t.symbol, t.action, t.side, t.qty, t.order_id, t.status, COALESCE(t.reason,''), t.created_at
		FROM fut_copy_trades t JOIN fut_copy_follows f ON f.id=t.follow_id WHERE f.id=$1 AND f.follower=$2 ORDER BY t.id DESC LIMIT $3`, id, uid, limitParam(r, 50, 200))
	if err != nil {
		writeErr(w, err)
		return
	}
	defer rows.Close()
	out := []map[string]any{}
	for rows.Next() {
		var ev, oid sql.NullInt64
		var sym, action, side, st, reason string
		var qty decimal.Decimal
		var at time.Time
		if rows.Scan(&ev, &sym, &action, &side, &qty, &oid, &st, &reason, &at) == nil {
			out = append(out, map[string]any{"eventId": ev.Int64, "symbol": sym, "action": action, "side": side, "qty": qty, "orderId": oid.Int64, "status": st, "reason": reason, "createdAt": at})
		}
	}
	var sets []map[string]any
	srows, err := s.db.QueryContext(r.Context(), `SELECT s.period, s.realized, s.share, s.rate, s.created_at FROM fut_copy_settlements s JOIN fut_copy_follows f ON f.id=s.follow_id WHERE f.id=$1 AND f.follower=$2 ORDER BY s.created_at DESC LIMIT 100`, id, uid)
	if err != nil {
		log.Printf("copy settlements: %v", err)
	} else {
		defer srows.Close()
		for srows.Next() {
			var p string
			var re, sh, rt decimal.Decimal
			var at time.Time
			if srows.Scan(&p, &re, &sh, &rt, &at) == nil {
				sets = append(sets, map[string]any{"period": p, "realizedNet": re, "share": sh, "rate": rt, "createdAt": at})
			}
		}
	}
	common.WriteJSON(w, http.StatusOK, map[string]any{"trades": out, "settlements": sets})
}

func (s *Server) hTraderStatus(w http.ResponseWriter, r *http.Request) {
	var req struct {
		Status string `json:"status"`
	}
	if !decode(w, r, &req) {
		return
	}
	if req.Status != "active" && req.Status != "closed" && req.Status != "pending" {
		writeErr(w, bad("INVALID_STATUS", "状态不合法"))
		return
	}
	if err := s.SetTraderStatus(r.Context(), r.PathValue("id"), req.Status); err != nil {
		writeErr(w, err)
		return
	}
	common.WriteJSON(w, http.StatusOK, map[string]string{"status": req.Status})
}

func (s *Server) hDevCopySettle(w http.ResponseWriter, r *http.Request) {
	var req struct {
		Period string `json:"period"`
	}
	_ = common.DecodeJSON(r, &req)
	if req.Period == "" {
		req.Period = "dev-" + s.now().UTC().Format("20060102T150405.000")
	}
	s.mu.Lock()
	defer s.mu.Unlock()
	if err := s.settleAll(r.Context(), req.Period); err != nil {
		writeErr(w, err)
		return
	}
	common.WriteJSON(w, http.StatusOK, map[string]string{"period": req.Period})
}

// ---- manual review of trader applications (admin console) ----

// GET /internal/copy/applications?status=pending|active|rejected|closed|all&page=&pageSize=
func (s *Server) hTraderApplications(w http.ResponseWriter, r *http.Request) {
	q := r.URL.Query()
	status := q.Get("status")
	if status == "" {
		status = "pending"
	}
	page, _ := strconv.Atoi(q.Get("page"))
	size, _ := strconv.Atoi(q.Get("pageSize"))
	if page < 1 {
		page = 1
	}
	if size < 1 || size > 100 {
		size = 20
	}
	where, args := "", []any{}
	if status != "all" {
		where, args = "WHERE status=$1", append(args, status)
	}
	var total int
	if err := s.db.QueryRowContext(r.Context(), `SELECT count(*) FROM fut_copy_traders `+where, args...).Scan(&total); err != nil {
		writeErr(w, err)
		return
	}
	args = append(args, size, (page-1)*size)
	n := len(args)
	rows, err := s.db.QueryContext(r.Context(), fmt.Sprintf(`SELECT user_id, nickname, bio, profit_share, status, review_note, reviewed_by, reviewed_at, created_at
		FROM fut_copy_traders %s ORDER BY created_at DESC LIMIT $%d OFFSET $%d`, where, n-1, n), args...)
	if err != nil {
		writeErr(w, err)
		return
	}
	type app struct {
		Trader
		ReviewedBy string     `json:"reviewedBy"`
		ReviewedAt *time.Time `json:"reviewedAt"`
		Equity     string     `json:"equity"`
	}
	items := []app{}
	for rows.Next() {
		var a app
		if err := rows.Scan(&a.UserID, &a.Nickname, &a.Bio, &a.ProfitShare, &a.Status, &a.ReviewNote, &a.ReviewedBy, &a.ReviewedAt, &a.CreatedAt); err != nil {
			rows.Close()
			writeErr(w, err)
			return
		}
		items = append(items, a)
	}
	rows.Close()
	for i := range items {
		if st, err := s.state(r.Context(), s.db, items[i].UserID); err == nil {
			items[i].Equity = money(st.Equity).String()
		}
	}
	common.WriteJSON(w, http.StatusOK, map[string]any{"items": items, "total": total, "page": page, "pageSize": size})
}

// POST /internal/copy/applications/{id}/review {decision: approve|reject, reason, reviewer}
// Only pending applications can be decided; a rejection needs a reason.
func (s *Server) hTraderReview(w http.ResponseWriter, r *http.Request) {
	var req struct {
		Decision string `json:"decision"`
		Reason   string `json:"reason"`
		Reviewer string `json:"reviewer"`
	}
	if !decode(w, r, &req) {
		return
	}
	req.Reason = strings.TrimSpace(req.Reason)
	status := map[string]string{"approve": "active", "reject": "rejected"}[req.Decision]
	if status == "" {
		writeErr(w, bad("INVALID_DECISION", "decision 必须为 approve 或 reject"))
		return
	}
	if status == "rejected" && req.Reason == "" {
		writeErr(w, bad("REASON_REQUIRED", "驳回必须填写原因"))
		return
	}
	uid := r.PathValue("id")
	s.mu.Lock()
	res, err := s.db.ExecContext(r.Context(), `UPDATE fut_copy_traders SET status=$2, review_note=$3, reviewed_by=$4, reviewed_at=now(), updated_at=now()
		WHERE user_id=$1 AND status='pending'`, uid, status, req.Reason, req.Reviewer)
	if err == nil {
		if n, _ := res.RowsAffected(); n == 1 && status == "active" {
			s.traders[uid] = true
		} else if n == 0 {
			err = errNotPending
		}
	}
	s.mu.Unlock()
	if errors.Is(err, errNotPending) {
		if _, e := s.getTrader(r.Context(), uid); e != nil {
			writeErr(w, e)
			return
		}
		common.WriteError(w, http.StatusConflict, "NOT_PENDING", "该申请不在待审核状态")
		return
	}
	if err != nil {
		writeErr(w, err)
		return
	}
	t, err := s.getTrader(r.Context(), uid)
	if err != nil {
		writeErr(w, err)
		return
	}
	common.WriteJSON(w, http.StatusOK, t)
}

var errNotPending = errors.New("not pending")
