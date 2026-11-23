package main

// 风控规则：只读检测 + 告警。
//
// 每条规则是一类针对共享库的轻量查询（只扫小表/短时间窗口，带 statement_timeout），
// 命中写入 admin_risk_hits 供风控人工处理（已处理/忽略），可选 Telegram 告警。
// 规则不会自动冻结账户、拦截提现或改变任何资金流程；处置仍走现有的冻结/审核流程。

import (
	"context"
	"database/sql"
	"encoding/json"
	"errors"
	"fmt"
	"log"
	"math"
	"net/http"
	"os"
	"strconv"
	"strings"
	"sync"
	"time"

	"github.com/linbit/platform/services/go/common"
)

type paramSpec struct {
	Key   string  `json:"key"`
	Label string  `json:"label"`
	Type  string  `json:"type"` // num | text
	Min   float64 `json:"min,omitempty"`
	Max   float64 `json:"max,omitempty"`
}

var (
	pCooldown = paramSpec{"cooldownHours", "处理后静默（小时）", "num", 0, 720}
	pExclude  = paramSpec{"excludeUids", "排除 UID（逗号分隔）", "text", 0, 0}
	pLookback = paramSpec{"lookbackHours", "回看窗口（小时）", "num", 1, 168}
)

type riskKind struct {
	Label  string
	Specs  []paramSpec
	Detect func(ctx context.Context, tx *sql.Tx, p map[string]float64, txt map[string]string) ([]riskFinding, error)
}

type riskFinding struct {
	UID, Subject, Summary string
	Detail                json.RawMessage
}

const riskRowCap = 200

func scanFindings(rows *sql.Rows, err error) ([]riskFinding, error) {
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	out := []riskFinding{}
	for rows.Next() {
		var f riskFinding
		var d []byte
		if err := rows.Scan(&f.UID, &f.Subject, &f.Summary, &d); err != nil {
			return nil, err
		}
		f.Detail = d
		out = append(out, f)
	}
	return out, rows.Err()
}

func numStr(v float64) string { return strconv.FormatFloat(v, 'f', -1, 64) }

