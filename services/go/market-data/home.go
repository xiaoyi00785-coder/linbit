package main

import (
	"context"
	"encoding/json"
	"log"
	"net/http"
	"sort"
	"strconv"
	"strings"
	"time"

	"github.com/linbit/platform/services/go/common"
	"github.com/shopspring/decimal"
)

// home.go: home-page operations slots.
//
//	public   GET /home/placements?slots=&platform=&locale=&appVersion=&region=&audience=
//	public   GET /home/banners?platform=&locale=   (alias: active slot=carousel)
//	public   GET /home/hot?type=hot|gainers|losers|new|volume&category=&limit=
//	internal /internal/home/placements[/{id}], /internal/home/hot-pins
//	         (X-Internal-Token; called by admin, which enforces 2FA+reason+audit)

var hotTypes = map[string]bool{"hot": true, "gainers": true, "losers": true, "new": true, "volume": true}

// HotPin is an admin-pinned symbol in a hot-list tab.
type HotPin struct {
	Type   string `json:"type"`
	Symbol string `json:"symbol"`
	Sort   int    `json:"sort"`
}

func (s *Store) HotPins(ctx context.Context) ([]HotPin, error) {
	rows, err := s.db.QueryContext(ctx, `SELECT list_type, symbol, sort FROM home_hot_pins ORDER BY list_type, sort, symbol`)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	out := []HotPin{}
	for rows.Next() {
		var p HotPin
		if err := rows.Scan(&p.Type, &p.Symbol, &p.Sort); err != nil {
			return nil, err
		}
		out = append(out, p)
	}
	return out, rows.Err()
}

// ReplaceHotPins atomically replaces all pins of one list type.
func (s *Store) ReplaceHotPins(ctx context.Context, typ string, symbols []string, by string) error {
	tx, err := s.db.BeginTx(ctx, nil)
	if err != nil {
		return err
	}
	defer tx.Rollback() //nolint:errcheck
	if _, err := tx.ExecContext(ctx, `DELETE FROM home_hot_pins WHERE list_type=$1`, typ); err != nil {
		return err
	}
	for i, sym := range symbols {
		if _, err := tx.ExecContext(ctx, `INSERT INTO home_hot_pins (list_type, symbol, sort, updated_by) VALUES ($1,$2,$3,$4)`, typ, sym, i, by); err != nil {
			return err
		}
	}
	return tx.Commit()
}

// FirstSeen returns the earliest daily candle per symbol, used to rank "new".
func (s *Store) FirstSeen(ctx context.Context) (map[string]time.Time, error) {
	rows, err := s.db.QueryContext(ctx, `SELECT symbol, min(open_time) FROM klines WHERE interval='1d' GROUP BY symbol`)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	out := map[string]time.Time{}
	for rows.Next() {
		var sym string
		var t time.Time
		if rows.Scan(&sym, &t) == nil {
			out[sym] = t
		}
	}
	return out, rows.Err()
}

// ---- hot list ----

// HotItem is one row of GET /home/hot.
type HotItem struct {
	Symbol             string `json:"symbol"`
	Base               string `json:"base"`
	Quote              string `json:"quote"`
	LastPrice          string `json:"lastPrice"`
	PriceChangePercent string `json:"priceChangePercent"`
	Volume             string `json:"volume"`
	QuoteVolume        string `json:"quoteVolume"`
	PriceSource        string `json:"priceSource"`
	Pinned             bool   `json:"pinned"`
}

func decOrZero(s string) decimal.Decimal {
	d, err := decimal.NewFromString(s)
	if err != nil {
		return decimal.Zero
	}
	return d
}

