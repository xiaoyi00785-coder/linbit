package main

import (
	"context"
	"database/sql"
	"strings"
	"time"

	"github.com/shopspring/decimal"
)

// SymbolInfo is a trading pair as exposed by GET /market/symbols. It mirrors the
// openapi Symbol schema and is read from the shared `symbols` table.
type SymbolInfo struct {
	Symbol            string
	Base              string
	Quote             string
	PricePrecision    int
	QuantityPrecision int
	MinQuantity       decimal.Decimal
	Status            string
}

// Store is the market-data database: kline persistence plus a read-only view of
// the trading-pair catalogue owned by the order service.
type Store struct {
	db *sql.DB
}

// NewStore constructs a Store.
func NewStore(db *sql.DB) *Store { return &Store{db: db} }

// Symbols returns the configured trading pairs ordered by symbol. It reads the
// `symbols` table maintained by the order service (shared database). If that
// table is not present yet it returns no rows (the caller falls back to config).
func (s *Store) Symbols(ctx context.Context) ([]SymbolInfo, error) {
	rows, err := s.db.QueryContext(ctx,
		`SELECT symbol, base, quote, price_precision, quantity_precision,
		        min_quantity::text, status
		 FROM symbols ORDER BY symbol`)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	var out []SymbolInfo
	for rows.Next() {
		var si SymbolInfo
		var minQty string
		if err := rows.Scan(&si.Symbol, &si.Base, &si.Quote, &si.PricePrecision,
			&si.QuantityPrecision, &minQty, &si.Status); err != nil {
			return nil, err
		}
		si.MinQuantity = decimal.RequireFromString(minQty)
		out = append(out, si)
	}
	return out, rows.Err()
}

// Category is one market-page tab (GET /market/categories).
type Category struct {
	ID      string   `json:"id"`
	Name    string   `json:"name"`
	Symbols []string `json:"symbols"`
}

// Categories returns the market_categories rows by sort order. Symbols are
// returned as stored (callers filter to listed pairs).
func (s *Store) Categories(ctx context.Context) ([]Category, error) {
	rows, err := s.db.QueryContext(ctx,
		`SELECT id, name, array_to_string(symbols, ',') FROM market_categories ORDER BY sort, id`)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	out := []Category{}
	for rows.Next() {
		var c Category
		var syms string
		if err := rows.Scan(&c.ID, &c.Name, &syms); err != nil {
			return nil, err
		}
		c.Symbols = []string{}
		for _, x := range strings.Split(syms, ",") {
			if x = strings.ToUpper(strings.TrimSpace(x)); x != "" {
				c.Symbols = append(c.Symbols, x)
			}
		}
		out = append(out, c)
	}
	return out, rows.Err()
}

// UpsertKline inserts or updates one candle, keyed by (symbol, interval,
// open_time). Updating the in-progress candle on every trade keeps the durable
// copy current so the service recovers exact state on restart.
func (s *Store) UpsertKline(ctx context.Context, symbol, interval string, c Candle) error {
	_, err := s.db.ExecContext(ctx,
		`INSERT INTO klines (symbol, interval, open_time, open, high, low, close, volume, quote_volume, trades)
		 VALUES ($1,$2,$3,$4,$5,$6,$7,$8,$9,$10)
		 ON CONFLICT (symbol, interval, open_time) DO UPDATE SET
		     high = EXCLUDED.high, low = EXCLUDED.low, close = EXCLUDED.close,
		     volume = EXCLUDED.volume, quote_volume = EXCLUDED.quote_volume, trades = EXCLUDED.trades`,
		symbol, interval, c.OpenTime,
		c.Open.String(), c.High.String(), c.Low.String(), c.Close.String(),
		c.Volume.String(), c.QuoteVolume.String(), c.Trades)
	return err
}

// RecentKlines loads the most recent `limit` candles for a symbol/interval,
// returned in ascending open_time order (oldest first).
func (s *Store) RecentKlines(ctx context.Context, symbol, interval string, limit int) ([]Candle, error) {
	rows, err := s.db.QueryContext(ctx,
		`SELECT open_time, open::text, high::text, low::text, close::text,
		        volume::text, quote_volume::text, trades
		 FROM (
		     SELECT * FROM klines
		     WHERE symbol = $1 AND interval = $2
		     ORDER BY open_time DESC
		     LIMIT $3
		 ) t
		 ORDER BY open_time ASC`, symbol, interval, limit)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	var out []Candle
	for rows.Next() {
		var c Candle
		var o, h, l, cl, v, qv string
		if err := rows.Scan(&c.OpenTime, &o, &h, &l, &cl, &v, &qv, &c.Trades); err != nil {
			return nil, err
		}
		c.Open = decimal.RequireFromString(o)
		c.High = decimal.RequireFromString(h)
		c.Low = decimal.RequireFromString(l)
		c.Close = decimal.RequireFromString(cl)
		c.Volume = decimal.RequireFromString(v)
		c.QuoteVolume = decimal.RequireFromString(qv)
		c.Closed = true
		out = append(out, c)
	}
	return out, rows.Err()
}

// RecentTakerTrades reads public trades since `since` from the order service's
// `trades` table (shared DB; one taker row per engine trade), ascending, capped
// at max rows (newest kept). Used only for restart recovery.
func (s *Store) RecentTakerTrades(ctx context.Context, since time.Time, max int) ([]TradeEvent, error) {
	rows, err := s.db.QueryContext(ctx, `SELECT trade_id, symbol, side, price, quantity, created_at FROM (
		SELECT id, trade_id, symbol, side, price, quantity, created_at FROM trades
		WHERE role = 'taker' AND created_at >= $1 ORDER BY created_at DESC, id DESC LIMIT $2) t
		ORDER BY created_at, id`, since, max)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	var out []TradeEvent
	for rows.Next() {
		var ev TradeEvent
		if err := rows.Scan(&ev.TradeID, &ev.Symbol, &ev.TakerSide, &ev.Price, &ev.Quantity, &ev.Time); err != nil {
			return nil, err
		}
		out = append(out, ev)
	}
	return out, rows.Err()
}

// FuturesSymbols lists trading perpetual contracts from the futures service's
// fut_contracts table (shared DB).
func (s *Store) FuturesSymbols(ctx context.Context) ([]string, error) {
	rows, err := s.db.QueryContext(ctx, `SELECT symbol FROM fut_contracts WHERE status = 'trading' ORDER BY sort, symbol`)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	var out []string
	for rows.Next() {
		var sym string
		if err := rows.Scan(&sym); err != nil {
			return nil, err
		}
		out = append(out, sym)
	}
	return out, rows.Err()
}
