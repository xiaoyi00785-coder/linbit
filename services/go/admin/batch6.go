package main

// batch6.go (t84): 后台补齐
//   - 网格强停（strategy internal 接口，strategy 未部署新版本前返回明确提示）
//   - 资金类配置双人审批（staking 产品、launchpad 项目及结算/派发/取消、做市参数与基线、闪兑配置）
//   - launchpad / staking / NFT / options / web3wallet / paper / DCA / IM 红包与群组 查看页
//   - App 版本升级配置（iOS / Android）、帮助中心与活动文案、App 推送（设备来自 compat 注册表）

import (
	"context"
	"database/sql"
	"encoding/json"
	"errors"
	"net/http"
	"net/url"
	"os"
	"path/filepath"
	"regexp"
	"sort"
	"strconv"
	"strings"
	"time"

	"github.com/linbit/platform/services/go/common"
)

const (
	permBizView         = "biz:view"
	permBizEdit         = "biz:edit"
	permConfigApprove   = "config:approve"
	permStrategyForce   = "strategy:force_stop"
	kindFundConfig      = "fund_config"
	fundChangeModule    = "fund_config"
	errFundPendingTitle = "已提交双人审批"
)

func (s *Server) svcURL(svc string) string {
	switch svc {
	case "staking":
		return common.Env("STAKING_INTERNAL_URL", "http://staking:8095")
	case "launchpad":
		return common.Env("LAUNCHPAD_INTERNAL_URL", "http://launchpad:8096")
	case "nft":
		return common.Env("NFT_INTERNAL_URL", "http://nft:8095")
	case "strategy":
		return common.Env("STRATEGY_INTERNAL_URL", "http://strategy:8090")
	case "mm":
		return s.cfg.MMURL
	case "asset":
		return s.cfg.AssetURL
	}
	return ""
}

