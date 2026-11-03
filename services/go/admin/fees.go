package main

import (
	"context"
	"database/sql"
	"encoding/json"
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

// Fee changes need two approvals from distinct people, neither the submitter:
// one super_admin and one finance_second (DESIGN §3/§5). Once both are in, a
// public announcement is published and the change takes effect no earlier
// than FeeNotice (24h) later; the scheduler then applies it to the order
// service. A rollback is itself a fee change that restores the "before" rates
// and goes through the same approvals and notice.

const kindFee = "fee_rates"

type feePayload struct {
	SpotMaker string `json:"spotMaker,omitempty"`
	SpotTaker string `json:"spotTaker,omitempty"`
}

type feeChangeReq struct {
	SpotMaker   string `json:"spotMaker"`
	SpotTaker   string `json:"spotTaker"`
	Reason      string `json:"reason"`
	EffectiveAt string `json:"effectiveAt"` // optional RFC3339; clamped to approval + notice
}

func validRate(v string) (string, bool) {
	if strings.TrimSpace(v) == "" {
		return "", true
	}
	d, err := decimal.NewFromString(v)
	if err != nil || d.IsNegative() || d.GreaterThanOrEqual(decimal.RequireFromString("0.1")) {
		return "", false
	}
	return d.String(), true
}

func (s *Server) currentFees(ctx context.Context) (feePayload, error) {
	u, err := s.call(ctx, http.MethodGet, s.cfg.OrderURL, "/internal/admin/fees", nil)
	if err != nil {
		return feePayload{}, err
	}
	if !u.ok() {
		return feePayload{}, fmt.Errorf("order fees: %d %s", u.Status, u.errCode())
	}
	var p feePayload
	return p, u.decode(&p)
}

func (s *Server) handleGetFees(w http.ResponseWriter, r *http.Request) {
	cur, err := s.currentFees(r.Context())
	if err != nil {
		upstreamErr(w, "fees", err)
		return
	}
	pending, err := s.listChanges(r.Context(), "open")
	if err != nil {
		internalErr(w, "fees pending", err)
		return
	}
	common.WriteJSON(w, http.StatusOK, map[string]interface{}{"current": cur, "pendingChanges": pending,
		"noticeHours": int(s.cfg.FeeNotice.Hours())})
}

type approvalDTO struct {
	AdminID  int64  `json:"adminId"`
	Admin    string `json:"admin"`
	AsRole   string `json:"asRole"`
	Decision string `json:"decision"`
	Comment  string `json:"comment,omitempty"`
	At       string `json:"at"`
}

type changeDTO struct {
	ID                   int64           `json:"id"`
	Kind                 string          `json:"kind"`
	Payload              json.RawMessage `json:"payload"`
	Before               json.RawMessage `json:"before,omitempty"`
	Status               string          `json:"status"`
	Reason               string          `json:"reason"`
	SubmittedBy          int64           `json:"submittedBy"`
	RequestedEffectiveAt string          `json:"requestedEffectiveAt,omitempty"`
	EffectiveAt          string          `json:"effectiveAt,omitempty"`
	AnnouncementID       *int64          `json:"announcementId,omitempty"`
	RollbackOf           *int64          `json:"rollbackOf,omitempty"`
	AppliedAt            string          `json:"appliedAt,omitempty"`
	Error                string          `json:"error,omitempty"`
	CreatedAt            string          `json:"createdAt"`
	Approvals            []approvalDTO   `json:"approvals"`
}

const changeCols = `id, kind, payload::text, COALESCE(before::text, ''), status, reason, submitted_by, requested_effective_at, effective_at,
	announcement_id, rollback_of, applied_at, COALESCE(error, ''), created_at`

func scanChange(sc interface{ Scan(...interface{}) error }) (*changeDTO, error) {
	var c changeDTO
	var payload, before string
	var req, eff, applied sql.NullTime
	var ann, rb sql.NullInt64
	var created time.Time
	if err := sc.Scan(&c.ID, &c.Kind, &payload, &before, &c.Status, &c.Reason, &c.SubmittedBy, &req, &eff, &ann, &rb, &applied, &c.Error, &created); err != nil {
		return nil, err
	}
	c.Payload = json.RawMessage(payload)
	if before != "" {
		c.Before = json.RawMessage(before)
	}
	if req.Valid {
		c.RequestedEffectiveAt = fmtTime(req.Time)
	}
	if eff.Valid {
		c.EffectiveAt = fmtTime(eff.Time)
	}
	if applied.Valid {
		c.AppliedAt = fmtTime(applied.Time)
	}
	if ann.Valid {
		c.AnnouncementID = &ann.Int64
	}
	if rb.Valid {
		c.RollbackOf = &rb.Int64
	}
	c.CreatedAt = fmtTime(created)
	c.Approvals = []approvalDTO{}
	return &c, nil
}

func (s *Server) loadApprovals(ctx context.Context, c *changeDTO) error {
	rows, err := s.db.QueryContext(ctx, `SELECT a.admin_id, u.username, a.as_role, a.decision, COALESCE(a.comment, ''), a.at
		FROM change_approvals a JOIN admin_users u ON u.id = a.admin_id WHERE a.change_id = $1 ORDER BY a.at`, c.ID)
	if err != nil {
		return err
	}
	defer rows.Close()
	for rows.Next() {
		var d approvalDTO
		var at time.Time
		if err := rows.Scan(&d.AdminID, &d.Admin, &d.AsRole, &d.Decision, &d.Comment, &at); err != nil {
			return err
		}
		d.At = fmtTime(at)
		c.Approvals = append(c.Approvals, d)
	}
	return rows.Err()
}

func (s *Server) getChange(ctx context.Context, id int64) (*changeDTO, error) {
	c, err := scanChange(s.db.QueryRowContext(ctx, `SELECT `+changeCols+` FROM change_requests WHERE id = $1`, id))
	if err != nil {
		return nil, err
	}
	return c, s.loadApprovals(ctx, c)
}

// listChanges: filter "" = all, "open" = pending|scheduled, otherwise a status.
func (s *Server) listChanges(ctx context.Context, filter string) ([]*changeDTO, error) {
	return s.listChangesOf(ctx, []string{kindFee}, filter)
}

// listChangesOf lists changes of the given kinds.
func (s *Server) listChangesOf(ctx context.Context, kinds []string, filter string) ([]*changeDTO, error) {
	q := `SELECT ` + changeCols + ` FROM change_requests WHERE kind = ANY($1)`
	args := []interface{}{textArray(kinds)}
	switch filter {
	case "":
	case "open":
		q += ` AND status IN ('pending', 'scheduled')`
	default:
		q += ` AND status = $2`
		args = append(args, filter)
	}
	rows, err := s.db.QueryContext(ctx, q+` ORDER BY id DESC LIMIT 200`, args...)
	if err != nil {
		return nil, err
	}
	var out []*changeDTO
	for rows.Next() {
		c, err := scanChange(rows)
		if err != nil {
			rows.Close()
			return nil, err
		}
		out = append(out, c)
	}
	rows.Close()
	for _, c := range out {
		if err := s.loadApprovals(ctx, c); err != nil {
			return nil, err
		}
	}
	if out == nil {
		out = []*changeDTO{}
	}
	return out, nil
}

func (s *Server) handleListFeeChanges(w http.ResponseWriter, r *http.Request) {
	items, err := s.listChanges(r.Context(), r.URL.Query().Get("status"))
	if err != nil {
		internalErr(w, "list fee changes", err)
		return
	}
	common.WriteJSON(w, http.StatusOK, map[string]interface{}{"items": items})
}

func (s *Server) createFeeChange(ctx context.Context, a *actor, p feePayload, reason string, reqEff *time.Time, rollbackOf *int64) (*changeDTO, error) {
	cur, err := s.currentFees(ctx)
	if err != nil {
		return nil, err
	}
	before := feePayload{}
	if p.SpotMaker != "" {
		before.SpotMaker = cur.SpotMaker
	}
	if p.SpotTaker != "" {
		before.SpotTaker = cur.SpotTaker
	}
	pj, _ := json.Marshal(p)
	bj, _ := json.Marshal(before)
	var id int64
	err = s.db.QueryRowContext(ctx, `INSERT INTO change_requests (kind, payload, before, reason, submitted_by, requested_effective_at, rollback_of)
		VALUES ($1, $2, $3, $4, $5, $6, $7) RETURNING id`, kindFee, string(pj), string(bj), reason, a.ID, reqEff, rollbackOf).Scan(&id)
	if err != nil {
		return nil, err
	}
	return s.getChange(ctx, id)
}

func (s *Server) handleSubmitFeeChange(w http.ResponseWriter, r *http.Request) {
	a := actorOf(r)
	var req feeChangeReq
	if err := common.DecodeJSON(r, &req); err != nil {
		common.WriteError(w, http.StatusBadRequest, "VALIDATION_ERROR", "请求格式错误")
		return
	}
	m, ok1 := validRate(req.SpotMaker)
	t, ok2 := validRate(req.SpotTaker)
	if !ok1 || !ok2 || (m == "" && t == "") {
		common.WriteError(w, http.StatusBadRequest, "VALIDATION_ERROR", "费率需在 [0, 0.1) 之间，且至少修改一项")
		return
	}
	if strings.TrimSpace(req.Reason) == "" {
		common.WriteError(w, http.StatusBadRequest, "REASON_REQUIRED", "必须填写变更原因")
		return
	}
	var eff *time.Time
	if req.EffectiveAt != "" {
		tm, err := time.Parse(time.RFC3339, req.EffectiveAt)
		if err != nil {
			common.WriteError(w, http.StatusBadRequest, "VALIDATION_ERROR", "effectiveAt 需为 RFC3339")
			return
		}
		eff = &tm
	}
	c, err := s.createFeeChange(r.Context(), a, feePayload{SpotMaker: m, SpotTaker: t}, req.Reason, eff, nil)
	if err != nil {
		internalErr(w, "submit fee change", err)
		return
	}
	s.audit(r, a, "fee", "submit_change", "fee_change:"+strconv.FormatInt(c.ID, 10), levelHigh, "ok", c.Before, c.Payload, req.Reason)
	common.WriteJSON(w, http.StatusCreated, c)
}

// handleFeeDecision records an approval or rejection. The approver must hold
// super_admin or finance_second, not be the submitter, and not have voted.
func (s *Server) handleFeeDecision(decision string) http.HandlerFunc {
	return s.handleChangeDecision([]string{kindFee}, decision)
}

// handleChangeDecision is the shared dual-approval step for every change
// kind (fee rates, earn/loan rates): super_admin + finance_second, neither the
// submitter, then a public notice and the FeeNotice delay before applying.
func (s *Server) handleChangeDecision(kinds []string, decision string) http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		a := actorOf(r)
		id, ok := pathID(r)
		if !ok {
			common.WriteError(w, http.StatusBadRequest, "VALIDATION_ERROR", "id 无效")
			return
		}
		var req reasonReq
		_ = common.DecodeJSON(r, &req)
		comment := firstNonEmpty(req.Comment, req.Reason)
		if decision == "reject" && strings.TrimSpace(comment) == "" {
			common.WriteError(w, http.StatusBadRequest, "REASON_REQUIRED", "驳回必须填写原因")
			return
		}
		c, err := s.getChange(r.Context(), id)
		if errors.Is(err, sql.ErrNoRows) {
			common.WriteError(w, http.StatusNotFound, "NOT_FOUND", "变更不存在")
			return
		} else if err != nil {
			internalErr(w, "fee decision load", err)
			return
		}
		if !kindIn(c.Kind, kinds) {
			common.WriteError(w, http.StatusNotFound, "NOT_FOUND", "变更不存在")
			return
		}
		if c.Status != "pending" {
			common.WriteError(w, http.StatusConflict, "NOT_PENDING", "该变更不在待审批状态")
			return
		}
		if c.SubmittedBy == a.ID {
			common.WriteError(w, http.StatusForbidden, "SELF_APPROVAL", "提交人不能审批自己的变更")
			return
		}
		have := map[string]bool{}
		for _, ap := range c.Approvals {
			if ap.AdminID == a.ID {
				common.WriteError(w, http.StatusConflict, "ALREADY_DECIDED", "你已审批过该变更")
				return
			}
			if ap.Decision == "approve" {
				have[ap.AsRole] = true
			}
		}
		// Pick the role slot this approver fills.
		asRole := ""
		for _, role := range []string{roleFinanceSecond, roleSuper} {
			if hasRole(a.Roles, role) && !have[role] {
				asRole = role
				break
			}
		}
		if asRole == "" {
			if hasRole(a.Roles, roleFinanceSecond) || hasRole(a.Roles, roleSuper) {
				asRole = a.Roles[0]
				if decision == "approve" {
					common.WriteError(w, http.StatusConflict, "ROLE_SLOT_FILLED", "你所属角色的审批已完成，需要另一角色审批")
					return
				}
			} else {
				common.WriteError(w, http.StatusForbidden, "FORBIDDEN", "需要超级管理员或财务复审角色")
				return
			}
		}
		if decision == "approve" && !s.verify2FA(w, a, req.TOTPCode) {
			return
		}
		tx, err := s.db.BeginTx(r.Context(), nil)
		if err != nil {
			internalErr(w, "fee decision", err)
			return
		}
		defer tx.Rollback()
		var st string
		if err := tx.QueryRowContext(r.Context(), `SELECT status FROM change_requests WHERE id = $1 FOR UPDATE`, id).Scan(&st); err != nil || st != "pending" {
			common.WriteError(w, http.StatusConflict, "NOT_PENDING", "该变更不在待审批状态")
			return
		}
		if _, err := tx.ExecContext(r.Context(), `INSERT INTO change_approvals (change_id, admin_id, as_role, decision, comment) VALUES ($1,$2,$3,$4,$5)`,
			id, a.ID, asRole, decision, nullStr(comment)); err != nil {
			common.WriteError(w, http.StatusConflict, "ALREADY_DECIDED", "你已审批过该变更")
			return
		}
		now := s.now()
		result := decision
		if decision == "reject" {
			if _, err := tx.ExecContext(r.Context(), `UPDATE change_requests SET status = 'rejected', updated_at = $2 WHERE id = $1`, id, now); err != nil {
				internalErr(w, "fee reject", err)
				return
			}
		} else {
			have[asRole] = true
			if have[roleSuper] && have[roleFinanceSecond] {
				eff := now.Add(s.cfg.FeeNotice)
				if c.RequestedEffectiveAt != "" {
					if rt, _ := time.Parse(time.RFC3339, c.RequestedEffectiveAt); rt.After(eff) {
						eff = rt
					}
				}
				cat, title, body := changeNotice(c, eff)
				var annID int64
				if err := tx.QueryRowContext(r.Context(), `INSERT INTO announcements (category, title, body, publish_at, created_by) VALUES ($1, $2, $3, $4, 'system') RETURNING id`,
					cat, title, body, now).Scan(&annID); err != nil {
					internalErr(w, "fee announcement", err)
					return
				}
				if _, err := tx.ExecContext(r.Context(), `UPDATE change_requests SET status = 'scheduled', effective_at = $2, announcement_id = $3, updated_at = $4 WHERE id = $1`,
					id, eff, annID, now); err != nil {
					internalErr(w, "fee schedule", err)
					return
				}
				result = "scheduled"
			}
		}
		if err := tx.Commit(); err != nil {
			internalErr(w, "fee decision commit", err)
			return
		}
		after, _ := s.getChange(r.Context(), id)
		s.audit(r, a, changeModule(c.Kind), decision+"_change", changeModule(c.Kind)+"_change:"+strconv.FormatInt(id, 10), levelHigh, result, nil, map[string]string{"status": after.Status, "asRole": asRole}, comment)
		common.WriteJSON(w, http.StatusOK, after)
	}
}

