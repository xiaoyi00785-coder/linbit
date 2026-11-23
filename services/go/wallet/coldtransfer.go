package main

import (
	"context"
	"database/sql"
	"errors"
	"fmt"
	"log"
	"net/http"
	"strconv"
	"strings"
	"time"

	"github.com/linbit/platform/services/go/common"
	"github.com/shopspring/decimal"
)

// Manual hot→cold transfer statuses. Broadcast happens only after a second
// admin approves (status=approved); the worker then claims, signs via
// wallet-signer, and tracks confirmations.
const (
	ctPending    = "pending_review"
	ctApproved   = "approved"
	ctProcessing = "processing"
	ctCompleted  = "completed"
	ctFailed     = "failed"
	ctRejected   = "rejected"
	ctCanceled   = "canceled"
)

const (
	ctModeAmount  = "amount"
	ctModeReserve = "reserve"
)

// ColdSnap is the on-chain view of one (asset, network) hot/cold pair.
type ColdSnap struct {
	Asset         string          `json:"asset"`
	Network       string          `json:"network"`
	Chain         string          `json:"chain"`
	Hot           string          `json:"hot"`
	Cold          string          `json:"cold"`
	Balance       decimal.Decimal `json:"balance"`
	NativeBalance decimal.Decimal `json:"nativeBalance"`
	NativeAsset   string          `json:"nativeAsset"`
	GasEstimate   decimal.Decimal `json:"gasEstimate"`
	NativeIsAsset bool            `json:"nativeIsAsset"`
}

// coldMover is the chain-side of a manual hot→cold transfer. RealChain
// implements it; tests inject a stub. SendToConfiguredCold must refuse any
// destination other than the currently configured cold address.
type coldMover interface {
	Snapshot(ctx context.Context, asset, network string) (ColdSnap, error)
	SendToConfiguredCold(ctx context.Context, asset, network, dest string, amount decimal.Decimal) (string, error)
	ConfirmTx(ctx context.Context, network, txHash string) (confs int64, failed bool, err error)
}

// HotColdTransfer is one admin-initiated hot→cold movement.
type HotColdTransfer struct {
	ID              int64           `json:"id"`
	Asset           string          `json:"asset"`
	Network         string          `json:"network"`
	Mode            string          `json:"mode"`
	Amount          decimal.Decimal `json:"amount"`
	ReserveAmount   decimal.Decimal `json:"reserveAmount"`
	Destination     string          `json:"destination"`
	Reason          string          `json:"reason"`
	InitiatedBy     int64           `json:"initiatedBy"`
	InitiatedByName string          `json:"initiatedByName"`
	ApprovedBy      *int64          `json:"approvedBy,omitempty"`
	ApprovedByName  string          `json:"approvedByName,omitempty"`
	ApprovedAt      *time.Time      `json:"approvedAt,omitempty"`
	RejectedBy      *int64          `json:"rejectedBy,omitempty"`
	RejectedByName  string          `json:"rejectedByName,omitempty"`
	RejectedAt      *time.Time      `json:"rejectedAt,omitempty"`
	RejectReason    string          `json:"rejectReason,omitempty"`
	IdempotencyKey  string          `json:"idempotencyKey"`
	TxHash          string          `json:"txHash,omitempty"`
	BroadcastHeight int64           `json:"broadcastHeight,omitempty"`
	Confirmations   int64           `json:"confirmations"`
	Status          string          `json:"status"`
	Error           string          `json:"error,omitempty"`
	CreatedAt       time.Time       `json:"createdAt"`
	UpdatedAt       time.Time       `json:"updatedAt"`
}

func (s *Server) mover() coldMover {
	if s.coldMove != nil {
		return s.coldMove
	}
	if s.real != nil {
		return s.real
	}
	return nil
}

// computeColdSend returns the amount to send from the hot wallet.
func computeColdSend(mode string, amount, reserve, balance decimal.Decimal) (decimal.Decimal, error) {
	switch mode {
	case ctModeAmount:
		if amount.Sign() <= 0 {
			return decimal.Zero, errors.New("amount must be positive")
		}
		if amount.GreaterThan(balance) {
			return decimal.Zero, errors.New("amount exceeds hot wallet balance")
		}
		return amount, nil
	case ctModeReserve:
		if reserve.Sign() < 0 {
			return decimal.Zero, errors.New("reserve must be >= 0")
		}
		send := balance.Sub(reserve)
		if send.Sign() <= 0 {
			return decimal.Zero, errors.New("hot wallet balance does not exceed the reserve")
		}
		return send, nil
	default:
		return decimal.Zero, errors.New("mode must be amount or reserve")
	}
}

