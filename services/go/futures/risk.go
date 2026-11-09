package main

// 每个 tick：刷新价格、推送、采样、资金费、强平、止盈止损、条件单、负余额覆盖、跟单。
// 强平用标记价。逐仓按仓；全仓按该用户全部全仓一起。标记价缺失不强平。

import (
	"context"
	"database/sql"
	"log"
	"time"

	"github.com/shopspring/decimal"
)

// RunTicker 按 TickInterval 调用 Tick。采样间隔不在这里强制，forceSample 固定为 false。
func (s *Server) RunTicker(ctx context.Context) {
	t := time.NewTicker(s.cfg.TickInterval)
	defer t.Stop()
	for {
		s.Tick(ctx, false)
		select {
		case <-ctx.Done():
			return
		case <-t.C:
		}
	}
}

// Tick 先刷新指数/标记价，再在 s.mu 内做资金费、强平、止盈止损、条件单和负余额覆盖。
// forceSample 为真时忽略采样间隔，立刻写标记价与保险基金快照。
func (s *Server) Tick(ctx context.Context, forceSample bool) {
	s.tickMu.Lock()
	defer s.tickMu.Unlock()
	s.refreshPrices(ctx)
	s.pushMarks(ctx)
	s.sampleMarks(ctx, forceSample)
	s.snapshotInsurance(ctx, forceSample)
	s.mu.Lock()
	defer s.mu.Unlock()
	s.fundingDue(ctx)
	s.liquidations(ctx)
	s.triggerTPSL(ctx)
	s.triggerAlgos(ctx)
	s.coverNegative(ctx)
	s.processCopy(ctx)
	s.copyHousekeeping(ctx)
}

func (s *Server) liqFeeRate(c Contract) decimal.Decimal {
	if s.cfg.LiqFeeRate.Sign() >= 0 {
		return s.cfg.LiqFeeRate
	}
	return c.MMR
}

// liquidations 扫所有非 system: 的持仓用户。调用方持有 s.mu。
func (s *Server) liquidations(ctx context.Context) {
	rows, err := s.db.QueryContext(ctx, `SELECT DISTINCT user_id FROM fut_positions WHERE qty <> 0 AND user_id NOT LIKE 'system:%'`)
	if err != nil {
		return
	}
	var users []string
	for rows.Next() {
		var u string
		if rows.Scan(&u) == nil {
			users = append(users, u)
		}
	}
	rows.Close()
	for _, uid := range users {
		if err := s.checkUser(ctx, uid); err != nil {
			log.Printf("futures: liquidation check %s: %v", uid, err)
		}
	}
}

// checkUser：逐仓在 Iso+未实现盈亏 <= 维持保证金时只强平该仓，标记价缺失则跳过该仓。
// 全仓在钱包余额+全部全仓未实现盈亏 <= 全部全仓维持保证金时一起强平；任一全仓标记价缺失则整户跳过。
func (s *Server) checkUser(ctx context.Context, uid string) error {
	st, err := s.state(ctx, s.db, uid)
	if err != nil {
		return err
	}
	for _, v := range st.Positions {
		if v.pos.Mode == "isolated" && !v.markMissing && v.pos.Iso.Add(v.upnl).LessThanOrEqual(v.mm) {
			if err := s.txLocked(ctx, func(tx *sql.Tx) error { return s.liquidateIsolated(ctx, tx, uid, v.pos.Symbol, v.pos.Side, v.mark) }); err != nil {
				return err
			}
		}
	}
	st, err = s.state(ctx, s.db, uid)
	if err != nil {
		return err
	}
	if st.CrossMM.Sign() > 0 && st.Balance.Add(st.CrossUPnL).LessThanOrEqual(st.CrossMM) {
		for _, v := range st.Positions {
			if v.pos.Mode == "cross" && v.markMissing {
				return nil // never liquidate on a stale price
			}
		}
		return s.txLocked(ctx, func(tx *sql.Tx) error { return s.liquidateCross(ctx, tx, uid) })
	}
	return nil
}

