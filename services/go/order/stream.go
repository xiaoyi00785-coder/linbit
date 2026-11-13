package main

import (
	"bytes"
	"context"
	"encoding/json"
	"fmt"
	"io"
	"log"
	"net/http"
	"strings"
	"sync"
	"time"

	"github.com/linbit/platform/services/go/common"
	"github.com/shopspring/decimal"
)

// stream.go: the per-symbol consumer of the matching engine's ordered result
// stream. All spot settlement is driven from this stream so that a dropped or
// timed-out HTTP reply can never lose a fill and fills are settled in engine
// order (no cancel-vs-fill races).
//
// The consumer holds an advisory lease on a symbol's cursor row, polls
// GET /engine/reports?after=<seq>, settles each entry, advances the cursor,
// and ACKs the engine. A separate sweep rejects orders whose engine deadline
// has passed below the stream watermark without ever appearing in the stream.

// ---- engine stream client ----

// EngineStream fetches the ordered result stream from the matching engine.
type EngineStream interface {
	StreamPage(ctx context.Context, symbol string, after int64, limit int) (*StreamPageResponse, error)
	Ack(ctx context.Context, symbol string, seq int64) error
}

// StreamPageResponse mirrors the engine's GET /engine/reports response.
type StreamPageResponse struct {
	Entries   []StreamEntry `json:"entries"`
	Head      int64         `json:"head"`
	Watermark int64         `json:"watermark"`
}

// StreamEntry mirrors one entry of the engine's result stream.
type StreamEntry struct {
	Seq    int64      `json:"seq"`
	Ts     int64      `json:"ts"`
	Kind   string     `json:"kind"` // "place" | "cancel"
	Report ExecReport `json:"report"`
}

// HTTPStream implements EngineStream over HTTP.
type HTTPStream struct {
	url   string
	token string
	http  *http.Client
}

// NewHTTPStream constructs an HTTP-backed stream client.
func NewHTTPStream(url, token string) *HTTPStream {
	return &HTTPStream{
		url:   strings.TrimRight(url, "/"),
		token: token,
		http:  &http.Client{Timeout: 10 * time.Second},
	}
}

func (s *HTTPStream) StreamPage(ctx context.Context, symbol string, after int64, limit int) (*StreamPageResponse, error) {
	u := fmt.Sprintf("%s/engine/reports?symbol=%s&after=%d&limit=%d", s.url, symbol, after, limit)
	req, err := http.NewRequestWithContext(ctx, http.MethodGet, u, nil)
	if err != nil {
		return nil, err
	}
	if s.token != "" {
		req.Header.Set("X-Internal-Token", s.token)
	}
	resp, err := s.http.Do(req)
	if err != nil {
		return nil, err
	}
	defer resp.Body.Close()
	data, _ := io.ReadAll(resp.Body)
	if resp.StatusCode != http.StatusOK {
		return nil, fmt.Errorf("engine reports returned %d: %s", resp.StatusCode, string(data))
	}
	var page StreamPageResponse
	if err := json.Unmarshal(data, &page); err != nil {
		return nil, fmt.Errorf("decode reports: %w", err)
	}
	return &page, nil
}

func (s *HTTPStream) Ack(ctx context.Context, symbol string, seq int64) error {
	body, _ := json.Marshal(map[string]any{"symbol": symbol, "seq": seq})
	req, err := http.NewRequestWithContext(ctx, http.MethodPost, s.url+"/engine/reports/ack", bytes.NewReader(body))
	if err != nil {
		return err
	}
	req.Header.Set("Content-Type", "application/json")
	if s.token != "" {
		req.Header.Set("X-Internal-Token", s.token)
	}
	resp, err := s.http.Do(req)
	if err != nil {
		return err
	}
	defer resp.Body.Close()
	if resp.StatusCode != http.StatusOK {
		data, _ := io.ReadAll(resp.Body)
		return fmt.Errorf("engine ack returned %d: %s", resp.StatusCode, string(data))
	}
	return nil
}

