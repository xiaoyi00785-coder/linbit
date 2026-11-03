package main

import (
	"errors"
	"log"
	"net/http"
	"strconv"
	"time"

	"github.com/linbit/platform/services/go/common"
	"github.com/shopspring/decimal"
)

// Internal read endpoints for service-to-service callers (strategy bots).
// Placing and cancelling go through the normal /spot/orders routes with the
// caller-supplied X-User-Id, exactly like the gateway does.

func (s *Server) internalRoutes(mux *http.ServeMux) {
	mux.HandleFunc("GET /internal/orders/{orderId}", s.internal(s.handleInternalOrder))
	mux.HandleFunc("GET /internal/symbols/{symbol}", s.internal(s.handleInternalSymbol))
	mux.HandleFunc("GET /internal/trades/feed", s.internal(s.handleTradeFeed))
}

// handleInternalOrder returns one order of ?userId= plus its fill totals.
func (s *Server) handleInternalOrder(w http.ResponseWriter, r *http.Request) {
	uid := r.URL.Query().Get("userId")
	o, err := s.store.GetOrder(r.Context(), uid, r.PathValue("orderId"))
	if errors.Is(err, ErrOrderNotFound) {
		common.WriteError(w, http.StatusNotFound, "ORDER_NOT_FOUND", "订单不存在")
		return
	}
	if err != nil {
		log.Printf("internal order: %v", err)
		common.WriteError(w, http.StatusInternalServerError, "INTERNAL", "服务器内部错误")
		return
	}
	sym, err := s.store.Symbol(r.Context(), o.Symbol)
	if err != nil {
		common.WriteError(w, http.StatusInternalServerError, "INTERNAL", "服务器内部错误")
		return
	}
	var quote, baseFee, quoteFee decimal.Decimal
	err = s.store.db.QueryRowContext(r.Context(), `SELECT COALESCE(sum(price*quantity),0),
		COALESCE(sum(CASE WHEN fee_asset=$3 THEN fee ELSE 0 END),0),
		COALESCE(sum(CASE WHEN fee_asset=$4 THEN fee ELSE 0 END),0)
		FROM trades WHERE order_id=$1 AND user_id=$2`, o.OrderID, uid, sym.Base, sym.Quote).Scan(&quote, &baseFee, &quoteFee)
	if err != nil {
		log.Printf("internal order fills: %v", err)
		common.WriteError(w, http.StatusInternalServerError, "INTERNAL", "服务器内部错误")
		return
	}
	common.WriteJSON(w, http.StatusOK, map[string]any{
		"order": toOrderDTO(o, sym), "filledQuantity": o.FilledQuantity.String(), "filledQuote": quote.String(),
		"baseFee": baseFee.String(), "quoteFee": quoteFee.String(),
	})
}

func (s *Server) handleInternalSymbol(w http.ResponseWriter, r *http.Request) {
	sym, err := s.store.Symbol(r.Context(), r.PathValue("symbol"))
	if errors.Is(err, ErrSymbolNotFound) {
		common.WriteError(w, http.StatusNotFound, "SYMBOL_NOT_FOUND", "交易对不存在")
		return
	}
	if err != nil {
		common.WriteError(w, http.StatusInternalServerError, "INTERNAL", "服务器内部错误")
		return
	}
	common.WriteJSON(w, http.StatusOK, map[string]any{
		"symbol": sym.Symbol, "base": sym.Base, "quote": sym.Quote, "pricePrecision": sym.PricePrecision,
		"quantityPrecision": sym.QuantityPrecision, "minQuantity": sym.MinQuantity.String(),
		"minNotional": sym.MinNotional.String(), "status": sym.Status,
	})
}

// handleTradeFeed streams spot fills in id order for the growth service
// (rebates, referral, points): GET /internal/trades/feed?after=<id>&limit=.
func (s *Server) handleTradeFeed(w http.ResponseWriter, r *http.Request) {
	after, _ := strconv.ParseInt(r.URL.Query().Get("after"), 10, 64)
	limit, _ := strconv.Atoi(r.URL.Query().Get("limit"))
	if limit <= 0 || limit > 1000 {
		limit = 500
	}
	rows, err := s.store.db.QueryContext(r.Context(), `SELECT t.id, t.user_id, t.symbol, s.base, s.quote, t.price::text, t.quantity::text,
		t.fee::text, COALESCE(t.fee_asset,''), t.role, t.created_at
		FROM trades t JOIN symbols s ON s.symbol = t.symbol WHERE t.id > $1 ORDER BY t.id LIMIT $2`, after, limit)
	if err != nil {
		log.Printf("trade feed: %v", err)
		common.WriteError(w, http.StatusInternalServerError, "INTERNAL", "服务器内部错误")
		return
	}
	defer rows.Close()
	items := []map[string]any{}
	for rows.Next() {
		var id int64
		var uid, sym, base, quote, px, qty, fee, feeAsset, role string
		var at time.Time
		if err := rows.Scan(&id, &uid, &sym, &base, &quote, &px, &qty, &fee, &feeAsset, &role, &at); err != nil {
			common.WriteError(w, http.StatusInternalServerError, "INTERNAL", "服务器内部错误")
			return
		}
		items = append(items, map[string]any{"id": id, "userId": uid, "symbol": sym, "base": base, "quote": quote,
			"price": px, "quantity": qty, "fee": fee, "feeAsset": feeAsset, "role": role, "time": at.UnixMilli()})
	}
	common.WriteJSON(w, http.StatusOK, map[string]any{"items": items})
}
