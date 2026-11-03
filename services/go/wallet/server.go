package main

import (
	"log"
	"net/http"
	"strings"
	"time"

	"github.com/linbit/platform/services/go/common"
	"github.com/shopspring/decimal"
)

// Server wires configuration, storage and the pluggable chain/signer/risk layers
// into the wallet HTTP handlers.
//
// /wallet/* are user-facing (the gateway injects X-User-Id after validating the
// JWT). /internal/* are service-to-service / admin and require the shared
// X-Internal-Token; the gateway never proxies them. Dev-only mock-chain endpoints
// additionally require DEV_MODE.
type Server struct {
	cfg        Config
	store      *Store
	chain      ChainAdapter
	mock       *MockChain // same instance as chain; exposes dev-only injection
	hotSigner  Signer
	coldSigner *MultisigSigner
	risk       RiskScorer
	asset      *AssetClient
	account    *AccountClient
	market     *MarketClient
	scanner    *Scanner
	real       *RealChain // non-nil when WALLET_CHAIN=testnet|mainnet
}

// NewServer constructs a Server with the mock chain, mock signers and local risk
// scorer wired in.
func NewServer(cfg Config, store *Store) *Server {
	mock := NewMockChain(store)
	asset := NewAssetClient(cfg.AssetInternalURL, cfg.InternalToken)
	return &Server{
		cfg:        cfg,
		store:      store,
		chain:      mock,
		mock:       mock,
		hotSigner:  NewHotMPCSigner(common.Env("WALLET_HOT_SECRET", "")),
		coldSigner: NewColdMultisigSigner(nil),
		risk:       NewLocalRiskScorer(),
		asset:      asset,
		account:    NewAccountClient(cfg.AccountInternalURL, cfg.InternalToken),
		market:     NewMarketClient(cfg.MarketURL),
		scanner:    NewScanner(store, cfg.Networks, asset, cfg.CreditBelowMinDeposit),
	}
}

// Routes returns the wallet service HTTP handler.
func (s *Server) Routes() http.Handler {
	mux := http.NewServeMux()
	mux.HandleFunc("GET /healthz", func(w http.ResponseWriter, r *http.Request) { w.Write([]byte("ok")) })

	// User-facing (routed via the gateway under /wallet/*).
	mux.HandleFunc("GET /wallet/deposit/address", s.handleDepositAddress)
	mux.HandleFunc("GET /wallet/deposit/records", s.handleDepositRecords)
	mux.HandleFunc("GET /wallet/withdraw/networks", s.handleWithdrawNetworks)
	mux.HandleFunc("GET /wallet/networks", s.handleNetworks)
	mux.HandleFunc("POST /wallet/internal-transfer", s.handleInternalTransfer)
	mux.HandleFunc("GET /wallet/internal-transfers", s.handleInternalTransferList)
	mux.HandleFunc("GET /wallet/withdraw/records", s.handleWithdrawRecords)
	mux.HandleFunc("POST /wallet/withdraw", s.handleCreateWithdrawal)
	mux.HandleFunc("POST /wallet/withdraw/{id}/cancel", s.handleCancelWithdrawal)

	// Internal / admin.
	mux.HandleFunc("POST /internal/scan", s.internal(s.handleScan))
	mux.HandleFunc("POST /internal/process", s.internal(s.handleProcess))
	mux.HandleFunc("GET /internal/solvency/holdings", s.internal(s.handleHoldings))
	mux.HandleFunc("GET /internal/admin/withdrawals", s.internal(s.handleAdminList))
	mux.HandleFunc("POST /internal/admin/withdrawals/{id}/approve", s.internal(s.handleAdminApprove))
	mux.HandleFunc("POST /internal/admin/withdrawals/{id}/reject", s.internal(s.handleAdminReject))
	mux.HandleFunc("GET /internal/admin/wallet/networks", s.internal(s.handleAdminNetworks))
	mux.HandleFunc("PUT /internal/admin/wallet/networks/{asset}/{network}", s.internal(s.handleAdminPutNetwork))
	mux.HandleFunc("GET /internal/admin/internal-transfer/config", s.internal(s.handleAdminITConfig))
	mux.HandleFunc("PUT /internal/admin/internal-transfer/config", s.internal(s.handleAdminPutITConfig))
	mux.HandleFunc("GET /internal/admin/internal-transfers", s.internal(s.handleAdminITList))

	// Dev-only simulated-chain control.
	if s.real != nil {
		mux.HandleFunc("POST /internal/chain/sync", s.internal(s.handleChainSync))
		mux.HandleFunc("POST /internal/chain/sweep", s.internal(s.handleChainSweep))
		mux.HandleFunc("GET /internal/admin/wallet/hot", s.internal(s.handleHotWallets))
		mux.HandleFunc("GET /internal/admin/wallet/sweeps", s.internal(s.handleSweeps))
	}
	if s.cfg.DevMode && s.mock != nil {
		mux.HandleFunc("POST /internal/dev/chain/deposit", s.internal(s.handleDevInjectDeposit))
		mux.HandleFunc("POST /internal/dev/chain/advance", s.internal(s.handleDevAdvance))
	}
	return mux
}

