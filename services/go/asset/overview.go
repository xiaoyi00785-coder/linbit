package main

import (
	"context"
	"encoding/json"
	"fmt"
	"log"
	"net/http"
	"net/url"
	"strings"
	"time"

	"github.com/linbit/platform/services/go/common"
	"github.com/shopspring/decimal"
)

// overview.go: GET /assets/overview — per-wallet balances (funding, spot,
// futures) plus USDT valuations from the market-data index. A missing price
// leaves valuationUsdt null for that asset (and out of the totals) rather than
// failing the whole call; an unreachable futures service yields
// futures.available=false.

var overviewHTTP = &http.Client{Timeout: 3 * time.Second}

type overviewAccount struct {
	Account        string       `json:"account"`
	Available      bool         `json:"available"`
	TotalValueUsdt string       `json:"totalValueUsdt"`
	Balances       []balanceDTO `json:"balances"`
}

func (s *Server) indexPrice(ctx context.Context, asset string, cache map[string]*decimal.Decimal) *decimal.Decimal {
	asset = strings.ToUpper(asset)
	if asset == "USDT" {
		one := decimal.NewFromInt(1)
		return &one
	}
	if p, ok := cache[asset]; ok {
		return p
	}
	cache[asset] = nil
	if s.cfg.MarketURL == "" {
		return nil
	}
	u := fmt.Sprintf("%s/market/index?symbol=%s", strings.TrimRight(s.cfg.MarketURL, "/"), url.QueryEscape(asset+"USDT"))
	req, err := http.NewRequestWithContext(ctx, http.MethodGet, u, nil)
	if err != nil {
		return nil
	}
	resp, err := overviewHTTP.Do(req)
	if err != nil {
		return nil
	}
	defer resp.Body.Close()
	if resp.StatusCode != http.StatusOK {
		return nil
	}
	var t struct {
		IndexPrice string `json:"indexPrice"`
	}
	if json.NewDecoder(resp.Body).Decode(&t) != nil {
		return nil
	}
	p, err := decimal.NewFromString(t.IndexPrice)
	if err != nil || p.Sign() <= 0 {
		return nil
	}
	cache[asset] = &p
	return &p
}

func (s *Server) valued(ctx context.Context, name string, bs []Balance, cache map[string]*decimal.Decimal) overviewAccount {
	acc := overviewAccount{Account: name, Available: true, Balances: []balanceDTO{}}
	total := decimal.Zero
	for _, b := range bs {
		d := toBalanceDTO(b)
		if p := s.indexPrice(ctx, b.Asset, cache); p != nil {
			v := b.Total().Mul(*p).StringFixed(2)
			d.ValuationUsd = &v
			total = total.Add(b.Total().Mul(*p))
		}
		acc.Balances = append(acc.Balances, d)
	}
	acc.TotalValueUsdt = total.StringFixed(2)
	return acc
}

// futuresWallet reads the user's futures USDT wallet from the futures service.
func (s *Server) futuresWallet(ctx context.Context, userID string) (Balance, bool) {
	if s.cfg.FuturesURL == "" {
		return Balance{}, false
	}
	req, err := http.NewRequestWithContext(ctx, http.MethodGet, strings.TrimRight(s.cfg.FuturesURL, "/")+"/futures/account", nil)
	if err != nil {
		return Balance{}, false
	}
	req.Header.Set("X-User-Id", userID)
	req.Header.Set("X-Internal-Token", s.cfg.InternalToken)
	resp, err := overviewHTTP.Do(req)
	if err != nil {
		log.Printf("overview: futures: %v", err)
		return Balance{}, false
	}
	defer resp.Body.Close()
	if resp.StatusCode != http.StatusOK {
		return Balance{}, false
	}
	var a struct {
		Asset            string `json:"asset"`
		MarginBalance    string `json:"marginBalance"`
		AvailableBalance string `json:"availableBalance"`
	}
	if json.NewDecoder(resp.Body).Decode(&a) != nil {
		return Balance{}, false
	}
	eq, err1 := decimal.NewFromString(a.MarginBalance)
	av, err2 := decimal.NewFromString(a.AvailableBalance)
	if err1 != nil || err2 != nil {
		return Balance{}, false
	}
	if a.Asset == "" {
		a.Asset = "USDT"
	}
	// free = withdrawable to spot/funding; locked = margin in use (+uPnL).
	locked := eq.Sub(av)
	if locked.IsNegative() {
		locked = decimal.Zero
	}
	return Balance{Asset: a.Asset, Free: av, Locked: locked}, true
}

// GET /assets/overview
func (s *Server) handleOverview(w http.ResponseWriter, r *http.Request) {
	userID := r.Header.Get("X-User-Id")
	if userID == "" {
		common.WriteError(w, http.StatusUnauthorized, "AUTH_UNAUTHORIZED", "请先登录")
		return
	}
	ctx := r.Context()
	if err := s.store.EnsureAccount(ctx, userID); err != nil {
		log.Printf("overview: ensure: %v", err)
		common.WriteError(w, http.StatusInternalServerError, "INTERNAL", "服务器内部错误")
		return
	}
	spot, err := s.store.WalletBalances(ctx, AcctSpot, userID)
	if err != nil {
		log.Printf("overview: spot: %v", err)
		common.WriteError(w, http.StatusInternalServerError, "INTERNAL", "服务器内部错误")
		return
	}
	funding, err := s.store.WalletBalances(ctx, AcctFunding, userID)
	if err != nil {
		log.Printf("overview: funding: %v", err)
		common.WriteError(w, http.StatusInternalServerError, "INTERNAL", "服务器内部错误")
		return
	}
	cache := map[string]*decimal.Decimal{}
	fa := s.valued(ctx, AcctFunding, funding, cache)
	sa := s.valued(ctx, AcctSpot, spot, cache)
	var fu overviewAccount
	if b, ok := s.futuresWallet(ctx, userID); ok {
		fu = s.valued(ctx, "futures", []Balance{b}, cache)
	} else {
		fu = overviewAccount{Account: "futures", Available: false, TotalValueUsdt: "0.00", Balances: []balanceDTO{}}
	}
	total := decimal.Zero
	for _, a := range []overviewAccount{fa, sa, fu} {
		total = total.Add(decimal.RequireFromString(a.TotalValueUsdt))
	}
	settings, _ := s.store.Settings(ctx)
	common.WriteJSON(w, http.StatusOK, map[string]any{
		"totalValueUsdt":  total.StringFixed(2),
		"accounts":        []overviewAccount{fa, sa, fu},
		"depositAccount":  settings["deposit_account"],
		"withdrawAccount": settings["withdraw_account"],
		"transferable":    map[string]any{"funding": []string{"spot", "futures"}, "spot": []string{"funding", "futures"}, "futures": []string{"funding", "spot"}, "futuresAssets": []string{"USDT"}},
	})
}
