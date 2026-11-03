package main

import (
	"context"
	"database/sql"
	"errors"
	"log"
	"net/http"
	"regexp"
	"strconv"
	"strings"
	"time"

	"github.com/linbit/platform/services/go/common"
	"github.com/shopspring/decimal"
)

// transfer.go: general account transfer (gateway routes /assets/transfer and
// /assets/transfers here; the rest of /assets stays on the asset service).
//
// Accounts: spot (asset service free balance) and futures (USDT margin wallet).
// earn is NOT a transfer account: flexible savings are per-product holdings
// (e_holdings) with their own subscribe/redeem rules and interest accrual,
// not a fungible wallet, so it answers ACCOUNT_NOT_SUPPORTED.
//
// It reuses the futures transfer saga (TransferIdem): each leg is atomic in
// its own database and idempotent on the transfer ref; the cross-service leg
// is retried by RunTransferRetry until done. Ledger: asset service journal
// (futures_in / futures_out vs system:futures) + fut_bills (transfer_in/out).

var (
	transferAccounts = map[string]bool{"funding": true, "spot": true, "futures": true}
	knownAccounts    = map[string]bool{"spot": true, "futures": true, "earn": true, "funding": true, "margin": true, "copy": true}
	clientIDRe       = regexp.MustCompile(`^[A-Za-z0-9_\-:.]{1,64}$`)
)

type transferReq struct {
	Asset    string `json:"asset"`
	Amount   string `json:"amount"`
	From     string `json:"from"`
	To       string `json:"to"`
	ClientID string `json:"clientId"`
}

// validateTransfer checks the request and returns the direction: "in" =
// spot|funding -> futures, "out" = futures -> spot|funding, "internal" =
// spot <-> funding. Pure; unit tested.
func validateTransfer(req *transferReq, minAmt decimal.Decimal) (dir string, amt decimal.Decimal, err error) {
	req.Asset = strings.ToUpper(strings.TrimSpace(req.Asset))
	req.From = strings.ToLower(strings.TrimSpace(req.From))
	req.To = strings.ToLower(strings.TrimSpace(req.To))
	req.ClientID = strings.TrimSpace(req.ClientID)
	if req.Asset == "" || req.From == "" || req.To == "" {
		return "", amt, bad("VALIDATION_ERROR", "asset、from、to 必填")
	}
	if !clientIDRe.MatchString(req.ClientID) {
		return "", amt, bad("VALIDATION_ERROR", "clientId 必填，1-64 位字母数字或 _-:.")
	}
	amt, perr := decimal.NewFromString(strings.TrimSpace(req.Amount))
	if perr != nil || amt.Sign() <= 0 || amt.Exponent() < -8 || ((req.From == "futures" || req.To == "futures") && amt.LessThan(minAmt)) {
		return "", amt, bad("VALIDATION_ERROR", "金额不合法（最小 "+minAmt.String()+"，最多 8 位小数）")
	}
	if req.From == req.To {
		return "", amt, bad("SAME_ACCOUNT", "转出和转入账户不能相同")
	}
	for _, a := range []string{req.From, req.To} {
		if !transferAccounts[a] {
			msg := "不支持的账户: " + a
			if a == "earn" {
				msg = "理财账户请使用申购/赎回，不支持直接划转"
			} else if !knownAccounts[a] {
				msg = "from/to 必须为 funding、spot 或 futures"
			}
			return "", amt, bad("ACCOUNT_NOT_SUPPORTED", msg)
		}
	}
	if req.From != "futures" && req.To != "futures" {
		if !assetRe.MatchString(req.Asset) {
			return "", amt, bad("VALIDATION_ERROR", "asset 不合法")
		}
		return "internal", amt, nil
	}
	if req.Asset != "USDT" {
		return "", amt, bad("VALIDATION_ERROR", "合约账户仅支持 USDT")
	}
	if req.To == "futures" {
		return "in", amt, nil
	}
	return "out", amt, nil
}

var assetRe = regexp.MustCompile(`^[A-Z0-9]{2,15}$`)

// walletOf is the asset-service wallet on the non-futures side of a request.
func walletOf(req *transferReq) string {
	if req.From == "funding" || req.To == "funding" {
		return "funding"
	}
	return "spot"
}

// transferByClientID looks up an earlier transfer with the same clientId.
func (s *Server) transferByClientID(ctx context.Context, q queryer, uid, clientID, dir string, amount decimal.Decimal) (string, string, bool, error) {
	return s.transferByClientIDW(ctx, q, uid, clientID, dir, amount, "")
}

