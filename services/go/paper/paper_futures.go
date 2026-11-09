package main

import (
	"context"
	"database/sql"
	"fmt"
	"strings"
	"time"

	"github.com/shopspring/decimal"
)

// Paper USDX perpetuals. Prices and contract specs are supplied by FutSource,
// which must read the live futures service. This file never writes fut_accounts,
// the asset ledger, or the live matcher.

var paperFutBases = []string{"BTC", "ETH", "BNB", "ARB", "XRP", "SOL"}

const (
	paperFutInitial   = "10000"
	paperFutClaimCap  = "100000"
	paperFutMaxBal    = "1000000"
	paperFutClaimWait = time.Minute
)

type FutSpec struct {
	Symbol            string          `json:"symbol"`
	LiveSymbol        string          `json:"liveSymbol"`
	Base              string          `json:"baseAsset"`
	Quote             string          `json:"quoteAsset"`
	TickSize          decimal.Decimal `json:"tickSize"`
	MinQty            decimal.Decimal `json:"minQty"`
	MaxQty            decimal.Decimal `json:"maxQty"`
	MMR               decimal.Decimal `json:"maintenanceMarginRate"`
	MaxLeverage       int             `json:"maxLeverage"`
	MakerFee          decimal.Decimal `json:"makerFeeRate"`
	TakerFee          decimal.Decimal `json:"takerFeeRate"`
	FundingIntervalH  int             `json:"fundingIntervalHours"`
	Tiers             any             `json:"riskTiers"`
	Available         bool            `json:"available"`
	UnavailableReason string          `json:"unavailableReason,omitempty"`
	Mark              decimal.Decimal `json:"markPrice"`
	Index             decimal.Decimal `json:"indexPrice"`
	FundingRate       *string         `json:"fundingRate"`
	FundingAvailable  bool            `json:"fundingAvailable"`
	PriceSource       string          `json:"priceSource"`
}

type FutSource interface {
	Specs(ctx context.Context) ([]FutSpec, error)
	Quote(ctx context.Context, liveSymbol string) (mark, index decimal.Decimal, funding *decimal.Decimal, ok bool, err error)
}

type futOrderIn struct {
	Symbol        string
	Side          string
	PositionSide  string
	Action        string
	Type          string
	Price         decimal.Decimal
	Trigger       decimal.Decimal
	SizeMode      string
	Quantity      decimal.Decimal
	Cost          decimal.Decimal
	Notional      decimal.Decimal
	Leverage      int
	MarginMode    string
	TP            decimal.Decimal
	SL            decimal.Decimal
	HasTP         bool
	HasSL         bool
	ClientOrderID string
}

func (s *Store) ensureFutAccount(ctx context.Context, tx *sql.Tx, uid string) error {
	_, err := tx.ExecContext(ctx, `INSERT INTO paper_fut_accounts (user_id, balance) VALUES ($1, $2) ON CONFLICT DO NOTHING`, uid, paperFutInitial)
	return err
}

func (s *Store) FutAccountView(ctx context.Context, uid string, src FutSource) (map[string]any, error) {
	tx, err := s.db.BeginTx(ctx, nil)
	if err != nil {
		return nil, err
	}
	defer tx.Rollback()
	if err := s.ensureFutAccount(ctx, tx, uid); err != nil {
		return nil, err
	}
	if err := tx.Commit(); err != nil {
		return nil, err
	}
	var bal, frozen decimal.Decimal
	var mode string
	var gen int64
	if err := s.db.QueryRowContext(ctx, `SELECT balance, frozen, position_mode, generation FROM paper_fut_accounts WHERE user_id=$1`, uid).Scan(&bal, &frozen, &mode, &gen); err != nil {
		return nil, err
	}
	upnl := decimal.Zero
	rows, err := s.db.QueryContext(ctx, `SELECT symbol, qty, entry_price FROM paper_fut_positions WHERE user_id=$1 AND qty <> 0`, uid)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	for rows.Next() {
		var sym string
		var qty, entry decimal.Decimal
		if err := rows.Scan(&sym, &qty, &entry); err != nil {
			return nil, err
		}
		live := strings.TrimSuffix(sym, "USDX") + "USDT"
		mark, _, _, ok, qerr := src.Quote(ctx, live)
		if qerr != nil || !ok || !mark.IsPositive() {
			continue
		}
		upnl = upnl.Add(mark.Sub(entry).Mul(qty))
	}
	avail := bal.Sub(frozen)
	if avail.IsNegative() {
		avail = decimal.Zero
	}
	return map[string]any{
		"asset": "USDX", "balance": bal.StringFixed(8), "available": avail.StringFixed(8),
		"frozen": frozen.StringFixed(8), "unrealizedPnl": upnl.StringFixed(8),
		"positionMode": mode, "generation": gen, "initialBalance": paperFutInitial,
		"claimCap": paperFutClaimCap, "maxBalance": paperFutMaxBal,
		"isolated": true,
	}, rows.Err()
}

