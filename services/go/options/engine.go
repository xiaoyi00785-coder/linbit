package main

import (
	"context"
	"crypto/rand"
	"database/sql"
	"encoding/hex"
	"fmt"
	"log"
	"net/http"
	"sort"
	"strings"
	"sync"
	"time"

	"github.com/shopspring/decimal"
)

// engine.go: options engine. One mu serializes every state mutation so the
// order book, positions and margin never disagree. Each place/cancel is
// committed in a single DB transaction; the matching loop lives in memory on
// the hot path and writes the fills + position deltas as part of that tx.

type Server struct {
	cfg    Config
	db     *sql.DB
	asset  *AssetClient
	market *MarketClient
	now    func() time.Time

	mu            sync.Mutex
	instruments   map[int64]*Instrument       // id → instrument
	instrByName   map[string]*Instrument      // name → instrument
	markPrices    map[int64]*MarkSnapshot     // id → latest mark/Greeks (nil until priced)
	underlyings   map[string]decimal.Decimal  // UPPER(base) → index snapshot
}

type Instrument struct {
	ID              int64
	Name            string
	Underlying      string
	Quote           string
	OptionType      string // call | put
	Strike          decimal.Decimal
	Expiry          time.Time
	ContractSize    decimal.Decimal
	TickSize        decimal.Decimal
	MinQty          decimal.Decimal
	Status          string // active | expired | halted
	SettlementPrice decimal.NullDecimal
}

// MarkSnapshot is the latest Black-Scholes output for an instrument.
type MarkSnapshot struct {
	Mark       decimal.Decimal
	Underlying decimal.Decimal
	IV         decimal.Decimal
	Delta      decimal.Decimal
	Gamma      decimal.Decimal
	Theta      decimal.Decimal
	Vega       decimal.Decimal
	At         time.Time
}

// NewServer loads instruments from the DB.
func NewServer(cfg Config, db *sql.DB) (*Server, error) {
	s := &Server{
		cfg:         cfg,
		db:          db,
		asset:       NewAssetClient(cfg.AssetURL, cfg.InternalToken),
		market:      NewMarketClient(cfg.MarketURL),
		now:         func() time.Time { return time.Now().UTC() },
		instruments: map[int64]*Instrument{},
		instrByName: map[string]*Instrument{},
		markPrices:  map[int64]*MarkSnapshot{},
		underlyings: map[string]decimal.Decimal{},
	}
	if err := s.reloadInstruments(context.Background()); err != nil {
		return nil, err
	}
	return s, nil
}

func (s *Server) reloadInstruments(ctx context.Context) error {
	rows, err := s.db.QueryContext(ctx, `
	    SELECT id, name, underlying, quote, option_type, strike, expiry,
	           contract_size, tick_size, min_qty, status, settlement_price
	      FROM opt_instruments`)
	if err != nil {
		return err
	}
	defer rows.Close()
	s.mu.Lock()
	defer s.mu.Unlock()
	s.instruments = map[int64]*Instrument{}
	s.instrByName = map[string]*Instrument{}
	for rows.Next() {
		var in Instrument
		if err := rows.Scan(&in.ID, &in.Name, &in.Underlying, &in.Quote, &in.OptionType,
			&in.Strike, &in.Expiry, &in.ContractSize, &in.TickSize, &in.MinQty, &in.Status,
			&in.SettlementPrice); err != nil {
			return err
		}
		s.instruments[in.ID] = &in
		s.instrByName[in.Name] = &in
	}
	return rows.Err()
}

func (s *Server) instrumentByName(name string) (*Instrument, error) {
	s.mu.Lock()
	defer s.mu.Unlock()
	in := s.instrByName[strings.ToUpper(name)]
	if in == nil {
		return nil, errNotFound
	}
	return in, nil
}

