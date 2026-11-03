package main

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"net/http"
	"net/url"
	"strconv"
	"strings"
	"time"

	"github.com/linbit/platform/services/go/common"
	"github.com/shopspring/decimal"
)

// Earn (savings) and loan product management. Reads need earn:view, writes
// need earn:edit + 2FA and are audited at high level with before/after.
// Rates are never written directly: they go through a change request with the
// same dual approval (super_admin + finance_second), public notice and
// FeeNotice delay as trading-fee changes, and the scheduler applies them.

const (
	kindEarnRate = "earn_rate"
	kindLoanRate = "loan_rate"
)

var rateKinds = []string{kindEarnRate, kindLoanRate}

// writeReq decodes a JSON object body and pulls out totpCode / reason.
func writeReq(r *http.Request) (map[string]any, string, string, error) {
	m := map[string]any{}
	if r.ContentLength != 0 {
		if err := common.DecodeJSON(r, &m); err != nil {
			return nil, "", "", err
		}
	}
	code, _ := m["totpCode"].(string)
	reason, _ := m["reason"].(string)
	delete(m, "totpCode")
	delete(m, "reason")
	return m, code, reason, nil
}

// earnWrite forwards a 2FA-checked write to the earn service and audits it
// with the before/after the service returns.
func (s *Server) earnWrite(w http.ResponseWriter, r *http.Request, method, path, module, action, target string, body map[string]any, reason string) {
	a := actorOf(r)
	var b any
	if body != nil {
		b = body
	}
	u, err := s.call(r.Context(), method, s.cfg.EarnURL, path, b)
	if err != nil {
		upstreamErr(w, module+" "+action, err)
		return
	}
	result := "ok"
	var ba struct {
		Before json.RawMessage `json:"before"`
		After  json.RawMessage `json:"after"`
	}
	if !u.ok() {
		result = "failed:" + u.errCode()
	} else {
		_ = json.Unmarshal(u.Body, &ba)
	}
	var before, after any
	if len(ba.Before) > 0 && string(ba.Before) != "null" {
		before = ba.Before
	}
	if len(ba.After) > 0 && string(ba.After) != "null" {
		after = ba.After
	} else if u.ok() && body != nil {
		after = body
	}
	s.audit(r, a, module, action, target, levelHigh, result, before, after, reason)
	relay(w, u)
}

func (s *Server) earnRead(path string) http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		p := path
		for _, k := range []string{"id", "term", "asset"} {
			p = strings.ReplaceAll(p, "{"+k+"}", url.PathEscape(r.PathValue(k)))
		}
		if q := r.URL.RawQuery; q != "" {
			p += "?" + q
		}
		u, err := s.call(r.Context(), http.MethodGet, s.cfg.EarnURL, p, nil)
		if err != nil {
			upstreamErr(w, "earn read", err)
			return
		}
		relay(w, u)
	}
}

// guardedWrite wraps a write: decode, 2FA, forbid rate fields, then forward.
func (s *Server) guardedWrite(method, path, module, action string, prep func(r *http.Request, m map[string]any) (map[string]any, error)) http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		a := actorOf(r)
		m, code, reason, err := writeReq(r)
		if err != nil {
			common.WriteError(w, http.StatusBadRequest, "VALIDATION_ERROR", "请求格式错误")
			return
		}
		for _, k := range []string{"apr", "bonusApr", "hourlyRate"} {
			if _, ok := m[k]; ok && action != "create" {
				common.WriteError(w, http.StatusBadRequest, "RATE_CHANGE_REQUIRES_APPROVAL", "利率需通过 /admin/v1/rate-changes 提交双人审批")
				return
			}
		}
		if !s.verify2FA(w, a, code) {
			return
		}
		if prep != nil {
			if m, err = prep(r, m); err != nil {
				var ae *apiError
				if errors.As(err, &ae) {
					common.WriteError(w, ae.status, ae.code, ae.msg)
				} else {
					upstreamErr(w, module+" "+action, err)
				}
				return
			}
		}
		p := path
		target := module
		for _, k := range []string{"id", "term", "asset"} {
			if v := r.PathValue(k); v != "" {
				p = strings.ReplaceAll(p, "{"+k+"}", url.PathEscape(v))
				target += ":" + v
			}
		}
		if id, ok := m["id"].(string); ok && target == module {
			target += ":" + id
		}
		if as, ok := m["asset"].(string); ok && target == module {
			target += ":" + strings.ToUpper(as)
		}
		if method == http.MethodDelete {
			m = nil
		}
		s.earnWrite(w, r, method, p, module, action, target, m, reason)
	}
}