func (s *Store) FutClaim(ctx context.Context, uid string, amount decimal.Decimal) (map[string]any, error) {
	capD := decimal.RequireFromString(paperFutClaimCap)
	maxD := decimal.RequireFromString(paperFutMaxBal)
	if !amount.IsPositive() || amount.GreaterThan(capD) {
		return nil, apiErr(400, "VALIDATION_ERROR", "领取金额必须大于 0 且不超过 "+paperFutClaimCap+" USDX")
	}
	tx, err := s.db.BeginTx(ctx, nil)
	if err != nil {
		return nil, err
	}
	defer tx.Rollback()
	if err := s.ensureFutAccount(ctx, tx, uid); err != nil {
		return nil, err
	}
	var bal decimal.Decimal
	if err := tx.QueryRowContext(ctx, `SELECT balance FROM paper_fut_accounts WHERE user_id=$1 FOR UPDATE`, uid).Scan(&bal); err != nil {
		return nil, err
	}
	var last sql.NullTime
	_ = tx.QueryRowContext(ctx, `SELECT max(created_at) FROM paper_fut_claims WHERE user_id=$1`, uid).Scan(&last)
	if last.Valid && time.Since(last.Time) < paperFutClaimWait {
		return nil, apiErr(429, "RATE_LIMITED", "领取过于频繁")
	}
	next := bal.Add(amount)
	if next.GreaterThan(maxD) {
		return nil, apiErr(409, "PAPER_FUTURES_CAP", "虚拟 USDX 余额不能超过 "+paperFutMaxBal)
	}
	if _, err := tx.ExecContext(ctx, `UPDATE paper_fut_accounts SET balance=$2, updated_at=now() WHERE user_id=$1`, uid, next); err != nil {
		return nil, err
	}
	if _, err := tx.ExecContext(ctx, `INSERT INTO paper_fut_claims (user_id, amount) VALUES ($1,$2)`, uid, amount); err != nil {
		return nil, err
	}
	ref := randomID("claim")
	if _, err := tx.ExecContext(ctx, `INSERT INTO paper_fut_ledger (user_id, kind, amount, balance_after, ref) VALUES ($1,'claim',$2,$3,$4)`, uid, amount, next, ref); err != nil {
		return nil, err
	}
	if err := tx.Commit(); err != nil {
		return nil, err
	}
	return map[string]any{"balance": next.StringFixed(8), "credited": amount.StringFixed(8), "asset": "USDX"}, nil
}

func (s *Store) FutReset(ctx context.Context, uid, confirm string) error {
	if confirm != "RESET_PAPER_FUTURES" {
		return apiErr(400, "VALIDATION_ERROR", "请传入 confirmation=RESET_PAPER_FUTURES")
	}
	tx, err := s.db.BeginTx(ctx, nil)
	if err != nil {
		return err
	}
	defer tx.Rollback()
	if err := s.ensureFutAccount(ctx, tx, uid); err != nil {
		return err
	}
	if _, err := tx.ExecContext(ctx, `SELECT user_id FROM paper_fut_accounts WHERE user_id=$1 FOR UPDATE`, uid); err != nil {
		return err
	}
	init := decimal.RequireFromString(paperFutInitial)
	if _, err := tx.ExecContext(ctx, `DELETE FROM paper_fut_orders WHERE user_id=$1 AND status IN ('open','untriggered')`, uid); err != nil {
		return err
	}
	if _, err := tx.ExecContext(ctx, `DELETE FROM paper_fut_positions WHERE user_id=$1`, uid); err != nil {
		return err
	}
	if _, err := tx.ExecContext(ctx, `UPDATE paper_fut_accounts SET balance=$2, frozen=0, generation=generation+1, updated_at=now() WHERE user_id=$1`, uid, init); err != nil {
		return err
	}
	ref := randomID("reset")
	if _, err := tx.ExecContext(ctx, `INSERT INTO paper_fut_ledger (user_id, kind, amount, balance_after, ref) VALUES ($1,'reset',$2,$2,$3)`, uid, init, ref); err != nil {
		return err
	}
	return tx.Commit()
}

func liqPrice(entry, margin, qty, mmr decimal.Decimal, lev int) decimal.Decimal {
	if !entry.IsPositive() || qty.IsZero() || lev < 1 {
		return decimal.Zero
	}
	q := qty.Abs()
	if margin.IsZero() {
		margin = entry.Mul(q).Div(decimal.NewFromInt(int64(lev)))
	}
	if qty.IsPositive() {
		den := q.Mul(decimal.NewFromInt(1).Sub(mmr))
		if !den.IsPositive() {
			return decimal.Zero
		}
		// margin + (mark-entry)*q = q*mark*mmr
		return margin.Sub(entry.Mul(q)).Div(q.Mul(mmr.Sub(decimal.NewFromInt(1))))
	}
	return margin.Add(entry.Mul(q)).Div(q.Mul(decimal.NewFromInt(1).Add(mmr)))
}

func (s *Store) specBySymbol(ctx context.Context, src FutSource, symbol string) (FutSpec, error) {
	symbol = strings.ToUpper(strings.TrimSpace(symbol))
	specs, err := src.Specs(ctx)
	if err != nil {
		return FutSpec{}, apiErr(503, "MARKET_UNAVAILABLE", "无法读取真实合约规格")
	}
	for _, sp := range specs {
		if sp.Symbol == symbol {
			return sp, nil
		}
	}
	return FutSpec{}, apiErr(404, "SYMBOL_UNAVAILABLE", "该模拟合约没有真实行情源")
}

