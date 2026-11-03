package main

import (
	"context"
	"database/sql"
	"encoding/json"
	"errors"
	"fmt"
	"net/http"
	"strconv"
	"strings"
	"time"

	"github.com/linbit/platform/services/go/common"
	"github.com/shopspring/decimal"
)

// 质押借币. LTV = (principal + unpaid interest) / (collateral × index price).
// Interest accrues per started hour on the outstanding principal (rounded up
// to 8 dp); after the due date of a fixed term the rate is multiplied by the
// overdue multiplier, and a loan overdue for more than OverdueDays, or whose
// LTV reaches the liquidation line, is liquidated with a fee.

const dp = 8

var hour = time.Hour

func ceil8(d decimal.Decimal) decimal.Decimal { return d.RoundCeil(dp) }

type Loan struct {
	ID                int64           `json:"id"`
	UID               string          `json:"-"`
	Asset             string          `json:"asset"`
	CollateralAsset   string          `json:"collateralAsset"`
	Term              string          `json:"term"`
	HourlyRate        decimal.Decimal `json:"hourlyRate"`
	InitialPrincipal  decimal.Decimal `json:"initialPrincipal"`
	InitialCollateral decimal.Decimal `json:"initialCollateral"`
	Principal         decimal.Decimal `json:"principal"`
	Collateral        decimal.Decimal `json:"collateral"`
	Interest          decimal.Decimal `json:"interest"`
	InterestPaid      decimal.Decimal `json:"interestPaid"`
	HoursCharged      int             `json:"hoursCharged"`
	Status            string          `json:"status"`
	WarnedAt          *time.Time      `json:"-"`
	StartedAt         time.Time       `json:"-"`
	DueAt             *time.Time      `json:"-"`
	ClosedAt          *time.Time      `json:"-"`
}

const loanCols = `id, uid, asset, collateral_asset, term, hourly_rate, initial_principal, initial_collateral, principal, collateral,
	interest, interest_paid, hours_charged, status, warned_at, started_at, due_at, closed_at`

func scanLoan(sc interface{ Scan(...any) error }) (*Loan, error) {
	l := &Loan{}
	err := sc.Scan(&l.ID, &l.UID, &l.Asset, &l.CollateralAsset, &l.Term, &l.HourlyRate, &l.InitialPrincipal, &l.InitialCollateral,
		&l.Principal, &l.Collateral, &l.Interest, &l.InterestPaid, &l.HoursCharged, &l.Status, &l.WarnedAt, &l.StartedAt, &l.DueAt, &l.ClosedAt)
	return l, err
}

func (l *Loan) debt() decimal.Decimal { return l.Principal.Add(l.Interest) }

// price returns the USDT index price of a collateral asset (futures index of
// <ASSET>USDT). Fails closed when unavailable.
func (s *Server) price(ctx context.Context, asset string) (decimal.Decimal, error) {
	var t struct {
		IndexPrice decimal.Decimal `json:"indexPrice"`
	}
	if err := s.call(ctx, "GET", s.cfg.FuturesURL+"/futures/market/ticker?symbol="+asset+s.cfg.Loan.Asset, nil, &t); err != nil || t.IndexPrice.Sign() <= 0 {
		return decimal.Zero, &userErr{Status: http.StatusServiceUnavailable, Code: "PRICE_UNAVAILABLE", Msg: "价格暂不可用，请稍后再试"}
	}
	return t.IndexPrice, nil
}

func ltv(debt, coll, price decimal.Decimal) decimal.Decimal {
	v := coll.Mul(price)
	if v.Sign() <= 0 {
		return decimal.Zero
	}
	return debt.Div(v).Round(dp)
}

func (s *Server) loanRoutes(mux *http.ServeMux) {
	mux.HandleFunc("GET /loans/market", s.handleLoanMarket)
	mux.HandleFunc("GET /loans/quote", s.handleLoanQuote)
	mux.HandleFunc("POST /loans", s.user(s.handleBorrow))
	mux.HandleFunc("GET /loans", s.user(s.handleLoans))
	mux.HandleFunc("GET /loans/summary", s.user(s.handleLoanSummary))
	mux.HandleFunc("GET /loans/records", s.user(s.handleLoanRecords))
	mux.HandleFunc("GET /loans/{id}", s.user(s.handleLoan))
	mux.HandleFunc("POST /loans/{id}/repay", s.user(s.handleRepay))
	mux.HandleFunc("POST /loans/{id}/collateral", s.user(s.handleAddCollateral))
	mux.HandleFunc("POST /loans/{id}/renew", s.user(s.handleRenew))
}