var riskKinds = map[string]riskKind{
	"large_withdrawal": {Label: "大额提现", Specs: []paramSpec{{"assets", "币种（逗号分隔）", "text", 0, 0}, {"minAmount", "单笔金额 ≥", "num", 0, 1e12}, pLookback, pCooldown, pExclude},
		Detect: func(ctx context.Context, tx *sql.Tx, p map[string]float64, t map[string]string) ([]riskFinding, error) {
			return scanFindings(tx.QueryContext(ctx, `SELECT user_id, 'withdrawal:'||id, format('提现 %s %s（%s）', trim_scale(amount)::text, asset, status),
				json_build_object('withdrawalId', id, 'asset', asset, 'network', network, 'amount', amount::text, 'status', status, 'riskScore', risk_score, 'createdAt', created_at)
				FROM withdrawals WHERE created_at > now() - make_interval(hours => $1::int) AND asset = ANY(string_to_array(upper(replace($2,' ','')), ','))
				AND amount >= $3::numeric AND NOT (user_id = ANY(string_to_array(replace($4,' ',''), ','))) ORDER BY id DESC LIMIT `+strconv.Itoa(riskRowCap),
				int(p["lookbackHours"]), t["assets"], numStr(p["minAmount"]), t["excludeUids"]))
		}},
	"withdraw_frequency": {Label: "高频提现", Specs: []paramSpec{{"minCount", "提现次数 ≥", "num", 2, 1000}, pLookback, pCooldown, pExclude},
		Detect: func(ctx context.Context, tx *sql.Tx, p map[string]float64, t map[string]string) ([]riskFinding, error) {
			return scanFindings(tx.QueryContext(ctx, `SELECT user_id, 'uid:'||user_id||':'||to_char(now(),'YYYYMMDD'), format('%s 小时内提现 %s 次', $1::int, count(*)),
				json_build_object('count', count(*), 'assets', string_agg(DISTINCT asset, ','), 'lastAt', max(created_at))
				FROM withdrawals WHERE created_at > now() - make_interval(hours => $1::int) AND NOT (user_id = ANY(string_to_array(replace($3,' ',''), ',')))
				GROUP BY user_id HAVING count(*) >= $2::int ORDER BY count(*) DESC LIMIT `+strconv.Itoa(riskRowCap),
				int(p["lookbackHours"]), int(p["minCount"]), t["excludeUids"]))
		}},
	"quick_out": {Label: "充值后快速提现", Specs: []paramSpec{{"maxHours", "充值后（小时）内提现", "num", 0.25, 72}, pLookback, pCooldown, pExclude},
		Detect: func(ctx context.Context, tx *sql.Tx, p map[string]float64, t map[string]string) ([]riskFinding, error) {
			return scanFindings(tx.QueryContext(ctx, `SELECT w.user_id, 'withdrawal:'||w.id,
				format('充值 %s %s 后 %s 分钟提现 %s %s', trim_scale(d.amount)::text, d.asset, round(extract(epoch from w.created_at - d.created_at)/60), trim_scale(w.amount)::text, w.asset),
				json_build_object('withdrawalId', w.id, 'depositId', d.id, 'depositAt', d.created_at, 'withdrawAt', w.created_at, 'withdrawAsset', w.asset, 'withdrawAmount', w.amount::text)
				FROM withdrawals w JOIN LATERAL (SELECT id, asset, amount, created_at FROM deposits d WHERE d.user_id = w.user_id AND d.status = 'completed'
				  AND d.created_at <= w.created_at AND d.created_at > w.created_at - make_interval(secs => $2::float8 * 3600) ORDER BY d.created_at DESC LIMIT 1) d ON true
				WHERE w.created_at > now() - make_interval(hours => $1::int) AND NOT (w.user_id = ANY(string_to_array(replace($3,' ',''), ',')))
				ORDER BY w.id DESC LIMIT `+strconv.Itoa(riskRowCap), int(p["lookbackHours"]), p["maxHours"], t["excludeUids"]))
		}},
	"large_position": {Label: "大额持仓", Specs: []paramSpec{{"minNotional", "名义价值 ≥（USDT）", "num", 0, 1e12}, pCooldown, pExclude},
		Detect: func(ctx context.Context, tx *sql.Tx, p map[string]float64, t map[string]string) ([]riskFinding, error) {
			return scanFindings(tx.QueryContext(ctx, `SELECT user_id, 'pos:'||user_id||':'||symbol, format('%s %s 名义 %s USDT，%sx', symbol, CASE WHEN qty > 0 THEN '多' ELSE '空' END, round(abs(qty)*entry_price), leverage),
				json_build_object('symbol', symbol, 'qty', qty::text, 'entryPrice', entry_price::text, 'leverage', leverage, 'marginMode', margin_mode, 'notional', round(abs(qty)*entry_price, 2)::text)
				FROM fut_positions WHERE qty <> 0 AND user_id ~ '^[0-9]+$' AND abs(qty)*entry_price >= $1::numeric
				AND NOT (user_id = ANY(string_to_array(replace($2,' ',''), ','))) ORDER BY abs(qty)*entry_price DESC LIMIT `+strconv.Itoa(riskRowCap), numStr(p["minNotional"]), t["excludeUids"]))
		}},
	"high_leverage": {Label: "高杠杆持仓", Specs: []paramSpec{{"minLeverage", "杠杆 ≥", "num", 2, 200}, {"minNotional", "名义价值 ≥（USDT）", "num", 0, 1e12}, pCooldown, pExclude},
		Detect: func(ctx context.Context, tx *sql.Tx, p map[string]float64, t map[string]string) ([]riskFinding, error) {
			return scanFindings(tx.QueryContext(ctx, `SELECT user_id, 'lev:'||user_id||':'||symbol, format('%s %sx 名义 %s USDT', symbol, leverage, round(abs(qty)*entry_price)),
				json_build_object('symbol', symbol, 'qty', qty::text, 'entryPrice', entry_price::text, 'leverage', leverage, 'marginMode', margin_mode)
				FROM fut_positions WHERE qty <> 0 AND user_id ~ '^[0-9]+$' AND leverage >= $1::int AND abs(qty)*entry_price >= $2::numeric
				AND NOT (user_id = ANY(string_to_array(replace($3,' ',''), ','))) ORDER BY leverage DESC LIMIT `+strconv.Itoa(riskRowCap), int(p["minLeverage"]), numStr(p["minNotional"]), t["excludeUids"]))
		}},
	"liquidation_burst": {Label: "连续强平", Specs: []paramSpec{{"minCount", "强平次数 ≥", "num", 2, 1000}, pLookback, pCooldown, pExclude},
		Detect: func(ctx context.Context, tx *sql.Tx, p map[string]float64, t map[string]string) ([]riskFinding, error) {
			return scanFindings(tx.QueryContext(ctx, `SELECT user_id, 'uid:'||user_id||':'||to_char(date_trunc('hour', now()),'YYYYMMDDHH24'), format('%s 小时内被强平 %s 次，穿仓 %s', $1::int, count(*), trim_scale(sum(shortfall))::text),
				json_build_object('count', count(*), 'symbols', string_agg(DISTINCT symbol, ','), 'shortfall', sum(shortfall)::text, 'lastAt', max(created_at))
				FROM fut_liquidations WHERE created_at > now() - make_interval(hours => $1::int) AND NOT (user_id = ANY(string_to_array(replace($3,' ',''), ',')))
				GROUP BY user_id HAVING count(*) >= $2::int ORDER BY count(*) DESC LIMIT `+strconv.Itoa(riskRowCap), int(p["lookbackHours"]), int(p["minCount"]), t["excludeUids"]))
		}},
	"apikey_no_ip": {Label: "API Key 未绑定 IP", Specs: []paramSpec{pCooldown, pExclude},
		Detect: func(ctx context.Context, tx *sql.Tx, p map[string]float64, t map[string]string) ([]riskFinding, error) {
			return scanFindings(tx.QueryContext(ctx, `SELECT u.uid::text, 'apikey:'||k.id, format('API Key #%s（%s）未设置 IP 白名单', k.id, COALESCE(NULLIF(k.label,''),'无备注')),
				json_build_object('apiKeyId', k.id, 'label', k.label, 'createdAt', k.created_at, 'lastUsedAt', k.last_used_at)
				FROM user_api_keys k JOIN users u ON u.id = k.user_id WHERE k.revoked_at IS NULL AND k.ip_allowlist = ''
				AND NOT (u.uid::text = ANY(string_to_array(replace($1,' ',''), ','))) ORDER BY k.id DESC LIMIT `+strconv.Itoa(riskRowCap), t["excludeUids"]))
		}},
}