func (s *Store) PlaceFutOrder(ctx context.Context, uid string, in futOrderIn, src FutSource) (map[string]any, error) {
	in.Symbol = strings.ToUpper(in.Symbol)
	in.Side = strings.ToLower(in.Side)
	in.Type = strings.ToLower(in.Type)
	in.Action = strings.ToLower(in.Action)
	in.MarginMode = strings.ToLower(in.MarginMode)
	in.PositionSide = strings.ToUpper(in.PositionSide)
	if in.PositionSide == "" {
		in.PositionSide = "BOTH"
	}
	if in.ClientOrderID == "" {
		in.ClientOrderID = randomID("c")
	}
	sp, err := s.specBySymbol(ctx, src, in.Symbol)
	if err != nil {
		return nil, err
	}
	if !sp.Available {
		return nil, apiErr(409, "SYMBOL_UNAVAILABLE", sp.UnavailableReason)
	}
	mark, index, _, ok, err := src.Quote(ctx, sp.LiveSymbol)
	if err != nil || !ok || !mark.IsPositive() {
		return nil, apiErr(503, "MARK_UNAVAILABLE", "真实标记价格不可用，拒绝模拟下单")
	}
	_ = index
	if in.Leverage < 1 || in.Leverage > sp.MaxLeverage {
		return nil, apiErr(400, "VALIDATION_ERROR", "杠杆超出该合约真实档位上限")
	}
	if in.MarginMode != "cross" && in.MarginMode != "isolated" {
		return nil, apiErr(400, "VALIDATION_ERROR", "保证金模式只能是 cross 或 isolated")
	}
	if in.Type != "market" && in.Type != "limit" && in.Type != "plan" && in.Type != "post_only" {
		return nil, apiErr(400, "VALIDATION_ERROR", "订单类型不支持")
	}
	if in.Action != "open" && in.Action != "close" {
		return nil, apiErr(400, "VALIDATION_ERROR", "action 只能是 open 或 close")
	}
	qty, err := sizeToQty(in, mark, sp)
	if err != nil {
		return nil, err
	}
	tx, err := s.db.BeginTx(ctx, nil)
	if err != nil {
		return nil, err
	}
	defer tx.Rollback()
	if err := s.ensureFutAccount(ctx, tx, uid); err != nil {
		return nil, err
	}
	var mode string
	if err := tx.QueryRowContext(ctx, `SELECT position_mode FROM paper_fut_accounts WHERE user_id=$1 FOR UPDATE`, uid).Scan(&mode); err != nil {
		return nil, err
	}
	var recent int
	if err := tx.QueryRowContext(ctx, `SELECT count(*) FROM paper_fut_orders WHERE user_id=$1 AND created_at > now() - interval '10 seconds'`, uid).Scan(&recent); err != nil {
		return nil, err
	}
	if recent >= 30 {
		return nil, apiErr(429, "RATE_LIMITED", "模拟合约下单过于频繁")
	}
	if mode == "one_way" && in.PositionSide != "BOTH" {
		return nil, apiErr(400, "POSITION_MODE", "合仓模式的 positionSide 必须是 BOTH")
	}
	if mode == "hedge" && in.PositionSide != "LONG" && in.PositionSide != "SHORT" {
		return nil, apiErr(400, "POSITION_MODE", "分仓模式的 positionSide 必须是 LONG 或 SHORT")
	}
	var existing string
	err = tx.QueryRowContext(ctx, `SELECT order_id FROM paper_fut_orders WHERE user_id=$1 AND client_order_id=$2`, uid, in.ClientOrderID).Scan(&existing)
	if err == nil {
		return map[string]any{"orderId": existing, "replayed": true}, nil
	}
	if err != sql.ErrNoRows {
		return nil, err
	}
	id := randomID("pf")
	status := "open"
	if in.Type == "plan" {
		status = "untriggered"
	}
	if in.Type == "market" || (in.Type == "limit" && crosses(in.Side, in.Price, mark)) || (in.Type == "post_only" && crosses(in.Side, in.Price, mark)) {
		if in.Type == "post_only" && crosses(in.Side, in.Price, mark) {
			return nil, apiErr(409, "POST_ONLY_REJECTED", "只做 Maker 订单会立即成交")
		}
		if in.Type == "market" || (in.Type == "limit" && crosses(in.Side, in.Price, mark)) {
			fillPx := mark
			if in.Type == "limit" {
				fillPx = in.Price
			}
			if err := s.fillLocked(ctx, tx, uid, id, in, sp, qty, fillPx); err != nil {
				return nil, err
			}
			status = "filled"
		}
	}
	if status != "filled" {
		margin := decimal.Zero
		if in.Action == "open" {
			margin = mark.Mul(qty).Div(decimal.NewFromInt(int64(in.Leverage)))
			if err := freeze(ctx, tx, uid, margin); err != nil {
				return nil, err
			}
		}
		_, err = tx.ExecContext(ctx, `INSERT INTO paper_fut_orders (order_id, user_id, client_order_id, symbol, side, position_side, action, order_type, price, trigger_price, qty, leverage, margin_mode, status, reduce_only, tp_trigger, sl_trigger)
			VALUES ($1,$2,$3,$4,$5,$6,$7,$8,$9,$10,$11,$12,$13,$14,$15,$16,$17)`,
			id, uid, in.ClientOrderID, in.Symbol, in.Side, in.PositionSide, in.Action, in.Type, nullDec(in.Price), nullDec(in.Trigger), qty, in.Leverage, in.MarginMode, status, in.Action == "close", nullDecOpt(in.TP, in.HasTP), nullDecOpt(in.SL, in.HasSL))
		if err != nil {
			return nil, err
		}
		_ = margin
	} else {
		_, err = tx.ExecContext(ctx, `INSERT INTO paper_fut_orders (order_id, user_id, client_order_id, symbol, side, position_side, action, order_type, price, trigger_price, qty, filled_qty, leverage, margin_mode, status, reduce_only, tp_trigger, sl_trigger)
			VALUES ($1,$2,$3,$4,$5,$6,$7,$8,$9,$10,$11,$11,$12,$13,'filled',$14,$15,$16)`,
			id, uid, in.ClientOrderID, in.Symbol, in.Side, in.PositionSide, in.Action, in.Type, nullDec(in.Price), nullDec(in.Trigger), qty, in.Leverage, in.MarginMode, in.Action == "close", nullDecOpt(in.TP, in.HasTP), nullDecOpt(in.SL, in.HasSL))
		if err != nil {
			return nil, err
		}
	}
	if err := tx.Commit(); err != nil {
		return nil, err
	}
	return map[string]any{"orderId": id, "status": status, "symbol": in.Symbol, "markPrice": mark.String(), "priceSource": "futures_mark"}, nil
}