// ---- consumer ----

// StreamConsumer runs one goroutine per symbol that polls the engine's result
// stream and settles every entry.
type StreamConsumer struct {
	server    *Server
	stream    EngineStream
	symbols   []string
	pollDelay time.Duration
	sweepEvery time.Duration
}

// NewStreamConsumer constructs a consumer for the given symbols.
func NewStreamConsumer(server *Server, stream EngineStream, symbols []string) *StreamConsumer {
	return &StreamConsumer{
		server:    server,
		stream:    stream,
		symbols:   symbols,
		pollDelay: 50 * time.Millisecond,
		sweepEvery: 5 * time.Second,
	}
}

// Run blocks until ctx is cancelled, running one consumer goroutine per symbol.
func (c *StreamConsumer) Run(ctx context.Context) {
	var wg sync.WaitGroup
	for _, sym := range c.symbols {
		wg.Add(1)
		go func(symbol string) {
			defer wg.Done()
			c.runSymbol(ctx, symbol)
		}(sym)
	}
	wg.Wait()
}

// runSymbol is the per-symbol loop: poll the stream, settle entries in seq
// order, advance the cursor, and periodically sweep for expired orders.
func (c *StreamConsumer) runSymbol(ctx context.Context, symbol string) {
	if err := c.server.store.EnsureCursor(ctx, symbol); err != nil {
		log.Printf("stream[%s]: ensure cursor: %v", symbol, err)
		return
	}

	lastSweep := time.Now()
	for {
		select {
		case <-ctx.Done():
			return
		default:
		}

		cursor, err := c.server.store.GetCursor(ctx, symbol)
		if err != nil {
			log.Printf("stream[%s]: cursor: %v", symbol, err)
			sleepCtx(ctx, c.pollDelay)
			continue
		}

		page, err := c.stream.StreamPage(ctx, symbol, cursor, 500)
		if err != nil {
			log.Printf("stream[%s]: fetch: %v", symbol, err)
			sleepCtx(ctx, c.pollDelay)
			continue
		}

		// Settle entries in seq order. Stop at the first error so the cursor
		// stays behind it; the next poll retries.
		lastSettled := cursor
		for _, entry := range page.Entries {
			if err := c.settleEntry(ctx, symbol, entry); err != nil {
				log.Printf("stream[%s]: settle seq=%d: %v", symbol, entry.Seq, err)
				break
			}
			lastSettled = entry.Seq
		}

		// Advance and ack the cursor.
		if lastSettled > cursor {
			if err := c.server.store.AdvanceCursor(ctx, symbol, lastSettled); err != nil {
				log.Printf("stream[%s]: advance cursor: %v", symbol, err)
			} else if err := c.stream.Ack(ctx, symbol, lastSettled); err != nil {
				log.Printf("stream[%s]: ack: %v", symbol, err)
			}
		}

		// Periodic sweep for orders whose deadline passed without appearing.
		if time.Since(lastSweep) >= c.sweepEvery {
			c.sweepExpired(ctx, symbol, page.Watermark)
			lastSweep = time.Now()
		}

		if lastSettled == cursor {
			sleepCtx(ctx, c.pollDelay)
		}
	}
}

