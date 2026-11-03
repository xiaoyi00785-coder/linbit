package main

import (
	"context"
	"database/sql"
	"encoding/json"
	"log"
	"net/http"
	"strings"
	"sync"

	"github.com/linbit/platform/services/go/common"
	"github.com/shopspring/decimal"
)

// netOverride is one admin override row; nil fields keep the registry value.
type netOverride struct {
	Confirmations   *int
	MinDeposit      *decimal.Decimal
	MinWithdraw     *decimal.Decimal
	MaxWithdraw     *decimal.Decimal
	WithdrawFee     *decimal.Decimal
	MemoRequired    *bool
	DepositEnabled  *bool
	WithdrawEnabled *bool
}

var (
	overlayMu sync.RWMutex
	overlay   = map[string]netOverride{}
)

func setOverlay(m map[string]netOverride) {
	overlayMu.Lock()
	overlay = m
	overlayMu.Unlock()
}

// applyOverlay layers the admin override (if any) on top of a registry entry.
func applyOverlay(n Network) Network {
	overlayMu.RLock()
	o, ok := overlay[pairKey(n.Asset, n.Network)]
	overlayMu.RUnlock()
	if !ok {
		return n
	}
	if o.Confirmations != nil {
		n.Confirmations = *o.Confirmations
	}
	if o.MinDeposit != nil {
		n.MinDeposit = *o.MinDeposit
	}
	if o.MinWithdraw != nil {
		n.MinWithdraw = *o.MinWithdraw
	}
	if o.MaxWithdraw != nil {
		n.MaxWithdraw = *o.MaxWithdraw
	}
	if o.WithdrawFee != nil {
		n.GasFeeEstimate = *o.WithdrawFee
	}
	if o.MemoRequired != nil {
		n.MemoRequired = *o.MemoRequired
	}
	if o.DepositEnabled != nil {
		n.DepositDisabled = !*o.DepositEnabled
	}
	if o.WithdrawEnabled != nil {
		n.WithdrawDisabled = !*o.WithdrawEnabled
	}
	return n
}

// LoadNetworkOverrides reads wallet_network_settings into the in-memory overlay.
func (s *Store) LoadNetworkOverrides(ctx context.Context) error {
	if s == nil || s.db == nil {
		return nil
	}
	rows, err := s.db.QueryContext(ctx, `SELECT asset, network, confirmations, min_deposit, min_withdraw,
		max_withdraw, withdraw_fee, memo_required, deposit_enabled, withdraw_enabled FROM wallet_network_settings`)
	if err != nil {
		return err
	}
	defer rows.Close()
	m := map[string]netOverride{}
	for rows.Next() {
		var (
			asset, network        string
			conf                  sql.NullInt64
			minD, minW, maxW, fee decimal.NullDecimal
			memo, depOn, wdOn     sql.NullBool
		)
		if err := rows.Scan(&asset, &network, &conf, &minD, &minW, &maxW, &fee, &memo, &depOn, &wdOn); err != nil {
			return err
		}
		var o netOverride
		if conf.Valid {
			v := int(conf.Int64)
			o.Confirmations = &v
		}
		nd := func(d decimal.NullDecimal) *decimal.Decimal {
			if !d.Valid {
				return nil
			}
			v := d.Decimal
			return &v
		}
		nb := func(b sql.NullBool) *bool {
			if !b.Valid {
				return nil
			}
			v := b.Bool
			return &v
		}
		o.MinDeposit, o.MinWithdraw, o.MaxWithdraw, o.WithdrawFee = nd(minD), nd(minW), nd(maxW), nd(fee)
		o.MemoRequired, o.DepositEnabled, o.WithdrawEnabled = nb(memo), nb(depOn), nb(wdOn)
		m[pairKey(asset, network)] = o
	}
	if err := rows.Err(); err != nil {
		return err
	}
	setOverlay(m)
	return nil
}