func (s *Server) listInstruments() []*Instrument {
	s.mu.Lock()
	defer s.mu.Unlock()
	out := make([]*Instrument, 0, len(s.instruments))
	for _, in := range s.instruments {
		out = append(out, in)
	}
	sort.Slice(out, func(i, j int) bool {
		if !out[i].Expiry.Equal(out[j].Expiry) {
			return out[i].Expiry.Before(out[j].Expiry)
		}
		if out[i].Underlying != out[j].Underlying {
			return out[i].Underlying < out[j].Underlying
		}
		if !out[i].Strike.Equal(out[j].Strike) {
			return out[i].Strike.LessThan(out[j].Strike)
		}
		return out[i].OptionType < out[j].OptionType
	})
	return out
}

// mark returns the latest MarkSnapshot for an instrument, or false.
func (s *Server) mark(id int64) (MarkSnapshot, bool) {
	s.mu.Lock()
	defer s.mu.Unlock()
	m := s.markPrices[id]
	if m == nil {
		return MarkSnapshot{}, false
	}
	return *m, true
}

func (s *Server) setMark(id int64, m MarkSnapshot) {
	s.mu.Lock()
	defer s.mu.Unlock()
	s.markPrices[id] = &m
}

func (s *Server) underlyingPrice(asset string) (decimal.Decimal, bool) {
	s.mu.Lock()
	defer s.mu.Unlock()
	p, ok := s.underlyings[strings.ToUpper(asset)]
	return p, ok && p.Sign() > 0
}

func (s *Server) setUnderlying(asset string, p decimal.Decimal) {
	s.mu.Lock()
	defer s.mu.Unlock()
	s.underlyings[strings.ToUpper(asset)] = p
}

// PlaceReq is the POST /options/orders request body.
type PlaceReq struct {
	Instrument string `json:"instrument"`
	ClientID   string `json:"clientId,omitempty"`
	Side       string `json:"side"` // buy | sell
	Type       string `json:"type"` // limit | market
	Price      string `json:"price,omitempty"`
	Qty        string `json:"qty"`
}

// OrderRow is one order as surfaced by GET /options/orders.
type OrderRow struct {
	ID            int64           `json:"id"`
	UserID        string          `json:"userId,omitempty"`
	Instrument    string          `json:"instrument"`
	InstrumentID  int64           `json:"instrumentId"`
	Side          string          `json:"side"`
	Type          string          `json:"type"`
	Price         decimal.Decimal `json:"price"`
	Qty           decimal.Decimal `json:"qty"`
	FilledQty     decimal.Decimal `json:"filledQty"`
	AvgFillPrice  decimal.Decimal `json:"avgFillPrice"`
	Status        string          `json:"status"`
	RejectReason  string          `json:"rejectReason,omitempty"`
	CreatedAt     time.Time       `json:"createdAt"`
	UpdatedAt     time.Time       `json:"updatedAt"`
}

