package main

import (
	"context"
	"errors"
	"log"
	"net/http"
	"strconv"
	"strings"
	"time"

	"github.com/linbit/platform/services/go/common"
	"github.com/shopspring/decimal"
)

// Server wires configuration, storage, the asset client and the matching engine
// into the order service HTTP handlers.
type Server struct {
	cfg    Config
	store  *Store
	asset  *AssetClient
	engine Matching
	fees   *feeOverrides
	ufees  *userFeeCache
}

// NewServer constructs a Server.
func NewServer(cfg Config, store *Store, asset *AssetClient, engine Matching) *Server {
	return &Server{cfg: cfg, store: store, asset: asset, engine: engine, fees: &feeOverrides{}, ufees: &userFeeCache{m: map[string]userFee{}}}
}

// Routes returns the order service HTTP handler. The /spot/* paths mirror
// docs/api/openapi.yaml and expect the X-User-Id header injected by the gateway.
func (s *Server) Routes() http.Handler {
	mux := http.NewServeMux()
	mux.HandleFunc("GET /healthz", func(w http.ResponseWriter, r *http.Request) { w.Write([]byte("ok")) })

	mux.HandleFunc("POST /spot/orders", s.withUser(s.handlePlace))
	mux.HandleFunc("DELETE /spot/orders/{orderId}", s.withUser(s.handleCancel))
	mux.HandleFunc("GET /spot/orders/open", s.withUser(s.handleOpenOrders))
	mux.HandleFunc("GET /spot/orders/history", s.withUser(s.handleHistory))
	mux.HandleFunc("GET /spot/trades", s.withUser(s.handleTrades))
	mux.HandleFunc("GET /spot/pnl/history", s.withUser(s.handleSpotPnlHistory))
	mux.HandleFunc("POST /spot/orders/conditional", s.withUser(s.handleCondCreate))
	mux.HandleFunc("GET /spot/orders/conditional", s.withUser(s.handleCondList))
	mux.HandleFunc("DELETE /spot/orders/conditional/{id}", s.withUser(s.handleCondCancel))
	mux.HandleFunc("GET /account/fee-rates", s.withUser(s.handleFeeRates))
	mux.HandleFunc("GET /spot/fee-schedule", s.handleFeeSchedule) // public default rates
	s.adminRoutes(mux)
	s.internalRoutes(mux)
	return mux
}

// withUser extracts the gateway-injected user id and rejects anonymous callers.
func (s *Server) withUser(next func(w http.ResponseWriter, r *http.Request, userID string)) http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		userID := r.Header.Get("X-User-Id")
		if userID == "" {
			common.WriteError(w, http.StatusUnauthorized, "AUTH_UNAUTHORIZED", "请先登录")
			return
		}
		next(w, r, userID)
	}
}

// ---- DTOs ----

type placeOrderRequest struct {
	Symbol        string  `json:"symbol"`
	Side          string  `json:"side"`
	Type          string  `json:"type"`
	Price         *string `json:"price"`
	Quantity      *string `json:"quantity"`
	QuoteOrderQty *string `json:"quoteOrderQty"` // market-buy budget (extension)
	TimeInForce   *string `json:"timeInForce"`
	ClientOrderID *string `json:"clientOrderId"`
	// PostOnly is an alias for timeInForce=PostOnly (also accepted as "GTX").
	PostOnly *bool `json:"postOnly,omitempty"`
}

type orderDTO struct {
	OrderID        string  `json:"orderId"`
	ClientOrderID  *string `json:"clientOrderId"`
	Symbol         string  `json:"symbol"`
	Side           string  `json:"side"`
	Type           string  `json:"type"`
	Price          *string `json:"price"`
	Quantity       string  `json:"quantity"`
	QuoteOrderQty  *string `json:"quoteOrderQty,omitempty"`
	FilledQuantity string  `json:"filledQuantity"`
	Status         string  `json:"status"`
	TimeInForce    string  `json:"timeInForce"`
	CreatedAt      string  `json:"createdAt"`
}