// settleEntry settles one result-stream entry: trades go through the asset
// service (idempotent on trade id), order status advances monotonically, and
// the leftover is released once the order is terminal with no dead letters.
func (c *StreamConsumer) settleEntry(ctx context.Context, symbol string, entry StreamEntry) error {
	rep := entry.Report

	// The engine has seen this order now.
	_ = c.server.store.MarkEngineSeen(ctx, rep.OrderID)

	// A rejected place/cancel never rested and never filled.
	if rep.Status == "rejected" {
		_ = c.server.store.RankUpdateFill(ctx, rep.OrderID, decimal.Zero, "rejected")
		c.releaseLeftoverByID(ctx, rep.OrderID)
		return nil
	}

	sym, err := c.server.store.Symbol(ctx, symbol)
	if err != nil {
		return fmt.Errorf("symbol config: %w", err)
	}

	// Freeze the fee rate the first time the order is seen (not rejected).
	c.recordFeeRates(ctx, rep.OrderID)

	// Settle each trade. A settlement failure is dead-lettered, not fatal: the
	// order still gets its status update, but its leftover is held until the
	// dead letters are resolved.
	for i := range rep.Trades {
		if err := c.settleTrade(ctx, sym, &rep.Trades[i]); err != nil {
			c.deadLetter(ctx, &rep.Trades[i], err)
		}
	}

	// Update resting maker orders touched by this match.
	for _, u := range rep.MakerUpdates {
		filled, _ := decimal.NewFromString(u.FilledQuantity)
		_ = c.server.store.MarkEngineSeen(ctx, u.OrderID)
		_ = c.server.store.RankUpdateFill(ctx, u.OrderID, filled, u.Status)
		c.releaseLeftoverByID(ctx, u.OrderID)
	}

	// Update the taker order.
	filled, _ := decimal.NewFromString(rep.FilledQuantity)
	_ = c.server.store.RankUpdateFill(ctx, rep.OrderID, filled, rep.Status)
	c.releaseLeftoverByID(ctx, rep.OrderID)

	return nil
}

// settleTrade settles one fill through the asset service and records both
// sides locally. Idempotent on (trade_id, order_id).
func (c *StreamConsumer) settleTrade(ctx context.Context, sym *SymbolConfig, t *EngineTrade) error {
	price, err := decimal.NewFromString(t.Price)
	if err != nil {
		return fmt.Errorf("bad price: %w", err)
	}
	qty, err := decimal.NewFromString(t.Quantity)
	if err != nil {
		return fmt.Errorf("bad quantity: %w", err)
	}
	quoteAmount := price.Mul(qty)

	buyerIsTaker := t.TakerSide == "buy"
	buyerUser, sellerUser := t.MakerUserID, t.TakerUserID
	buyerOrder, sellerOrder := t.MakerOrderID, t.TakerOrderID
	buyerRole, sellerRole := "maker", "taker"
	if buyerIsTaker {
		buyerUser, sellerUser = t.TakerUserID, t.MakerUserID
		buyerOrder, sellerOrder = t.TakerOrderID, t.MakerOrderID
		buyerRole, sellerRole = "taker", "maker"
	}
	buyerFeeBase := qty.Mul(c.server.feeRateFor(ctx, buyerOrder, buyerUser, buyerRole))
	sellerFeeQuote := quoteAmount.Mul(c.server.feeRateFor(ctx, sellerOrder, sellerUser, sellerRole))

	if err := c.server.asset.Settle(ctx, SettleParams{
		TradeID:        t.TradeID,
		Base:           sym.Base,
		Quote:          sym.Quote,
		BuyerUserID:    buyerUser,
		SellerUserID:   sellerUser,
		FeeAccount:     c.server.cfg.FeeAccount,
		QuoteAmount:    quoteAmount,
		BaseQty:        qty,
		BuyerFeeBase:   buyerFeeBase,
		SellerFeeQuote: sellerFeeQuote,
	}); err != nil {
		return fmt.Errorf("settle: %w", err)
	}

	if err := c.server.store.InsertTrade(ctx, TradeRow{
		TradeID: t.TradeID, OrderID: buyerOrder, UserID: buyerUser, Symbol: sym.Symbol,
		Side: "buy", Price: price, Quantity: qty, Fee: buyerFeeBase, FeeAsset: sym.Base, Role: buyerRole,
	}); err != nil {
		return fmt.Errorf("insert buyer trade: %w", err)
	}
	if err := c.server.store.InsertTrade(ctx, TradeRow{
		TradeID: t.TradeID, OrderID: sellerOrder, UserID: sellerUser, Symbol: sym.Symbol,
		Side: "sell", Price: price, Quantity: qty, Fee: sellerFeeQuote, FeeAsset: sym.Quote, Role: sellerRole,
	}); err != nil {
		return fmt.Errorf("insert seller trade: %w", err)
	}
	return nil
}