func (s *Server) registerBatch6Routes(a func(pattern, perm string, h http.HandlerFunc)) {
	// 网格强停
	a("POST /admin/v1/grids/{id}/force-stop", permStrategyForce, s.handleGridForceStop)
	// 资金类配置双人审批
	a("GET /admin/v1/fund-changes", permBizView, s.handleListFundChanges)
	a("POST /admin/v1/fund-changes", permBizView, s.handleSubmitFundChange)
	a("POST /admin/v1/fund-changes/{id}/approve", permConfigApprove, s.handleFundDecision("approve"))
	a("POST /admin/v1/fund-changes/{id}/reject", permConfigApprove, s.handleFundDecision("reject"))
	// 业务模块查看
	a("GET /admin/v1/biz/staking/products", permBizView, s.bizList(`id, asset, chain, validator, apr::text AS apr, unbonding_days AS "unbondingDays",
		min_amount::text AS "minAmount", max_amount::text AS "maxAmount", total_quota::text AS "totalQuota", sold::text AS sold,
		commission::text AS commission, logo_url AS "logoUrl", status, hot, sort`, "s_products", "sort, id", map[string]string{"status": "status", "asset": "asset"}))
	a("GET /admin/v1/biz/staking/positions", permBizView, s.bizList(`id, uid, product_id AS "productId", asset, amount::text AS amount, apr_snapshot::text AS apr,
		status, started_at AS "startedAt", unbond_at AS "unbondAt", released_at AS "releasedAt", pending_rewards::text AS "pendingRewards",
		claimed_rewards::text AS "claimedRewards"`, "s_positions", "id DESC", map[string]string{"uid": "uid", "product": "product_id", "status": "status"}))
	a("GET /admin/v1/biz/launchpad/projects", permBizView, s.bizList(`id, symbol, name, base_asset AS "baseAsset", price::text AS price,
		total_supply::text AS "totalSupply", min_sub::text AS "minSub", max_sub::text AS "maxSub", start_at AS "startAt", end_at AS "endAt",
		settle_at AS "settleAt", distribute_at AS "distributeAt", status, allocated::text AS allocated, raised::text AS raised,
		settled_at AS "settledAt", distributed_at AS "distributedAt", sort, logo, description, rules_md AS "rulesMd",
		(SELECT count(*) FROM launchpad_subscriptions x WHERE x.project_id = launchpad_projects.id) AS subscriptions`,
		"launchpad_projects", "start_at DESC", map[string]string{"status": "status"}))
	a("GET /admin/v1/biz/launchpad/subscriptions", permBizView, s.bizList(`id, uid, project_id AS "projectId", amount::text AS amount,
		result_amount::text AS "resultAmount", used_base::text AS "usedBase", refund_amount::text AS "refundAmount", status,
		locked_at AS "lockedAt", settled_at AS "settledAt", distributed_at AS "distributedAt"`, "launchpad_subscriptions", "id DESC",
		map[string]string{"uid": "uid", "project": "project_id", "status": "status"}))
	a("GET /admin/v1/biz/nft/collections", permBizView, s.bizList(`id, slug, name, category, creator_id AS "creatorId", royalty_bps AS "royaltyBps",
		pay_asset AS "payAsset", item_count AS "itemCount", owner_count AS "ownerCount", floor_price::text AS "floorPrice",
		volume_total::text AS "volumeTotal", sales_total AS "salesTotal", status, created_at AS "createdAt"`, "nft_collections", "id DESC",
		map[string]string{"status": "status"}))
	a("GET /admin/v1/biz/nft/trades", permBizView, s.bizList(`id, item_id AS "itemId", collection_id AS "collectionId", seller_id AS seller,
		buyer_id AS buyer, price::text AS price, asset, royalty_amount::text AS royalty, source, created_at AS "createdAt"`, "nft_trades", "id DESC",
		map[string]string{"uid": "buyer_id", "collection": "collection_id"}))
	a("PATCH /admin/v1/biz/nft/collections/{slug}", permBizEdit, s.handleNFTCollectionStatus)
	a("GET /admin/v1/biz/options/instruments", permBizView, s.bizList(`id, name, underlying, quote, option_type AS "optionType", strike::text AS strike,
		expiry, contract_size::text AS "contractSize", min_qty::text AS "minQty", status, settlement_price::text AS "settlementPrice"`,
		"opt_instruments", "expiry DESC, id", map[string]string{"status": "status", "underlying": "underlying"}))
	a("GET /admin/v1/biz/options/positions", permBizView, s.bizList(`user_id AS uid, instrument_id AS "instrumentId", qty::text AS qty,
		avg_price::text AS "avgPrice", realized_pnl::text AS "realizedPnl", margin::text AS margin, settled, updated_at AS "updatedAt"`,
		"opt_positions", "updated_at DESC", map[string]string{"uid": "user_id"}))
	a("GET /admin/v1/biz/options/orders", permBizView, s.bizList(`id, user_id AS uid, instrument_id AS "instrumentId", side, order_type AS "orderType",
		price::text AS price, qty::text AS qty, filled_qty::text AS "filledQty", status, reject_reason AS "rejectReason", created_at AS "createdAt"`,
		"opt_orders", "id DESC", map[string]string{"uid": "user_id", "status": "status"}))
	a("GET /admin/v1/biz/options/config", permBizView, s.bizList(`id, risk_free_rate::text AS "riskFreeRate", fee_rate::text AS "feeRate",
		min_premium::text AS "minPremium", max_order_qty::text AS "maxOrderQty", settle_grace_s AS "settleGraceS", updated_at AS "updatedAt"`,
		"opt_config", "id", nil))
	// web3wallet: 非托管钱包，绝不读取 encrypted_mnemonic。
	a("GET /admin/v1/biz/web3/wallets", permBizView, s.bizList(`uid, pin_version AS "pinVersion", pin_failures AS "pinFailures",
		locked_until AS "lockedUntil", backup_at AS "backupAt", created_at AS "createdAt"`, "web3_wallets", "created_at DESC", map[string]string{"uid": "uid"}))
	a("GET /admin/v1/biz/web3/transactions", permBizView, s.bizList(`id, uid, chain, tx_hash AS "txHash", from_addr AS "from", to_addr AS "to",
		value::text AS value, token, status, kind, created_at AS "createdAt", confirmed_at AS "confirmedAt"`, "web3_transactions", "id DESC",
		map[string]string{"uid": "uid", "chain": "chain", "status": "status"}))
	a("GET /admin/v1/biz/paper/accounts", permBizView, s.bizList(`user_id AS uid, balance::text AS balance, frozen::text AS frozen,
		position_mode AS "positionMode", generation, updated_at AS "updatedAt",
		(SELECT count(*) FROM paper_fut_positions p WHERE p.user_id = paper_fut_accounts.user_id) AS positions`,
		"paper_fut_accounts", "updated_at DESC", map[string]string{"uid": "user_id"}))
	a("GET /admin/v1/biz/dca/plans", permBizView, s.bizList(`id, user_id AS uid, symbol, side, amount_per_run::text AS "amountPerRun",
		interval_seconds AS "intervalSeconds", trigger_type AS "triggerType", status, stop_reason AS "stopReason", total_runs AS "totalRuns",
		total_base::text AS "totalBase", total_quote::text AS "totalQuote", next_run_at AS "nextRunAt", last_run_at AS "lastRunAt", created_at AS "createdAt"`,
		"dca_plans", "id DESC", map[string]string{"uid": "user_id", "status": "status", "symbol": "symbol"}))
	a("GET /admin/v1/biz/dca/executions", permBizView, s.bizList(`id, plan_id AS "planId", user_id AS uid, symbol, side, order_id AS "orderId", status,
		requested_quote::text AS "requestedQuote", filled_base::text AS "filledBase", filled_quote::text AS "filledQuote", avg_price::text AS "avgPrice",
		error, created_at AS "createdAt"`, "dca_executions", "id DESC", map[string]string{"plan": "plan_id", "uid": "user_id", "status": "status"}))
	a("GET /admin/v1/biz/im/groups", permBizView, s.bizList(`id, name, owner_uid AS owner, member_count AS members, max_members AS "maxMembers",
		mute_all AS "muteAll", announcement, created_at AS "createdAt", updated_at AS "updatedAt"`, "im_groups", "id DESC",
		map[string]string{"owner": "owner_uid"}))
	a("GET /admin/v1/biz/im/redpackets", permBizView, s.bizList(`id, group_id AS "groupId", sender_uid AS sender, asset, total_amount::text AS total,
		shares, remaining_shares AS "remainingShares", remaining_amount::text AS remaining, mode, status, expires_at AS "expiresAt",
		created_at AS "createdAt", closed_at AS "closedAt", refunded_at AS "refundedAt",
		(SELECT count(*) FROM im_redpacket_claims c WHERE c.redpacket_id = im_redpackets.id) AS claims`, "im_redpackets", "id DESC",
		map[string]string{"sender": "sender_uid", "status": "status", "group": "group_id"}))
	a("POST /admin/v1/biz/im/groups/{id}/mute", permContentEdit, s.handleIMGroupMute)
	// App 版本 / 文案 / 推送
	a("GET /admin/v1/app/versions", permContentEdit, s.handleGetAppVersions)
	a("PUT /admin/v1/app/versions/{platform}", permContentEdit, s.handlePutAppVersion)
	a("GET /admin/v1/content/docs", permContentEdit, s.handleListDocs)
	a("PUT /admin/v1/content/docs/{key}", permContentEdit, s.handlePutDoc)
	a("GET /admin/v1/push/devices", permContentEdit, s.handlePushDevices)
	a("GET /admin/v1/push/jobs", permContentEdit, s.handleListPushJobs)
	a("POST /admin/v1/push/jobs", permContentEdit, s.handleCreatePushJob)
}

// registerBatch6Public: compat 读取（127.0.0.1:8088，不经 ops 域名）。
func (s *Server) registerBatch6Public(mux *http.ServeMux) {
	mux.HandleFunc("GET /public/app/versions", s.handlePublicAppVersions)
	mux.HandleFunc("GET /public/content/docs/{key}", s.handlePublicDoc)
}

// ---------------- generic list ----------------

func (s *Server) bizList(cols, table, order string, params map[string]string) http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		f := &filter{}
		keys := make([]string, 0, len(params))
		for k := range params {
			keys = append(keys, k)
		}
		sort.Strings(keys)
		for _, k := range keys {
			f.eq(r, k, params[k])
		}
		s.pagedList(w, r, table, cols, table, order, f, nil)
	}
}

// ---------------- grid force stop ----------------

