package main

import (
	"net/http"

	"github.com/linbit/platform/services/go/common"
)

type devicesResponse struct {
	Items []Device `json:"items"`
}

// handleListDevices lists the user's recognised devices.
func (s *Server) handleListDevices(w http.ResponseWriter, r *http.Request) {
	u, ok := s.authedUser(r)
	if !ok {
		common.WriteError(w, http.StatusUnauthorized, "AUTH_UNAUTHORIZED", "请先登录")
		return
	}
	devices, err := s.store.ListDevices(r.Context(), u.ID)
	if err != nil {
		common.WriteError(w, http.StatusInternalServerError, "INTERNAL", "服务器内部错误")
		return
	}
	if devices == nil {
		devices = []Device{}
	}
	common.WriteJSON(w, http.StatusOK, devicesResponse{Items: devices})
}

// handleRemoveDevice removes a device and revokes its active sessions.
func (s *Server) handleRemoveDevice(w http.ResponseWriter, r *http.Request) {
	u, ok := s.authedUser(r)
	if !ok {
		common.WriteError(w, http.StatusUnauthorized, "AUTH_UNAUTHORIZED", "请先登录")
		return
	}
	deviceKey := r.PathValue("deviceId")
	removed, err := s.store.DeleteDevice(r.Context(), u.ID, deviceKey)
	if err != nil {
		common.WriteError(w, http.StatusInternalServerError, "INTERNAL", "服务器内部错误")
		return
	}
	if !removed {
		common.WriteError(w, http.StatusNotFound, "NOT_FOUND", "设备不存在")
		return
	}
	if err := s.store.RevokeDeviceRefreshTokens(r.Context(), u.ID, deviceKey); err != nil {
		common.WriteError(w, http.StatusInternalServerError, "INTERNAL", "服务器内部错误")
		return
	}
	w.WriteHeader(http.StatusNoContent)
}