type tradeDTO struct {
	TradeID   string  `json:"tradeId"`
	OrderID   string  `json:"orderId"`
	Symbol    string  `json:"symbol"`
	Side      string  `json:"side"`
	Price     string  `json:"price"`
	Quantity  string  `json:"quantity"`
	Fee       *string `json:"fee"`
	FeeAsset  *string `json:"feeAsset"`
	Role      string  `json:"role"`
	CreatedAt string  `json:"createdAt"`
}

type paginated struct {
	Items    any `json:"items"`
	Page     int `json:"page"`
	PageSize int `json:"pageSize"`
	Total    int `json:"total"`
}

const rfc3339 = "2006-01-02T15:04:05Z07:00"

// ---- place ----

func (s *Server) handlePlace(w http.ResponseWriter, r *http.Request, userID string) {
	var req placeOrderRequest
	if err := common.DecodeJSON(r, &req); err != nil {
		common.WriteError(w, http.StatusBadRequest, "VALIDATION_ERROR", "请求参数不合法")
		return
	}
	dto, perr := s.placeOrder(r.Context(), userID, req)
	if perr != nil {
		common.WriteError(w, perr.status, perr.code, perr.msg)
		return
	}
	common.WriteJSON(w, http.StatusCreated, dto)
}

// placeError is a placement failure mapped to an HTTP error.
type placeError struct {
	status int
	code   string
	msg    string
}

func perrf(status int, code, msg string) *placeError { return &placeError{status, code, msg} }

// placeOrder validates, freezes, persists, and submits one order to the
// matching engine. Settlement is driven asynchronously by the stream consumer;
// this path only determines whether the order was definitively rejected (400)
// or is live/ambiguous (return the order in its initial state).
func (s *Server) placeOrder(ctx context.Context, userID string, req placeOrderRequest) (*orderDTO, *placeError) {
	o, sym, errCode, errMsg := s.buildOrder(ctx, userID, req)
	if errCode != "" {
		if errCode == "INTERNAL" {
			return nil, perrf(http.StatusInternalServerError, errCode, errMsg)
		}
		if errCode == "ENGINE_UNAVAILABLE" {
			return nil, perrf(http.StatusBadGateway, errCode, errMsg)
		}
		return nil, perrf(http.StatusBadRequest, errCode, errMsg)
	}

	// Set a 10-second engine-clock deadline. If the place doesn't reach the
	// engine before this instant the engine rejects it as `expired` before
	// any WAL write, so it can never execute later.
	o.EngineDeadline = time.Now().Add(10 * time.Second).UnixMilli()

	// Freeze funds before the order can match, so a fill can never settle
	// against money the user does not have. Reject on insufficient balance.
	if err := s.asset.Freeze(ctx, userID, o.FrozenAsset, o.FrozenAmount, "order:"+o.OrderID); err != nil {
		if errors.Is(err, ErrInsufficient) {
			return nil, perrf(http.StatusBadRequest, "INSUFFICIENT_BALANCE", "余额不足")
		}
		log.Printf("place: freeze: %v", err)
		return nil, perrf(http.StatusInternalServerError, "INTERNAL", "服务器内部错误")
	}

	if err := s.store.InsertOrder(ctx, o); err != nil {
		log.Printf("place: insert order: %v", err)
		_ = s.asset.Unfreeze(ctx, userID, o.FrozenAsset, o.FrozenAmount, "order:"+o.OrderID)
		return nil, perrf(http.StatusInternalServerError, "INTERNAL", "服务器内部错误")
	}

	// Submit to the matching engine. A 400 response means the order was
	// definitively rejected (never applied); anything else leaves the outcome
	// ambiguous — the stream consumer will resolve it.
	rep, err := s.engine.Submit(ctx, s.placeCommand(o))
	if err != nil {
		// Transport error or 5xx: outcome unknown, leave the order as `new`.
		// The stream consumer will confirm or expire it.
		log.Printf("place: engine submit (ambiguous): %v", err)
		fresh, _ := s.store.GetOrder(ctx, userID, o.OrderID)
		if fresh == nil {
			fresh = o
		}
		dto := toOrderDTO(fresh, sym)
		return &dto, nil
	}

	if rep.Status == "rejected" {
		reason := "订单被拒绝"
		if rep.RejectReason != nil {
			reason = *rep.RejectReason
		}
		_ = s.store.RankUpdateFill(ctx, o.OrderID, decimal.Zero, "rejected")
		won, _ := s.store.ClaimRelease(ctx, o.OrderID)
		if won {
			consumed, _ := s.store.Consumed(ctx, o.OrderID, o.Side)
			leftover := o.FrozenAmount.Sub(consumed)
			if leftover.Sign() > 0 {
				_ = s.asset.Unfreeze(ctx, userID, o.FrozenAsset, leftover, "order:"+o.OrderID+":leftover")
			}
		}
		if strings.Contains(reason, "post_only") {
			return nil, perrf(http.StatusBadRequest, "POST_ONLY_REJECTED", "只做 Maker 订单会立即成交，已被拒绝")
		}
		return nil, perrf(http.StatusBadRequest, "ORDER_REJECTED", reason)
	}

	// A non-rejection response (new, partially_filled, filled, canceled) means
	// the engine has applied the order. Settlement arrives via the stream; just
	// return the current state.
	fresh, err := s.store.GetOrder(ctx, userID, o.OrderID)
	if err != nil {
		log.Printf("place: reload order: %v", err)
		return nil, perrf(http.StatusInternalServerError, "INTERNAL", "服务器内部错误")
	}
	dto := toOrderDTO(fresh, sym)
	return &dto, nil
}

