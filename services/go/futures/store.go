package main

import (
	"context"
	"crypto/rand"
	"database/sql"
	"encoding/hex"
	"errors"
	"fmt"
	"net/http"
	"time"

	"github.com/shopspring/decimal"
)

// store.go: domain types and row helpers.

const (
	insuranceUser = "system:insurance"
	feeUser       = "system:fee"
)

type queryer interface {
	QueryRowContext(ctx context.Context, q string, args ...any) *sql.Row
	QueryContext(ctx context.Context, q string, args ...any) (*sql.Rows, error)
	ExecContext(ctx context.Context, q string, args ...any) (sql.Result, error)
}

// apiError is a typed business error surfaced to the client.
type apiError struct {
	Status        int
	Code, Message string
}

func (e *apiError) Error() string { return e.Code + ": " + e.Message }

func bad(code, msg string) error { return &apiError{http.StatusBadRequest, code, msg} }

var (
	errNotFound       = &apiError{http.StatusNotFound, "NOT_FOUND", "记录不存在"}
	errMarkUnavail    = &apiError{http.StatusServiceUnavailable, "MARK_PRICE_UNAVAILABLE", "标记价格暂不可用，请稍后再试"}
	errInsufficient   = bad("INSUFFICIENT_MARGIN", "可用保证金不足")
	errContractHalted = bad("CONTRACT_HALTED", "该合约暂停交易")
)

func asAPI(err error) (*apiError, bool) {
	var a *apiError
	ok := errors.As(err, &a)
	return a, ok
}

func newRef(prefix string) string {
	b := make([]byte, 8)
	_, _ = rand.Read(b)
	return prefix + "-" + hex.EncodeToString(b)
}

type Contract struct {
	Symbol      string          `json:"symbol"`
	Base        string          `json:"baseAsset"`
	Quote       string          `json:"quoteAsset"`
	MaxLeverage int             `json:"maxLeverage"`
	MMR         decimal.Decimal `json:"maintenanceMarginRate"`
	Tick        decimal.Decimal `json:"tickSize"`
	MinQty      decimal.Decimal `json:"minQty"`
	MaxQty      decimal.Decimal `json:"maxQty"`
	MakerFee    decimal.Decimal `json:"makerFeeRate"`
	TakerFee    decimal.Decimal `json:"takerFeeRate"`
	Status      string          `json:"status"`
	Type        string          `json:"contractType"`
	Margin      string          `json:"marginAsset"`
	FundingH    int             `json:"fundingIntervalHours"`
	Tiers       []Tier          `json:"riskTiers"`
	Tags        []string        `json:"tags"` // market_categories ids (same as spot Symbol.tags)
}

func loadContracts(ctx context.Context, db queryer) ([]Contract, error) {
	rows, err := db.QueryContext(ctx, `SELECT symbol, base, quote, max_leverage, mmr, tick_size, min_qty, max_qty, maker_fee, taker_fee, status FROM fut_contracts ORDER BY sort, symbol`)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	var out []Contract
	for rows.Next() {
		var c Contract
		if err := rows.Scan(&c.Symbol, &c.Base, &c.Quote, &c.MaxLeverage, &c.MMR, &c.Tick, &c.MinQty, &c.MaxQty, &c.MakerFee, &c.TakerFee, &c.Status); err != nil {
			return nil, err
		}
		c.Type, c.Margin = "perpetual", "USDT"
		out = append(out, c)
	}
	if err := rows.Err(); err != nil {
		return nil, err
	}
	for i := range out {
		t, err := loadTiers(ctx, db, out[i].Symbol)
		if err != nil {
			return nil, err
		}
		out[i].Tiers = t
	}
	return out, nil
}

// ---- accounts / bills ----

func lockAccount(ctx context.Context, tx queryer, uid string) (bal, frozen decimal.Decimal, err error) {
	if _, err = tx.ExecContext(ctx, `INSERT INTO fut_accounts (user_id) VALUES ($1) ON CONFLICT DO NOTHING`, uid); err != nil {
		return
	}
	err = tx.QueryRowContext(ctx, `SELECT balance, frozen FROM fut_accounts WHERE user_id=$1 FOR UPDATE`, uid).Scan(&bal, &frozen)
	return
}

func readAccount(ctx context.Context, q queryer, uid string) (bal, frozen decimal.Decimal, err error) {
	err = q.QueryRowContext(ctx, `SELECT balance, frozen FROM fut_accounts WHERE user_id=$1`, uid).Scan(&bal, &frozen)
	if errors.Is(err, sql.ErrNoRows) {
		return dZero, dZero, nil
	}
	return
}

