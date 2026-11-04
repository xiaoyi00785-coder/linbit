package main

// Read-only operational views for the admin console. Every service shares
// the same Postgres database, so list pages and dashboard aggregates are
// served straight from the source tables here instead of being proxied.
// Writes still go through the owning service's /internal API (gapWrite).

import (
	"context"
	"database/sql"
	"net/http"
	"strconv"
	"strings"
	"time"

	"github.com/linbit/platform/services/go/common"
)

// filter builds a parameterised WHERE clause.
type filter struct {
	where []string
	args  []interface{}
}

func (f *filter) add(cond string, v interface{}) {
	f.args = append(f.args, v)
	f.where = append(f.where, strings.ReplaceAll(cond, "?", "$"+strconv.Itoa(len(f.args))))
}

// eq adds "col = value" when the query parameter is non-empty.
func (f *filter) eq(r *http.Request, param, col string) {
	if v := strings.TrimSpace(r.URL.Query().Get(param)); v != "" {
		f.add(col+" = ?", v)
	}
}

func (f *filter) sql() string {
	if len(f.where) == 0 {
		return ""
	}
	return " WHERE " + strings.Join(f.where, " AND ")
}

// rowsToMaps scans arbitrary rows into JSON-friendly maps keyed by column
// alias. NUMERIC values arrive as []byte and are kept as exact strings.
func rowsToMaps(rows *sql.Rows) ([]map[string]interface{}, error) {
	cols, err := rows.Columns()
	if err != nil {
		return nil, err
	}
	out := []map[string]interface{}{}
	for rows.Next() {
		vals := make([]interface{}, len(cols))
		ptrs := make([]interface{}, len(cols))
		for i := range vals {
			ptrs[i] = &vals[i]
		}
		if err := rows.Scan(ptrs...); err != nil {
			return nil, err
		}
		m := make(map[string]interface{}, len(cols))
		for i, c := range cols {
			switch v := vals[i].(type) {
			case []byte:
				m[c] = string(v)
			case time.Time:
				m[c] = v.UTC().Format(time.RFC3339)
			default:
				m[c] = v
			}
		}
		out = append(out, m)
	}
	return out, rows.Err()
}

func (s *Server) queryMaps(ctx context.Context, q string, args ...interface{}) ([]map[string]interface{}, error) {
	rows, err := s.db.QueryContext(ctx, q, args...)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	return rowsToMaps(rows)
}

// pagedList runs "SELECT <cols> FROM <from> <where> ORDER BY <order>" with
// count + LIMIT/OFFSET and writes {items,total,page,pageSize}.
func (s *Server) pagedList(w http.ResponseWriter, r *http.Request, where string, cols, from, order string, f *filter, extra map[string]interface{}) {
	page, size := pageParams(r)
	cond := f.sql()
	var total int
	if err := s.db.QueryRowContext(r.Context(), `SELECT count(*) FROM `+from+cond, f.args...).Scan(&total); err != nil {
		internalErr(w, where+" count", err)
		return
	}
	args := append(append([]interface{}{}, f.args...), size, (page-1)*size)
	n := len(args)
	items, err := s.queryMaps(r.Context(), `SELECT `+cols+` FROM `+from+cond+` ORDER BY `+order+
		` LIMIT $`+strconv.Itoa(n-1)+` OFFSET $`+strconv.Itoa(n), args...)
	if err != nil {
		internalErr(w, where+" query", err)
		return
	}
	resp := map[string]interface{}{"items": items, "total": total, "page": page, "pageSize": size}
	for k, v := range extra {
		resp[k] = v
	}
	common.WriteJSON(w, http.StatusOK, resp)
}

// ---- dashboard ----

