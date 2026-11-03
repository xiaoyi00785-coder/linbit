package main

import (
	"context"
	"database/sql"
	"errors"
	"time"

	"github.com/shopspring/decimal"
)

// ErrSymbolNotFound is returned when a symbol is not configured for trading.
var ErrSymbolNotFound = errors.New("symbol not found")

// ErrOrderNotFound is returned when an order does not exist for the user.
var ErrOrderNotFound = errors.New("order not found")

// SymbolConfig is a trading pair's admission rules.
type SymbolConfig struct {
	Symbol            string
	Base              string
	Quote             string
	PricePrecision    int
	QuantityPrecision int
	MinQuantity       decimal.Decimal
	MinNotional       decimal.Decimal
	Status            string
}

// Order is a persisted spot order. Price and QuoteOrderQty are optional
// (market orders have no price; only market-buy-by-quote carries a budget).
type Order struct {
	OrderID       string
	ClientOrderID string
	UserID        string
	Symbol        string
	Side          string // buy | sell
	Type          string // limit | market
	TimeInForce   string // GTC | IOC | FOK | PostOnly
	Price         decimal.Decimal
	HasPrice      bool
	Quantity      decimal.Decimal
	QuoteOrderQty decimal.Decimal
	HasQuoteQty   bool
	FilledQuantity decimal.Decimal
	Status        string
	FrozenAsset   string
	FrozenAmount  decimal.Decimal
	Unfrozen      bool
	CreatedAt     time.Time
}

// TradeRow is one side (maker or taker) of a matched trade, as seen by one user.
type TradeRow struct {
	TradeID   string
	OrderID   string
	UserID    string
	Symbol    string
	Side      string
	Price     decimal.Decimal
	Quantity  decimal.Decimal
	Fee       decimal.Decimal
	FeeAsset  string
	Role      string // maker | taker
	CreatedAt time.Time
}

// Store wraps the order database.
type Store struct {
	db *sql.DB
}

// NewStore constructs a Store.
func NewStore(db *sql.DB) *Store { return &Store{db: db} }

// Symbol returns the configuration for a trading pair.
func (s *Store) Symbol(ctx context.Context, symbol string) (*SymbolConfig, error) {
	var c SymbolConfig
	var minQty, minNotional string
	err := s.db.QueryRowContext(ctx,
		`SELECT symbol, base, quote, price_precision, quantity_precision,
		        min_quantity::text, min_notional::text, status
		 FROM symbols WHERE symbol = $1`, symbol).
		Scan(&c.Symbol, &c.Base, &c.Quote, &c.PricePrecision, &c.QuantityPrecision,
			&minQty, &minNotional, &c.Status)
	if errors.Is(err, sql.ErrNoRows) {
		return nil, ErrSymbolNotFound
	}
	if err != nil {
		return nil, err
	}
	c.MinQuantity = decimal.RequireFromString(minQty)
	c.MinNotional = decimal.RequireFromString(minNotional)
	return &c, nil
}

// Symbols returns all configured trading pairs ordered by symbol.
func (s *Store) Symbols(ctx context.Context) ([]SymbolConfig, error) {
	rows, err := s.db.QueryContext(ctx,
		`SELECT symbol, base, quote, price_precision, quantity_precision,
		        min_quantity::text, min_notional::text, status
		 FROM symbols ORDER BY symbol`)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	var out []SymbolConfig
	for rows.Next() {
		var c SymbolConfig
		var minQty, minNotional string
		if err := rows.Scan(&c.Symbol, &c.Base, &c.Quote, &c.PricePrecision,
			&c.QuantityPrecision, &minQty, &minNotional, &c.Status); err != nil {
			return nil, err
		}
		c.MinQuantity = decimal.RequireFromString(minQty)
		c.MinNotional = decimal.RequireFromString(minNotional)
		out = append(out, c)
	}
	return out, rows.Err()
}

// NextOrderID draws the next human-readable order id from the sequence.
func (s *Store) NextOrderID(ctx context.Context) (string, error) {
	var id int64
	if err := s.db.QueryRowContext(ctx, `SELECT nextval('order_id_seq')`).Scan(&id); err != nil {
		return "", err
	}
	return decimal.NewFromInt(id).String(), nil
}

// InsertOrder persists a newly admitted order.
func (s *Store) InsertOrder(ctx context.Context, o *Order) error {
	_, err := s.db.ExecContext(ctx,
		`INSERT INTO orders (order_id, client_order_id, user_id, symbol, side, type,
		    time_in_force, price, quantity, quote_order_qty, filled_quantity, status,
		    frozen_asset, frozen_amount)
		 VALUES ($1,$2,$3,$4,$5,$6,$7,$8,$9,$10,$11,$12,$13,$14)`,
		o.OrderID, nullString(o.ClientOrderID), o.UserID, o.Symbol, o.Side, o.Type,
		o.TimeInForce, nullDecimal(o.Price, o.HasPrice), o.Quantity.String(),
		nullDecimal(o.QuoteOrderQty, o.HasQuoteQty), o.FilledQuantity.String(), o.Status,
		o.FrozenAsset, o.FrozenAmount.String())
	return err
}