func sameChainAddr(a, b string) bool {
	a, b = strings.TrimSpace(a), strings.TrimSpace(b)
	if a == "" || b == "" {
		return false
	}
	if strings.HasPrefix(a, "0x") || strings.HasPrefix(strings.ToLower(b), "0x") {
		return strings.EqualFold(a, b)
	}
	return a == b
}

// coldTransferMax returns the optional per-asset (or per-pair) cap, or zero if unset.
func coldTransferMax(asset, network string) decimal.Decimal {
	if v := common.Env(envKey("WALLET_COLD_TRANSFER_MAX", asset, network), ""); v != "" {
		if d, err := decimal.NewFromString(v); err == nil {
			return d
		}
	}
	if v := common.Env(envKey("WALLET_COLD_TRANSFER_MAX", asset), ""); v != "" {
		if d, err := decimal.NewFromString(v); err == nil {
			return d
		}
	}
	return decimal.Zero
}

// coldTransferMaxUSDT is the USD-equivalent per-tx cap (default 100000).
func coldTransferMaxUSDT() decimal.Decimal {
	return decimalEnv("WALLET_COLD_TRANSFER_MAX_USDT", "100000")
}

func (s *Server) checkColdMax(ctx context.Context, asset, network string, amount decimal.Decimal) *apiError {
	if capAmt := coldTransferMax(asset, network); capAmt.Sign() > 0 && amount.GreaterThan(capAmt) {
		return errf(http.StatusBadRequest, "OVER_MAX", fmt.Sprintf("单笔超过上限 %s %s", capAmt.String(), asset))
	}
	usdtCap := coldTransferMaxUSDT()
	if usdtCap.Sign() <= 0 {
		return nil
	}
	px, ok := s.usdtPrice(ctx, asset)
	if !ok {
		return errf(http.StatusBadRequest, "PRICE_UNAVAILABLE", "无法计价，拒绝发起热转冷")
	}
	if amount.Mul(px).GreaterThan(usdtCap) {
		return errf(http.StatusBadRequest, "OVER_MAX", fmt.Sprintf("单笔超过上限 %s USDT", usdtCap.String()))
	}
	return nil
}

func validateGas(snap ColdSnap, send decimal.Decimal) error {
	if snap.NativeIsAsset {
		need := send.Add(snap.GasEstimate)
		if snap.Balance.LessThan(need) {
			return fmt.Errorf("native balance %s %s is short of amount+gas %s", snap.Balance, snap.NativeAsset, need)
		}
		return nil
	}
	if snap.GasEstimate.Sign() > 0 && snap.NativeBalance.LessThan(snap.GasEstimate) {
		return fmt.Errorf("hot wallet gas %s %s is short of %s", snap.NativeBalance, snap.NativeAsset, snap.GasEstimate)
	}
	if snap.Balance.LessThan(send) {
		return fmt.Errorf("hot wallet balance %s is short of %s", snap.Balance, send)
	}
	return nil
}

func (s *Server) handleColdTransferQuote(w http.ResponseWriter, r *http.Request) {
	asset := strings.TrimSpace(r.URL.Query().Get("asset"))
	network := strings.TrimSpace(r.URL.Query().Get("network"))
	if asset == "" || network == "" {
		common.WriteError(w, http.StatusBadRequest, "VALIDATION_ERROR", "缺少 asset/network")
		return
	}
	m := s.mover()
	if m == nil {
		common.WriteError(w, http.StatusServiceUnavailable, "CHAIN_UNAVAILABLE", "未启用链上钱包")
		return
	}
	snap, err := m.Snapshot(r.Context(), asset, network)
	if err != nil {
		common.WriteError(w, http.StatusBadRequest, "QUOTE_FAILED", err.Error())
		return
	}
	inFlight, _ := s.store.HasInFlightColdTransfer(r.Context(), asset, network)
	common.WriteJSON(w, http.StatusOK, map[string]any{
		"snapshot": snap,
		"inFlight": inFlight,
		"maxAsset": coldTransferMax(asset, network).String(),
		"maxUsdt":  coldTransferMaxUSDT().String(),
	})
}