// networkDTO is the GET /wallet/networks item.
type networkDTO struct {
	Asset                   string `json:"asset"`
	Network                 string `json:"network"`
	MinDeposit              string `json:"minDeposit"`
	Confirmations           int    `json:"confirmations"`
	MemoRequired            bool   `json:"memoRequired"`
	WithdrawFee             string `json:"withdrawFee"`
	WithdrawFeeAsset        string `json:"withdrawFeeAsset"`
	MinWithdraw             string `json:"minWithdraw"`
	MaxWithdraw             string `json:"maxWithdraw,omitempty"`
	DepositEnabled          bool   `json:"depositEnabled"`
	WithdrawEnabled         bool   `json:"withdrawEnabled"`
	EstimatedArrivalMinutes int    `json:"estimatedArrivalMinutes"`
}

func toNetworkDTO(n Network) networkDTO {
	d := networkDTO{
		Asset: n.Asset, Network: n.Network, MinDeposit: fmt8(n.MinDeposit), Confirmations: n.Confirmations,
		MemoRequired: n.MemoRequired, WithdrawFee: fmt8(n.GasFeeEstimate), WithdrawFeeAsset: n.GasFeeAsset,
		MinWithdraw: fmt8(n.MinWithdraw), DepositEnabled: !n.DepositDisabled, WithdrawEnabled: !n.WithdrawDisabled,
		EstimatedArrivalMinutes: n.EstimatedArrivalMinutes,
	}
	if n.MaxWithdraw.Sign() > 0 {
		d.MaxWithdraw = fmt8(n.MaxWithdraw)
	}
	return d
}

func sortNetworks(list []networkDTO) {
	for i := 1; i < len(list); i++ {
		for j := i; j > 0 && (list[j].Asset+"|"+list[j].Network) < (list[j-1].Asset+"|"+list[j-1].Network); j-- {
			list[j], list[j-1] = list[j-1], list[j]
		}
	}
}

// handleNetworks: GET /wallet/networks?asset= (asset optional = all assets).
func (s *Server) handleNetworks(w http.ResponseWriter, r *http.Request) {
	if _, ok := requireUser(w, r); !ok {
		return
	}
	asset := strings.ToUpper(strings.TrimSpace(r.URL.Query().Get("asset")))
	var nets []Network
	if asset == "" {
		nets = s.cfg.Networks.All()
	} else {
		nets = s.cfg.Networks.ForAsset(asset)
	}
	out := make([]networkDTO, 0, len(nets))
	for _, n := range nets {
		out = append(out, toNetworkDTO(n))
	}
	sortNetworks(out)
	common.WriteJSON(w, http.StatusOK, map[string]any{"asset": asset, "networks": out})
}

// handleAdminNetworks: GET /internal/admin/wallet/networks — effective values
// plus the raw override row.
func (s *Server) handleAdminNetworks(w http.ResponseWriter, r *http.Request) {
	if err := s.store.LoadNetworkOverrides(r.Context()); err != nil {
		log.Printf("network overrides: %v", err)
	}
	out := make([]networkDTO, 0)
	for _, n := range s.cfg.Networks.All() {
		out = append(out, toNetworkDTO(n))
	}
	sortNetworks(out)
	common.WriteJSON(w, http.StatusOK, map[string]any{"items": out})
}

type networkOverrideReq struct {
	Confirmations   *int    `json:"confirmations"`
	MinDeposit      *string `json:"minDeposit"`
	MinWithdraw     *string `json:"minWithdraw"`
	MaxWithdraw     *string `json:"maxWithdraw"`
	WithdrawFee     *string `json:"withdrawFee"`
	MemoRequired    *bool   `json:"memoRequired"`
	DepositEnabled  *bool   `json:"depositEnabled"`
	WithdrawEnabled *bool   `json:"withdrawEnabled"`
	UpdatedBy       string  `json:"updatedBy"`
	Reset           bool    `json:"reset"` // true = delete the override row
}