func (s *Server) handleGridForceStop(w http.ResponseWriter, r *http.Request) {
	a := actorOf(r)
	id, ok := pathID(r)
	if !ok {
		common.WriteError(w, http.StatusBadRequest, "VALIDATION_ERROR", "策略 ID 不正确")
		return
	}
	var req struct {
		reasonReq
		SellBase *bool `json:"sellBase"`
	}
	if err := common.DecodeJSON(r, &req); err != nil {
		common.WriteError(w, http.StatusBadRequest, "VALIDATION_ERROR", "请求格式错误")
		return
	}
	if strings.TrimSpace(req.Reason) == "" {
		common.WriteError(w, http.StatusBadRequest, "VALIDATION_ERROR", "请填写强停原因")
		return
	}
	if !s.verify2FA(w, a, req.TOTPCode) {
		return
	}
	var uid, status string
	if err := s.db.QueryRowContext(r.Context(), `SELECT user_id, status FROM grid_bots WHERE id=$1`, id).Scan(&uid, &status); err != nil {
		common.WriteError(w, http.StatusNotFound, "NOT_FOUND", "策略不存在")
		return
	}
	before := map[string]any{"uid": uid, "status": status}
	if status != "running" {
		common.WriteError(w, http.StatusConflict, "NOT_RUNNING", "策略不在运行中")
		return
	}
	body := map[string]any{"by": a.Username, "reason": req.Reason, "sellBase": req.SellBase}
	u, err := s.call(r.Context(), http.MethodPost, s.svcURL("strategy"), "/internal/strategy/grid/"+strconv.FormatInt(id, 10)+"/force-stop", body)
	target := "grid:" + strconv.FormatInt(id, 10)
	if err != nil {
		s.audit(r, a, "strategy", "grid_force_stop", target, levelHigh, "failed:UPSTREAM_UNAVAILABLE", before, nil, req.Reason)
		upstreamErr(w, "strategy force-stop", err)
		return
	}
	if u.Status == http.StatusNotFound || u.Status == http.StatusMethodNotAllowed {
		s.audit(r, a, "strategy", "grid_force_stop", target, levelHigh, "failed:NOT_DEPLOYED", before, nil, req.Reason)
		common.WriteError(w, http.StatusNotImplemented, "STRATEGY_NOT_DEPLOYED", "strategy 服务尚未上线强停接口（新版本已备好，待批准重启 strategy）")
		return
	}
	s.audit(r, a, "strategy", "grid_force_stop", target, levelHigh, resultOf(u), before, body, req.Reason)
	relay(w, u)
}

// ---------------- fund-config two-person approval ----------------

type fundAction struct {
	Target string          `json:"target"`
	Key    string          `json:"key"`
	Svc    string          `json:"svc"`
	Method string          `json:"method"`
	Path   string          `json:"path"`
	Body   json.RawMessage `json:"body,omitempty"`
}

var fundKeyRe = regexp.MustCompile(`^[A-Za-z0-9_.:-]{1,64}$`)

// fundTargets: 可提交的资金类配置及提交所需权限。
var fundTargets = map[string]string{
	"staking_product":        permEarnEdit,
	"staking_product_delete": permEarnEdit,
	"launchpad_project":      permEarnEdit,
	"launchpad_cancel":       permEarnEdit,
	"launchpad_settle":       permEarnEdit,
	"launchpad_distribute":   permEarnEdit,
	"mm_pair":                permMMEdit,
	"mm_baseline":            permMMEdit,
	"convert_config":         permSymbolEdit,
}

var fundTargetCN = map[string]string{
	"staking_product": "质押产品上架/修改", "staking_product_delete": "质押产品删除", "launchpad_project": "Launchpad 项目创建/修改",
	"launchpad_cancel": "Launchpad 项目取消（退款）", "launchpad_settle": "Launchpad 强制结算", "launchpad_distribute": "Launchpad 强制派发",
	"mm_pair": "做市交易对参数", "mm_baseline": "做市库存基线", "convert_config": "闪兑配置",
}

func buildFundAction(target, key string, body json.RawMessage) (*fundAction, error) {
	if !fundKeyRe.MatchString(key) && target != "convert_config" {
		return nil, errors.New("key 不合法")
	}
	esc := url.PathEscape
	fa := &fundAction{Target: target, Key: key, Body: body}
	switch target {
	case "staking_product":
		fa.Svc, fa.Method, fa.Path = "staking", http.MethodPost, "/internal/staking/products"
	case "staking_product_delete":
		fa.Svc, fa.Method, fa.Path, fa.Body = "staking", http.MethodDelete, "/internal/staking/products/"+esc(key), nil
	case "launchpad_project":
		fa.Svc, fa.Method, fa.Path = "launchpad", http.MethodPost, "/internal/launchpad/projects"
	case "launchpad_cancel", "launchpad_settle", "launchpad_distribute":
		fa.Svc, fa.Method, fa.Path, fa.Body = "launchpad", http.MethodPost, "/internal/launchpad/projects/"+esc(key)+"/"+strings.TrimPrefix(target, "launchpad_"), json.RawMessage(`{}`)
	case "mm_pair":
		parts := strings.SplitN(key, ":", 2)
		if len(parts) != 2 || (parts[0] != "spot" && parts[0] != "futures") {
			return nil, errors.New("mm_pair key 应为 market:SYMBOL")
		}
		fa.Svc, fa.Method, fa.Path = "mm", http.MethodPut, "/internal/mm/pairs/"+esc(parts[0])+"/"+esc(strings.ToUpper(parts[1]))
	case "mm_baseline":
		fa.Svc, fa.Method, fa.Path = "mm", http.MethodPut, "/internal/mm/baselines/"+esc(strings.ToUpper(key))
	case "convert_config":
		fa.Key = "config"
		fa.Svc, fa.Method, fa.Path = "asset", http.MethodPut, "/internal/convert/config"
	default:
		return nil, errors.New("不支持的配置类型")
	}
	if fa.Method != http.MethodDelete && len(fa.Body) == 0 {
		return nil, errors.New("缺少变更内容 body")
	}
	if len(fa.Body) > 0 {
		var m map[string]json.RawMessage
		if json.Unmarshal(fa.Body, &m) != nil {
			return nil, errors.New("body 必须是 JSON 对象")
		}
		delete(m, "totpCode")
		delete(m, "reason")
		delete(m, "updatedBy")
		fa.Body, _ = json.Marshal(m)
	}
	return fa, nil
}

