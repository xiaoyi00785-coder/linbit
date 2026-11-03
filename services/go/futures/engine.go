package main

import (
	"context"
	"database/sql"
	"fmt"
	"log"
	"strings"
	"sync"
	"time"

	"github.com/shopspring/decimal"
)

// Server owns the futures engine. mu serializes every state mutation (orders,
// fills, funding, liquidation, transfers); pmu guards the in-memory price state.
type Server struct {
	cfg    Config
	db     *sql.DB
	asset  *AssetClient
	market *MarketClient
	now    func() time.Time

	tradfi *copyTradFi

	mu        sync.Mutex
	tickMu    sync.Mutex
	contracts map[string]Contract
	symbols   []string

	pmu            sync.RWMutex
	prices         map[string]*priceState
	devIndex       map[string]decimal.Decimal
	lastMarkSample time.Time
	tpslRetry      map[string]time.Time
	traders        map[string]bool // active copy traders (their fills emit copy events)
	lastCopySettle string
}

func NewServer(cfg Config, db *sql.DB) (*Server, error) {
	s := &Server{cfg: cfg, db: db, asset: NewAssetClient(cfg.AssetURL, cfg.InternalToken), market: NewMarketClient(cfg.MarketURL),
		now: time.Now, contracts: map[string]Contract{}, prices: map[string]*priceState{}, devIndex: map[string]decimal.Decimal{}, tpslRetry: map[string]time.Time{}, traders: map[string]bool{}}
	s.tradfi = newCopyTradFi(cfg.MarketURL, cfg.KycURL, cfg.InternalToken)
	ctx := context.Background()
	cs, err := loadContracts(ctx, db)
	if err != nil {
		return nil, err
	}
	for _, c := range cs {
		c.FundingH = int(cfg.FundingInterval / time.Hour)
		s.contracts[c.Symbol] = c
		s.symbols = append(s.symbols, c.Symbol)
		ps := &priceState{}
		_ = db.QueryRowContext(ctx, `SELECT price FROM fut_trades WHERE symbol=$1 ORDER BY id DESC LIMIT 1`, c.Symbol).Scan(&ps.Last)
		var ft sql.NullTime
		_ = db.QueryRowContext(ctx, `SELECT funding_time, rate FROM fut_funding_history WHERE symbol=$1 ORDER BY funding_time DESC LIMIT 1`, c.Symbol).Scan(&ft, &ps.LastRate)
		ps.Bucket = s.bucket(s.now())
		s.prices[c.Symbol] = ps
	}
	if err := s.loadTraders(ctx); err != nil {
		return nil, err
	}
	return s, nil
}

func (s *Server) contract(sym string) (Contract, error) {
	c, ok := s.contracts[strings.ToUpper(strings.TrimSpace(sym))]
	if !ok {
		return Contract{}, bad("UNKNOWN_SYMBOL", "合约不存在")
	}
	return c, nil
}

// withTx runs fn under the engine lock in a single transaction.
func (s *Server) withTx(ctx context.Context, fn func(tx *sql.Tx) error) error {
	s.mu.Lock()
	defer s.mu.Unlock()
	return s.txLocked(ctx, fn)
}

func (s *Server) txLocked(ctx context.Context, fn func(tx *sql.Tx) error) error {
	tx, err := s.db.BeginTx(ctx, nil)
	if err != nil {
		return err
	}
	if err := fn(tx); err != nil {
		_ = tx.Rollback()
		return err
	}
	return tx.Commit()
}

// ---- account state ----

type PosView struct {
	Symbol      string  `json:"symbol"`
	Side        string  `json:"side"`
	Qty         string  `json:"qty"`
	Entry       string  `json:"entryPrice"`
	Mark        string  `json:"markPrice"`
	Notional    string  `json:"notional"`
	UPnL        string  `json:"unrealizedPnl"`
	ROE         string  `json:"roe"`
	Realized    string  `json:"realizedPnl"`
	Mode        string  `json:"marginMode"`
	Leverage    int     `json:"leverage"`
	Margin      string  `json:"margin"`
	MM          string  `json:"maintenanceMargin"`
	Liq         string  `json:"liquidationPrice"`
	TP          *string `json:"tpPrice"`
	SL          *string `json:"slPrice"`
	ADLRank     int     `json:"adlRank"`
	pos         Position
	mark, upnl  decimal.Decimal
	im, mm      decimal.Decimal
	markMissing bool
}

type AcctState struct {
	Balance   decimal.Decimal `json:"walletBalance"`
	Frozen    decimal.Decimal `json:"orderMargin"`
	CrossIM   decimal.Decimal `json:"crossPositionMargin"`
	CrossMM   decimal.Decimal `json:"crossMaintenanceMargin"`
	CrossUPnL decimal.Decimal `json:"crossUnrealizedPnl"`
	IsoMargin decimal.Decimal `json:"isolatedMargin"`
	IsoUPnL   decimal.Decimal `json:"isolatedUnrealizedPnl"`
	Available decimal.Decimal `json:"availableBalance"`
	Equity    decimal.Decimal `json:"marginBalance"`
	Positions []PosView       `json:"positions"`
}

func nullStr(n decimal.NullDecimal) *string {
	if !n.Valid {
		return nil
	}
	v := n.Decimal.String()
	return &v
}