// handleAdminPutNetwork: PUT /internal/admin/wallet/networks/{asset}/{network}.
// Only the supplied fields change; others keep their previous override.
func (s *Server) handleAdminPutNetwork(w http.ResponseWriter, r *http.Request) {
	asset := strings.ToUpper(r.PathValue("asset"))
	network := strings.ToUpper(r.PathValue("network"))
	before, ok := s.cfg.Networks.Lookup(asset, network)
	if !ok {
		common.WriteError(w, http.StatusNotFound, "UNSUPPORTED_NETWORK", "网络不存在")
		return
	}
	var req networkOverrideReq
	if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
		common.WriteError(w, http.StatusBadRequest, "VALIDATION_ERROR", "请求格式错误")
		return
	}
	ctx := r.Context()
	if req.Reset {
		if _, err := s.store.db.ExecContext(ctx, `DELETE FROM wallet_network_settings WHERE asset=$1 AND network=$2`, asset, network); err != nil {
			common.WriteError(w, http.StatusInternalServerError, "INTERNAL", "服务器内部错误")
			return
		}
	} else {
		dec := func(p *string) (any, bool) {
			if p == nil {
				return nil, true
			}
			d, err := decimal.NewFromString(strings.TrimSpace(*p))
			if err != nil || d.Sign() < 0 {
				return nil, false
			}
			return d.String(), true
		}
		minD, ok1 := dec(req.MinDeposit)
		minW, ok2 := dec(req.MinWithdraw)
		maxW, ok3 := dec(req.MaxWithdraw)
		fee, ok4 := dec(req.WithdrawFee)
		if !ok1 || !ok2 || !ok3 || !ok4 || (req.Confirmations != nil && (*req.Confirmations < 0 || *req.Confirmations > 10000)) {
			common.WriteError(w, http.StatusBadRequest, "VALIDATION_ERROR", "参数不合法")
			return
		}
		var conf any
		if req.Confirmations != nil {
			conf = *req.Confirmations
		}
		nb := func(b *bool) any {
			if b == nil {
				return nil
			}
			return *b
		}
		_, err := s.store.db.ExecContext(ctx, `INSERT INTO wallet_network_settings
			(asset, network, confirmations, min_deposit, min_withdraw, max_withdraw, withdraw_fee,
			 memo_required, deposit_enabled, withdraw_enabled, updated_by, updated_at)
			VALUES ($1,$2,$3,$4,$5,$6,$7,$8,$9,$10,$11,now())
			ON CONFLICT (asset, network) DO UPDATE SET
			 confirmations    = COALESCE(EXCLUDED.confirmations, wallet_network_settings.confirmations),
			 min_deposit      = COALESCE(EXCLUDED.min_deposit, wallet_network_settings.min_deposit),
			 min_withdraw     = COALESCE(EXCLUDED.min_withdraw, wallet_network_settings.min_withdraw),
			 max_withdraw     = COALESCE(EXCLUDED.max_withdraw, wallet_network_settings.max_withdraw),
			 withdraw_fee     = COALESCE(EXCLUDED.withdraw_fee, wallet_network_settings.withdraw_fee),
			 memo_required    = COALESCE(EXCLUDED.memo_required, wallet_network_settings.memo_required),
			 deposit_enabled  = COALESCE(EXCLUDED.deposit_enabled, wallet_network_settings.deposit_enabled),
			 withdraw_enabled = COALESCE(EXCLUDED.withdraw_enabled, wallet_network_settings.withdraw_enabled),
			 updated_by = EXCLUDED.updated_by, updated_at = now()`,
			asset, network, conf, minD, minW, maxW, fee, nb(req.MemoRequired), nb(req.DepositEnabled),
			nb(req.WithdrawEnabled), req.UpdatedBy)
		if err != nil {
			log.Printf("network override: %v", err)
			common.WriteError(w, http.StatusInternalServerError, "INTERNAL", "服务器内部错误")
			return
		}
	}
	if err := s.store.LoadNetworkOverrides(ctx); err != nil {
		log.Printf("network overrides reload: %v", err)
	}
	after, _ := s.cfg.Networks.Lookup(asset, network)
	common.WriteJSON(w, http.StatusOK, map[string]any{"before": toNetworkDTO(before), "after": toNetworkDTO(after)})
}

// userMemo is the deterministic per-user memo/tag for memo-required networks.
func userMemo(uid string) string {
	var h uint64 = 1469598103934665603
	for i := 0; i < len(uid); i++ {
		h ^= uint64(uid[i])
		h *= 1099511628211
	}
	return itoa(int(100000000 + h%900000000))
}

// ensureMemo stamps a memo on a deposit address row that has none.
func (s *Store) ensureMemo(ctx context.Context, uid, asset, network, memo string) error {
	_, err := s.db.ExecContext(ctx, `UPDATE deposit_addresses SET memo=$4
		WHERE user_id=$1 AND asset=$2 AND network=$3 AND memo=''`, uid, asset, network, memo)
	return err
}