// launchpadEditable: 已开始认购或已有认购的项目不允许再改价格/额度。
func (s *Server) launchpadEditable(ctx context.Context, id string) error {
	var status string
	var start time.Time
	var subs int
	err := s.db.QueryRowContext(ctx, `SELECT status, start_at, (SELECT count(*) FROM launchpad_subscriptions WHERE project_id=$1) FROM launchpad_projects WHERE id=$1`, id).Scan(&status, &start, &subs)
	if errors.Is(err, sql.ErrNoRows) {
		return nil // 新项目
	}
	if err != nil {
		return err
	}
	if status != "upcoming" || subs > 0 || !time.Now().Before(start) {
		return errors.New("项目已开始或已有认购，不能再修改参数")
	}
	return nil
}

// submitFundChange 由各写接口与 POST /fund-changes 共用。
func (s *Server) submitFundChange(ctx context.Context, a *actor, fa *fundAction, reason string) (int64, error) {
	if fa.Target == "launchpad_project" {
		var b struct {
			ID string `json:"id"`
		}
		_ = json.Unmarshal(fa.Body, &b)
		if b.ID == "" || b.ID != fa.Key {
			return 0, errors.New("body.id 必须与 key 一致")
		}
		if err := s.launchpadEditable(ctx, fa.Key); err != nil {
			return 0, err
		}
	}
	before := s.fundBefore(ctx, fa)
	pj, _ := json.Marshal(fa)
	var id int64
	err := s.db.QueryRowContext(ctx, `INSERT INTO change_requests (kind, payload, before, reason, submitted_by) VALUES ($1,$2,$3,$4,$5) RETURNING id`,
		kindFundConfig, string(pj), nullJSON(before), reason, a.ID).Scan(&id)
	return id, err
}

func nullJSON(b []byte) interface{} {
	if len(b) == 0 || !json.Valid(b) {
		return nil
	}
	return string(b)
}

// fundBefore 记录提交时的当前值，便于审批人对比。
func (s *Server) fundBefore(ctx context.Context, fa *fundAction) []byte {
	var q string
	var args []interface{}
	switch fa.Target {
	case "staking_product", "staking_product_delete":
		var b struct {
			ID string `json:"id"`
		}
		_ = json.Unmarshal(fa.Body, &b)
		k := fa.Key
		if b.ID != "" {
			k = b.ID
		}
		q, args = `SELECT row_to_json(p)::text FROM s_products p WHERE id=$1`, []interface{}{k}
	case "launchpad_project", "launchpad_cancel", "launchpad_settle", "launchpad_distribute":
		q, args = `SELECT row_to_json(p)::text FROM (SELECT id, symbol, price, total_supply, min_sub, max_sub, start_at, end_at, status, raised, allocated FROM launchpad_projects WHERE id=$1) p`, []interface{}{fa.Key}
	case "mm_pair":
		parts := strings.SplitN(fa.Key, ":", 2)
		q, args = `SELECT row_to_json(p)::text FROM mm_pairs p WHERE market=$1 AND symbol=$2`, []interface{}{parts[0], strings.ToUpper(parts[1])}
	case "convert_config":
		if u, err := s.call(ctx, http.MethodGet, s.cfg.AssetURL, "/internal/convert/config", nil); err == nil && u.ok() {
			return u.Body
		}
		return nil
	default:
		return nil
	}
	var out string
	if err := s.db.QueryRowContext(ctx, q, args...).Scan(&out); err != nil {
		return nil
	}
	return []byte(out)
}

func writePending(w http.ResponseWriter, id int64, target string) {
	common.WriteJSON(w, http.StatusAccepted, map[string]any{
		"pendingApproval": true, "changeId": id, "target": target,
		"message": errFundPendingTitle + " #" + strconv.FormatInt(id, 10) + "：需另一名有 config:approve 权限的管理员审批后生效",
	})
}

func (s *Server) handleSubmitFundChange(w http.ResponseWriter, r *http.Request) {
	a := actorOf(r)
	var req struct {
		reasonReq
		Target string          `json:"target"`
		Key    string          `json:"key"`
		Body   json.RawMessage `json:"body"`
	}
	if err := common.DecodeJSON(r, &req); err != nil {
		common.WriteError(w, http.StatusBadRequest, "VALIDATION_ERROR", "请求格式错误")
		return
	}
	need, ok := fundTargets[req.Target]
	if !ok {
		common.WriteError(w, http.StatusBadRequest, "VALIDATION_ERROR", "不支持的配置类型")
		return
	}
	if !can(a.Roles, need) {
		common.WriteError(w, http.StatusForbidden, "FORBIDDEN", "无权提交该类变更（需要 "+need+"）")
		return
	}
	if strings.TrimSpace(req.Reason) == "" {
		common.WriteError(w, http.StatusBadRequest, "VALIDATION_ERROR", "请填写变更原因")
		return
	}
	if !s.verify2FA(w, a, req.TOTPCode) {
		return
	}
	fa, err := buildFundAction(req.Target, req.Key, req.Body)
	if err != nil {
		common.WriteError(w, http.StatusBadRequest, "VALIDATION_ERROR", err.Error())
		return
	}
	id, err := s.submitFundChange(r.Context(), a, fa, req.Reason)
	if err != nil {
		common.WriteError(w, http.StatusConflict, "CHANGE_REFUSED", err.Error())
		return
	}
	s.audit(r, a, fundChangeModule, "submit", req.Target+":"+fa.Key, levelHigh, "ok", nil, fa, req.Reason)
	writePending(w, id, req.Target)
}

func (s *Server) handleListFundChanges(w http.ResponseWriter, r *http.Request) {
	st := strings.TrimSpace(r.URL.Query().Get("status"))
	q := `SELECT c.id, c.payload::text, COALESCE(c.before::text,''), c.status, c.reason, u.username, c.created_at, c.applied_at, COALESCE(c.error,''),
		COALESCE((SELECT string_agg(au.username||':'||x.decision||COALESCE(':'||x.comment,''), '; ') FROM change_approvals x JOIN admin_users au ON au.id=x.admin_id WHERE x.change_id=c.id),'')
		FROM change_requests c JOIN admin_users u ON u.id=c.submitted_by WHERE c.kind=$1`
	args := []interface{}{kindFundConfig}
	if st != "" {
		q += ` AND c.status=$2`
		args = append(args, st)
	}
	q += ` ORDER BY c.id DESC LIMIT 200`
	rows, err := s.db.QueryContext(r.Context(), q, args...)
	if err != nil {
		internalErr(w, "fund changes", err)
		return
	}
	defer rows.Close()
	out := []map[string]any{}
	for rows.Next() {
		var id int64
		var payload, before, status, reason, by, errS, decisions string
		var created time.Time
		var applied sql.NullTime
		if err := rows.Scan(&id, &payload, &before, &status, &reason, &by, &created, &applied, &errS, &decisions); err != nil {
			internalErr(w, "fund changes", err)
			return
		}
		var fa fundAction
		_ = json.Unmarshal([]byte(payload), &fa)
		m := map[string]any{"id": id, "target": fa.Target, "targetName": fundTargetCN[fa.Target], "key": fa.Key, "after": json.RawMessage(nz(fa.Body)),
			"before": json.RawMessage(nz([]byte(before))), "status": status, "reason": reason, "submittedBy": by, "createdAt": fmtTime(created),
			"error": errS, "decisions": decisions}
		if applied.Valid {
			m["appliedAt"] = fmtTime(applied.Time)
		}
		out = append(out, m)
	}
	common.WriteJSON(w, http.StatusOK, map[string]any{"items": out})
}