// transferByClientIDW also requires the same wallet ("" = don't care).
func (s *Server) transferByClientIDW(ctx context.Context, q queryer, uid, clientID, dir string, amount decimal.Decimal, wallet string) (string, string, bool, error) {
	var ref, d, st, wl string
	var amt decimal.Decimal
	err := q.QueryRowContext(ctx, `SELECT ref, direction, amount, status, wallet FROM fut_transfers WHERE user_id=$1 AND client_id=$2`, uid, clientID).Scan(&ref, &d, &amt, &st, &wl)
	if errors.Is(err, sql.ErrNoRows) {
		return "", "", false, nil
	}
	if err != nil {
		return "", "", false, err
	}
	if d != dir || !amt.Equal(amount) || (wallet != "" && wl != wallet) {
		return "", "", true, &apiError{http.StatusConflict, "IDEMPOTENCY_CONFLICT", "clientId 已用于另一笔不同的划转"}
	}
	return ref, st, true, nil
}

func accountsOf(dir string) (string, string) {
	return accountsOfRow(dir, "spot", "", "")
}

func accountsOfRow(dir, wallet, from, to string) (string, string) {
	if dir == "internal" {
		return from, to
	}
	if wallet == "" {
		wallet = "spot"
	}
	if dir == "in" {
		return wallet, "futures"
	}
	return "futures", wallet
}

// POST /assets/transfer {asset, amount, from, to, clientId}
func (s *Server) hAssetTransfer(w http.ResponseWriter, r *http.Request, uid string) {
	var req transferReq
	if !decode(w, r, &req) {
		return
	}
	dir, amt, err := validateTransfer(&req, s.cfg.MinTransfer)
	if err != nil {
		writeErr(w, err)
		return
	}
	wallet := walletOf(&req)
	var ref, status string
	if dir == "internal" {
		ref, status, err = s.InternalTransfer(r.Context(), uid, req.Asset, req.From, req.To, amt, req.ClientID)
	} else {
		ref, status, err = s.TransferWallet(r.Context(), uid, dir, wallet, amt, req.ClientID)
	}
	if err != nil {
		if a, ok := asAPI(err); ok && a.Code == "INVALID_AMOUNT" {
			err = bad("VALIDATION_ERROR", a.Message)
		}
		writeErr(w, err)
		return
	}
	if status == "failed" {
		common.WriteError(w, http.StatusBadRequest, "INSUFFICIENT_BALANCE", "转出账户可用余额不足")
		return
	}
	code := http.StatusOK
	if status == "pending" {
		code = http.StatusAccepted
	}
	common.WriteJSON(w, code, map[string]any{"id": ref, "clientId": req.ClientID, "asset": req.Asset, "amount": amt.String(), "from": req.From, "to": req.To, "status": status})
}

// GET /assets/transfers?page&pageSize&asset — unified history (includes
// transfers made through POST /futures/transfer and copy profit share).
func (s *Server) hAssetTransfers(w http.ResponseWriter, r *http.Request, uid string) {
	q := r.URL.Query()
	page, size := 1, 20
	if v := q.Get("page"); v != "" {
		n, err := strconv.Atoi(v)
		if err != nil || n < 1 {
			writeErr(w, bad("VALIDATION_ERROR", "page 必须 ≥ 1"))
			return
		}
		page = n
	}
	if v := q.Get("pageSize"); v != "" {
		n, err := strconv.Atoi(v)
		if err != nil || n < 1 || n > 100 {
			writeErr(w, bad("VALIDATION_ERROR", "pageSize 范围 1-100"))
			return
		}
		size = n
	}
	asset := strings.ToUpper(strings.TrimSpace(q.Get("asset")))
	var total int
	if err := s.db.QueryRowContext(r.Context(), `SELECT count(*) FROM fut_transfers WHERE user_id=$1 AND ($2='' OR asset=$2)`, uid, asset).Scan(&total); err != nil {
		writeErr(w, err)
		return
	}
	rows, err := s.db.QueryContext(r.Context(), `SELECT ref, COALESCE(client_id,''), asset, direction, amount, status, kind, created_at, updated_at,
		wallet, COALESCE(from_account,''), COALESCE(to_account,'')
		FROM fut_transfers WHERE user_id=$1 AND ($2='' OR asset=$2) ORDER BY created_at DESC, ref DESC LIMIT $3 OFFSET $4`, uid, asset, size, (page-1)*size)
	if err != nil {
		writeErr(w, err)
		return
	}
	defer rows.Close()
	items := []map[string]any{}
	for rows.Next() {
		var ref, cid, as, dir, st, kind, wl, fa, ta string
		var amt decimal.Decimal
		var at, up time.Time
		if err := rows.Scan(&ref, &cid, &as, &dir, &amt, &st, &kind, &at, &up, &wl, &fa, &ta); err != nil {
			writeErr(w, err)
			return
		}
		from, to := accountsOfRow(dir, wl, fa, ta)
		var cidv any
		if cid != "" {
			cidv = cid
		}
		items = append(items, map[string]any{"id": ref, "clientId": cidv, "asset": as, "amount": amt.String(), "from": from, "to": to,
			"status": st, "kind": kind, "createdAt": at.UTC(), "updatedAt": up.UTC()})
	}
	common.WriteJSON(w, http.StatusOK, map[string]any{"items": items, "page": page, "pageSize": size, "total": total})
}

