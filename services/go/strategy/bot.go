package main

import (
	"context"
	"database/sql"
	"errors"
	"fmt"
	"log"
	"sync"
	"time"

	"github.com/shopspring/decimal"
)

type Server struct {
	cfg      Config
	db       *sql.DB
	mu       sync.Mutex // serialises all bot state changes (poller + API)
	lastSnap map[int64]time.Time
}

func NewServer(cfg Config, db *sql.DB) *Server {
	return &Server{cfg: cfg, db: db, lastSnap: map[int64]time.Time{}}
}

type Bot struct {
	ID          int64
	UserID      string
	Symbol      string
	Base, Quote string
	Mode        string
	Lower       decimal.Decimal
	Upper       decimal.Decimal
	Grids       int
	Investment  decimal.Decimal
	Qty         decimal.Decimal
	StopLoss    decimal.NullDecimal
	TakeProfit  decimal.NullDecimal
	SellOnStop  bool
	StartPrice  decimal.Decimal
	Status      string
	StopReason  sql.NullString
	BaseQty     decimal.Decimal
	QuoteBal    decimal.Decimal
	GridProfit  decimal.Decimal
	FeesQuote   decimal.Decimal
	FeesBase    decimal.Decimal
	Cycles      int
	PeakValue   decimal.Decimal
	MaxDD       decimal.Decimal
	FinalValue  decimal.NullDecimal
	FinalPrice  decimal.NullDecimal
	CreatedAt   time.Time
	StoppedAt   sql.NullTime
}

const botCols = `id,user_id,symbol,base_asset,quote_asset,mode,lower_price,upper_price,grids,investment,qty_per_grid,
	stop_loss,take_profit,sell_on_stop,start_price,status,stop_reason,base_qty,quote_bal,grid_profit,fees_quote,fees_base,
	cycles,peak_value,max_drawdown,final_value,final_price,created_at,stopped_at`

func scanBot(sc interface{ Scan(...any) error }) (Bot, error) {
	var b Bot
	err := sc.Scan(&b.ID, &b.UserID, &b.Symbol, &b.Base, &b.Quote, &b.Mode, &b.Lower, &b.Upper, &b.Grids, &b.Investment, &b.Qty,
		&b.StopLoss, &b.TakeProfit, &b.SellOnStop, &b.StartPrice, &b.Status, &b.StopReason, &b.BaseQty, &b.QuoteBal, &b.GridProfit,
		&b.FeesQuote, &b.FeesBase, &b.Cycles, &b.PeakValue, &b.MaxDD, &b.FinalValue, &b.FinalPrice, &b.CreatedAt, &b.StoppedAt)
	return b, err
}

func (s *Server) bots(ctx context.Context, where string, args ...any) ([]Bot, error) {
	rows, err := s.db.QueryContext(ctx, `SELECT `+botCols+` FROM grid_bots `+where, args...)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	var out []Bot
	for rows.Next() {
		b, err := scanBot(rows)
		if err != nil {
			return nil, err
		}
		out = append(out, b)
	}
	return out, rows.Err()
}

func (s *Server) bot(ctx context.Context, id int64) (Bot, error) {
	return scanBot(s.db.QueryRowContext(ctx, `SELECT `+botCols+` FROM grid_bots WHERE id=$1`, id))
}

// Value is the bot's mark-to-market equity in quote.
func (b Bot) Value(p decimal.Decimal) decimal.Decimal { return b.QuoteBal.Add(b.BaseQty.Mul(p)) }

type GridOrder struct {
	ID       int64
	BotID    int64
	Level    int
	Side     string
	Kind     string
	Price    decimal.NullDecimal
	Quantity decimal.Decimal
	Cost     decimal.Decimal
	OrderID  string
	Status   string
}