type riskRule struct {
	Code        string                 `json:"code"`
	Name        string                 `json:"name"`
	Kind        string                 `json:"kind"`
	Description string                 `json:"description"`
	Params      map[string]interface{} `json:"params"`
	Severity    string                 `json:"severity"`
	Enabled     bool                   `json:"enabled"`
	Notify      bool                   `json:"notify"`
	LastRunAt   *string                `json:"lastRunAt"`
	LastHits    int                    `json:"lastHits"`
	LastError   string                 `json:"lastError"`
	UpdatedBy   string                 `json:"updatedBy"`
	UpdatedAt   string                 `json:"updatedAt"`
	OpenHits    int                    `json:"openHits"`
	Specs       []paramSpec            `json:"specs"`
}

func (s *Server) loadRiskRules(ctx context.Context, code string) ([]riskRule, error) {
	q := `SELECT r.code, r.name, r.kind, r.description, r.params, r.severity, r.enabled, r.notify, r.last_run_at, r.last_hits, r.last_error, r.updated_by, r.updated_at,
		(SELECT count(*) FROM admin_risk_hits h WHERE h.rule_code = r.code AND h.status = 'open')
		FROM admin_risk_rules r`
	args := []interface{}{}
	if code != "" {
		q += ` WHERE r.code = $1`
		args = append(args, code)
	}
	rows, err := s.db.QueryContext(ctx, q+` ORDER BY r.code`, args...)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	out := []riskRule{}
	for rows.Next() {
		var rr riskRule
		var params []byte
		var last sql.NullTime
		var upd time.Time
		if err := rows.Scan(&rr.Code, &rr.Name, &rr.Kind, &rr.Description, &params, &rr.Severity, &rr.Enabled, &rr.Notify, &last, &rr.LastHits, &rr.LastError, &rr.UpdatedBy, &upd, &rr.OpenHits); err != nil {
			return nil, err
		}
		_ = json.Unmarshal(params, &rr.Params)
		if last.Valid {
			t := fmtTime(last.Time)
			rr.LastRunAt = &t
		}
		rr.UpdatedAt = fmtTime(upd)
		rr.Specs = riskKinds[rr.Kind].Specs
		out = append(out, rr)
	}
	return out, rows.Err()
}