func (s *Server) handleLoanMarket(w http.ResponseWriter, r *http.Request) {
	lr := s.cfg.Loan
	ctx := r.Context()
	all, err := s.loanTerms(ctx)
	if err != nil {
		writeErr(w, err)
		return
	}
	terms := []*loanTerm{}
	for _, t := range all {
		if t.Status == "online" {
			terms = append(terms, t)
		}
	}
	cs, err := s.loanCollaterals(ctx, true)
	if err != nil {
		writeErr(w, err)
		return
	}
	cols := []map[string]any{}
	for _, c := range cs {
		m := map[string]any{"asset": c.Asset, "cap": c.Cap}
		if p, err := s.price(ctx, c.Asset); err == nil {
			m["price"] = p
		}
		cols = append(cols, m)
	}
	common.WriteJSON(w, http.StatusOK, map[string]any{"asset": lr.Asset, "terms": terms, "collaterals": cols,
		"initialLtv": lr.InitialLTV, "warnLtv": lr.WarnLTV, "liquidationLtv": lr.LiqLTV, "userLimit": lr.UserLimit, "minAmount": lr.MinAmount,
		"overdueMultiplier": lr.OverdueMult, "overdueLiquidationDays": lr.OverdueDays, "liquidationFeeRate": lr.LiqFeeRate,
		"earlyRepayPenaltyRate": lr.EarlyPenalty})
}

type quote struct {
	Amount         decimal.Decimal `json:"amount"`
	Collateral     decimal.Decimal `json:"collateral"`
	Price          decimal.Decimal `json:"price"`
	LTV            decimal.Decimal `json:"ltv"`
	HourlyRate     decimal.Decimal `json:"hourlyRate"`
	HourlyInterest decimal.Decimal `json:"hourlyInterest"`
	TermInterest   decimal.Decimal `json:"termInterest"` // fixed terms: interest over the whole term
	LiqPrice       decimal.Decimal `json:"liquidationPrice"`
}

func (s *Server) makeQuote(ctx context.Context, colAsset, term string, amount, coll decimal.Decimal) (*quote, error) {
	lr := s.cfg.Loan
	if c, err := s.collateralOf(ctx, colAsset); err != nil {
		return nil, err
	} else if c.Status != "online" {
		return nil, bad("UNSUPPORTED_COLLATERAL", "该质押币种已下架")
	}
	lt, err := s.loanTerm(ctx, term)
	if err != nil {
		return nil, err
	}
	if lt.Status != "online" {
		return nil, bad("TERM_OFFLINE", "该借款期限已下架")
	}
	rate := lt.HourlyRate
	if amount.Sign() <= 0 || amount.Exponent() < -dp {
		return nil, bad("VALIDATION_ERROR", "借款金额不合法")
	}
	p, err := s.price(ctx, strings.ToUpper(colAsset))
	if err != nil {
		return nil, err
	}
	if coll.Sign() <= 0 {
		coll = amount.Div(p.Mul(lr.InitialLTV)).RoundCeil(dp)
	}
	q := &quote{Amount: amount, Collateral: coll, Price: p, LTV: ltv(amount, coll, p), HourlyRate: rate,
		HourlyInterest: ceil8(amount.Mul(rate))}
	q.TermInterest = q.HourlyInterest.Mul(decimal.NewFromInt(int64(lr.TermDays[term] * 24)))
	q.LiqPrice = amount.Div(coll.Mul(lr.LiqLTV)).Round(2)
	return q, nil
}

func (s *Server) handleLoanQuote(w http.ResponseWriter, r *http.Request) {
	q := r.URL.Query()
	amount, _ := decimal.NewFromString(q.Get("amount"))
	coll, _ := decimal.NewFromString(q.Get("collateralAmount"))
	term := q.Get("term")
	if term == "" {
		term = "flexible"
	}
	out, err := s.makeQuote(r.Context(), q.Get("collateralAsset"), term, amount, coll)
	if err != nil {
		writeErr(w, err)
		return
	}
	common.WriteJSON(w, http.StatusOK, out)
}

func (s *Server) event(ctx context.Context, tx *sql.Tx, l *Loan, kind string, amount, interest, penalty, coll decimal.Decimal, price *decimal.Decimal, detail map[string]any) error {
	var lv any
	if price != nil {
		lv = ltv(l.debt(), l.Collateral, *price)
	}
	d, _ := json.Marshal(detail)
	if detail == nil {
		d = []byte("{}")
	}
	_, err := tx.ExecContext(ctx, `INSERT INTO e_loan_events (loan_id, uid, kind, amount, interest, penalty, collateral, price, ltv, detail, at)
		VALUES ($1,$2,$3,$4,$5,$6,$7,$8,$9,$10,$11)`, l.ID, l.UID, kind, amount, interest, penalty, coll, price, lv, d, s.now())
	return err
}