func (s *Server) handleDashboard(w http.ResponseWriter, r *http.Request) {
	ctx := r.Context()
	out := map[string]interface{}{"generatedAt": s.now().UTC().Format(time.RFC3339)}
	errs := []string{}
	one := func(key, q string) {
		m, err := s.queryMaps(ctx, q)
		if err != nil {
			errs = append(errs, key)
			out[key] = nil
			return
		}
		if len(m) > 0 {
			out[key] = m[0]
		} else {
			out[key] = map[string]interface{}{}
		}
	}
	list := func(key, q string) {
		m, err := s.queryMaps(ctx, q)
		if err != nil {
			errs = append(errs, key)
			out[key] = nil
			return
		}
		out[key] = m
	}
	one("users", `SELECT count(*) AS total,
		count(*) FILTER (WHERE created_at > now() - interval '24 hours') AS new24h,
		count(*) FILTER (WHERE kyc_level >= 1) AS kyc,
		count(*) FILTER (WHERE status <> 'active') AS frozen FROM users`)
	one("kyc", `SELECT count(*) FILTER (WHERE status = 'pending') AS pending FROM kyc_applications`)
	list("withdrawStatus", `SELECT status, count(*) AS count FROM withdrawals GROUP BY status ORDER BY status`)
	list("deposits24h", `SELECT asset, count(*) AS count, sum(amount)::text AS amount FROM deposits
		WHERE status = 'completed' AND created_at > now() - interval '24 hours' GROUP BY asset ORDER BY asset`)
	list("withdrawals24h", `SELECT asset, count(*) AS count, sum(amount)::text AS amount FROM withdrawals
		WHERE status = 'completed' AND created_at > now() - interval '24 hours' GROUP BY asset ORDER BY asset`)
	one("spot24h", `SELECT count(*) AS trades, COALESCE(sum(price*quantity),0)::text AS "quoteVolume"
		FROM trades WHERE role = 'taker' AND created_at > now() - interval '24 hours'`)
	list("spotFees24h", `SELECT fee_asset AS asset, sum(fee)::text AS amount FROM trades
		WHERE created_at > now() - interval '24 hours' AND fee > 0 GROUP BY fee_asset ORDER BY fee_asset`)
	one("futures24h", `SELECT count(*) AS trades, COALESCE(sum(price*qty),0)::text AS volume,
		COALESCE(sum(maker_fee+taker_fee),0)::text AS fees
		FROM fut_trades WHERE created_at > now() - interval '24 hours'`)
	one("openInterest", `SELECT count(*) FILTER (WHERE qty <> 0) AS positions,
		COALESCE(sum(abs(qty)*entry_price),0)::text AS notional FROM fut_positions`)
	one("liquidations24h", `SELECT count(*) AS count, COALESCE(sum(qty*mark_price),0)::text AS notional,
		COALESCE(sum(shortfall),0)::text AS shortfall FROM fut_liquidations WHERE created_at > now() - interval '24 hours'`)
	one("insurance", `SELECT asset, balance::text AS balance, ts FROM fut_insurance_snapshots ORDER BY ts DESC LIMIT 1`)
	list("daily", `WITH d AS (SELECT generate_series(date_trunc('day', now()) - interval '29 days', date_trunc('day', now()), interval '1 day') AS day)
		SELECT to_char(d.day,'YYYY-MM-DD') AS day,
		(SELECT count(*) FROM users u WHERE u.created_at >= d.day AND u.created_at < d.day + interval '1 day') AS signups,
		(SELECT COALESCE(sum(price*quantity),0)::text FROM trades t WHERE t.role='taker' AND t.created_at >= d.day AND t.created_at < d.day + interval '1 day') AS "spotVolume",
		(SELECT COALESCE(sum(price*qty),0)::text FROM fut_trades f WHERE f.created_at >= d.day AND f.created_at < d.day + interval '1 day') AS "futuresVolume",
		(SELECT COALESCE(sum(maker_fee+taker_fee),0)::text FROM fut_trades f WHERE f.created_at >= d.day AND f.created_at < d.day + interval '1 day') AS "futuresFees"
		FROM d ORDER BY d.day`)
	list("topContracts", `SELECT symbol, count(*) AS trades, sum(price*qty)::text AS volume FROM fut_trades
		WHERE created_at > now() - interval '24 hours' GROUP BY symbol ORDER BY sum(price*qty) DESC LIMIT 8`)
	list("recentWithdrawals", `SELECT id, user_id AS uid, asset, network, amount::text AS amount, status, risk_score AS "riskScore", created_at AS "createdAt"
		FROM withdrawals ORDER BY created_at DESC LIMIT 8`)
	out["errors"] = errs
	common.WriteJSON(w, http.StatusOK, out)
}