// takeover 按 px 平掉 uid 的仓位。px 不等于标记价（破产且基金盖不住）时先 ADL，
// 剩下的仓位与 system:insurance 对倒。历史状态记 liquidated。
func (s *Server) takeover(ctx context.Context, tx *sql.Tx, uid, posSide string, c Contract, mark, px decimal.Decimal) (Position, error) {
	s.histStatus = "liquidated"
	defer func() { s.histStatus = "" }()
	p, _, err := loadPosition(ctx, tx, uid, c.Symbol, posSide, true)
	if err != nil || p.Qty.IsZero() {
		return p, err
	}
	rem := p.Qty.Abs()
	if !px.Equal(mark) {
		if rem, err = s.deleverage(ctx, tx, uid, c, p, px, mark); err != nil {
			return p, err
		}
	}
	if rem.Sign() > 0 {
		d := rem.Mul(decimal.NewFromInt(int64(p.Qty.Sign())))
		if _, _, _, err := s.fill(ctx, tx, c, uid, p.Side, d.Neg(), px, dZero, p.Leverage, p.Mode, decimal.NullDecimal{}, decimal.NullDecimal{}, "", ""); err != nil {
			return p, err
		}
		if _, _, _, err := s.fill(ctx, tx, c, insuranceUser, "BOTH", d, px, dZero, 1, "cross", decimal.NullDecimal{}, decimal.NullDecimal{}, "", ""); err != nil {
			return p, err
		}
	}
	side := "sell"
	if p.Qty.Sign() < 0 {
		side = "buy"
	}
	_, err = tx.ExecContext(ctx, `INSERT INTO fut_orders (user_id, symbol, side, type, price, qty, filled, filled_quote, status, reduce_only, leverage, margin_mode, source, position_side)
		VALUES ($1,$2,$3,'market',$4,$5,$5,$6,'filled',true,$7,$8,'liquidation',$9)`, uid, c.Symbol, side, px, p.Qty.Abs(), p.Qty.Abs().Mul(px), p.Leverage, p.Mode, p.Side)
	return p, err
}

// liquidateIsolated 先撤该合约挂单。权益 = Iso + 按标记价的未实现盈亏。
// ADL 开启且权益为负时，成交价改为破产价（按保险基金盖不住的缺口偏移），否则按标记价接管。
// 平仓退回的正权益里，最多扣 |qty|*mark*强平费率（FUTURES_LIQUIDATION_FEE_RATE 为空则用合约 MMR）给保险基金；缺口由保险基金补。
func (s *Server) liquidateIsolated(ctx context.Context, tx *sql.Tx, uid, sym, posSide string, mark decimal.Decimal) error {
	c := s.contracts[sym]
	bal0, _, err := lockAccount(ctx, tx, uid)
	if err != nil {
		return err
	}
	if err := cancelUserOrders(ctx, tx, uid, sym, "liquidation"); err != nil {
		return err
	}
	px := mark
	if p0, _, err := loadPosition(ctx, tx, uid, sym, posSide, true); err != nil {
		return err
	} else if eq := p0.Iso.Add(upnl(p0.Qty, p0.Entry, mark)); s.cfg.ADLEnabled && eq.Sign() < 0 {
		px = bankruptPrice(p0.Qty, mark, eq.Neg().Sub(insuranceAvailable(ctx, tx)))
	}
	p, err := s.takeover(ctx, tx, uid, posSide, c, mark, px)
	if err != nil || p.Qty.IsZero() {
		return err
	}
	bal1, _, err := readAccount(ctx, tx, uid)
	if err != nil {
		return err
	}
	equity := bal1.Sub(bal0) // iso margin + PnL returned by the close
	fee, shortfall := dZero, dZero
	if equity.Sign() > 0 {
		fee = dmin(equity, money(p.Qty.Abs().Mul(mark).Mul(s.liqFeeRate(c))))
	} else {
		shortfall = equity.Neg()
	}
	return s.settleLiq(ctx, tx, uid, c, p, mark, fee, shortfall)
}