func (s *Server) handleBorrow(w http.ResponseWriter, r *http.Request, uid string) {
	var req struct {
		CollateralAsset  string `json:"collateralAsset"`
		Amount           string `json:"amount"`
		CollateralAmount string `json:"collateralAmount"`
		Term             string `json:"term"`
	}
	if err := common.DecodeJSON(r, &req); err != nil {
		writeErr(w, bad("VALIDATION_ERROR", "请求参数不合法"))
		return
	}
	amount, err := decimal.NewFromString(req.Amount)
	if err != nil {
		writeErr(w, bad("VALIDATION_ERROR", "借款金额不合法"))
		return
	}
	coll := decimal.Zero
	if req.CollateralAmount != "" {
		if coll, err = decimal.NewFromString(req.CollateralAmount); err != nil || coll.Sign() <= 0 || coll.Exponent() < -dp {
			writeErr(w, bad("VALIDATION_ERROR", "质押数量不合法"))
			return
		}
	}
	if req.Term == "" {
		req.Term = "flexible"
	}
	ctx := r.Context()
	lr := s.cfg.Loan
	s.mu.Lock()
	defer s.mu.Unlock()
	q, err := s.makeQuote(ctx, req.CollateralAsset, req.Term, amount, coll)
	if err != nil {
		writeErr(w, err)
		return
	}
	colAsset := strings.ToUpper(req.CollateralAsset)
	c, err := s.collateralOf(ctx, colAsset)
	if err != nil {
		writeErr(w, err)
		return
	}
	if lt, err := s.loanTerm(ctx, req.Term); err != nil {
		writeErr(w, err)
		return
	} else if lt.Remaining != nil && amount.GreaterThan(*lt.Remaining) {
		writeErr(w, bad("TERM_QUOTA_EXCEEDED", "该期限可借额度不足，剩余 "+lt.Remaining.String()+" "+lr.Asset))
		return
	}
	switch {
	case amount.LessThan(lr.MinAmount):
		writeErr(w, bad("AMOUNT_TOO_SMALL", "单次借款最少 "+lr.MinAmount.String()+" "+lr.Asset))
		return
	case q.LTV.GreaterThan(lr.InitialLTV):
		writeErr(w, bad("LTV_TOO_HIGH", "质押率超过初始质押率"))
		return
	}
	var owed, locked decimal.Decimal
	if err := s.db.QueryRowContext(ctx, `SELECT COALESCE(SUM(principal),0), COALESCE(SUM(collateral) FILTER (WHERE collateral_asset=$2),0)
		FROM e_loans WHERE uid=$1 AND status IN ('pending','active')`, uid, colAsset).Scan(&owed, &locked); err != nil {
		writeErr(w, err)
		return
	}
	if owed.Add(amount).GreaterThan(lr.UserLimit) {
		writeErr(w, bad("LOAN_LIMIT_EXCEEDED", "超过个人借款限额"))
		return
	}
	if locked.Add(q.Collateral).GreaterThan(c.Cap) {
		writeErr(w, bad("COLLATERAL_LIMIT_EXCEEDED", "超过个人质押上限 "+c.Cap.String()+" "+colAsset))
		return
	}
	now := s.now()
	var due *time.Time
	if d := lr.TermDays[req.Term]; d > 0 {
		t := now.Add(time.Duration(d) * 24 * hour)
		due = &t
	}
	var id int64
	if err := s.db.QueryRowContext(ctx, `INSERT INTO e_loans (uid, asset, collateral_asset, term, hourly_rate, initial_principal, initial_collateral,
		principal, collateral, status, started_at, due_at) VALUES ($1,$2,$3,$4,$5,$6,$7,$6,$7,'pending',$8,$9) RETURNING id`,
		uid, lr.Asset, colAsset, req.Term, q.HourlyRate, amount, q.Collateral, now, due).Scan(&id); err != nil {
		writeErr(w, err)
		return
	}
	ref := fmt.Sprintf("loan:%d", id)
	fail := func(e error) {
		_, _ = s.db.ExecContext(ctx, `UPDATE e_loans SET status='failed', closed_at=now() WHERE id=$1`, id)
		writeErr(w, e)
	}
	if err := s.systemTransfer(ctx, uid, colAsset, "loan_collateral", "debit", q.Collateral, ref+":collateral"); err != nil {
		fail(err)
		return
	}
	if err := s.systemTransfer(ctx, uid, lr.Asset, "loan", "credit", amount, ref+":disburse"); err != nil {
		_ = s.systemTransfer(ctx, uid, colAsset, "loan_collateral", "credit", q.Collateral, ref+":collateral-refund")
		fail(err)
		return
	}
	tx, err := s.db.BeginTx(ctx, nil)
	if err != nil {
		writeErr(w, err)
		return
	}
	defer tx.Rollback()
	l, err := scanLoan(tx.QueryRowContext(ctx, `UPDATE e_loans SET status='active' WHERE id=$1 RETURNING `+loanCols, id))
	if err == nil {
		err = s.accrue(ctx, tx, l)
	}
	if err == nil {
		err = s.event(ctx, tx, l, "borrow", amount, decimal.Zero, decimal.Zero, q.Collateral, &q.Price, map[string]any{"term": req.Term})
	}
	if err == nil {
		err = tx.Commit()
	}
	if err != nil {
		writeErr(w, err)
		return
	}
	common.WriteJSON(w, http.StatusOK, s.view(l, &q.Price))
}

