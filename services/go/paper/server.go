package main

// 模拟盘 HTTP。身份用 Bearer 访问令牌再验一次，不信任调用方自带的 X-User-Id。
// 现货和合约模拟都不写生产余额。

import (
	"encoding/json"
	"errors"
	"io"
	"log"
	"net/http"
	"strconv"
	"strings"
	"time"

	"github.com/linbit/platform/services/go/common"
	"github.com/shopspring/decimal"
)

// Server 的 futures 为空时，合约行情回落到 NewLiveFutures(FUTURES_URL)，仍然只读。
type Server struct {
	cfg      Config
	store    *Store
	features *FeatureGate
	market   Market
	notifier NotificationSender
	futures  FutSource
}

// NewServer 不连行情、不建模拟账户。futures 留空；请求时若仍为空，按 FUTURES_URL 只读真实行情。
func NewServer(cfg Config, store *Store, features *FeatureGate, market Market, notifier NotificationSender) *Server {
	return &Server{cfg: cfg, store: store, features: features, market: market, notifier: notifier}
}

// authenticate 再验一次 Bearer 访问令牌，不信任 X-User-Id。子账户会话（Acct 非空）拒绝。网关是正常入口。
func (s *Server) authenticate(r *http.Request) (string, error) {
	parts := strings.SplitN(r.Header.Get("Authorization"), " ", 2)
	if len(parts) != 2 || !strings.EqualFold(parts[0], "Bearer") {
		return "", apiErr(http.StatusUnauthorized, "UNAUTHORIZED", "请先登录")
	}
	claims, err := common.ParseAccessToken(s.cfg.JWTSecret, parts[1])
	if err != nil || claims.Acct != "" {
		return "", apiErr(http.StatusUnauthorized, "UNAUTHORIZED", "令牌无效、已过期或不是主账户会话")
	}
	if claims.Subject == "" {
		return "", apiErr(http.StatusUnauthorized, "UNAUTHORIZED", "令牌缺少用户身份")
	}
	return claims.Subject, nil
}
func (s *Server) feature(w http.ResponseWriter, r *http.Request, key string) (string, bool) {
	uid, err := s.authenticate(r)
	if err != nil {
		writeErr(w, err)
		return "", false
	}
	enabled, err := s.features.Enabled(r.Context(), key)
	if err != nil {
		log.Printf("feature %s: %v", key, err)
		writeErr(w, apiErr(http.StatusForbidden, "FEATURE_DISABLED", "功能未开放"))
		return "", false
	}
	if !enabled {
		writeErr(w, apiErr(http.StatusForbidden, "FEATURE_DISABLED", "功能未开放"))
		return "", false
	}
	return uid, true
}