func nz(b []byte) []byte {
	if len(b) == 0 || !json.Valid(b) {
		return []byte("null")
	}
	return b
}

func (s *Server) handleFundDecision(decision string) http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		a := actorOf(r)
		id, ok := pathID(r)
		if !ok {
			common.WriteError(w, http.StatusBadRequest, "VALIDATION_ERROR", "id 不正确")
			return
		}
		var req reasonReq
		_ = common.DecodeJSON(r, &req)
		if decision == "reject" && strings.TrimSpace(req.Comment+req.Reason) == "" {
			common.WriteError(w, http.StatusBadRequest, "VALIDATION_ERROR", "驳回需填写原因")
			return
		}
		if !s.verify2FA(w, a, req.TOTPCode) {
			return
		}
		ctx := r.Context()
		tx, err := s.db.BeginTx(ctx, nil)
		if err != nil {
			internalErr(w, "fund decision", err)
			return
		}
		defer tx.Rollback()
		var payload, status string
		var submitter int64
		err = tx.QueryRowContext(ctx, `SELECT payload::text, status, submitted_by FROM change_requests WHERE id=$1 AND kind=$2 FOR UPDATE`, id, kindFundConfig).Scan(&payload, &status, &submitter)
		if err != nil {
			common.WriteError(w, http.StatusNotFound, "NOT_FOUND", "变更单不存在")
			return
		}
		if status != "pending" {
			common.WriteError(w, http.StatusConflict, "NOT_PENDING", "变更单已处理")
			return
		}
		if submitter == a.ID {
			s.audit(r, a, fundChangeModule, decision, "fund_change:"+strconv.FormatInt(id, 10), levelHigh, "refused:SELF_REVIEW", nil, nil, req.Comment)
			common.WriteError(w, http.StatusForbidden, "SELF_REVIEW", "提交人不能审批自己的变更（双人审批）")
			return
		}
		comment := strings.TrimSpace(req.Comment + " " + req.Reason)
		role := "approver"
		if len(a.Roles) > 0 {
			role = a.Roles[0]
		}
		if _, err := tx.ExecContext(ctx, `INSERT INTO change_approvals (change_id, admin_id, as_role, decision, comment) VALUES ($1,$2,$3,$4,$5)`,
			id, a.ID, role, decision, nullStr(comment)); err != nil {
			internalErr(w, "fund decision", err)
			return
		}
		var fa fundAction
		_ = json.Unmarshal([]byte(payload), &fa)
		target := "fund_change:" + strconv.FormatInt(id, 10)
		if decision == "reject" {
			if _, err := tx.ExecContext(ctx, `UPDATE change_requests SET status='rejected', updated_at=now() WHERE id=$1`, id); err != nil {
				internalErr(w, "fund decision", err)
				return
			}
			if err := tx.Commit(); err != nil {
				internalErr(w, "fund decision", err)
				return
			}
			s.audit(r, a, fundChangeModule, "reject", target, levelHigh, "ok", nil, fa, comment)
			common.WriteJSON(w, http.StatusOK, map[string]any{"id": id, "status": "rejected"})
			return
		}
		// 审批通过：先锁定为 applying，提交事务后再调用下游，避免重复执行。
		if _, err := tx.ExecContext(ctx, `UPDATE change_requests SET status='applying', updated_at=now() WHERE id=$1`, id); err != nil {
			internalErr(w, "fund decision", err)
			return
		}
		if err := tx.Commit(); err != nil {
			internalErr(w, "fund decision", err)
			return
		}
		if fa.Target == "launchpad_project" {
			if err := s.launchpadEditable(ctx, fa.Key); err != nil {
				s.finishFund(ctx, id, "failed", err.Error())
				s.audit(r, a, fundChangeModule, "apply", target, levelHigh, "failed:NOT_EDITABLE", nil, fa, comment)
				common.WriteError(w, http.StatusConflict, "CHANGE_REFUSED", err.Error())
				return
			}
		}
		var body interface{}
		if len(fa.Body) > 0 {
			var m map[string]json.RawMessage
			_ = json.Unmarshal(fa.Body, &m)
			if fa.Svc == "mm" || fa.Svc == "asset" {
				m["updatedBy"], _ = json.Marshal(a.Username)
			}
			body = m
		}
		base := s.svcURL(fa.Svc)
		u, err := s.call(ctx, fa.Method, base, fa.Path, body)
		if err != nil {
			s.finishFund(ctx, id, "failed", "上游不可用: "+err.Error())
			s.audit(r, a, fundChangeModule, "apply", target, levelHigh, "failed:UPSTREAM_UNAVAILABLE", nil, fa, comment)
			upstreamErr(w, "fund apply", err)
			return
		}
		if !u.ok() {
			s.finishFund(ctx, id, "failed", strings.TrimSpace(string(u.Body)))
		} else {
			s.finishFund(ctx, id, "applied", "")
		}
		s.audit(r, a, fundChangeModule, "apply", target, levelHigh, resultOf(u), nil, fa, comment)
		relay(w, u)
	}
}

func (s *Server) finishFund(ctx context.Context, id int64, status, errS string) {
	_, _ = s.db.ExecContext(context.WithoutCancel(ctx), `UPDATE change_requests SET status=$2, error=NULLIF($3,''), applied_at=CASE WHEN $2='applied' THEN now() END, updated_at=now() WHERE id=$1`, id, status, errS)
}

// ---------------- NFT collection status ----------------

