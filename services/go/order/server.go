package main

// 现货 HTTP。/spot/* 对齐 docs/api/openapi.yaml，用户 id 来自网关注入的 X-User-Id。
// 价格和数量超出交易对精度直接拒绝，不截断。撮合回报按 tradeId 幂等结算。

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

// Server 把配置、存储、资产客户端和撮合接成 HTTP。fees 为 nil 时用环境变量费率。
type Server struct {
	cfg    Config
	store  *Store
	asset  *AssetClient
	engine Matching
	fees   *feeOverrides
	ufees  *userFeeCache
}

// NewServer 只组装依赖，不连库、不探活撮合。
func NewServer(cfg Config, store *Store, asset *AssetClient, engine Matching) *Server {
	return &Server{cfg: cfg, store: store, asset: asset, engine: engine, fees: &feeOverrides{}, ufees: &userFeeCache{m: map[string]userFee{}}}
}

// Routes 的 /spot/* 对齐 openapi，并且要求网关注入的 X-User-Id。没有这个头按未登录拒绝。
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

// withUser 只认 X-User-Id。空则 401。本服务不解析登录态。
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

// placeError 是下单失败，Code 原样返回，不把校验错误改成 500。
type placeError struct {
	status int
	code   string
	msg    string
}

func perrf(status int, code, msg string) *placeError { return &placeError{status, code, msg} }

// placeOrder 校验、冻结、落库、提交撮合、结算。POST /spot/orders 和条件单触发共用。冻结成功但撮合失败要解冻。
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

	// 先冻结再撮合，成交不能动用户没有的钱。可用不足直接拒单。
	if err := s.asset.Freeze(ctx, userID, o.FrozenAsset, o.FrozenAmount, "order:"+o.OrderID); err != nil {
		if errors.Is(err, ErrInsufficient) {
			return nil, perrf(http.StatusBadRequest, "INSUFFICIENT_BALANCE", "余额不足")
		}
		log.Printf("place: freeze: %v", err)
		return nil, perrf(http.StatusInternalServerError, "INTERNAL", "服务器内部错误")
	}

	if err := s.store.InsertOrder(ctx, o); err != nil {
		log.Printf("place: insert order: %v", err)
		// 落库失败则尽力解冻刚冻的资金，ref 与冻结相同。
		_ = s.asset.Unfreeze(ctx, userID, o.FrozenAsset, o.FrozenAmount, "order:"+o.OrderID)
		return nil, perrf(http.StatusInternalServerError, "INTERNAL", "服务器内部错误")
	}

	// 交给撮合。引擎没看到订单或我们没拿到回报时，把订单标 rejected 并释放冻结。
	rep, err := s.engine.Submit(ctx, s.placeCommand(o))
	if err != nil {
		log.Printf("place: engine submit: %v", err)
		// 引擎没接到，或应答丢失：撤掉本地订单并释放冻结。不应答当成已成交。
		_ = s.store.UpdateFill(ctx, o.OrderID, decimal.Zero, "rejected")
		s.releaseLeftover(ctx, o)
		return nil, perrf(http.StatusBadGateway, "ENGINE_UNAVAILABLE", "撮合引擎不可用")
	}

	if rep.Status == "rejected" {
		reason := "订单被拒绝"
		if rep.RejectReason != nil {
			reason = *rep.RejectReason
		}
		_ = s.store.UpdateFill(ctx, o.OrderID, decimal.Zero, "rejected")
		s.releaseLeftover(ctx, o)
		if strings.Contains(reason, "post_only") {
			return nil, perrf(http.StatusBadRequest, "POST_ONLY_REJECTED", "只做 Maker 订单会立即成交，已被拒绝")
		}
		return nil, perrf(http.StatusBadRequest, "ORDER_REJECTED", reason)
	}

	if err := s.applyReport(ctx, o, sym, rep); err != nil {
		log.Printf("place: apply report: %v", err)
		return nil, perrf(http.StatusInternalServerError, "INTERNAL", "成交结算失败")
	}

	fresh, err := s.store.GetOrder(ctx, userID, o.OrderID)
	if err != nil {
		log.Printf("place: reload order: %v", err)
		return nil, perrf(http.StatusInternalServerError, "INTERNAL", "服务器内部错误")
	}
	dto := toOrderDTO(fresh, sym)
	return &dto, nil
}

