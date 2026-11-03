package main

import (
	"context"
	"errors"
	"log"
	"net/http"
	"regexp"
	"sort"
	"strings"
	"time"

	"github.com/linbit/platform/services/go/common"
	"github.com/shopspring/decimal"
)

// Baseline is the spot base inventory that does NOT count toward max_position.
type Baseline struct {
	Asset      string           `json:"asset"`
	Qty        decimal.Decimal  `json:"baseline"`
	Source     string           `json:"source"` // admin | seed | start | none
	TargetBase *decimal.Decimal `json:"targetBase"`
	StartBase  *decimal.Decimal `json:"startBase"`
	SeedQty    decimal.Decimal  `json:"seedQty"`
	UpdatedBy  string           `json:"updatedBy,omitempty"`
}

// resolveBaseline picks the baseline: admin target > testnet seed > start balance > 0.
func resolveBaseline(b Baseline, testnet bool) Baseline {
	switch {
	case b.TargetBase != nil:
		b.Qty, b.Source = *b.TargetBase, "admin"
	case testnet && b.SeedQty.IsPositive():
		b.Qty, b.Source = b.SeedQty, "seed"
	case b.StartBase != nil:
		b.Qty, b.Source = *b.StartBase, "start"
	default:
		b.Qty, b.Source = decimal.Zero, "none"
	}
	return b
}

// seedTotals returns virtual mm_testnet_seed credits per asset (asset ledger),
// cached briefly; invalidated after a seed.
func (m *Maker) seedTotals(ctx context.Context) (map[string]decimal.Decimal, error) {
	m.mu.Lock()
	if m.seedCache != nil && time.Since(m.seedAt) < 15*time.Second {
		c := m.seedCache
		m.mu.Unlock()
		return c, nil
	}
	m.mu.Unlock()
	var body struct {
		Items []struct {
			Asset       string `json:"asset"`
			VirtualSeed string `json:"virtualSeed"`
		} `json:"items"`
	}
	if err := call(ctx, "GET", m.cfg.AssetURL+"/internal/solvency/liabilities", m.internalHdr(), nil, &body); err != nil {
		return nil, err
	}
	out := map[string]decimal.Decimal{}
	for _, it := range body.Items {
		v, _ := decimal.NewFromString(it.VirtualSeed)
		out[strings.ToUpper(it.Asset)] = v
	}
	m.mu.Lock()
	m.seedCache, m.seedAt = out, time.Now()
	m.mu.Unlock()
	return out, nil
}

func (m *Maker) invalidateSeeds() {
	m.mu.Lock()
	m.seedCache = nil
	m.mu.Unlock()
}

// Baselines loads every stored/seeded baseline, resolved.
func (m *Maker) Baselines(ctx context.Context) (map[string]Baseline, error) {
	out := map[string]Baseline{}
	rows, err := m.db.QueryContext(ctx, `SELECT asset, target_base, start_base, updated_by FROM mm_inventory_baseline`)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	for rows.Next() {
		var b Baseline
		var tb, sb decimal.NullDecimal
		if err := rows.Scan(&b.Asset, &tb, &sb, &b.UpdatedBy); err != nil {
			return nil, err
		}
		if tb.Valid {
			b.TargetBase = &tb.Decimal
		}
		if sb.Valid {
			b.StartBase = &sb.Decimal
		}
		out[b.Asset] = b
	}
	if err := rows.Err(); err != nil {
		return nil, err
	}
	testnet, _ := common.TestnetSeedAllowed()
	if testnet {
		seeds, err := m.seedTotals(ctx)
		if err != nil {
			return nil, err
		}
		for a, q := range seeds {
			if a == "USDT" {
				continue
			}
			b := out[a]
			b.Asset, b.SeedQty = a, q
			out[a] = b
		}
	}
	for a, b := range out {
		out[a] = resolveBaseline(b, testnet)
	}
	return out, nil
}