// InternalTransfer moves any asset between the user's spot and funding wallets
// (asset service only; futures is not involved). Idempotent on clientId; a
// network failure leaves the row pending for RunTransferRetry.
func (s *Server) InternalTransfer(ctx context.Context, uid, asset, from, to string, amount decimal.Decimal, clientID string) (string, string, error) {
	if amount.Sign() <= 0 || amount.Exponent() < -8 {
		return "", "", bad("VALIDATION_ERROR", "划转金额不合法")
	}
	var cid any
	if clientID != "" {
		cid = clientID
		if ref, st, found, err := s.internalByClientID(ctx, uid, clientID, asset, from, to, amount); err != nil || found {
			return ref, st, err
		}
	}
	ref := newRef("acx")
	res, err := s.db.ExecContext(ctx, `INSERT INTO fut_transfers (ref, user_id, direction, amount, status, client_id, asset, kind, from_account, to_account)
		VALUES ($1,$2,'internal',$3,'pending',$4,$5,'account_transfer',$6,$7)
		ON CONFLICT (user_id, client_id) WHERE client_id IS NOT NULL DO NOTHING`, ref, uid, amount, cid, asset, from, to)
	if err != nil {
		return "", "", err
	}
	if n, _ := res.RowsAffected(); n == 0 {
		r, st, _, err := s.internalByClientID(ctx, uid, clientID, asset, from, to, amount)
		return r, st, err
	}
	return ref, s.completeInternal(ctx, ref, uid, asset, from, to, amount), nil
}

func (s *Server) internalByClientID(ctx context.Context, uid, clientID, asset, from, to string, amount decimal.Decimal) (string, string, bool, error) {
	var ref, d, st, as, fa, ta string
	var amt decimal.Decimal
	err := s.db.QueryRowContext(ctx, `SELECT ref, direction, amount, status, asset, COALESCE(from_account,''), COALESCE(to_account,'')
		FROM fut_transfers WHERE user_id=$1 AND client_id=$2`, uid, clientID).Scan(&ref, &d, &amt, &st, &as, &fa, &ta)
	if errors.Is(err, sql.ErrNoRows) {
		return "", "", false, nil
	}
	if err != nil {
		return "", "", false, err
	}
	if d != "internal" || as != asset || fa != from || ta != to || !amt.Equal(amount) {
		return "", "", true, &apiError{http.StatusConflict, "IDEMPOTENCY_CONFLICT", "clientId 已用于另一笔不同的划转"}
	}
	return ref, st, true, nil
}

func (s *Server) completeInternal(ctx context.Context, ref, uid, asset, from, to string, amount decimal.Decimal) string {
	err := s.asset.AccountTransfer(ctx, uid, asset, from, to, amount, ref)
	if err == ErrSpotInsufficient {
		s.markTransfer(ctx, ref, "failed", from+" balance insufficient")
		return "failed"
	}
	if err != nil {
		log.Printf("futures: account transfer %s pending: %v", ref, err)
		s.markTransfer(ctx, ref, "pending", err.Error())
		return "pending"
	}
	_, _ = s.db.ExecContext(ctx, `UPDATE fut_transfers SET status='done', attempts=attempts+1, updated_at=now() WHERE ref=$1 AND status='pending'`, ref)
	return "done"
}