func feeNoticeBody(p feePayload, eff time.Time) string {
	var parts []string
	if p.SpotMaker != "" {
		parts = append(parts, "现货 Maker 费率调整为 "+p.SpotMaker)
	}
	if p.SpotTaker != "" {
		parts = append(parts, "现货 Taker 费率调整为 "+p.SpotTaker)
	}
	return strings.Join(parts, "；") + "，生效时间 " + eff.UTC().Format("2006-01-02 15:04 UTC") + "。"
}

// handleFeeRollback opens a change restoring an applied change's "before" rates.
func (s *Server) handleFeeRollback(w http.ResponseWriter, r *http.Request) {
	a := actorOf(r)
	id, ok := pathID(r)
	if !ok {
		common.WriteError(w, http.StatusBadRequest, "VALIDATION_ERROR", "id 无效")
		return
	}
	var req reasonReq
	if err := common.DecodeJSON(r, &req); err != nil || strings.TrimSpace(req.Reason) == "" {
		common.WriteError(w, http.StatusBadRequest, "REASON_REQUIRED", "必须填写回滚原因")
		return
	}
	c, err := s.getChange(r.Context(), id)
	if errors.Is(err, sql.ErrNoRows) {
		common.WriteError(w, http.StatusNotFound, "NOT_FOUND", "变更不存在")
		return
	} else if err != nil {
		internalErr(w, "rollback load", err)
		return
	}
	if c.Kind != kindFee {
		common.WriteError(w, http.StatusNotFound, "NOT_FOUND", "变更不存在")
		return
	}
	if c.Status != "applied" || len(c.Before) == 0 {
		common.WriteError(w, http.StatusConflict, "NOT_APPLIED", "只能回滚已生效的变更")
		return
	}
	var p feePayload
	_ = json.Unmarshal(c.Before, &p)
	nc, err := s.createFeeChange(r.Context(), a, p, req.Reason, nil, &id)
	if err != nil {
		internalErr(w, "rollback create", err)
		return
	}
	s.audit(r, a, "fee", "submit_rollback", "fee_change:"+strconv.FormatInt(nc.ID, 10), levelHigh, "ok", nc.Before, nc.Payload, req.Reason)
	common.WriteJSON(w, http.StatusCreated, nc)
}