func sizeToQty(in futOrderIn, mark decimal.Decimal, sp FutSpec) (decimal.Decimal, error) {
	var qty decimal.Decimal
	switch in.SizeMode {
	case "", "quantity":
		qty = in.Quantity
	case "notional":
		if !in.Notional.IsPositive() {
			return decimal.Zero, apiErr(400, "VALIDATION_ERROR", "notional 必须大于 0")
		}
		qty = in.Notional.Div(mark)
	case "cost":
		if !in.Cost.IsPositive() {
			return decimal.Zero, apiErr(400, "VALIDATION_ERROR", "cost 必须大于 0")
		}
		qty = in.Cost.Mul(decimal.NewFromInt(int64(in.Leverage))).Div(mark)
	default:
		return decimal.Zero, apiErr(400, "VALIDATION_ERROR", "sizeMode 只能是 quantity、cost 或 notional")
	}
	if sp.MinQty.IsPositive() {
		steps := qty.Div(sp.MinQty).Floor()
		qty = steps.Mul(sp.MinQty)
	}
	if !qty.IsPositive() || (sp.MinQty.IsPositive() && qty.LessThan(sp.MinQty)) {
		return decimal.Zero, apiErr(400, "VALIDATION_ERROR", "数量低于真实合约最小下单量")
	}
	if sp.MaxQty.IsPositive() && qty.GreaterThan(sp.MaxQty) {
		return decimal.Zero, apiErr(400, "VALIDATION_ERROR", "数量超过真实合约最大下单量")
	}
	return qty, nil
}

func crosses(side string, limit, mark decimal.Decimal) bool {
	if !limit.IsPositive() {
		return false
	}
	if side == "buy" {
		return mark.LessThanOrEqual(limit)
	}
	return mark.GreaterThanOrEqual(limit)
}

func nullDec(d decimal.Decimal) any {
	if !d.IsPositive() {
		return nil
	}
	return d
}
func nullDecOpt(d decimal.Decimal, ok bool) any {
	if !ok {
		return nil
	}
	return d
}

func freeze(ctx context.Context, tx *sql.Tx, uid string, margin decimal.Decimal) error {
	var bal, frozen decimal.Decimal
	if err := tx.QueryRowContext(ctx, `SELECT balance, frozen FROM paper_fut_accounts WHERE user_id=$1 FOR UPDATE`, uid).Scan(&bal, &frozen); err != nil {
		return err
	}
	if bal.Sub(frozen).LessThan(margin) {
		return apiErr(409, "INSUFFICIENT_MARGIN", "虚拟 USDX 可用保证金不足")
	}
	_, err := tx.ExecContext(ctx, `UPDATE paper_fut_accounts SET frozen=frozen+$2, updated_at=now() WHERE user_id=$1`, uid, margin)
	return err
}