func (s *Server) handleNFTCollectionStatus(w http.ResponseWriter, r *http.Request) {
	a := actorOf(r)
	slug := r.PathValue("slug")
	var req struct {
		reasonReq
		Status string `json:"status"`
	}
	if err := common.DecodeJSON(r, &req); err != nil || (req.Status != "active" && req.Status != "hidden") {
		common.WriteError(w, http.StatusBadRequest, "VALIDATION_ERROR", "status 只能是 active / hidden")
		return
	}
	if strings.TrimSpace(req.Reason) == "" {
		common.WriteError(w, http.StatusBadRequest, "VALIDATION_ERROR", "请填写原因")
		return
	}
	if !s.verify2FA(w, a, req.TOTPCode) {
		return
	}
	u, err := s.call(r.Context(), http.MethodPatch, s.svcURL("nft"), "/internal/nft/collections/"+url.PathEscape(slug), map[string]any{"status": req.Status})
	if err != nil {
		upstreamErr(w, "nft collection", err)
		return
	}
	s.audit(r, a, "nft", "collection_status", "nft_collection:"+slug, levelSensitive, resultOf(u), nil, map[string]any{"status": req.Status}, req.Reason)
	relay(w, u)
}

// ---------------- IM group mute ----------------

func (s *Server) handleIMGroupMute(w http.ResponseWriter, r *http.Request) {
	a := actorOf(r)
	id, ok := pathID(r)
	if !ok {
		common.WriteError(w, http.StatusBadRequest, "VALIDATION_ERROR", "群 ID 不正确")
		return
	}
	var req struct {
		reasonReq
		MuteAll bool `json:"muteAll"`
	}
	if err := common.DecodeJSON(r, &req); err != nil || strings.TrimSpace(req.Reason) == "" {
		common.WriteError(w, http.StatusBadRequest, "VALIDATION_ERROR", "请填写原因")
		return
	}
	if !s.verify2FA(w, a, req.TOTPCode) {
		return
	}
	var before bool
	if err := s.db.QueryRowContext(r.Context(), `SELECT mute_all FROM im_groups WHERE id=$1`, id).Scan(&before); err != nil {
		common.WriteError(w, http.StatusNotFound, "NOT_FOUND", "群不存在")
		return
	}
	if _, err := s.db.ExecContext(r.Context(), `UPDATE im_groups SET mute_all=$2, updated_at=now() WHERE id=$1`, id, req.MuteAll); err != nil {
		internalErr(w, "im group mute", err)
		return
	}
	s.audit(r, a, "im", "group_mute_all", "im_group:"+strconv.FormatInt(id, 10), levelSensitive, "ok", map[string]any{"muteAll": before}, map[string]any{"muteAll": req.MuteAll}, req.Reason)
	common.WriteJSON(w, http.StatusOK, map[string]any{"id": id, "muteAll": req.MuteAll})
}

// ---------------- App versions ----------------

var verRe = regexp.MustCompile(`^[0-9]{1,4}(\.[0-9]{1,4}){0,3}$`)

type appVersion struct {
	Platform      string `json:"platform"`
	LatestVersion string `json:"latestVersion"`
	LatestBuild   int    `json:"latestBuild"`
	MinVersion    string `json:"minVersion"`
	DownloadURL   string `json:"downloadUrl"`
	ReleaseNotes  string `json:"releaseNotes"`
	Enabled       bool   `json:"enabled"`
	UpdatedBy     string `json:"updatedBy,omitempty"`
	UpdatedAt     string `json:"updatedAt,omitempty"`
}

func (s *Server) loadAppVersions(ctx context.Context, onlyEnabled bool) ([]appVersion, error) {
	q := `SELECT platform, latest_version, latest_build, min_version, download_url, release_notes, enabled, updated_by, updated_at FROM admin_app_versions`
	if onlyEnabled {
		q += ` WHERE enabled`
	}
	rows, err := s.db.QueryContext(ctx, q+` ORDER BY platform`)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	out := []appVersion{}
	for rows.Next() {
		var v appVersion
		var t time.Time
		if err := rows.Scan(&v.Platform, &v.LatestVersion, &v.LatestBuild, &v.MinVersion, &v.DownloadURL, &v.ReleaseNotes, &v.Enabled, &v.UpdatedBy, &t); err != nil {
			return nil, err
		}
		v.UpdatedAt = fmtTime(t)
		out = append(out, v)
	}
	return out, rows.Err()
}

func (s *Server) handleGetAppVersions(w http.ResponseWriter, r *http.Request) {
	vs, err := s.loadAppVersions(r.Context(), false)
	if err != nil {
		internalErr(w, "app versions", err)
		return
	}
	common.WriteJSON(w, http.StatusOK, map[string]any{"items": vs, "note": "compat 下一次构建接入 /public/app/versions 后客户端生效"})
}

func (s *Server) handlePublicAppVersions(w http.ResponseWriter, r *http.Request) {
	vs, err := s.loadAppVersions(r.Context(), true)
	if err != nil {
		internalErr(w, "app versions", err)
		return
	}
	for i := range vs {
		vs[i].UpdatedBy = ""
	}
	common.WriteJSON(w, http.StatusOK, map[string]any{"items": vs})
}

