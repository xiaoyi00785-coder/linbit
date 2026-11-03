package main

import (
	"github.com/shopspring/decimal"
	"log"
	"net/http"
	"sort"
	"strconv"
	"strings"

	"github.com/linbit/platform/services/go/common"
)

// Server wires the aggregator, store, WebSocket hub and event ingest into the
// market-data HTTP surface.
type Server struct {
	cfg    Config
	store  *Store
	agg    *Aggregator
	hub    *Hub
	ingest *HTTPEventSource
	feed   *IndexFeed
	chat   *Chat
	tradfi *TradFi
}

// NewServer constructs a Server.
func NewServer(cfg Config, store *Store, agg *Aggregator, hub *Hub, ingest *HTTPEventSource) *Server {
	return &Server{cfg: cfg, store: store, agg: agg, hub: hub, ingest: ingest}
}

// Routes returns the market-data HTTP handler. The /market/* paths mirror
// docs/api/openapi.yaml and are public (the gateway forwards them without auth);
// /ws is the WebSocket endpoint from docs/api/websocket.md; /internal/events is
// the engine event ingest and is never exposed through the gateway.
func (s *Server) Routes() http.Handler {
	mux := http.NewServeMux()
	mux.HandleFunc("GET /healthz", func(w http.ResponseWriter, r *http.Request) { w.Write([]byte("ok")) })

	mux.HandleFunc("GET /market/symbols", s.handleSymbols)
	mux.HandleFunc("GET /market/categories", s.handleCategories)
	mux.HandleFunc("GET /market/ticker", s.handleTicker)
	mux.HandleFunc("GET /market/depth", s.handleDepth)
	mux.HandleFunc("GET /market/klines", s.handleKlines)
	mux.HandleFunc("GET /market/trades", s.handleTrades)
	mux.HandleFunc("GET /market/index", s.handleIndex)
	mux.HandleFunc("GET /market/proof-of-reserves", s.handleProofOfReserves)

	s.homeInternalRoutes(mux)
	s.chatRoutes(mux)
	s.porRoutes(mux)
	s.tradfiRoutes(mux)

	mux.HandleFunc("GET /ws", s.hub.ServeHTTP)
	mux.HandleFunc("POST /internal/ws/push", s.internal(s.hWSPush))
	mux.HandleFunc("POST /internal/events", s.ingest.Handler())
	if s.cfg.DevMode {
		mux.HandleFunc("POST /internal/dev/index", s.handleDevIndex)
	}
	return mux
}

// ---- REST handlers ----

func (s *Server) handleSymbols(w http.ResponseWriter, r *http.Request) {
	infos, err := s.store.Symbols(r.Context())
	if err != nil {
		log.Printf("symbols: %v", err)
		common.WriteError(w, http.StatusInternalServerError, "INTERNAL", "服务器内部错误")
		return
	}
	tags := map[string][]string{}
	if cats, err := s.store.Categories(r.Context()); err == nil {
		for _, c := range cats {
			for _, sym := range c.Symbols {
				tags[sym] = append(tags[sym], c.ID)
			}
		}
	} else {
		log.Printf("symbols: categories: %v", err)
	}
	out := make([]symbolDTO, 0, len(infos))
	for _, si := range infos {
		d := symbolDTO{
			Symbol:            si.Symbol,
			Base:              si.Base,
			Quote:             si.Quote,
			PricePrecision:    si.PricePrecision,
			QuantityPrecision: si.QuantityPrecision,
			Status:            si.Status,
			Tags:              tags[si.Symbol],
		}
		if d.Tags == nil {
			d.Tags = []string{}
		}
		mq := si.MinQuantity.String()
		d.MinQuantity = &mq
		out = append(out, d)
	}
	common.WriteJSON(w, http.StatusOK, out)
}