func (s *Store) fillLocked(ctx context.Context, tx *sql.Tx, uid, orderID string, in futOrderIn, sp FutSpec, qty, px decimal.Decimal) error {
	sideQty := qty
	if in.Side == "sell" {
		sideQty = qty.Neg()
	}
	if in.Action == "close" {
		return s.reduce(ctx, tx, uid, orderID, in, qty, px)
	}
	notional := px.Mul(qty)
	margin := notional.Div(decimal.NewFromInt(int64(in.Leverage)))
	var bal, frozen decimal.Decimal
	if err := tx.QueryRowContext(ctx, `SELECT balance, frozen FROM paper_fut_accounts WHERE user_id=$1 FOR UPDATE`, uid).Scan(&bal, &frozen); err != nil {
		return err
	}
	if bal.Sub(frozen).LessThan(margin) {
		return apiErr(409, "INSUFFICIENT_MARGIN", "虚拟 USDX 可用保证金不足")
	}
	ps := in.PositionSide
	var haveQty, entry, iso decimal.Decimal
	err := tx.QueryRowContext(ctx, `SELECT qty, entry_price, isolated_margin FROM paper_fut_positions WHERE user_id=$1 AND symbol=$2 AND position_side=$3 FOR UPDATE`, uid, in.Symbol, ps).Scan(&haveQty, &entry, &iso)
	if err == sql.ErrNoRows {
		haveQty, entry, iso = decimal.Zero, decimal.Zero, decimal.Zero
		err = nil
	}
	if err != nil {
		return err
	}
	newQty := haveQty.Add(sideQty)
	newEntry := px
	if !haveQty.IsZero() && haveQty.Sign() == sideQty.Sign() {
		newEntry = haveQty.Abs().Mul(entry).Add(qty.Mul(px)).Div(newQty.Abs())
	}
	newIso := iso
	if in.MarginMode == "isolated" {
		newIso = iso.Add(margin)
		if _, err := tx.ExecContext(ctx, `UPDATE paper_fut_accounts SET balance=balance-$2, updated_at=now() WHERE user_id=$1`, uid, margin); err != nil {
			return err
		}
	} else if _, err := tx.ExecContext(ctx, `UPDATE paper_fut_accounts SET frozen=frozen+$2, updated_at=now() WHERE user_id=$1`, uid, margin); err != nil {
		return err
	}
	_, err = tx.ExecContext(ctx, `INSERT INTO paper_fut_positions (user_id, symbol, position_side, margin_mode, leverage, qty, entry_price, isolated_margin, tp_trigger, sl_trigger)
		VALUES ($1,$2,$3,$4,$5,$6,$7,$8,$9,$10)
		ON CONFLICT (user_id, symbol, position_side) DO UPDATE SET qty=$6, entry_price=$7, isolated_margin=$8, leverage=$5, margin_mode=$4, version=paper_fut_positions.version+1, tp_trigger=COALESCE($9, paper_fut_positions.tp_trigger), sl_trigger=COALESCE($10, paper_fut_positions.sl_trigger), updated_at=now()`,
		uid, in.Symbol, ps, in.MarginMode, in.Leverage, newQty, newEntry, newIso, nullDecOpt(in.TP, in.HasTP), nullDecOpt(in.SL, in.HasSL))
	if err != nil {
		return err
	}
	var after decimal.Decimal
	_ = tx.QueryRowContext(ctx, `SELECT balance FROM paper_fut_accounts WHERE user_id=$1`, uid).Scan(&after)
	if _, err := tx.ExecContext(ctx, `INSERT INTO paper_fut_trades (trade_id, order_id, user_id, symbol, side, price, qty, realized_pnl) VALUES ($1,$2,$3,$4,$5,$6,$7,0)`, randomID("pt"), orderID, uid, in.Symbol, in.Side, px, qty); err != nil {
		return err
	}
	_, err = tx.ExecContext(ctx, `INSERT INTO paper_fut_ledger (user_id, kind, amount, balance_after, ref) VALUES ($1,'open_margin',$2,$3,$4)`, uid, margin.Neg(), after, randomID("led"))
	return err
}

func (s *Store) reduce(ctx context.Context, tx *sql.Tx, uid, orderID string, in futOrderIn, qty, px decimal.Decimal) error {
	ps := in.PositionSide
	var have, entry, iso decimal.Decimal
	var mode string
	var lev int
	var ver int64
	err := tx.QueryRowContext(ctx, `SELECT qty, entry_price, isolated_margin, margin_mode, leverage, version FROM paper_fut_positions WHERE user_id=$1 AND symbol=$2 AND position_side=$3 FOR UPDATE`, uid, in.Symbol, ps).Scan(&have, &entry, &iso, &mode, &lev, &ver)
	if err == sql.ErrNoRows || have.IsZero() {
		return apiErr(409, "NO_POSITION", "没有可平的模拟仓位")
	}
	if err != nil {
		return err
	}
	if qty.GreaterThan(have.Abs()) {
		qty = have.Abs()
	}
	sign := decimal.NewFromInt(1)
	if have.IsNegative() {
		sign = decimal.NewFromInt(-1)
	}
	closed := qty
	pnl := px.Sub(entry).Mul(closed).Mul(sign)
	frac := closed.Div(have.Abs())
	release := decimal.Zero
	if mode == "isolated" {
		release = iso.Mul(frac)
		iso = iso.Sub(release)
	}
	newQty := have.Sub(sign.Mul(closed))
	if _, err := tx.ExecContext(ctx, `UPDATE paper_fut_positions SET qty=$4, isolated_margin=$5, realized_pnl=realized_pnl+$6, version=version+1, updated_at=now() WHERE user_id=$1 AND symbol=$2 AND position_side=$3 AND version=$7`, uid, in.Symbol, ps, newQty, iso, pnl, ver); err != nil {
		return err
	}
	if _, err := tx.ExecContext(ctx, `UPDATE paper_fut_accounts SET balance=balance+$2, updated_at=now() WHERE user_id=$1`, uid, release.Add(pnl)); err != nil {
		return err
	}
	if mode == "cross" {
		marginRelease := entry.Mul(closed).Div(decimal.NewFromInt(int64(lev)))
		if _, err := tx.ExecContext(ctx, `UPDATE paper_fut_accounts SET frozen=GREATEST(frozen-$2,0), updated_at=now() WHERE user_id=$1`, uid, marginRelease); err != nil {
			return err
		}
	}
	var after decimal.Decimal
	_ = tx.QueryRowContext(ctx, `SELECT balance FROM paper_fut_accounts WHERE user_id=$1`, uid).Scan(&after)
	if _, err := tx.ExecContext(ctx, `INSERT INTO paper_fut_trades (trade_id, order_id, user_id, symbol, side, price, qty, realized_pnl) VALUES ($1,$2,$3,$4,$5,$6,$7,$8)`, randomID("pt"), orderID, uid, in.Symbol, in.Side, px, closed, pnl); err != nil {
		return err
	}
	_, err = tx.ExecContext(ctx, `INSERT INTO paper_fut_ledger (user_id, kind, amount, balance_after, ref) VALUES ($1,'close_pnl',$2,$3,$4)`, uid, pnl, after, randomID("led"))
	return err
}

