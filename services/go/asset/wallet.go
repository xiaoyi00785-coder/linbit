package main

import (
	"net/http"

	"github.com/linbit/platform/services/go/common"
)

// wallet.go implements the minimal user-facing wallet read endpoints used by the
// withdraw UI. They are routed through the gateway under /wallet/* and therefore
// require authentication (the gateway injects X-User-Id after validating the JWT).

// withdrawNetworkDTO mirrors the openapi WithdrawNetwork schema.
type withdrawNetworkDTO struct {
	Network                 string `json:"network"`
	MinWithdraw             string `json:"minWithdraw,omitempty"`
	GasFeeEstimate          string `json:"gasFeeEstimate,omitempty"`
	GasFeeAsset             string `json:"gasFeeAsset,omitempty"`
	EstimatedArrivalMinutes *int   `json:"estimatedArrivalMinutes,omitempty"`
}

// withdrawNetworkOptionsDTO mirrors the openapi WithdrawNetworkOptions schema.
type withdrawNetworkOptionsDTO struct {
	Asset    string               `json:"asset"`
	Networks []withdrawNetworkDTO `json:"networks"`
}

// withdrawalDTO mirrors the openapi Withdrawal schema.
type withdrawalDTO struct {
	ID          string  `json:"id"`
	Asset       string  `json:"asset"`
	Network     string  `json:"network"`
	Address     string  `json:"address"`
	Amount      string  `json:"amount"`
	Fee         string  `json:"fee"`
	GasFee      *string `json:"gasFee,omitempty"`
	GasFeeAsset *string `json:"gasFeeAsset,omitempty"`
	Status      string  `json:"status"`
	CreatedAt   string  `json:"createdAt"`
}

// paginatedWithdrawals mirrors the openapi PaginatedWithdrawals schema.
type paginatedWithdrawals struct {
	Items    []withdrawalDTO `json:"items"`
	Page     int             `json:"page"`
	PageSize int             `json:"pageSize"`
	Total    int             `json:"total"`
}

// handleWithdrawNetworks returns the configured withdrawal networks and their
// gas estimates for the requested asset. Unknown assets return an empty list.
func (s *Server) handleWithdrawNetworks(w http.ResponseWriter, r *http.Request) {
	if r.Header.Get("X-User-Id") == "" {
		common.WriteError(w, http.StatusUnauthorized, "AUTH_UNAUTHORIZED", "请先登录")
		return
	}
	asset := r.URL.Query().Get("asset")
	if asset == "" {
		common.WriteError(w, http.StatusBadRequest, "VALIDATION_ERROR", "缺少 asset")
		return
	}
	networks := make([]withdrawNetworkDTO, 0)
	for _, n := range s.cfg.WithdrawNetworks[asset] {
		networks = append(networks, withdrawNetworkDTO{
			Network:                 n.Network,
			MinWithdraw:             n.MinWithdraw,
			GasFeeEstimate:          n.GasFeeEstimate,
			GasFeeAsset:             n.GasFeeAsset,
			EstimatedArrivalMinutes: n.EstimatedArrivalMinutes,
		})
	}
	common.WriteJSON(w, http.StatusOK, withdrawNetworkOptionsDTO{Asset: asset, Networks: networks})
}

// handleWithdrawRecords returns the user's withdrawal records. Withdrawals are
// not yet persisted, so this returns an empty paginated list with the correct
// envelope; the shape is stable for the UI and ready to back with real rows.
func (s *Server) handleWithdrawRecords(w http.ResponseWriter, r *http.Request) {
	if r.Header.Get("X-User-Id") == "" {
		common.WriteError(w, http.StatusUnauthorized, "AUTH_UNAUTHORIZED", "请先登录")
		return
	}
	page := atoiDefault(r.URL.Query().Get("page"), 1, 1, 1<<30)
	pageSize := atoiDefault(r.URL.Query().Get("pageSize"), 20, 1, 100)
	common.WriteJSON(w, http.StatusOK, paginatedWithdrawals{
		Items:    make([]withdrawalDTO, 0),
		Page:     page,
		PageSize: pageSize,
		Total:    0,
	})
}