func (s *Server) handlePutAppVersion(w http.ResponseWriter, r *http.Request) {
	a := actorOf(r)
	p := r.PathValue("platform")
	if p != "ios" && p != "android" {
		common.WriteError(w, http.StatusBadRequest, "VALIDATION_ERROR", "platform 只能是 ios / android")
		return
	}
	var req struct {
		reasonReq
		appVersion
	}
	if err := common.DecodeJSON(r, &req); err != nil {
		common.WriteError(w, http.StatusBadRequest, "VALIDATION_ERROR", "请求格式错误")
		return
	}
	v := req.appVersion
	if !verRe.MatchString(v.LatestVersion) || (v.MinVersion != "" && !verRe.MatchString(v.MinVersion)) || v.LatestBuild < 0 {
		common.WriteError(w, http.StatusBadRequest, "VALIDATION_ERROR", "版本号格式应为 1.2.3")
		return
	}
	if v.MinVersion != "" && cmpVer(v.MinVersion, v.LatestVersion) > 0 {
		common.WriteError(w, http.StatusBadRequest, "VALIDATION_ERROR", "强制更新版本不能高于最新版本")
		return
	}
	if v.DownloadURL != "" {
		u, err := url.Parse(v.DownloadURL)
		if err != nil || u.Scheme != "https" || u.Host == "" {
			common.WriteError(w, http.StatusBadRequest, "VALIDATION_ERROR", "下载地址必须是 https 链接")
			return
		}
	}
	if len(v.ReleaseNotes) > 4000 {
		common.WriteError(w, http.StatusBadRequest, "VALIDATION_ERROR", "更新说明过长")
		return
	}
	if strings.TrimSpace(req.Reason) == "" {
		common.WriteError(w, http.StatusBadRequest, "VALIDATION_ERROR", "请填写原因")
		return
	}
	if !s.verify2FA(w, a, req.TOTPCode) {
		return
	}
	var before []byte
	_ = s.db.QueryRowContext(r.Context(), `SELECT row_to_json(v)::text FROM admin_app_versions v WHERE platform=$1`, p).Scan(&before)
	_, err := s.db.ExecContext(r.Context(), `INSERT INTO admin_app_versions (platform, latest_version, latest_build, min_version, download_url, release_notes, enabled, updated_by, updated_at)
		VALUES ($1,$2,$3,$4,$5,$6,$7,$8,now()) ON CONFLICT (platform) DO UPDATE SET latest_version=$2, latest_build=$3, min_version=$4,
		download_url=$5, release_notes=$6, enabled=$7, updated_by=$8, updated_at=now()`,
		p, v.LatestVersion, v.LatestBuild, v.MinVersion, v.DownloadURL, v.ReleaseNotes, v.Enabled, a.Username)
	if err != nil {
		internalErr(w, "app version", err)
		return
	}
	v.Platform = p
	s.audit(r, a, "app", "set_version", "app_version:"+p, levelSensitive, "ok", json.RawMessage(nz(before)), v, req.Reason)
	common.WriteJSON(w, http.StatusOK, v)
}

func cmpVer(a, b string) int {
	pa, pb := strings.Split(a, "."), strings.Split(b, ".")
	for i := 0; i < 4; i++ {
		var x, y int
		if i < len(pa) {
			x, _ = strconv.Atoi(pa[i])
		}
		if i < len(pb) {
			y, _ = strconv.Atoi(pb[i])
		}
		if x != y {
			if x < y {
				return -1
			}
			return 1
		}
	}
	return 0
}

// ---------------- content docs (help center / activities) ----------------

var docKeyRe = regexp.MustCompile(`^(help_center|activity:[a-z0-9-]{1,48}|page:[a-z0-9-]{1,48})$`)

func (s *Server) handleListDocs(w http.ResponseWriter, r *http.Request) {
	items, err := s.queryMaps(r.Context(), `SELECT key, title, format, body, published, updated_by AS "updatedBy", updated_at AS "updatedAt" FROM admin_content_docs ORDER BY key`)
	if err != nil {
		internalErr(w, "content docs", err)
		return
	}
	common.WriteJSON(w, http.StatusOK, map[string]any{"items": items, "note": "未发布或不存在的文档，客户端继续使用 compat 内置版本"})
}

func (s *Server) handlePutDoc(w http.ResponseWriter, r *http.Request) {
	a := actorOf(r)
	key := r.PathValue("key")
	if !docKeyRe.MatchString(key) {
		common.WriteError(w, http.StatusBadRequest, "VALIDATION_ERROR", "key 只能是 help_center / activity:<slug> / page:<slug>")
		return
	}
	var req struct {
		reasonReq
		Title     string `json:"title"`
		Format    string `json:"format"`
		Body      string `json:"body"`
		Published bool   `json:"published"`
	}
	if err := common.DecodeJSON(r, &req); err != nil {
		common.WriteError(w, http.StatusBadRequest, "VALIDATION_ERROR", "请求格式错误")
		return
	}
	if req.Format == "" {
		req.Format = "markdown"
		if key == "help_center" {
			req.Format = "json"
		}
	}
	if req.Format != "markdown" && req.Format != "json" {
		common.WriteError(w, http.StatusBadRequest, "VALIDATION_ERROR", "format 只能是 markdown / json")
		return
	}
	if strings.TrimSpace(req.Body) == "" || len(req.Body) > 512*1024 {
		common.WriteError(w, http.StatusBadRequest, "VALIDATION_ERROR", "正文为空或超过 512KB")
		return
	}
	if req.Format == "json" && !json.Valid([]byte(req.Body)) {
		common.WriteError(w, http.StatusBadRequest, "VALIDATION_ERROR", "JSON 格式不正确")
		return
	}
	if !s.verify2FA(w, a, req.TOTPCode) {
		return
	}
	_, err := s.db.ExecContext(r.Context(), `INSERT INTO admin_content_docs (key, title, format, body, published, updated_by, updated_at) VALUES ($1,$2,$3,$4,$5,$6,now())
		ON CONFLICT (key) DO UPDATE SET title=$2, format=$3, body=$4, published=$5, updated_by=$6, updated_at=now()`,
		key, req.Title, req.Format, req.Body, req.Published, a.Username)
	if err != nil {
		internalErr(w, "content doc", err)
		return
	}
	s.audit(r, a, "content", "edit_doc", "doc:"+key, levelNormal, "ok", nil, map[string]any{"title": req.Title, "published": req.Published, "bytes": len(req.Body)}, req.Reason)
	common.WriteJSON(w, http.StatusOK, map[string]any{"key": key, "published": req.Published})
}

func (s *Server) handlePublicDoc(w http.ResponseWriter, r *http.Request) {
	key := r.PathValue("key")
	if !docKeyRe.MatchString(key) {
		common.WriteError(w, http.StatusNotFound, "NOT_FOUND", "not found")
		return
	}
	var title, format, body string
	var t time.Time
	err := s.db.QueryRowContext(r.Context(), `SELECT title, format, body, updated_at FROM admin_content_docs WHERE key=$1 AND published`, key).Scan(&title, &format, &body, &t)
	if err != nil {
		common.WriteError(w, http.StatusNotFound, "NOT_FOUND", "not found")
		return
	}
	common.WriteJSON(w, http.StatusOK, map[string]any{"key": key, "title": title, "format": format, "body": body, "updatedAt": fmtTime(t)})
}

// ---------------- push ----------------

type pushDevice struct {
	DeviceID   string `json:"deviceId"`
	UID        string `json:"uid,omitempty"`
	Token      string `json:"token,omitempty"`
	Provider   string `json:"provider,omitempty"`
	Platform   string `json:"platform,omitempty"`
	Model      string `json:"model,omitempty"`
	AppVersion string `json:"appVersion,omitempty"`
	UpdatedAt  string `json:"updatedAt"`
}