func (s *Store) CancelFutOrder(ctx context.Context, uid, orderID string) error {
	tx, err := s.db.BeginTx(ctx, nil)
	if err != nil {
		return err
	}
	defer tx.Rollback()
	var status, action, symbol string
	var qty, price decimal.Decimal
	var lev int
	err = tx.QueryRowContext(ctx, `SELECT status, action, symbol, qty, COALESCE(price,0), leverage FROM paper_fut_orders WHERE order_id=$1 AND user_id=$2 FOR UPDATE`, orderID, uid).Scan(&status, &action, &symbol, &qty, &price, &lev)
	if err == sql.ErrNoRows {
		return apiErr(404, "NOT_FOUND", "订单不存在")
	}
	if err != nil {
		return err
	}
	if status != "open" && status != "untriggered" {
		return apiErr(409, "ORDER_CLOSED", "订单已结束")
	}
	if action == "open" {
		px := price
		if !px.IsPositive() {
			px = decimal.NewFromInt(1)
		}
		margin := px.Mul(qty).Div(decimal.NewFromInt(int64(lev)))
		if _, err := tx.ExecContext(ctx, `UPDATE paper_fut_accounts SET frozen=GREATEST(frozen-$2,0) WHERE user_id=$1`, uid, margin); err != nil {
			return err
		}
	}
	if _, err := tx.ExecContext(ctx, `UPDATE paper_fut_orders SET status='canceled', updated_at=now() WHERE order_id=$1`, orderID); err != nil {
		return err
	}
	return tx.Commit()
}

func (s *Store) CancelAllFut(ctx context.Context, uid string) (int, error) {
	rows, err := s.db.QueryContext(ctx, `SELECT order_id FROM paper_fut_orders WHERE user_id=$1 AND status IN ('open','untriggered')`, uid)
	if err != nil {
		return 0, err
	}
	defer rows.Close()
	var ids []string
	for rows.Next() {
		var id string
		if err := rows.Scan(&id); err != nil {
			return 0, err
		}
		ids = append(ids, id)
	}
	n := 0
	for _, id := range ids {
		if err := s.CancelFutOrder(ctx, uid, id); err == nil {
			n++
		}
	}
	return n, rows.Err()
}

func (s *Store) ClosePosition(ctx context.Context, uid, symbol, positionSide string, src FutSource) error {
	sp, err := s.specBySymbol(ctx, src, symbol)
	if err != nil {
		return err
	}
	mark, _, _, ok, err := src.Quote(ctx, sp.LiveSymbol)
	if err != nil || !ok {
		return apiErr(503, "MARK_UNAVAILABLE", "真实标记价格不可用")
	}
	tx, err := s.db.BeginTx(ctx, nil)
	if err != nil {
		return err
	}
	defer tx.Rollback()
	if err := s.ensureFutAccount(ctx, tx, uid); err != nil {
		return err
	}
	var qty decimal.Decimal
	if err := tx.QueryRowContext(ctx, `SELECT qty FROM paper_fut_positions WHERE user_id=$1 AND symbol=$2 AND position_side=$3`, uid, strings.ToUpper(symbol), strings.ToUpper(positionSide)).Scan(&qty); err != nil {
		return apiErr(404, "NO_POSITION", "没有可平的模拟仓位")
	}
	side := "sell"
	if qty.IsNegative() {
		side = "buy"
	}
	in := futOrderIn{Symbol: sp.Symbol, Side: side, PositionSide: strings.ToUpper(positionSide), Action: "close", Type: "market", Leverage: 1, MarginMode: "cross"}
	if err := s.reduce(ctx, tx, uid, randomID("close"), in, qty.Abs(), mark); err != nil {
		return err
	}
	return tx.Commit()
}

func (s *Store) CloseAll(ctx context.Context, uid string, src FutSource) (int, error) {
	rows, err := s.db.QueryContext(ctx, `SELECT symbol, position_side FROM paper_fut_positions WHERE user_id=$1 AND qty <> 0`, uid)
	if err != nil {
		return 0, err
	}
	defer rows.Close()
	type key struct{ s, p string }
	var ks []key
	for rows.Next() {
		var k key
		if err := rows.Scan(&k.s, &k.p); err != nil {
			return 0, err
		}
		ks = append(ks, k)
	}
	n := 0
	for _, k := range ks {
		if err := s.ClosePosition(ctx, uid, k.s, k.p, src); err == nil {
			n++
		}
	}
	return n, rows.Err()
}

