package main

import (
	"context"
	"encoding/json"
	"fmt"
	"log"
	"net/http"
	"sort"
	"strings"
	"time"

	"github.com/linbit/platform/services/go/common"
	"github.com/shopspring/decimal"
)

// solvency.go: daily reconciliation — per asset, on-chain holdings (wallet)
// must cover every user balance (asset spot ledger + futures margin
// accounts), the market-maker account included. The testnet-only virtual MM
// seed (mm_testnet_seed) is unbacked by design and excluded from the required
// amount; in production it cannot exist. A shortfall or an unreadable source
// is a failure and sends a Telegram alert.

type SolvencyLine struct {
	Asset       string `json:"asset"`
	Holdings    string `json:"holdings"`
	Users       string `json:"userBalances"` // all accounts incl. MM
	MarketMaker string `json:"marketMaker"`
	VirtualSeed string `json:"virtualSeed"`
	Required    string `json:"required"`
	Surplus     string `json:"surplus"`
	OK          bool   `json:"ok"`
}

type SolvencyReport struct {
	OK        bool           `json:"ok"`
	Mode      string         `json:"walletMode"`
	Lines     []SolvencyLine `json:"lines"`
	Errors    []string       `json:"errors"`
	CheckedAt string         `json:"checkedAt"`
}

type liabItem struct {
	Asset       string `json:"asset"`
	Total       string `json:"total"`
	MarketMaker string `json:"marketMaker"`
	VirtualSeed string `json:"virtualSeed"`
}

func dz(s string) decimal.Decimal { v, _ := decimal.NewFromString(s); return v }

func (s *Server) fetchJSON(ctx context.Context, base, path string, out any) error {
	u, err := s.call(ctx, http.MethodGet, base, path, nil)
	if err != nil {
		return err
	}
	if !u.ok() {
		return fmt.Errorf("status %d %s", u.Status, u.errCode())
	}
	return json.Unmarshal(u.Body, out)
}

// ComputeSolvency is the pure comparison (unit tested).
func ComputeSolvency(holdings map[string]string, liabs []liabItem, errs []string) SolvencyReport {
	type agg struct{ users, mm, seed decimal.Decimal }
	by := map[string]*agg{}
	for _, l := range liabs {
		a := strings.ToUpper(l.Asset)
		if by[a] == nil {
			by[a] = &agg{}
		}
		by[a].users = by[a].users.Add(dz(l.Total))
		by[a].mm = by[a].mm.Add(dz(l.MarketMaker))
		by[a].seed = by[a].seed.Add(dz(l.VirtualSeed))
	}
	h := map[string]decimal.Decimal{}
	for k, v := range holdings {
		h[strings.ToUpper(k)] = dz(v)
	}
	assets := map[string]bool{}
	for a := range by {
		assets[a] = true
	}
	for a := range h {
		assets[a] = true
	}
	keys := make([]string, 0, len(assets))
	for a := range assets {
		keys = append(keys, a)
	}
	sort.Strings(keys)
	rep := SolvencyReport{OK: len(errs) == 0, Errors: errs, Lines: []SolvencyLine{}}
	if rep.Errors == nil {
		rep.Errors = []string{}
	}
	for _, a := range keys {
		g := by[a]
		if g == nil {
			g = &agg{}
		}
		req := decimal.Max(g.users.Sub(g.seed), decimal.Zero)
		line := SolvencyLine{Asset: a, Holdings: h[a].String(), Users: g.users.String(), MarketMaker: g.mm.String(),
			VirtualSeed: g.seed.String(), Required: req.String(), Surplus: h[a].Sub(req).String(), OK: h[a].GreaterThanOrEqual(req)}
		if !line.OK {
			rep.OK = false
		}
		rep.Lines = append(rep.Lines, line)
	}
	return rep
}