func (s *Server) openOrders(ctx context.Context, botID int64) ([]GridOrder, error) {
	rows, err := s.db.QueryContext(ctx, `SELECT id,bot_id,level,side,kind,price,quantity,cost,order_id,status
		FROM grid_orders WHERE bot_id=$1 AND status='open' ORDER BY id`, botID)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	var out []GridOrder
	for rows.Next() {
		var g GridOrder
		if err := rows.Scan(&g.ID, &g.BotID, &g.Level, &g.Side, &g.Kind, &g.Price, &g.Quantity, &g.Cost, &g.OrderID, &g.Status); err != nil {
			return nil, err
		}
		out = append(out, g)
	}
	return out, rows.Err()
}

// ---- errors surfaced to the API ----

type userErr struct{ Code, Msg string }

func (e *userErr) Error() string { return e.Code + ": " + e.Msg }
func bad(code, msg string) error { return &userErr{code, msg} }

// ---- order placement ----

// placeLimit places one grid limit order and records it.
func (s *Server) placeLimit(ctx context.Context, b Bot, level int, side string, price, qty, cost decimal.Decimal) error {
	ps, qs := price.String(), qty.String()
	o, err := s.place(ctx, b.UserID, placeReq{Symbol: b.Symbol, Side: side, Type: "limit", Price: &ps, Quantity: &qs,
		ClientOrderID: fmt.Sprintf("grid-%d-%d-%d", b.ID, level, time.Now().UnixNano()%1e9)})
	if err != nil {
		return err
	}
	_, err = s.db.ExecContext(ctx, `INSERT INTO grid_orders (bot_id,level,side,kind,price,quantity,cost,order_id)
		VALUES ($1,$2,$3,'grid',$4,$5,$6,$7)`, b.ID, level, side, price, qty, cost, o.OrderID)
	return err
}

// marketOrder places a market order, waits for it to finish and books its fills.
func (s *Server) marketOrder(ctx context.Context, b *Bot, kind, side string, qty, quote decimal.Decimal) (orderInfo, error) {
	r := placeReq{Symbol: b.Symbol, Side: side, Type: "market"}
	if side == "buy" {
		qs := quote.String()
		r.QuoteOrderQty = &qs
	} else {
		qs := qty.String()
		r.Quantity = &qs
	}
	o, err := s.place(ctx, b.UserID, r)
	if err != nil {
		return orderInfo{}, err
	}
	var rowID int64
	if err := s.db.QueryRowContext(ctx, `INSERT INTO grid_orders (bot_id,level,side,kind,quantity,order_id)
		VALUES ($1,-1,$2,$3,$4,$5) RETURNING id`, b.ID, side, kind, qty, o.OrderID).Scan(&rowID); err != nil {
		return orderInfo{}, err
	}
	info, err := s.waitFinal(ctx, b.UserID, o.OrderID)
	if err != nil {
		return info, err
	}
	return info, s.book(ctx, b, GridOrder{ID: rowID, Side: side, Kind: kind}, info, nil)
}

// book applies an order's (final) fills to the bot sub-ledger and closes the row.
func (s *Server) book(ctx context.Context, b *Bot, g GridOrder, info orderInfo, profit *decimal.Decimal) error {
	fq, fquote := info.FilledQuantity, info.FilledQuote
	if g.Side == "buy" {
		b.BaseQty = b.BaseQty.Add(fq).Sub(info.BaseFee)
		b.QuoteBal = b.QuoteBal.Sub(fquote).Sub(info.QuoteFee)
	} else {
		b.BaseQty = b.BaseQty.Sub(fq).Sub(info.BaseFee)
		b.QuoteBal = b.QuoteBal.Add(fquote).Sub(info.QuoteFee)
	}
	b.FeesBase = b.FeesBase.Add(info.BaseFee)
	b.FeesQuote = b.FeesQuote.Add(info.QuoteFee)
	if profit != nil {
		b.GridProfit = b.GridProfit.Add(*profit)
		b.Cycles++
	}
	status := "canceled"
	if info.Order.Status == "filled" {
		status = "filled"
	}
	tx, err := s.db.BeginTx(ctx, nil)
	if err != nil {
		return err
	}
	defer tx.Rollback()
	if _, err := tx.ExecContext(ctx, `UPDATE grid_orders SET status=$2,filled_qty=$3,filled_quote=$4,fee_base=$5,fee_quote=$6,
		profit=$7,done_at=now() WHERE id=$1`, g.ID, status, fq, fquote, info.BaseFee, info.QuoteFee, nullDec(profit)); err != nil {
		return err
	}
	if _, err := tx.ExecContext(ctx, `UPDATE grid_bots SET base_qty=$2,quote_bal=$3,grid_profit=$4,fees_quote=$5,fees_base=$6,cycles=$7
		WHERE id=$1`, b.ID, b.BaseQty, b.QuoteBal, b.GridProfit, b.FeesQuote, b.FeesBase, b.Cycles); err != nil {
		return err
	}
	return tx.Commit()
}

