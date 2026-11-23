package main

// P1 只读列表：现货成交 / 合约委托 / 合约成交 / 登录设备 / 报表导出 / 合约风险参数。
//
// 大表（orders 1.8 千万、fut_orders 7 百万、trades 6 百万、fut_trades 2 百万）不做 count(*)，
// 一律按主键倒序 + before 游标（keyset）分页，在只读事务里带 3s statement_timeout；
// 超时返回 QUERY_TIMEOUT 提示加过滤条件，而不是拖慢数据库。

import (
	"context"
	"database/sql"
	"errors"
	"net/http"
	"strconv"
	"strings"

	"github.com/jackc/pgx/v5/pgconn"
	"github.com/linbit/platform/services/go/common"
)

// seekList runs "SELECT cols FROM from WHERE … ORDER BY key DESC LIMIT size+1" in a
// read-only, time-boxed transaction and writes {items, hasMore, nextBefore, pageSize}.
func (s *Server) seekList(w http.ResponseWriter, r *http.Request, where, cols, from, key, keyAlias string, f *filter) {
	size, _ := strconv.Atoi(r.URL.Query().Get("size"))
	if size < 1 || size > 200 {
		size = 50
	}
	if b := strings.TrimSpace(r.URL.Query().Get("before")); b != "" {
		n, err := strconv.ParseInt(b, 10, 64)
		if err != nil || n <= 0 {
			common.WriteError(w, http.StatusBadRequest, "VALIDATION_ERROR", "before 无效")
			return
		}
		f.add(key+" < ?", n)
	}
	args := append(append([]interface{}{}, f.args...), size+1)
	q := `SELECT ` + cols + ` FROM ` + from + f.sql() + ` ORDER BY ` + key + ` DESC LIMIT $` + strconv.Itoa(len(args))
	items, err := s.readOnlyMaps(r.Context(), "3s", q, args...)
	if err != nil {
		if isTimeout(err) {
			common.WriteError(w, http.StatusUnprocessableEntity, "QUERY_TIMEOUT", "查询超时：请增加 UID / 交易对等过滤条件")
			return
		}
		internalErr(w, where, err)
		return
	}
	resp := map[string]interface{}{"pageSize": size, "hasMore": false}
	if len(items) > size {
		items = items[:size]
		resp["hasMore"] = true
		resp["nextBefore"] = items[size-1][keyAlias]
	}
	resp["items"] = items
	common.WriteJSON(w, http.StatusOK, resp)
}

func (s *Server) readOnlyMaps(ctx context.Context, timeout, q string, args ...interface{}) ([]map[string]interface{}, error) {
	tx, err := s.db.BeginTx(ctx, &sql.TxOptions{ReadOnly: true})
	if err != nil {
		return nil, err
	}
	defer tx.Rollback()
	if _, err := tx.ExecContext(ctx, `SET LOCAL statement_timeout = '`+timeout+`'`); err != nil {
		return nil, err
	}
	rows, err := tx.QueryContext(ctx, q, args...)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	return rowsToMaps(rows)
}

func isTimeout(err error) bool {
	var pe *pgconn.PgError
	return errors.As(err, &pe) && pe.Code == "57014"
}

func (s *Server) p1Routes(a func(pattern, perm string, h http.HandlerFunc)) {
	a("GET /admin/v1/spot/trades", permLedgerView, s.handleSpotTrades)
	a("GET /admin/v1/futures/orders", permLedgerView, s.handleFutOrders)
	a("GET /admin/v1/futures/trades", permLedgerView, s.handleFutTrades)
	a("GET /admin/v1/login-history", permUserView, s.handleLoginHistory)
	a("GET /admin/v1/report-exports", permLedgerView, s.handleReportExports)
	a("GET /admin/v1/futures/risk-overview", permSymbolView, s.handleFutRiskOverview)
	a("GET /admin/v1/futures/funding", permSymbolView, s.handleFutFunding)
	a("GET /admin/v1/futures/marks", permSymbolView, s.handleFutMarks)
	a("GET /admin/v1/futures/adl", permLedgerView, s.handleFutADL)
	a("GET /admin/v1/futures/risk-tiers", permSymbolView, s.handleFutRiskTiers)
	a("GET /admin/v1/referrals/{uid}/invitees", permUserView, s.handleInvitees)
}