// handleCategories: GET /market/categories -> [{id,name,symbols}].
// ?market=spot (default) limits symbols to listed spot pairs; ?market=futures
// limits them to trading perpetual contracts (fut_contracts), so the futures
// page's tabs show exactly what can be traded there. ?nonEmpty=true drops
// categories with no symbols for that market; otherwise empty ones keep [].
func (s *Server) handleCategories(w http.ResponseWriter, r *http.Request) {
	cats, err := s.store.Categories(r.Context())
	if err != nil {
		log.Printf("categories: %v", err)
		common.WriteError(w, http.StatusInternalServerError, "INTERNAL", "服务器内部错误")
		return
	}
	listed := map[string]bool{}
	switch m := strings.ToLower(r.URL.Query().Get("market")); m {
	case "", "spot":
		if infos, err := s.store.Symbols(r.Context()); err == nil {
			for _, si := range infos {
				listed[si.Symbol] = true
			}
		}
	case "futures":
		fs, err := s.store.FuturesSymbols(r.Context())
		if err != nil {
			log.Printf("categories: futures symbols: %v", err)
		}
		for _, sym := range fs {
			listed[sym] = true
		}
	default:
		common.WriteError(w, http.StatusBadRequest, "VALIDATION_ERROR", "market 仅支持 spot/futures")
		return
	}
	out := filterCategories(cats, listed)
	if r.URL.Query().Get("nonEmpty") == "true" {
		out = dropEmptyCategories(out)
	}
	common.WriteJSON(w, http.StatusOK, out)
}

func dropEmptyCategories(cats []Category) []Category {
	out := make([]Category, 0, len(cats))
	for _, c := range cats {
		if len(c.Symbols) > 0 {
			out = append(out, c)
		}
	}
	return out
}

// filterCategories drops symbols that are not listed (pure; unit tested).
func filterCategories(cats []Category, listed map[string]bool) []Category {
	out := make([]Category, 0, len(cats))
	for _, c := range cats {
		keep := []string{}
		for _, sym := range c.Symbols {
			if listed[sym] {
				keep = append(keep, sym)
			}
		}
		out = append(out, Category{ID: c.ID, Name: c.Name, Symbols: keep})
	}
	return out
}

func (s *Server) handleTicker(w http.ResponseWriter, r *http.Request) {
	symbol := r.URL.Query().Get("symbol")
	if symbol != "" {
		// With a symbol the response is a single Ticker object (openapi oneOf).
		t, ok := s.agg.TickerFrame(symbol)
		if !ok {
			common.WriteError(w, http.StatusNotFound, "NOT_FOUND", "交易对不存在")
			return
		}
		common.WriteJSON(w, http.StatusOK, t)
		return
	}
	// Without a symbol the response is an array of all tracked symbols' tickers,
	// optionally filtered by ?category=<market_categories id> (or a comma list
	// via ?symbols=), sorted server-side by ?sort=gainers|losers|volume and
	// truncated by ?limit= (rankings).
	q := r.URL.Query()
	syms := s.agg.TrackedSymbols()
	if v := strings.TrimSpace(q.Get("symbols")); v != "" {
		syms = splitUpper(v)
	} else if cat := strings.TrimSpace(q.Get("category")); cat != "" {
		cats, err := s.store.Categories(r.Context())
		if err != nil {
			log.Printf("ticker: categories: %v", err)
			common.WriteError(w, http.StatusInternalServerError, "INTERNAL", "服务器内部错误")
			return
		}
		syms = nil
		for _, c := range cats {
			if c.ID == cat {
				syms = c.Symbols
			}
		}
	}
	out := make([]tickerDTO, 0, len(syms))
	for _, sym := range syms {
		if t, ok := s.agg.TickerFrame(sym); ok {
			out = append(out, t)
		}
	}
	sortBy := strings.ToLower(strings.TrimSpace(q.Get("sort")))
	switch sortBy {
	case "", "symbol", "gainers", "losers", "volume":
	default:
		common.WriteError(w, http.StatusBadRequest, "VALIDATION_ERROR", "sort 仅支持 gainers/losers/volume/symbol")
		return
	}
	sortTickers(out, sortBy)
	if lim := atoiDefault(q.Get("limit"), 0, 1, 1000); lim > 0 && len(out) > lim {
		out = out[:lim]
	}
	common.WriteJSON(w, http.StatusOK, out)
}