// deadLetter records a settlement failure for manual review and alerts.
func (c *StreamConsumer) deadLetter(ctx context.Context, t *EngineTrade, settleErr error) {
	userID := t.TakerUserID
	orderID := t.TakerOrderID
	if err := c.server.store.RecordSettlementFailure(ctx, t.TradeID, orderID, userID, t.Symbol, settleErr.Error()); err != nil {
		log.Printf("stream: dead letter %s: %v", t.TradeID, err)
	}
	common.AlertTelegram(fmt.Sprintf("Settlement failure: trade %s order %s user %s: %v", t.TradeID, orderID, userID, settleErr))
}

// releaseLeftoverByID releases the unspent frozen funds of a terminal order,
// exactly once. The release_state column ensures idempotency; unresolved dead
// letters block the release.
func (c *StreamConsumer) releaseLeftoverByID(ctx context.Context, orderID string) {
	o, err := c.server.store.OrderByID(ctx, orderID)
	if err != nil {
		return
	}
	if !terminal(o.Status) {
		return
	}
	won, err := c.server.store.ClaimRelease(ctx, orderID)
	if err != nil || !won {
		return
	}
	consumed, err := c.server.store.Consumed(ctx, orderID, o.Side)
	if err != nil {
		log.Printf("stream: release %s: consumed: %v", orderID, err)
		return
	}
	leftover := o.FrozenAmount.Sub(consumed)
	if leftover.Sign() <= 0 {
		return
	}
	if err := c.server.asset.Unfreeze(ctx, o.UserID, o.FrozenAsset, leftover, "order:"+orderID+":leftover"); err != nil {
		log.Printf("stream: release %s: unfreeze: %v", orderID, err)
	}
}

// sweepExpired rejects orders whose engine deadline is below the stream
// watermark and that never appeared in the stream — the engine never got them.
func (c *StreamConsumer) sweepExpired(ctx context.Context, symbol string, watermark int64) {
	orders, err := c.server.store.ExpiredUnseenOrders(ctx, symbol, watermark)
	if err != nil {
		log.Printf("stream[%s]: sweep: %v", symbol, err)
		return
	}
	for _, o := range orders {
		_ = c.server.store.RankUpdateFill(ctx, o.OrderID, decimal.Zero, "rejected")
		c.releaseLeftoverByID(ctx, o.OrderID)
		log.Printf("stream[%s]: expired unseen order %s (deadline %d < watermark %d)",
			symbol, o.OrderID, 0, watermark)
	}
}

// recordFeeRates freezes the maker and taker fee rates for an order the first
// time it is seen by the engine.
func (c *StreamConsumer) recordFeeRates(ctx context.Context, orderID string) {
	o, err := c.server.store.OrderByID(ctx, orderID)
	if err != nil {
		return
	}
	maker := c.server.userFeeRate(o.UserID, "maker")
	taker := c.server.userFeeRate(o.UserID, "taker")
	_ = c.server.store.RecordTradeFee(ctx, orderID, "maker", maker)
	_ = c.server.store.RecordTradeFee(ctx, orderID, "taker", taker)
}

// feeRateFor returns the fee rate for an order+role from the frozen trade_fees
// table, falling back to the live rate.
func (s *Server) feeRateFor(ctx context.Context, orderID, userID, role string) decimal.Decimal {
	rate, err := s.store.GetTradeFee(ctx, orderID, role)
	if err == nil && rate.GreaterThan(decimal.Zero) {
		return rate
	}
	return s.userFeeRate(userID, role)
}

// sleepCtx sleeps for d or until ctx is cancelled.
func sleepCtx(ctx context.Context, d time.Duration) {
	select {
	case <-ctx.Done():
	case <-time.After(d):
	}
}