func (s *Server) state(ctx context.Context, q queryer, uid string) (AcctState, error) {
	var st AcctState
	var err error
	if st.Balance, st.Frozen, err = readAccount(ctx, q, uid); err != nil {
		return st, err
	}
	ps, err := openPositions(ctx, q, uid)
	if err != nil {
		return st, err
	}
	for _, p := range ps {
		c := s.contracts[p.Symbol]
		m, ok := s.mark(p.Symbol)
		v := PosView{pos: p, markMissing: !ok}
		if !ok {
			m = p.Entry
		}
		v.mark = m
		v.upnl = upnl(p.Qty, p.Entry, m)
		lev := decimal.NewFromInt(int64(max(p.Leverage, 1)))
		v.mm = c.maintMargin(p.Qty.Abs().Mul(m))
		if p.Mode == "isolated" {
			v.im = p.Iso
			st.IsoMargin = st.IsoMargin.Add(p.Iso)
			st.IsoUPnL = st.IsoUPnL.Add(v.upnl)
		} else {
			v.im = p.Qty.Abs().Mul(m).Div(lev)
			st.CrossIM = st.CrossIM.Add(v.im)
			st.CrossMM = st.CrossMM.Add(v.mm)
			st.CrossUPnL = st.CrossUPnL.Add(v.upnl)
		}
		st.Positions = append(st.Positions, v)
	}
	st.Available = st.Balance.Sub(st.Frozen).Sub(st.CrossIM).Add(dmin(dZero, st.CrossUPnL))
	st.Equity = st.Balance.Add(st.IsoMargin).Add(st.CrossUPnL).Add(st.IsoUPnL)
	for i := range st.Positions {
		v := &st.Positions[i]
		p := v.pos
		c := s.contracts[p.Symbol]
		var M decimal.Decimal
		if p.Mode == "isolated" {
			M = p.Iso
		} else {
			M = st.Balance.Add(st.CrossUPnL.Sub(v.upnl)).Sub(st.CrossMM.Sub(v.mm))
		}
		v.Symbol, v.Mode, v.Leverage = p.Symbol, p.Mode, p.Leverage
		v.Side = "long"
		if p.Qty.Sign() < 0 {
			v.Side = "short"
		}
		v.Qty, v.Entry, v.Mark = p.Qty.Abs().String(), p.Entry.Round(8).String(), v.mark.Round(8).String()
		v.Notional = p.Qty.Abs().Mul(v.mark).Round(8).String()
		v.UPnL = money(v.upnl).String()
		roe := dZero
		if v.im.Sign() > 0 {
			roe = v.upnl.Div(v.im).Round(4)
		}
		v.ROE = roe.String()
		v.Realized = money(p.Realized).String()
		v.Margin = money(v.im).String()
		v.MM = money(v.mm).String()
		t := c.tierFor(p.Qty.Abs().Mul(v.mark))
		v.Liq = liqPrice(p.Qty, p.Entry, M.Add(t.MaintAmount), t.MMR).Round(8).String()
		v.TP, v.SL = nullStr(p.TP), nullStr(p.SL)
	}
	if st.Positions == nil {
		st.Positions = []PosView{}
	}
	return st, nil
}

// ---- fills ----

type Trade struct {
	ID        int64           `json:"tradeId"`
	Symbol    string          `json:"symbol"`
	Price     decimal.Decimal `json:"price"`
	Qty       decimal.Decimal `json:"qty"`
	TakerSide string          `json:"takerSide"`
	Time      time.Time       `json:"time"`
}