func (s *Server) handleColdTransferList(w http.ResponseWriter, r *http.Request) {
	limit := 100
	if v := r.URL.Query().Get("limit"); v != "" {
		if n, err := strconv.Atoi(v); err == nil && n > 0 && n <= 200 {
			limit = n
		}
	}
	rows, err := s.store.ListHotColdTransfers(r.Context(), strings.TrimSpace(r.URL.Query().Get("status")), limit)
	if err != nil {
		log.Printf("cold-transfers list: %v", err)
		common.WriteError(w, http.StatusInternalServerError, "INTERNAL", "服务器内部错误")
		return
	}
	if rows == nil {
		rows = []HotColdTransfer{}
	}
	common.WriteJSON(w, http.StatusOK, map[string]any{"items": rows})
}

func (s *Server) handleColdTransferGet(w http.ResponseWriter, r *http.Request) {
	id, ok := pathInt64(r, "id")
	if !ok {
		common.WriteError(w, http.StatusBadRequest, "VALIDATION_ERROR", "id 不合法")
		return
	}
	row, err := s.store.GetHotColdTransfer(r.Context(), id)
	if errors.Is(err, sql.ErrNoRows) {
		common.WriteError(w, http.StatusNotFound, "NOT_FOUND", "记录不存在")
		return
	}
	if err != nil {
		log.Printf("cold-transfer get: %v", err)
		common.WriteError(w, http.StatusInternalServerError, "INTERNAL", "服务器内部错误")
		return
	}
	common.WriteJSON(w, http.StatusOK, row)
}

type coldTransferCreateReq struct {
	Asset           string `json:"asset"`
	Network         string `json:"network"`
	Mode            string `json:"mode"`
	Amount          string `json:"amount"`
	ReserveAmount   string `json:"reserveAmount"`
	Reason          string `json:"reason"`
	InitiatedBy     int64  `json:"initiatedBy"`
	InitiatedByName string `json:"initiatedByName"`
	IdempotencyKey  string `json:"idempotencyKey"`
	// Destination is accepted only to be discarded: the configured cold
	// address is always used. Clients must not send a custom target.
	Destination string `json:"destination"`
}

func (s *Server) handleColdTransferCreate(w http.ResponseWriter, r *http.Request) {
	var req coldTransferCreateReq
	if err := common.DecodeJSON(r, &req); err != nil {
		common.WriteError(w, http.StatusBadRequest, "VALIDATION_ERROR", "请求格式错误")
		return
	}
	row, apiErr := s.createColdTransfer(r.Context(), req)
	if apiErr != nil {
		common.WriteError(w, apiErr.status, apiErr.code, apiErr.msg)
		return
	}
	common.WriteJSON(w, http.StatusOK, row)
}

