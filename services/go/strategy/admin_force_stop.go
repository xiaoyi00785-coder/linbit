package main

import (
	"database/sql"
	"errors"
	"net/http"
	"strconv"

	"github.com/linbit/platform/services/go/common"
)

// hInternalForceStop: admin 强制停止某个网格（需 X-Internal-Token）。
// 与用户主动停止走同一套 stopLocked（撤单、按需卖出、冻结终值），
// stop_reason = admin_force_stop。默认不卖出底仓（不替用户做市价卖出），
// 只有 admin 显式传 sellBase=true 才卖。
func (s *Server) hInternalForceStop(w http.ResponseWriter, r *http.Request) {
	if s.cfg.InternalToken == "" || r.Header.Get("X-Internal-Token") != s.cfg.InternalToken {
		common.WriteError(w, http.StatusUnauthorized, "AUTH_UNAUTHORIZED", "内部令牌无效")
		return
	}
	id, err := strconv.ParseInt(r.PathValue("id"), 10, 64)
	if err != nil {
		common.WriteError(w, http.StatusNotFound, "NOT_FOUND", "策略不存在")
		return
	}
	var body struct {
		SellBase *bool  `json:"sellBase"`
		By       string `json:"by"`
		Reason   string `json:"reason"`
	}
	if r.ContentLength > 0 {
		if err := common.DecodeJSON(r, &body); err != nil {
			common.WriteError(w, http.StatusBadRequest, "VALIDATION_ERROR", "请求体不合法")
			return
		}
	}
	sell := body.SellBase != nil && *body.SellBase
	s.mu.Lock()
	b, err := s.bot(r.Context(), id)
	if errors.Is(err, sql.ErrNoRows) {
		s.mu.Unlock()
		common.WriteError(w, http.StatusNotFound, "BOT_NOT_FOUND", "网格不存在")
		return
	}
	if err != nil {
		s.mu.Unlock()
		writeErr(w, err)
		return
	}
	if b.Status == "stopped" {
		s.mu.Unlock()
		common.WriteError(w, http.StatusConflict, "BOT_NOT_RUNNING", "网格已停止")
		return
	}
	s.stopLocked(r.Context(), id, "admin_force_stop", sell)
	b, err = s.bot(r.Context(), id)
	s.mu.Unlock()
	if err != nil {
		writeErr(w, err)
		return
	}
	common.WriteJSON(w, http.StatusOK, s.view(r.Context(), b, true))
}
