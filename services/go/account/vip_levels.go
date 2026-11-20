package main

import (
	"context"
	"database/sql"
	"encoding/json"
	"errors"
	"log"
	"net/http"
	"net/url"
	"sort"
	"strconv"
	"time"

	"github.com/shopspring/decimal"

	"github.com/linbit/platform/services/go/common"
)

// vip_levels.go: equity/volume-based VIP tier system. Distinct from the manual
// VIP concierge in vip.go — every user has a tier (default 0), recomputed
// hourly from their 30-day trading volume and current assets. Order and futures
// services read the applicable maker/taker rate for the user from
// GET /internal/vip/fee-rate?uid= rather than falling back on globals.

type vipTier struct {
	Level            int             `json:"level"`
	Name             string          `json:"name"`
	MinVolume30d     decimal.Decimal `json:"minVolume30d"`
	MinAssets        decimal.Decimal `json:"minAssets"`
	MakerRate        decimal.Decimal `json:"spotMakerRate"`
	TakerRate        decimal.Decimal `json:"spotTakerRate"`
	FuturesMakerRate decimal.Decimal `json:"futuresMakerRate"`
	FuturesTakerRate decimal.Decimal `json:"futuresTakerRate"`
	WithdrawalFree   int             `json:"withdrawalFreeCount"`
	Perks            []string        `json:"perks"`
}

type userVIPLevel struct {
	Level     int             `json:"level"`
	Volume30d decimal.Decimal `json:"volume30d"`
	Assets    decimal.Decimal `json:"assetsUsdt"`
	UpdatedAt time.Time       `json:"updatedAt"`
}

func (s *Server) listVIPTiers(ctx context.Context) ([]vipTier, error) {
	if s.store == nil || s.store.db == nil {
		return nil, errors.New("storage unavailable")
	}
	rows, err := s.store.db.QueryContext(ctx, `SELECT level, name, min_volume_30d_usdt, min_asset_usdt,
		maker_rate, taker_rate, futures_maker_rate, futures_taker_rate, withdrawal_free_cnt, perks
		FROM vip_tiers ORDER BY level`)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	out := []vipTier{}
	for rows.Next() {
		var t vipTier
		var perks []byte
		if err := rows.Scan(&t.Level, &t.Name, &t.MinVolume30d, &t.MinAssets,
			&t.MakerRate, &t.TakerRate, &t.FuturesMakerRate, &t.FuturesTakerRate,
			&t.WithdrawalFree, &perks); err != nil {
			return nil, err
		}
		if len(perks) > 0 {
			_ = json.Unmarshal(perks, &t.Perks)
		}
		if t.Perks == nil {
			t.Perks = []string{}
		}
		out = append(out, t)
	}
	return out, rows.Err()
}

func (s *Server) userVIP(ctx context.Context, userID int64) (userVIPLevel, error) {
	var lv userVIPLevel
	err := s.store.db.QueryRowContext(ctx, `SELECT level, volume_30d, assets_usdt, updated_at
		FROM user_vip_levels WHERE user_id=$1`, userID).Scan(&lv.Level, &lv.Volume30d, &lv.Assets, &lv.UpdatedAt)
	if errors.Is(err, sql.ErrNoRows) {
		return userVIPLevel{Level: 0}, nil
	}
	return lv, err
}

// GET /vip/levels — the public tier table.
func (s *Server) handleVIPLevels(w http.ResponseWriter, r *http.Request) {
	tiers, err := s.listVIPTiers(r.Context())
	if err != nil {
		log.Printf("vip levels: %v", err)
		common.WriteError(w, http.StatusInternalServerError, "INTERNAL", "服务器内部错误")
		return
	}
	common.WriteJSON(w, http.StatusOK, map[string]any{"items": tiers})
}