// 直接邀请的下级（growth.g_users，只读）。
func (s *Server) handleInvitees(w http.ResponseWriter, r *http.Request) {
	f := &filter{}
	f.add("inviter = ?", r.PathValue("uid"))
	s.pagedList(w, r, "invitees", `uid, kyc_level AS "kycLevel", registered_at AS "registeredAt", valid_at AS "validAt",
		(SELECT count(*) FROM g_users g2 WHERE g2.inviter = g_users.uid) AS "subInvited"`, "g_users", "registered_at DESC", f, nil)
}

func upperQ(r *http.Request, k string) string { return strings.ToUpper(strings.TrimSpace(r.URL.Query().Get(k))) }

func (s *Server) handleSpotTrades(w http.ResponseWriter, r *http.Request) {
	f := &filter{}
	f.eq(r, "uid", "user_id")
	if v := upperQ(r, "symbol"); v != "" {
		f.add("symbol = ?", v)
	}
	f.eq(r, "side", "side")
	f.eq(r, "orderId", "order_id")
	s.seekList(w, r, "spot trades", `id, trade_id AS "tradeId", order_id AS "orderId", user_id AS uid, symbol, side, role, price::text AS price,
		quantity::text AS quantity, (price*quantity)::text AS "quoteQty", fee::text AS fee, fee_asset AS "feeAsset", created_at AS "createdAt"`,
		"trades", "id", "id", f)
}

func (s *Server) handleFutOrders(w http.ResponseWriter, r *http.Request) {
	f := &filter{}
	f.eq(r, "uid", "user_id")
	if v := upperQ(r, "symbol"); v != "" {
		f.add("symbol = ?", v)
	}
	f.eq(r, "side", "side")
	switch v := r.URL.Query().Get("status"); v {
	case "":
	case "open":
		f.where = append(f.where, "status IN ('new','partially_filled')")
	default:
		f.add("status = ?", v)
	}
	s.seekList(w, r, "futures orders", `id, user_id AS uid, symbol, side, position_side AS "positionSide", type, tif, price::text AS price, qty::text AS qty,
		filled::text AS filled, CASE WHEN filled > 0 THEN (filled_quote/filled)::text END AS "avgPrice", fee::text AS fee, status, reduce_only AS "reduceOnly",
		leverage, margin_mode AS "marginMode", amend_count AS "amendCount", cancel_reason AS "cancelReason", created_at AS "createdAt", updated_at AS "updatedAt"`,
		"fut_orders", "id", "id", f)
}

func (s *Server) handleFutTrades(w http.ResponseWriter, r *http.Request) {
	f := &filter{}
	uid := strings.TrimSpace(r.URL.Query().Get("uid"))
	if uid != "" {
		f.args = append(f.args, uid)
		n := "$" + strconv.Itoa(len(f.args))
		f.where = append(f.where, "(maker_user = "+n+" OR taker_user = "+n+")")
	}
	if v := upperQ(r, "symbol"); v != "" {
		f.add("symbol = ?", v)
	}
	cols := `id, symbol, price::text AS price, qty::text AS qty, (price*qty)::text AS notional, taker_side AS "takerSide", maker_user AS "makerUid", taker_user AS "takerUid",
		maker_order AS "makerOrder", taker_order AS "takerOrder", maker_fee::text AS "makerFee", taker_fee::text AS "takerFee", created_at AS "createdAt"`
	s.seekList(w, r, "futures trades", cols, "fut_trades", "id", "id", f)
}