func (s *Server) createColdTransfer(ctx context.Context, req coldTransferCreateReq) (*HotColdTransfer, *apiError) {
	// Never honour a client-supplied destination.
	req.Destination = ""
	asset := strings.ToUpper(strings.TrimSpace(req.Asset))
	network := strings.ToUpper(strings.TrimSpace(req.Network))
	mode := strings.ToLower(strings.TrimSpace(req.Mode))
	reason := strings.TrimSpace(req.Reason)
	key := strings.TrimSpace(req.IdempotencyKey)
	if asset == "" || network == "" || reason == "" || key == "" || req.InitiatedBy <= 0 {
		return nil, errf(http.StatusBadRequest, "VALIDATION_ERROR", "asset/network/reason/idempotencyKey/initiatedBy 必填")
	}
	if len(reason) > 500 {
		return nil, errf(http.StatusBadRequest, "VALIDATION_ERROR", "原因过长")
	}
	if _, ok := s.cfg.Networks.Lookup(asset, network); !ok {
		return nil, errf(http.StatusBadRequest, "UNSUPPORTED_NETWORK", "不支持的网络")
	}
	m := s.mover()
	if m == nil {
		return nil, errf(http.StatusServiceUnavailable, "CHAIN_UNAVAILABLE", "未启用链上钱包")
	}
	if existing, err := s.store.GetHotColdTransferByKey(ctx, key); err == nil {
		return existing, nil // idempotent replay
	} else if err != nil && !errors.Is(err, sql.ErrNoRows) {
		return nil, errf(http.StatusInternalServerError, "INTERNAL", "服务器内部错误")
	}
	if ok, err := s.store.HasInFlightColdTransfer(ctx, asset, network); err != nil {
		return nil, errf(http.StatusInternalServerError, "INTERNAL", "服务器内部错误")
	} else if ok {
		return nil, errf(http.StatusConflict, "IN_FLIGHT", "该币种网络已有进行中的热转冷，请等待完成")
	}
	snap, err := m.Snapshot(ctx, asset, network)
	if err != nil {
		return nil, errf(http.StatusBadRequest, "QUOTE_FAILED", err.Error())
	}
	if strings.TrimSpace(snap.Cold) == "" {
		return nil, errf(http.StatusBadRequest, "NO_COLD_ADDRESS", "未配置冷钱包地址")
	}
	amt, _ := decimal.NewFromString(strings.TrimSpace(req.Amount))
	res, _ := decimal.NewFromString(strings.TrimSpace(req.ReserveAmount))
	send, err := computeColdSend(mode, amt, res, snap.Balance)
	if err != nil {
		return nil, errf(http.StatusBadRequest, "VALIDATION_ERROR", err.Error())
	}
	if err := validateGas(snap, send); err != nil {
		return nil, errf(http.StatusBadRequest, "INSUFFICIENT_GAS", err.Error())
	}
	if apiErr := s.checkColdMax(ctx, asset, network, send); apiErr != nil {
		return nil, apiErr
	}
	row := HotColdTransfer{
		Asset: asset, Network: network, Mode: mode,
		Amount: send, ReserveAmount: res, Destination: snap.Cold,
		Reason: reason, InitiatedBy: req.InitiatedBy,
		InitiatedByName: strings.TrimSpace(req.InitiatedByName),
		IdempotencyKey:  key, Status: ctPending,
	}
	id, err := s.store.InsertHotColdTransfer(ctx, row)
	if err != nil {
		if isUniqueViolation(err) {
			if existing, e2 := s.store.GetHotColdTransferByKey(ctx, key); e2 == nil {
				return existing, nil
			}
			return nil, errf(http.StatusConflict, "IN_FLIGHT", "该币种网络已有进行中的热转冷，请等待完成")
		}
		log.Printf("insert hot_cold_transfers: %v", err)
		return nil, errf(http.StatusInternalServerError, "INTERNAL", "服务器内部错误")
	}
	got, err := s.store.GetHotColdTransfer(ctx, id)
	if err != nil {
		return nil, errf(http.StatusInternalServerError, "INTERNAL", "服务器内部错误")
	}
	return got, nil
}

type coldTransferDecisionReq struct {
	ActorID   int64  `json:"actorId"`
	ActorName string `json:"actorName"`
	Reason    string `json:"reason"`
}

func (s *Server) handleColdTransferApprove(w http.ResponseWriter, r *http.Request) {
	s.handleColdDecision(w, r, true)
}

func (s *Server) handleColdTransferReject(w http.ResponseWriter, r *http.Request) {
	s.handleColdDecision(w, r, false)
}

func (s *Server) handleColdTransferCancel(w http.ResponseWriter, r *http.Request) {
	id, ok := pathInt64(r, "id")
	if !ok {
		common.WriteError(w, http.StatusBadRequest, "VALIDATION_ERROR", "id 不合法")
		return
	}
	var req coldTransferDecisionReq
	_ = common.DecodeJSON(r, &req)
	if req.ActorID <= 0 {
		common.WriteError(w, http.StatusBadRequest, "VALIDATION_ERROR", "actorId 必填")
		return
	}
	row, err := s.store.GetHotColdTransfer(r.Context(), id)
	if errors.Is(err, sql.ErrNoRows) {
		common.WriteError(w, http.StatusNotFound, "NOT_FOUND", "记录不存在")
		return
	}
	if err != nil {
		common.WriteError(w, http.StatusInternalServerError, "INTERNAL", "服务器内部错误")
		return
	}
	if row.InitiatedBy != req.ActorID {
		common.WriteError(w, http.StatusForbidden, "NOT_INITIATOR", "只有发起人可以撤销")
		return
	}
	okApply, err := s.store.CancelHotColdTransfer(r.Context(), id, req.ActorID, strings.TrimSpace(req.Reason))
	if err != nil {
		common.WriteError(w, http.StatusInternalServerError, "INTERNAL", "服务器内部错误")
		return
	}
	if !okApply {
		common.WriteError(w, http.StatusConflict, "INVALID_STATE", "当前状态不可撤销")
		return
	}
	got, _ := s.store.GetHotColdTransfer(r.Context(), id)
	common.WriteJSON(w, http.StatusOK, got)
}