// GetOrder loads an order owned by userID.
func (s *Store) GetOrder(ctx context.Context, userID, orderID string) (*Order, error) {
	return s.scanOrder(s.db.QueryRowContext(ctx,
		`SELECT order_id, client_order_id, user_id, symbol, side, type, time_in_force,
		        price::text, quantity::text, quote_order_qty::text, filled_quantity::text,
		        status, frozen_asset, frozen_amount::text, unfrozen, created_at
		 FROM orders WHERE user_id = $1 AND order_id = $2`, userID, orderID))
}

// UpdateFill sets the filled quantity and status of an order.
func (s *Store) UpdateFill(ctx context.Context, orderID string, filled decimal.Decimal, status string) error {
	_, err := s.db.ExecContext(ctx,
		`UPDATE orders SET filled_quantity = $2, status = $3, updated_at = now()
		 WHERE order_id = $1`, orderID, filled.String(), status)
	return err
}

// ClaimUnfreeze flips the one-shot unfrozen flag, returning true if this call won
// the race (so the caller should release the leftover exactly once).
func (s *Store) ClaimUnfreeze(ctx context.Context, orderID string) (bool, error) {
	res, err := s.db.ExecContext(ctx,
		`UPDATE orders SET unfrozen = true WHERE order_id = $1 AND unfrozen = false`, orderID)
	if err != nil {
		return false, err
	}
	n, _ := res.RowsAffected()
	return n > 0, nil
}

// OpenOrders returns a page of the user's live orders (new / partially_filled).
func (s *Store) OpenOrders(ctx context.Context, userID, symbol string, page, size int) ([]Order, int, error) {
	where := `user_id = $1 AND status IN ('new','partially_filled')`
	args := []any{userID}
	if symbol != "" {
		where += ` AND symbol = $2`
		args = append(args, symbol)
	}
	return s.queryOrders(ctx, where, args, page, size)
}

// terminalStatuses are the final order states surfaced by order history. Live
// orders (new / partially_filled) are returned by OpenOrders, never here.
var terminalStatuses = []string{"filled", "canceled", "rejected", "expired"}

// OrderHistory returns a page of the user's terminal orders (filled / canceled /
// rejected / expired), optionally filtered by symbol and by one terminal status.
// A non-terminal status filter matches nothing by design.
func (s *Store) OrderHistory(ctx context.Context, userID, symbol, status string, page, size int) ([]Order, int, error) {
	args := []any{userID}
	// Fixed terminal set, expanded to a ($2,$3,...) IN list so live orders can
	// never leak into history regardless of the caller's status filter.
	in := make([]string, 0, len(terminalStatuses))
	for _, st := range terminalStatuses {
		in = append(in, `$`+itoa(len(args)+1))
		args = append(args, st)
	}
	where := `user_id = $1 AND status IN (` + join(in, ",") + `)`
	if symbol != "" {
		where += ` AND symbol = $` + itoa(len(args)+1)
		args = append(args, symbol)
	}
	if status != "" {
		where += ` AND status = $` + itoa(len(args)+1)
		args = append(args, status)
	}
	return s.queryOrders(ctx, where, args, page, size)
}

func (s *Store) queryOrders(ctx context.Context, where string, args []any, page, size int) ([]Order, int, error) {
	var total int
	if err := s.db.QueryRowContext(ctx, `SELECT count(*) FROM orders WHERE `+where, args...).Scan(&total); err != nil {
		return nil, 0, err
	}
	q := `SELECT order_id, client_order_id, user_id, symbol, side, type, time_in_force,
	             price::text, quantity::text, quote_order_qty::text, filled_quantity::text,
	             status, frozen_asset, frozen_amount::text, unfrozen, created_at
	      FROM orders WHERE ` + where +
		` ORDER BY created_at DESC, order_id DESC LIMIT $` + itoa(len(args)+1) + ` OFFSET $` + itoa(len(args)+2)
	args = append(args, size, (page-1)*size)
	rows, err := s.db.QueryContext(ctx, q, args...)
	if err != nil {
		return nil, 0, err
	}
	defer rows.Close()
	var out []Order
	for rows.Next() {
		o, err := s.scanOrder(rows)
		if err != nil {
			return nil, 0, err
		}
		out = append(out, *o)
	}
	return out, total, rows.Err()
}

// rowScanner abstracts *sql.Row and *sql.Rows.
type rowScanner interface {
	Scan(dest ...any) error
}