// ---- scheduler ----

// RunScheduler periodically applies due fee changes.
func (s *Server) RunScheduler(ctx context.Context) {
	t := time.NewTicker(s.cfg.SchedulerInterval)
	defer t.Stop()
	for {
		if n, err := s.applyDue(ctx, s.now()); err != nil {
			log.Printf("scheduler: %v", err)
		} else if n > 0 {
			log.Printf("scheduler: applied %d change(s)", n)
		}
		select {
		case <-ctx.Done():
			return
		case <-t.C:
		}
	}
}

// applyDue pushes every scheduled change whose effective time has passed.
// Failures stay scheduled (retried next tick) with the error recorded.
func (s *Server) applyDue(ctx context.Context, now time.Time) (int, error) {
	rows, err := s.db.QueryContext(ctx, `SELECT id, kind, payload::text FROM change_requests WHERE kind = ANY($1) AND status = 'scheduled' AND effective_at <= $2 ORDER BY effective_at, id`,
		textArray([]string{kindFee, kindEarnRate, kindLoanRate}), now)
	if err != nil {
		return 0, err
	}
	type due struct {
		id      int64
		kind    string
		payload string
	}
	var list []due
	for rows.Next() {
		var d due
		if err := rows.Scan(&d.id, &d.kind, &d.payload); err != nil {
			rows.Close()
			return 0, err
		}
		list = append(list, d)
	}
	rows.Close()
	n := 0
	for _, d := range list {
		err := s.applyChange(ctx, d.id, d.kind, []byte(d.payload))
		if err != nil {
			_, _ = s.db.ExecContext(ctx, `UPDATE change_requests SET error = $2, updated_at = now() WHERE id = $1`, d.id, err.Error())
			s.auditSystem(changeModule(d.kind), "apply_change", changeModule(d.kind)+"_change:"+strconv.FormatInt(d.id, 10), "failed", err.Error())
			continue
		}
		if _, err := s.db.ExecContext(ctx, `UPDATE change_requests SET status = 'applied', applied_at = $2, error = NULL, updated_at = $2 WHERE id = $1`, d.id, now); err != nil {
			return n, err
		}
		s.auditSystem(changeModule(d.kind), "apply_change", changeModule(d.kind)+"_change:"+strconv.FormatInt(d.id, 10), "ok", "")
		n++
	}
	return n, nil
}