// ---- lists ----

func (s *Server) handleRiskUsers(w http.ResponseWriter, r *http.Request) {
	f := &filter{}
	f.add("status <> ?", "active")
	f.eq(r, "uid", "uid")
	s.pagedList(w, r, "risk users", `uid, email, phone, nickname, kyc_level AS "kycLevel", status,
		status_reason AS reason, status_changed_at AS "changedAt", created_at AS "createdAt"`,
		"users", "status_changed_at DESC NULLS LAST, uid", f, nil)
}

func (s *Server) handleDeposits(w http.ResponseWriter, r *http.Request) {
	f := &filter{}
	f.eq(r, "uid", "user_id")
	f.eq(r, "asset", "asset")
	f.eq(r, "network", "network")
	f.eq(r, "status", "status")
	f.eq(r, "txHash", "tx_hash")
	s.pagedList(w, r, "deposits", `id, user_id AS uid, asset, network, address, amount::text AS amount, tx_hash AS "txHash",
		confirmations, status, credited_at AS "creditedAt", created_at AS "createdAt"`, "deposits", "created_at DESC, id DESC", f, nil)
}

func (s *Server) handleSpotOrders(w http.ResponseWriter, r *http.Request) {
	f := &filter{}
	f.eq(r, "uid", "user_id")
	f.eq(r, "symbol", "symbol")
	f.eq(r, "side", "side")
	if v := r.URL.Query().Get("status"); v == "open" {
		f.where = append(f.where, "status IN ('new','partially_filled')")
	} else if v != "" {
		f.add("status = ?", v)
	}
	s.pagedList(w, r, "spot orders", `order_id AS "orderId", user_id AS uid, symbol, side, type, price::text AS price,
		quantity::text AS quantity, filled_quantity::text AS filled, status, created_at AS "createdAt"`,
		"orders", "created_at DESC", f, nil)
}

func (s *Server) handleFutPositions(w http.ResponseWriter, r *http.Request) {
	f := &filter{}
	f.add("p.qty <> ?", 0)
	f.eq(r, "uid", "p.user_id")
	f.eq(r, "symbol", "p.symbol")
	switch r.URL.Query().Get("side") {
	case "long":
		f.add("p.qty > ?", 0)
	case "short":
		f.add("p.qty < ?", 0)
	}
	s.pagedList(w, r, "fut positions", `p.user_id AS uid, p.symbol, p.position_side AS "positionSide", p.qty::text AS qty,
		p.entry_price::text AS "entryPrice", m.mark_price::text AS "markPrice", p.margin_mode AS "marginMode", p.leverage,
		p.iso_margin::text AS "isoMargin", p.realized_pnl::text AS "realizedPnl",
		(abs(p.qty)*COALESCE(m.mark_price,p.entry_price))::text AS notional,
		((COALESCE(m.mark_price,p.entry_price)-p.entry_price)*p.qty)::text AS "unrealizedPnl"`,
		`fut_positions p LEFT JOIN LATERAL (SELECT mark_price FROM fut_mark_history h WHERE h.symbol = p.symbol ORDER BY ts DESC LIMIT 1) m ON true`,
		"abs(p.qty)*p.entry_price DESC", f, nil)
}

func (s *Server) handleGrids(w http.ResponseWriter, r *http.Request) {
	f := &filter{}
	f.eq(r, "uid", "user_id")
	f.eq(r, "symbol", "symbol")
	f.eq(r, "status", "status")
	s.pagedList(w, r, "grids", `id, user_id AS uid, symbol, mode, lower_price::text AS "lowerPrice", upper_price::text AS "upperPrice",
		grids, investment::text AS investment, grid_profit::text AS "gridProfit", status, created_at AS "createdAt", stopped_at AS "stoppedAt"`,
		"grid_bots", "created_at DESC, id DESC", f, nil)
}

