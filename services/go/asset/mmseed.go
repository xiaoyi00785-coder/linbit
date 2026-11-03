package main

import (
	"context"
	"errors"
	"log"
	"net/http"
	"strings"

	"github.com/linbit/platform/services/go/common"
	"github.com/shopspring/decimal"
)

// mmseed.go: market-maker test funding and solvency figures.
//
// mm_testnet_seed credits the market-maker account with VIRTUAL funds. It is
// only possible when common.TestnetSeedAllowed() (not production, and
// WALLET_CHAIN=mock|testnet); otherwise the endpoint refuses and the market
// maker can only use assets really deposited to it. Only MM_USER_ID can be
// seeded, at most MM_SEED_MAX per call, idempotent on ref. The counterparty
// is system:mm_testnet_virtual so the virtual amount is visible in the ledger
// and excluded from the backed-liabilities figure.

// MMSeedAccount is the ledger counterparty for virtual seed funds.
const MMSeedAccount = "system:mm_testnet_virtual"

// ErrSeedDisabled is returned when the virtual seed is not allowed here.
var ErrSeedDisabled = errors.New("mm_testnet_seed disabled")

// MMTestnetSeed credits amount of asset to userID's free balance as a virtual
// testnet seed. Idempotent on ref.
func (s *Store) MMTestnetSeed(ctx context.Context, userID, asset string, amount decimal.Decimal, ref string) error {
	if ok, _ := common.TestnetSeedAllowed(); !ok {
		return ErrSeedDisabled
	}
	if amount.Sign() <= 0 || ref == "" {
		return ErrInvalidAmount
	}
	tx, err := s.db.BeginTx(ctx, nil)
	if err != nil {
		return err
	}
	defer tx.Rollback() //nolint:errcheck
	if err := claimRef(ctx, tx, ref, "mm_testnet_seed", userID, asset, amount); err != nil {
		return err
	}
	if err := s.applyDeltas(ctx, tx, []balanceDelta{{userID: userID, asset: asset, free: amount}}); err != nil {
		return err
	}
	lines := []ledgerLine{
		{Account: userAcct(userID, "free"), Asset: asset, Direction: "debit", Amount: amount},
		{Account: MMSeedAccount, Asset: asset, Direction: "credit", Amount: amount},
	}
	if err := s.writeJournal(ctx, tx, userID, "mm_testnet_seed", ref, lines); err != nil {
		return err
	}
	return tx.Commit()
}

// Liability is the per-asset total owed to account holders.
type Liability struct {
	Asset       string `json:"asset"`
	Total       string `json:"total"`       // all balances (free+locked), every account
	MarketMaker string `json:"marketMaker"` // part held by MM_USER_ID
	VirtualSeed string `json:"virtualSeed"` // mm_testnet_seed credited (unbacked by design)
}

// Liabilities sums every account-holder balance per asset (platform system:*
// accounts excluded; funds moved to futures are counted by the futures service).
func (s *Store) Liabilities(ctx context.Context, mmUser string) ([]Liability, error) {
	rows, err := s.db.QueryContext(ctx, `
		SELECT b.asset,
		       COALESCE(SUM(b.free + b.locked), 0)::text,
		       COALESCE(SUM(CASE WHEN b.user_id = $1 THEN b.free + b.locked ELSE 0 END), 0)::text,
		       COALESCE((SELECT SUM(o.amount) FROM wallet_ops o WHERE o.op = 'mm_testnet_seed' AND o.asset = b.asset), 0)::text
		FROM balances b WHERE b.user_id NOT LIKE 'system:%' GROUP BY b.asset ORDER BY b.asset`, mmUser)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	out := []Liability{}
	for rows.Next() {
		var l Liability
		if err := rows.Scan(&l.Asset, &l.Total, &l.MarketMaker, &l.VirtualSeed); err != nil {
			return nil, err
		}
		for _, p := range []*string{&l.Total, &l.MarketMaker, &l.VirtualSeed} {
			*p = decimal.RequireFromString(*p).String()
		}
		out = append(out, l)
	}
	return out, rows.Err()
}

func mmUserID() string { return common.Env("MM_USER_ID", "mm:main") }

// POST /internal/mm/testnet-seed {userId, asset, amount, ref}
func (s *Server) handleMMSeed(w http.ResponseWriter, r *http.Request) {
	if ok, why := common.TestnetSeedAllowed(); !ok {
		common.WriteError(w, http.StatusForbidden, "SEED_DISABLED", "生产环境/主网禁止虚拟注资（"+why+"）")
		return
	}
	var req walletMovementRequest
	if err := common.DecodeJSON(r, &req); err != nil || req.UserID == "" || req.Asset == "" || strings.TrimSpace(req.Ref) == "" {
		common.WriteError(w, http.StatusBadRequest, "VALIDATION_ERROR", "请求参数不合法")
		return
	}
	if req.UserID != mmUserID() {
		common.WriteError(w, http.StatusForbidden, "NOT_MM_ACCOUNT", "只能为做市账户注资")
		return
	}
	amount, err := decimal.NewFromString(req.Amount)
	maxSeed := decimal.RequireFromString(common.Env("MM_SEED_MAX", "10000000"))
	if err != nil || amount.Sign() <= 0 || amount.GreaterThan(maxSeed) {
		common.WriteError(w, http.StatusBadRequest, "VALIDATION_ERROR", "金额不合法")
		return
	}
	err = s.store.MMTestnetSeed(r.Context(), req.UserID, strings.ToUpper(req.Asset), amount, req.Ref)
	switch {
	case errors.Is(err, ErrAlreadyApplied):
		common.WriteJSON(w, http.StatusOK, map[string]string{"status": "already_applied"})
	case errors.Is(err, ErrSeedDisabled):
		common.WriteError(w, http.StatusForbidden, "SEED_DISABLED", "生产环境/主网禁止虚拟注资")
	case err != nil:
		log.Printf("mm seed: %v", err)
		common.WriteError(w, http.StatusInternalServerError, "INTERNAL", "服务器内部错误")
	default:
		log.Printf("mm_testnet_seed: %s %s to %s ref=%s", amount, req.Asset, req.UserID, req.Ref)
		common.WriteJSON(w, http.StatusOK, map[string]string{"status": "ok", "txType": "mm_testnet_seed"})
	}
}

// GET /internal/solvency/liabilities
func (s *Server) handleLiabilities(w http.ResponseWriter, r *http.Request) {
	items, err := s.store.Liabilities(r.Context(), mmUserID())
	if err != nil {
		log.Printf("liabilities: %v", err)
		common.WriteError(w, http.StatusInternalServerError, "INTERNAL", "服务器内部错误")
		return
	}
	common.WriteJSON(w, http.StatusOK, map[string]any{"mmUserId": mmUserID(), "items": items})
}