func (s *Server) handleColdDecision(w http.ResponseWriter, r *http.Request, approve bool) {
	id, ok := pathInt64(r, "id")
	if !ok {
		common.WriteError(w, http.StatusBadRequest, "VALIDATION_ERROR", "id 不合法")
		return
	}
	var req coldTransferDecisionReq
	if err := common.DecodeJSON(r, &req); err != nil && r.ContentLength != 0 {
		common.WriteError(w, http.StatusBadRequest, "VALIDATION_ERROR", "请求格式错误")
		return
	}
	if req.ActorID <= 0 {
		common.WriteError(w, http.StatusBadRequest, "VALIDATION_ERROR", "actorId 必填")
		return
	}
	if !approve && strings.TrimSpace(req.Reason) == "" {
		common.WriteError(w, http.StatusBadRequest, "REASON_REQUIRED", "请填写驳回原因")
		return
	}
	row, err := s.store.GetHotColdTransfer(r.Context(), id)
	if errors.Is(err, sql.ErrNoRows) {
		common.WriteError(w, http.StatusNotFound, "NOT_FOUND", "记录不存在")
		return
	}
	if err != nil {
		common.WriteError(w, http.StatusInternalServerError, "INTERNAL", "服务器内部错误")
		return
	}
	if row.InitiatedBy == req.ActorID {
		common.WriteError(w, http.StatusForbidden, "SELF_REVIEW", "不能复核自己发起的热转冷")
		return
	}
	var applied bool
	if approve {
		applied, err = s.store.ApproveHotColdTransfer(r.Context(), id, req.ActorID, strings.TrimSpace(req.ActorName))
	} else {
		applied, err = s.store.RejectHotColdTransfer(r.Context(), id, req.ActorID, strings.TrimSpace(req.ActorName), strings.TrimSpace(req.Reason))
	}
	if err != nil {
		common.WriteError(w, http.StatusInternalServerError, "INTERNAL", "服务器内部错误")
		return
	}
	if !applied {
		common.WriteError(w, http.StatusConflict, "INVALID_STATE", "当前状态不可复核")
		return
	}
	got, _ := s.store.GetHotColdTransfer(r.Context(), id)
	common.WriteJSON(w, http.StatusOK, got)
}

// ProcessColdTransfers signs+broadcasts approved hot→cold transfers and
// completes those with enough confirmations. Returns (broadcast, completed).
func (s *Server) ProcessColdTransfers(ctx context.Context) (int, int, error) {
	m := s.mover()
	if m == nil {
		return 0, 0, nil
	}
	broadcast, completed := 0, 0
	approved, err := s.store.ListHotColdTransfers(ctx, ctApproved, 50)
	if err != nil {
		return 0, 0, err
	}
	for i := range approved {
		if s.broadcastColdTransfer(ctx, m, &approved[i]) {
			broadcast++
		}
	}
	processing, err := s.store.ListHotColdTransfers(ctx, ctProcessing, 50)
	if err != nil {
		return broadcast, 0, err
	}
	for i := range processing {
		if s.completeColdTransfer(ctx, m, &processing[i]) {
			completed++
		}
	}
	return broadcast, completed, nil
}