// accrue charges every started hour up to now (or the close time). Caller
// holds s.mu; updates l and the row inside tx.
func (s *Server) accrue(ctx context.Context, tx *sql.Tx, l *Loan) error {
	if l.Status != "active" {
		return nil
	}
	target := int(s.now().Sub(l.StartedAt)/hour) + 1
	if target <= l.HoursCharged {
		return nil
	}
	add := decimal.Zero
	for k := l.HoursCharged + 1; k <= target; k++ {
		rate := l.HourlyRate
		if l.DueAt != nil && !l.StartedAt.Add(time.Duration(k-1)*hour).Before(*l.DueAt) {
			rate = rate.Mul(s.cfg.Loan.OverdueMult)
		}
		add = add.Add(ceil8(l.Principal.Mul(rate)))
	}
	l.Interest, l.HoursCharged = l.Interest.Add(add), target
	_, err := tx.ExecContext(ctx, `UPDATE e_loans SET interest=$2, hours_charged=$3 WHERE id=$1`, l.ID, l.Interest, l.HoursCharged)
	return err
}

func (s *Server) view(l *Loan, price *decimal.Decimal) map[string]any {
	m := map[string]any{"id": l.ID, "asset": l.Asset, "collateralAsset": l.CollateralAsset, "term": l.Term, "hourlyRate": l.HourlyRate,
		"initialPrincipal": l.InitialPrincipal, "initialCollateral": l.InitialCollateral, "principal": l.Principal,
		"collateral": l.Collateral, "interest": l.Interest, "interestPaid": l.InterestPaid, "debt": l.debt(), "status": l.Status,
		"hoursCharged": l.HoursCharged, "startedAt": l.StartedAt.UnixMilli(), "overdue": false}
	if l.DueAt != nil {
		m["dueAt"] = l.DueAt.UnixMilli()
		m["overdue"] = l.Status == "active" && !s.now().Before(*l.DueAt)
	}
	if l.ClosedAt != nil {
		m["closedAt"] = l.ClosedAt.UnixMilli()
	}
	if price != nil && l.Status == "active" {
		m["price"], m["ltv"] = *price, ltv(l.debt(), l.Collateral, *price)
		m["liquidationPrice"] = l.debt().Div(l.Collateral.Mul(s.cfg.Loan.LiqLTV)).Round(2)
		m["warning"] = ltv(l.debt(), l.Collateral, *price).GreaterThanOrEqual(s.cfg.Loan.WarnLTV)
	}
	return m
}

// loadForUpdate loads the user's loan in tx and brings interest up to date.
func (s *Server) loadForUpdate(ctx context.Context, tx *sql.Tx, uid, id string) (*Loan, error) {
	if _, perr := strconv.ParseInt(id, 10, 64); perr != nil {
		return nil, notFound("LOAN_NOT_FOUND", "借款订单不存在")
	}
	l, err := scanLoan(tx.QueryRowContext(ctx, `SELECT `+loanCols+` FROM e_loans WHERE id=$1 AND uid=$2 AND status<>'failed' FOR UPDATE`, id, uid))
	if errors.Is(err, sql.ErrNoRows) {
		return nil, notFound("LOAN_NOT_FOUND", "借款订单不存在")
	}
	if err != nil {
		return nil, err
	}
	return l, s.accrue(ctx, tx, l)
}