// buildOrder validates the request against the symbol rules and computes the
// order's frozen asset/amount. On validation failure it returns a code+message.
func (s *Server) buildOrder(ctx context.Context, userID string, req placeOrderRequest) (*Order, *SymbolConfig, string, string) {
	side := strings.ToLower(req.Side)
	typ := strings.ToLower(req.Type)
	if side != "buy" && side != "sell" {
		return nil, nil, "VALIDATION_ERROR", "side 必须为 buy 或 sell"
	}
	if typ != "limit" && typ != "market" {
		return nil, nil, "VALIDATION_ERROR", "type 必须为 limit 或 market"
	}

	sym, err := s.store.Symbol(ctx, req.Symbol)
	if errors.Is(err, ErrSymbolNotFound) {
		return nil, nil, "VALIDATION_ERROR", "交易对不存在"
	}
	if err != nil {
		log.Printf("buildOrder: symbol: %v", err)
		return nil, nil, "INTERNAL", "服务器内部错误"
	}
	if sym.Status != "trading" {
		return nil, nil, "VALIDATION_ERROR", "交易对暂停交易"
	}

	tif := "GTC"
	if req.TimeInForce != nil && *req.TimeInForce != "" {
		tif = normalizeTIF(*req.TimeInForce)
	}
	postOnly := req.PostOnly != nil && *req.PostOnly
	if postOnly {
		if typ == "market" {
			return nil, nil, "VALIDATION_ERROR", "市价单不支持只做 Maker"
		}
		if req.TimeInForce != nil && *req.TimeInForce != "" && tif != "PostOnly" && tif != "GTC" {
			return nil, nil, "VALIDATION_ERROR", "postOnly 与 timeInForce 冲突"
		}
		tif = "PostOnly"
	}
	if typ == "market" {
		if tif == "PostOnly" {
			return nil, nil, "VALIDATION_ERROR", "市价单不支持只做 Maker"
		}
		tif = "IOC" // market orders are immediate-or-cancel by nature
	} else if !validTIF(tif) {
		return nil, nil, "VALIDATION_ERROR", "timeInForce 必须为 GTC/IOC/FOK/PostOnly/GTX"
	}

	o := &Order{
		OrderID:        "", // assigned below
		UserID:         userID,
		Symbol:         sym.Symbol,
		Side:           side,
		Type:           typ,
		TimeInForce:    tif,
		FilledQuantity: decimal.Zero,
		Status:         "new",
	}
	if req.ClientOrderID != nil {
		o.ClientOrderID = *req.ClientOrderID
	}

	if typ == "market" && side == "buy" {
		// Market buy by quote budget: freeze the quote amount.
		if req.QuoteOrderQty == nil || *req.QuoteOrderQty == "" {
			if req.Quantity == nil || *req.Quantity == "" {
				return nil, nil, "VALIDATION_ERROR", "市价买单需提供 quantity 或 quoteOrderQty"
			}
			return s.marketBuyByQty(ctx, o, sym, *req.Quantity)
		}
		budget, err := decimal.NewFromString(*req.QuoteOrderQty)
		if err != nil || budget.Sign() <= 0 {
			return nil, nil, "VALIDATION_ERROR", "quoteOrderQty 不合法"
		}
		if budget.LessThan(sym.MinNotional) {
			return nil, nil, "VALIDATION_ERROR", "低于最小下单额"
		}
		o.QuoteOrderQty = budget
		o.HasQuoteQty = true
		o.Quantity = decimal.Zero
		o.FrozenAsset = sym.Quote
		o.FrozenAmount = budget
	} else {
		// Everything else is driven by base quantity.
		if req.Quantity == nil {
			return nil, nil, "VALIDATION_ERROR", "quantity 为必填"
		}
		qty, err := decimal.NewFromString(*req.Quantity)
		if err != nil || qty.Sign() <= 0 {
			return nil, nil, "VALIDATION_ERROR", "quantity 不合法"
		}
		if !precisionOK(qty, sym.QuantityPrecision) {
			return nil, nil, "VALIDATION_ERROR", "数量精度超出限制"
		}
		if qty.LessThan(sym.MinQuantity) {
			return nil, nil, "VALIDATION_ERROR", "低于最小下单数量"
		}
		o.Quantity = qty

		if typ == "limit" {
			if req.Price == nil || *req.Price == "" {
				return nil, nil, "VALIDATION_ERROR", "限价单需提供 price"
			}
			price, err := decimal.NewFromString(*req.Price)
			if err != nil || price.Sign() <= 0 {
				return nil, nil, "VALIDATION_ERROR", "price 不合法"
			}
			if !precisionOK(price, sym.PricePrecision) {
				return nil, nil, "VALIDATION_ERROR", "价格精度超出限制"
			}
			notional := price.Mul(qty)
			if notional.LessThan(sym.MinNotional) {
				return nil, nil, "VALIDATION_ERROR", "低于最小下单额"
			}
			o.Price = price
			o.HasPrice = true
			if side == "buy" {
				o.FrozenAsset = sym.Quote
				o.FrozenAmount = notional // max spend at the limit price
			} else {
				o.FrozenAsset = sym.Base
				o.FrozenAmount = qty
			}
		} else {
			// Market sell: freeze the base quantity being sold.
			o.FrozenAsset = sym.Base
			o.FrozenAmount = qty
		}
	}

	orderID, err := s.store.NextOrderID(ctx)
	if err != nil {
		log.Printf("buildOrder: next id: %v", err)
		return nil, nil, "INTERNAL", "服务器内部错误"
	}
	o.OrderID = orderID
	return o, sym, "", ""
}