func (s *Server) auditSystem(module, action, target, result, reason string) {
	_, err := s.db.Exec(`INSERT INTO audit_logs (admin_name, module, action, target, request_id, level, result, reason)
		VALUES ('system', $1, $2, $3, $4, 'high', $5, $6)`, module, action, target, common.RequestID(), result, nullStr(reason))
	if err != nil {
		log.Printf("audit system: %v", err)
	}
}

func (s *Server) handleDevRunScheduler(w http.ResponseWriter, r *http.Request) {
	var req struct {
		Now string `json:"now"`
	}
	_ = common.DecodeJSON(r, &req)
	now := s.now()
	if req.Now != "" {
		t, err := time.Parse(time.RFC3339, req.Now)
		if err != nil {
			common.WriteError(w, http.StatusBadRequest, "VALIDATION_ERROR", "now 需为 RFC3339")
			return
		}
		now = t
	}
	n, err := s.applyDue(r.Context(), now)
	if err != nil {
		internalErr(w, "dev scheduler", err)
		return
	}
	common.WriteJSON(w, http.StatusOK, map[string]int{"applied": n})
}

func kindIn(k string, kinds []string) bool {
	for _, x := range kinds {
		if x == k {
			return true
		}
	}
	return false
}

// changeModule is the audit module of a change kind.
func changeModule(kind string) string {
	switch kind {
	case kindEarnRate:
		return "earn_rate"
	case kindLoanRate:
		return "loan_rate"
	}
	return "fee"
}

// changeNotice builds the public announcement for an approved change.
func changeNotice(c *changeDTO, eff time.Time) (category, title, body string) {
	switch c.Kind {
	case kindEarnRate, kindLoanRate:
		return rateNotice(c, eff)
	}
	var p feePayload
	_ = json.Unmarshal(c.Payload, &p)
	return "fee", "交易手续费调整公告", feeNoticeBody(p, eff)
}

// applyChange pushes one approved, due change to its owning service.
func (s *Server) applyChange(ctx context.Context, id int64, kind string, payload []byte) error {
	switch kind {
	case kindEarnRate, kindLoanRate:
		return s.applyRateChange(ctx, kind, payload)
	}
	var p feePayload
	_ = json.Unmarshal(payload, &p)
	body := map[string]string{"spotMaker": p.SpotMaker, "spotTaker": p.SpotTaker, "by": "admin:change:" + strconv.FormatInt(id, 10)}
	u, err := s.call(ctx, http.MethodPut, s.cfg.OrderURL, "/internal/admin/fees", body)
	if err == nil && !u.ok() {
		err = fmt.Errorf("order %d %s", u.Status, u.errCode())
	}
	return err
}