func (s *Server) handleLoans(w http.ResponseWriter, r *http.Request, uid string) {
	ctx := r.Context()
	st := r.URL.Query().Get("status") // active | closed | (all)
	q := `SELECT ` + loanCols + ` FROM e_loans WHERE uid=$1 AND status<>'failed' AND status<>'pending'`
	switch st {
	case "active":
		q += ` AND status='active'`
	case "closed":
		q += ` AND status IN ('repaid','liquidated')`
	}
	s.mu.Lock()
	defer s.mu.Unlock()
	_ = s.accrueUser(ctx, uid)
	rows, err := s.db.QueryContext(ctx, q+` ORDER BY id DESC LIMIT 200`, uid)
	if err != nil {
		writeErr(w, err)
		return
	}
	defer rows.Close()
	prices := map[string]*decimal.Decimal{}
	items := []map[string]any{}
	for rows.Next() {
		l, err := scanLoan(rows)
		if err != nil {
			writeErr(w, err)
			return
		}
		if _, ok := prices[l.CollateralAsset]; !ok {
			if p, err := s.price(ctx, l.CollateralAsset); err == nil {
				prices[l.CollateralAsset] = &p
			} else {
				prices[l.CollateralAsset] = nil
			}
		}
		items = append(items, s.view(l, prices[l.CollateralAsset]))
	}
	common.WriteJSON(w, http.StatusOK, map[string]any{"items": items})
}

func (s *Server) accrueUser(ctx context.Context, uid string) error {
	tx, err := s.db.BeginTx(ctx, nil)
	if err != nil {
		return err
	}
	defer tx.Rollback()
	rows, err := tx.QueryContext(ctx, `SELECT `+loanCols+` FROM e_loans WHERE uid=$1 AND status='active'`, uid)
	if err != nil {
		return err
	}
	var ls []*Loan
	for rows.Next() {
		if l, err := scanLoan(rows); err == nil {
			ls = append(ls, l)
		}
	}
	rows.Close()
	for _, l := range ls {
		if err := s.accrue(ctx, tx, l); err != nil {
			return err
		}
	}
	return tx.Commit()
}

func (s *Server) handleLoan(w http.ResponseWriter, r *http.Request, uid string) {
	ctx := r.Context()
	s.mu.Lock()
	defer s.mu.Unlock()
	tx, err := s.db.BeginTx(ctx, nil)
	if err != nil {
		writeErr(w, err)
		return
	}
	defer tx.Rollback()
	l, err := s.loadForUpdate(ctx, tx, uid, r.PathValue("id"))
	if err == nil {
		err = tx.Commit()
	}
	if err != nil {
		writeErr(w, err)
		return
	}
	var pp *decimal.Decimal
	if p, err := s.price(ctx, l.CollateralAsset); err == nil {
		pp = &p
	}
	common.WriteJSON(w, http.StatusOK, s.view(l, pp))
}

func (s *Server) handleLoanSummary(w http.ResponseWriter, r *http.Request, uid string) {
	ctx := r.Context()
	s.mu.Lock()
	_ = s.accrueUser(ctx, uid)
	s.mu.Unlock()
	rows, err := s.db.QueryContext(ctx, `SELECT collateral_asset, SUM(principal+interest), SUM(collateral), COUNT(*) FROM e_loans
		WHERE uid=$1 AND status='active' GROUP BY collateral_asset`, uid)
	if err != nil {
		writeErr(w, err)
		return
	}
	defer rows.Close()
	debt, collUSD := decimal.Zero, decimal.Zero
	n := 0
	for rows.Next() {
		var a string
		var d, c decimal.Decimal
		var k int
		if rows.Scan(&a, &d, &c, &k) != nil {
			continue
		}
		debt, n = debt.Add(d), n+k
		if p, err := s.price(ctx, a); err == nil {
			collUSD = collUSD.Add(c.Mul(p))
		}
	}
	common.WriteJSON(w, http.StatusOK, map[string]any{"active": n, "totalDebt": debt, "totalCollateralValue": collUSD.Round(dp),
		"asset": s.cfg.Loan.Asset})
}

func (s *Server) handleLoanRecords(w http.ResponseWriter, r *http.Request, uid string) {
	kind := r.URL.Query().Get("type")
	q := `SELECT e.id, e.loan_id, e.kind, e.amount, e.interest, e.penalty, e.collateral, e.price, e.ltv, e.detail, e.at, l.term, l.asset, l.collateral_asset
		FROM e_loan_events e JOIN e_loans l ON l.id=e.loan_id WHERE e.uid=$1`
	args := []any{uid}
	if kind != "" {
		q += ` AND e.kind=$2`
		args = append(args, kind)
	}
	rows, err := s.db.QueryContext(r.Context(), q+` ORDER BY e.id DESC LIMIT 200`, args...)
	if err != nil {
		writeErr(w, err)
		return
	}
	defer rows.Close()
	items := []map[string]any{}
	for rows.Next() {
		var id, lid int64
		var k, term, asset, ca string
		var amt, intr, pen, coll decimal.Decimal
		var price, lv decimal.NullDecimal
		var detail []byte
		var at time.Time
		if err := rows.Scan(&id, &lid, &k, &amt, &intr, &pen, &coll, &price, &lv, &detail, &at, &term, &asset, &ca); err != nil {
			writeErr(w, err)
			return
		}
		m := map[string]any{"id": id, "loanId": lid, "type": k, "amount": amt, "interest": intr, "penalty": pen, "collateral": coll,
			"term": term, "asset": asset, "collateralAsset": ca, "time": at.UnixMilli(), "detail": json.RawMessage(detail)}
		if price.Valid {
			m["price"] = price.Decimal
		}
		if lv.Valid {
			m["ltv"] = lv.Decimal
		}
		items = append(items, m)
	}
	common.WriteJSON(w, http.StatusOK, map[string]any{"items": items})
}