// fill applies a signed quantity d at price px to uid's position and wallet:
// realized PnL, isolated-margin movement, and fee = |d|*px*feeRate (by position
// value, on open and on close alike) moved to the fee account.
func (s *Server) fill(ctx context.Context, tx *sql.Tx, c Contract, uid string, d, px, feeRate decimal.Decimal, lev int, mode string, tp, sl decimal.NullDecimal) (fee, pnl, bonus decimal.Decimal, err error) {
	if _, _, err = lockAccount(ctx, tx, uid); err != nil {
		return
	}
	p, _, err := loadPosition(ctx, tx, uid, c.Symbol, true)
	if err != nil {
		return
	}
	if p.Qty.IsZero() {
		p.Mode, p.Leverage, p.Iso, p.Realized, p.TP, p.SL = mode, lev, dZero, dZero, decimal.NullDecimal{}, decimal.NullDecimal{}
	}
	if p.Leverage < 1 {
		p.Leverage = max(lev, 1)
	}
	oldQ := p.Qty
	nq, ne, rp, closed := applyTrade(p.Qty, p.Entry, d, px)
	rp = money(rp)
	balDelta := rp
	if p.Mode == "isolated" {
		if closed.Sign() > 0 && !oldQ.IsZero() {
			rel := money(p.Iso.Mul(closed).Div(oldQ.Abs()))
			p.Iso = p.Iso.Sub(rel)
			balDelta = balDelta.Add(rel)
		}
		if opened := d.Abs().Sub(closed); opened.Sign() > 0 {
			add := money(opened.Mul(px).Div(decimal.NewFromInt(int64(p.Leverage))))
			p.Iso = p.Iso.Add(add)
			balDelta = balDelta.Sub(add)
		}
	}
	p.Qty, p.Entry = nq, ne
	p.Realized = p.Realized.Add(rp)
	if nq.IsZero() {
		balDelta = balDelta.Add(p.Iso)
		p.Iso, p.Entry, p.TP, p.SL = dZero, dZero, decimal.NullDecimal{}, decimal.NullDecimal{}
	} else if (tp.Valid || sl.Valid) && closed.LessThan(d.Abs()) {
		if tp.Valid {
			p.TP = tp
		}
		if sl.Valid {
			p.SL = sl
		}
	}
	if oldQ.Sign() != 0 && nq.Sign() != 0 && oldQ.Sign() != nq.Sign() {
		p.TP, p.SL = decimal.NullDecimal{}, decimal.NullDecimal{} // flipped: old TP/SL no longer apply
	}
	if err = savePosition(ctx, tx, p); err != nil {
		return
	}
	if s.traders[uid] && !oldQ.Equal(nq) {
		if _, err = tx.ExecContext(ctx, `INSERT INTO fut_copy_events (trader, symbol, old_qty, new_qty, price) VALUES ($1,$2,$3,$4,$5)`, uid, c.Symbol, oldQ, nq, px); err != nil {
			return
		}
	}
	// Isolated-margin moves are internal (balance <-> position) and not billed;
	// realized PnL and fees are.
	if _, err = tx.ExecContext(ctx, `UPDATE fut_accounts SET balance = balance + $2, updated_at=now() WHERE user_id=$1`, uid, balDelta.Sub(rp)); err != nil {
		return
	}
	if err = addBalance(ctx, tx, uid, c.Symbol, "realized_pnl", rp, ""); err != nil {
		return
	}
	fee = money(d.Abs().Mul(px).Mul(feeRate))
	if fee.Sign() > 0 {
		if err = addBalance(ctx, tx, uid, c.Symbol, "fee", fee.Neg(), ""); err != nil {
			return
		}
		if _, err = tx.ExecContext(ctx, `UPDATE fut_accounts SET balance = balance + $2 WHERE user_id=$1`, feeUser, fee); err != nil {
			return
		}
		if bonus, err = useBonus(ctx, tx, uid, c.Symbol, "bonus_fee", fee, "deduction", "trial"); err != nil {
			return
		}
	}
	return fee, rp, bonus, nil
}

// ---- orders ----

type PlaceReq struct {
	Symbol     string `json:"symbol"`
	Side       string `json:"side"`
	Type       string `json:"type"`
	TIF        string `json:"timeInForce"`
	Price      string `json:"price"`
	Qty        string `json:"qty"`
	ReduceOnly bool   `json:"reduceOnly"`
	TP         string `json:"tpPrice"`
	SL         string `json:"slPrice"`
	source     string
}

func optDec(v string) (decimal.NullDecimal, error) {
	v = strings.TrimSpace(v)
	if v == "" || v == "0" {
		return decimal.NullDecimal{}, nil
	}
	d, err := decimal.NewFromString(v)
	if err != nil || d.Sign() <= 0 {
		return decimal.NullDecimal{}, bad("VALIDATION_ERROR", "价格不合法")
	}
	return decimal.NullDecimal{Decimal: d, Valid: true}, nil
}

func (s *Server) PlaceOrder(ctx context.Context, uid string, r PlaceReq) (Order, []Trade, error) {
	s.mu.Lock()
	defer s.mu.Unlock()
	o, t, err := s.placeLocked(ctx, uid, r)
	s.processCopy(ctx)
	return o, t, err
}