// 登录设备 / IP（account.devices：每台设备首次与最近一次登录）。
func (s *Server) handleLoginHistory(w http.ResponseWriter, r *http.Request) {
	f := &filter{}
	f.eq(r, "uid", "u.uid")
	if v := strings.TrimSpace(r.URL.Query().Get("ip")); v != "" {
		f.add("d.ip = ?", v)
	}
	s.pagedList(w, r, "login history", `d.id, u.uid, d.name, d.user_agent AS "userAgent", d.ip, d.first_seen AS "firstSeen", d.last_seen AS "lastSeen",
		(SELECT count(DISTINCT d2.user_id) FROM devices d2 WHERE d2.ip = d.ip AND d.ip <> '') AS "ipUsers"`,
		"devices d JOIN users u ON u.id = d.user_id", "d.last_seen DESC", f, nil)
}

// 报表导出任务（reports.report_exports）。不返回 download_token / file_path。
func (s *Server) handleReportExports(w http.ResponseWriter, r *http.Request) {
	f := &filter{}
	f.eq(r, "uid", "user_id")
	f.eq(r, "status", "status")
	f.eq(r, "kind", "kind")
	stats := map[string]interface{}{}
	if m, err := s.queryMaps(r.Context(), `SELECT count(*) FILTER (WHERE status IN ('queued','running')) AS pending,
		count(*) FILTER (WHERE status = 'failed' AND created_at > now() - interval '24 hours') AS "failed24h",
		count(*) FILTER (WHERE created_at > now() - interval '24 hours') AS "created24h" FROM report_exports`); err == nil && len(m) > 0 {
		stats = m[0]
	}
	s.pagedList(w, r, "report exports", `id, job_id AS "jobId", user_id AS uid, kind, format, from_at AS "fromAt", to_at AS "toAt", status, size_bytes AS "sizeBytes",
		row_count AS "rowCount", error, created_at AS "createdAt", started_at AS "startedAt", finished_at AS "finishedAt", expires_at AS "expiresAt"`,
		"report_exports", "created_at DESC", f, map[string]interface{}{"stats": stats})
}

// 合约风险参数总览（只读）：每个合约的最新标记/指数价、最近资金费率、风险档位、持仓量；保险基金与 ADL。
func (s *Server) handleFutRiskOverview(w http.ResponseWriter, r *http.Request) {
	ctx := r.Context()
	out := map[string]interface{}{}
	errs := []string{}
	q := func(key, sqlq string) {
		m, err := s.readOnlyMaps(ctx, "5s", sqlq)
		if err != nil {
			errs = append(errs, key)
			out[key] = []interface{}{}
			return
		}
		out[key] = m
	}
	q("contracts", `SELECT c.symbol, c.status, c.max_leverage AS "maxLeverage", c.mmr::text AS mmr, c.maker_fee::text AS "makerFee", c.taker_fee::text AS "takerFee",
		m.mark_price::text AS "markPrice", m.index_price::text AS "indexPrice", m.ts AS "markTs",
		CASE WHEN m.index_price > 0 THEN ((m.mark_price - m.index_price) / m.index_price * 10000)::numeric(12,2)::text END AS "basisBps",
		fh.rate::text AS "lastFundingRate", fh.funding_time AS "lastFundingTime", fh.source AS "fundingSource",
		(SELECT count(*) FROM fut_risk_tiers t WHERE t.symbol = c.symbol) AS tiers,
		(SELECT COALESCE(sum(abs(qty)),0)::text FROM fut_positions p WHERE p.symbol = c.symbol AND p.qty > 0) AS "longQty",
		(SELECT COALESCE(sum(abs(qty)),0)::text FROM fut_positions p WHERE p.symbol = c.symbol AND p.qty < 0) AS "shortQty"
		FROM fut_contracts c
		LEFT JOIN LATERAL (SELECT mark_price, index_price, ts FROM fut_mark_history h WHERE h.symbol = c.symbol ORDER BY ts DESC LIMIT 1) m ON true
		LEFT JOIN LATERAL (SELECT rate, funding_time, source FROM fut_funding_history f WHERE f.symbol = c.symbol ORDER BY funding_time DESC LIMIT 1) fh ON true
		ORDER BY c.sort, c.symbol`)
	q("insurance", `SELECT ts, asset, balance::text AS balance, equity::text AS equity FROM fut_insurance_snapshots ORDER BY ts DESC LIMIT 60`)
	q("adl24h", `SELECT symbol, count(*) AS count, COALESCE(sum(counter_pnl),0)::text AS "counterPnl" FROM fut_adl WHERE created_at > now() - interval '24 hours' GROUP BY symbol ORDER BY symbol`)
	out["errors"] = errs
	common.WriteJSON(w, http.StatusOK, out)
}

