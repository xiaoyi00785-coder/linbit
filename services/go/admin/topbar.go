package main

// 顶栏：待办计数 + 通知流（只读）。
// 待办计数是全站共享数据，30 秒缓存一次（多个管理员同时在线也只查一次库）；
// 每项单独查询、单独超时，某张表不存在/超时只会让该项缺失，不影响其他项。
// 返回时按当前管理员权限过滤，前端只展示有权处理的项。

import (
	"log"
	"net/http"
	"sort"
	"sync"
	"time"

	"github.com/linbit/platform/services/go/common"
)

type todoDef struct {
	Key, Label, Route, Perm, SQL string
}

// 计数封顶 99（子查询 LIMIT 100），避免大表全量 count。
var todoDefs = []todoDef{
	{"withdraw_first", "提现初审", "withdraw", permWithdrawFirst,
		`SELECT count(*) FROM (SELECT 1 FROM withdrawals w WHERE w.status = 'pending_review' AND NOT EXISTS
		 (SELECT 1 FROM withdrawal_reviews r WHERE r.withdrawal_id = w.id::text AND r.status = 'awaiting_second') LIMIT 100) x`},
	{"withdraw_second", "提现复审", "withdraw", permWithdrawSecond,
		`SELECT count(*) FROM (SELECT 1 FROM withdrawal_reviews r JOIN withdrawals w ON w.id::text = r.withdrawal_id
		 WHERE r.status = 'awaiting_second' AND w.status = 'pending_review' LIMIT 100) x`},
	{"kyc", "KYC 待审核", "kyc", permKYCApprove, `SELECT count(*) FROM (SELECT 1 FROM kyc_applications WHERE status = 'pending' LIMIT 100) x`},
	{"adjust", "人工调账待复核", "adjust", permFundAdjustApprove, `SELECT count(*) FROM (SELECT 1 FROM balance_adjustments WHERE status = 'pending_approval' LIMIT 100) x`},
	{"fee", "费率变更待审批", "fees", permFeeApprove, `SELECT count(*) FROM (SELECT 1 FROM change_requests WHERE kind = 'fee_rates' AND status = 'pending' LIMIT 100) x`},
	{"rate", "利率变更待审批", "earn", permEarnEdit, `SELECT count(*) FROM (SELECT 1 FROM change_requests WHERE kind IN ('earn_rate','loan_rate') AND status = 'pending' LIMIT 100) x`},
	{"coupon", "卡券待财务审批", "coupons", permCouponApprove, `SELECT count(*) FROM (SELECT 1 FROM coupon_templates WHERE status = 'pending_finance' LIMIT 100) x`},
	{"reward", "奖励待财务审批", "rewards", permRewardApprove, `SELECT count(*) FROM (SELECT 1 FROM reward_grants WHERE status = 'pending_finance' LIMIT 100) x`},
	{"vip", "VIP 申请待审核", "vip", permVIPReview, `SELECT count(*) FROM (SELECT 1 FROM vip_applications WHERE status = 'pending' LIMIT 100) x`},
	{"support", "未分配工单", "cs", permSupportView, `SELECT count(*) FROM (SELECT 1 FROM support_tickets WHERE status <> 'closed' AND assignee_id IS NULL LIMIT 100) x`},
	{"risk", "风控命中待处理", "risk-rules", permRiskView, `SELECT count(*) FROM (SELECT 1 FROM admin_risk_hits WHERE status = 'open' LIMIT 100) x`},
}

var todoCache struct {
	sync.Mutex
	at     time.Time
	counts map[string]int64
}

func (s *Server) todoCounts(r *http.Request) map[string]int64 {
	todoCache.Lock()
	defer todoCache.Unlock()
	if todoCache.counts != nil && time.Since(todoCache.at) < 30*time.Second {
		return todoCache.counts
	}
	out := map[string]int64{}
	for _, d := range todoDefs {
		m, err := s.readOnlyMaps(r.Context(), "1500ms", d.SQL)
		if err != nil {
			log.Printf("todos %s: %v", d.Key, err)
			continue
		}
		if len(m) > 0 {
			switch v := m[0]["count"].(type) {
			case int64:
				out[d.Key] = v
			case int:
				out[d.Key] = int64(v)
			}
		}
	}
	todoCache.at, todoCache.counts = time.Now(), out
	return out
}

func (s *Server) registerTopbarRoutes(a func(pattern, perm string, h http.HandlerFunc)) {
	a("GET /admin/v1/todos", "", s.handleTodos)
	a("GET /admin/v1/inbox", "", s.handleInbox)
}

