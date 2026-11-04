package main

import (
	"github.com/linbit/platform/services/go/common"

	"context"
	"fmt"
	"log"
	"net/http"
	"strings"
	"time"

	"github.com/shopspring/decimal"
)

// withdraw.go implements the withdrawal flow and state machine:
//
//   submit -> freeze(amount[+gas]) -> risk decision
//     auto    -> approved -> (processor) sign + broadcast -> processing -> complete
//     review  -> pending_review -> admin approve -> approved -> ...  (or reject)
//   user cancel (while pending_review) / broadcast failure -> unfreeze.

// apiError is a handler-level error carrying an HTTP status and business code.
type apiError struct {
	status int
	code   string
	msg    string
}

func errf(status int, code, msg string) *apiError { return &apiError{status, code, msg} }

// withdrawInput is the validated submit payload.
type withdrawInput struct {
	Asset    string
	Network  string
	Address  string
	Memo     string
	Amount   decimal.Decimal
	TOTPCode string
	Code     string
	// FundPassword is checked by account when the fund-password policy applies.
	FundPassword string
}

// withdrawRef is the idempotency/bookkeeping key for a withdrawal's ledger ops.
func withdrawRef(id int64) string { return fmt.Sprintf("withdraw:%d", id) }

// sendAmount is the amount that actually leaves to the destination. In "amount"
// gas mode the gas is taken out of it; in "extra" mode the full amount is sent.
func sendAmount(w *Withdrawal) decimal.Decimal {
	if w.GasDeductMode == "amount" {
		return w.Amount.Sub(w.GasFee)
	}
	return w.Amount
}

// submitWithdrawal validates, authorises, freezes, risk-scores and persists a
// withdrawal. It returns the created record (re-read) or an apiError.
func (s *Server) submitWithdrawal(ctx context.Context, userID string, in withdrawInput) (*Withdrawal, *apiError) {
	net, ok := s.cfg.Networks.Lookup(in.Asset, in.Network)
	if !ok {
		return nil, errf(http.StatusBadRequest, "UNSUPPORTED_NETWORK", "不支持的提现网络")
	}
	if net.WithdrawDisabled {
		return nil, errf(http.StatusBadRequest, "WITHDRAW_DISABLED", "该网络暂停提现")
	}
	if net.MemoRequired && strings.TrimSpace(in.Memo) == "" {
		return nil, errf(http.StatusBadRequest, "MEMO_REQUIRED", "该网络提现需要填写 Memo/Tag")
	}
	if !s.tradfiAllowed(ctx, in.Asset, in.Network, true) {
		return nil, errTradFiDisabled
	}
	if in.Amount.Sign() <= 0 {
		return nil, errf(http.StatusBadRequest, "VALIDATION_ERROR", "金额不合法")
	}
	if in.Amount.LessThan(net.MinWithdraw) {
		return nil, errf(http.StatusBadRequest, "MIN_WITHDRAW", "低于最小提现数量 "+net.MinWithdraw.String())
	}
	if net.MaxWithdraw.Sign() > 0 && in.Amount.GreaterThan(net.MaxWithdraw) {
		return nil, errf(http.StatusBadRequest, "MAX_WITHDRAW", "超过最大提现数量 "+net.MaxWithdraw.String())
	}

	if v, ok := s.chain.(AddressValidator); ok {
		if err := v.ValidateAddress(net.Network, in.Address); err != nil {
			return nil, errf(http.StatusBadRequest, "INVALID_ADDRESS", "提现地址格式不正确")
		}
	}

	// KYC-tiered rolling-24h withdrawal limit (checked before any funds are frozen).
	if apiErr := s.checkDailyLimit(ctx, userID, in.Asset, in.Amount); apiErr != nil {
		return nil, apiErr
	}

	// Gas estimate and how much to freeze.
	gas, err := s.chain.EstimateGas(ctx, net)
	if err != nil {
		return nil, errf(http.StatusInternalServerError, "INTERNAL", "Gas 估算失败")
	}
	frozen := in.Amount
	if s.cfg.GasDeductMode == "extra" {
		frozen = in.Amount.Add(gas)
	} else if in.Amount.LessThanOrEqual(gas) {
		// "amount" mode: the amount must exceed gas or nothing would be sent.
		return nil, errf(http.StatusBadRequest, "MIN_WITHDRAW", "提现金额需大于链上 Gas")
	}

	// Second factor (reuses account's verification via the internal endpoint).
	vr, err := s.account.Verify2FA(ctx, userID, in.TOTPCode, in.Code, OutflowOpts{Action: "withdraw", FundPassword: in.FundPassword})
	if err != nil {
		return nil, errf(http.StatusInternalServerError, "INTERNAL", "二次验证服务不可用")
	}
	if !vr.OK {
		return nil, verifyError(vr)
	}

	// Whitelist: when enabled, the destination must be a whitelisted address.
	wl, err := s.account.WhitelistCheck(ctx, userID, in.Asset, in.Network, in.Address)
	if err != nil {
		return nil, errf(http.StatusInternalServerError, "INTERNAL", "白名单校验失败")
	}
	if wl.Enabled && !wl.Allowed {
		return nil, errf(http.StatusForbidden, "WHITELIST_BLOCKED", "已开启提现白名单，只能提现到白名单地址")
	}

	// Risk scoring.
	newAddr, err := notPriorAddress(ctx, s.store, userID, in.Asset, in.Network, in.Address)
	if err != nil {
		return nil, errf(http.StatusInternalServerError, "INTERNAL", "服务器内部错误")
	}
	count24h, err := s.store.CountWithdrawals24h(ctx, userID, time.Now().Add(-24*time.Hour))
	if err != nil {
		return nil, errf(http.StatusInternalServerError, "INTERNAL", "服务器内部错误")
	}
	score := s.risk.Score(RiskInput{
		Amount:      in.Amount,
		NewAddress:  newAddr,
		NewDevice:   count24h == 0, // proxy: first withdrawal in 24h (see summary)
		WhitelistOn: wl.Enabled,
		Count24h:    count24h,
	})
	// Thresholds are USDT-equivalent; an unknown price fails closed to review.
	riskValue := in.Amount
	if p, ok := s.usdtPrice(ctx, in.Asset); ok {
		riskValue = in.Amount.Mul(p)
	} else {
		riskValue = decimal.Max(in.Amount, s.cfg.Risk.ReviewMinAmount)
	}
	decision := decide(s.cfg.Risk, riskValue, score)

	// Persist as pending_review, then freeze, then (if auto) release to approved.
	w := Withdrawal{
		UserID: userID, Asset: in.Asset, Network: in.Network, Address: in.Address, Memo: in.Memo,
		Amount: in.Amount, GasFee: gas, GasFeeAsset: net.GasFeeAsset, GasDeductMode: s.cfg.GasDeductMode,
		FrozenAmount: frozen, RiskScore: score, Status: wStatusReview,
	}
	id, err := s.store.InsertWithdrawal(ctx, w)
	if err != nil {
		return nil, errf(http.StatusInternalServerError, "INTERNAL", "服务器内部错误")
	}

	if err := s.asset.FreezeWithdraw(ctx, userID, in.Asset, frozen, withdrawRef(id)); err != nil {
		_, _ = s.store.FailSubmit(ctx, id, "freeze_failed")
		if strings.Contains(err.Error(), "INSUFFICIENT_BALANCE") {
			return nil, errf(http.StatusBadRequest, "INSUFFICIENT_BALANCE", "余额不足")
		}
		return nil, errf(http.StatusInternalServerError, "INTERNAL", "冻结资金失败")
	}

	if decision == decisionAuto {
		if _, err := s.store.Approve(ctx, id); err != nil {
			log.Printf("auto-approve %d: %v", id, err)
		}
	}
	wd, err := s.store.GetWithdrawal(ctx, id)
	if err != nil {
		return nil, errf(http.StatusInternalServerError, "INTERNAL", "服务器内部错误")
	}
	return wd, nil
}