func (s *Server) placeLocked(ctx context.Context, uid string, r PlaceReq) (Order, []Trade, error) {
	c, err := s.contract(r.Symbol)
	if err != nil {
		return Order{}, nil, err
	}
	if c.Status != "trading" {
		return Order{}, nil, errContractHalted
	}
	r.Side, r.Type, r.TIF = strings.ToLower(r.Side), strings.ToLower(r.Type), strings.ToUpper(r.TIF)
	if r.Side != "buy" && r.Side != "sell" {
		return Order{}, nil, bad("VALIDATION_ERROR", "side 必须为 buy 或 sell")
	}
	if r.Type == "" {
		r.Type = "limit"
	}
	if r.Type != "limit" && r.Type != "market" {
		return Order{}, nil, bad("VALIDATION_ERROR", "type 必须为 limit 或 market")
	}
	if r.TIF == "" {
		r.TIF = "GTC"
	}
	if r.TIF != "GTC" && r.TIF != "IOC" {
		return Order{}, nil, bad("VALIDATION_ERROR", "timeInForce 仅支持 GTC/IOC")
	}
	if r.source == "" {
		r.source = "user"
	}
	qty, err := decimal.NewFromString(strings.TrimSpace(r.Qty))
	if err != nil || !isStep(qty, c.MinQty) {
		return Order{}, nil, bad("INVALID_QTY", fmt.Sprintf("数量须为 %s 的整数倍", c.MinQty))
	}
	if qty.GreaterThan(c.MaxQty) {
		return Order{}, nil, bad("INVALID_QTY", "超过单笔最大数量")
	}
	var price decimal.NullDecimal
	if r.Type == "limit" {
		p, err := decimal.NewFromString(strings.TrimSpace(r.Price))
		if err != nil || !isStep(p, c.Tick) {
			return Order{}, nil, bad("INVALID_PRICE", fmt.Sprintf("价格须为 %s 的整数倍", c.Tick))
		}
		price = decimal.NullDecimal{Decimal: p, Valid: true}
	}
	tp, err := optDec(r.TP)
	if err != nil {
		return Order{}, nil, err
	}
	sl, err := optDec(r.SL)
	if err != nil {
		return Order{}, nil, err
	}
	mark, ok := s.mark(c.Symbol)
	if !ok {
		return Order{}, nil, errMarkUnavail
	}
	if (tp.Valid || sl.Valid) && r.ReduceOnly {
		return Order{}, nil, bad("VALIDATION_ERROR", "只减仓订单不能附带止盈止损")
	}
	ref := mark
	if price.Valid {
		ref = price.Decimal
	}
	if tp.Valid || sl.Valid {
		long := r.Side == "buy"
		if (tp.Valid && (long != tp.Decimal.GreaterThan(ref))) || (sl.Valid && (long != sl.Decimal.LessThan(ref))) {
			return Order{}, nil, bad("INVALID_TPSL", "止盈/止损价格方向不正确")
		}
	}
	var o Order
	var trades []Trade
	err = s.txLocked(ctx, func(tx *sql.Tx) error {
		if _, _, err := lockAccount(ctx, tx, uid); err != nil {
			return err
		}
		lev, mode, err := s.settings(ctx, tx, uid, c)
		if err != nil {
			return err
		}
		reserve := dZero
		if !r.ReduceOnly {
			p, _, err := loadPosition(ctx, tx, uid, c.Symbol, false)
			if err != nil {
				return err
			}
			var pend decimal.Decimal
			if err := tx.QueryRowContext(ctx, `SELECT COALESCE(sum(qty-filled),0) FROM fut_orders WHERE user_id=$1 AND symbol=$2 AND side=$3 AND NOT reduce_only AND status IN ('new','partially_filled')`, uid, c.Symbol, r.Side).Scan(&pend); err != nil {
				return err
			}
			proj := p.Qty.Add(signed(r.Side, qty.Add(pend))).Abs().Mul(ref)
			if lim := c.maxNotional(lev); proj.GreaterThan(lim) {
				return bad("POSITION_LIMIT", fmt.Sprintf("超过 %dx 杠杆可持有的最大仓位价值 %s USDT，请降低杠杆或数量", lev, lim))
			}
		}
		if r.ReduceOnly {
			p, _, err := loadPosition(ctx, tx, uid, c.Symbol, false)
			if err != nil {
				return err
			}
			if p.Qty.IsZero() || (p.Qty.Sign() > 0) == (r.Side == "buy") {
				return bad("REDUCE_ONLY_REJECTED", "没有可减少的仓位")
			}
		} else {
			rp := ref
			if r.Type == "market" {
				rp = mark.Mul(dOne.Add(s.cfg.MarketBand))
			}
			reserve = money(qty.Mul(rp).Div(decimal.NewFromInt(int64(lev))).Add(qty.Mul(rp).Mul(s.reserveTaker(uid, c))))
			st, err := s.state(ctx, tx, uid)
			if err != nil {
				return err
			}
			if st.Available.LessThan(reserve) {
				return errInsufficient
			}
			if _, err := tx.ExecContext(ctx, `UPDATE fut_accounts SET frozen = frozen + $2 WHERE user_id=$1`, uid, reserve); err != nil {
				return err
			}
		}
		row := tx.QueryRowContext(ctx, `INSERT INTO fut_orders (user_id, symbol, side, type, tif, price, qty, reserved, status, reduce_only, leverage, margin_mode, source, tp_price, sl_price)
			VALUES ($1,$2,$3,$4,$5,$6,$7,$8,'new',$9,$10,$11,$12,$13,$14) RETURNING `+orderCols,
			uid, c.Symbol, r.Side, r.Type, r.TIF, price, qty, reserve, r.ReduceOnly, lev, mode, r.source, tp, sl)
		if o, err = scanOrder(row); err != nil {
			return err
		}
		trades, err = s.match(ctx, tx, c, &o, mark)
		return err
	})
	if err != nil {
		return Order{}, nil, err
	}
	if len(trades) > 0 {
		s.setLast(c.Symbol, trades[len(trades)-1].Price)
	}
	return o, trades, nil
}

// closable returns how much of an order on side can reduce uid's position.
func closable(ctx context.Context, tx *sql.Tx, uid, sym, side string) (decimal.Decimal, error) {
	p, _, err := loadPosition(ctx, tx, uid, sym, false)
	if err != nil || p.Qty.IsZero() {
		return dZero, err
	}
	if (p.Qty.Sign() > 0) == (side == "buy") {
		return dZero, nil
	}
	return p.Qty.Abs(), nil
}

func signed(side string, q decimal.Decimal) decimal.Decimal {
	if side == "sell" {
		return q.Neg()
	}
	return q
}