type apiError struct {
	status    int
	code, msg string
}

func (e *apiError) Error() string { return e.code }

// prepCreateProduct: create only (never overwrite an existing product's rate)
// and new products start offline unless explicitly put online.
func (s *Server) prepCreateProduct(r *http.Request, m map[string]any) (map[string]any, error) {
	id, _ := m["id"].(string)
	if strings.TrimSpace(id) == "" || strings.ContainsAny(id, "/?#% ") || len(id) > 64 {
		return nil, &apiError{http.StatusBadRequest, "VALIDATION_ERROR", "产品 id 不合法"}
	}
	u, err := s.call(r.Context(), http.MethodGet, s.cfg.EarnURL, "/internal/earn/products/"+url.PathEscape(id), nil)
	if err != nil {
		return nil, err
	}
	if u.ok() {
		return nil, &apiError{http.StatusConflict, "PRODUCT_EXISTS", "产品已存在；修改请用 PATCH，利率请走审批"}
	}
	if u.Status != http.StatusNotFound {
		return nil, fmt.Errorf("earn product lookup %d", u.Status)
	}
	for _, k := range []string{"apr", "bonusApr"} {
		if v, ok := m[k]; ok {
			if _, good := decStr(fmt.Sprint(v), "1", 6); !good {
				return nil, &apiError{http.StatusBadRequest, "VALIDATION_ERROR", "年化收益率需在 [0, 1] 之间（最多 6 位小数）"}
			}
		}
	}
	if _, ok := m["status"]; !ok {
		m["status"] = "offline"
	}
	return m, nil
}

func (s *Server) registerEarnRoutes(a func(pattern, perm string, h http.HandlerFunc)) {
	a("GET /admin/v1/earn/products", permEarnView, s.earnRead("/internal/earn/products"))
	a("GET /admin/v1/earn/products/{id}", permEarnView, s.earnRead("/internal/earn/products/{id}"))
	a("POST /admin/v1/earn/products", permEarnEdit, s.guardedWrite(http.MethodPost, "/internal/earn/products", "earn_product", "create", s.prepCreateProduct))
	a("PATCH /admin/v1/earn/products/{id}", permEarnEdit, s.guardedWrite(http.MethodPatch, "/internal/earn/products/{id}", "earn_product", "update", nil))
	a("PUT /admin/v1/earn/products/{id}/quota", permEarnEdit, s.guardedWrite(http.MethodPut, "/internal/earn/products/{id}/quota", "earn_product", "quota", nil))
	a("DELETE /admin/v1/earn/products/{id}", permEarnEdit, s.guardedWrite(http.MethodDelete, "/internal/earn/products/{id}", "earn_product", "delete", nil))

	a("GET /admin/v1/loans/terms", permEarnView, s.earnRead("/internal/loans/terms"))
	a("PATCH /admin/v1/loans/terms/{term}", permEarnEdit, s.guardedWrite(http.MethodPatch, "/internal/loans/terms/{term}", "loan_term", "update", nil))
	a("PUT /admin/v1/loans/terms/{term}/quota", permEarnEdit, s.guardedWrite(http.MethodPut, "/internal/loans/terms/{term}/quota", "loan_term", "quota", nil))
	a("GET /admin/v1/loans/collaterals", permEarnView, s.earnRead("/internal/loans/collaterals"))
	a("POST /admin/v1/loans/collaterals", permEarnEdit, s.guardedWrite(http.MethodPost, "/internal/loans/collaterals", "loan_collateral", "upsert", nil))
	a("DELETE /admin/v1/loans/collaterals/{asset}", permEarnEdit, s.guardedWrite(http.MethodDelete, "/internal/loans/collaterals/{asset}", "loan_collateral", "delete", nil))

	a("GET /admin/v1/rate-changes", permEarnView, s.handleListRateChanges)
	a("POST /admin/v1/rate-changes", permEarnEdit, s.handleSubmitRateChange)
	a("POST /admin/v1/rate-changes/{id}/approve", permEarnView, s.handleChangeDecision(rateKinds, "approve"))
	a("POST /admin/v1/rate-changes/{id}/reject", permEarnView, s.handleChangeDecision(rateKinds, "reject"))
	a("POST /admin/v1/rate-changes/{id}/rollback", permEarnEdit, s.handleRateRollback)
}