// handleRepay: {amount} (interest first, then penalty + principal) or {all:true}.
func (s *Server) handleRepay(w http.ResponseWriter, r *http.Request, uid string) {
	var req struct {
		Amount string `json:"amount"`
		All    bool   `json:"all"`
	}
	if err := common.DecodeJSON(r, &req); err != nil {
		writeErr(w, bad("VALIDATION_ERROR", "请求参数不合法"))
		return
	}
	ctx := r.Context()
	s.mu.Lock()
	defer s.mu.Unlock()
	tx, err := s.db.BeginTx(ctx, nil)
	if err != nil {
		writeErr(w, err)
		return
	}
	defer tx.Rollback()
	l, err := s.loadForUpdate(ctx, tx, uid, r.PathValue("id"))
	if err != nil {
		writeErr(w, err)
		return
	}
	if l.Status != "active" {
		writeErr(w, conflict("LOAN_NOT_ACTIVE", "借款订单已结束"))
		return
	}
	penRate := decimal.Zero
	if l.DueAt != nil && s.now().Before(*l.DueAt) {
		penRate = s.cfg.Loan.EarlyPenalty
	}
	fullPenalty := ceil8(l.Principal.Mul(penRate))
	full := l.Interest.Add(l.Principal).Add(fullPenalty)
	var amount decimal.Decimal
	if req.All {
		amount = full
	} else if amount, err = decimal.NewFromString(req.Amount); err != nil || amount.Sign() <= 0 || amount.Exponent() < -dp {
		writeErr(w, bad("VALIDATION_ERROR", "还款金额不合法"))
		return
	}
	if amount.GreaterThan(full) {
		amount = full
	}
	intPart := decimal.Min(amount, l.Interest)
	rest := amount.Sub(intPart)
	var prin, pen decimal.Decimal
	if rest.Equal(l.Principal.Add(fullPenalty)) {
		prin, pen = l.Principal, fullPenalty
	} else if rest.Sign() > 0 {
		prin = rest.Div(decimal.NewFromInt(1).Add(penRate)).RoundFloor(dp)
		pen = rest.Sub(prin)
	}
	n := time.Now().UnixNano()
	ref := fmt.Sprintf("loan:%d:repay:%d", l.ID, n)
	// Principal first (largest leg); if the interest leg then fails, the
	// principal debit is refunded so the repayment is all-or-nothing.
	if prin.Sign() > 0 {
		if err := s.systemTransfer(ctx, uid, l.Asset, "loan", "debit", prin, ref+":principal"); err != nil {
			writeErr(w, err)
			return
		}
	}
	if intPart.Add(pen).Sign() > 0 {
		if err := s.systemTransfer(ctx, uid, l.Asset, "loan_interest", "debit", intPart.Add(pen), ref+":interest"); err != nil {
			if prin.Sign() > 0 {
				_ = s.systemTransfer(ctx, uid, l.Asset, "loan", "credit", prin, ref+":principal-refund")
			}
			writeErr(w, err)
			return
		}
	}
	l.Interest, l.InterestPaid, l.Principal = l.Interest.Sub(intPart), l.InterestPaid.Add(intPart), l.Principal.Sub(prin)
	closed := l.Principal.Sign() == 0 && l.Interest.Sign() == 0
	released := decimal.Zero
	if closed {
		released = l.Collateral
		if err := s.systemTransfer(ctx, uid, l.CollateralAsset, "loan_collateral", "credit", released, fmt.Sprintf("loan:%d:release", l.ID)); err != nil {
			writeErr(w, err)
			return
		}
		l.Status, l.Collateral = "repaid", decimal.Zero
		t := s.now()
		l.ClosedAt = &t
	}
	if _, err := tx.ExecContext(ctx, `UPDATE e_loans SET principal=$2, interest=$3, interest_paid=$4, collateral=$5, status=$6, closed_at=$7 WHERE id=$1`,
		l.ID, l.Principal, l.Interest, l.InterestPaid, l.Collateral, l.Status, l.ClosedAt); err != nil {
		writeErr(w, err)
		return
	}
	if err := s.event(ctx, tx, l, "repay", prin, intPart, pen, released, nil, map[string]any{"closed": closed}); err != nil {
		writeErr(w, err)
		return
	}
	if err := tx.Commit(); err != nil {
		writeErr(w, err)
		return
	}
	out := s.view(l, nil)
	out["repaid"] = map[string]any{"principal": prin, "interest": intPart, "penalty": pen, "total": prin.Add(intPart).Add(pen), "collateralReleased": released}
	common.WriteJSON(w, http.StatusOK, out)
}