// consume records qty filled at px on o (with fee) and releases the matching
// share of its reservation.
func consume(ctx context.Context, tx *sql.Tx, o *Order, qty, px, fee decimal.Decimal) error {
	rem := o.remaining()
	rel := o.Reserved
	if rem.GreaterThan(qty) {
		rel = money(o.Reserved.Mul(qty).Div(rem))
	}
	o.Reserved = o.Reserved.Sub(rel)
	o.Filled = o.Filled.Add(qty)
	o.FilledQuote = o.FilledQuote.Add(qty.Mul(px))
	o.Fee = o.Fee.Add(fee)
	o.Status = "partially_filled"
	if o.remaining().IsZero() {
		o.Status = "filled"
		rel = rel.Add(o.Reserved)
		o.Reserved = dZero
	}
	if err := addFrozen(ctx, tx, o.UserID, rel.Neg()); err != nil {
		return err
	}
	return updateOrder(ctx, tx, o)
}

// match crosses taker against the book (price-time priority, maker price).
func (s *Server) match(ctx context.Context, tx *sql.Tx, c Contract, taker *Order, mark decimal.Decimal) ([]Trade, error) {
	var trades []Trade
	opp, cmp, ord := "sell", "<=", "ASC"
	if taker.Side == "sell" {
		opp, cmp, ord = "buy", ">=", "DESC"
	}
	band := mark.Mul(s.cfg.MarketBand)
	reason := ""
	for taker.remaining().Sign() > 0 {
		qs := `SELECT ` + orderCols + ` FROM fut_orders WHERE symbol=$1 AND side=$2 AND type='limit' AND status IN ('new','partially_filled')`
		args := []any{c.Symbol, opp}
		if taker.Price.Valid {
			qs += ` AND price ` + cmp + ` $3`
			args = append(args, taker.Price.Decimal)
		}
		qs += ` ORDER BY price ` + ord + `, id ASC LIMIT 1 FOR UPDATE`
		maker, err := scanOrder(tx.QueryRowContext(ctx, qs, args...))
		if err == sql.ErrNoRows {
			break
		}
		if err != nil {
			return nil, err
		}
		px := maker.Price.Decimal
		if taker.Type == "market" && px.Sub(mark).Abs().GreaterThan(band) {
			reason = "price_band"
			break
		}
		if maker.UserID == taker.UserID {
			if err := releaseOrder(ctx, tx, &maker, "self_trade_prevention"); err != nil {
				return nil, err
			}
			continue
		}
		qty := dmin(taker.remaining(), maker.remaining())
		if taker.ReduceOnly {
			cl, err := closable(ctx, tx, taker.UserID, c.Symbol, taker.Side)
			if err != nil {
				return nil, err
			}
			if qty = dmin(qty, cl); qty.IsZero() {
				reason = "reduce_only"
				break
			}
		}
		if maker.ReduceOnly {
			cl, err := closable(ctx, tx, maker.UserID, c.Symbol, maker.Side)
			if err != nil {
				return nil, err
			}
			if cl.IsZero() {
				if err := releaseOrder(ctx, tx, &maker, "reduce_only"); err != nil {
					return nil, err
				}
				continue
			}
			qty = dmin(qty, cl)
		}
		mRate, _, _ := s.userFees(maker.UserID, c)
		_, tRate, _ := s.userFees(taker.UserID, c)
		mfee, _, mbonus, err := s.fill(ctx, tx, c, maker.UserID, signed(maker.Side, qty), px, mRate, maker.Leverage, maker.Mode, maker.TP, maker.SL)
		if err != nil {
			return nil, err
		}
		tfee, _, tbonus, err := s.fill(ctx, tx, c, taker.UserID, signed(taker.Side, qty), px, tRate, taker.Leverage, taker.Mode, taker.TP, taker.SL)
		if err != nil {
			return nil, err
		}
		if err := consume(ctx, tx, &maker, qty, px, mfee); err != nil {
			return nil, err
		}
		if maker.ReduceOnly && maker.open() {
			if cl, _ := closable(ctx, tx, maker.UserID, c.Symbol, maker.Side); cl.IsZero() {
				if err := releaseOrder(ctx, tx, &maker, "reduce_only"); err != nil {
					return nil, err
				}
			}
		}
		if err := consume(ctx, tx, taker, qty, px, tfee); err != nil {
			return nil, err
		}
		t := Trade{Symbol: c.Symbol, Price: px, Qty: qty, TakerSide: taker.Side}
		if err := tx.QueryRowContext(ctx, `INSERT INTO fut_trades (symbol, price, qty, taker_side, maker_order, taker_order, maker_user, taker_user, maker_fee, taker_fee, maker_bonus_fee, taker_bonus_fee)
			VALUES ($1,$2,$3,$4,$5,$6,$7,$8,$9,$10,$11,$12) RETURNING id, created_at`, c.Symbol, px, qty, taker.Side, maker.ID, taker.ID, maker.UserID, taker.UserID, mfee, tfee, mbonus, tbonus).Scan(&t.ID, &t.Time); err != nil {
			return nil, err
		}
		trades = append(trades, t)
	}
	if taker.open() && taker.remaining().Sign() > 0 {
		if taker.Type == "market" || taker.TIF == "IOC" || reason == "reduce_only" {
			if reason == "" {
				reason = "no_liquidity"
				if taker.TIF == "IOC" && taker.Type == "limit" {
					reason = "ioc_remainder"
				}
			}
			if err := releaseOrder(ctx, tx, taker, reason); err != nil {
				return nil, err
			}
		}
	}
	return trades, nil
}

