package main

import (
	"context"
	"errors"
	"log"
	"net/http"
	"strconv"
	"strings"

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
}

// NewServer constructs a Server.
func NewServer(cfg Config, store *Store, asset *AssetClient, engine Matching) *Server {
	return &Server{cfg: cfg, store: store, asset: asset, engine: engine}
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

	o, sym, errCode, errMsg := s.buildOrder(r.Context(), userID, req)
	if errCode != "" {
		common.WriteError(w, http.StatusBadRequest, errCode, errMsg)
		return
	}

	// Freeze funds before the order can match, so a fill can never settle against
	// money the user does not have. Reject on insufficient balance.
	if err := s.asset.Freeze(r.Context(), userID, o.FrozenAsset, o.FrozenAmount, "order:"+o.OrderID); err != nil {
		if errors.Is(err, ErrInsufficient) {
			common.WriteError(w, http.StatusBadRequest, "INSUFFICIENT_BALANCE", "余额不足")
			return
		}
		log.Printf("place: freeze: %v", err)
		common.WriteError(w, http.StatusInternalServerError, "INTERNAL", "服务器内部错误")
		return
	}

	if err := s.store.InsertOrder(r.Context(), o); err != nil {
		log.Printf("place: insert order: %v", err)
		// Best-effort release of the funds we just froze.
		_ = s.asset.Unfreeze(r.Context(), userID, o.FrozenAsset, o.FrozenAmount, "order:"+o.OrderID)
		common.WriteError(w, http.StatusInternalServerError, "INTERNAL", "服务器内部错误")
		return
	}

	// Submit to the matching engine and settle whatever it reports.
	rep, err := s.engine.Submit(r.Context(), s.placeCommand(o))
	if err != nil {
		log.Printf("place: engine submit: %v", err)
		// The engine never saw the order (or we never saw its reply): unwind.
		_ = s.store.UpdateFill(r.Context(), o.OrderID, decimal.Zero, "rejected")
		s.releaseLeftover(r.Context(), o)
		common.WriteError(w, http.StatusBadGateway, "ENGINE_UNAVAILABLE", "撮合引擎不可用")
		return
	}

	if rep.Status == "rejected" {
		reason := "订单被拒绝"
		if rep.RejectReason != nil {
			reason = *rep.RejectReason
		}
		_ = s.store.UpdateFill(r.Context(), o.OrderID, decimal.Zero, "rejected")
		s.releaseLeftover(r.Context(), o)
		common.WriteError(w, http.StatusBadRequest, "ORDER_REJECTED", reason)
		return
	}

	if err := s.applyReport(r.Context(), o, sym, rep); err != nil {
		log.Printf("place: apply report: %v", err)
		common.WriteError(w, http.StatusInternalServerError, "INTERNAL", "成交结算失败")
		return
	}

	fresh, err := s.store.GetOrder(r.Context(), userID, o.OrderID)
	if err != nil {
		log.Printf("place: reload order: %v", err)
		common.WriteError(w, http.StatusInternalServerError, "INTERNAL", "服务器内部错误")
		return
	}
	common.WriteJSON(w, http.StatusCreated, toOrderDTO(fresh))
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
		tif = *req.TimeInForce
	}
	if typ == "market" {
		tif = "IOC" // market orders are immediate-or-cancel by nature
	} else if !validTIF(tif) {
		return nil, nil, "VALIDATION_ERROR", "timeInForce 必须为 GTC/IOC/FOK/PostOnly"
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
		if req.QuoteOrderQty == nil {
			return nil, nil, "VALIDATION_ERROR", "市价买单需提供 quoteOrderQty"
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

	rep, err := s.engine.Submit(r.Context(), EngineCommand{Cmd: "cancel", Symbol: o.Symbol, OrderID: orderID})
	if err != nil {
		log.Printf("cancel: engine submit: %v", err)
		common.WriteError(w, http.StatusBadGateway, "ENGINE_UNAVAILABLE", "撮合引擎不可用")
		return
	}
	// Whether the engine still held the order or not, mark it canceled locally and
	// release the leftover frozen funds (exactly once).
	filled := o.FilledQuantity
	if f, err := decimal.NewFromString(rep.FilledQuantity); err == nil && f.GreaterThan(filled) {
		filled = f
	}
	if err := s.store.UpdateFill(r.Context(), orderID, filled, "canceled"); err != nil {
		log.Printf("cancel: update: %v", err)
		common.WriteError(w, http.StatusInternalServerError, "INTERNAL", "服务器内部错误")
		return
	}
	s.releaseLeftover(r.Context(), o)

	fresh, err := s.store.GetOrder(r.Context(), userID, orderID)
	if err != nil {
		log.Printf("cancel: reload: %v", err)
		common.WriteError(w, http.StatusInternalServerError, "INTERNAL", "服务器内部错误")
		return
	}
	common.WriteJSON(w, http.StatusOK, toOrderDTO(fresh))
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
	common.WriteJSON(w, http.StatusOK, paginated{Items: toOrderDTOs(orders), Page: page, PageSize: size, Total: total})
}

func (s *Server) handleHistory(w http.ResponseWriter, r *http.Request, userID string) {
	page, size := pageParams(r)
	orders, total, err := s.store.OrderHistory(r.Context(), userID, r.URL.Query().Get("symbol"), r.URL.Query().Get("status"), page, size)
	if err != nil {
		log.Printf("history: %v", err)
		common.WriteError(w, http.StatusInternalServerError, "INTERNAL", "服务器内部错误")
		return
	}
	common.WriteJSON(w, http.StatusOK, paginated{Items: toOrderDTOs(orders), Page: page, PageSize: size, Total: total})
}

func (s *Server) handleTrades(w http.ResponseWriter, r *http.Request, userID string) {
	page, size := pageParams(r)
	trades, total, err := s.store.MyTrades(r.Context(), userID, r.URL.Query().Get("symbol"), page, size)
	if err != nil {
		log.Printf("trades: %v", err)
		common.WriteError(w, http.StatusInternalServerError, "INTERNAL", "服务器内部错误")
		return
	}
	items := make([]tradeDTO, 0, len(trades))
	for _, t := range trades {
		items = append(items, toTradeDTO(t))
	}
	common.WriteJSON(w, http.StatusOK, paginated{Items: items, Page: page, PageSize: size, Total: total})
}

// ---- settlement ----

// applyReport settles every fill in the engine report and updates order state.
// Each trade is settled atomically by the asset service and recorded locally;
// both steps are idempotent on tradeId so re-delivery is harmless.
func (s *Server) applyReport(ctx context.Context, taker *Order, sym *SymbolConfig, rep *ExecReport) error {
	for _, t := range rep.Trades {
		price, err := decimal.NewFromString(t.Price)
		if err != nil {
			return err
		}
		qty, err := decimal.NewFromString(t.Quantity)
		if err != nil {
			return err
		}
		quoteAmount := price.Mul(qty)

		// Resolve buyer/seller and their maker/taker roles from the trade.
		buyerIsTaker := t.TakerSide == "buy"
		buyerUser, sellerUser := t.MakerUserID, t.TakerUserID
		buyerOrder, sellerOrder := t.MakerOrderID, t.TakerOrderID
		buyerRole, sellerRole := "maker", "taker"
		if buyerIsTaker {
			buyerUser, sellerUser = t.TakerUserID, t.MakerUserID
			buyerOrder, sellerOrder = t.TakerOrderID, t.MakerOrderID
			buyerRole, sellerRole = "taker", "maker"
		}
		buyerFeeBase := qty.Mul(s.feeRate(buyerRole))
		sellerFeeQuote := quoteAmount.Mul(s.feeRate(sellerRole))

		if err := s.asset.Settle(ctx, SettleParams{
			TradeID:        t.TradeID,
			Base:           sym.Base,
			Quote:          sym.Quote,
			BuyerUserID:    buyerUser,
			SellerUserID:   sellerUser,
			FeeAccount:     s.cfg.FeeAccount,
			QuoteAmount:    quoteAmount,
			BaseQty:        qty,
			BuyerFeeBase:   buyerFeeBase,
			SellerFeeQuote: sellerFeeQuote,
		}); err != nil {
			return err
		}

		// Record both sides of the trade (idempotent on (trade_id, order_id)).
		if err := s.store.InsertTrade(ctx, TradeRow{
			TradeID: t.TradeID, OrderID: buyerOrder, UserID: buyerUser, Symbol: sym.Symbol,
			Side: "buy", Price: price, Quantity: qty, Fee: buyerFeeBase, FeeAsset: sym.Base, Role: buyerRole,
		}); err != nil {
			return err
		}
		if err := s.store.InsertTrade(ctx, TradeRow{
			TradeID: t.TradeID, OrderID: sellerOrder, UserID: sellerUser, Symbol: sym.Symbol,
			Side: "sell", Price: price, Quantity: qty, Fee: sellerFeeQuote, FeeAsset: sym.Quote, Role: sellerRole,
		}); err != nil {
			return err
		}
	}

	// Update resting maker orders touched by this match.
	for _, u := range rep.MakerUpdates {
		filled, err := decimal.NewFromString(u.FilledQuantity)
		if err != nil {
			return err
		}
		if err := s.store.UpdateFill(ctx, u.OrderID, filled, u.Status); err != nil {
			return err
		}
	}

	// Update the taker order and release leftover frozen funds if it is terminal.
	takerFilled, err := decimal.NewFromString(rep.FilledQuantity)
	if err != nil {
		return err
	}
	if err := s.store.UpdateFill(ctx, taker.OrderID, takerFilled, rep.Status); err != nil {
		return err
	}
	if terminal(rep.Status) {
		s.releaseLeftover(ctx, taker)
	}
	return nil
}

// releaseLeftover unfreezes the unspent part of an order's frozen funds, exactly
// once. The leftover is the frozen amount minus what the order actually consumed
// across its fills (so a buy that filled at a better price releases the excess).
func (s *Server) releaseLeftover(ctx context.Context, o *Order) {
	won, err := s.store.ClaimUnfreeze(ctx, o.OrderID)
	if err != nil {
		log.Printf("releaseLeftover: claim: %v", err)
		return
	}
	if !won {
		return
	}
	consumed, err := s.store.Consumed(ctx, o.OrderID, o.Side)
	if err != nil {
		log.Printf("releaseLeftover: consumed: %v", err)
		return
	}
	leftover := o.FrozenAmount.Sub(consumed)
	if leftover.Sign() <= 0 {
		return
	}
	if err := s.asset.Unfreeze(ctx, o.UserID, o.FrozenAsset, leftover, "order:"+o.OrderID+":leftover"); err != nil {
		log.Printf("releaseLeftover: unfreeze: %v", err)
	}
}

// feeRate returns the configured spot fee rate for a maker/taker role.
func (s *Server) feeRate(role string) decimal.Decimal {
	if role == "taker" {
		return s.cfg.SpotTakerFeeRate
	}
	return s.cfg.SpotMakerFeeRate
}

// ---- helpers ----

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

func toOrderDTO(o *Order) orderDTO {
	d := orderDTO{
		OrderID:        o.OrderID,
		Symbol:         o.Symbol,
		Side:           o.Side,
		Type:           o.Type,
		Quantity:       o.Quantity.String(),
		FilledQuantity: o.FilledQuantity.String(),
		Status:         o.Status,
		TimeInForce:    o.TimeInForce,
		CreatedAt:      o.CreatedAt.UTC().Format(rfc3339),
	}
	if o.ClientOrderID != "" {
		c := o.ClientOrderID
		d.ClientOrderID = &c
	}
	if o.HasPrice {
		p := o.Price.String()
		d.Price = &p
	}
	if o.HasQuoteQty {
		q := o.QuoteOrderQty.String()
		d.QuoteOrderQty = &q
	}
	return d
}

func toOrderDTOs(orders []Order) []orderDTO {
	out := make([]orderDTO, 0, len(orders))
	for i := range orders {
		out = append(out, toOrderDTO(&orders[i]))
	}
	return out
}

func toTradeDTO(t TradeRow) tradeDTO {
	d := tradeDTO{
		TradeID:   t.TradeID,
		OrderID:   t.OrderID,
		Symbol:    t.Symbol,
		Side:      t.Side,
		Price:     t.Price.String(),
		Quantity:  t.Quantity.String(),
		Role:      t.Role,
		CreatedAt: t.CreatedAt.UTC().Format(rfc3339),
	}
	fee := t.Fee.String()
	d.Fee = &fee
	if t.FeeAsset != "" {
		fa := t.FeeAsset
		d.FeeAsset = &fa
	}
	return d
}