// liquidateCross 撤掉该用户全部挂单，再按标记价接管每笔全仓。任一全仓没有标记价则整笔放弃。
// 账户权益为负且 ADL 开启时，缺口按各仓名义价值分摊进破产价。强平费和穿仓只在第一笔合约上记一次。
func (s *Server) liquidateCross(ctx context.Context, tx *sql.Tx, uid string) error {
	if _, _, err := lockAccount(ctx, tx, uid); err != nil {
		return err
	}
	if err := cancelUserOrders(ctx, tx, uid, "", "liquidation"); err != nil {
		return err
	}
	ps, err := openPositions(ctx, tx, uid)
	if err != nil {
		return err
	}
	type done struct {
		p    Position
		mark decimal.Decimal
	}
	bal0, _, err := readAccount(ctx, tx, uid)
	if err != nil {
		return err
	}
	marks := map[string]decimal.Decimal{}
	eq, totN := bal0, dZero
	for _, p := range ps {
		if p.Mode != "cross" {
			continue
		}
		m, ok := s.mark(p.Symbol)
		if !ok {
			return errMarkUnavail
		}
		marks[p.Symbol] = m
		eq = eq.Add(upnl(p.Qty, p.Entry, m))
		totN = totN.Add(p.Qty.Abs().Mul(m))
	}
	unc := dZero
	if s.cfg.ADLEnabled && eq.Sign() < 0 && totN.Sign() > 0 {
		unc = eq.Neg().Sub(insuranceAvailable(ctx, tx))
	}
	var closed []done
	notional := dZero
	for _, p := range ps {
		if p.Mode != "cross" {
			continue
		}
		c := s.contracts[p.Symbol]
		m := marks[p.Symbol]
		px := m
		if unc.Sign() > 0 {
			px = bankruptPrice(p.Qty, m, unc.Mul(p.Qty.Abs().Mul(m)).Div(totN))
		}
		if _, err := s.takeover(ctx, tx, uid, p.Side, c, m, px); err != nil {
			return err
		}
		closed = append(closed, done{p, m})
		notional = notional.Add(p.Qty.Abs().Mul(m).Mul(s.liqFeeRate(c)))
	}
	bal, _, err := readAccount(ctx, tx, uid)
	if err != nil {
		return err
	}
	fee, shortfall := dZero, dZero
	if bal.Sign() > 0 {
		fee = dmin(bal, money(notional))
	} else {
		shortfall = bal.Neg()
	}
	for i, d := range closed {
		f, sf := dZero, dZero
		if i == 0 {
			f, sf = fee, shortfall // booked once, on the first contract
		}
		if err := s.settleLiq(ctx, tx, uid, s.contracts[d.p.Symbol], d.p, d.mark, f, sf); err != nil {
			return err
		}
	}
	return nil
}

func (s *Server) settleLiq(ctx context.Context, tx *sql.Tx, uid string, c Contract, p Position, mark, fee, shortfall decimal.Decimal) error {
	ref := newRef("liq")
	if fee.Sign() > 0 {
		if err := addBalance(ctx, tx, uid, c.Symbol, "liquidation_fee", fee.Neg(), ref); err != nil {
			return err
		}
		if err := addBalance(ctx, tx, insuranceUser, c.Symbol, "liquidation_fee", fee, ref); err != nil {
			return err
		}
	}
	if shortfall.Sign() > 0 {
		if err := addBalance(ctx, tx, uid, c.Symbol, "insurance_cover", shortfall, ref); err != nil {
			return err
		}
		if err := addBalance(ctx, tx, insuranceUser, c.Symbol, "insurance_cover", shortfall.Neg(), ref); err != nil {
			return err
		}
	}
	log.Printf("futures: liquidated %s %s %s qty=%s mark=%s fee=%s shortfall=%s", uid, c.Symbol, p.Mode, p.Qty, mark, fee, shortfall)
	_, err := tx.ExecContext(ctx, `INSERT INTO fut_liquidations (user_id, symbol, margin_mode, qty, mark_price, fee, shortfall) VALUES ($1,$2,$3,$4,$5,$6,$7)`,
		uid, c.Symbol, p.Mode, p.Qty, mark, fee, shortfall)
	if err == nil {
		var ins decimal.Decimal
		_ = tx.QueryRowContext(ctx, `SELECT balance FROM fut_accounts WHERE user_id=$1`, insuranceUser).Scan(&ins)
		if ins.Sign() < 0 {
			log.Printf("ALERT futures: insurance fund balance negative: %s", ins)
		}
	}
	return err
}