func (s *Store) scanOrder(sc rowScanner) (*Order, error) {
	var o Order
	var client, price, quoteQty sql.NullString
	if err := sc.Scan(&o.OrderID, &client, &o.UserID, &o.Symbol, &o.Side, &o.Type,
		&o.TimeInForce, &price, &o.Quantity, &quoteQty, &o.FilledQuantity, &o.Status,
		&o.FrozenAsset, &o.FrozenAmount, &o.Unfrozen, &o.CreatedAt); err != nil {
		if errors.Is(err, sql.ErrNoRows) {
			return nil, ErrOrderNotFound
		}
		return nil, err
	}
	o.ClientOrderID = client.String
	if price.Valid {
		o.Price = decimal.RequireFromString(price.String)
		o.HasPrice = true
	}
	if quoteQty.Valid {
		o.QuoteOrderQty = decimal.RequireFromString(quoteQty.String)
		o.HasQuoteQty = true
	}
	return &o, nil
}

// Consumed returns how much of an order's frozen asset has been spent across its
// recorded fills: quote spent (Σ price·qty) for a buy, base delivered (Σ qty)
// for a sell. Used to release the exact over-frozen leftover when an order ends.
func (s *Store) Consumed(ctx context.Context, orderID, side string) (decimal.Decimal, error) {
	expr := `COALESCE(SUM(quantity), 0)::text`
	if side == "buy" {
		expr = `COALESCE(SUM(price * quantity), 0)::text`
	}
	var sum string
	if err := s.db.QueryRowContext(ctx,
		`SELECT `+expr+` FROM trades WHERE order_id = $1`, orderID).Scan(&sum); err != nil {
		return decimal.Zero, err
	}
	return decimal.RequireFromString(sum), nil
}

// InsertTrade records one side of a matched trade; idempotent on (trade_id, order_id).
func (s *Store) InsertTrade(ctx context.Context, t TradeRow) error {
	_, err := s.db.ExecContext(ctx,
		`INSERT INTO trades (trade_id, order_id, user_id, symbol, side, price, quantity, fee, fee_asset, role)
		 VALUES ($1,$2,$3,$4,$5,$6,$7,$8,$9,$10)
		 ON CONFLICT (trade_id, order_id) DO NOTHING`,
		t.TradeID, t.OrderID, t.UserID, t.Symbol, t.Side, t.Price.String(), t.Quantity.String(),
		t.Fee.String(), nullString(t.FeeAsset), t.Role)
	return err
}

// MyTrades returns a page of the user's trade fills.
func (s *Store) MyTrades(ctx context.Context, userID, symbol string, page, size int) ([]TradeRow, int, error) {
	where := `user_id = $1`
	args := []any{userID}
	if symbol != "" {
		where += ` AND symbol = $2`
		args = append(args, symbol)
	}
	var total int
	if err := s.db.QueryRowContext(ctx, `SELECT count(*) FROM trades WHERE `+where, args...).Scan(&total); err != nil {
		return nil, 0, err
	}
	q := `SELECT trade_id, order_id, user_id, symbol, side, price::text, quantity::text,
	             fee::text, fee_asset, role, created_at
	      FROM trades WHERE ` + where +
		` ORDER BY created_at DESC, id DESC LIMIT $` + itoa(len(args)+1) + ` OFFSET $` + itoa(len(args)+2)
	args = append(args, size, (page-1)*size)
	rows, err := s.db.QueryContext(ctx, q, args...)
	if err != nil {
		return nil, 0, err
	}
	defer rows.Close()
	var out []TradeRow
	for rows.Next() {
		var t TradeRow
		var price, qty, fee string
		var feeAsset sql.NullString
		if err := rows.Scan(&t.TradeID, &t.OrderID, &t.UserID, &t.Symbol, &t.Side,
			&price, &qty, &fee, &feeAsset, &t.Role, &t.CreatedAt); err != nil {
			return nil, 0, err
		}
		t.Price = decimal.RequireFromString(price)
		t.Quantity = decimal.RequireFromString(qty)
		t.Fee = decimal.RequireFromString(fee)
		t.FeeAsset = feeAsset.String
		out = append(out, t)
	}
	return out, total, rows.Err()
}

func nullString(s string) interface{} {
	if s == "" {
		return nil
	}
	return s
}

func nullDecimal(d decimal.Decimal, ok bool) interface{} {
	if !ok {
		return nil
	}
	return d.String()
}

func itoa(n int) string { return decimal.NewFromInt(int64(n)).String() }

// join concatenates parts with sep (small local helper to avoid importing strings).
func join(parts []string, sep string) string {
	out := ""
	for i, p := range parts {
		if i > 0 {
			out += sep
		}
		out += p
	}
	return out
}