// normParams validates params against the kind's specs; unknown keys are rejected.
func normParams(kind string, in map[string]interface{}) (map[string]interface{}, map[string]float64, map[string]string, error) {
	k, ok := riskKinds[kind]
	if !ok {
		return nil, nil, nil, fmt.Errorf("未知规则类型 %s", kind)
	}
	out, nums, txt := map[string]interface{}{}, map[string]float64{}, map[string]string{}
	specs := map[string]paramSpec{}
	for _, sp := range k.Specs {
		specs[sp.Key] = sp
	}
	for key := range in {
		if _, ok := specs[key]; !ok {
			return nil, nil, nil, fmt.Errorf("未知参数 %s", key)
		}
	}
	for _, sp := range k.Specs {
		v := in[sp.Key]
		if sp.Type == "text" {
			s := ""
			if v != nil {
				s = strings.TrimSpace(fmt.Sprint(v))
			}
			if len(s) > 500 || strings.ContainsAny(s, "'\";\\") {
				return nil, nil, nil, fmt.Errorf("%s 格式错误", sp.Label)
			}
			out[sp.Key], txt[sp.Key] = s, s
			continue
		}
		var f float64
		switch x := v.(type) {
		case float64:
			f = x
		case string:
			var err error
			if f, err = strconv.ParseFloat(strings.TrimSpace(x), 64); err != nil {
				return nil, nil, nil, fmt.Errorf("%s 需为数字", sp.Label)
			}
		case nil:
			return nil, nil, nil, fmt.Errorf("缺少参数 %s", sp.Label)
		default:
			return nil, nil, nil, fmt.Errorf("%s 需为数字", sp.Label)
		}
		if math.IsNaN(f) || f < sp.Min || (sp.Max > 0 && f > sp.Max) {
			return nil, nil, nil, fmt.Errorf("%s 超出范围 [%s, %s]", sp.Label, numStr(sp.Min), numStr(sp.Max))
		}
		out[sp.Key], nums[sp.Key] = f, f
	}
	if kind == "large_withdrawal" && txt["assets"] == "" {
		return nil, nil, nil, errors.New("请填写币种")
	}
	return out, nums, txt, nil
}

var riskRunMu sync.Mutex

type riskRunResult struct {
	Rules    int      `json:"rules"`
	Findings int      `json:"findings"`
	NewHits  int      `json:"newHits"`
	Errors   []string `json:"errors"`
}