func nullDec(d *decimal.Decimal) any {
	if d == nil {
		return nil
	}
	return *d
}

// ---- create / start ----

type createReq struct {
	Symbol     string  `json:"symbol"`
	Mode       string  `json:"mode"`
	Lower      string  `json:"lowerPrice"`
	Upper      string  `json:"upperPrice"`
	Grids      int     `json:"gridCount"`
	Investment string  `json:"investment"`
	StopLoss   *string `json:"stopLossPrice"`
	TakeProfit *string `json:"takeProfitPrice"`
	SellOnStop *bool   `json:"sellBaseOnStop"`
}

type validated struct {
	sym          SymbolInfo
	mode         string
	lower, upper decimal.Decimal
	invest       decimal.Decimal
	sl, tp       decimal.NullDecimal
	levels       []decimal.Decimal
	price        decimal.Decimal
	plan         Plan
}

func parsePos(v, code, msg string) (decimal.Decimal, error) {
	d, err := decimal.NewFromString(v)
	if err != nil || d.Sign() <= 0 {
		return decimal.Zero, bad(code, msg)
	}
	return d, nil
}

func (s *Server) validate(ctx context.Context, r createReq) (validated, error) {
	var v validated
	var err error
	if r.Mode == "" {
		r.Mode = "arithmetic"
	}
	if r.Mode != "arithmetic" && r.Mode != "geometric" {
		return v, bad("INVALID_MODE", "网格类型须为 arithmetic(等差)或 geometric(等比)")
	}
	v.mode = r.Mode
	if v.sym, err = s.symbol(ctx, r.Symbol); err != nil {
		var ae *apiErr
		if errors.As(err, &ae) && ae.Status == 404 {
			return v, bad("SYMBOL_NOT_FOUND", "交易对不存在")
		}
		return v, err
	}
	if v.sym.Status != "trading" {
		return v, bad("SYMBOL_NOT_TRADING", "交易对暂停交易")
	}
	if v.lower, err = parsePos(r.Lower, "INVALID_RANGE", "价格下限不合法"); err != nil {
		return v, err
	}
	if v.upper, err = parsePos(r.Upper, "INVALID_RANGE", "价格上限不合法"); err != nil {
		return v, err
	}
	if !v.upper.GreaterThan(v.lower) {
		return v, bad("INVALID_RANGE", "价格上限须大于下限")
	}
	if v.upper.Div(v.lower).GreaterThan(s.cfg.MaxRangeRatio) {
		return v, bad("INVALID_RANGE", "价格区间过宽")
	}
	if r.Grids < s.cfg.MinGrids || r.Grids > s.cfg.MaxGrids {
		return v, bad("INVALID_GRID_COUNT", fmt.Sprintf("网格数须在 %d–%d 之间", s.cfg.MinGrids, s.cfg.MaxGrids))
	}
	if v.invest, err = parsePos(r.Investment, "INVALID_INVESTMENT", "投资额不合法"); err != nil {
		return v, err
	}
	if v.invest.LessThan(s.cfg.MinInvestment) {
		return v, bad("INVESTMENT_TOO_LOW", "投资额低于最小值 "+s.cfg.MinInvestment.String())
	}
	if v.levels, err = Levels(v.mode, v.lower, v.upper, r.Grids, v.sym.PricePrecision); err != nil {
		return v, bad("GRID_TOO_DENSE", "网格过密:价格精度不足以划分")
	}
	if lo, _ := GridProfitRange(v.levels, s.cfg.FeeRate); lo.Sign() <= 0 {
		return v, bad("GRID_TOO_DENSE", "网格过密:单格利润不足以覆盖手续费")
	}
	if v.price, err = s.lastPrice(ctx, v.sym.Symbol); err != nil {
		return v, bad("NO_PRICE", "暂无最新成交价")
	}
	if !v.price.GreaterThan(v.lower) || !v.price.LessThan(v.upper) {
		return v, bad("PRICE_OUT_OF_RANGE", "当前价格不在网格区间内")
	}
	if r.StopLoss != nil && *r.StopLoss != "" {
		d, err := parsePos(*r.StopLoss, "INVALID_STOP_LOSS", "止损价不合法")
		if err != nil {
			return v, err
		}
		if !d.LessThan(v.price) {
			return v, bad("INVALID_STOP_LOSS", "止损价须低于当前价")
		}
		v.sl = decimal.NullDecimal{Decimal: d, Valid: true}
	}
	if r.TakeProfit != nil && *r.TakeProfit != "" {
		d, err := parsePos(*r.TakeProfit, "INVALID_TAKE_PROFIT", "止盈价不合法")
		if err != nil {
			return v, err
		}
		if !d.GreaterThan(v.price) {
			return v, bad("INVALID_TAKE_PROFIT", "止盈价须高于当前价")
		}
		v.tp = decimal.NullDecimal{Decimal: d, Valid: true}
	}
	v.plan = MakePlan(v.levels, v.price, v.invest, s.cfg.FeeRate, s.cfg.BuyBuffer, v.sym.QuantityPrecision)
	q := v.plan.Qty
	if q.LessThan(v.sym.MinQuantity) || q.Mul(v.levels[0]).LessThan(v.sym.MinNotional) {
		return v, bad("INVESTMENT_TOO_LOW", "投资额不足:每格下单量低于最小下单量/额")
	}
	if len(v.plan.SellLevels) > 0 && v.plan.InitQuote.LessThan(v.sym.MinNotional) {
		return v, bad("INVESTMENT_TOO_LOW", "投资额不足:初始买入额低于最小下单额")
	}
	return v, nil
}

