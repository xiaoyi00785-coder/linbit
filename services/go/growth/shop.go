package main

import (
	"context"
	"database/sql"
	"errors"
	"net/http"
	"strconv"
	"strings"
	"time"
	"unicode/utf8"

	"github.com/linbit/platform/services/go/common"
	"github.com/shopspring/decimal"
)

// Points shop. Products cost persistent g_user_points; buying debits the
// balance and dispatches the reward (physical SKUs record shipping details
// and wait for operations to fulfil).

type shopProduct struct {
	ID           int64           `json:"id"`
	SKU          string          `json:"sku"`
	Name         string          `json:"name"`
	Description  string          `json:"description"`
	Image        string          `json:"image"`
	Category     string          `json:"category"`
	CostPoints   int64           `json:"costPoints"`
	Stock        int             `json:"stock"`
	RewardKind   string          `json:"rewardKind"`
	RewardAmount decimal.Decimal `json:"rewardAmount"`
	RewardAsset  string          `json:"rewardAsset"`
	DailyLimit   int             `json:"dailyLimit"`
	UserLimit    int             `json:"userLimit"`
	Enabled      bool            `json:"enabled"`
	Sort         int             `json:"sort"`
}

const shopProductCols = `id, sku, name, description, image, category, cost_points, stock,
	reward_kind, reward_amount, reward_asset, daily_limit, user_limit, enabled, sort`

func scanShopProduct(sc interface{ Scan(...any) error }) (*shopProduct, error) {
	p := &shopProduct{}
	if err := sc.Scan(&p.ID, &p.SKU, &p.Name, &p.Description, &p.Image, &p.Category, &p.CostPoints, &p.Stock,
		&p.RewardKind, &p.RewardAmount, &p.RewardAsset, &p.DailyLimit, &p.UserLimit, &p.Enabled, &p.Sort); err != nil {
		return nil, err
	}
	return p, nil
}

func (s *Server) shopRoutes(mux *http.ServeMux) {
	mux.HandleFunc("GET /points/shop/products", s.handleShopProducts)
	mux.HandleFunc("POST /points/shop/orders", s.user(s.handleShopOrder))
	mux.HandleFunc("GET /points/shop/orders", s.user(s.handleShopOrders))
	mux.HandleFunc("GET /points/balance", s.user(s.handleBalance))
	mux.HandleFunc("GET /points/ledger", s.user(s.handleLedger))
	mux.HandleFunc("POST /internal/points/credit", s.internal(s.handleInternalCredit))
}

func (s *Server) handleShopProducts(w http.ResponseWriter, r *http.Request) {
	category := strings.TrimSpace(r.URL.Query().Get("category"))
	q := `SELECT ` + shopProductCols + ` FROM g_shop_products WHERE enabled`
	args := []any{}
	if category != "" {
		q += ` AND category=$1`
		args = append(args, category)
	}
	q += ` ORDER BY sort, id`
	rows, err := s.db.QueryContext(r.Context(), q, args...)
	if err != nil {
		writeErr(w, err)
		return
	}
	defer rows.Close()
	items := []*shopProduct{}
	for rows.Next() {
		p, err := scanShopProduct(rows)
		if err != nil {
			writeErr(w, err)
			return
		}
		items = append(items, p)
	}
	common.WriteJSON(w, http.StatusOK, map[string]any{"items": items})
}

func (s *Server) handleBalance(w http.ResponseWriter, r *http.Request, uid string) {
	bal, lifetime, err := s.pointsBalanceAndLifetime(r.Context(), uid)
	if err != nil {
		writeErr(w, err)
		return
	}
	common.WriteJSON(w, http.StatusOK, map[string]any{"uid": uid, "balance": bal, "lifetime": lifetime})
}

func (s *Server) handleLedger(w http.ResponseWriter, r *http.Request, uid string) {
	rows, err := s.db.QueryContext(r.Context(), `SELECT id, delta, reason, ref, balance, at
		FROM g_points_ledger WHERE uid=$1 ORDER BY id DESC LIMIT 200`, uid)
	if err != nil {
		writeErr(w, err)
		return
	}
	defer rows.Close()
	items := []map[string]any{}
	for rows.Next() {
		var id, delta, balance int64
		var reason, ref string
		var at time.Time
		if err := rows.Scan(&id, &delta, &reason, &ref, &balance, &at); err != nil {
			writeErr(w, err)
			return
		}
		items = append(items, map[string]any{"id": id, "delta": delta, "reason": reason, "ref": ref, "balance": balance, "time": at.UnixMilli()})
	}
	common.WriteJSON(w, http.StatusOK, map[string]any{"items": items})
}