// runRiskRules evaluates every enabled rule once.
func (s *Server) runRiskRules(ctx context.Context, trigger string) riskRunResult {
	riskRunMu.Lock()
	defer riskRunMu.Unlock()
	res := riskRunResult{Errors: []string{}}
	rules, err := s.loadRiskRules(ctx, "")
	if err != nil {
		res.Errors = append(res.Errors, "load rules: "+err.Error())
		return res
	}
	alerts := []string{}
	for _, rr := range rules {
		if !rr.Enabled {
			continue
		}
		res.Rules++
		k, ok := riskKinds[rr.Kind]
		var findings []riskFinding
		var nums map[string]float64
		var txt map[string]string
		if ok {
			_, nums, txt, err = normParams(rr.Kind, rr.Params)
		} else {
			err = fmt.Errorf("未知规则类型 %s", rr.Kind)
		}
		if err == nil {
			findings, err = s.detectReadOnly(ctx, k, nums, txt)
		}
		newHits := 0
		if err == nil {
			for _, f := range findings {
				inserted, ierr := s.upsertRiskHit(ctx, rr, f, nums["cooldownHours"])
				if ierr != nil {
					err = ierr
					break
				}
				if inserted {
					newHits++
					if rr.Notify && len(alerts) < 15 {
						alerts = append(alerts, fmt.Sprintf("[%s] UID %s · %s", rr.Name, f.UID, f.Summary))
					}
				}
			}
		}
		lastErr := ""
		if err != nil {
			lastErr = err.Error()
			res.Errors = append(res.Errors, rr.Code+": "+lastErr)
			log.Printf("risk rule %s: %v", rr.Code, err)
		}
		res.Findings += len(findings)
		res.NewHits += newHits
		_, _ = s.db.ExecContext(ctx, `UPDATE admin_risk_rules SET last_run_at = now(), last_hits = $2, last_error = $3 WHERE code = $1`, rr.Code, len(findings), lastErr)
	}
	if len(alerts) > 0 {
		common.AlertTelegram("⚠️ Linbit 风控规则新命中（" + trigger + "）：\n" + strings.Join(alerts, "\n"))
	}
	return res
}

func (s *Server) detectReadOnly(ctx context.Context, k riskKind, nums map[string]float64, txt map[string]string) ([]riskFinding, error) {
	tx, err := s.db.BeginTx(ctx, &sql.TxOptions{ReadOnly: true})
	if err != nil {
		return nil, err
	}
	defer tx.Rollback()
	if _, err := tx.ExecContext(ctx, `SET LOCAL statement_timeout = '5s'`); err != nil {
		return nil, err
	}
	return k.Detect(ctx, tx, nums, txt)
}

func (s *Server) upsertRiskHit(ctx context.Context, rr riskRule, f riskFinding, cooldown float64) (bool, error) {
	detail := f.Detail
	if len(detail) == 0 {
		detail = json.RawMessage(`{}`)
	}
	var inserted bool
	err := s.db.QueryRowContext(ctx, `
		INSERT INTO admin_risk_hits (rule_code, uid, subject, summary, detail, severity)
		SELECT $1, $2, $3, $4, $5::jsonb, $6
		WHERE NOT EXISTS (SELECT 1 FROM admin_risk_hits WHERE rule_code = $1 AND subject = $3 AND status <> 'open'
		                  AND handled_at > now() - make_interval(secs => $7::float8 * 3600))
		ON CONFLICT (rule_code, subject) WHERE status = 'open'
		DO UPDATE SET last_seen = now(), hit_count = admin_risk_hits.hit_count + 1, summary = EXCLUDED.summary, detail = EXCLUDED.detail
		RETURNING (xmax = 0)`, rr.Code, f.UID, f.Subject, f.Summary, string(detail), rr.Severity, cooldown).Scan(&inserted)
	if errors.Is(err, sql.ErrNoRows) {
		return false, nil // within cooldown after manual handling
	}
	return inserted, err
}

// RunRiskScheduler evaluates the rules every ADMIN_RISK_INTERVAL_MIN minutes (default 10, 0 = off).
func (s *Server) RunRiskScheduler(ctx context.Context) {
	mins := 10
	if v := strings.TrimSpace(os.Getenv("ADMIN_RISK_INTERVAL_MIN")); v != "" {
		if n, err := strconv.Atoi(v); err == nil && n >= 0 {
			mins = n
		}
	}
	if mins == 0 {
		log.Printf("risk rules: scheduler disabled")
		return
	}
	select {
	case <-ctx.Done():
		return
	case <-time.After(90 * time.Second):
	}
	t := time.NewTicker(time.Duration(mins) * time.Minute)
	defer t.Stop()
	for {
		c, cancel := context.WithTimeout(ctx, 2*time.Minute)
		res := s.runRiskRules(c, "定时")
		cancel()
		if len(res.Errors) > 0 {
			log.Printf("risk rules: %d errors", len(res.Errors))
		}
		select {
		case <-ctx.Done():
			return
		case <-t.C:
		}
	}
}