func (s *Server) handleLiquidations(w http.ResponseWriter, r *http.Request) {
	f := &filter{}
	f.eq(r, "uid", "user_id")
	f.eq(r, "symbol", "symbol")
	extra := map[string]interface{}{}
	if ins, err := s.queryMaps(r.Context(), `SELECT asset, balance::text AS balance, equity::text AS equity, ts
		FROM fut_insurance_snapshots ORDER BY ts DESC LIMIT 30`); err == nil {
		extra["insurance"] = ins
	}
	s.pagedList(w, r, "liquidations", `id, user_id AS uid, symbol, margin_mode AS "marginMode", qty::text AS qty,
		mark_price::text AS "markPrice", fee::text AS fee, shortfall::text AS shortfall, created_at AS "createdAt"`,
		"fut_liquidations", "created_at DESC, id DESC", f, extra)
}

func (s *Server) handleFutContracts(w http.ResponseWriter, r *http.Request) {
	items, err := s.queryMaps(r.Context(), `SELECT symbol, base, max_leverage AS "maxLeverage", mmr::text AS mmr, tick_size::text AS "tickSize",
		min_qty::text AS "minQty", max_qty::text AS "maxQty", maker_fee::text AS "makerFee", taker_fee::text AS "takerFee", status,
		(SELECT count(*) FROM fut_risk_tiers t WHERE t.symbol = c.symbol) AS tiers,
		(SELECT COALESCE(sum(abs(qty)*entry_price),0)::text FROM fut_positions p WHERE p.symbol = c.symbol) AS "openInterest"
		FROM fut_contracts c ORDER BY symbol`)
	if err != nil {
		internalErr(w, "fut contracts", err)
		return
	}
	common.WriteJSON(w, http.StatusOK, map[string]interface{}{"items": items})
}

func (s *Server) handleCopyTraders(w http.ResponseWriter, r *http.Request) {
	f := &filter{}
	f.eq(r, "status", "t.status")
	s.pagedList(w, r, "copy traders", `t.user_id AS uid, t.nickname, t.profit_share::text AS "profitShare", t.status,
		(SELECT count(*) FROM fut_copy_follows c WHERE c.trader = t.user_id AND c.status = 'active') AS followers,
		(SELECT COALESCE(sum(principal),0)::text FROM fut_copy_follows c WHERE c.trader = t.user_id AND c.status = 'active') AS aum,
		(SELECT COALESCE(sum(share_paid),0)::text FROM fut_copy_follows c WHERE c.trader = t.user_id) AS "sharePaid"`,
		"fut_copy_traders t", "t.user_id", f, nil)
}

func (s *Server) handleCommissions(w http.ResponseWriter, r *http.Request) {
	f := &filter{}
	f.eq(r, "uid", "beneficiary")
	f.eq(r, "sourceUid", "source_uid")
	f.eq(r, "kind", "kind")
	extra := map[string]interface{}{}
	if sum, err := s.queryMaps(r.Context(), `SELECT kind, count(*) AS count, sum(amount)::text AS amount FROM g_commissions
		WHERE day > now() - interval '30 days' GROUP BY kind ORDER BY kind`); err == nil {
		extra["summary30d"] = sum
	}
	s.pagedList(w, r, "commissions", `beneficiary AS uid, source_uid AS "sourceUid", kind, rate::text AS rate, base::text AS base,
		amount::text AS amount, day`, "g_commissions", "day DESC, beneficiary", f, extra)
}

func (s *Server) handleRedemptions(w http.ResponseWriter, r *http.Request) {
	f := &filter{}
	f.eq(r, "uid", "uid")
	if v := r.URL.Query().Get("campaignId"); v != "" {
		if id, err := strconv.ParseInt(v, 10, 64); err == nil {
			f.add("campaign_id = ?", id)
		}
	}
	s.pagedList(w, r, "redemptions", `campaign_id AS "campaignId", uid, points, coupon_id AS "couponId", at`,
		"g_redemptions", "at DESC", f, nil)
}