func (s *Store) SetTPSL(ctx context.Context, uid, symbol, positionSide string, tp, sl decimal.Decimal, hasTP, hasSL bool) error {
	res, err := s.db.ExecContext(ctx, `UPDATE paper_fut_positions SET tp_trigger=$4, sl_trigger=$5, version=version+1, updated_at=now() WHERE user_id=$1 AND symbol=$2 AND position_side=$3 AND qty <> 0`,
		uid, strings.ToUpper(symbol), strings.ToUpper(positionSide), nullDecOpt(tp, hasTP), nullDecOpt(sl, hasSL))
	if err != nil {
		return err
	}
	n, _ := res.RowsAffected()
	if n == 0 {
		return apiErr(404, "NO_POSITION", "没有可设置止盈止损的仓位")
	}
	return nil
}

func (s *Store) SetFutLeverage(ctx context.Context, uid, symbol string, lev int, src FutSource) error {
	sp, err := s.specBySymbol(ctx, src, symbol)
	if err != nil {
		return err
	}
	if !sp.Available || lev < 1 || lev > sp.MaxLeverage {
		return apiErr(400, "VALIDATION_ERROR", "杠杆超出真实合约上限或合约不可用")
	}
	tx, err := s.db.BeginTx(ctx, nil)
	if err != nil {
		return err
	}
	defer tx.Rollback()
	if err := s.ensureFutAccount(ctx, tx, uid); err != nil {
		return err
	}
	_, err = tx.ExecContext(ctx, `INSERT INTO paper_fut_settings (user_id, symbol, leverage, margin_mode) VALUES ($1,$2,$3,'cross')
		ON CONFLICT (user_id, symbol) DO UPDATE SET leverage=$3`, uid, sp.Symbol, lev)
	if err != nil {
		return err
	}
	_, err = tx.ExecContext(ctx, `UPDATE paper_fut_positions SET leverage=$3, version=version+1 WHERE user_id=$1 AND symbol=$2 AND qty <> 0`, uid, sp.Symbol, lev)
	if err != nil {
		return err
	}
	return tx.Commit()
}

func (s *Store) SetMarginMode(ctx context.Context, uid, symbol, mode string) error {
	mode = strings.ToLower(mode)
	if mode != "cross" && mode != "isolated" {
		return apiErr(400, "VALIDATION_ERROR", "保证金模式只能是 cross 或 isolated")
	}
	var n int
	_ = s.db.QueryRowContext(ctx, `SELECT count(*) FROM paper_fut_positions WHERE user_id=$1 AND symbol=$2 AND qty <> 0`, uid, strings.ToUpper(symbol)).Scan(&n)
	if n > 0 {
		return apiErr(409, "HAS_POSITION", "有持仓时不能切换保证金模式")
	}
	tx, err := s.db.BeginTx(ctx, nil)
	if err != nil {
		return err
	}
	defer tx.Rollback()
	if err := s.ensureFutAccount(ctx, tx, uid); err != nil {
		return err
	}
	_, err = tx.ExecContext(ctx, `INSERT INTO paper_fut_settings (user_id, symbol, leverage, margin_mode) VALUES ($1,$2,10,$3)
		ON CONFLICT (user_id, symbol) DO UPDATE SET margin_mode=$3`, uid, strings.ToUpper(symbol), mode)
	if err != nil {
		return err
	}
	return tx.Commit()
}

func (s *Store) SetPositionMode(ctx context.Context, uid, mode string) error {
	if mode != "one_way" && mode != "hedge" {
		return apiErr(400, "VALIDATION_ERROR", "持仓模式只能是 one_way 或 hedge")
	}
	var n int
	_ = s.db.QueryRowContext(ctx, `SELECT count(*) FROM paper_fut_positions WHERE user_id=$1 AND qty <> 0`, uid).Scan(&n)
	if n > 0 {
		return apiErr(409, "HAS_POSITION", "有持仓时不能切换合仓/分仓")
	}
	tx, err := s.db.BeginTx(ctx, nil)
	if err != nil {
		return err
	}
	defer tx.Rollback()
	if err := s.ensureFutAccount(ctx, tx, uid); err != nil {
		return err
	}
	_, err = tx.ExecContext(ctx, `UPDATE paper_fut_accounts SET position_mode=$2, updated_at=now() WHERE user_id=$1`, uid, mode)
	if err != nil {
		return err
	}
	return tx.Commit()
}