type shopOrderRequest struct {
	ProductID     int64  `json:"productId"`
	SKU           string `json:"sku"`
	Quantity      int    `json:"quantity"`
	ShippingName  string `json:"shippingName"`
	ShippingPhone string `json:"shippingPhone"`
	ShippingAddr  string `json:"shippingAddr"`
}

func (s *Server) handleShopOrder(w http.ResponseWriter, r *http.Request, uid string) {
	ctx := r.Context()
	var req shopOrderRequest
	if err := common.DecodeJSON(r, &req); err != nil {
		common.WriteError(w, http.StatusBadRequest, "INVALID_REQUEST", "请求格式错误")
		return
	}
	if req.Quantity <= 0 {
		req.Quantity = 1
	}
	if req.Quantity > 10 {
		writeErr(w, bad("INVALID_QTY", "单次兑换数量不得超过 10"))
		return
	}
	if err := s.ensureUser(ctx, uid); err != nil {
		writeErr(w, err)
		return
	}
	tx, err := s.db.BeginTx(ctx, nil)
	if err != nil {
		writeErr(w, err)
		return
	}
	defer tx.Rollback()
	var row *sql.Row
	if req.ProductID > 0 {
		row = tx.QueryRowContext(ctx, `SELECT `+shopProductCols+` FROM g_shop_products WHERE id=$1 AND enabled FOR UPDATE`, req.ProductID)
	} else {
		row = tx.QueryRowContext(ctx, `SELECT `+shopProductCols+` FROM g_shop_products WHERE sku=$1 AND enabled FOR UPDATE`, req.SKU)
	}
	p, err := scanShopProduct(row)
	if errors.Is(err, sql.ErrNoRows) {
		writeErr(w, notFound("PRODUCT_NOT_FOUND", "商品不存在或已下架"))
		return
	} else if err != nil {
		writeErr(w, err)
		return
	}
	if p.Category == "physical" {
		name, ok1 := clean(req.ShippingName, 32)
		phone, ok2 := clean(req.ShippingPhone, 32)
		addr, ok3 := clean(req.ShippingAddr, 300)
		if name == "" || phone == "" || addr == "" || !ok1 || !ok2 || !ok3 || utf8.RuneCountInString(addr) < 8 {
			writeErr(w, bad("INVALID_SHIPPING", "请填写完整收件信息"))
			return
		}
		req.ShippingName, req.ShippingPhone, req.ShippingAddr = name, phone, addr
	}
	if p.Stock >= 0 && int64(p.Stock) < int64(req.Quantity) {
		writeErr(w, conflict("OUT_OF_STOCK", "库存不足"))
		return
	}
	if p.UserLimit > 0 {
		var owned int
		_ = tx.QueryRowContext(ctx, `SELECT COALESCE(SUM(quantity),0) FROM g_shop_orders WHERE uid=$1 AND product_id=$2 AND status <> 'cancelled'`, uid, p.ID).Scan(&owned)
		if owned+req.Quantity > p.UserLimit {
			writeErr(w, conflict("USER_LIMIT_REACHED", "已达个人兑换上限"))
			return
		}
	}
	if p.DailyLimit > 0 {
		var today int
		_ = tx.QueryRowContext(ctx, `SELECT COALESCE(SUM(quantity),0) FROM g_shop_orders WHERE product_id=$1 AND status <> 'cancelled' AND created_at >= $2`, p.ID, utcDay(s.now())).Scan(&today)
		if today+req.Quantity > p.DailyLimit {
			writeErr(w, conflict("DAILY_LIMIT_REACHED", "今日兑换已达上限"))
			return
		}
	}
	cost := p.CostPoints * int64(req.Quantity)
	// Debit points with row-level lock (pointsBalanceLocked serializes per uid).
	bal, err := s.pointsBalanceLocked(ctx, tx, uid)
	if err != nil {
		writeErr(w, err)
		return
	}
	if bal < cost {
		writeErr(w, conflict("INSUFFICIENT_POINTS", "积分不足"))
		return
	}
	if p.Stock >= 0 {
		if _, err := tx.ExecContext(ctx, `UPDATE g_shop_products SET stock=stock-$2 WHERE id=$1`, p.ID, req.Quantity); err != nil {
			writeErr(w, err)
			return
		}
	}
	// Create order first (gives us id for reward ref).
	var orderID int64
	status := "pending"
	if p.Category != "physical" {
		status = "delivered" // voucher/asset — instant delivery
	}
	if err := tx.QueryRowContext(ctx, `INSERT INTO g_shop_orders (uid, product_id, sku, quantity, cost_points, status, shipping_name, shipping_phone, shipping_addr)
		VALUES ($1,$2,$3,$4,$5,$6,$7,$8,$9) RETURNING id`,
		uid, p.ID, p.SKU, req.Quantity, cost, status, req.ShippingName, req.ShippingPhone, req.ShippingAddr).Scan(&orderID); err != nil {
		writeErr(w, err)
		return
	}
	ref := "shop:" + strconv.FormatInt(orderID, 10)
	if err := s.debitPoints(ctx, tx, uid, cost, "shop:"+p.SKU, ref); err != nil {
		writeErr(w, err)
		return
	}
	// Dispatch the reward for non-physical items.
	switch p.RewardKind {
	case "coupon_trial":
		for i := 0; i < req.Quantity; i++ {
			title := couponTitle("trial", p.RewardAmount, p.RewardAsset)
			if _, err := s.issueCoupon(ctx, tx, uid, "trial", p.RewardAmount, title, ref+":"+strconv.Itoa(i)); err != nil {
				writeErr(w, err)
				return
			}
		}
	case "coupon_deduction":
		for i := 0; i < req.Quantity; i++ {
			title := couponTitle("deduction", p.RewardAmount, p.RewardAsset)
			if _, err := s.issueCoupon(ctx, tx, uid, "deduction", p.RewardAmount, title, ref+":"+strconv.Itoa(i)); err != nil {
				writeErr(w, err)
				return
			}
		}
	case "asset":
		total := p.RewardAmount.Mul(decimal.NewFromInt(int64(req.Quantity)))
		if err := s.systemTransfer(ctx, uid, p.RewardAsset, "spot", "credit", total, ref); err != nil {
			writeErr(w, err)
			return
		}
	case "physical":
		// handled by ops; order stays 'pending' until shipped
	}
	if _, err := tx.ExecContext(ctx, `UPDATE g_shop_orders SET reward_ref=$2 WHERE id=$1`, orderID, ref); err != nil {
		writeErr(w, err)
		return
	}
	if err := tx.Commit(); err != nil {
		writeErr(w, err)
		return
	}
	newBal, _ := s.pointsBalance2(ctx, uid)
	common.WriteJSON(w, http.StatusOK, map[string]any{"orderId": orderID, "sku": p.SKU, "quantity": req.Quantity,
		"costPoints": cost, "status": status, "pointsBalance": newBal, "ref": ref})
}