// captureStartBaselines records the current base balance of every spot base
// asset the first time the MM is started (never overwritten; admin can reset).
func (m *Maker) captureStartBaselines(ctx context.Context) {
	bal, err := m.spotBalances(ctx)
	if err != nil {
		log.Printf("mm: baseline capture: %v", err)
		return
	}
	m.mu.Lock()
	assets := map[string]bool{}
	for _, r := range m.rules {
		if r.Market == "spot" && r.Base != "" {
			assets[strings.ToUpper(r.Base)] = true
		}
	}
	m.mu.Unlock()
	for a := range assets {
		if _, err := m.db.ExecContext(ctx, `INSERT INTO mm_inventory_baseline (asset, start_base, start_at) VALUES ($1,$2,now())
			ON CONFLICT (asset) DO UPDATE SET start_base=$2, start_at=now(), updated_at=now()
			WHERE mm_inventory_baseline.start_base IS NULL`, a, bal[a]); err != nil {
			log.Printf("mm: baseline capture %s: %v", a, err)
		}
	}
}

func (m *Maker) handleBaselines(w http.ResponseWriter, r *http.Request) {
	bs, err := m.Baselines(r.Context())
	if err != nil {
		common.WriteError(w, 500, "INTERNAL", "服务器内部错误")
		return
	}
	items := make([]Baseline, 0, len(bs))
	for _, b := range bs {
		items = append(items, b)
	}
	sort.Slice(items, func(i, j int) bool { return items[i].Asset < items[j].Asset })
	common.WriteJSON(w, 200, map[string]any{"items": items})
}

var assetRe = regexp.MustCompile(`^[A-Z0-9]{1,16}$`)

type baselinePatch struct {
	TargetBase  *string `json:"targetBase"` // "" or null clears (with clearTarget)
	ClearTarget bool    `json:"clearTarget"`
	ResetStart  bool    `json:"resetStart"` // re-capture the start balance at next start
	UpdatedBy   string  `json:"updatedBy"`
}

func (p baselinePatch) target() (*decimal.Decimal, error) {
	if p.TargetBase == nil || p.ClearTarget {
		return nil, nil
	}
	v, err := decimal.NewFromString(strings.TrimSpace(*p.TargetBase))
	if err != nil || v.IsNegative() || v.GreaterThan(decimal.NewFromInt(1e12)) {
		return nil, errors.New("targetBase 必须为 0–1e12 的数字")
	}
	return &v, nil
}

// PUT /internal/mm/baselines/{asset} {targetBase?, clearTarget?, resetStart?, updatedBy}
func (m *Maker) handleUpdateBaseline(w http.ResponseWriter, r *http.Request) {
	a := strings.ToUpper(r.PathValue("asset"))
	var p baselinePatch
	if !assetRe.MatchString(a) || common.DecodeJSON(r, &p) != nil {
		common.WriteError(w, 400, "INVALID_REQUEST", "请求格式错误")
		return
	}
	tb, err := p.target()
	if err != nil {
		common.WriteError(w, 400, "INVALID_PARAM", err.Error())
		return
	}
	if tb == nil && !p.ClearTarget && !p.ResetStart {
		common.WriteError(w, 400, "INVALID_PARAM", "需提供 targetBase / clearTarget / resetStart")
		return
	}
	by := firstNonEmpty(p.UpdatedBy, "admin")
	_, err = m.db.ExecContext(r.Context(), `INSERT INTO mm_inventory_baseline (asset, target_base, updated_by) VALUES ($1,$2,$5)
		ON CONFLICT (asset) DO UPDATE SET
		  target_base = CASE WHEN $3 THEN NULL WHEN $2::numeric IS NOT NULL THEN $2::numeric ELSE mm_inventory_baseline.target_base END,
		  start_base  = CASE WHEN $4 THEN NULL ELSE mm_inventory_baseline.start_base END,
		  start_at    = CASE WHEN $4 THEN NULL ELSE mm_inventory_baseline.start_at END,
		  updated_by=$5, updated_at=now()`, a, nd(tb), p.ClearTarget, p.ResetStart, by)
	if err != nil {
		log.Printf("mm: baseline %s: %v", a, err)
		common.WriteError(w, 500, "INTERNAL", "服务器内部错误")
		return
	}
	bs, err := m.Baselines(r.Context())
	if err != nil {
		common.WriteError(w, 500, "INTERNAL", "服务器内部错误")
		return
	}
	common.WriteJSON(w, 200, bs[a])
}