// Routes 挂模拟现货、价格预警和模拟合约。这些路径都不写生产余额。
func (s *Server) Routes() http.Handler {
	mux := http.NewServeMux()
	mux.HandleFunc("GET /healthz", func(w http.ResponseWriter, r *http.Request) { w.Write([]byte("ok")) })
	mux.HandleFunc("GET /paper/account", s.handleAccount)
	mux.HandleFunc("POST /paper/account/reset", s.handleReset)
	mux.HandleFunc("GET /paper/balances", s.handleBalances)
	mux.HandleFunc("POST /paper/orders", s.handlePlaceOrder)
	mux.HandleFunc("GET /paper/orders", s.handleListOrders)
	mux.HandleFunc("GET /paper/orders/{orderId}", s.handleGetOrder)
	mux.HandleFunc("GET /paper/trades", s.handleListTrades)
	mux.HandleFunc("GET /paper/futures/account", s.handleFutAccount)
	mux.HandleFunc("POST /paper/futures/account/claim", s.handleFutClaim)
	mux.HandleFunc("POST /paper/futures/account/reset", s.handleFutReset)
	mux.HandleFunc("GET /paper/futures/contracts", s.handleFutContracts)
	mux.HandleFunc("POST /paper/futures/orders", s.handleFutPlace)
	mux.HandleFunc("GET /paper/futures/orders", s.handleFutOrders)
	mux.HandleFunc("DELETE /paper/futures/orders/{orderId}", s.handleFutCancel)
	mux.HandleFunc("POST /paper/futures/orders/cancel-all", s.handleFutCancelAll)
	mux.HandleFunc("GET /paper/futures/positions", s.handleFutPositions)
	mux.HandleFunc("POST /paper/futures/positions/close-all", s.handleFutCloseAll)
	mux.HandleFunc("POST /paper/futures/positions/{symbol}/close", s.handleFutClose)
	mux.HandleFunc("POST /paper/futures/positions/{symbol}/tpsl", s.handleFutTPSL)
	mux.HandleFunc("POST /paper/futures/positions/{symbol}/leverage", s.handleFutLeverage)
	mux.HandleFunc("POST /paper/futures/positions/{symbol}/margin-mode", s.handleFutMarginMode)
	mux.HandleFunc("POST /paper/futures/position-mode", s.handleFutPositionMode)
	mux.HandleFunc("GET /paper/futures/trades", s.handleFutTrades)
	mux.HandleFunc("GET /paper/futures/ledger", s.handleFutLedger)
	mux.HandleFunc("GET /internal/paper/futures/snapshot", s.handleFutSnapshot)
	mux.HandleFunc("GET /price-alerts", s.handleListAlerts)
	mux.HandleFunc("POST /price-alerts", s.handleCreateAlert)
	mux.HandleFunc("PATCH /price-alerts/{alertId}", s.handleUpdateAlert)
	mux.HandleFunc("DELETE /price-alerts/{alertId}", s.handleDeleteAlert)
	if s.cfg.DevMode {
		mux.HandleFunc("POST /internal/dev/alerts/evaluate", s.internal(s.handleEvaluateAlerts))
		mux.HandleFunc("POST /internal/dev/alerts/deliver", s.internal(s.handleDeliverAlerts))
	}
	return mux
}
func (s *Server) internal(h http.HandlerFunc) http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		if !strings.EqualFold(r.Header.Get("X-Internal-Token"), s.cfg.InternalToken) || s.cfg.InternalToken == "" {
			common.WriteError(w, http.StatusUnauthorized, "UNAUTHORIZED", "内部调用未授权")
			return
		}
		h(w, r)
	}
}
func decode(w http.ResponseWriter, r *http.Request, out any) bool {
	if err := common.DecodeJSON(r, out); err != nil {
		writeErr(w, apiErr(http.StatusBadRequest, "VALIDATION_ERROR", "JSON 参数无效"))
		return false
	}
	var extra any
	if err := json.NewDecoder(r.Body).Decode(&extra); !errors.Is(err, io.EOF) {
		writeErr(w, apiErr(http.StatusBadRequest, "VALIDATION_ERROR", "仅允许一个 JSON 对象"))
		return false
	}
	return true
}
func (s *Server) handleAccount(w http.ResponseWriter, r *http.Request) {
	uid, ok := s.feature(w, r, "paperTrading")
	if !ok {
		return
	}
	a, err := s.store.Account(r.Context(), uid)
	if err != nil {
		writeErr(w, err)
		return
	}
	common.WriteJSON(w, http.StatusOK, a)
}
func (s *Server) handleReset(w http.ResponseWriter, r *http.Request) {
	uid, ok := s.feature(w, r, "paperTrading")
	if !ok {
		return
	}
	var in struct {
		Confirmation string `json:"confirmation"`
	}
	if !decode(w, r, &in) {
		return
	}
	out, err := s.store.Reset(r.Context(), uid, r.Header.Get("Idempotency-Key"), in.Confirmation)
	if err != nil {
		writeErr(w, err)
		return
	}
	common.WriteJSON(w, http.StatusOK, out)
}
func (s *Server) handleBalances(w http.ResponseWriter, r *http.Request) {
	uid, ok := s.feature(w, r, "paperTrading")
	if !ok {
		return
	}
	page, size, err := parsePage(r.URL.Query().Get("page"), r.URL.Query().Get("pageSize"))
	if err != nil {
		writeErr(w, err)
		return
	}
	if _, err := s.store.Account(r.Context(), uid); err != nil {
		writeErr(w, err)
		return
	}
	out, err := s.store.Balances(r.Context(), uid, page, size)
	if err != nil {
		writeErr(w, err)
		return
	}
	if items, ok := out["items"].([]PaperBalance); ok {
		for i := range items {
			if items[i].Asset == "USDT" {
				continue
			}
			// 非 USDT 的美元估值只用于展示。行情缺失不编价格，也不改 paper_balances。
			quote, err := s.market.Index(r.Context(), items[i].Asset+"USDT")
			if err == nil {
				items[i].PriceUSD = dptr(quote.Price)
				items[i].ValuationUSD = dptr(decimalRequire(items[i].Free).Mul(quote.Price).Round(2))
			}
		}
		out["items"] = items
	}
	common.WriteJSON(w, http.StatusOK, out)
}
func (s *Server) handlePlaceOrder(w http.ResponseWriter, r *http.Request) {
	uid, ok := s.feature(w, r, "paperTrading")
	if !ok {
		return
	}
	var in PlacePaperOrder
	if !decode(w, r, &in) {
		return
	}
	out, err := s.store.PlaceOrder(r.Context(), uid, in, s.market)
	if err != nil {
		writeErr(w, err)
		return
	}
	common.WriteJSON(w, http.StatusCreated, out)
}
func (s *Server) handleListOrders(w http.ResponseWriter, r *http.Request) {
	uid, ok := s.feature(w, r, "paperTrading")
	if !ok {
		return
	}
	q := r.URL.Query()
	page, size, err := parsePage(q.Get("page"), q.Get("pageSize"))
	if err != nil {
		writeErr(w, err)
		return
	}
	generation, err := optionalGeneration(q.Get("generation"))
	if err != nil {
		writeErr(w, err)
		return
	}
	gen := int64(0)
	if generation != nil {
		gen = *generation
	}
	if gen == 0 {
		a, err := s.store.Account(r.Context(), uid)
		if err != nil {
			writeErr(w, err)
			return
		}
		gen = a.Generation
	}
	out, err := s.store.ListOrders(r.Context(), uid, q.Get("symbol"), q.Get("status"), gen, page, size)
	if err != nil {
		writeErr(w, err)
		return
	}
	common.WriteJSON(w, http.StatusOK, out)
}
func (s *Server) handleGetOrder(w http.ResponseWriter, r *http.Request) {
	uid, ok := s.feature(w, r, "paperTrading")
	if !ok {
		return
	}
	o, err := s.store.Order(r.Context(), uid, r.PathValue("orderId"))
	if err != nil {
		writeErr(w, err)
		return
	}
	common.WriteJSON(w, http.StatusOK, o)
}
func (s *Server) handleListTrades(w http.ResponseWriter, r *http.Request) {
	uid, ok := s.feature(w, r, "paperTrading")
	if !ok {
		return
	}
	q := r.URL.Query()
	page, size, err := parsePage(q.Get("page"), q.Get("pageSize"))
	if err != nil {
		writeErr(w, err)
		return
	}
	gen, err := optionalGeneration(q.Get("generation"))
	if err != nil {
		writeErr(w, err)
		return
	}
	generation := int64(0)
	if gen != nil {
		generation = *gen
	}
	if generation == 0 {
		a, err := s.store.Account(r.Context(), uid)
		if err != nil {
			writeErr(w, err)
			return
		}
		generation = a.Generation
	}
	out, err := s.store.ListTrades(r.Context(), tradeFilter{userID: uid, symbol: q.Get("symbol"), generation: generation, page: page, pageSize: size})
	if err != nil {
		writeErr(w, err)
		return
	}
	common.WriteJSON(w, http.StatusOK, out)
}
func (s *Server) handleCreateAlert(w http.ResponseWriter, r *http.Request) {
	uid, ok := s.feature(w, r, "priceAlerts")
	if !ok {
		return
	}
	if !s.admitAlertWrite(w, r, uid) {
		return
	}
	var in CreatePriceAlert
	if !decode(w, r, &in) {
		return
	}
	a, err := s.store.CreateAlert(r.Context(), uid, in, s.market, time.Now())
	if err != nil {
		writeErr(w, err)
		return
	}
	common.WriteJSON(w, http.StatusCreated, a)
}
func (s *Server) admitAlertWrite(w http.ResponseWriter, r *http.Request, uid string) bool {
	if err := s.store.AdmitAlertWrite(r.Context(), uid, time.Now(), s.cfg.AlertWriteLimit, s.cfg.AlertWriteWindow); err != nil {
		writeErr(w, err)
		return false
	}
	return true
}
func (s *Server) handleListAlerts(w http.ResponseWriter, r *http.Request) {
	uid, ok := s.feature(w, r, "priceAlerts")
	if !ok {
		return
	}
	q := r.URL.Query()
	page, size, err := parsePage(q.Get("page"), q.Get("pageSize"))
	if err != nil {
		writeErr(w, err)
		return
	}
	out, err := s.store.ListAlerts(r.Context(), uid, q.Get("symbol"), q.Get("status"), page, size)
	if err != nil {
		writeErr(w, err)
		return
	}
	common.WriteJSON(w, http.StatusOK, out)
}
func (s *Server) handleUpdateAlert(w http.ResponseWriter, r *http.Request) {
	uid, ok := s.feature(w, r, "priceAlerts")
	if !ok {
		return
	}
	if !s.admitAlertWrite(w, r, uid) {
		return
	}
	var in UpdatePriceAlert
	if !decode(w, r, &in) {
		return
	}
	a, err := s.store.UpdateAlert(r.Context(), uid, r.PathValue("alertId"), in, s.market, time.Now())
	if err != nil {
		writeErr(w, err)
		return
	}
	common.WriteJSON(w, http.StatusOK, a)
}
func (s *Server) handleDeleteAlert(w http.ResponseWriter, r *http.Request) {
	uid, ok := s.feature(w, r, "priceAlerts")
	if !ok {
		return
	}
	if !s.admitAlertWrite(w, r, uid) {
		return
	}
	var revision *int64
	if v := r.URL.Query().Get("revision"); v != "" {
		n, err := strconv.ParseInt(v, 10, 64)
		if err != nil || n < 1 {
			writeErr(w, apiErr(http.StatusBadRequest, "VALIDATION_ERROR", "revision 无效"))
			return
		}
		revision = &n
	}
	if err := s.store.DeleteAlert(r.Context(), uid, r.PathValue("alertId"), revision, time.Now()); err != nil {
		writeErr(w, err)
		return
	}
	w.WriteHeader(http.StatusNoContent)
}
func (s *Server) handleEvaluateAlerts(w http.ResponseWriter, r *http.Request) {
	var in struct {
		Symbol string `json:"symbol"`
	}
	if !decode(w, r, &in) || in.Symbol == "" {
		if in.Symbol == "" {
			writeErr(w, apiErr(http.StatusBadRequest, "VALIDATION_ERROR", "symbol 必填"))
		}
		return
	}
	if err := s.evaluateSymbol(r.Context(), strings.ToUpper(in.Symbol)); err != nil {
		writeErr(w, err)
		return
	}
	common.WriteJSON(w, http.StatusOK, map[string]bool{"evaluated": true})
}
func (s *Server) handleDeliverAlerts(w http.ResponseWriter, r *http.Request) {
	if err := s.deliverAlerts(r.Context()); err != nil {
		writeErr(w, err)
		return
	}
	common.WriteJSON(w, http.StatusOK, map[string]bool{"delivered": true})
}
func decimalRequire(v string) decimal.Decimal { return decimal.RequireFromString(v) }