func (s *Server) registerRiskRoutes(a func(pattern, perm string, h http.HandlerFunc)) {
	a("GET /admin/v1/risk/rules", permRiskView, s.handleRiskRules)
	a("PUT /admin/v1/risk/rules/{code}", permRiskEdit, s.handlePutRiskRule)
	a("POST /admin/v1/risk/run", permRiskEdit, s.handleRiskRun)
	a("GET /admin/v1/risk/hits", permRiskView, s.handleRiskHits)
	a("POST /admin/v1/risk/hits/{id}", permRiskEdit, s.handleRiskHitHandle)
}

func (s *Server) handleRiskRules(w http.ResponseWriter, r *http.Request) {
	items, err := s.loadRiskRules(r.Context(), "")
	if err != nil {
		internalErr(w, "risk rules", err)
		return
	}
	common.WriteJSON(w, http.StatusOK, map[string]interface{}{"items": items})
}

func (s *Server) handlePutRiskRule(w http.ResponseWriter, r *http.Request) {
	a := actorOf(r)
	code := r.PathValue("code")
	body, totpCode, reason, err := writeReq(r)
	if err != nil {
		common.WriteError(w, http.StatusBadRequest, "VALIDATION_ERROR", "请求格式错误")
		return
	}
	cur, err := s.loadRiskRules(r.Context(), code)
	if err != nil {
		internalErr(w, "load risk rule", err)
		return
	}
	if len(cur) == 0 {
		common.WriteError(w, http.StatusNotFound, "NOT_FOUND", "规则不存在")
		return
	}
	before := cur[0]
	next := before
	if v, ok := body["enabled"].(bool); ok {
		next.Enabled = v
	}
	if v, ok := body["notify"].(bool); ok {
		next.Notify = v
	}
	if v, ok := body["severity"].(string); ok {
		if v != "low" && v != "medium" && v != "high" {
			common.WriteError(w, http.StatusBadRequest, "VALIDATION_ERROR", "级别只能是 low/medium/high")
			return
		}
		next.Severity = v
	}
	params := before.Params
	if v, ok := body["params"].(map[string]interface{}); ok {
		merged := map[string]interface{}{}
		for k, x := range before.Params {
			merged[k] = x
		}
		for k, x := range v {
			merged[k] = x
		}
		params = merged
	}
	norm, _, _, perr := normParams(before.Kind, params)
	if perr != nil {
		common.WriteError(w, http.StatusBadRequest, "VALIDATION_ERROR", perr.Error())
		return
	}
	if strings.TrimSpace(reason) == "" {
		common.WriteError(w, http.StatusBadRequest, "REASON_REQUIRED", "必须填写原因")
		return
	}
	if !s.verify2FA(w, a, totpCode) {
		return
	}
	pj, _ := json.Marshal(norm)
	if _, err := s.db.ExecContext(r.Context(), `UPDATE admin_risk_rules SET enabled = $2, notify = $3, severity = $4, params = $5::jsonb, updated_by = $6, updated_at = now() WHERE code = $1`,
		code, next.Enabled, next.Notify, next.Severity, string(pj), a.Username); err != nil {
		internalErr(w, "put risk rule", err)
		return
	}
	after, _ := s.loadRiskRules(r.Context(), code)
	snap := func(x riskRule) map[string]interface{} {
		return map[string]interface{}{"enabled": x.Enabled, "notify": x.Notify, "severity": x.Severity, "params": x.Params}
	}
	var afterSnap interface{}
	var out interface{} = nil
	if len(after) > 0 {
		afterSnap, out = snap(after[0]), after[0]
	}
	s.audit(r, a, "risk", "rule_update", "risk_rule:"+code, levelSensitive, "ok", snap(before), afterSnap, reason)
	common.WriteJSON(w, http.StatusOK, out)
}