// LiquidateAt closes positions whose mark has crossed the liquidation price.
// The event key includes the position version, so a second instance no-ops.
func (s *Store) LiquidateAt(ctx context.Context, uid, symbol, positionSide string, mark, mmr decimal.Decimal) (bool, error) {
	tx, err := s.db.BeginTx(ctx, nil)
	if err != nil {
		return false, err
	}
	defer tx.Rollback()
	var qty, entry, iso decimal.Decimal
	var lev int
	var ver int64
	var mode string
	err = tx.QueryRowContext(ctx, `SELECT qty, entry_price, isolated_margin, leverage, version, margin_mode FROM paper_fut_positions WHERE user_id=$1 AND symbol=$2 AND position_side=$3 FOR UPDATE`, uid, symbol, positionSide).Scan(&qty, &entry, &iso, &lev, &ver, &mode)
	if err == sql.ErrNoRows || qty.IsZero() {
		return false, nil
	}
	if err != nil {
		return false, err
	}
	lp := liqPrice(entry, iso, qty, mmr, lev)
	hit := false
	if qty.IsPositive() && lp.IsPositive() && mark.LessThanOrEqual(lp) {
		hit = true
	}
	if qty.IsNegative() && lp.IsPositive() && mark.GreaterThanOrEqual(lp) {
		hit = true
	}
	if !hit {
		return false, nil
	}
	key := fmt.Sprintf("liq:%s:%s:%s:%d", uid, symbol, positionSide, ver)
	res, err := tx.ExecContext(ctx, `INSERT INTO paper_fut_events (idempotency_key, user_id, kind) VALUES ($1,$2,'liquidation') ON CONFLICT DO NOTHING`, key, uid)
	if err != nil {
		return false, err
	}
	if n, _ := res.RowsAffected(); n == 0 {
		return false, nil
	}
	side := "sell"
	if qty.IsNegative() {
		side = "buy"
	}
	in := futOrderIn{Symbol: symbol, Side: side, PositionSide: positionSide, Action: "close", Leverage: lev, MarginMode: mode}
	if err := s.reduce(ctx, tx, uid, randomID("liq"), in, qty.Abs(), mark); err != nil {
		return false, err
	}
	var after decimal.Decimal
	_ = tx.QueryRowContext(ctx, `SELECT balance FROM paper_fut_accounts WHERE user_id=$1`, uid).Scan(&after)
	if _, err := tx.ExecContext(ctx, `INSERT INTO paper_fut_ledger (user_id, kind, amount, balance_after, ref) VALUES ($1,'liquidation',0,$2,$3)`, uid, after, key); err != nil {
		return false, err
	}
	if err := tx.Commit(); err != nil {
		return false, err
	}
	return true, nil
}

func (s *Store) LiquidationSweep(ctx context.Context, src FutSource) error {
	specs, err := src.Specs(ctx)
	if err != nil {
		return err
	}
	by := map[string]FutSpec{}
	for _, sp := range specs {
		by[sp.Symbol] = sp
	}
	rows, err := s.db.QueryContext(ctx, `SELECT user_id, symbol, position_side FROM paper_fut_positions WHERE qty <> 0`)
	if err != nil {
		return err
	}
	defer rows.Close()
	type row struct{ u, s, p string }
	var list []row
	for rows.Next() {
		var r row
		if err := rows.Scan(&r.u, &r.s, &r.p); err != nil {
			return err
		}
		list = append(list, r)
	}
	if err := rows.Err(); err != nil {
		return err
	}
	for _, r := range list {
		sp, ok := by[r.s]
		if !ok || !sp.Available {
			continue
		}
		mark, _, _, qok, qerr := src.Quote(ctx, sp.LiveSymbol)
		if qerr != nil || !qok {
			continue
		}
		if _, err := s.LiquidateAt(ctx, r.u, r.s, r.p, mark, sp.MMR); err != nil {
			return err
		}
	}
	return nil
}

func (s *Store) ApplyFunding(ctx context.Context, uid, symbol string, rate decimal.Decimal, fundingTime time.Time, available bool) error {
	if !available {
		return nil
	}
	key := fmt.Sprintf("fund:%s:%s:%d", uid, symbol, fundingTime.UTC().Unix())
	tx, err := s.db.BeginTx(ctx, nil)
	if err != nil {
		return err
	}
	defer tx.Rollback()
	res, err := tx.ExecContext(ctx, `INSERT INTO paper_fut_events (idempotency_key, user_id, kind) VALUES ($1,$2,'funding') ON CONFLICT DO NOTHING`, key, uid)
	if err != nil {
		return err
	}
	if n, _ := res.RowsAffected(); n == 0 {
		return nil
	}
	var qty, entry decimal.Decimal
	err = tx.QueryRowContext(ctx, `SELECT qty, entry_price FROM paper_fut_positions WHERE user_id=$1 AND symbol=$2 AND qty <> 0`, uid, symbol).Scan(&qty, &entry)
	if err == sql.ErrNoRows {
		return nil
	}
	if err != nil {
		return err
	}
	pay := entry.Mul(qty.Abs()).Mul(rate)
	if qty.IsNegative() {
		pay = pay.Neg()
	}
	if _, err := tx.ExecContext(ctx, `UPDATE paper_fut_accounts SET balance=GREATEST(balance-$2,0), updated_at=now() WHERE user_id=$1`, uid, pay); err != nil {
		return err
	}
	var after decimal.Decimal
	_ = tx.QueryRowContext(ctx, `SELECT balance FROM paper_fut_accounts WHERE user_id=$1`, uid).Scan(&after)
	if _, err := tx.ExecContext(ctx, `INSERT INTO paper_fut_ledger (user_id, kind, amount, balance_after, ref) VALUES ($1,'funding',$2,$3,$4)`, uid, pay.Neg(), after, key); err != nil {
		return err
	}
	return tx.Commit()
}