// rankHot orders tickers for a list type: pinned first (pin order), then the
// derived ranking. Pure; unit tested. firstSeen may be nil.
func rankHot(typ string, tickers []tickerDTO, pins []string, firstSeen map[string]time.Time, limit int) []tickerDTO {
	bySym := map[string]tickerDTO{}
	for _, t := range tickers {
		bySym[t.Symbol] = t
	}
	out := []tickerDTO{}
	used := map[string]bool{}
	for _, p := range pins {
		if t, ok := bySym[p]; ok && !used[p] {
			out = append(out, t)
			used[p] = true
		}
	}
	rest := []tickerDTO{}
	for _, t := range tickers {
		if used[t.Symbol] {
			continue
		}
		// gainers/losers only list real moves in the right direction.
		chg := decOrZero(t.PriceChangePercent)
		if (typ == "gainers" && chg.Sign() <= 0) || (typ == "losers" && chg.Sign() >= 0) {
			continue
		}
		rest = append(rest, t)
	}
	less := func(i, j int) bool {
		a, b := rest[i], rest[j]
		switch typ {
		case "gainers":
			if c := decOrZero(a.PriceChangePercent).Cmp(decOrZero(b.PriceChangePercent)); c != 0 {
				return c > 0
			}
		case "losers":
			if c := decOrZero(a.PriceChangePercent).Cmp(decOrZero(b.PriceChangePercent)); c != 0 {
				return c < 0
			}
		case "new":
			ta, oka := firstSeen[a.Symbol]
			tb, okb := firstSeen[b.Symbol]
			if oka != okb {
				return !oka // never-traded (no candle yet) counts as newest
			}
			if oka && !ta.Equal(tb) {
				return ta.After(tb)
			}
		default: // hot, volume: 24h quote volume
			if c := decOrZero(a.QuoteVolume).Cmp(decOrZero(b.QuoteVolume)); c != 0 {
				return c > 0
			}
		}
		return a.Symbol < b.Symbol
	}
	sort.SliceStable(rest, less)
	out = append(out, rest...)
	if limit > 0 && len(out) > limit {
		out = out[:limit]
	}
	return out
}

func (s *Server) handleHomeHot(w http.ResponseWriter, r *http.Request) {
	q := r.URL.Query()
	typ := strings.ToLower(q.Get("type"))
	if typ == "" {
		typ = "hot"
	}
	if !hotTypes[typ] {
		common.WriteError(w, http.StatusBadRequest, "VALIDATION_ERROR", "type 必须为 hot/gainers/losers/new/volume")
		return
	}
	limit := 20
	if v := q.Get("limit"); v != "" {
		n, err := strconv.Atoi(v)
		if err != nil || n <= 0 || n > 100 {
			common.WriteError(w, http.StatusBadRequest, "VALIDATION_ERROR", "limit 范围 1-100")
			return
		}
		limit = n
	}
	ctx := r.Context()
	infos, err := s.store.Symbols(ctx)
	if err != nil {
		log.Printf("home hot symbols: %v", err)
		common.WriteError(w, http.StatusInternalServerError, "INTERNAL", "服务器内部错误")
		return
	}
	listed := map[string]SymbolInfo{}
	for _, si := range infos {
		if si.Status == "" || si.Status == "trading" {
			listed[si.Symbol] = si
		}
	}
	var allow map[string]bool
	if cat := strings.TrimSpace(q.Get("category")); cat != "" {
		cats, err := s.store.Categories(ctx)
		if err != nil {
			log.Printf("home hot categories: %v", err)
			common.WriteError(w, http.StatusInternalServerError, "INTERNAL", "服务器内部错误")
			return
		}
		found := false
		allow = map[string]bool{}
		for _, c := range cats {
			if c.ID == cat {
				found = true
				for _, sym := range c.Symbols {
					allow[sym] = true
				}
			}
		}
		if !found {
			common.WriteError(w, http.StatusBadRequest, "VALIDATION_ERROR", "category 不存在")
			return
		}
	}
	tickers := []tickerDTO{}
	for _, sym := range s.agg.TrackedSymbols() {
		if _, ok := listed[sym]; !ok || (allow != nil && !allow[sym]) {
			continue
		}
		if t, ok := s.agg.TickerFrame(sym); ok {
			tickers = append(tickers, t)
		}
	}
	var pins []string
	if all, err := s.store.HotPins(ctx); err == nil {
		for _, p := range all {
			if p.Type == typ {
				pins = append(pins, p.Symbol)
			}
		}
	} else {
		log.Printf("home hot pins: %v", err)
	}
	var first map[string]time.Time
	if typ == "new" {
		if first, err = s.store.FirstSeen(ctx); err != nil {
			log.Printf("home hot first-seen: %v", err)
		}
	}
	pinned := map[string]bool{}
	for _, p := range pins {
		pinned[p] = true
	}
	items := []HotItem{}
	for _, t := range rankHot(typ, tickers, pins, first, limit) {
		si := listed[t.Symbol]
		items = append(items, HotItem{Symbol: t.Symbol, Base: si.Base, Quote: si.Quote, LastPrice: t.LastPrice,
			PriceChangePercent: t.PriceChangePercent, Volume: t.Volume, QuoteVolume: t.QuoteVolume, PriceSource: t.PriceSource, Pinned: pinned[t.Symbol]})
	}
	common.WriteJSON(w, http.StatusOK, map[string]any{"type": typ, "category": q.Get("category"), "items": items})
}