func (s *Server) Create(ctx context.Context, uid string, r createReq) (Bot, error) {
	s.mu.Lock()
	defer s.mu.Unlock()
	var n int
	if err := s.db.QueryRowContext(ctx, `SELECT count(*) FROM grid_bots WHERE user_id=$1 AND status<>'stopped'`, uid).Scan(&n); err != nil {
		return Bot{}, err
	}
	if n >= s.cfg.MaxBotsPerUser {
		return Bot{}, bad("TOO_MANY_BOTS", fmt.Sprintf("最多同时运行 %d 个网格", s.cfg.MaxBotsPerUser))
	}
	v, err := s.validate(ctx, r)
	if err != nil {
		return Bot{}, err
	}
	sell := s.cfg.DefaultSellBase
	if r.SellOnStop != nil {
		sell = *r.SellOnStop
	}
	var id int64
	err = s.db.QueryRowContext(ctx, `INSERT INTO grid_bots (user_id,symbol,base_asset,quote_asset,mode,lower_price,upper_price,grids,
		investment,qty_per_grid,stop_loss,take_profit,sell_on_stop,start_price,status,quote_bal,peak_value)
		VALUES ($1,$2,$3,$4,$5,$6,$7,$8,$9,$10,$11,$12,$13,$14,'starting',$9,$9) RETURNING id`,
		uid, v.sym.Symbol, v.sym.Base, v.sym.Quote, v.mode, v.lower, v.upper, r.Grids, v.invest, v.plan.Qty, v.sl, v.tp, sell, v.price).Scan(&id)
	if err != nil {
		return Bot{}, err
	}
	b, err := s.bot(ctx, id)
	if err != nil {
		return b, err
	}
	if err := s.start(ctx, &b, v); err != nil {
		log.Printf("grid %d start failed: %v", id, err)
		s.stopLocked(ctx, b.ID, "start_failed", true)
		// Nothing ever reached the order book: drop the bot instead of keeping
		// an empty "stopped" record in the user's history.
		_, _ = s.db.ExecContext(ctx, `DELETE FROM grid_bots WHERE id=$1 AND NOT EXISTS (SELECT 1 FROM grid_orders WHERE bot_id=$1)`, b.ID)
		var ae *apiErr
		if errors.As(err, &ae) && ae.Status/100 == 4 {
			if ae.Code == "INSUFFICIENT_BALANCE" {
				return Bot{}, bad("INSUFFICIENT_BALANCE", "可用余额不足")
			}
			return Bot{}, bad("ORDER_REJECTED", ae.Message)
		}
		return Bot{}, err
	}
	if _, err := s.db.ExecContext(ctx, `UPDATE grid_bots SET status='running' WHERE id=$1`, id); err != nil {
		return Bot{}, err
	}
	return s.bot(ctx, id)
}