func (s *Server) placeCommand(o *Order) EngineCommand {
	cmd := EngineCommand{
		Cmd:         "place",
		Symbol:      o.Symbol,
		OrderID:     o.OrderID,
		UserID:      o.UserID,
		Side:        o.Side,
		Type:        o.Type,
		TimeInForce: o.TimeInForce,
		Quantity:    o.Quantity.String(),
		DeadlineMs:  o.EngineDeadline,
	}
	if o.Type == "market" && o.Side == "buy" && !o.HasQuoteQty && o.HasPrice {
		cmd.Type = "limit"
		cmd.TimeInForce = "IOC"
	}
	if o.HasPrice {
		cmd.Price = o.Price.String()
	}
	if o.HasQuoteQty {
		cmd.QuoteOrderQty = o.QuoteOrderQty.String()
	}
	return cmd
}

// ---- cancel ----

// handleCancel submits a cancel to the engine. The engine's cancel report
// arrives via the result stream and the stream consumer settles it; this
// handler just validates the order is live and forwards the command.
// A 502 means the engine is unreachable; the order stays open and may be
// retried. The cancel report from the stream will update status and release
// leftover funds atomically.
func (s *Server) handleCancel(w http.ResponseWriter, r *http.Request, userID string) {
	orderID := r.PathValue("orderId")
	o, err := s.store.GetOrder(r.Context(), userID, orderID)
	if errors.Is(err, ErrOrderNotFound) {
		common.WriteError(w, http.StatusNotFound, "NOT_FOUND", "订单不存在")
		return
	}
	if err != nil {
		log.Printf("cancel: get order: %v", err)
		common.WriteError(w, http.StatusInternalServerError, "INTERNAL", "服务器内部错误")
		return
	}
	if o.Status != "new" && o.Status != "partially_filled" {
		common.WriteError(w, http.StatusBadRequest, "ORDER_NOT_CANCELABLE", "订单无法撤销")
		return
	}

	_, err = s.engine.Submit(r.Context(), EngineCommand{Cmd: "cancel", Symbol: o.Symbol, OrderID: orderID})
	if err != nil {
		log.Printf("cancel: engine submit: %v", err)
		common.WriteError(w, http.StatusBadGateway, "ENGINE_UNAVAILABLE", "撮合引擎不可用")
		return
	}
	// The engine accepted the cancel; the stream consumer will deliver the
	// cancel report (with any final fills) and release the leftover. Return
	// the current order state so the caller can poll for the final status.
	fresh, err := s.store.GetOrder(r.Context(), userID, orderID)
	if err != nil {
		log.Printf("cancel: reload: %v", err)
		common.WriteError(w, http.StatusInternalServerError, "INTERNAL", "服务器内部错误")
		return
	}
	sym, err := s.store.Symbol(r.Context(), fresh.Symbol)
	if err != nil {
		log.Printf("cancel: symbol: %v", err)
		common.WriteError(w, http.StatusInternalServerError, "INTERNAL", "服务器内部错误")
		return
	}
	common.WriteJSON(w, http.StatusOK, toOrderDTO(fresh, sym))
}