// ratePayload is the change-request payload for both rate kinds.
type ratePayload struct {
	ProductID  string `json:"productId,omitempty"`
	APR        string `json:"apr,omitempty"`
	BonusAPR   string `json:"bonusApr,omitempty"`
	Term       string `json:"term,omitempty"`
	HourlyRate string `json:"hourlyRate,omitempty"`
}

func decStr(v string, max string, places int32) (string, bool) {
	if strings.TrimSpace(v) == "" {
		return "", true
	}
	d, err := decimal.NewFromString(v)
	if err != nil || d.IsNegative() || d.GreaterThan(decimal.RequireFromString(max)) || d.Exponent() < -places {
		return "", false
	}
	return d.String(), true
}

// currentRate reads the live values for the fields a payload touches.
func (s *Server) currentRate(ctx context.Context, kind string, p ratePayload) (ratePayload, error) {
	if kind == kindEarnRate {
		u, err := s.call(ctx, http.MethodGet, s.cfg.EarnURL, "/internal/earn/products/"+url.PathEscape(p.ProductID), nil)
		if err != nil {
			return ratePayload{}, err
		}
		if u.Status == http.StatusNotFound {
			return ratePayload{}, &apiError{http.StatusNotFound, "PRODUCT_NOT_FOUND", "产品不存在"}
		}
		if !u.ok() {
			return ratePayload{}, fmt.Errorf("earn product %d", u.Status)
		}
		var cur struct {
			Kind     string           `json:"kind"`
			APR      decimal.Decimal  `json:"apr"`
			BonusAPR *decimal.Decimal `json:"bonusApr"`
		}
		if err := u.decode(&cur); err != nil {
			return ratePayload{}, err
		}
		if p.BonusAPR != "" && cur.Kind != "simple" {
			return ratePayload{}, &apiError{http.StatusBadRequest, "VALIDATION_ERROR", "只有活期产品有加息档"}
		}
		b := ratePayload{ProductID: p.ProductID}
		if p.APR != "" {
			b.APR = cur.APR.String()
		}
		if p.BonusAPR != "" {
			b.BonusAPR = "0"
			if cur.BonusAPR != nil {
				b.BonusAPR = cur.BonusAPR.String()
			}
		}
		return b, nil
	}
	u, err := s.call(ctx, http.MethodGet, s.cfg.EarnURL, "/internal/loans/terms", nil)
	if err != nil {
		return ratePayload{}, err
	}
	if !u.ok() {
		return ratePayload{}, fmt.Errorf("loan terms %d", u.Status)
	}
	var list struct {
		Items []struct {
			Term       string          `json:"term"`
			HourlyRate decimal.Decimal `json:"hourlyRate"`
		} `json:"items"`
	}
	if err := u.decode(&list); err != nil {
		return ratePayload{}, err
	}
	for _, t := range list.Items {
		if t.Term == p.Term {
			return ratePayload{Term: t.Term, HourlyRate: t.HourlyRate.String()}, nil
		}
	}
	return ratePayload{}, &apiError{http.StatusNotFound, "TERM_NOT_FOUND", "借款期限不存在"}
}