// GET /vip/me — current level, 30d volume and assets, next-tier gap and perks.
func (s *Server) handleVIPMe(w http.ResponseWriter, r *http.Request) {
	u, ok := s.authedUser(r)
	if !ok {
		common.WriteError(w, http.StatusUnauthorized, "AUTH_UNAUTHORIZED", "请先登录")
		return
	}
	tiers, err := s.listVIPTiers(r.Context())
	if err != nil || len(tiers) == 0 {
		common.WriteError(w, http.StatusInternalServerError, "INTERNAL", "服务器内部错误")
		return
	}
	lv, err := s.userVIP(r.Context(), u.ID)
	if err != nil {
		common.WriteError(w, http.StatusInternalServerError, "INTERNAL", "服务器内部错误")
		return
	}
	var cur, next *vipTier
	for i := range tiers {
		if tiers[i].Level == lv.Level {
			cur = &tiers[i]
		}
		if tiers[i].Level == lv.Level+1 {
			next = &tiers[i]
		}
	}
	if cur == nil {
		cur = &tiers[0]
	}
	resp := map[string]any{
		"uid":       u.UID,
		"level":     cur.Level,
		"name":      cur.Name,
		"current":   cur,
		"volume30d": lv.Volume30d.String(),
		"assetsUsdt": lv.Assets.String(),
		"updatedAt": lv.UpdatedAt.UnixMilli(),
	}
	if next != nil {
		volGap := next.MinVolume30d.Sub(lv.Volume30d)
		if volGap.IsNegative() {
			volGap = decimal.Zero
		}
		assetGap := next.MinAssets.Sub(lv.Assets)
		if assetGap.IsNegative() {
			assetGap = decimal.Zero
		}
		resp["next"] = next
		resp["nextGap"] = map[string]string{"volume30d": volGap.String(), "assetsUsdt": assetGap.String()}
	}
	common.WriteJSON(w, http.StatusOK, resp)
}

// GET /internal/vip/fee-rate?uid=&product=spot|futures
// Returns the user's effective (maker, taker) for the product.
func (s *Server) handleInternalVIPFeeRate(w http.ResponseWriter, r *http.Request) {
	if !s.authedInternal(r) {
		common.WriteError(w, http.StatusUnauthorized, "UNAUTHORIZED", "内部调用未授权")
		return
	}
	uid := r.URL.Query().Get("uid")
	product := r.URL.Query().Get("product")
	if uid == "" {
		common.WriteError(w, http.StatusBadRequest, "VALIDATION_ERROR", "uid required")
		return
	}
	if product == "" {
		product = "spot"
	}
	tiers, err := s.listVIPTiers(r.Context())
	if err != nil || len(tiers) == 0 {
		common.WriteError(w, http.StatusInternalServerError, "INTERNAL", "tier table unavailable")
		return
	}
	u, err := s.store.UserByUID(r.Context(), uid)
	if err != nil || u == nil {
		common.WriteJSON(w, http.StatusOK, defaultFeeRateFor(tiers[0], product, 0, uid))
		return
	}
	lv, _ := s.userVIP(r.Context(), u.ID)
	var cur vipTier = tiers[0]
	for _, t := range tiers {
		if t.Level == lv.Level {
			cur = t
			break
		}
	}
	common.WriteJSON(w, http.StatusOK, defaultFeeRateFor(cur, product, cur.Level, uid))
}

func defaultFeeRateFor(t vipTier, product string, level int, uid string) map[string]any {
	maker, taker := t.MakerRate, t.TakerRate
	if product == "futures" {
		maker, taker = t.FuturesMakerRate, t.FuturesTakerRate
	}
	return map[string]any{
		"uid": uid, "level": level, "product": product,
		"makerRate": maker.String(), "takerRate": taker.String(),
	}
}

// vipRecomputeLoop updates every known user's VIP tier hourly. Reads 30-day
// growth volume and asset valuation per user, picks the matching tier, writes
// user_vip_levels. Failures per user are logged and skipped.
func (s *Server) vipRecomputeLoop(ctx context.Context) {
	if s.store == nil || s.store.db == nil {
		return
	}
	// First pass soon after start-up.
	t := time.NewTimer(30 * time.Second)
	defer t.Stop()
	for {
		select {
		case <-ctx.Done():
			return
		case <-t.C:
		}
		if err := s.recomputeAllVIP(ctx); err != nil {
			log.Printf("vip recompute: %v", err)
		}
		t.Reset(time.Hour)
	}
}