func (s *Server) viewRoutes(a func(pattern, perm string, h http.HandlerFunc)) {
	wallet := func() string { return s.cfg.WalletURL }
	a("GET /admin/v1/dashboard/summary", permDashView, s.handleDashboard)
	a("GET /admin/v1/risk-users", permUserView, s.handleRiskUsers)
	a("GET /admin/v1/deposits", permLedgerView, s.handleDeposits)
	a("GET /admin/v1/spot/orders", permLedgerView, s.handleSpotOrders)
	a("GET /admin/v1/futures/positions", permLedgerView, s.handleFutPositions)
	a("GET /admin/v1/futures/liquidations", permLedgerView, s.handleLiquidations)
	a("GET /admin/v1/futures/contracts", permSymbolView, s.handleFutContracts)
	a("GET /admin/v1/grids", permLedgerView, s.handleGrids)
	a("GET /admin/v1/copy/traders", permCopyView, s.handleCopyTraders)
	a("GET /admin/v1/rebate/commissions", permPartnerView, s.handleCommissions)
	a("GET /admin/v1/points/redemptions", permRewardView, s.handleRedemptions)
	a("GET /admin/v1/users/{uid}/devices", permUserView, s.handleUserDevices)
	a("GET /admin/v1/users/{uid}/profile30d", permUserView, s.handleUserProfile30d)
	// Hot-wallet balances and sweep history exist only when a real chain
	// adapter is configured; otherwise the wallet service returns 404.
	a("GET /admin/v1/wallet/hot", permWithdrawView, s.gapRead(wallet, "/internal/admin/wallet/hot"))
	a("GET /admin/v1/wallet/sweeps", permWithdrawView, s.gapRead(wallet, "/internal/admin/wallet/sweeps"))
}

func (s *Server) handleUserDevices(w http.ResponseWriter, r *http.Request) {
	m, err := s.queryMaps(r.Context(), `SELECT d.id, d.name, d.user_agent AS "userAgent", d.ip, d.first_seen AS "firstSeen", d.last_seen AS "lastSeen"
		FROM devices d JOIN users u ON u.id = d.user_id WHERE u.uid = $1 ORDER BY d.last_seen DESC LIMIT 100`, r.PathValue("uid"))
	if err != nil {
		internalErr(w, "user devices", err)
		return
	}
	common.WriteJSON(w, http.StatusOK, map[string]interface{}{"items": m})
}

// handleUserProfile30d summarises one user's last 30 days of flows and volume.
func (s *Server) handleUserProfile30d(w http.ResponseWriter, r *http.Request) {
	uid := r.PathValue("uid")
	out := map[string]interface{}{}
	for key, q := range map[string]string{
		"deposits":    `SELECT asset, count(*) AS count, sum(amount)::text AS amount FROM deposits WHERE user_id = $1 AND status = 'completed' AND created_at > now() - interval '30 days' GROUP BY asset ORDER BY asset`,
		"withdrawals": `SELECT asset, count(*) AS count, sum(amount)::text AS amount FROM withdrawals WHERE user_id = $1 AND status = 'completed' AND created_at > now() - interval '30 days' GROUP BY asset ORDER BY asset`,
		"spot":        `SELECT count(*) AS trades, COALESCE(sum(price*quantity),0)::text AS "quoteVolume" FROM trades WHERE user_id = $1 AND created_at > now() - interval '30 days'`,
		"futures":     `SELECT count(*) AS trades, COALESCE(sum(price*qty),0)::text AS volume FROM fut_trades WHERE (maker_user = $1 OR taker_user = $1) AND created_at > now() - interval '30 days'`,
	} {
		m, err := s.queryMaps(r.Context(), q, uid)
		if err != nil {
			internalErr(w, "user profile", err)
			return
		}
		if key == "spot" || key == "futures" {
			if len(m) > 0 {
				out[key] = m[0]
			}
			continue
		}
		out[key] = m
	}
	common.WriteJSON(w, http.StatusOK, out)
}