func (s *Server) handleShopOrders(w http.ResponseWriter, r *http.Request, uid string) {
	rows, err := s.db.QueryContext(r.Context(), `SELECT o.id, o.sku, p.name, o.quantity, o.cost_points, o.status,
		o.shipping_name, o.shipping_phone, o.shipping_addr, o.tracking, o.reward_ref, o.created_at, o.updated_at
		FROM g_shop_orders o JOIN g_shop_products p ON p.id=o.product_id WHERE o.uid=$1 ORDER BY o.id DESC LIMIT 200`, uid)
	if err != nil {
		writeErr(w, err)
		return
	}
	defer rows.Close()
	items := []map[string]any{}
	for rows.Next() {
		var id int64
		var qty int
		var cost int64
		var sku, name, status, sName, sPhone, sAddr, tracking, ref string
		var created, updated time.Time
		if err := rows.Scan(&id, &sku, &name, &qty, &cost, &status, &sName, &sPhone, &sAddr, &tracking, &ref, &created, &updated); err != nil {
			writeErr(w, err)
			return
		}
		item := map[string]any{"id": id, "sku": sku, "name": name, "quantity": qty, "costPoints": cost,
			"status": status, "tracking": tracking, "ref": ref, "createdAt": created.UnixMilli(), "updatedAt": updated.UnixMilli()}
		if sName != "" {
			item["shipping"] = map[string]string{"name": sName, "phone": sPhone, "address": sAddr}
		}
		items = append(items, item)
	}
	common.WriteJSON(w, http.StatusOK, map[string]any{"items": items})
}

// --- points balance helpers (shared with tasks.go) ---------------------

func (s *Server) pointsBalance2(ctx context.Context, uid string) (int64, error) {
	var bal int64
	err := s.db.QueryRowContext(ctx, `SELECT COALESCE(balance,0) FROM g_user_points WHERE uid=$1`, uid).Scan(&bal)
	if errors.Is(err, sql.ErrNoRows) {
		return 0, nil
	}
	return bal, err
}