func (s *Server) CancelOrder(ctx context.Context, uid string, id int64) (Order, error) {
	var o Order
	err := s.withTx(ctx, func(tx *sql.Tx) error {
		var err error
		o, err = scanOrder(tx.QueryRowContext(ctx, `SELECT `+orderCols+` FROM fut_orders WHERE id=$1 AND user_id=$2 FOR UPDATE`, id, uid))
		if err == sql.ErrNoRows {
			return errNotFound
		}
		if err != nil {
			return err
		}
		if !o.open() {
			return bad("ORDER_NOT_OPEN", "订单已完成或已撤销")
		}
		return releaseOrder(ctx, tx, &o, "user")
	})
	return o, err
}

// ---- settings / margin / TP-SL ----

func (s *Server) SetLeverage(ctx context.Context, uid, sym string, lev int, mode string) error {
	c, err := s.contract(sym)
	if err != nil {
		return err
	}
	return s.withTx(ctx, func(tx *sql.Tx) error {
		if _, _, err := lockAccount(ctx, tx, uid); err != nil {
			return err
		}
		curLev, curMode, err := s.settings(ctx, tx, uid, c)
		if err != nil {
			return err
		}
		if lev == 0 {
			lev = curLev
		}
		if mode == "" {
			mode = curMode
		}
		if lev < 1 || lev > c.MaxLeverage {
			return bad("INVALID_LEVERAGE", fmt.Sprintf("杠杆倍数范围 1-%d", c.MaxLeverage))
		}
		if mode != "cross" && mode != "isolated" {
			return bad("VALIDATION_ERROR", "marginMode 必须为 cross 或 isolated")
		}
		var openOrders int
		if err := tx.QueryRowContext(ctx, `SELECT count(*) FROM fut_orders WHERE user_id=$1 AND symbol=$2 AND status IN ('new','partially_filled')`, uid, c.Symbol).Scan(&openOrders); err != nil {
			return err
		}
		p, _, err := loadPosition(ctx, tx, uid, c.Symbol, true)
		if err != nil {
			return err
		}
		if (lev != curLev || mode != curMode) && openOrders > 0 {
			return bad("HAS_OPEN_ORDERS", "请先撤销该合约的挂单")
		}
		if !p.Qty.IsZero() && lev != curLev {
			if m, ok := s.mark(c.Symbol); ok && p.Qty.Abs().Mul(m).GreaterThan(c.maxNotional(lev)) {
				return bad("POSITION_LIMIT", fmt.Sprintf("当前仓位价值超过 %dx 杠杆的上限 %s USDT", lev, c.maxNotional(lev)))
			}
		}
		if mode != curMode && !p.Qty.IsZero() {
			return bad("HAS_POSITION", "持仓期间不能切换保证金模式")
		}
		if _, err := tx.ExecContext(ctx, `INSERT INTO fut_settings (user_id, symbol, leverage, margin_mode) VALUES ($1,$2,$3,$4)
			ON CONFLICT (user_id, symbol) DO UPDATE SET leverage=EXCLUDED.leverage, margin_mode=EXCLUDED.margin_mode`, uid, c.Symbol, lev, mode); err != nil {
			return err
		}
		if !p.Qty.IsZero() && lev != p.Leverage {
			if p.Mode == "isolated" && lev < p.Leverage {
				return bad("INVALID_LEVERAGE", "逐仓持仓期间只能调高杠杆，或先追加保证金")
			}
			p.Leverage = lev
			if err := savePosition(ctx, tx, p); err != nil {
				return err
			}
			st, err := s.state(ctx, tx, uid)
			if err != nil {
				return err
			}
			if st.Available.Sign() < 0 {
				return errInsufficient
			}
		}
		return nil
	})
}

// AdjustMargin adds (delta>0) or removes isolated margin.
func (s *Server) AdjustMargin(ctx context.Context, uid, sym string, delta decimal.Decimal) error {
	c, err := s.contract(sym)
	if err != nil {
		return err
	}
	return s.withTx(ctx, func(tx *sql.Tx) error {
		if _, _, err := lockAccount(ctx, tx, uid); err != nil {
			return err
		}
		p, _, err := loadPosition(ctx, tx, uid, c.Symbol, true)
		if err != nil {
			return err
		}
		if p.Qty.IsZero() || p.Mode != "isolated" {
			return bad("NOT_ISOLATED", "仅逐仓持仓可调整保证金")
		}
		if delta.Sign() > 0 {
			st, err := s.state(ctx, tx, uid)
			if err != nil {
				return err
			}
			if st.Available.LessThan(delta) {
				return errInsufficient
			}
		} else {
			m, ok := s.mark(c.Symbol)
			if !ok {
				return errMarkUnavail
			}
			im := p.Qty.Abs().Mul(m).Div(decimal.NewFromInt(int64(p.Leverage)))
			left := p.Iso.Add(delta).Add(dmin(dZero, upnl(p.Qty, p.Entry, m)))
			if left.LessThan(im) {
				return bad("MARGIN_TOO_LOW", "减少后保证金低于起始保证金")
			}
		}
		p.Iso = p.Iso.Add(delta)
		if err := savePosition(ctx, tx, p); err != nil {
			return err
		}
		_, err = tx.ExecContext(ctx, `UPDATE fut_accounts SET balance = balance - $2 WHERE user_id=$1`, uid, delta)
		return err
	})
}