// sortTickers orders tickers for rankings (pure; unit tested). gainers: change
// desc; losers: change asc; volume: quoteVolume desc. Ties (and "symbol" / "")
// fall back to symbol asc so the order is deterministic. Symbols without a
// price (priceSource none) always sink to the end.
func sortTickers(ts []tickerDTO, by string) {
	num := func(v string) decimal.Decimal {
		d, err := decimal.NewFromString(v)
		if err != nil {
			return decimal.Zero
		}
		return d
	}
	sort.SliceStable(ts, func(i, j int) bool {
		a, b := ts[i], ts[j]
		if (a.PriceSource == "none") != (b.PriceSource == "none") {
			return b.PriceSource == "none"
		}
		var c int
		switch by {
		case "gainers":
			c = num(b.PriceChangePercent).Cmp(num(a.PriceChangePercent))
		case "losers":
			c = num(a.PriceChangePercent).Cmp(num(b.PriceChangePercent))
		case "volume":
			c = num(b.QuoteVolume).Cmp(num(a.QuoteVolume))
		}
		if c != 0 {
			return c < 0
		}
		return a.Symbol < b.Symbol
	})
}

func splitUpper(v string) []string {
	var out []string
	for _, x := range strings.Split(v, ",") {
		if x = strings.ToUpper(strings.TrimSpace(x)); x != "" {
			out = append(out, x)
		}
	}
	return out
}

func (s *Server) handleDepth(w http.ResponseWriter, r *http.Request) {
	symbol := r.URL.Query().Get("symbol")
	if symbol == "" {
		common.WriteError(w, http.StatusBadRequest, "VALIDATION_ERROR", "symbol 为必填")
		return
	}
	limit := atoiDefault(r.URL.Query().Get("limit"), 20, 1, 200)
	d, ok := s.agg.DepthFrame(symbol, limit)
	if !ok {
		common.WriteError(w, http.StatusNotFound, "NOT_FOUND", "交易对不存在")
		return
	}
	common.WriteJSON(w, http.StatusOK, d)
}

func (s *Server) handleKlines(w http.ResponseWriter, r *http.Request) {
	q := r.URL.Query()
	symbol := q.Get("symbol")
	interval := q.Get("interval")
	if symbol == "" || interval == "" {
		common.WriteError(w, http.StatusBadRequest, "VALIDATION_ERROR", "symbol 与 interval 为必填")
		return
	}
	interval = normInterval(interval)
	if !s.agg.ValidInterval(interval) {
		common.WriteError(w, http.StatusBadRequest, "VALIDATION_ERROR", "不支持的 interval")
		return
	}
	limit := atoiDefault(q.Get("limit"), 160, 1, 1000)
	candles, ok := s.agg.Klines(symbol, interval, limit)
	if !ok {
		common.WriteError(w, http.StatusNotFound, "NOT_FOUND", "交易对不存在")
		return
	}
	if candles == nil {
		candles = []restKline{}
	}
	common.WriteJSON(w, http.StatusOK, candles)
}

func (s *Server) handleTrades(w http.ResponseWriter, r *http.Request) {
	symbol := r.URL.Query().Get("symbol")
	if symbol == "" {
		common.WriteError(w, http.StatusBadRequest, "VALIDATION_ERROR", "symbol 为必填")
		return
	}
	limit := atoiDefault(r.URL.Query().Get("limit"), 50, 1, 1000)
	trades, ok := s.agg.RecentTrades(symbol, limit)
	if !ok {
		common.WriteError(w, http.StatusNotFound, "NOT_FOUND", "交易对不存在")
		return
	}
	if trades == nil {
		trades = []tradeDTO{}
	}
	common.WriteJSON(w, http.StatusOK, trades)
}

// atoiDefault parses s as an int, clamping to [min,max] and using def when empty
// or malformed.
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