// RunSolvency gathers the inputs, stores the report and alerts on failure.
func (s *Server) RunSolvency(ctx context.Context, trigger string) SolvencyReport {
	var errs []string
	var hold struct {
		Mode     string            `json:"mode"`
		Holdings map[string]string `json:"holdings"`
		Errors   []string          `json:"errors"`
	}
	if err := s.fetchJSON(ctx, s.cfg.WalletURL, "/internal/solvency/holdings", &hold); err != nil {
		errs = append(errs, "wallet holdings: "+err.Error())
	}
	errs = append(errs, hold.Errors...)
	var liabs []liabItem
	for _, src := range []struct{ name, base string }{{"asset", s.cfg.AssetURL}, {"futures", s.cfg.FuturesURL}} {
		var body struct {
			Items []liabItem `json:"items"`
		}
		if err := s.fetchJSON(ctx, src.base, "/internal/solvency/liabilities", &body); err != nil {
			errs = append(errs, src.name+" liabilities: "+err.Error())
			continue
		}
		liabs = append(liabs, body.Items...)
	}
	rep := ComputeSolvency(hold.Holdings, liabs, errs)
	rep.Mode = hold.Mode
	rep.CheckedAt = time.Now().UTC().Format(time.RFC3339)
	b, _ := json.Marshal(rep)
	if _, err := s.db.ExecContext(ctx, `INSERT INTO solvency_reports (trigger, ok, report) VALUES ($1,$2,$3)`, trigger, rep.OK, string(b)); err != nil {
		log.Printf("solvency store: %v", err)
	}
	result := "ok"
	if !rep.OK {
		result = "shortfall"
		var parts []string
		for _, l := range rep.Lines {
			if !l.OK {
				parts = append(parts, fmt.Sprintf("%s 链上 %s < 应付 %s（差 %s）", l.Asset, l.Holdings, l.Required, l.Surplus))
			}
		}
		parts = append(parts, rep.Errors...)
		common.AlertTelegram("🚨 Linbit 偿付对账失败（" + trigger + "）：" + strings.Join(parts, "；"))
	}
	s.auditSystem("solvency", "reconcile", "solvency:"+trigger, result, "")
	return rep
}

// RunSolvencyDaily runs once a day at SOLVENCY_HOUR_UTC (UTC).
func (s *Server) RunSolvencyDaily(ctx context.Context) {
	if s.cfg.SolvencyHourUTC < 0 || s.cfg.SolvencyHourUTC > 23 {
		log.Printf("solvency: daily job disabled")
		return
	}
	for {
		now := time.Now().UTC()
		next := time.Date(now.Year(), now.Month(), now.Day(), s.cfg.SolvencyHourUTC, 0, 0, 0, time.UTC)
		if !next.After(now) {
			next = next.Add(24 * time.Hour)
		}
		select {
		case <-ctx.Done():
			return
		case <-time.After(time.Until(next)):
		}
		rctx, cancel := context.WithTimeout(ctx, 2*time.Minute)
		rep := s.RunSolvency(rctx, "daily")
		cancel()
		log.Printf("solvency: daily ok=%v", rep.OK)
	}
}

func (s *Server) handleSolvencyRun(w http.ResponseWriter, r *http.Request) {
	a := actorOf(r)
	rep := s.RunSolvency(r.Context(), "manual:"+a.Username)
	common.WriteJSON(w, http.StatusOK, rep)
}

func (s *Server) handleSolvencyLatest(w http.ResponseWriter, r *http.Request) {
	var raw string
	var trig string
	var at time.Time
	err := s.db.QueryRowContext(r.Context(), `SELECT trigger, report::text, created_at FROM solvency_reports ORDER BY id DESC LIMIT 1`).Scan(&trig, &raw, &at)
	if err != nil {
		common.WriteJSON(w, http.StatusOK, map[string]any{"report": nil})
		return
	}
	common.WriteJSON(w, http.StatusOK, map[string]any{"trigger": trig, "createdAt": at.UTC().Format(time.RFC3339), "report": json.RawMessage(raw)})
}