// notPriorAddress returns true when the user has NOT withdrawn to this address
// before (i.e. it is a new destination — a risk signal).
func notPriorAddress(ctx context.Context, store *Store, userID, asset, network, address string) (bool, error) {
	prior, err := store.HasPriorWithdrawalTo(ctx, userID, asset, network, address)
	if err != nil {
		return false, err
	}
	return !prior, nil
}

// adminReject rejects a pending_review withdrawal and releases the frozen funds.
func (s *Server) adminReject(ctx context.Context, id int64, reason string) (bool, error) {
	w, err := s.store.GetWithdrawal(ctx, id)
	if err != nil {
		return false, err
	}
	applied, err := s.store.Reject(ctx, id, reason)
	if err != nil || !applied {
		return false, err
	}
	common.Notify(common.Notice{UID: w.UserID, Kind: common.NoticeTrade, Title: "提现申请被驳回",
		Body: "您的 " + w.Amount.String() + " " + w.Asset + " 提现申请未通过审核，冻结资金已退回。原因：" + reason})
	if err := s.asset.Unfreeze(ctx, w.UserID, w.Asset, w.FrozenAmount, withdrawRef(id)); err != nil {
		log.Printf("reject %d: unfreeze: %v", id, err)
		return true, err
	}
	return true, nil
}

// userCancel cancels a pending_review withdrawal owned by userID and releases the
// frozen funds.
func (s *Server) userCancel(ctx context.Context, id int64, userID string) (bool, error) {
	w, err := s.store.GetWithdrawal(ctx, id)
	if err != nil {
		return false, err
	}
	if w.UserID != userID {
		return false, nil // not the owner: treat as not-cancelable
	}
	applied, err := s.store.Cancel(ctx, id)
	if err != nil || !applied {
		return false, err
	}
	if err := s.asset.Unfreeze(ctx, w.UserID, w.Asset, w.FrozenAmount, withdrawRef(id)); err != nil {
		log.Printf("cancel %d: unfreeze: %v", id, err)
		return true, err
	}
	return true, nil
}