// Place validates, reserves margin, matches against the book and persists the
// order and any fills in one tx. Returns the final order row.
func (s *Server) Place(ctx context.Context, userID string, req PlaceReq) (OrderRow, error) {
	in, err := s.instrumentByName(req.Instrument)
	if err != nil {
		return OrderRow{}, err
	}
	if in.Status != "active" {
		return OrderRow{}, errInstrInactive
	}
	if s.now().After(in.Expiry) {
		return OrderRow{}, errInstrInactive
	}
	side := strings.ToLower(req.Side)
	if side != "buy" && side != "sell" {
		return OrderRow{}, bad("INVALID_SIDE", "side 必须为 buy 或 sell")
	}
	typ := strings.ToLower(req.Type)
	if typ != "limit" && typ != "market" {
		return OrderRow{}, bad("INVALID_TYPE", "type 必须为 limit 或 market")
	}
	qty, err := decimal.NewFromString(strings.TrimSpace(req.Qty))
	if err != nil || qty.Sign() <= 0 {
		return OrderRow{}, bad("INVALID_QTY", "数量不合法")
	}
	if qty.LessThan(in.MinQty) {
		return OrderRow{}, bad("INVALID_QTY", fmt.Sprintf("数量不能小于最小下单量 %s", in.MinQty))
	}
	if !qty.Mod(in.MinQty).IsZero() {
		return OrderRow{}, bad("INVALID_QTY", fmt.Sprintf("数量须为 %s 的整数倍", in.MinQty))
	}
	if qty.GreaterThan(s.cfg.MaxOrderQty) {
		return OrderRow{}, bad("INVALID_QTY", "数量超过单笔上限")
	}
	var price decimal.NullDecimal
	if typ == "limit" {
		p, err := decimal.NewFromString(strings.TrimSpace(req.Price))
		if err != nil || p.Sign() <= 0 {
			return OrderRow{}, bad("INVALID_PRICE", "价格不合法")
		}
		if !p.Mod(in.TickSize).IsZero() {
			return OrderRow{}, bad("INVALID_PRICE", fmt.Sprintf("价格须为最小变动价位 %s 的整数倍", in.TickSize))
		}
		price = decimal.NullDecimal{Decimal: p, Valid: true}
	}

	// For buys we lock premium up-front at the worst-case cost (limit price or
	// best ask). For sells we lock initial margin per contract. Either way the
	// lock is held in system:options and released on cancel / released on fill
	// (into counter-position accounting) / kept on settlement.
	s.mu.Lock()
	defer s.mu.Unlock()

	lock, err := s.lockFor(ctx, userID, in, side, typ, price, qty)
	if err != nil {
		return OrderRow{}, err
	}
	ref := newRef(fmt.Sprintf("optord-%d-", in.ID))
	if err := s.asset.System(ctx, userID, "debit", lock, ref); err != nil {
		if err == ErrSpotInsufficient {
			return OrderRow{}, errInsufficient
		}
		return OrderRow{}, fmt.Errorf("lock: %w", err)
	}

	tx, err := s.db.BeginTx(ctx, nil)
	if err != nil {
		// Give the money back.
		_ = s.asset.System(ctx, userID, "credit", lock, ref+"-rb")
		return OrderRow{}, err
	}
	defer tx.Rollback()

	var orderID int64
	err = tx.QueryRowContext(ctx, `
	    INSERT INTO opt_orders (user_id, instrument_id, client_id, side, order_type, price, qty, status)
	    VALUES ($1,$2,$3,$4,$5,$6,$7,'open') RETURNING id`,
		userID, in.ID, nullIfEmpty(req.ClientID), side, typ, price, qty).Scan(&orderID)
	if err != nil {
		_ = s.asset.System(ctx, userID, "credit", lock, ref+"-rb")
		return OrderRow{}, err
	}

	fills, err := s.matchAndWrite(ctx, tx, in, orderID, userID, side, typ, price, qty)
	if err != nil {
		_ = s.asset.System(ctx, userID, "credit", lock, ref+"-rb")
		return OrderRow{}, err
	}

	// Compute final status + avg price and persist.
	filled := decimal.Zero
	notional := decimal.Zero
	for _, f := range fills {
		filled = filled.Add(f.Qty)
		notional = notional.Add(f.Price.Mul(f.Qty))
	}
	var avg decimal.NullDecimal
	if filled.Sign() > 0 {
		avg = decimal.NullDecimal{Decimal: notional.Div(filled), Valid: true}
	}
	status := "open"
	switch {
	case filled.Equal(qty):
		status = "filled"
	case filled.Sign() > 0:
		status = "partial"
	case typ == "market":
		status = "cancelled" // market with nothing to match
	}
	if _, err := tx.ExecContext(ctx, `
	    UPDATE opt_orders SET filled_qty=$1, avg_fill_price=$2, status=$3, updated_at=now()
	    WHERE id=$4`, filled, avg, status, orderID); err != nil {
		_ = s.asset.System(ctx, userID, "credit", lock, ref+"-rb")
		return OrderRow{}, err
	}

	if err := tx.Commit(); err != nil {
		_ = s.asset.System(ctx, userID, "credit", lock, ref+"-rb")
		return OrderRow{}, err
	}

	// For a market order that went nowhere, give the lock back.
	if filled.Sign() == 0 && typ == "market" {
		_ = s.asset.System(ctx, userID, "credit", lock, ref+"-unfilled")
	}
	// Buy limit filled better than its limit: refund the over-locked premium
	// on the filled quantity so the user is charged only the executed cost.
	// The unfilled remainder stays locked until a later fill or cancel.
	if filled.Sign() > 0 && typ == "limit" && side == "buy" && avg.Valid {
		over := price.Decimal.Sub(avg.Decimal).Mul(filled).Mul(in.ContractSize)
		if over.Sign() > 0 {
			_ = s.asset.System(ctx, userID, "credit", over, ref+"-rebate")
		}
	}

	out, err := s.loadOrder(ctx, orderID)
	if err != nil {
		return OrderRow{}, err
	}
	return out, nil
}