// ---- internal (admin) ----

func (s *Server) internal(next http.HandlerFunc) http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		tok := s.cfg.InternalToken
		if (tok == "" && !s.cfg.DevMode) || (tok != "" && r.Header.Get("X-Internal-Token") != tok) {
			common.WriteError(w, http.StatusUnauthorized, "AUTH_UNAUTHORIZED", "内部令牌无效")
			return
		}
		next(w, r)
	}
}

func (s *Server) homeInternalRoutes(mux *http.ServeMux) {
	mux.HandleFunc("GET /home/hot", s.handleHomeHot)
	s.placementRoutes(mux)
	mux.HandleFunc("GET /market/fiat-rates", s.handleFiatRates)
	s.featureRoutes(mux)
	mux.HandleFunc("GET /internal/home/hot-pins", s.internal(s.hIntPinsList))
	mux.HandleFunc("PUT /internal/home/hot-pins/{type}", s.internal(s.hIntPinsPut))
}

func intErr(w http.ResponseWriter, what string, err error) {
	log.Printf("home %s: %v", what, err)
	common.WriteError(w, http.StatusInternalServerError, "INTERNAL", "服务器内部错误")
}

func (s *Server) hIntPinsList(w http.ResponseWriter, r *http.Request) {
	pins, err := s.store.HotPins(r.Context())
	if err != nil {
		intErr(w, "pins", err)
		return
	}
	common.WriteJSON(w, http.StatusOK, map[string]any{"items": pins})
}

// PUT /internal/home/hot-pins/{type} {symbols:[...], updatedBy}
func (s *Server) hIntPinsPut(w http.ResponseWriter, r *http.Request) {
	typ := strings.ToLower(r.PathValue("type"))
	if !hotTypes[typ] {
		common.WriteError(w, http.StatusBadRequest, "VALIDATION_ERROR", "type 必须为 hot/gainers/losers/new/volume")
		return
	}
	var in struct {
		Symbols   []string `json:"symbols"`
		UpdatedBy string   `json:"updatedBy"`
	}
	if err := json.NewDecoder(r.Body).Decode(&in); err != nil {
		common.WriteError(w, http.StatusBadRequest, "VALIDATION_ERROR", "请求参数不合法")
		return
	}
	if len(in.Symbols) > 20 {
		common.WriteError(w, http.StatusBadRequest, "VALIDATION_ERROR", "每个榜单最多置顶 20 个")
		return
	}
	infos, err := s.store.Symbols(r.Context())
	if err != nil {
		intErr(w, "symbols", err)
		return
	}
	listed := map[string]bool{}
	for _, si := range infos {
		listed[si.Symbol] = true
	}
	seen := map[string]bool{}
	clean := []string{}
	for _, sym := range in.Symbols {
		sym = strings.ToUpper(strings.TrimSpace(sym))
		if !listed[sym] {
			common.WriteError(w, http.StatusBadRequest, "VALIDATION_ERROR", "交易对未上架: "+sym)
			return
		}
		if !seen[sym] {
			seen[sym] = true
			clean = append(clean, sym)
		}
	}
	var before []string
	if all, err := s.store.HotPins(r.Context()); err == nil {
		for _, p := range all {
			if p.Type == typ {
				before = append(before, p.Symbol)
			}
		}
	}
	if err := s.store.ReplaceHotPins(r.Context(), typ, clean, in.UpdatedBy); err != nil {
		intErr(w, "pins put", err)
		return
	}
	common.WriteJSON(w, http.StatusOK, map[string]any{"type": typ, "symbols": clean,
		"before": map[string]any{"type": typ, "symbols": before}, "after": map[string]any{"type": typ, "symbols": clean}})
}