// ---- queries ----

func (s *Server) handleOpenOrders(w http.ResponseWriter, r *http.Request, userID string) {
	page, size := pageParams(r)
	orders, total, err := s.store.OpenOrders(r.Context(), userID, r.URL.Query().Get("symbol"), page, size)
	if err != nil {
		log.Printf("open orders: %v", err)
		common.WriteError(w, http.StatusInternalServerError, "INTERNAL", "服务器内部错误")
		return
	}
	syms, err := s.symbolMap(r.Context())
	if err != nil {
		log.Printf("open orders: symbols: %v", err)
		common.WriteError(w, http.StatusInternalServerError, "INTERNAL", "服务器内部错误")
		return
	}
	common.WriteJSON(w, http.StatusOK, paginated{Items: toOrderDTOs(orders, syms), Page: page, PageSize: size, Total: total})
}

func (s *Server) handleHistory(w http.ResponseWriter, r *http.Request, userID string) {
	page, size := pageParams(r)
	orders, total, err := s.store.OrderHistory(r.Context(), userID, r.URL.Query().Get("symbol"), r.URL.Query().Get("status"), page, size)
	if err != nil {
		log.Printf("history: %v", err)
		common.WriteError(w, http.StatusInternalServerError, "INTERNAL", "服务器内部错误")
		return
	}
	syms, err := s.symbolMap(r.Context())
	if err != nil {
		log.Printf("history: symbols: %v", err)
		common.WriteError(w, http.StatusInternalServerError, "INTERNAL", "服务器内部错误")
		return
	}
	common.WriteJSON(w, http.StatusOK, paginated{Items: toOrderDTOs(orders, syms), Page: page, PageSize: size, Total: total})
}

func (s *Server) handleTrades(w http.ResponseWriter, r *http.Request, userID string) {
	page, size := pageParams(r)
	trades, total, err := s.store.MyTrades(r.Context(), userID, r.URL.Query().Get("symbol"), page, size)
	if err != nil {
		log.Printf("trades: %v", err)
		common.WriteError(w, http.StatusInternalServerError, "INTERNAL", "服务器内部错误")
		return
	}
	syms, err := s.symbolMap(r.Context())
	if err != nil {
		log.Printf("trades: symbols: %v", err)
		common.WriteError(w, http.StatusInternalServerError, "INTERNAL", "服务器内部错误")
		return
	}
	items := make([]tradeDTO, 0, len(trades))
	for _, t := range trades {
		items = append(items, toTradeDTO(t, syms[t.Symbol]))
	}
	common.WriteJSON(w, http.StatusOK, paginated{Items: items, Page: page, PageSize: size, Total: total})
}