// addBalance applies a delta to balance (and optionally frozen) and writes a bill.
func addBalance(ctx context.Context, tx queryer, uid, symbol, kind string, delta decimal.Decimal, ref string) error {
	if delta.IsZero() {
		return nil
	}
	if _, err := tx.ExecContext(ctx, `INSERT INTO fut_accounts (user_id) VALUES ($1) ON CONFLICT DO NOTHING`, uid); err != nil {
		return err
	}
	if _, err := tx.ExecContext(ctx, `UPDATE fut_accounts SET balance = balance + $2, updated_at=now() WHERE user_id=$1`, uid, delta); err != nil {
		return err
	}
	return addBill(ctx, tx, uid, symbol, kind, delta, ref)
}

func addFrozen(ctx context.Context, tx queryer, uid string, delta decimal.Decimal) error {
	if delta.IsZero() {
		return nil
	}
	_, err := tx.ExecContext(ctx, `UPDATE fut_accounts SET frozen = GREATEST(frozen + $2, 0), updated_at=now() WHERE user_id=$1`, uid, delta)
	return err
}

func addBill(ctx context.Context, tx queryer, uid, symbol, kind string, amount decimal.Decimal, ref string) error {
	var sym, r any
	if symbol != "" {
		sym = symbol
	}
	if ref != "" {
		r = ref
	}
	_, err := tx.ExecContext(ctx, `INSERT INTO fut_bills (user_id, symbol, kind, amount, ref) VALUES ($1,$2,$3,$4,$5)`, uid, sym, kind, amount, r)
	return err
}

// ---- settings ----

func (s *Server) settings(ctx context.Context, q queryer, uid string, c Contract) (lev int, mode string, err error) {
	err = q.QueryRowContext(ctx, `SELECT leverage, margin_mode FROM fut_settings WHERE user_id=$1 AND symbol=$2`, uid, c.Symbol).Scan(&lev, &mode)
	if errors.Is(err, sql.ErrNoRows) {
		lev, mode, err = s.cfg.DefaultLeverage, s.cfg.DefaultMarginMode, nil
	}
	if lev > c.MaxLeverage {
		lev = c.MaxLeverage
	}
	return
}

// ---- positions ----

type Position struct {
	UserID    string
	Symbol    string
	Qty       decimal.Decimal
	Entry     decimal.Decimal
	Mode      string
	Leverage  int
	Iso       decimal.Decimal
	Realized  decimal.Decimal
	TP, SL    decimal.NullDecimal
	UpdatedAt time.Time
}

const posCols = `user_id, symbol, qty, entry_price, margin_mode, leverage, iso_margin, realized_pnl, tp_price, sl_price, updated_at`

func scanPos(sc interface{ Scan(...any) error }) (Position, error) {
	var p Position
	err := sc.Scan(&p.UserID, &p.Symbol, &p.Qty, &p.Entry, &p.Mode, &p.Leverage, &p.Iso, &p.Realized, &p.TP, &p.SL, &p.UpdatedAt)
	return p, err
}

// loadPosition returns the (possibly empty) position; forUpdate locks the row.
func loadPosition(ctx context.Context, q queryer, uid, sym string, forUpdate bool) (Position, bool, error) {
	qs := `SELECT ` + posCols + ` FROM fut_positions WHERE user_id=$1 AND symbol=$2`
	if forUpdate {
		qs += ` FOR UPDATE`
	}
	p, err := scanPos(q.QueryRowContext(ctx, qs, uid, sym))
	if errors.Is(err, sql.ErrNoRows) {
		return Position{UserID: uid, Symbol: sym}, false, nil
	}
	return p, err == nil, err
}

func savePosition(ctx context.Context, tx queryer, p Position) error {
	_, err := tx.ExecContext(ctx, `INSERT INTO fut_positions (`+posCols[:len(posCols)-len(", updated_at")]+`, updated_at)
		VALUES ($1,$2,$3,$4,$5,$6,$7,$8,$9,$10, now())
		ON CONFLICT (user_id, symbol) DO UPDATE SET qty=EXCLUDED.qty, entry_price=EXCLUDED.entry_price,
		  margin_mode=EXCLUDED.margin_mode, leverage=EXCLUDED.leverage, iso_margin=EXCLUDED.iso_margin,
		  realized_pnl=EXCLUDED.realized_pnl, tp_price=EXCLUDED.tp_price, sl_price=EXCLUDED.sl_price, updated_at=now()`,
		p.UserID, p.Symbol, p.Qty, p.Entry, p.Mode, p.Leverage, p.Iso, p.Realized, p.TP, p.SL)
	return err
}