func (s *Server) recomputeAllVIP(ctx context.Context) error {
	tiers, err := s.listVIPTiers(ctx)
	if err != nil || len(tiers) == 0 {
		return err
	}
	sort.Slice(tiers, func(i, j int) bool { return tiers[i].Level < tiers[j].Level })
	rows, err := s.store.db.QueryContext(ctx, `SELECT id, uid FROM users WHERE status='active' ORDER BY id`)
	if err != nil {
		return err
	}
	type u struct {
		id  int64
		uid string
	}
	var users []u
	for rows.Next() {
		var x u
		if err := rows.Scan(&x.id, &x.uid); err == nil {
			users = append(users, x)
		}
	}
	rows.Close()
	for _, uu := range users {
		vol, verr := s.fetchGrowthVolume(ctx, uu.uid, 30)
		assets, aerr := s.fetchAssets(ctx, uu.uid)
		if verr != nil && aerr != nil {
			continue // both sources unreachable; skip
		}
		best := pickTier(tiers, vol, assets)
		_, _ = s.store.db.ExecContext(ctx, `INSERT INTO user_vip_levels (user_id, uid, level, volume_30d, assets_usdt)
			VALUES ($1,$2,$3,$4,$5)
			ON CONFLICT (user_id) DO UPDATE SET level=EXCLUDED.level, volume_30d=EXCLUDED.volume_30d,
				assets_usdt=EXCLUDED.assets_usdt, uid=EXCLUDED.uid, updated_at=now()`,
			uu.id, uu.uid, best.Level, vol, assets)
	}
	return nil
}

// pickTier returns the highest tier whose thresholds the user meets. A tier
// with a MinVolume30d==0 is reachable through assets alone; a tier with
// MinAssets==0 through volume alone. Tiers with both positive need both.
func pickTier(tiers []vipTier, vol, assets decimal.Decimal) vipTier {
	best := tiers[0]
	for _, t := range tiers {
		volOk := t.MinVolume30d.Sign() == 0 || vol.GreaterThanOrEqual(t.MinVolume30d)
		astOk := t.MinAssets.Sign() == 0 || assets.GreaterThanOrEqual(t.MinAssets)
		var meets bool
		switch {
		case t.MinVolume30d.Sign() > 0 && t.MinAssets.Sign() > 0:
			meets = volOk && astOk
		case t.MinVolume30d.Sign() > 0:
			meets = volOk
		case t.MinAssets.Sign() > 0:
			meets = astOk
		default:
			meets = true
		}
		if meets && t.Level > best.Level {
			best = t
		}
	}
	return best
}

func (s *Server) fetchGrowthVolume(ctx context.Context, uid string, days int) (decimal.Decimal, error) {
	if s.cfg.GrowthInternalURL == "" {
		return decimal.Zero, errors.New("growth internal url unset")
	}
	cctx, cancel := context.WithTimeout(ctx, 4*time.Second)
	defer cancel()
	code, body, err := s.internalCall(cctx, http.MethodGet, s.cfg.GrowthInternalURL,
		"/internal/growth/volume?uid="+url.QueryEscape(uid)+"&days="+strconv.Itoa(days), nil, nil)
	if err != nil || code != http.StatusOK {
		return decimal.Zero, errors.New("growth volume unavailable")
	}
	var out struct {
		TotalUsdt string `json:"totalUsdt"`
	}
	if err := json.Unmarshal(body, &out); err != nil {
		return decimal.Zero, err
	}
	v, _ := decimal.NewFromString(out.TotalUsdt)
	return v, nil
}

func (s *Server) fetchAssets(ctx context.Context, uid string) (decimal.Decimal, error) {
	if s.cfg.AssetInternalURL == "" {
		return decimal.Zero, errors.New("asset internal url unset")
	}
	cctx, cancel := context.WithTimeout(ctx, 4*time.Second)
	defer cancel()
	code, body, err := s.internalCall(cctx, http.MethodGet, s.cfg.AssetInternalURL,
		"/internal/valuation?userId="+url.QueryEscape(uid), nil, nil)
	if err != nil || code != http.StatusOK {
		return decimal.Zero, errors.New("asset valuation unavailable")
	}
	var out struct {
		TotalValueUsdt string `json:"totalValueUsdt"`
	}
	if err := json.Unmarshal(body, &out); err != nil {
		return decimal.Zero, err
	}
	v, _ := decimal.NewFromString(out.TotalValueUsdt)
	return v, nil
}