// SetTPSL sets/clears the position-level take-profit / stop-loss (mark-triggered).
func (s *Server) SetTPSL(ctx context.Context, uid, sym string, tp, sl decimal.NullDecimal, clearTP, clearSL bool) error {
	c, err := s.contract(sym)
	if err != nil {
		return err
	}
	return s.withTx(ctx, func(tx *sql.Tx) error {
		p, _, err := loadPosition(ctx, tx, uid, c.Symbol, true)
		if err != nil {
			return err
		}
		if p.Qty.IsZero() {
			return bad("NO_POSITION", "当前无持仓")
		}
		m, ok := s.mark(c.Symbol)
		if !ok {
			return errMarkUnavail
		}
		long := p.Qty.Sign() > 0
		if tp.Valid {
			if long != tp.Decimal.GreaterThan(m) {
				return bad("INVALID_TPSL", "止盈价方向不正确（多仓需高于标记价，空仓需低于标记价）")
			}
			p.TP = tp
		} else if clearTP {
			p.TP = decimal.NullDecimal{}
		}
		if sl.Valid {
			if long != sl.Decimal.LessThan(m) {
				return bad("INVALID_TPSL", "止损价方向不正确（多仓需低于标记价，空仓需高于标记价）")
			}
			p.SL = sl
		} else if clearSL {
			p.SL = decimal.NullDecimal{}
		}
		return savePosition(ctx, tx, p)
	})
}

// ClosePosition submits a reduce-only market order for the whole position.
func (s *Server) ClosePosition(ctx context.Context, uid, sym, source string) (Order, []Trade, error) {
	s.mu.Lock()
	defer s.mu.Unlock()
	o, t, err := s.closeLocked(ctx, uid, sym, source)
	s.processCopy(ctx)
	return o, t, err
}

func (s *Server) closeLocked(ctx context.Context, uid, sym, source string) (Order, []Trade, error) {
	c, err := s.contract(sym)
	if err != nil {
		return Order{}, nil, err
	}
	p, _, err := loadPosition(ctx, s.db, uid, c.Symbol, false)
	if err != nil {
		return Order{}, nil, err
	}
	if p.Qty.IsZero() {
		return Order{}, nil, bad("NO_POSITION", "当前无持仓")
	}
	side := "sell"
	if p.Qty.Sign() < 0 {
		side = "buy"
	}
	return s.placeLocked(ctx, uid, PlaceReq{Symbol: c.Symbol, Side: side, Type: "market", Qty: p.Qty.Abs().String(), ReduceOnly: true, source: source})
}

// ---- transfers (spot <-> futures) ----

// TransferIn debits spot then credits the futures wallet. TransferOut debits the
// futures wallet first (under the lock) then credits spot. A pending row is
// retried by RunTransferRetry until the (idempotent) asset call succeeds.
func (s *Server) Transfer(ctx context.Context, uid, dir string, amount decimal.Decimal) (string, string, error) {
	return s.TransferIdem(ctx, uid, dir, amount, "")
}

// TransferIdem is Transfer with an optional client idempotency key: a repeat
// with the same clientId returns the original ref/status (no second move);
// the same clientId with a different direction/amount is IDEMPOTENCY_CONFLICT.
func (s *Server) TransferIdem(ctx context.Context, uid, dir string, amount decimal.Decimal, clientID string) (string, string, error) {
	return s.TransferWallet(ctx, uid, dir, "spot", amount, clientID)
}