func (s *Server) start(ctx context.Context, b *Bot, v validated) error {
	q := v.plan.Qty
	// 1) Buy the base that backs the sell side.
	sells := v.plan.SellLevels
	var costPerUnit decimal.Decimal
	if len(sells) > 0 {
		info, err := s.marketOrder(ctx, b, "init", "buy", decimal.Zero, v.plan.InitQuote)
		if err != nil {
			return err
		}
		net := info.FilledQuantity.Sub(info.BaseFee)
		if net.Sign() <= 0 {
			return bad("NO_LIQUIDITY", "初始买入未成交")
		}
		costPerUnit = info.FilledQuote.Add(info.QuoteFee).Div(net)
	}
	// 2) Sells above P, each q (the last may be smaller if the buy filled short).
	avail := floorStep(b.BaseQty, v.sym.QuantityPrecision)
	for _, i := range sells {
		qty := decimal.Min(q, avail)
		if qty.LessThan(v.sym.MinQuantity) || qty.Mul(v.levels[i]).LessThan(v.sym.MinNotional) {
			break
		}
		if err := s.placeLimit(ctx, *b, i, "sell", v.levels[i], qty, qty.Mul(costPerUnit).Round(18)); err != nil {
			return err
		}
		avail = avail.Sub(qty)
	}
	// 3) Buys below P.
	for _, i := range v.plan.BuyLevels {
		if err := s.placeLimit(ctx, *b, i, "buy", v.levels[i], q, decimal.Zero); err != nil {
			return err
		}
	}
	return nil
}

// ---- running ----

func (s *Server) Run(ctx context.Context) {
	t := time.NewTicker(s.cfg.Poll)
	defer t.Stop()
	for {
		select {
		case <-ctx.Done():
			return
		case <-t.C:
			s.Tick(ctx)
		}
	}
}

// Tick processes fills and SL/TP for every running bot, and resumes any bot
// left in "stopping" (e.g. after a restart).
func (s *Server) Tick(ctx context.Context) {
	s.mu.Lock()
	defer s.mu.Unlock()
	bs, err := s.bots(ctx, `WHERE status IN ('running','stopping') ORDER BY id`)
	if err != nil {
		log.Printf("grid tick: %v", err)
		return
	}
	for _, b := range bs {
		if b.Status == "stopping" {
			s.stopLocked(ctx, b.ID, b.StopReason.String, b.SellOnStop)
			continue
		}
		if err := s.tickBot(ctx, b); err != nil {
			log.Printf("grid %d tick: %v", b.ID, err)
		}
	}
}

func (s *Server) levelsOf(b Bot, sym SymbolInfo) ([]decimal.Decimal, error) {
	return Levels(b.Mode, b.Lower, b.Upper, b.Grids, sym.PricePrecision)
}