// triggerTPSL 按仓位上记录的触发价类型（缺省 MARK）比较。未成交的平仓在 TPSLRetry 内不重复提交。调用方持有 s.mu。
func (s *Server) triggerTPSL(ctx context.Context) {
	rows, err := s.db.QueryContext(ctx, `SELECT `+posCols+` FROM fut_positions WHERE qty <> 0 AND (tp_price IS NOT NULL OR sl_price IS NOT NULL)`)
	if err != nil {
		return
	}
	var ps []Position
	for rows.Next() {
		if p, err := scanPos(rows); err == nil {
			ps = append(ps, p)
		}
	}
	rows.Close()
	now := s.now()
	for _, p := range ps {
		long := p.Qty.Sign() > 0
		src := ""
		if mt, ok := s.triggerRef(p.Symbol, trigOr(p.TPType)); ok && p.TP.Valid && ((long && mt.GreaterThanOrEqual(p.TP.Decimal)) || (!long && mt.LessThanOrEqual(p.TP.Decimal))) {
			src = "tp"
		} else if ms, ok := s.triggerRef(p.Symbol, trigOr(p.SLType)); ok && p.SL.Valid && ((long && ms.LessThanOrEqual(p.SL.Decimal)) || (!long && ms.GreaterThanOrEqual(p.SL.Decimal))) {
			src = "sl"
		}
		if src == "" {
			continue
		}
		key := p.UserID + "|" + p.Symbol + "|" + p.Side
		if t, ok := s.tpslRetry[key]; ok && now.Sub(t) < s.cfg.TPSLRetry {
			continue
		}
		s.tpslRetry[key] = now
		o, _, err := s.closeLocked(ctx, p.UserID, p.Symbol, p.Side, src)
		if err != nil {
			log.Printf("futures: %s trigger %s %s: %v", src, p.UserID, p.Symbol, err)
			continue
		}
		if o.Status == "filled" {
			delete(s.tpslRetry, key)
		}
	}
}

// coverNegative 由保险基金补上没有全仓持仓的负钱包余额（例如舍入或空仓后的资金费）。仍有全仓的负余额留给强平处理。
func (s *Server) coverNegative(ctx context.Context) {
	rows, err := s.db.QueryContext(ctx, `SELECT a.user_id FROM fut_accounts a WHERE a.balance < 0 AND a.user_id NOT LIKE 'system:%'
		AND NOT EXISTS (SELECT 1 FROM fut_positions p WHERE p.user_id=a.user_id AND p.qty<>0 AND p.margin_mode='cross')`)
	if err != nil {
		return
	}
	var users []string
	for rows.Next() {
		var u string
		if rows.Scan(&u) == nil {
			users = append(users, u)
		}
	}
	rows.Close()
	for _, uid := range users {
		_ = s.txLocked(ctx, func(tx *sql.Tx) error {
			bal, _, err := lockAccount(ctx, tx, uid)
			if err != nil || bal.Sign() >= 0 {
				return err
			}
			ref := newRef("cover")
			if err := addBalance(ctx, tx, uid, "", "insurance_cover", bal.Neg(), ref); err != nil {
				return err
			}
			return addBalance(ctx, tx, insuranceUser, "", "insurance_cover", bal, ref)
		})
	}
}