func (s *Server) handleAddCollateral(w http.ResponseWriter, r *http.Request, uid string) {
	var req struct {
		Amount string `json:"amount"`
	}
	_ = common.DecodeJSON(r, &req)
	amt, err := decimal.NewFromString(req.Amount)
	if err != nil || amt.Sign() <= 0 || amt.Exponent() < -dp {
		writeErr(w, bad("VALIDATION_ERROR", "质押数量不合法"))
		return
	}
	ctx := r.Context()
	s.mu.Lock()
	defer s.mu.Unlock()
	tx, err := s.db.BeginTx(ctx, nil)
	if err != nil {
		writeErr(w, err)
		return
	}
	defer tx.Rollback()
	l, err := s.loadForUpdate(ctx, tx, uid, r.PathValue("id"))
	if err != nil {
		writeErr(w, err)
		return
	}
	if l.Status != "active" {
		writeErr(w, conflict("LOAN_NOT_ACTIVE", "借款订单已结束"))
		return
	}
	c, err := s.collateralOf(ctx, l.CollateralAsset)
	if err != nil {
		writeErr(w, err)
		return
	}
	var locked decimal.Decimal
	_ = tx.QueryRowContext(ctx, `SELECT COALESCE(SUM(collateral),0) FROM e_loans WHERE uid=$1 AND collateral_asset=$2 AND status='active'`, uid, l.CollateralAsset).Scan(&locked)
	if locked.Add(amt).GreaterThan(c.Cap) {
		writeErr(w, bad("COLLATERAL_LIMIT_EXCEEDED", "超过个人质押上限 "+c.Cap.String()+" "+l.CollateralAsset))
		return
	}
	n := time.Now().UnixNano()
	if err := s.systemTransfer(ctx, uid, l.CollateralAsset, "loan_collateral", "debit", amt, fmt.Sprintf("loan:%d:add:%d", l.ID, n)); err != nil {
		writeErr(w, err)
		return
	}
	l.Collateral = l.Collateral.Add(amt)
	if _, err := tx.ExecContext(ctx, `UPDATE e_loans SET collateral=$2, warned_at=NULL WHERE id=$1`, l.ID, l.Collateral); err != nil {
		writeErr(w, err)
		return
	}
	var pp *decimal.Decimal
	if p, err := s.price(ctx, l.CollateralAsset); err == nil {
		pp = &p
	}
	if err := s.event(ctx, tx, l, "collateral", decimal.Zero, decimal.Zero, decimal.Zero, amt, pp, nil); err != nil {
		writeErr(w, err)
		return
	}
	if err := tx.Commit(); err != nil {
		writeErr(w, err)
		return
	}
	common.WriteJSON(w, http.StatusOK, s.view(l, pp))
}

// handleRenew (续借): fixed terms only. Pays the accrued interest, then
// extends the due date by one term (from the old due date, or from now when
// already overdue).
func (s *Server) handleRenew(w http.ResponseWriter, r *http.Request, uid string) {
	ctx := r.Context()
	s.mu.Lock()
	defer s.mu.Unlock()
	tx, err := s.db.BeginTx(ctx, nil)
	if err != nil {
		writeErr(w, err)
		return
	}
	defer tx.Rollback()
	l, err := s.loadForUpdate(ctx, tx, uid, r.PathValue("id"))
	if err != nil {
		writeErr(w, err)
		return
	}
	if l.Status != "active" {
		writeErr(w, conflict("LOAN_NOT_ACTIVE", "借款订单已结束"))
		return
	}
	if l.DueAt == nil {
		writeErr(w, bad("TERM_NOT_RENEWABLE", "活期借款无需续借"))
		return
	}
	paid := l.Interest
	if paid.Sign() > 0 {
		n := time.Now().UnixNano()
		if err := s.systemTransfer(ctx, uid, l.Asset, "loan_interest", "debit", paid, fmt.Sprintf("loan:%d:renew:%d", l.ID, n)); err != nil {
			writeErr(w, err)
			return
		}
	}
	base := *l.DueAt
	now := s.now()
	if base.Before(now) {
		base = now
	}
	due := base.Add(time.Duration(s.cfg.Loan.TermDays[l.Term]) * 24 * hour)
	l.Interest, l.InterestPaid, l.DueAt = decimal.Zero, l.InterestPaid.Add(paid), &due
	if _, err := tx.ExecContext(ctx, `UPDATE e_loans SET interest=0, interest_paid=$2, due_at=$3 WHERE id=$1`, l.ID, l.InterestPaid, due); err != nil {
		writeErr(w, err)
		return
	}
	if err := s.event(ctx, tx, l, "renew", decimal.Zero, paid, decimal.Zero, decimal.Zero, nil, map[string]any{"dueAt": due.UnixMilli()}); err != nil {
		writeErr(w, err)
		return
	}
	if err := tx.Commit(); err != nil {
		writeErr(w, err)
		return
	}
	common.WriteJSON(w, http.StatusOK, s.view(l, nil))
}