// buildOrder 按交易对规则校验并算出冻结资产和金额。价格、数量多出来的小数返回 VALIDATION_ERROR，不截断。
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
		// 按计价币预算的市价买：整笔 quoteOrderQty 冻结。这里不按 price_precision 截断，也不拒绝多余小数，只要求大于 0 且不低于最小名义额。
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
		// 其余订单按基础币数量。限价买冻结 price×qty，卖单冻结数量本身。
		if req.Quantity == nil {
			return nil, nil, "VALIDATION_ERROR", "quantity 为必填"
		}
		qty, err := decimal.NewFromString(*req.Quantity)
		if err != nil || qty.Sign() <= 0 {
			return nil, nil, "VALIDATION_ERROR", "quantity 不合法"
		}
		// 数量小数位超过 quantity_precision 则拒绝，不截断。
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
			// 价格小数位超过 price_precision 则拒绝，不截断。
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
	if o.Type == "market" && o.Side == "buy" && !o.HasQuoteQty && o.HasPrice {
		// 按基础币数量的市价买：引擎只按计价币预算定价，所以改成 IOC 限价，价格上限是最优卖价加滑点。未成交部分释放。
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

// symbolMap 按交易对取出精度，列表里的价格和数量按各自的 price_precision / quantity_precision 格式化。
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

// ---- settlement ----

// applyReport 结算回报里的每笔成交并改订单状态。
// 资产服务按 tradeId 原子结算；本地 trades 按 (trade_id, order_id) 冲突则跳过。同一笔回报再投递不会划第二次。
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
		buyerFeeBase := qty.Mul(s.userFeeRate(buyerUser, buyerRole))
		sellerFeeQuote := quoteAmount.Mul(s.userFeeRate(sellerUser, sellerRole))

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

		// 买卖双方各记一行，(trade_id, order_id) 冲突则跳过。
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

	// 这次成交碰到的 maker 挂单，按引擎给的已成交量和状态回写。
	for _, u := range rep.MakerUpdates {
		filled, err := decimal.NewFromString(u.FilledQuantity)
		if err != nil {
			return err
		}
		if err := s.store.UpdateFill(ctx, u.OrderID, filled, u.Status); err != nil {
			return err
		}
	}

	// taker 终态（filled/canceled/rejected）时释放未用完的冻结。部分成交的买单若价格更好，差额在这里退。
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

// releaseLeftover 只解冻一次未花掉的冻结。余额 = 冻结额 - 成交实际消耗（买单是 Σ价格×数量，卖单是 Σ数量）。
// 先 ClaimUnfreeze，没抢到就返回，避免重复解冻。
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

// feeRate 返回当前现货费率。taker 用 taker，其余用 maker。用户级覆盖不在这里。
func (s *Server) feeRate(role string) decimal.Decimal {
	maker, taker := s.currentFees()
	if role == "taker" {
		return taker
	}
	return maker
}

// ---- helpers ----

// normalizeTIF 把别名收成引擎的 TIF。GTX、POSTONLY、POST_ONLY 都是 PostOnly。
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

// precisionOK：小数位不超过 places 才为真。Truncate 后不相等说明有多余小数，调用方必须拒绝，不能改成截断后的值。
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

// 价格和数量渲染成定长小数字符串，不用浮点。价格和计价币金额用 price_precision，基础币数量用 quantity_precision。
// 交易对配置缺失时退回原始十进制字符串，避免把精度弄丢。展示用 StringFixed，和校验用的拒绝多余小数不是一回事。

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

// fmtFee 按收费资产的精度格式化：基础币用 quantity_precision，计价币用 price_precision。
// 非零手续费如果比该精度更长（例如极小成交的 0.000000065 BTC），返回原值，不四舍五入成 0。这不是固定 8 位。
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

// marketBuySlippage 是按基础币数量市价买相对最优卖价的上限，默认 0.05，可用 MARKET_BUY_SLIPPAGE 覆盖。
var marketBuySlippage = decimal.RequireFromString(common.Env("MARKET_BUY_SLIPPAGE", "0.05"))

// marketBuyByQty 按基础币数量市价买：冻结 qty×保护价 的计价币。保护价 = 最优卖价×(1+滑点)，向价格精度进位。
// 订单类型仍记 market，提交给引擎时改成 IOC 限价。数量精度超出则拒绝，不截断。卖盘为空返回 NO_LIQUIDITY。
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