func (s *Server) pointsBalanceAndLifetime(ctx context.Context, uid string) (int64, int64, error) {
	var bal, life int64
	err := s.db.QueryRowContext(ctx, `SELECT balance, lifetime FROM g_user_points WHERE uid=$1`, uid).Scan(&bal, &life)
	if errors.Is(err, sql.ErrNoRows) {
		return 0, 0, nil
	}
	return bal, life, err
}

func (s *Server) pointsBalanceLocked(ctx context.Context, tx *sql.Tx, uid string) (int64, error) {
	if _, err := tx.ExecContext(ctx, `INSERT INTO g_user_points (uid, balance, lifetime) VALUES ($1,0,0) ON CONFLICT (uid) DO NOTHING`, uid); err != nil {
		return 0, err
	}
	var bal int64
	err := tx.QueryRowContext(ctx, `SELECT balance FROM g_user_points WHERE uid=$1 FOR UPDATE`, uid).Scan(&bal)
	return bal, err
}

func (s *Server) creditPoints(ctx context.Context, tx *sql.Tx, uid string, amount int64, reason, ref string) error {
	if amount <= 0 {
		return nil
	}
	bal, err := s.pointsBalanceLocked(ctx, tx, uid)
	if err != nil {
		return err
	}
	bal += amount
	if _, err := tx.ExecContext(ctx, `UPDATE g_user_points SET balance=$2, lifetime=lifetime+$3, updated_at=now() WHERE uid=$1`, uid, bal, amount); err != nil {
		return err
	}
	_, err = tx.ExecContext(ctx, `INSERT INTO g_points_ledger (uid, delta, reason, ref, balance) VALUES ($1,$2,$3,$4,$5)
		ON CONFLICT (ref) DO NOTHING`, uid, amount, reason, ref, bal)
	return err
}

func (s *Server) debitPoints(ctx context.Context, tx *sql.Tx, uid string, amount int64, reason, ref string) error {
	if amount <= 0 {
		return nil
	}
	bal, err := s.pointsBalanceLocked(ctx, tx, uid)
	if err != nil {
		return err
	}
	if bal < amount {
		return conflict("INSUFFICIENT_POINTS", "积分不足")
	}
	bal -= amount
	if _, err := tx.ExecContext(ctx, `UPDATE g_user_points SET balance=$2, updated_at=now() WHERE uid=$1`, uid, bal); err != nil {
		return err
	}
	_, err = tx.ExecContext(ctx, `INSERT INTO g_points_ledger (uid, delta, reason, ref, balance) VALUES ($1,$2,$3,$4,$5)
		ON CONFLICT (ref) DO NOTHING`, uid, -amount, reason, ref, bal)
	return err
}

// POST /internal/points/credit — service-to-service points credit (e.g. ops
// grants a makeup, or growth campaigns flush to the persistent wallet).
func (s *Server) handleInternalCredit(w http.ResponseWriter, r *http.Request) {
	var b struct {
		UID    string `json:"uid"`
		Amount int64  `json:"amount"`
		Reason string `json:"reason"`
		Ref    string `json:"ref"`
	}
	if err := common.DecodeJSON(r, &b); err != nil || b.UID == "" || b.Amount == 0 || b.Ref == "" {
		common.WriteError(w, http.StatusBadRequest, "INVALID_REQUEST", "uid, amount, ref required")
		return
	}
	if err := s.ensureUser(r.Context(), b.UID); err != nil {
		writeErr(w, err)
		return
	}
	tx, err := s.db.BeginTx(r.Context(), nil)
	if err != nil {
		writeErr(w, err)
		return
	}
	defer tx.Rollback()
	if b.Amount > 0 {
		if err := s.creditPoints(r.Context(), tx, b.UID, b.Amount, b.Reason, b.Ref); err != nil {
			writeErr(w, err)
			return
		}
	} else {
		if err := s.debitPoints(r.Context(), tx, b.UID, -b.Amount, b.Reason, b.Ref); err != nil {
			writeErr(w, err)
			return
		}
	}
	if err := tx.Commit(); err != nil {
		writeErr(w, err)
		return
	}
	bal, life, _ := s.pointsBalanceAndLifetime(r.Context(), b.UID)
	common.WriteJSON(w, http.StatusOK, map[string]any{"uid": b.UID, "balance": bal, "lifetime": life})
}