// lockFor returns the up-front hold for a new order. For buys: premium cost
// (limit price * qty, or an aggressive bound for market). For sells: initial
// margin per contract * qty via InitialMarginShort.
func (s *Server) lockFor(ctx context.Context, userID string, in *Instrument, side, typ string, price decimal.NullDecimal, qty decimal.Decimal) (decimal.Decimal, error) {
	if side == "buy" {
		if typ == "limit" {
			return price.Decimal.Mul(qty).Mul(in.ContractSize), nil
		}
		// Market: use best ask if any, else current mark, else fall back to
		// strike (worst case for upper bound).
		if p, ok := s.bestAsk(ctx, in.ID); ok {
			return p.Mul(qty).Mul(in.ContractSize), nil
		}
		if m, ok := s.markPrices[in.ID]; ok && m.Mark.Sign() > 0 {
			return m.Mark.Mul(qty).Mul(in.ContractSize), nil
		}
		return in.Strike.Mul(qty).Mul(in.ContractSize), nil
	}
	// Sell: initial margin per contract (SPAN-style).
	spot, ok := s.underlyings[strings.ToUpper(in.Underlying)]
	if !ok {
		return decimal.Zero, errMarkUnavail
	}
	premium := decimal.Zero
	if typ == "limit" && price.Valid {
		premium = price.Decimal
	} else if m, ok := s.markPrices[in.ID]; ok {
		premium = m.Mark
	}
	marg := InitialMarginShort(
		mustFloat(spot), mustFloat(in.Strike), mustFloat(premium), in.OptionType == "call",
	)
	per := decimal.NewFromFloat(marg)
	return per.Mul(qty).Mul(in.ContractSize), nil
}

// bestAsk returns the lowest active sell price for an instrument.
func (s *Server) bestAsk(ctx context.Context, instrID int64) (decimal.Decimal, bool) {
	var p decimal.Decimal
	err := s.db.QueryRowContext(ctx, `
	    SELECT price FROM opt_orders
	     WHERE instrument_id=$1 AND side='sell' AND order_type='limit'
	       AND status IN ('open','partial') AND price IS NOT NULL
	     ORDER BY price ASC, id ASC LIMIT 1`, instrID).Scan(&p)
	if err != nil {
		return decimal.Zero, false
	}
	return p, true
}

// bestBid returns the highest active buy price for an instrument.
func (s *Server) bestBid(ctx context.Context, instrID int64) (decimal.Decimal, bool) {
	var p decimal.Decimal
	err := s.db.QueryRowContext(ctx, `
	    SELECT price FROM opt_orders
	     WHERE instrument_id=$1 AND side='buy' AND order_type='limit'
	       AND status IN ('open','partial') AND price IS NOT NULL
	     ORDER BY price DESC, id ASC LIMIT 1`, instrID).Scan(&p)
	if err != nil {
		return decimal.Zero, false
	}
	return p, true
}