func (s *Server) handleRiskRun(w http.ResponseWriter, r *http.Request) {
	a := actorOf(r)
	ctx, cancel := context.WithTimeout(r.Context(), 60*time.Second)
	defer cancel()
	res := s.runRiskRules(ctx, "manual:"+a.Username)
	s.audit(r, a, "risk", "run", "risk_rules", levelNormal, "ok", nil, res, "")
	common.WriteJSON(w, http.StatusOK, res)
}

func (s *Server) handleRiskHits(w http.ResponseWriter, r *http.Request) {
	f := &filter{}
	q := r.URL.Query()
	if v := q.Get("status"); v != "" && v != "all" {
		f.add("h.status = ?", v)
	}
	f.eq(r, "rule", "h.rule_code")
	f.eq(r, "uid", "h.uid")
	f.eq(r, "severity", "h.severity")
	stats := map[string]interface{}{}
	if m, err := s.queryMaps(r.Context(), `SELECT count(*) FILTER (WHERE status = 'open') AS open,
		count(*) FILTER (WHERE status = 'open' AND severity = 'high') AS "openHigh",
		count(*) FILTER (WHERE status <> 'open' AND handled_at > now() - interval '24 hours') AS "handled24h",
		count(*) FILTER (WHERE first_seen > now() - interval '24 hours') AS "new24h" FROM admin_risk_hits`); err == nil && len(m) > 0 {
		stats = m[0]
	}
	s.pagedList(w, r, "risk hits", `h.id, h.rule_code AS "ruleCode", r.name AS "ruleName", h.uid, h.subject, h.summary, h.detail, h.severity, h.status,
		h.hit_count AS "hitCount", h.first_seen AS "firstSeen", h.last_seen AS "lastSeen", h.handled_by AS "handledBy", h.handled_at AS "handledAt", h.note`,
		`admin_risk_hits h JOIN admin_risk_rules r ON r.code = h.rule_code`, "(h.status = 'open') DESC, h.last_seen DESC", f, map[string]interface{}{"stats": stats})
}

func (s *Server) handleRiskHitHandle(w http.ResponseWriter, r *http.Request) {
	a := actorOf(r)
	id, ok := pathID(r)
	if !ok {
		common.WriteError(w, http.StatusBadRequest, "VALIDATION_ERROR", "id 无效")
		return
	}
	body, _, reason, err := writeReq(r)
	if err != nil {
		common.WriteError(w, http.StatusBadRequest, "VALIDATION_ERROR", "请求格式错误")
		return
	}
	status, _ := body["status"].(string)
	if status != "ack" && status != "ignored" {
		common.WriteError(w, http.StatusBadRequest, "VALIDATION_ERROR", "status 只能是 ack/ignored")
		return
	}
	if strings.TrimSpace(reason) == "" {
		common.WriteError(w, http.StatusBadRequest, "REASON_REQUIRED", "请填写处理说明")
		return
	}
	var rule, uid, subject string
	err = s.db.QueryRowContext(r.Context(), `UPDATE admin_risk_hits SET status = $2, handled_by = $3, handled_at = now(), note = $4
		WHERE id = $1 AND status = 'open' RETURNING rule_code, uid, subject`, id, status, a.Username, strings.TrimSpace(reason)).Scan(&rule, &uid, &subject)
	if errors.Is(err, sql.ErrNoRows) {
		common.WriteError(w, http.StatusConflict, "NOT_OPEN", "该命中不存在或已处理")
		return
	} else if err != nil {
		internalErr(w, "handle risk hit", err)
		return
	}
	s.audit(r, a, "risk", "hit_"+status, "risk_hit:"+strconv.FormatInt(id, 10)+" user:"+uid, levelNormal, "ok", map[string]string{"status": "open"},
		map[string]string{"status": status, "rule": rule, "subject": subject}, reason)
	common.WriteJSON(w, http.StatusOK, map[string]string{"status": status})
}