// internal guards service-to-service endpoints with the shared token.
func (s *Server) internal(next http.HandlerFunc) http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		if s.cfg.InternalToken == "" || r.Header.Get("X-Internal-Token") != s.cfg.InternalToken {
			common.WriteError(w, http.StatusUnauthorized, "AUTH_UNAUTHORIZED", "内部令牌无效")
			return
		}
		next(w, r)
	}
}

func requireUser(w http.ResponseWriter, r *http.Request) (string, bool) {
	uid := r.Header.Get("X-User-Id")
	if uid == "" {
		common.WriteError(w, http.StatusUnauthorized, "AUTH_UNAUTHORIZED", "请先登录")
		return "", false
	}
	return uid, true
}

func fmt8(d decimal.Decimal) string { return d.StringFixed(8) }

func strptr(s string) *string {
	if s == "" {
		return nil
	}
	return &s
}

func intptr(n int) *int { return &n }

// ---- DTOs ----

type depositAddressDTO struct {
	Asset      string  `json:"asset"`
	Network    string  `json:"network"`
	Address    string  `json:"address"`
	Memo       *string `json:"memo"`
	MinDeposit *string `json:"minDeposit"`
	// Additive fields (backward compatible).
	MemoRequired  bool `json:"memoRequired"`
	Confirmations *int `json:"confirmations,omitempty"`
}

type depositDTO struct {
	ID            string  `json:"id"`
	Asset         string  `json:"asset"`
	Network       string  `json:"network"`
	Amount        string  `json:"amount"`
	TxID          *string `json:"txId"`
	Status        string  `json:"status"`
	Confirmations *int    `json:"confirmations"`
	CreatedAt     string  `json:"createdAt"`
}

type paginated struct {
	Items    interface{} `json:"items"`
	Page     int         `json:"page"`
	PageSize int         `json:"pageSize"`
	Total    int         `json:"total"`
}

type withdrawNetworkDTO struct {
	Network                 string `json:"network"`
	MinWithdraw             string `json:"minWithdraw,omitempty"`
	MaxWithdraw             string `json:"maxWithdraw,omitempty"`
	GasFeeEstimate          string `json:"gasFeeEstimate,omitempty"`
	GasFeeAsset             string `json:"gasFeeAsset,omitempty"`
	Confirmations           *int   `json:"confirmations,omitempty"`
	EstimatedArrivalMinutes *int   `json:"estimatedArrivalMinutes,omitempty"`
	WithdrawFee             string `json:"withdrawFee,omitempty"`
	MemoRequired            bool   `json:"memoRequired"`
	WithdrawEnabled         bool   `json:"withdrawEnabled"`
}

type withdrawNetworkOptionsDTO struct {
	Asset    string               `json:"asset"`
	Networks []withdrawNetworkDTO `json:"networks"`
}

type withdrawalDTO struct {
	ID          string  `json:"id"`
	Asset       string  `json:"asset"`
	Network     string  `json:"network"`
	Address     string  `json:"address"`
	Amount      string  `json:"amount"`
	Fee         string  `json:"fee"`
	GasFee      *string `json:"gasFee"`
	GasFeeAsset *string `json:"gasFeeAsset"`
	Status      string  `json:"status"`
	CreatedAt   string  `json:"createdAt"`
}

