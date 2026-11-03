package main

import (
	"net/http"
	"strings"

	"github.com/linbit/platform/services/go/common"
)

type whitelistResponse struct {
	Enabled bool `json:"enabled"`
}

type setWhitelistRequest struct {
	Enabled  bool   `json:"enabled"`
	TOTPCode string `json:"totpCode"`
	Code     string `json:"code"`
}

// handleGetWhitelist returns the user's withdrawal-whitelist switch.
func (s *Server) handleGetWhitelist(w http.ResponseWriter, r *http.Request) {
	u, ok := s.authedUser(r)
	if !ok {
		common.WriteError(w, http.StatusUnauthorized, "AUTH_UNAUTHORIZED", "请先登录")
		return
	}
	common.WriteJSON(w, http.StatusOK, whitelistResponse{Enabled: u.WithdrawWhitelistEnabled})
}

// handleSetWhitelist toggles the withdrawal whitelist (gated by code/2FA).
func (s *Server) handleSetWhitelist(w http.ResponseWriter, r *http.Request) {
	u, ok := s.authedUser(r)
	if !ok {
		common.WriteError(w, http.StatusUnauthorized, "AUTH_UNAUTHORIZED", "请先登录")
		return
	}
	var req setWhitelistRequest
	if err := common.DecodeJSON(r, &req); err != nil {
		common.WriteError(w, http.StatusBadRequest, "VALIDATION_ERROR", "请求参数不合法")
		return
	}
	if !s.requireSensitiveAuth(w, r, u, req.TOTPCode, req.Code, purposeAddress) {
		return
	}
	if err := s.store.SetWithdrawWhitelistEnabled(r.Context(), u.ID, req.Enabled); err != nil {
		common.WriteError(w, http.StatusInternalServerError, "INTERNAL", "服务器内部错误")
		return
	}
	common.WriteJSON(w, http.StatusOK, whitelistResponse{Enabled: req.Enabled})
}

type addressesResponse struct {
	Items []WithdrawAddress `json:"items"`
}

type createAddressRequest struct {
	Asset       string `json:"asset"`
	Network     string `json:"network"`
	Address     string `json:"address"`
	Memo        string `json:"memo"`
	Label       string `json:"label"`
	Whitelisted bool   `json:"whitelisted"`
	TOTPCode    string `json:"totpCode"`
	Code        string `json:"code"`
}

// handleListAddresses lists the user's withdrawal address book.
func (s *Server) handleListAddresses(w http.ResponseWriter, r *http.Request) {
	u, ok := s.authedUser(r)
	if !ok {
		common.WriteError(w, http.StatusUnauthorized, "AUTH_UNAUTHORIZED", "请先登录")
		return
	}
	items, err := s.store.ListWithdrawAddresses(r.Context(), u.ID)
	if err != nil {
		common.WriteError(w, http.StatusInternalServerError, "INTERNAL", "服务器内部错误")
		return
	}
	if items == nil {
		items = []WithdrawAddress{}
	}
	common.WriteJSON(w, http.StatusOK, addressesResponse{Items: items})
}

// handleCreateAddress adds an address-book entry (gated by code/2FA).
func (s *Server) handleCreateAddress(w http.ResponseWriter, r *http.Request) {
	u, ok := s.authedUser(r)
	if !ok {
		common.WriteError(w, http.StatusUnauthorized, "AUTH_UNAUTHORIZED", "请先登录")
		return
	}
	var req createAddressRequest
	if err := common.DecodeJSON(r, &req); err != nil {
		common.WriteError(w, http.StatusBadRequest, "VALIDATION_ERROR", "请求参数不合法")
		return
	}
	req.Asset = strings.TrimSpace(req.Asset)
	req.Network = strings.TrimSpace(req.Network)
	req.Address = strings.TrimSpace(req.Address)
	if req.Asset == "" || req.Network == "" || req.Address == "" {
		common.WriteError(w, http.StatusBadRequest, "VALIDATION_ERROR", "asset/network/address 必填")
		return
	}
	if len(req.Address) > 128 || len(req.Memo) > 64 || len(req.Label) > 64 {
		common.WriteError(w, http.StatusBadRequest, "VALIDATION_ERROR", "address/memo/label 过长")
		return
	}
	if !s.requireSensitiveAuth(w, r, u, req.TOTPCode, req.Code, purposeAddress) {
		return
	}
	id, err := s.store.CreateWithdrawAddress(r.Context(), u.ID, WithdrawAddress{
		Asset:       strings.ToUpper(req.Asset),
		Network:     strings.ToUpper(req.Network),
		Address:     req.Address,
		Memo:        strings.TrimSpace(req.Memo),
		Label:       strings.TrimSpace(req.Label),
		Whitelisted: req.Whitelisted,
	})
	if err == ErrAddressExists {
		common.WriteError(w, http.StatusConflict, "ADDRESS_EXISTS", "该地址已在地址簿中")
		return
	}
	if err != nil {
		common.WriteError(w, http.StatusInternalServerError, "INTERNAL", "服务器内部错误")
		return
	}
	items, _ := s.store.ListWithdrawAddresses(r.Context(), u.ID)
	for _, a := range items {
		if a.ID == id {
			common.WriteJSON(w, http.StatusCreated, a)
			return
		}
	}
	common.WriteJSON(w, http.StatusCreated, map[string]string{"id": id})
}

// handleDeleteAddress removes an address-book entry (gated by code/2FA via
// query params totpCode / code).
func (s *Server) handleDeleteAddress(w http.ResponseWriter, r *http.Request) {
	u, ok := s.authedUser(r)
	if !ok {
		common.WriteError(w, http.StatusUnauthorized, "AUTH_UNAUTHORIZED", "请先登录")
		return
	}
	id := r.PathValue("id")
	if !s.requireSensitiveAuth(w, r, u, r.URL.Query().Get("totpCode"), r.URL.Query().Get("code"), purposeAddress) {
		return
	}
	removed, err := s.store.DeleteWithdrawAddress(r.Context(), u.ID, id)
	if err != nil {
		common.WriteError(w, http.StatusInternalServerError, "INTERNAL", "服务器内部错误")
		return
	}
	if !removed {
		common.WriteError(w, http.StatusNotFound, "NOT_FOUND", "地址不存在")
		return
	}
	w.WriteHeader(http.StatusNoContent)
}
