package main

import (
	"context"
	"errors"
	"log"
	"net/http"
	"strconv"
	"strings"
	"time"

	"github.com/linbit/platform/services/go/common"
	"github.com/shopspring/decimal"
)

// adminops.go holds back-office ledger operations: reading the double-entry
// journal and posting manual marketing rewards (reissues).

// MarketingAccount is the platform account that funds rewards and reissues.
const MarketingAccount = "system:marketing"

// Reward credits a marketing reward to the user's free balance. Counterparty is
// system:marketing so campaign spend is visible in the journal. Idempotent on ref.
func (s *Store) Reward(ctx context.Context, userID, asset string, amount decimal.Decimal, ref string) error {
	if amount.Sign() <= 0 {
		return ErrInvalidAmount
	}
	tx, err := s.db.BeginTx(ctx, nil)
	if err != nil {
		return err
	}
	defer tx.Rollback() //nolint:errcheck
	if err := claimRef(ctx, tx, ref, "reward", userID, asset, amount); err != nil {
		return err
	}
	if err := s.applyDeltas(ctx, tx, []balanceDelta{{userID: userID, asset: asset, free: amount}}); err != nil {
		return err
	}
	lines := []ledgerLine{
		{Account: userAcct(userID, "free"), Asset: asset, Direction: "debit", Amount: amount},
		{Account: MarketingAccount, Asset: asset, Direction: "credit", Amount: amount},
	}
	if err := s.writeJournal(ctx, tx, userID, "reward", ref, lines); err != nil {
		return err
	}
	return tx.Commit()
}

type ledgerEntryDTO struct {
	Account   string `json:"account"`
	Asset     string `json:"asset"`
	Direction string `json:"direction"`
	Amount    string `json:"amount"`
}

type ledgerTxDTO struct {
	ID        int64            `json:"id"`
	UserID    string           `json:"userId"`
	TxType    string           `json:"txType"`
	Ref       *string          `json:"ref"`
	CreatedAt string           `json:"createdAt"`
	Entries   []ledgerEntryDTO `json:"entries"`
}

// LedgerFilter narrows a journal query.
type LedgerFilter struct {
	UserID, Asset, TxType, Ref string
	Page, Size                 int
}

// Ledger lists journal transactions (newest first) with their entries.
func (s *Store) Ledger(ctx context.Context, f LedgerFilter) ([]ledgerTxDTO, int, error) {
	where := []string{"TRUE"}
	var args []interface{}
	add := func(cond string, v interface{}) {
		args = append(args, v)
		where = append(where, strings.ReplaceAll(cond, "?", "$"+strconv.Itoa(len(args))))
	}
	if f.UserID != "" {
		add("t.user_id = ?", f.UserID)
	}
	if f.TxType != "" {
		add("t.tx_type = ?", f.TxType)
	}
	if f.Ref != "" {
		add("t.ref = ?", f.Ref)
	}
	if f.Asset != "" {
		add("EXISTS (SELECT 1 FROM ledger_entries e WHERE e.tx_id = t.id AND e.asset = ?)", strings.ToUpper(f.Asset))
	}
	cond := strings.Join(where, " AND ")
	var total int
	if err := s.db.QueryRowContext(ctx, `SELECT count(*) FROM ledger_transactions t WHERE `+cond, args...).Scan(&total); err != nil {
		return nil, 0, err
	}
	args = append(args, f.Size, (f.Page-1)*f.Size)
	rows, err := s.db.QueryContext(ctx, `SELECT t.id, t.user_id, t.tx_type, t.ref, t.created_at
		FROM ledger_transactions t WHERE `+cond+` ORDER BY t.id DESC
		LIMIT $`+strconv.Itoa(len(args)-1)+` OFFSET $`+strconv.Itoa(len(args)), args...)
	if err != nil {
		return nil, 0, err
	}
	var txs []ledgerTxDTO
	idx := map[int64]int{}
	var ids []int64
	for rows.Next() {
		var t ledgerTxDTO
		var ref *string
		var at time.Time
		if err := rows.Scan(&t.ID, &t.UserID, &t.TxType, &ref, &at); err != nil {
			rows.Close()
			return nil, 0, err
		}
		t.Ref, t.CreatedAt, t.Entries = ref, at.UTC().Format(time.RFC3339), []ledgerEntryDTO{}
		idx[t.ID] = len(txs)
		ids = append(ids, t.ID)
		txs = append(txs, t)
	}
	rows.Close()
	if len(ids) == 0 {
		return []ledgerTxDTO{}, total, nil
	}
	idArgs := make([]string, len(ids))
	eargs := make([]interface{}, len(ids))
	for i, id := range ids {
		idArgs[i] = "$" + strconv.Itoa(i+1)
		eargs[i] = id
	}
	erows, err := s.db.QueryContext(ctx, `SELECT tx_id, account, asset, direction, amount FROM ledger_entries
		WHERE tx_id IN (`+strings.Join(idArgs, ",")+`) ORDER BY id`, eargs...)
	if err != nil {
		return nil, 0, err
	}
	defer erows.Close()
	for erows.Next() {
		var txID int64
		var e ledgerEntryDTO
		var amt decimal.Decimal
		if err := erows.Scan(&txID, &e.Account, &e.Asset, &e.Direction, &amt); err != nil {
			return nil, 0, err
		}
		e.Amount = amt.StringFixed(8)
		txs[idx[txID]].Entries = append(txs[idx[txID]].Entries, e)
	}
	return txs, total, erows.Err()
}

// GET /internal/admin/ledger?userId=&asset=&txType=&ref=&page=&pageSize=
func (s *Server) handleAdminLedger(w http.ResponseWriter, r *http.Request) {
	q := r.URL.Query()
	f := LedgerFilter{UserID: q.Get("userId"), Asset: q.Get("asset"), TxType: q.Get("txType"), Ref: q.Get("ref"),
		Page: atoiDefault(q.Get("page"), 1, 1, 1_000_000), Size: atoiDefault(q.Get("pageSize"), 20, 1, 200)}
	items, total, err := s.store.Ledger(r.Context(), f)
	if err != nil {
		log.Printf("admin ledger: %v", err)
		common.WriteError(w, http.StatusInternalServerError, "INTERNAL", "服务器内部错误")
		return
	}
	common.WriteJSON(w, http.StatusOK, map[string]interface{}{"items": items, "total": total, "page": f.Page, "pageSize": f.Size})
}

// POST /internal/reward {userId, asset, amount, ref}
func (s *Server) handleReward(w http.ResponseWriter, r *http.Request) {
	var req walletMovementRequest
	if err := common.DecodeJSON(r, &req); err != nil || req.UserID == "" || req.Asset == "" || req.Ref == "" {
		common.WriteError(w, http.StatusBadRequest, "VALIDATION_ERROR", "请求参数不合法")
		return
	}
	amount, err := decimal.NewFromString(req.Amount)
	if err != nil || amount.Sign() <= 0 {
		common.WriteError(w, http.StatusBadRequest, "VALIDATION_ERROR", "金额不合法")
		return
	}
	err = s.store.Reward(r.Context(), req.UserID, strings.ToUpper(req.Asset), amount, req.Ref)
	switch {
	case errors.Is(err, ErrAlreadyApplied):
		common.WriteJSON(w, http.StatusOK, map[string]string{"status": "already_applied"})
	case err != nil:
		log.Printf("reward: %v", err)
		common.WriteError(w, http.StatusInternalServerError, "INTERNAL", "服务器内部错误")
	default:
		common.WriteJSON(w, http.StatusOK, map[string]string{"status": "ok"})
	}
}