func (s *Server) tickBot(ctx context.Context, b Bot) error {
	p, err := s.lastPrice(ctx, b.Symbol)
	if err == nil {
		if b.StopLoss.Valid && !p.GreaterThan(b.StopLoss.Decimal) {
			s.stopLocked(ctx, b.ID, "stop_loss", b.SellOnStop)
			return nil
		}
		if b.TakeProfit.Valid && !p.LessThan(b.TakeProfit.Decimal) {
			s.stopLocked(ctx, b.ID, "take_profit", b.SellOnStop)
			return nil
		}
	}
	oo, err := s.openOrders(ctx, b.ID)
	if err != nil {
		return err
	}
	var sym SymbolInfo
	var levels []decimal.Decimal
	for _, g := range oo {
		info, err := s.orderInfo(ctx, b.UserID, g.OrderID)
		if err != nil {
			return err
		}
		if !info.final() {
			continue
		}
		if info.Order.Status != "filled" {
			// Cancelled/rejected outside the bot: book partial fills and stop.
			if err := s.book(ctx, &b, g, info, nil); err != nil {
				return err
			}
			s.stopLocked(ctx, b.ID, "order_canceled", b.SellOnStop)
			return nil
		}
		if levels == nil {
			if sym, err = s.symbol(ctx, b.Symbol); err != nil {
				return err
			}
			if levels, err = s.levelsOf(b, sym); err != nil {
				return err
			}
		}
		if g.Side == "buy" {
			if err := s.book(ctx, &b, g, info, nil); err != nil {
				return err
			}
			net := info.FilledQuantity.Sub(info.BaseFee)
			qty := floorStep(net, sym.QuantityPrecision)
			if g.Level+1 >= len(levels) || qty.LessThan(sym.MinQuantity) {
				continue
			}
			cost := info.FilledQuote.Add(info.QuoteFee).Mul(qty).Div(net).Round(18)
			if err := s.placeLimit(ctx, b, g.Level+1, "sell", levels[g.Level+1], qty, cost); err != nil {
				log.Printf("grid %d replace sell: %v", b.ID, err)
				s.stopLocked(ctx, b.ID, "place_failed", b.SellOnStop)
				return nil
			}
		} else {
			profit := info.FilledQuote.Sub(info.QuoteFee).Sub(g.Cost).Sub(info.BaseFee.Mul(levels[g.Level]))
			if err := s.book(ctx, &b, g, info, &profit); err != nil {
				return err
			}
			if g.Level-1 < 0 {
				continue
			}
			if err := s.placeLimit(ctx, b, g.Level-1, "buy", levels[g.Level-1], b.Qty, decimal.Zero); err != nil {
				log.Printf("grid %d replace buy: %v", b.ID, err)
				s.stopLocked(ctx, b.ID, "place_failed", b.SellOnStop)
				return nil
			}
		}
	}
	if err == nil && p.Sign() > 0 {
		return s.markValue(ctx, &b, p)
	}
	return nil
}

// markValue tracks peak equity / max drawdown and samples the equity curve.
func (s *Server) markValue(ctx context.Context, b *Bot, p decimal.Decimal) error {
	v := b.Value(p)
	if v.GreaterThan(b.PeakValue) {
		b.PeakValue = v
	}
	if b.PeakValue.Sign() > 0 {
		if dd := b.PeakValue.Sub(v).Div(b.PeakValue); dd.GreaterThan(b.MaxDD) {
			b.MaxDD = dd.Round(8)
		}
	}
	if _, err := s.db.ExecContext(ctx, `UPDATE grid_bots SET peak_value=$2,max_drawdown=$3 WHERE id=$1`, b.ID, b.PeakValue, b.MaxDD); err != nil {
		return err
	}
	if time.Since(s.lastSnap[b.ID]) >= s.cfg.SnapshotEvery {
		s.lastSnap[b.ID] = time.Now()
		_, err := s.db.ExecContext(ctx, `INSERT INTO grid_snapshots (bot_id,value,price) VALUES ($1,$2,$3)`, b.ID, v, p)
		return err
	}
	return nil
}