func depositToDTO(d Deposit) depositDTO {
	confs := int(d.Confirmations)
	status := "pending"
	if d.Status == depCompleted {
		status = "completed"
	}
	return depositDTO{
		ID:            "dep_" + itoa64(d.ID),
		Asset:         d.Asset,
		Network:       d.Network,
		Amount:        fmt8(d.Amount),
		TxID:          strptr(d.TxHash),
		Status:        status,
		Confirmations: &confs,
		CreatedAt:     d.CreatedAt.UTC().Format(time.RFC3339),
	}
}

func withdrawalToDTO(w Withdrawal) withdrawalDTO {
	gas := fmt8(w.GasFee)
	return withdrawalDTO{
		ID:          "wd_" + itoa64(w.ID),
		Asset:       w.Asset,
		Network:     w.Network,
		Address:     w.Address,
		Amount:      fmt8(w.Amount),
		Fee:         gas,
		GasFee:      &gas,
		GasFeeAsset: strptr(w.GasFeeAsset),
		Status:      apiStatus(w.Status),
		CreatedAt:   w.CreatedAt.UTC().Format(time.RFC3339),
	}
}

func itoa64(n int64) string { return itoa(int(n)) }

// ---- user-facing handlers ----

func (s *Server) handleDepositAddress(w http.ResponseWriter, r *http.Request) {
	uid, ok := requireUser(w, r)
	if !ok {
		return
	}
	asset := strings.ToUpper(strings.TrimSpace(r.URL.Query().Get("asset")))
	network := strings.ToUpper(strings.TrimSpace(r.URL.Query().Get("network")))
	if asset == "" || network == "" {
		common.WriteError(w, http.StatusBadRequest, "VALIDATION_ERROR", "缺少 asset/network")
		return
	}
	net, found := s.cfg.Networks.Lookup(asset, network)
	if !found {
		common.WriteError(w, http.StatusBadRequest, "UNSUPPORTED_NETWORK", "不支持的充值网络")
		return
	}
	if net.DepositDisabled {
		common.WriteError(w, http.StatusBadRequest, "DEPOSIT_DISABLED", "该网络暂停充值")
		return
	}
	da, err := s.store.UpsertDepositAddress(r.Context(), uid, asset, network, func() (string, string, *int64, error) {
		if a, ok := s.chain.(AddressAllocator); ok {
			return a.AllocateAddress(r.Context(), net)
		}
		addr, memo := s.chain.NewAddress(net, uid)
		return addr, memo, nil, nil
	})
	if err != nil {
		log.Printf("deposit address: %v", err)
		common.WriteError(w, http.StatusInternalServerError, "INTERNAL", "服务器内部错误")
		return
	}
	if net.MemoRequired && da.Memo == "" {
		da.Memo = userMemo(uid)
		if err := s.store.ensureMemo(r.Context(), uid, asset, network, da.Memo); err != nil {
			log.Printf("deposit memo: %v", err)
			common.WriteError(w, http.StatusInternalServerError, "INTERNAL", "服务器内部错误")
			return
		}
	}
	common.WriteJSON(w, http.StatusOK, depositAddressDTO{
		Asset:         da.Asset,
		Network:       da.Network,
		Address:       da.Address,
		Memo:          strptr(da.Memo),
		MinDeposit:    strptr(fmt8(net.MinDeposit)),
		MemoRequired:  net.MemoRequired,
		Confirmations: intptr(net.Confirmations),
	})
}

func (s *Server) handleDepositRecords(w http.ResponseWriter, r *http.Request) {
	uid, ok := requireUser(w, r)
	if !ok {
		return
	}
	asset := strings.ToUpper(strings.TrimSpace(r.URL.Query().Get("asset")))
	page := atoiDefault(r.URL.Query().Get("page"), 1, 1, 1<<30)
	pageSize := atoiDefault(r.URL.Query().Get("pageSize"), 20, 1, 100)
	rows, total, err := s.store.ListDeposits(r.Context(), uid, asset, page, pageSize)
	if err != nil {
		log.Printf("deposit records: %v", err)
		common.WriteError(w, http.StatusInternalServerError, "INTERNAL", "服务器内部错误")
		return
	}
	items := make([]depositDTO, 0, len(rows))
	for _, d := range rows {
		items = append(items, depositToDTO(d))
	}
	common.WriteJSON(w, http.StatusOK, paginated{Items: items, Page: page, PageSize: pageSize, Total: total})
}