func openPositions(ctx context.Context, q queryer, uid string) ([]Position, error) {
	rows, err := q.QueryContext(ctx, `SELECT `+posCols+` FROM fut_positions WHERE user_id=$1 AND qty <> 0 ORDER BY symbol`, uid)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	var out []Position
	for rows.Next() {
		p, err := scanPos(rows)
		if err != nil {
			return nil, err
		}
		out = append(out, p)
	}
	return out, rows.Err()
}

// ---- orders ----

type Order struct {
	ID          int64               `json:"orderId"`
	UserID      string              `json:"-"`
	Symbol      string              `json:"symbol"`
	Side        string              `json:"side"`
	Type        string              `json:"type"`
	TIF         string              `json:"timeInForce"`
	Price       decimal.NullDecimal `json:"price"`
	Qty         decimal.Decimal     `json:"qty"`
	Filled      decimal.Decimal     `json:"filledQty"`
	FilledQuote decimal.Decimal     `json:"filledQuote"`
	Reserved    decimal.Decimal     `json:"reservedMargin"`
	Fee         decimal.Decimal     `json:"fee"`
	Status      string              `json:"status"`
	ReduceOnly  bool                `json:"reduceOnly"`
	Leverage    int                 `json:"leverage"`
	Mode        string              `json:"marginMode"`
	Source      string              `json:"source"`
	TP          decimal.NullDecimal `json:"tpPrice"`
	SL          decimal.NullDecimal `json:"slPrice"`
	Reason      sql.NullString      `json:"-"`
	CancelWhy   string              `json:"cancelReason,omitempty"`
	AvgPrice    string              `json:"avgPrice"`
	CreatedAt   time.Time           `json:"createdAt"`
	UpdatedAt   time.Time           `json:"updatedAt"`
}

const orderCols = `id, user_id, symbol, side, type, tif, price, qty, filled, filled_quote, reserved, fee, status, reduce_only, leverage, margin_mode, source, tp_price, sl_price, cancel_reason, created_at, updated_at`

func scanOrder(sc interface{ Scan(...any) error }) (Order, error) {
	var o Order
	err := sc.Scan(&o.ID, &o.UserID, &o.Symbol, &o.Side, &o.Type, &o.TIF, &o.Price, &o.Qty, &o.Filled, &o.FilledQuote, &o.Reserved, &o.Fee, &o.Status, &o.ReduceOnly, &o.Leverage, &o.Mode, &o.Source, &o.TP, &o.SL, &o.Reason, &o.CreatedAt, &o.UpdatedAt)
	if err == nil {
		o.CancelWhy = o.Reason.String
		o.AvgPrice = "0"
		if o.Filled.Sign() > 0 {
			o.AvgPrice = o.FilledQuote.Div(o.Filled).Round(8).String()
		}
	}
	return o, err
}

func (o Order) remaining() decimal.Decimal { return o.Qty.Sub(o.Filled) }

func (o Order) open() bool { return o.Status == "new" || o.Status == "partially_filled" }

func updateOrder(ctx context.Context, tx queryer, o *Order) error {
	var reason any
	if o.CancelWhy != "" {
		reason = o.CancelWhy
	}
	_, err := tx.ExecContext(ctx, `UPDATE fut_orders SET filled=$2, filled_quote=$3, reserved=$4, fee=$5, status=$6, cancel_reason=$7, updated_at=now() WHERE id=$1`,
		o.ID, o.Filled, o.FilledQuote, o.Reserved, o.Fee, o.Status, reason)
	return err
}

// releaseOrder cancels an open order and returns its reservation.
func releaseOrder(ctx context.Context, tx queryer, o *Order, why string) error {
	if err := addFrozen(ctx, tx, o.UserID, o.Reserved.Neg()); err != nil {
		return err
	}
	o.Reserved = dZero
	o.Status = "canceled"
	o.CancelWhy = why
	return updateOrder(ctx, tx, o)
}

func cancelUserOrders(ctx context.Context, tx queryer, uid, symbol, why string) error {
	qs := `SELECT ` + orderCols + ` FROM fut_orders WHERE user_id=$1 AND status IN ('new','partially_filled')`
	args := []any{uid}
	if symbol != "" {
		qs += ` AND symbol=$2`
		args = append(args, symbol)
	}
	rows, err := tx.QueryContext(ctx, qs+` FOR UPDATE`, args...)
	if err != nil {
		return err
	}
	var list []Order
	for rows.Next() {
		o, err := scanOrder(rows)
		if err != nil {
			rows.Close()
			return err
		}
		list = append(list, o)
	}
	rows.Close()
	for i := range list {
		if err := releaseOrder(ctx, tx, &list[i], why); err != nil {
			return fmt.Errorf("cancel %d: %w", list[i].ID, err)
		}
	}
	return nil
}