// ProcessWithdrawals runs one pass of the withdrawal pipeline: sign+broadcast
// approved withdrawals, then complete processing ones that have enough
// confirmations. Returns (broadcast, completed) counts.
func (s *Server) ProcessWithdrawals(ctx context.Context) (int, int, error) {
	broadcast := 0
	approved, err := s.store.ListWithdrawalsByStatus(ctx, wStatusApproved, 50)
	if err != nil {
		return 0, 0, err
	}
	for i := range approved {
		w := &approved[i]
		if s.broadcastWithdrawal(ctx, w) {
			broadcast++
		}
	}

	completed := 0
	processing, err := s.store.ListWithdrawalsByStatus(ctx, wStatusProcessing, 50)
	if err != nil {
		return broadcast, 0, err
	}
	for i := range processing {
		w := &processing[i]
		if s.completeWithdrawal(ctx, w) {
			completed++
		}
	}
	return broadcast, completed, nil
}

// broadcastWithdrawal signs an approved withdrawal with the hot wallet and
// broadcasts it, moving it to processing. On any failure it fails the withdrawal
// and releases the frozen funds.
func (s *Server) broadcastWithdrawal(ctx context.Context, w *Withdrawal) bool {
	net, ok := s.cfg.Networks.Lookup(w.Asset, w.Network)
	if !ok {
		s.failAndRelease(ctx, w, "unknown_network")
		return false
	}
	if sender, ok := s.chain.(ChainSender); ok {
		return s.sendRealWithdrawal(ctx, w, net, sender)
	}
	req := SignRequest{
		Asset: w.Asset, Network: w.Network,
		FromAddress: "platform-hot-" + strings.ToLower(w.Network),
		ToAddress:   w.Address, Amount: sendAmount(w), GasFee: w.GasFee,
		Nonce: withdrawRef(w.ID),
	}
	signed, err := s.hotSigner.Sign(req)
	if err != nil {
		log.Printf("sign %d: %v", w.ID, err)
		s.failAndRelease(ctx, w, "sign_failed")
		return false
	}
	txHash, height, err := s.chain.Broadcast(ctx, net, signed)
	if err != nil {
		log.Printf("broadcast %d: %v", w.ID, err)
		s.failAndRelease(ctx, w, "broadcast_failed")
		return false
	}
	applied, err := s.store.MarkProcessing(ctx, w.ID, txHash, height)
	if err != nil {
		log.Printf("mark processing %d: %v", w.ID, err)
		return false
	}
	return applied
}

// completeWithdrawal finalises a processing withdrawal once it has enough
// confirmations: it debits the locked funds (gas to the platform account) via
// the asset service and marks it completed.
func (s *Server) completeWithdrawal(ctx context.Context, w *Withdrawal) bool {
	net, ok := s.cfg.Networks.Lookup(w.Asset, w.Network)
	if !ok {
		return false
	}
	var confs int64
	if tc, ok := s.chain.(TxConfirmer); ok {
		if w.TxHash.String == "" {
			// Claimed but the send result was not recorded (crash/timeout): never
			// resend automatically; ops must reconcile on chain.
			log.Printf("withdrawal %d: processing without tx hash; manual reconciliation required", w.ID)
			return false
		}
		c, failed, err := tc.TxConfirmations(ctx, w.Network, w.TxHash.String)
		if err != nil {
			return false
		}
		if failed {
			// Reverted on chain: nothing left the hot wallet, so release the funds.
			log.Printf("withdrawal %d: tx %s failed on chain; releasing funds", w.ID, w.TxHash.String)
			s.failAndRelease(ctx, w, "tx_failed")
			return false
		}
		confs = c
	} else {
		bh, err := s.store.BroadcastHeight(ctx, w.ID)
		if err != nil {
			return false
		}
		if confs, err = s.chain.Confirmations(ctx, w.Network, bh); err != nil {
			return false
		}
	}
	if confs < int64(net.Confirmations) {
		return false // not confirmed yet
	}
	if err := s.asset.WithdrawComplete(ctx, w.UserID, w.Asset, sendAmount(w), w.GasFee, s.cfg.FeeAccount, withdrawRef(w.ID)); err != nil {
		log.Printf("withdraw-complete %d: %v", w.ID, err)
		return false // retried next pass (idempotent on ref)
	}
	applied, err := s.store.MarkCompleted(ctx, w.ID)
	if err != nil {
		log.Printf("mark completed %d: %v", w.ID, err)
		return false
	}
	if applied {
		common.Notify(common.Notice{UID: w.UserID, Kind: common.NoticeTrade, Title: "提现成功",
			Body: "您的 " + sendAmount(w).String() + " " + w.Asset + "（" + w.Network + "）提现已完成，到账地址 " + w.Address + "。"})
	}
	return applied
}

// failAndRelease fails a withdrawal and releases its frozen funds.
func (s *Server) failAndRelease(ctx context.Context, w *Withdrawal, reason string) {
	applied, err := s.store.MarkFailed(ctx, w.ID, reason)
	if err != nil || !applied {
		return
	}
	if err := s.asset.Unfreeze(ctx, w.UserID, w.Asset, w.FrozenAmount, withdrawRef(w.ID)); err != nil {
		log.Printf("fail %d: unfreeze: %v", w.ID, err)
	}
}