func (s *Server) handleWithdrawNetworks(w http.ResponseWriter, r *http.Request) {
	if _, ok := requireUser(w, r); !ok {
		return
	}
	asset := strings.ToUpper(strings.TrimSpace(r.URL.Query().Get("asset")))
	if asset == "" {
		common.WriteError(w, http.StatusBadRequest, "VALIDATION_ERROR", "缺少 asset")
		return
	}
	networks := make([]withdrawNetworkDTO, 0)
	for _, n := range s.cfg.Networks.ForAsset(asset) {
		dto := withdrawNetworkDTO{
			Network:                 n.Network,
			MinWithdraw:             fmt8(n.MinWithdraw),
			GasFeeEstimate:          fmt8(n.GasFeeEstimate),
			GasFeeAsset:             n.GasFeeAsset,
			Confirmations:           intptr(n.Confirmations),
			EstimatedArrivalMinutes: intptr(n.EstimatedArrivalMinutes),
			WithdrawFee:             fmt8(n.GasFeeEstimate),
			MemoRequired:            n.MemoRequired,
			WithdrawEnabled:         !n.WithdrawDisabled,
		}
		if n.MaxWithdraw.Sign() > 0 {
			dto.MaxWithdraw = fmt8(n.MaxWithdraw)
		}
		networks = append(networks, dto)
	}
	common.WriteJSON(w, http.StatusOK, withdrawNetworkOptionsDTO{Asset: asset, Networks: networks})
}

func (s *Server) handleWithdrawRecords(w http.ResponseWriter, r *http.Request) {
	uid, ok := requireUser(w, r)
	if !ok {
		return
	}
	asset := strings.ToUpper(strings.TrimSpace(r.URL.Query().Get("asset")))
	page := atoiDefault(r.URL.Query().Get("page"), 1, 1, 1<<30)
	pageSize := atoiDefault(r.URL.Query().Get("pageSize"), 20, 1, 100)
	rows, total, err := s.store.ListWithdrawals(r.Context(), uid, asset, page, pageSize)
	if err != nil {
		log.Printf("withdraw records: %v", err)
		common.WriteError(w, http.StatusInternalServerError, "INTERNAL", "服务器内部错误")
		return
	}
	items := make([]withdrawalDTO, 0, len(rows))
	for _, wd := range rows {
		items = append(items, withdrawalToDTO(wd))
	}
	common.WriteJSON(w, http.StatusOK, paginated{Items: items, Page: page, PageSize: pageSize, Total: total})
}

type createWithdrawalRequest struct {
	Asset    string `json:"asset"`
	Network  string `json:"network"`
	Address  string `json:"address"`
	Amount   string `json:"amount"`
	Memo     string `json:"memo"`
	TOTPCode string `json:"totpCode"`
	Code     string `json:"code"`
}

func (s *Server) handleCreateWithdrawal(w http.ResponseWriter, r *http.Request) {
	uid, ok := requireUser(w, r)
	if !ok {
		return
	}
	var req createWithdrawalRequest
	if err := common.DecodeJSON(r, &req); err != nil {
		common.WriteError(w, http.StatusBadRequest, "VALIDATION_ERROR", "请求参数不合法")
		return
	}
	req.Asset = strings.ToUpper(strings.TrimSpace(req.Asset))
	req.Network = strings.ToUpper(strings.TrimSpace(req.Network))
	req.Address = strings.TrimSpace(req.Address)
	if req.Asset == "" || req.Network == "" || req.Address == "" {
		common.WriteError(w, http.StatusBadRequest, "VALIDATION_ERROR", "asset/network/address 必填")
		return
	}
	amount, err := decimal.NewFromString(req.Amount)
	if err != nil {
		common.WriteError(w, http.StatusBadRequest, "VALIDATION_ERROR", "金额不合法")
		return
	}
	wd, apiErr := s.submitWithdrawal(r.Context(), uid, withdrawInput{
		Asset: req.Asset, Network: req.Network, Address: req.Address, Memo: strings.TrimSpace(req.Memo),
		Amount: amount, TOTPCode: strings.TrimSpace(req.TOTPCode), Code: strings.TrimSpace(req.Code),
	})
	if apiErr != nil {
		common.WriteError(w, apiErr.status, apiErr.code, apiErr.msg)
		return
	}
	common.WriteJSON(w, http.StatusAccepted, withdrawalToDTO(*wd))
}