// loadOrder fetches an order row rendered for the API.
func (s *Server) loadOrder(ctx context.Context, id int64) (OrderRow, error) {
	var o OrderRow
	var price, avg decimal.NullDecimal
	var rej sql.NullString
	err := s.db.QueryRowContext(ctx, `
	    SELECT o.id, o.user_id, o.instrument_id, i.name, o.side, o.order_type,
	           o.price, o.qty, o.filled_qty, o.avg_fill_price, o.status, o.reject_reason,
	           o.created_at, o.updated_at
	      FROM opt_orders o JOIN opt_instruments i ON i.id=o.instrument_id
	     WHERE o.id=$1`, id).Scan(&o.ID, &o.UserID, &o.InstrumentID, &o.Instrument,
		&o.Side, &o.Type, &price, &o.Qty, &o.FilledQty, &avg, &o.Status, &rej,
		&o.CreatedAt, &o.UpdatedAt)
	if err != nil {
		return OrderRow{}, err
	}
	if price.Valid {
		o.Price = price.Decimal
	}
	if avg.Valid {
		o.AvgFillPrice = avg.Decimal
	}
	if rej.Valid {
		o.RejectReason = rej.String
	}
	return o, nil
}

// CancelOrder flips an open/partial order to cancelled and refunds its
// remaining locked margin/premium.
func (s *Server) CancelOrder(ctx context.Context, userID string, orderID int64) (OrderRow, error) {
	s.mu.Lock()
	defer s.mu.Unlock()
	tx, err := s.db.BeginTx(ctx, nil)
	if err != nil {
		return OrderRow{}, err
	}
	defer tx.Rollback()
	var ownerID string
	var side, typ, status string
	var price decimal.NullDecimal
	var qty, filled decimal.Decimal
	var instrID int64
	err = tx.QueryRowContext(ctx, `
	    SELECT user_id, instrument_id, side, order_type, price, qty, filled_qty, status
	      FROM opt_orders WHERE id=$1 FOR UPDATE`, orderID).Scan(
		&ownerID, &instrID, &side, &typ, &price, &qty, &filled, &status)
	if err != nil {
		if err == sql.ErrNoRows {
			return OrderRow{}, errNotFound
		}
		return OrderRow{}, err
	}
	if ownerID != userID {
		return OrderRow{}, errNotFound
	}
	if status != "open" && status != "partial" {
		return OrderRow{}, bad("ORDER_NOT_CANCELLABLE", "订单状态无法撤销")
	}
	if _, err := tx.ExecContext(ctx,
		`UPDATE opt_orders SET status='cancelled', updated_at=now() WHERE id=$1`, orderID); err != nil {
		return OrderRow{}, err
	}
	if err := tx.Commit(); err != nil {
		return OrderRow{}, err
	}
	in := s.instruments[instrID]
	remain := qty.Sub(filled)
	if in != nil && remain.Sign() > 0 {
		lock, _ := s.lockFor(ctx, userID, in, side, typ,
			price, remain)
		if lock.Sign() > 0 {
			_ = s.asset.System(ctx, userID, "credit", lock, newRef("optcxl-"))
		}
	}
	return s.loadOrder(ctx, orderID)
}

// nullIfEmpty returns a NullString; empty maps to NULL for the client_id col.
func nullIfEmpty(s string) sql.NullString {
	if s == "" {
		return sql.NullString{}
	}
	return sql.NullString{String: s, Valid: true}
}

// mustFloat converts a decimal to float64; zero on parse failure.
func mustFloat(d decimal.Decimal) float64 {
	f, _ := d.Float64()
	return f
}

func newRef(prefix string) string {
	b := make([]byte, 8)
	_, _ = rand.Read(b)
	return prefix + hex.EncodeToString(b)
}

// writeErr converts an engine error to the public JSON envelope.
func writeErr(w http.ResponseWriter, err error) {
	if a, ok := asAPI(err); ok {
		writeError(w, a.Status, a.Code, a.Message)
		return
	}
	log.Printf("options: internal error: %v", err)
	writeError(w, http.StatusInternalServerError, "INTERNAL", "服务器内部错误")
}