func (s *Server) broadcastColdTransfer(ctx context.Context, m coldMover, row *HotColdTransfer) bool {
	claimed, err := s.store.ClaimHotColdTransfer(ctx, row.ID)
	if err != nil || !claimed {
		return false
	}
	snap, err := m.Snapshot(ctx, row.Asset, row.Network)
	if err != nil {
		s.failColdTransfer(ctx, row.ID, "snapshot: "+err.Error())
		return false
	}
	if !sameChainAddr(row.Destination, snap.Cold) {
		s.failColdTransfer(ctx, row.ID, "cold address changed; refusing to broadcast")
		return false
	}
	if err := validateGas(snap, row.Amount); err != nil {
		s.failColdTransfer(ctx, row.ID, err.Error())
		return false
	}
	hash, err := m.SendToConfiguredCold(ctx, row.Asset, row.Network, row.Destination, row.Amount)
	if err != nil {
		log.Printf("cold-transfer %d: send failed: %v", row.ID, err)
		s.failColdTransfer(ctx, row.ID, "broadcast_failed: "+err.Error())
		return false
	}
	if err := s.store.SetHotColdTx(ctx, row.ID, hash); err != nil {
		log.Printf("cold-transfer %d: sent tx %s but could not record it: %v", row.ID, hash, err)
		return false
	}
	log.Printf("cold-transfer %d broadcast tx=%s", row.ID, hash)
	return true
}

func (s *Server) completeColdTransfer(ctx context.Context, m coldMover, row *HotColdTransfer) bool {
	if row.TxHash == "" {
		if time.Since(row.UpdatedAt) > time.Hour {
			log.Printf("cold-transfer %d: processing without tx hash; manual reconciliation required", row.ID)
			s.failColdTransfer(ctx, row.ID, "no tx recorded; check on chain")
		}
		return false
	}
	confs, failed, err := m.ConfirmTx(ctx, row.Network, row.TxHash)
	if err != nil {
		return false
	}
	if failed {
		s.failColdTransfer(ctx, row.ID, "tx_failed")
		return false
	}
	_ = s.store.SetHotColdConfirmations(ctx, row.ID, confs)
	need := int64(1)
	if net, ok := s.cfg.Networks.Lookup(row.Asset, row.Network); ok && net.Confirmations > 0 {
		need = int64(net.Confirmations)
	}
	if confs < need {
		return false
	}
	ok, err := s.store.CompleteHotColdTransfer(ctx, row.ID, confs)
	if err != nil {
		log.Printf("cold-transfer %d complete: %v", row.ID, err)
		return false
	}
	return ok
}

func (s *Server) failColdTransfer(ctx context.Context, id int64, reason string) {
	if err := s.store.FailHotColdTransfer(ctx, id, reason); err != nil {
		log.Printf("cold-transfer %d fail: %v", id, err)
	}
}

func pathInt64(r *http.Request, name string) (int64, bool) {
	id, err := strconv.ParseInt(r.PathValue(name), 10, 64)
	return id, err == nil && id > 0
}

func isUniqueViolation(err error) bool {
	if err == nil {
		return false
	}
	s := err.Error()
	return strings.Contains(s, "duplicate key") || strings.Contains(s, "uq_hot_cold") || strings.Contains(s, "unique constraint")
}

// ---- RealChain implementation ----

// Snapshot implements coldMover.
func (rc *RealChain) Snapshot(ctx context.Context, asset, network string) (ColdSnap, error) {
	cn, tok, err := rc.lookup(network, asset)
	if err != nil {
		return ColdSnap{}, err
	}
	bal, err := rc.balance(ctx, cn, tok, cn.hot.Address)
	if err != nil {
		return ColdSnap{}, fmt.Errorf("hot balance: %w", err)
	}
	gas, err := cn.client.TransferCost(ctx, tok)
	if err != nil {
		return ColdSnap{}, fmt.Errorf("gas estimate: %w", err)
	}
	snap := ColdSnap{
		Asset: asset, Network: network, Chain: string(cn.chain),
		Hot: cn.hot.Address, Cold: cn.cold, Balance: bal,
		NativeAsset: cn.native, GasEstimate: gas, NativeIsAsset: tok == nil,
	}
	if tok != nil {
		nb, err := cn.client.NativeBalance(ctx, cn.hot.Address)
		if err != nil {
			return ColdSnap{}, fmt.Errorf("native balance: %w", err)
		}
		snap.NativeBalance = nb
	} else {
		snap.NativeBalance = bal
	}
	return snap, nil
}

// SendToConfiguredCold implements coldMover. dest must match the configured
// cold address; the send always goes to cn.cold, never to a caller-chosen addr.
func (rc *RealChain) SendToConfiguredCold(ctx context.Context, asset, network, dest string, amount decimal.Decimal) (string, error) {
	cn, tok, err := rc.lookup(network, asset)
	if err != nil {
		return "", err
	}
	if strings.TrimSpace(cn.cold) == "" {
		return "", errors.New("no configured cold address")
	}
	if !sameChainAddr(dest, cn.cold) {
		return "", errors.New("destination is not the configured cold address")
	}
	if amount.Sign() <= 0 {
		return "", errors.New("amount must be positive")
	}
	return cn.client.Send(ctx, rc.signer, cn.hot, cn.cold, tok, amount)
}

