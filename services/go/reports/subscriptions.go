package main

import (
	"net/http"
	"strconv"
	"strings"

	"github.com/linbit/platform/services/go/common"
	"github.com/shopspring/decimal"
)

var channelValues = map[string]bool{"push": true, "email": true, "both": true}

type createSubReq struct {
	Asset     string `json:"asset"`
	Chain     string `json:"chain"`
	Address   string `json:"address"`
	MinAmount string `json:"minAmount"`
	Channel   string `json:"channel"`
}

type subDTO struct {
	ID        string `json:"id"`
	Asset     string `json:"asset"`
	Chain     string `json:"chain,omitempty"`
	Address   string `json:"address,omitempty"`
	MinAmount string `json:"minAmount,omitempty"`
	Channel   string `json:"channel"`
	Active    bool   `json:"active"`
	CreatedAt int64  `json:"createdAt"`
}

func toSubDTO(s *Subscription) subDTO {
	d := subDTO{
		ID:        strconv.FormatInt(s.ID, 10),
		Asset:     s.Asset,
		Chain:     s.Chain,
		Address:   s.Address,
		Channel:   s.Channel,
		Active:    s.Active,
		CreatedAt: s.CreatedAt.UnixMilli(),
	}
	if s.MinAmount.Valid {
		d.MinAmount = s.MinAmount.Decimal.String()
	}
	return d
}

// POST /notifications/subscriptions
func (s *Server) handleCreateSubscription(w http.ResponseWriter, r *http.Request, uid string) {
	var req createSubReq
	if err := common.DecodeJSON(r, &req); err != nil {
		writeErr(w, bad("VALIDATION_ERROR", "请求格式错误"))
		return
	}
	req.Asset = strings.ToUpper(strings.TrimSpace(req.Asset))
	req.Chain = strings.ToUpper(strings.TrimSpace(req.Chain))
	req.Address = strings.TrimSpace(req.Address)
	req.Channel = strings.ToLower(strings.TrimSpace(req.Channel))

	if req.Asset == "" || len(req.Asset) > 20 {
		writeErr(w, bad("VALIDATION_ERROR", "资产代码无效"))
		return
	}
	if len(req.Chain) > 20 {
		writeErr(w, bad("VALIDATION_ERROR", "链名过长"))
		return
	}
	if len(req.Address) > 128 {
		writeErr(w, bad("VALIDATION_ERROR", "地址过长"))
		return
	}
	if req.Channel == "" {
		req.Channel = "push"
	}
	if !channelValues[req.Channel] {
		writeErr(w, bad("VALIDATION_ERROR", "通知通道无效"))
		return
	}

	sub := &Subscription{
		UserID:  uid,
		Asset:   req.Asset,
		Chain:   req.Chain,
		Address: req.Address,
		Channel: req.Channel,
	}
	if strings.TrimSpace(req.MinAmount) != "" {
		d, err := decimal.NewFromString(req.MinAmount)
		if err != nil || d.IsNegative() {
			writeErr(w, bad("VALIDATION_ERROR", "最低金额无效"))
			return
		}
		sub.MinAmount = decimal.NullDecimal{Decimal: d, Valid: true}
	}
	if err := s.store.InsertSubscription(r.Context(), sub); err != nil {
		writeErr(w, err)
		return
	}
	sub.Active = true
	common.WriteJSON(w, http.StatusCreated, toSubDTO(sub))
}

// GET /notifications/subscriptions
func (s *Server) handleListSubscriptions(w http.ResponseWriter, r *http.Request, uid string) {
	subs, err := s.store.ListSubscriptions(r.Context(), uid)
	if err != nil {
		writeErr(w, err)
		return
	}
	items := make([]subDTO, 0, len(subs))
	for _, sub := range subs {
		items = append(items, toSubDTO(sub))
	}
	common.WriteJSON(w, http.StatusOK, map[string]any{"items": items})
}

// DELETE /notifications/subscriptions/{id}
func (s *Server) handleDeleteSubscription(w http.ResponseWriter, r *http.Request, uid string) {
	id, err := strconv.ParseInt(r.PathValue("id"), 10, 64)
	if err != nil || id <= 0 {
		writeErr(w, bad("VALIDATION_ERROR", "订阅编号无效"))
		return
	}
	ok, err := s.store.DeleteSubscription(r.Context(), uid, id)
	if err != nil {
		writeErr(w, err)
		return
	}
	if !ok {
		writeErr(w, notFound("NOT_FOUND", "订阅不存在"))
		return
	}
	w.WriteHeader(http.StatusNoContent)
}