// ---- stop ----

func (s *Server) Stop(ctx context.Context, uid string, id int64, sellBase *bool) (Bot, error) {
	s.mu.Lock()
	defer s.mu.Unlock()
	b, err := s.bot(ctx, id)
	if errors.Is(err, sql.ErrNoRows) || (err == nil && b.UserID != uid) {
		return Bot{}, bad("BOT_NOT_FOUND", "网格不存在")
	}
	if err != nil {
		return Bot{}, err
	}
	if b.Status == "stopped" {
		return Bot{}, bad("BOT_NOT_RUNNING", "网格已停止")
	}
	sell := b.SellOnStop
	if sellBase != nil {
		sell = *sellBase
	}
	s.stopLocked(ctx, id, "manual", sell)
	return s.bot(ctx, id)
}

// stopLocked cancels every open grid order (booking any partial fills),
// optionally market-sells the base the bot holds, and freezes the final value.
// It is resumable: a failure leaves the bot in "stopping" for the next tick.
func (s *Server) stopLocked(ctx context.Context, id int64, reason string, sell bool) {
	if err := s.stopInner(ctx, id, reason, sell); err != nil {
		log.Printf("grid %d stop (%s): %v", id, reason, err)
	}
}

func (s *Server) stopInner(ctx context.Context, id int64, reason string, sell bool) error {
	if _, err := s.db.ExecContext(ctx, `UPDATE grid_bots SET status='stopping', stop_reason=COALESCE(stop_reason,$2), sell_on_stop=$3
		WHERE id=$1 AND status<>'stopped'`, id, reason, sell); err != nil {
		return err
	}
	b, err := s.bot(ctx, id)
	if err != nil {
		return err
	}
	oo, err := s.openOrders(ctx, id)
	if err != nil {
		return err
	}
	for _, g := range oo {
		if err := s.cancel(ctx, b.UserID, g.OrderID); err != nil {
			var ae *apiErr
			if !errors.As(err, &ae) || ae.Status/100 != 4 {
				return err
			}
		}
		info, err := s.waitFinal(ctx, b.UserID, g.OrderID)
		if err != nil {
			return err
		}
		var profit *decimal.Decimal
		if g.Side == "sell" && info.FilledQuantity.Sign() > 0 && g.Quantity.Sign() > 0 {
			pr := info.FilledQuote.Sub(info.QuoteFee).Sub(g.Cost.Mul(info.FilledQuantity).Div(g.Quantity))
			if info.Order.Status == "filled" {
				profit = &pr
			} else {
				// A partial sell is still realised grid profit, but not a full cycle.
				b.GridProfit = b.GridProfit.Add(pr)
			}
		}
		if err := s.book(ctx, &b, g, info, profit); err != nil {
			return err
		}
	}
	if b.SellOnStop && b.BaseQty.Sign() > 0 {
		sym, err := s.symbol(ctx, b.Symbol)
		if err != nil {
			return err
		}
		qty := floorStep(b.BaseQty, sym.QuantityPrecision)
		p, perr := s.lastPrice(ctx, b.Symbol)
		if qty.GreaterThanOrEqual(sym.MinQuantity) && (perr != nil || qty.Mul(p).GreaterThanOrEqual(sym.MinNotional)) {
			if _, err := s.marketOrder(ctx, &b, "close", "sell", qty, decimal.Zero); err != nil {
				var ae *apiErr
				if !errors.As(err, &ae) {
					return err
				}
				log.Printf("grid %d close sell rejected: %v", id, err)
			}
		}
	}
	p, err := s.lastPrice(ctx, b.Symbol)
	if err != nil {
		p = b.StartPrice
	}
	_, err = s.db.ExecContext(ctx, `UPDATE grid_bots SET status='stopped', stopped_at=now(), final_price=$2, final_value=$3
		WHERE id=$1`, id, p, b.Value(p))
	delete(s.lastSnap, id)
	return err
}