// symbolMap loads all trading-pair configs keyed by symbol, so list responses
// can format each row's price/quantity to that pair's precision.
func (s *Server) symbolMap(ctx context.Context) (map[string]*SymbolConfig, error) {
	syms, err := s.store.Symbols(ctx)
	if err != nil {
		return nil, err
	}
	m := make(map[string]*SymbolConfig, len(syms))
	for i := range syms {
		m[syms[i].Symbol] = &syms[i]
	}
	return m, nil
}

// feeRate returns the configured spot fee rate for a maker/taker role.
func (s *Server) feeRate(role string) decimal.Decimal {
	maker, taker := s.currentFees()
	if role == "taker" {
		return taker
	}
	return maker
}

// ---- helpers ----

// normalizeTIF maps aliases onto the engine's TIF names (GTX = PostOnly).
func normalizeTIF(tif string) string {
	switch strings.ToUpper(strings.TrimSpace(tif)) {
	case "GTX", "POSTONLY", "POST_ONLY":
		return "PostOnly"
	case "GTC", "IOC", "FOK":
		return strings.ToUpper(strings.TrimSpace(tif))
	}
	return tif
}

func validTIF(tif string) bool {
	switch tif {
	case "GTC", "IOC", "FOK", "PostOnly":
		return true
	}
	return false
}

func terminal(status string) bool {
	switch status {
	case "filled", "canceled", "rejected":
		return true
	}
	return false
}

func precisionOK(d decimal.Decimal, places int) bool {
	return d.Equal(d.Truncate(int32(places)))
}

func pageParams(r *http.Request) (int, int) {
	return atoiDefault(r.URL.Query().Get("page"), 1, 1, 1<<30),
		atoiDefault(r.URL.Query().Get("pageSize"), 20, 1, 100)
}

func atoiDefault(s string, def, min, max int) int {
	if s == "" {
		return def
	}
	n, err := strconv.Atoi(s)
	if err != nil || n < min {
		return def
	}
	if n > max {
		return max
	}
	return n
}

// Price/quantity amounts are rendered as fixed-precision decimal strings scaled
// to the trading pair's rules (never floats): prices and quote-denominated
// amounts to price_precision, base quantities to quantity_precision. When the
// symbol config is unavailable (should not happen for a valid order) the raw
// decimal string is used so no information is lost.

func fmtQty(d decimal.Decimal, sym *SymbolConfig) string {
	if sym == nil {
		return d.String()
	}
	return d.StringFixed(int32(sym.QuantityPrecision))
}

func fmtPrice(d decimal.Decimal, sym *SymbolConfig) string {
	if sym == nil {
		return d.String()
	}
	return d.StringFixed(int32(sym.PricePrecision))
}

// fmtFee scales a fee to the precision of its charging asset: base fees to
// quantity_precision, quote fees to price_precision. A non-zero fee is never
// rounded away: when it has more decimals than the asset precision (e.g. a
// 0.000000065 BTC fee on a tiny fill) the exact value is returned instead.
func fmtFee(d decimal.Decimal, feeAsset string, sym *SymbolConfig) string {
	prec := int32(-1)
	switch {
	case sym == nil:
	case feeAsset == sym.Base:
		prec = int32(sym.QuantityPrecision)
	case feeAsset == sym.Quote:
		prec = int32(sym.PricePrecision)
	}
	if prec < 0 {
		return d.String()
	}
	if !d.IsZero() && !d.Equal(d.Round(prec)) {
		return d.String()
	}
	return d.StringFixed(prec)
}

