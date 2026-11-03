package main

import (
	"errors"
	"log"
	"net/http"

	"github.com/linbit/platform/services/go/common"
	"github.com/shopspring/decimal"
)

// Internal read endpoints for service-to-service callers (strategy bots).
// Placing and cancelling go through the normal /spot/orders routes with the
// caller-supplied X-User-Id, exactly like the gateway does.

func (s *Server) internalRoutes(mux *http.ServeMux) {
	mux.HandleFunc("GET /internal/orders/{orderId}", s.internal(s.handleInternalOrder))
	mux.HandleFunc("GET /internal/symbols/{symbol}", s.internal(s.handleInternalSymbol))
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