func (s *Server) handleCancelWithdrawal(w http.ResponseWriter, r *http.Request) {
	uid, ok := requireUser(w, r)
	if !ok {
		return
	}
	id, valid := parseWithdrawalID(r.PathValue("id"))
	if !valid {
		common.WriteError(w, http.StatusBadRequest, "VALIDATION_ERROR", "无效的提现单号")
		return
	}
	applied, err := s.userCancel(r.Context(), id, uid)
	if err != nil {
		common.WriteError(w, http.StatusInternalServerError, "INTERNAL", "服务器内部错误")
		return
	}
	if !applied {
		common.WriteError(w, http.StatusConflict, "NOT_CANCELABLE", "该提现无法撤销（不存在或已进入处理）")
		return
	}
	wd, _ := s.store.GetWithdrawal(r.Context(), id)
	common.WriteJSON(w, http.StatusOK, withdrawalToDTO(*wd))
}

// ---- internal / admin handlers ----

func (s *Server) handleScan(w http.ResponseWriter, r *http.Request) {
	credited, err := s.scanner.ScanAll(r.Context())
	if err != nil {
		common.WriteError(w, http.StatusInternalServerError, "INTERNAL", err.Error())
		return
	}
	common.WriteJSON(w, http.StatusOK, map[string]int{"credited": credited})
}

func (s *Server) handleProcess(w http.ResponseWriter, r *http.Request) {
	broadcast, completed, err := s.ProcessWithdrawals(r.Context())
	if err != nil {
		common.WriteError(w, http.StatusInternalServerError, "INTERNAL", err.Error())
		return
	}
	common.WriteJSON(w, http.StatusOK, map[string]int{"broadcast": broadcast, "completed": completed})
}

type adminWithdrawalDTO struct {
	ID             string `json:"id"`
	UserID         string `json:"userId"`
	Asset          string `json:"asset"`
	Network        string `json:"network"`
	Address        string `json:"address"`
	Amount         string `json:"amount"`
	GasFee         string `json:"gasFee"`
	FrozenAmount   string `json:"frozenAmount"`
	RiskScore      int    `json:"riskScore"`
	InternalStatus string `json:"internalStatus"`
	Status         string `json:"status"`
	CreatedAt      string `json:"createdAt"`
}

func (s *Server) handleAdminList(w http.ResponseWriter, r *http.Request) {
	status := r.URL.Query().Get("status")
	if status == "" {
		status = wStatusReview
	}
	rows, err := s.store.ListWithdrawalsByStatus(r.Context(), status, 200)
	if err != nil {
		common.WriteError(w, http.StatusInternalServerError, "INTERNAL", "服务器内部错误")
		return
	}
	items := make([]adminWithdrawalDTO, 0, len(rows))
	for _, wd := range rows {
		items = append(items, adminWithdrawalDTO{
			ID: "wd_" + itoa64(wd.ID), UserID: wd.UserID, Asset: wd.Asset, Network: wd.Network,
			Address: wd.Address, Amount: fmt8(wd.Amount), GasFee: fmt8(wd.GasFee),
			FrozenAmount: fmt8(wd.FrozenAmount), RiskScore: wd.RiskScore,
			InternalStatus: wd.Status, Status: apiStatus(wd.Status),
			CreatedAt: wd.CreatedAt.UTC().Format(time.RFC3339),
		})
	}
	common.WriteJSON(w, http.StatusOK, map[string]interface{}{"items": items})
}