func (s *Server) createRateChange(ctx context.Context, a *actor, kind string, p ratePayload, reason string, reqEff *time.Time, rollbackOf *int64) (*changeDTO, error) {
	before, err := s.currentRate(ctx, kind, p)
	if err != nil {
		return nil, err
	}
	pj, _ := json.Marshal(p)
	bj, _ := json.Marshal(before)
	var id int64
	if err := s.db.QueryRowContext(ctx, `INSERT INTO change_requests (kind, payload, before, reason, submitted_by, requested_effective_at, rollback_of)
		VALUES ($1, $2, $3, $4, $5, $6, $7) RETURNING id`, kind, string(pj), string(bj), reason, a.ID, reqEff, rollbackOf).Scan(&id); err != nil {
		return nil, err
	}
	return s.getChange(ctx, id)
}

func writeRateErr(w http.ResponseWriter, where string, err error) {
	var ae *apiError
	if errors.As(err, &ae) {
		common.WriteError(w, ae.status, ae.code, ae.msg)
		return
	}
	internalErr(w, where, err)
}

// POST /admin/v1/rate-changes
//
//	{target:"earn_product", productId, apr?, bonusApr?, reason, effectiveAt?}
//	{target:"loan_term", term, hourlyRate, reason, effectiveAt?}
func (s *Server) handleSubmitRateChange(w http.ResponseWriter, r *http.Request) {
	a := actorOf(r)
	var req struct {
		Target      string `json:"target"`
		ProductID   string `json:"productId"`
		APR         string `json:"apr"`
		BonusAPR    string `json:"bonusApr"`
		Term        string `json:"term"`
		HourlyRate  string `json:"hourlyRate"`
		Reason      string `json:"reason"`
		EffectiveAt string `json:"effectiveAt"`
	}
	if err := common.DecodeJSON(r, &req); err != nil {
		common.WriteError(w, http.StatusBadRequest, "VALIDATION_ERROR", "请求格式错误")
		return
	}
	var kind string
	var p ratePayload
	switch req.Target {
	case "earn_product":
		apr, ok1 := decStr(req.APR, "1", 6)
		bonus, ok2 := decStr(req.BonusAPR, "1", 6)
		if !ok1 || !ok2 || (apr == "" && bonus == "") || strings.TrimSpace(req.ProductID) == "" || req.Term != "" || req.HourlyRate != "" {
			common.WriteError(w, http.StatusBadRequest, "VALIDATION_ERROR", "年化收益率需在 [0, 1] 之间（最多 6 位小数），且至少修改一项")
			return
		}
		kind, p = kindEarnRate, ratePayload{ProductID: req.ProductID, APR: apr, BonusAPR: bonus}
	case "loan_term":
		hr, ok := decStr(req.HourlyRate, "0.001", 12)
		if !ok || hr == "" || req.Term == "" || req.ProductID != "" || req.APR != "" || req.BonusAPR != "" {
			common.WriteError(w, http.StatusBadRequest, "VALIDATION_ERROR", "小时利率需在 [0, 0.001] 之间（最多 12 位小数）")
			return
		}
		kind, p = kindLoanRate, ratePayload{Term: req.Term, HourlyRate: hr}
	default:
		common.WriteError(w, http.StatusBadRequest, "VALIDATION_ERROR", "target 只能是 earn_product 或 loan_term")
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
	c, err := s.createRateChange(r.Context(), a, kind, p, req.Reason, eff, nil)
	if err != nil {
		writeRateErr(w, "submit rate change", err)
		return
	}
	s.audit(r, a, changeModule(kind), "submit_change", changeModule(kind)+"_change:"+strconv.FormatInt(c.ID, 10), levelHigh, "ok", c.Before, c.Payload, req.Reason)
	common.WriteJSON(w, http.StatusCreated, c)
}

func (s *Server) handleListRateChanges(w http.ResponseWriter, r *http.Request) {
	items, err := s.listChangesOf(r.Context(), rateKinds, r.URL.Query().Get("status"))
	if err != nil {
		internalErr(w, "list rate changes", err)
		return
	}
	common.WriteJSON(w, http.StatusOK, map[string]any{"items": items})
}

func (s *Server) handleRateRollback(w http.ResponseWriter, r *http.Request) {
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
	if err != nil || !kindIn(c.Kind, rateKinds) {
		common.WriteError(w, http.StatusNotFound, "NOT_FOUND", "变更不存在")
		return
	}
	if c.Status != "applied" || len(c.Before) == 0 {
		common.WriteError(w, http.StatusConflict, "NOT_APPLIED", "只能回滚已生效的变更")
		return
	}
	var p ratePayload
	_ = json.Unmarshal(c.Before, &p)
	nc, err := s.createRateChange(r.Context(), a, c.Kind, p, req.Reason, nil, &id)
	if err != nil {
		writeRateErr(w, "rate rollback", err)
		return
	}
	s.audit(r, a, changeModule(c.Kind), "submit_rollback", changeModule(c.Kind)+"_change:"+strconv.FormatInt(nc.ID, 10), levelHigh, "ok", nc.Before, nc.Payload, req.Reason)
	common.WriteJSON(w, http.StatusCreated, nc)
}

func pct(v string) string {
	d, err := decimal.NewFromString(v)
	if err != nil {
		return v
	}
	return d.Mul(decimal.NewFromInt(100)).String() + "%"
}

func rateNotice(c *changeDTO, eff time.Time) (string, string, string) {
	var p ratePayload
	_ = json.Unmarshal(c.Payload, &p)
	when := "，生效时间 " + eff.UTC().Format("2006-01-02 15:04 UTC") + "。"
	if c.Kind == kindLoanRate {
		hr, _ := decimal.NewFromString(p.HourlyRate)
		annual := hr.Mul(decimal.NewFromInt(24 * 365)).Mul(decimal.NewFromInt(100)).Round(4)
		return "loan", "借币利率调整公告", "借币期限 " + p.Term + " 的小时利率调整为 " + p.HourlyRate + "（约年化 " + annual.String() + "%），仅对生效后新借款生效" + when
	}
	var parts []string
	if p.APR != "" {
		parts = append(parts, "年化收益率调整为 "+pct(p.APR))
	}
	if p.BonusAPR != "" {
		parts = append(parts, "加息档年化调整为 "+pct(p.BonusAPR))
	}
	return "earn", "理财产品收益率调整公告", "理财产品 " + p.ProductID + " " + strings.Join(parts, "；") + when
}

func (s *Server) applyRateChange(ctx context.Context, kind string, payload []byte) error {
	var p ratePayload
	if err := json.Unmarshal(payload, &p); err != nil {
		return err
	}
	var path string
	body := map[string]any{}
	if kind == kindLoanRate {
		path = "/internal/loans/terms/" + url.PathEscape(p.Term) + "/rate"
		body["hourlyRate"] = p.HourlyRate
	} else {
		path = "/internal/earn/products/" + url.PathEscape(p.ProductID) + "/rate"
		if p.APR != "" {
			body["apr"] = p.APR
		}
		if p.BonusAPR != "" {
			body["bonusApr"] = p.BonusAPR
		}
	}
	u, err := s.call(ctx, http.MethodPut, s.cfg.EarnURL, path, body)
	if err == nil && !u.ok() {
		err = fmt.Errorf("earn %d %s", u.Status, u.errCode())
	}
	return err
}
