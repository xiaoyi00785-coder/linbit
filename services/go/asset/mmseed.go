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
	ok, why := common.TestnetSeedAllowed()
	if !ok {
		log.Printf("!!! REFUSED mm_testnet_seed user=%s asset=%s amount=%s: %s", userID, asset, amount, why)
		return ErrSeedDisabled
	}
	log.Printf("!!! VIRTUAL (UNBACKED) mm_testnet_seed user=%s asset=%s amount=%s ref=%s — %s; excluded from solvency", userID, asset, amount, ref, why)
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
	// ConvertPosition: net system:convert position (platform, not user funds).
	// Negative = the platform paid this asset out via convert and must cover it.
	ConvertPosition string `json:"convertPosition"`
	// HedgeBalance: balance of the convert hedge account (system:hedge,
	// platform-owned, excluded from Total). NetPosition = ConvertPosition +
	// HedgeBalance is the platform's real convert exposure.
	HedgeBalance string `json:"hedgeBalance"`
	NetPosition  string `json:"netPosition"`
}

// Liabilities sums every account-holder balance per asset (platform system:*
// accounts excluded; funds moved to futures are counted by the futures service).
func (s *Store) Liabilities(ctx context.Context, mmUser, hedgeUser string) ([]Liability, error) {
	rows, err := s.db.QueryContext(ctx, `
		SELECT b.asset,
		       COALESCE(SUM(b.free + b.locked), 0)::text,
		       COALESCE(SUM(CASE WHEN b.user_id = $1 THEN b.free + b.locked ELSE 0 END), 0)::text,
		       COALESCE((SELECT SUM(o.amount) FROM wallet_ops o WHERE o.op = 'mm_testnet_seed' AND o.asset = b.asset), 0)::text
		FROM (SELECT user_id, asset, free, locked FROM balances UNION ALL SELECT user_id, asset, free, locked FROM funding_balances) b WHERE b.user_id NOT LIKE 'system:%' GROUP BY b.asset ORDER BY b.asset`, mmUser)
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
		l.ConvertPosition, l.HedgeBalance, l.NetPosition = "0", "0", "0"
		out = append(out, l)
	}
	if err := rows.Err(); err != nil {
		return nil, err
	}
	pos, err := s.ConvertPosition(ctx)
	if err != nil {
		return nil, err
	}
	hedge, err := s.accountTotals(ctx, hedgeUser)
	if err != nil {
		return nil, err
	}
	line := func(a string) *Liability {
		for i := range out {
			if out[i].Asset == a {
				return &out[i]
			}
		}
		out = append(out, Liability{Asset: a, Total: "0", MarketMaker: "0", VirtualSeed: "0", ConvertPosition: "0", HedgeBalance: "0", NetPosition: "0"})
		return &out[len(out)-1]
	}
	for a, v := range pos {
		line(a).ConvertPosition = v.String()
	}
	for a, v := range hedge {
		if v.Sign() != 0 || pos[a].Sign() != 0 {
			line(a).HedgeBalance = v.String()
		}
	}
	for i := range out {
		out[i].NetPosition = pos[out[i].Asset].Add(hedge[out[i].Asset]).String()
	}
	return out, nil
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
	items, err := s.store.Liabilities(r.Context(), mmUserID(), s.cfg.Convert.HedgeUserID)
	if err != nil {
		log.Printf("liabilities: %v", err)
		common.WriteError(w, http.StatusInternalServerError, "INTERNAL", "服务器内部错误")
		return
	}
	common.WriteJSON(w, http.StatusOK, map[string]any{"mmUserId": mmUserID(), "hedgeUserId": s.cfg.Convert.HedgeUserID, "items": items})
}