func (s *Server) handleAdminApprove(w http.ResponseWriter, r *http.Request) {
	id, valid := parseWithdrawalID(r.PathValue("id"))
	if !valid {
		common.WriteError(w, http.StatusBadRequest, "VALIDATION_ERROR", "无效的提现单号")
		return
	}
	applied, err := s.store.Approve(r.Context(), id)
	if err != nil {
		common.WriteError(w, http.StatusInternalServerError, "INTERNAL", "服务器内部错误")
		return
	}
	if !applied {
		common.WriteError(w, http.StatusConflict, "NOT_PENDING", "该提现不在待审核状态")
		return
	}
	wd, _ := s.store.GetWithdrawal(r.Context(), id)
	common.WriteJSON(w, http.StatusOK, withdrawalToDTO(*wd))
}

type rejectRequest struct {
	Reason string `json:"reason"`
}

func (s *Server) handleAdminReject(w http.ResponseWriter, r *http.Request) {
	id, valid := parseWithdrawalID(r.PathValue("id"))
	if !valid {
		common.WriteError(w, http.StatusBadRequest, "VALIDATION_ERROR", "无效的提现单号")
		return
	}
	var req rejectRequest
	_ = common.DecodeJSON(r, &req)
	reason := strings.TrimSpace(req.Reason)
	if reason == "" {
		reason = "rejected_by_admin"
	}
	applied, err := s.adminReject(r.Context(), id, reason)
	if err != nil {
		common.WriteError(w, http.StatusInternalServerError, "INTERNAL", "服务器内部错误")
		return
	}
	if !applied {
		common.WriteError(w, http.StatusConflict, "NOT_PENDING", "该提现不在待审核状态")
		return
	}
	wd, _ := s.store.GetWithdrawal(r.Context(), id)
	common.WriteJSON(w, http.StatusOK, withdrawalToDTO(*wd))
}

// ---- dev-only simulated-chain handlers ----

type devDepositRequest struct {
	Network  string `json:"network"`
	Address  string `json:"address"`
	Asset    string `json:"asset"`
	Amount   string `json:"amount"`
	TxHash   string `json:"txHash"`
	LogIndex int64  `json:"logIndex"`
}

func (s *Server) handleDevInjectDeposit(w http.ResponseWriter, r *http.Request) {
	var req devDepositRequest
	if err := common.DecodeJSON(r, &req); err != nil {
		common.WriteError(w, http.StatusBadRequest, "VALIDATION_ERROR", "请求参数不合法")
		return
	}
	req.Network = strings.ToUpper(strings.TrimSpace(req.Network))
	req.Asset = strings.ToUpper(strings.TrimSpace(req.Asset))
	amount, err := decimal.NewFromString(req.Amount)
	if err != nil || amount.Sign() <= 0 {
		common.WriteError(w, http.StatusBadRequest, "VALIDATION_ERROR", "金额不合法")
		return
	}
	if req.TxHash == "" {
		req.TxHash = "0x" + shortHash(req.Network+req.Address+req.Amount+time.Now().String())
	}
	injected, err := s.mock.InjectDeposit(r.Context(), req.Network, req.Address, req.Asset, amount, req.TxHash, req.LogIndex)
	if err != nil {
		common.WriteError(w, http.StatusInternalServerError, "INTERNAL", err.Error())
		return
	}
	common.WriteJSON(w, http.StatusOK, map[string]interface{}{"injected": injected, "txHash": req.TxHash})
}

type devAdvanceRequest struct {
	Network string `json:"network"`
	Blocks  int64  `json:"blocks"`
}

func (s *Server) handleDevAdvance(w http.ResponseWriter, r *http.Request) {
	var req devAdvanceRequest
	if err := common.DecodeJSON(r, &req); err != nil {
		common.WriteError(w, http.StatusBadRequest, "VALIDATION_ERROR", "请求参数不合法")
		return
	}
	req.Network = strings.ToUpper(strings.TrimSpace(req.Network))
	if req.Blocks < 1 {
		req.Blocks = 1
	}
	height, err := s.mock.Advance(r.Context(), req.Network, req.Blocks)
	if err != nil {
		common.WriteError(w, http.StatusInternalServerError, "INTERNAL", err.Error())
		return
	}
	common.WriteJSON(w, http.StatusOK, map[string]int64{"height": height})
}