func toOrderDTO(o *Order, sym *SymbolConfig) orderDTO {
	d := orderDTO{
		OrderID:        o.OrderID,
		Symbol:         o.Symbol,
		Side:           o.Side,
		Type:           o.Type,
		Quantity:       fmtQty(o.Quantity, sym),
		FilledQuantity: fmtQty(o.FilledQuantity, sym),
		Status:         o.Status,
		TimeInForce:    o.TimeInForce,
		CreatedAt:      o.CreatedAt.UTC().Format(rfc3339),
	}
	if o.ClientOrderID != "" {
		c := o.ClientOrderID
		d.ClientOrderID = &c
	}
	if o.HasPrice {
		p := fmtPrice(o.Price, sym)
		d.Price = &p
	}
	if o.HasQuoteQty {
		q := fmtPrice(o.QuoteOrderQty, sym) // quote-denominated budget
		d.QuoteOrderQty = &q
	}
	return d
}

func toOrderDTOs(orders []Order, syms map[string]*SymbolConfig) []orderDTO {
	out := make([]orderDTO, 0, len(orders))
	for i := range orders {
		out = append(out, toOrderDTO(&orders[i], syms[orders[i].Symbol]))
	}
	return out
}

func toTradeDTO(t TradeRow, sym *SymbolConfig) tradeDTO {
	d := tradeDTO{
		TradeID:   t.TradeID,
		OrderID:   t.OrderID,
		Symbol:    t.Symbol,
		Side:      t.Side,
		Price:     fmtPrice(t.Price, sym),
		Quantity:  fmtQty(t.Quantity, sym),
		Role:      t.Role,
		CreatedAt: t.CreatedAt.UTC().Format(rfc3339),
	}
	fee := fmtFee(t.Fee, t.FeeAsset, sym)
	d.Fee = &fee
	if t.FeeAsset != "" {
		fa := t.FeeAsset
		d.FeeAsset = &fa
	}
	return d
}

// marketBuySlippage caps how far above the best ask a market buy placed by base
// quantity may fill (slippage protection). Overridable via MARKET_BUY_SLIPPAGE.
var marketBuySlippage = decimal.RequireFromString(common.Env("MARKET_BUY_SLIPPAGE", "0.05"))

// marketBuyByQty completes a market buy given in base quantity: it freezes
// qty × protectionPrice of quote, where protectionPrice = bestAsk × (1+slippage)
// rounded up to the pair's price precision. The order is stored as type=market
// with that protection price, and submitted to the engine as IOC limit.
func (s *Server) marketBuyByQty(ctx context.Context, o *Order, sym *SymbolConfig, qtyStr string) (*Order, *SymbolConfig, string, string) {
	qty, err := decimal.NewFromString(qtyStr)
	if err != nil || qty.Sign() <= 0 {
		return nil, nil, "VALIDATION_ERROR", "quantity 不合法"
	}
	if !precisionOK(qty, sym.QuantityPrecision) {
		return nil, nil, "VALIDATION_ERROR", "数量精度超出限制"
	}
	if qty.LessThan(sym.MinQuantity) {
		return nil, nil, "VALIDATION_ERROR", "低于最小下单数量"
	}
	bt, ok := s.engine.(BookTop)
	if !ok {
		return nil, nil, "VALIDATION_ERROR", "市价买单需提供 quoteOrderQty"
	}
	askStr, err := bt.BestAsk(ctx, sym.Symbol)
	if err != nil {
		log.Printf("buildOrder: best ask: %v", err)
		return nil, nil, "ENGINE_UNAVAILABLE", "撮合引擎不可用"
	}
	ask, err := decimal.NewFromString(askStr)
	if askStr == "" || err != nil || ask.Sign() <= 0 {
		return nil, nil, "NO_LIQUIDITY", "当前无卖单，暂时无法市价买入"
	}
	protect := ask.Mul(decimal.NewFromInt(1).Add(marketBuySlippage)).RoundCeil(int32(sym.PricePrecision))
	notional := protect.Mul(qty)
	if ask.Mul(qty).LessThan(sym.MinNotional) {
		return nil, nil, "VALIDATION_ERROR", "低于最小下单额"
	}
	o.Quantity = qty
	o.Price = protect
	o.HasPrice = true
	o.FrozenAsset = sym.Quote
	o.FrozenAmount = notional
	orderID, err := s.store.NextOrderID(ctx)
	if err != nil {
		log.Printf("buildOrder: next id: %v", err)
		return nil, nil, "INTERNAL", "服务器内部错误"
	}
	o.OrderID = orderID
	return o, sym, "", ""
}