// TransferWallet is TransferIdem with the asset-service wallet (spot|funding)
// on the non-futures side.
func (s *Server) TransferWallet(ctx context.Context, uid, dir, wallet string, amount decimal.Decimal, clientID string) (string, string, error) {
	if wallet != "spot" && wallet != "funding" {
		return "", "", bad("VALIDATION_ERROR", "wallet 不合法")
	}
	if amount.LessThan(s.cfg.MinTransfer) || amount.Exponent() < -8 {
		return "", "", bad("INVALID_AMOUNT", "划转金额不合法")
	}
	if dir != "in" && dir != "out" {
		return "", "", bad("VALIDATION_ERROR", "direction 必须为 in 或 out")
	}
	var cid any
	if clientID != "" {
		cid = clientID
		if ref, st, found, err := s.transferByClientIDW(ctx, s.db, uid, clientID, dir, amount, wallet); err != nil || found {
			return ref, st, err
		}
	}
	ref := newRef("fut")
	switch dir {
	case "in":
		res, err := s.db.ExecContext(ctx, `INSERT INTO fut_transfers (ref, user_id, direction, amount, status, client_id, wallet) VALUES ($1,$2,'in',$3,'pending',$4,$5)
			ON CONFLICT (user_id, client_id) WHERE client_id IS NOT NULL DO NOTHING`, ref, uid, amount, cid, wallet)
		if err != nil {
			return "", "", err
		}
		if n, _ := res.RowsAffected(); n == 0 { // lost a race with the same clientId
			r, st, _, err := s.transferByClientIDW(ctx, s.db, uid, clientID, dir, amount, wallet)
			return r, st, err
		}
		return ref, s.completeIn(ctx, ref, uid, wallet, amount), nil
	case "out":
		replay := ""
		err := s.withTx(ctx, func(tx *sql.Tx) error {
			if _, _, err := lockAccount(ctx, tx, uid); err != nil {
				return err
			}
			st, err := s.state(ctx, tx, uid)
			if err != nil {
				return err
			}
			if clientID != "" { // re-check under the account lock (serialises per user)
				r, rs, found, err := s.transferByClientIDW(ctx, tx, uid, clientID, dir, amount, wallet)
				if err != nil {
					return err
				}
				if found {
					ref, replay = r, rs
					return nil
				}
			}
			if st.Available.LessThan(amount) {
				return bad("INSUFFICIENT_BALANCE", "可划转余额不足")
			}
			if _, err := tx.ExecContext(ctx, `INSERT INTO fut_transfers (ref, user_id, direction, amount, status, client_id, wallet) VALUES ($1,$2,'out',$3,'pending',$4,$5)`, ref, uid, amount, cid, wallet); err != nil {
				return err
			}
			return addBalance(ctx, tx, uid, "", "transfer_out", amount.Neg(), ref)
		})
		if err != nil {
			return "", "", err
		}
		if replay != "" {
			return ref, replay, nil
		}
		return ref, s.completeOut(ctx, ref, uid, wallet, amount), nil
	}
	return "", "", bad("VALIDATION_ERROR", "direction 必须为 in 或 out")
}

func (s *Server) markTransfer(ctx context.Context, ref, status, errMsg string) {
	_, _ = s.db.ExecContext(ctx, `UPDATE fut_transfers SET status=$2, attempts=attempts+1, last_error=NULLIF($3,''), updated_at=now() WHERE ref=$1 AND status='pending'`, ref, status, errMsg)
}

func (s *Server) completeIn(ctx context.Context, ref, uid, wallet string, amount decimal.Decimal) string {
	err := s.asset.MoveWallet(ctx, wallet, uid, amount, ref, true)
	if err == ErrSpotInsufficient {
		s.markTransfer(ctx, ref, "failed", "spot balance insufficient")
		return "failed"
	}
	if err != nil {
		log.Printf("futures: transfer-in %s pending: %v", ref, err)
		s.markTransfer(ctx, ref, "pending", err.Error())
		return "pending"
	}
	err = s.withTx(ctx, func(tx *sql.Tx) error {
		res, err := tx.ExecContext(ctx, `UPDATE fut_transfers SET status='done', attempts=attempts+1, updated_at=now() WHERE ref=$1 AND status='pending'`, ref)
		if err != nil {
			return err
		}
		if n, _ := res.RowsAffected(); n == 0 {
			return nil
		}
		return addBalance(ctx, tx, uid, "", "transfer_in", amount, ref)
	})
	if err != nil {
		log.Printf("futures: credit %s: %v", ref, err)
		return "pending"
	}
	return "done"
}

func (s *Server) completeOut(ctx context.Context, ref, uid, wallet string, amount decimal.Decimal) string {
	if err := s.asset.MoveWallet(ctx, wallet, uid, amount, ref, false); err != nil {
		log.Printf("futures: transfer-out %s pending: %v", ref, err)
		s.markTransfer(ctx, ref, "pending", err.Error())
		return "pending"
	}
	_, _ = s.db.ExecContext(ctx, `UPDATE fut_transfers SET status='done', attempts=attempts+1, updated_at=now() WHERE ref=$1 AND status='pending'`, ref)
	return "done"
}

func (s *Server) RunTransferRetry(ctx context.Context) {
	t := time.NewTicker(s.cfg.TransferRetry)
	defer t.Stop()
	for {
		select {
		case <-ctx.Done():
			return
		case <-t.C:
		}
		rows, err := s.db.QueryContext(ctx, `SELECT ref, user_id, direction, amount, wallet, asset, COALESCE(from_account,''), COALESCE(to_account,'') FROM fut_transfers WHERE status='pending' AND updated_at < now() - interval '3 seconds' ORDER BY created_at LIMIT 50`)
		if err != nil {
			continue
		}
		type tr struct {
			ref, uid, dir, wallet, asset, from, to string
			amt                                    decimal.Decimal
		}
		var list []tr
		for rows.Next() {
			var x tr
			if rows.Scan(&x.ref, &x.uid, &x.dir, &x.amt, &x.wallet, &x.asset, &x.from, &x.to) == nil {
				list = append(list, x)
			}
		}
		rows.Close()
		for _, x := range list {
			switch x.dir {
			case "in":
				s.completeIn(ctx, x.ref, x.uid, x.wallet, x.amt)
			case "internal":
				s.completeInternal(ctx, x.ref, x.uid, x.asset, x.from, x.to, x.amt)
			default:
				s.completeOut(ctx, x.ref, x.uid, x.wallet, x.amt)
			}
		}
	}
}