// ConfirmTx implements coldMover.
func (rc *RealChain) ConfirmTx(ctx context.Context, network, txHash string) (int64, bool, error) {
	return rc.TxConfirmations(ctx, network, txHash)
}

// ---- store ----

func scanHotCold(sc interface {
	Scan(dest ...any) error
}) (*HotColdTransfer, error) {
	var row HotColdTransfer
	var approvedBy, rejectedBy sql.NullInt64
	var approvedAt, rejectedAt sql.NullTime
	err := sc.Scan(&row.ID, &row.Asset, &row.Network, &row.Mode, &row.Amount, &row.ReserveAmount,
		&row.Destination, &row.Reason, &row.InitiatedBy, &row.InitiatedByName,
		&approvedBy, &row.ApprovedByName, &approvedAt,
		&rejectedBy, &row.RejectedByName, &rejectedAt, &row.RejectReason,
		&row.IdempotencyKey, &row.TxHash, &row.BroadcastHeight, &row.Confirmations,
		&row.Status, &row.Error, &row.CreatedAt, &row.UpdatedAt)
	if err != nil {
		return nil, err
	}
	if approvedBy.Valid {
		id := approvedBy.Int64
		row.ApprovedBy = &id
	}
	if approvedAt.Valid {
		t := approvedAt.Time
		row.ApprovedAt = &t
	}
	if rejectedBy.Valid {
		id := rejectedBy.Int64
		row.RejectedBy = &id
	}
	if rejectedAt.Valid {
		t := rejectedAt.Time
		row.RejectedAt = &t
	}
	return &row, nil
}

const hotColdCols = `id, asset, network, mode, amount, reserve_amount, destination, reason,
	initiated_by, initiated_by_name, approved_by, approved_by_name, approved_at,
	rejected_by, rejected_by_name, rejected_at, reject_reason,
	idempotency_key, tx_hash, broadcast_height, confirmations, status, error, created_at, updated_at`

func (s *Store) GetHotColdTransfer(ctx context.Context, id int64) (*HotColdTransfer, error) {
	row := s.db.QueryRowContext(ctx, `SELECT `+hotColdCols+` FROM hot_cold_transfers WHERE id = $1`, id)
	return scanHotCold(row)
}

func (s *Store) GetHotColdTransferByKey(ctx context.Context, key string) (*HotColdTransfer, error) {
	row := s.db.QueryRowContext(ctx, `SELECT `+hotColdCols+` FROM hot_cold_transfers WHERE idempotency_key = $1`, key)
	return scanHotCold(row)
}

func (s *Store) HasInFlightColdTransfer(ctx context.Context, asset, network string) (bool, error) {
	var n int
	err := s.db.QueryRowContext(ctx, `SELECT count(*) FROM hot_cold_transfers
		WHERE asset = $1 AND network = $2 AND status IN ('pending_review','approved','processing')`,
		asset, network).Scan(&n)
	return n > 0, err
}

func (s *Store) ListHotColdTransfers(ctx context.Context, status string, limit int) ([]HotColdTransfer, error) {
	if limit <= 0 {
		limit = 100
	}
	var rows *sql.Rows
	var err error
	if status != "" {
		rows, err = s.db.QueryContext(ctx, `SELECT `+hotColdCols+` FROM hot_cold_transfers
			WHERE status = $1 ORDER BY id DESC LIMIT $2`, status, limit)
	} else {
		rows, err = s.db.QueryContext(ctx, `SELECT `+hotColdCols+` FROM hot_cold_transfers
			ORDER BY id DESC LIMIT $1`, limit)
	}
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	var out []HotColdTransfer
	for rows.Next() {
		row, err := scanHotCold(rows)
		if err != nil {
			return nil, err
		}
		out = append(out, *row)
	}
	return out, rows.Err()
}