// loanTick accrues interest, flags the warning line and liquidates loans at
// the liquidation line or overdue for more than OverdueDays.
func (s *Server) loanTick(ctx context.Context) error {
	s.mu.Lock()
	defer s.mu.Unlock()
	rows, err := s.db.QueryContext(ctx, `SELECT id, uid FROM e_loans WHERE status='active'`)
	if err != nil {
		return err
	}
	type ref struct{ id, uid string }
	var ids []ref
	for rows.Next() {
		var x ref
		if rows.Scan(&x.id, &x.uid) == nil {
			ids = append(ids, x)
		}
	}
	rows.Close()
	prices := map[string]decimal.Decimal{}
	var errs []error
	for _, x := range ids {
		errs = append(errs, s.checkLoan(ctx, x.uid, x.id, prices))
	}
	return errors.Join(errs...)
}

func (s *Server) checkLoan(ctx context.Context, uid, id string, prices map[string]decimal.Decimal) error {
	tx, err := s.db.BeginTx(ctx, nil)
	if err != nil {
		return err
	}
	defer tx.Rollback()
	l, err := s.loadForUpdate(ctx, tx, uid, id)
	if err != nil {
		return err
	}
	p, ok := prices[l.CollateralAsset]
	if !ok {
		if p, err = s.price(ctx, l.CollateralAsset); err != nil {
			p = decimal.Zero
		}
		prices[l.CollateralAsset] = p
	}
	lr := s.cfg.Loan
	overdueLiq := l.DueAt != nil && !s.now().Before(l.DueAt.Add(time.Duration(lr.OverdueDays)*24*hour))
	if p.Sign() <= 0 { // fail closed: no price, no liquidation
		return tx.Commit()
	}
	cur := ltv(l.debt(), l.Collateral, p)
	switch {
	case overdueLiq || cur.GreaterThanOrEqual(lr.LiqLTV):
		reason := "ltv"
		if overdueLiq {
			reason = "overdue"
		}
		return s.liquidate(ctx, tx, l, p, cur, reason)
	case cur.GreaterThanOrEqual(lr.WarnLTV) && l.WarnedAt == nil:
		if _, err := tx.ExecContext(ctx, `UPDATE e_loans SET warned_at=$2 WHERE id=$1`, l.ID, s.now()); err != nil {
			return err
		}
	case cur.LessThan(lr.WarnLTV) && l.WarnedAt != nil:
		if _, err := tx.ExecContext(ctx, `UPDATE e_loans SET warned_at=NULL WHERE id=$1`, l.ID); err != nil {
			return err
		}
	}
	return tx.Commit()
}

// liquidate: the platform takes collateral worth debt × (1 + fee) at the index
// price; any remainder goes back to the user. The debt is closed.
func (s *Server) liquidate(ctx context.Context, tx *sql.Tx, l *Loan, p, cur decimal.Decimal, reason string) error {
	debt := l.debt()
	fee := ceil8(debt.Mul(s.cfg.Loan.LiqFeeRate))
	seized := debt.Add(fee).Div(p).RoundCeil(dp)
	if seized.GreaterThan(l.Collateral) {
		seized = l.Collateral
	}
	back := l.Collateral.Sub(seized)
	if back.Sign() > 0 {
		if err := s.systemTransfer(ctx, l.UID, l.CollateralAsset, "loan_collateral", "credit", back, fmt.Sprintf("loan:%d:liquidation-return", l.ID)); err != nil {
			return err
		}
	}
	now := s.now()
	ev := *l
	if _, err := tx.ExecContext(ctx, `UPDATE e_loans SET status='liquidated', closed_at=$2, principal=0, interest=0, collateral=0 WHERE id=$1`, l.ID, now); err != nil {
		return err
	}
	if err := s.event(ctx, tx, &ev, "liquidate", l.Principal, l.Interest, fee, seized, &p,
		map[string]any{"reason": reason, "ltv": cur, "collateralReturned": back, "fee": fee}); err != nil {
		return err
	}
	return tx.Commit()
}