// GET /admin/v1/todos → {items:[{key,label,route,count}], total, cachedAt}
func (s *Server) handleTodos(w http.ResponseWriter, r *http.Request) {
	a := actorOf(r)
	counts := s.todoCounts(r)
	items := []map[string]interface{}{}
	var total int64
	for _, d := range todoDefs {
		c, ok := counts[d.Key]
		if !ok || !can(a.Roles, d.Perm) {
			continue
		}
		items = append(items, map[string]interface{}{"key": d.Key, "label": d.Label, "route": d.Route, "count": c})
		total += c
	}
	todoCache.Lock()
	at := todoCache.at
	todoCache.Unlock()
	common.WriteJSON(w, http.StatusOK, map[string]interface{}{"items": items, "total": total, "cachedAt": at.UTC().Format(time.RFC3339)})
}

// GET /admin/v1/inbox → 最近 7 天与当前管理员相关的通知（按权限过滤），最多 30 条。
// kind: risk(高危风控命中) | mine(我提交的申请已被处理) | solvency(偿付能力异常) | export(导出失败)
func (s *Server) handleInbox(w http.ResponseWriter, r *http.Request) {
	a := actorOf(r)
	ctx := r.Context()
	out := []map[string]interface{}{}
	add := func(perm, q string, args ...interface{}) {
		if perm != "" && !can(a.Roles, perm) {
			return
		}
		m, err := s.readOnlyMaps(ctx, "1500ms", q, args...)
		if err != nil {
			log.Printf("inbox: %v", err)
			return
		}
		out = append(out, m...)
	}
	add(permRiskView, `SELECT 'risk' AS kind, 'risk-'||id AS id, summary AS title, COALESCE(uid,'') AS uid, last_seen AS ts, 'risk-rules' AS route
		FROM admin_risk_hits WHERE status = 'open' AND severity = 'high' AND last_seen > now() - interval '7 days' ORDER BY last_seen DESC LIMIT 10`)
	add("", `SELECT 'mine' AS kind, 'cr-'||id AS id,
		(CASE kind WHEN 'fee_rates' THEN '费率变更' WHEN 'earn_rate' THEN '理财利率变更' WHEN 'loan_rate' THEN '借贷利率变更' ELSE kind END)
		|| ' #' || id || ' ' || (CASE status WHEN 'rejected' THEN '已驳回' WHEN 'scheduled' THEN '已通过，待生效' WHEN 'applied' THEN '已生效' ELSE status END) AS title,
		'' AS uid, updated_at AS ts, (CASE WHEN kind = 'fee_rates' THEN 'fees' ELSE 'earn' END) AS route
		FROM change_requests WHERE submitted_by = $1 AND status <> 'pending' AND updated_at > now() - interval '7 days' ORDER BY updated_at DESC LIMIT 10`, a.ID)
	add("", `SELECT 'mine' AS kind, 'adj-'||id AS id, '人工调账 '||ref||' '||(CASE status WHEN 'rejected' THEN '已驳回' WHEN 'applied' THEN '已入账' ELSE status END) AS title,
		user_id AS uid, reviewed_at AS ts, 'adjust' AS route
		FROM balance_adjustments WHERE created_by = $1 AND reviewed_at > now() - interval '7 days' ORDER BY reviewed_at DESC LIMIT 10`, a.ID)
	add("", `SELECT 'mine' AS kind, 'cp-'||id AS id, '卡券「'||name||'」'||(CASE status WHEN 'rejected' THEN '已驳回' ELSE '已审批' END) AS title,
		'' AS uid, updated_at AS ts, 'coupons' AS route
		FROM coupon_templates WHERE created_by = $1 AND reviewed_by IS NOT NULL AND updated_at > now() - interval '7 days' ORDER BY updated_at DESC LIMIT 10`, a.ID)
	add(permMMView, `SELECT 'solvency' AS kind, 'sol-'||id AS id, '偿付能力检查未通过（'||trigger||'）' AS title, '' AS uid, created_at AS ts, 'solvency' AS route
		FROM solvency_reports WHERE NOT ok AND created_at > now() - interval '48 hours' ORDER BY created_at DESC LIMIT 3`)
	add(permLedgerView, `SELECT 'export' AS kind, 'rx-24h' AS id, '近 24 小时 '||count(*)||' 个报表导出失败' AS title, '' AS uid, max(finished_at) AS ts, 'report-exports' AS route
		FROM report_exports WHERE status = 'failed' AND created_at > now() - interval '24 hours' HAVING count(*) > 0`)
	sort.SliceStable(out, func(i, j int) bool {
		ti, _ := out[i]["ts"].(string) // rowsToMaps: RFC3339 UTC, 字典序即时间序
		tj, _ := out[j]["ts"].(string)
		return ti > tj
	})
	if len(out) > 30 {
		out = out[:30]
	}
	common.WriteJSON(w, http.StatusOK, map[string]interface{}{"items": out})
}