func pushDeviceDir() string { return common.Env("ADMIN_PUSH_DEVICE_DIR", "/data/pushdev") }

func loadPushDevices() ([]pushDevice, error) {
	ents, err := os.ReadDir(pushDeviceDir())
	if err != nil {
		if os.IsNotExist(err) {
			return []pushDevice{}, nil
		}
		return nil, err
	}
	out := []pushDevice{}
	for _, e := range ents {
		if !strings.HasSuffix(e.Name(), ".json") {
			continue
		}
		b, err := os.ReadFile(filepath.Join(pushDeviceDir(), e.Name()))
		if err != nil || len(b) > 64*1024 {
			continue
		}
		var d pushDevice
		if json.Unmarshal(b, &d) == nil && d.DeviceID != "" {
			out = append(out, d)
		}
	}
	sort.Slice(out, func(i, j int) bool { return out[i].UpdatedAt > out[j].UpdatedAt })
	return out, nil
}

func pushCreds() map[string]bool {
	return map[string]bool{
		"apns": os.Getenv("APNS_KEY_ID") != "" && os.Getenv("APNS_TEAM_ID") != "" && os.Getenv("APNS_KEY_FILE") != "",
		"fcm":  os.Getenv("FCM_CREDENTIALS_FILE") != "",
	}
}

func (s *Server) handlePushDevices(w http.ResponseWriter, r *http.Request) {
	ds, err := loadPushDevices()
	if err != nil {
		internalErr(w, "push devices", err)
		return
	}
	uid := strings.TrimSpace(r.URL.Query().Get("uid"))
	summary := map[string]int{}
	items := []map[string]any{}
	for _, d := range ds {
		k := d.Platform
		if k == "" {
			k = "unknown"
		}
		if d.Token != "" {
			summary[k+"_with_token"]++
		}
		summary[k]++
		if uid != "" && d.UID != uid {
			continue
		}
		if len(items) < 200 {
			tok := ""
			if d.Token != "" {
				tok = d.Token[:min(6, len(d.Token))] + "…(" + strconv.Itoa(len(d.Token)) + ")"
			}
			items = append(items, map[string]any{"deviceId": d.DeviceID, "uid": d.UID, "platform": d.Platform, "provider": d.Provider,
				"model": d.Model, "appVersion": d.AppVersion, "token": tok, "updatedAt": d.UpdatedAt})
		}
	}
	common.WriteJSON(w, http.StatusOK, map[string]any{"total": len(ds), "summary": summary, "items": items, "credentials": pushCreds(), "source": pushDeviceDir()})
}

func (s *Server) handleListPushJobs(w http.ResponseWriter, r *http.Request) {
	items, err := s.queryMaps(r.Context(), `SELECT id, title, body, target, platform, device_count AS "deviceCount", status, note, created_by AS "createdBy", created_at AS "createdAt"
		FROM admin_push_jobs ORDER BY id DESC LIMIT 200`)
	if err != nil {
		internalErr(w, "push jobs", err)
		return
	}
	common.WriteJSON(w, http.StatusOK, map[string]any{"items": items, "credentials": pushCreds()})
}

func (s *Server) handleCreatePushJob(w http.ResponseWriter, r *http.Request) {
	a := actorOf(r)
	var req struct {
		reasonReq
		Title    string `json:"title"`
		Body     string `json:"body"`
		UID      string `json:"uid"`
		Platform string `json:"platform"`
	}
	if err := common.DecodeJSON(r, &req); err != nil {
		common.WriteError(w, http.StatusBadRequest, "VALIDATION_ERROR", "请求格式错误")
		return
	}
	req.Title, req.Body, req.UID = strings.TrimSpace(req.Title), strings.TrimSpace(req.Body), strings.TrimSpace(req.UID)
	if req.Title == "" || req.Body == "" || len(req.Title) > 120 || len(req.Body) > 1000 {
		common.WriteError(w, http.StatusBadRequest, "VALIDATION_ERROR", "标题/内容为空或过长")
		return
	}
	if req.Platform == "" {
		req.Platform = "all"
	}
	if req.Platform != "all" && req.Platform != "ios" && req.Platform != "android" {
		common.WriteError(w, http.StatusBadRequest, "VALIDATION_ERROR", "platform 只能是 all / ios / android")
		return
	}
	if !s.verify2FA(w, a, req.TOTPCode) {
		return
	}
	ds, err := loadPushDevices()
	if err != nil {
		internalErr(w, "push devices", err)
		return
	}
	n := 0
	need := map[string]bool{}
	for _, d := range ds {
		if d.Token == "" || (req.UID != "" && d.UID != req.UID) || (req.Platform != "all" && d.Platform != req.Platform) {
			continue
		}
		n++
		need[d.Provider] = true
	}
	creds := pushCreds()
	status, note := "queued", ""
	missing := []string{}
	for p := range need {
		if !creds[p] {
			missing = append(missing, p)
		}
	}
	if !creds["apns"] && !creds["fcm"] {
		status, note = "no_credentials", "未配置 APNs / FCM 凭证，仅记录任务，未实际推送"
	} else if len(missing) > 0 {
		sort.Strings(missing)
		status, note = "no_credentials", "缺少凭证: "+strings.Join(missing, ",")+"，仅记录任务"
	} else {
		note = "推送发送器尚未实现，任务已排队"
	}
	target := "all"
	if req.UID != "" {
		target = "uid:" + req.UID
	}
	var id int64
	if err := s.db.QueryRowContext(r.Context(), `INSERT INTO admin_push_jobs (title, body, target, platform, device_count, status, note, created_by) VALUES ($1,$2,$3,$4,$5,$6,$7,$8) RETURNING id`,
		req.Title, req.Body, target, req.Platform, n, status, note, a.Username).Scan(&id); err != nil {
		internalErr(w, "push job", err)
		return
	}
	s.audit(r, a, "push", "create_job", "push_job:"+strconv.FormatInt(id, 10), levelNormal, status, nil, map[string]any{"title": req.Title, "target": target, "platform": req.Platform, "devices": n}, req.Reason)
	common.WriteJSON(w, http.StatusOK, map[string]any{"id": id, "status": status, "deviceCount": n, "note": note})
}