func (s *Store) InsertHotColdTransfer(ctx context.Context, row HotColdTransfer) (int64, error) {
	var id int64
	err := s.db.QueryRowContext(ctx, `INSERT INTO hot_cold_transfers (
			asset, network, mode, amount, reserve_amount, destination, reason,
			initiated_by, initiated_by_name, idempotency_key, status)
		VALUES ($1,$2,$3,$4,$5,$6,$7,$8,$9,$10,$11) RETURNING id`,
		row.Asset, row.Network, row.Mode, row.Amount, row.ReserveAmount, row.Destination, row.Reason,
		row.InitiatedBy, row.InitiatedByName, row.IdempotencyKey, row.Status).Scan(&id)
	return id, err
}

func (s *Store) ApproveHotColdTransfer(ctx context.Context, id, actorID int64, actorName string) (bool, error) {
	res, err := s.db.ExecContext(ctx, `UPDATE hot_cold_transfers
		SET status = $3, approved_by = $2, approved_by_name = $4, approved_at = now(), updated_at = now()
		WHERE id = $1 AND status = $5 AND initiated_by <> $2`,
		id, actorID, ctApproved, actorName, ctPending)
	if err != nil {
		return false, err
	}
	n, _ := res.RowsAffected()
	return n == 1, nil
}

func (s *Store) RejectHotColdTransfer(ctx context.Context, id, actorID int64, actorName, reason string) (bool, error) {
	res, err := s.db.ExecContext(ctx, `UPDATE hot_cold_transfers
		SET status = $3, rejected_by = $2, rejected_by_name = $4, rejected_at = now(),
		    reject_reason = $5, updated_at = now()
		WHERE id = $1 AND status = $6 AND initiated_by <> $2`,
		id, actorID, ctRejected, actorName, reason, ctPending)
	if err != nil {
		return false, err
	}
	n, _ := res.RowsAffected()
	return n == 1, nil
}

func (s *Store) CancelHotColdTransfer(ctx context.Context, id, actorID int64, reason string) (bool, error) {
	res, err := s.db.ExecContext(ctx, `UPDATE hot_cold_transfers
		SET status = $3, reject_reason = $4, rejected_by = $2, rejected_by_name = initiated_by_name,
		    rejected_at = now(), updated_at = now()
		WHERE id = $1 AND status = $5 AND initiated_by = $2`,
		id, actorID, ctCanceled, reason, ctPending)
	if err != nil {
		return false, err
	}
	n, _ := res.RowsAffected()
	return n == 1, nil
}

func (s *Store) ClaimHotColdTransfer(ctx context.Context, id int64) (bool, error) {
	res, err := s.db.ExecContext(ctx, `UPDATE hot_cold_transfers
		SET status = $2, updated_at = now() WHERE id = $1 AND status = $3`,
		id, ctProcessing, ctApproved)
	if err != nil {
		return false, err
	}
	n, _ := res.RowsAffected()
	return n == 1, nil
}

func (s *Store) SetHotColdTx(ctx context.Context, id int64, txHash string) error {
	_, err := s.db.ExecContext(ctx, `UPDATE hot_cold_transfers
		SET tx_hash = $2, updated_at = now() WHERE id = $1 AND status = $3 AND tx_hash = ''`,
		id, txHash, ctProcessing)
	return err
}

func (s *Store) SetHotColdConfirmations(ctx context.Context, id int64, confs int64) error {
	_, err := s.db.ExecContext(ctx, `UPDATE hot_cold_transfers
		SET confirmations = $2, updated_at = now() WHERE id = $1 AND status = $3`,
		id, confs, ctProcessing)
	return err
}

func (s *Store) CompleteHotColdTransfer(ctx context.Context, id int64, confs int64) (bool, error) {
	res, err := s.db.ExecContext(ctx, `UPDATE hot_cold_transfers
		SET status = $2, confirmations = $3, updated_at = now()
		WHERE id = $1 AND status = $4 AND tx_hash <> ''`,
		id, ctCompleted, confs, ctProcessing)
	if err != nil {
		return false, err
	}
	n, _ := res.RowsAffected()
	return n == 1, nil
}

func (s *Store) FailHotColdTransfer(ctx context.Context, id int64, reason string) error {
	_, err := s.db.ExecContext(ctx, `UPDATE hot_cold_transfers
		SET status = $2, error = $3, updated_at = now()
		WHERE id = $1 AND status IN ($4, $5)`,
		id, ctFailed, reason, ctApproved, ctProcessing)
	return err
}