func (s *Server) handleFutFunding(w http.ResponseWriter, r *http.Request) {
	sym := upperQ(r, "symbol")
	if sym == "" {
		sym = "BTCUSDT"
	}
	size, _ := strconv.Atoi(r.URL.Query().Get("size"))
	if size < 1 || size > 500 {
		size = 100
	}
	m, err := s.readOnlyMaps(r.Context(), "3s", `SELECT funding_time AS "fundingTime", rate::text AS rate, mark_price::text AS "markPrice", source
		FROM fut_funding_history WHERE symbol = $1 ORDER BY funding_time DESC LIMIT $2`, sym, size)
	if err != nil {
		internalErr(w, "funding", err)
		return
	}
	common.WriteJSON(w, http.StatusOK, map[string]interface{}{"symbol": sym, "items": m})
}

func (s *Server) handleFutMarks(w http.ResponseWriter, r *http.Request) {
	sym := upperQ(r, "symbol")
	if sym == "" {
		sym = "BTCUSDT"
	}
	size, _ := strconv.Atoi(r.URL.Query().Get("size"))
	if size < 1 || size > 500 {
		size = 120
	}
	m, err := s.readOnlyMaps(r.Context(), "3s", `SELECT ts, index_price::text AS "indexPrice", mark_price::text AS "markPrice",
		CASE WHEN index_price > 0 THEN ((mark_price - index_price) / index_price * 10000)::numeric(12,2)::text END AS "basisBps"
		FROM fut_mark_history WHERE symbol = $1 ORDER BY ts DESC LIMIT $2`, sym, size)
	if err != nil {
		internalErr(w, "marks", err)
		return
	}
	common.WriteJSON(w, http.StatusOK, map[string]interface{}{"symbol": sym, "items": m})
}

func (s *Server) handleFutADL(w http.ResponseWriter, r *http.Request) {
	f := &filter{}
	if v := upperQ(r, "symbol"); v != "" {
		f.add("symbol = ?", v)
	}
	if v := strings.TrimSpace(r.URL.Query().Get("uid")); v != "" {
		f.args = append(f.args, v)
		n := "$" + strconv.Itoa(len(f.args))
		f.where = append(f.where, "(liq_user = "+n+" OR counter_user = "+n+")")
	}
	s.pagedList(w, r, "adl", `id, symbol, liq_user AS "liqUid", counter_user AS "counterUid", qty::text AS qty, price::text AS price, counter_pnl::text AS "counterPnl", created_at AS "createdAt"`,
		"fut_adl", "id DESC", f, nil)
}

func (s *Server) handleFutRiskTiers(w http.ResponseWriter, r *http.Request) {
	sym := upperQ(r, "symbol")
	if sym == "" {
		sym = "BTCUSDT"
	}
	m, err := s.queryMaps(r.Context(), `SELECT tier, max_notional::text AS "maxNotional", mmr::text AS mmr, maint_amount::text AS "maintAmount", max_leverage AS "maxLeverage"
		FROM fut_risk_tiers WHERE symbol = $1 ORDER BY tier`, sym)
	if err != nil {
		internalErr(w, "risk tiers", err)
		return
	}
	common.WriteJSON(w, http.StatusOK, map[string]interface{}{"symbol": sym, "items": m})
}
