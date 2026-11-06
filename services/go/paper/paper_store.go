package main

import (
	"context"
	"crypto/rand"
	"database/sql"
	"encoding/hex"
	"encoding/json"
	"fmt"
	"net/http"
	"regexp"
	"strings"
	"time"

	"github.com/shopspring/decimal"
)

type Store struct{ db *sql.DB }

func NewStore(db *sql.DB) *Store { return &Store{db: db} }

func randomID(prefix string) string {
	b := make([]byte, 12)
	if _, err := rand.Read(b); err != nil {
		panic(err)
	}
	return prefix + "_" + hex.EncodeToString(b)
}

func utcDay(t time.Time) time.Time {
	t = t.UTC()
	return time.Date(t.Year(), t.Month(), t.Day(), 0, 0, 0, 0, time.UTC)
}

func ensureAccountTx(ctx context.Context, tx *sql.Tx, userID string, now time.Time) (PaperAccount, error) {
	now = now.UTC()
	id := randomID("pa")
	if _, err := tx.ExecContext(ctx, `INSERT INTO paper_accounts (id,user_id,created_at,updated_at) VALUES ($1,$2,$3,$3) ON CONFLICT (user_id) DO NOTHING`, id, userID, now); err != nil {
		return PaperAccount{}, err
	}
	var a PaperAccount
	var initial string
	var day sql.NullTime
	err := tx.QueryRowContext(ctx, `SELECT id,generation,initial_balance::text,resets_used_today,reset_day,created_at,updated_at FROM paper_accounts WHERE user_id=$1 FOR UPDATE`, userID).Scan(&a.AccountID, &a.Generation, &initial, &a.ResetsUsedToday, &day, &a.CreatedAt, &a.UpdatedAt)
	if err != nil {
		return PaperAccount{}, err
	}
	today := utcDay(now)
	if !day.Valid || !day.Time.Equal(today) {
		a.ResetsUsedToday = 0
		if _, err := tx.ExecContext(ctx, `UPDATE paper_accounts SET reset_day=$2,resets_used_today=0 WHERE id=$1`, a.AccountID, today); err != nil {
			return PaperAccount{}, err
		}
	}
	if _, err := tx.ExecContext(ctx, `INSERT INTO paper_balances (account_id,generation,asset,free) VALUES ($1,$2,'USDT',10000) ON CONFLICT DO NOTHING`, a.AccountID, a.Generation); err != nil {
		return PaperAccount{}, err
	}
	if _, err := tx.ExecContext(ctx, `INSERT INTO paper_ledger_entries (account_id,generation,asset,delta,balance_after,business_ref,created_at) SELECT $1,$2,'USDT',10000,10000,'faucet:initial',$3 WHERE NOT EXISTS (SELECT 1 FROM paper_ledger_entries WHERE account_id=$1 AND generation=$2 AND business_ref='faucet:initial')`, a.AccountID, a.Generation, now); err != nil {
		return PaperAccount{}, err
	}
	a.Mode, a.Market, a.QuoteAsset = "paper", "spot", "USDT"
	a.InitialBalance = money(decimal.RequireFromString(initial), 8)
	a.ResetLimitPerUTCDay, a.OrderTypes, a.PriceSource = 1, []string{"market"}, "INDEX"
	a.FeeModel, a.SlippageModel = "none", "none"
	if a.ResetsUsedToday > 0 {
		a.NextResetAt = tptr(today.Add(24 * time.Hour))
	}
	return a, nil
}

func (s *Store) Account(ctx context.Context, userID string) (PaperAccount, error) {
	tx, err := s.db.BeginTx(ctx, nil)
	if err != nil {
		return PaperAccount{}, err
	}
	defer tx.Rollback()
	a, err := ensureAccountTx(ctx, tx, userID, time.Now())
	if err != nil {
		return PaperAccount{}, err
	}
	return a, tx.Commit()
}

type ResetResponse struct {
	AccountID  string         `json:"accountId"`
	Mode       string         `json:"mode"`
	Generation int64          `json:"generation"`
	Balances   []PaperBalance `json:"balances"`
	ResetAt    time.Time      `json:"resetAt"`
}

func (s *Store) Reset(ctx context.Context, userID, key, confirmation string) (ResetResponse, error) {
	if confirmation != "RESET_PAPER_ACCOUNT" {
		return ResetResponse{}, apiErr(http.StatusBadRequest, "RESET_CONFIRMATION_REQUIRED", "请输入 RESET_PAPER_ACCOUNT 确认重置")
	}
	if len(key) < 1 || len(key) > 128 || !asciiKey(key) {
		return ResetResponse{}, apiErr(http.StatusBadRequest, "VALIDATION_ERROR", "Idempotency-Key 必须为 1-128 位 ASCII")
	}
	hash := requestHash(map[string]string{"confirmation": confirmation})
	tx, err := s.db.BeginTx(ctx, nil)
	if err != nil {
		return ResetResponse{}, err
	}
	// Check retained replay before rolling the UTC counter: a same-key retry
	// remains successful even when a UTC boundary or that day's reset intervened.
	defer tx.Rollback()
	if out, ok, err := resetReplayTx(ctx, tx, userID, key, hash); err != nil {
		return ResetResponse{}, err
	} else if ok {
		return out, nil
	}

	a, err := ensureAccountTx(ctx, tx, userID, time.Now())
	if err != nil {
		return ResetResponse{}, err
	}
	// Re-read under the account lock: another identical concurrent reset may
	// have completed while this transaction waited. Return its exact response.
	if out, ok, err := resetReplayTx(ctx, tx, userID, key, hash); err != nil {
		return ResetResponse{}, err
	} else if ok {
		return out, nil
	}
	used := a.ResetsUsedToday
	if used >= 1 {
		return ResetResponse{}, apiErr(http.StatusTooManyRequests, "PAPER_RESET_LIMIT_EXCEEDED", "每 UTC 日仅可重置一次")
	}
	now := time.Now().UTC()
	nextGeneration := a.Generation + 1
	if _, err := tx.ExecContext(ctx, `DELETE FROM paper_balances WHERE account_id=$1`, a.AccountID); err != nil {
		return ResetResponse{}, err
	}
	if _, err := tx.ExecContext(ctx, `INSERT INTO paper_balances (account_id,generation,asset,free) VALUES ($1,$2,'USDT',10000)`, a.AccountID, nextGeneration); err != nil {
		return ResetResponse{}, err
	}
	if _, err := tx.ExecContext(ctx, `INSERT INTO paper_ledger_entries (account_id,generation,asset,delta,balance_after,business_ref,created_at) VALUES ($1,$2,'USDT',10000,10000,$3,$4)`, a.AccountID, nextGeneration, "reset:"+key, now); err != nil {
		return ResetResponse{}, err
	}
	if _, err := tx.ExecContext(ctx, `UPDATE paper_accounts SET generation=$2,resets_used_today=resets_used_today+1,reset_day=$3,updated_at=$4 WHERE id=$1 AND generation=$5`, a.AccountID, nextGeneration, utcDay(now), now, a.Generation); err != nil {
		return ResetResponse{}, err
	}
	out := ResetResponse{AccountID: a.AccountID, Mode: "paper", Generation: nextGeneration, ResetAt: now, Balances: []PaperBalance{{Asset: "USDT", Free: "10000.00000000", Locked: "0", Total: "10000.00000000", ValuationUSD: dptr(decimal.NewFromInt(10000)), PriceUSD: dptr(decimal.NewFromInt(1))}}}
	raw, _ := json.Marshal(out)
	if _, err := tx.ExecContext(ctx, `INSERT INTO paper_reset_idempotency (user_id,idempotency_key,request_hash,response_json,created_at) VALUES ($1,$2,$3,$4,$5) ON CONFLICT (user_id,idempotency_key) DO UPDATE SET request_hash=EXCLUDED.request_hash,response_json=EXCLUDED.response_json,created_at=EXCLUDED.created_at WHERE paper_reset_idempotency.created_at<=now()-interval '24 hours'`, userID, key, hash, raw, now); err != nil {
		return ResetResponse{}, err
	}
	return out, tx.Commit()
}

func (s *Store) Balances(ctx context.Context, userID string, page, pageSize int) (map[string]any, error) {
	tx, err := s.db.BeginTx(ctx, &sql.TxOptions{ReadOnly: true})
	if err != nil {
		return nil, err
	}
	defer tx.Rollback()
	var accountID string
	var generation int64
	if err := tx.QueryRowContext(ctx, `SELECT id,generation FROM paper_accounts WHERE user_id=$1`, userID).Scan(&accountID, &generation); err == sql.ErrNoRows {
		return nil, apiErr(http.StatusBadRequest, "PAPER_ACCOUNT_REQUIRED", "请先初始化模拟账户")
	} else if err != nil {
		return nil, err
	}
	var total int
	if err := tx.QueryRowContext(ctx, `SELECT count(*) FROM paper_balances WHERE account_id=$1 AND generation=$2`, accountID, generation).Scan(&total); err != nil {
		return nil, err
	}
	rows, err := tx.QueryContext(ctx, `SELECT asset,free::text FROM paper_balances WHERE account_id=$1 AND generation=$2 ORDER BY asset LIMIT $3 OFFSET $4`, accountID, generation, pageSize, (page-1)*pageSize)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	items := []PaperBalance{}
	for rows.Next() {
		var b PaperBalance
		var free string
		if err := rows.Scan(&b.Asset, &free); err != nil {
			return nil, err
		}
		d := decimal.RequireFromString(free)
		b.Free, b.Locked, b.Total = money(d, 8), "0", money(d, 8)
		if b.Asset == "USDT" {
			b.PriceUSD, b.ValuationUSD = dptr(decimal.NewFromInt(1)), dptr(d.Round(2))
		}
		items = append(items, b)
	}
	if err := rows.Err(); err != nil {
		return nil, err
	}
	return map[string]any{"items": items, "account": "paper", "mode": "paper", "generation": generation, "page": page, "pageSize": pageSize, "total": total}, tx.Commit()
}

var validClientID = regexp.MustCompile(`^[A-Za-z0-9_.:-]{1,64}$`)

func asciiKey(s string) bool {
	for _, r := range s {
		if r < 0x21 || r > 0x7e {
			return false
		}
	}
	return true
}
func positiveDecimal(v string) (decimal.Decimal, bool) {
	d, err := decimal.NewFromString(v)
	return d, err == nil && d.IsPositive()
}

type PlacePaperOrder struct {
	Symbol        string `json:"symbol"`
	Side          string `json:"side"`
	Type          string `json:"type"`
	Quantity      string `json:"quantity"`
	QuoteOrderQty string `json:"quoteOrderQty"`
	ClientOrderID string `json:"clientOrderId"`
}

func (s *Store) PlaceOrder(ctx context.Context, userID string, in PlacePaperOrder, market Market) (PaperOrder, error) {
	in.Symbol, in.Side, in.Type = strings.ToUpper(strings.TrimSpace(in.Symbol)), strings.ToLower(in.Side), strings.ToLower(in.Type)
	if in.Type != "market" {
		return PaperOrder{}, apiErr(http.StatusBadRequest, "ORDER_TYPE_UNSUPPORTED", "P0 仅支持 market")
	}
	if in.Side != "buy" && in.Side != "sell" {
		return PaperOrder{}, apiErr(http.StatusBadRequest, "VALIDATION_ERROR", "side 仅支持 buy/sell")
	}
	if !validClientID.MatchString(in.ClientOrderID) {
		return PaperOrder{}, apiErr(http.StatusBadRequest, "VALIDATION_ERROR", "clientOrderId 必须为 1-64 位 ASCII 标识")
	}
	hash := requestHash(in)
	tx, err := s.db.BeginTx(ctx, nil)
	if err != nil {
		return PaperOrder{}, err
	}
	defer tx.Rollback()
	a, err := ensureAccountTx(ctx, tx, userID, time.Now())
	if err != nil {
		return PaperOrder{}, err
	}
	var oldBody []byte
	var oldHash string
	err = tx.QueryRowContext(ctx, `SELECT request_hash,response_json FROM paper_orders WHERE user_id=$1 AND client_order_id=$2`, userID, in.ClientOrderID).Scan(&oldHash, &oldBody)
	if err == nil {
		if oldHash != hash {
			return PaperOrder{}, apiErr(http.StatusConflict, "IDEMPOTENCY_CONFLICT", "相同 clientOrderId 的请求内容不同")
		}
		var out PaperOrder
		if json.Unmarshal(oldBody, &out) != nil {
			return PaperOrder{}, fmt.Errorf("stored order decode")
		}
		return out, nil
	}
	if err != sql.ErrNoRows {
		return PaperOrder{}, err
	}
	symbol, err := market.TradingSymbol(ctx, in.Symbol)
	if err != nil {
		return PaperOrder{}, err
	}
	q, err := market.Index(ctx, in.Symbol)
	if err != nil {
		return PaperOrder{}, err
	}
	px := q.Price.Round(int32(symbol.PricePrecision))
	if !px.IsPositive() || !px.Equal(q.Price) {
		return PaperOrder{}, apiErr(http.StatusBadRequest, "INVALID_PRICE", "指数价格超出交易对精度")
	}
	var quantity, quote decimal.Decimal
	if in.Quantity != "" && in.QuoteOrderQty != "" {
		return PaperOrder{}, apiErr(http.StatusBadRequest, "INVALID_QTY", "quantity 与 quoteOrderQty 二选一")
	}
	if in.Side == "sell" && in.QuoteOrderQty != "" {
		return PaperOrder{}, apiErr(http.StatusBadRequest, "INVALID_QTY", "market sell 仅支持 quantity")
	}
	if (in.Quantity == "") == (in.QuoteOrderQty == "") {
		return PaperOrder{}, apiErr(http.StatusBadRequest, "INVALID_QTY", "quantity 或 quoteOrderQty 必填其一")
	}
	if in.Quantity != "" {
		var ok bool
		quantity, ok = positiveDecimal(in.Quantity)
		if !ok || !quantity.Equal(quantity.Round(int32(symbol.QuantityPrecision))) {
			return PaperOrder{}, apiErr(http.StatusBadRequest, "INVALID_QTY", "quantity 必须为正数且符合精度")
		}
		quote = quantity.Mul(px).Round(int32(symbol.PricePrecision))
	} else {
		var ok bool
		quote, ok = positiveDecimal(in.QuoteOrderQty)
		if !ok || !quote.Equal(quote.Round(int32(symbol.PricePrecision))) {
			return PaperOrder{}, apiErr(http.StatusBadRequest, "INVALID_QTY", "quoteOrderQty 必须为正数且符合价格精度")
		}
		quantity = quote.Div(px).Round(int32(symbol.QuantityPrecision))
		quote = quantity.Mul(px).Round(int32(symbol.PricePrecision))
		if !quantity.IsPositive() || quote.GreaterThan(decimal.RequireFromString(in.QuoteOrderQty)) {
			return PaperOrder{}, apiErr(http.StatusBadRequest, "INVALID_QTY", "下单金额不足以按数量精度全成")
		}
	}
	if quantity.LessThan(symbol.MinQuantity) {
		return PaperOrder{}, apiErr(http.StatusBadRequest, "INVALID_QTY", "低于交易对最小下单数量")
	}
	if !quote.Equal(quantity.Mul(px).Round(int32(symbol.PricePrecision))) || quote.LessThan(symbol.MinNotional) {
		return PaperOrder{}, apiErr(http.StatusBadRequest, "INVALID_QTY", "低于交易对最小名义额")
	}
	orderID, tradeID, now := randomID("po"), randomID("pt"), time.Now().UTC()
	debit, credit := symbol.Quote, symbol.Base
	debitQty, creditQty := quote, quantity
	if in.Side == "sell" {
		debit, credit, debitQty, creditQty = symbol.Base, symbol.Quote, quantity, quote
	}
	debitRef, creditRef := orderID+":debit", orderID+":credit"
	if err := changePaperBalance(ctx, tx, a.AccountID, a.Generation, debit, debitQty.Neg(), debitRef, now); err != nil {
		return PaperOrder{}, err
	}
	if err := changePaperBalance(ctx, tx, a.AccountID, a.Generation, credit, creditQty, creditRef, now); err != nil {
		return PaperOrder{}, err
	}
	quoteText := money(quote, symbol.PricePrecision)
	o := PaperOrder{OrderID: orderID, ClientOrderID: in.ClientOrderID, Mode: "paper", AccountID: a.AccountID, Generation: a.Generation, Symbol: in.Symbol, Side: in.Side, Type: "market", Price: noPrice, Quantity: money(quantity, symbol.QuantityPrecision), QuoteOrderQty: &quoteText, FilledQuantity: money(quantity, symbol.QuantityPrecision), Status: "filled", PriceSource: "INDEX", ReferencePrice: money(px, symbol.PricePrecision), MarketObservedAt: q.ObservedAt.UTC(), Fee: "0", FeeAsset: symbol.Quote, CreatedAt: now, ExecutedAt: now}
	rawResponse, err := json.Marshal(o)
	if err != nil {
		return PaperOrder{}, err
	}
	_, err = tx.ExecContext(ctx, `INSERT INTO paper_orders (order_id,account_id,user_id,generation,client_order_id,request_hash,response_json,symbol,side,order_type,quantity,quote_order_qty,filled_quantity,status,reference_price,market_observed_at,fee,fee_asset,created_at,executed_at) VALUES ($1,$2,$3,$4,$5,$6,$7,$8,$9,'market',$10,$11,$12,'filled',$13,$14,0,$15,$16,$16)`, o.OrderID, a.AccountID, userID, a.Generation, in.ClientOrderID, hash, rawResponse, in.Symbol, in.Side, quantity.String(), quote.String(), quantity.String(), px.String(), q.ObservedAt, symbol.Quote, now)
	if err != nil {
		return PaperOrder{}, err
	}
	if _, err := tx.ExecContext(ctx, `INSERT INTO paper_trades (trade_id,order_id,account_id,user_id,generation,symbol,side,price,quantity,fee_asset,market_observed_at,created_at) VALUES ($1,$2,$3,$4,$5,$6,$7,$8,$9,$10,$11,$12)`, tradeID, orderID, a.AccountID, userID, a.Generation, in.Symbol, in.Side, px.String(), quantity.String(), symbol.Quote, q.ObservedAt, now); err != nil {
		return PaperOrder{}, err
	}
	return o, tx.Commit()
}

func changePaperBalance(ctx context.Context, tx *sql.Tx, accountID string, generation int64, asset string, delta decimal.Decimal, ref string, now time.Time) error {
	var old string
	err := tx.QueryRowContext(ctx, `SELECT free::text FROM paper_balances WHERE account_id=$1 AND generation=$2 AND asset=$3 FOR UPDATE`, accountID, generation, asset).Scan(&old)
	current := decimal.Zero
	if err == nil {
		current = decimal.RequireFromString(old)
	} else if err != sql.ErrNoRows {
		return err
	}
	next := current.Add(delta)
	if next.IsNegative() {
		return apiErr(http.StatusBadRequest, "INSUFFICIENT_PAPER_BALANCE", "模拟盘余额不足")
	}
	if _, err := tx.ExecContext(ctx, `INSERT INTO paper_balances (account_id,generation,asset,free) VALUES ($1,$2,$3,$4) ON CONFLICT (account_id,generation,asset) DO UPDATE SET free=EXCLUDED.free`, accountID, generation, asset, next.String()); err != nil {
		return err
	}
	if _, err := tx.ExecContext(ctx, `INSERT INTO paper_ledger_entries (account_id,generation,asset,delta,balance_after,business_ref,created_at) VALUES ($1,$2,$3,$4,$5,$6,$7)`, accountID, generation, asset, delta.String(), next.String(), ref, now); err != nil {
		return err
	}
	return nil
}

// Database rows deliberately retain their requested precision. Generated views
// preserve immutable execution precision rather than re-rounding market prices.
func scanOrder(scanner interface{ Scan(...any) error }) (PaperOrder, error) {
	var o PaperOrder
	var quantity, filled, reference, fee string
	var quote sql.NullString
	err := scanner.Scan(&o.OrderID, &o.ClientOrderID, &o.AccountID, &o.Generation, &o.Symbol, &o.Side, &o.Type, &quantity, &quote, &filled, &o.Status, &reference, &o.MarketObservedAt, &fee, &o.FeeAsset, &o.CreatedAt, &o.ExecutedAt)
	if err != nil {
		return PaperOrder{}, err
	}
	o.Mode, o.Price, o.Quantity, o.FilledQuantity, o.Status = "paper", nil, quantity, filled, "filled"
	if quote.Valid {
		o.QuoteOrderQty = &quote.String
	}
	o.PriceSource, o.ReferencePrice, o.Fee = "INDEX", reference, fee
	return o, nil
}

const orderColumns = `order_id,client_order_id,account_id,generation,symbol,side,order_type,quantity::text,quote_order_qty::text,filled_quantity::text,status,reference_price::text,market_observed_at,fee::text,fee_asset,created_at,executed_at`

func (s *Store) Order(ctx context.Context, userID, orderID string) (PaperOrder, error) {
	o, err := scanOrder(s.db.QueryRowContext(ctx, `SELECT `+orderColumns+` FROM paper_orders WHERE user_id=$1 AND order_id=$2`, userID, orderID))
	if err == sql.ErrNoRows {
		return PaperOrder{}, apiErr(http.StatusNotFound, "PAPER_ORDER_NOT_FOUND", "模拟订单不存在")
	}
	if err != nil {
		return PaperOrder{}, err
	}
	o.Trades, err = s.orderTrades(ctx, userID, orderID)
	return o, err
}
func (s *Store) orderTrades(ctx context.Context, userID, orderID string) ([]PaperTrade, error) {
	rows, err := s.db.QueryContext(ctx, `SELECT trade_id,order_id,symbol,side,price::text,quantity::text,fee::text,fee_asset,market_observed_at,created_at FROM paper_trades WHERE user_id=$1 AND order_id=$2 ORDER BY created_at,trade_id`, userID, orderID)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	out := []PaperTrade{}
	for rows.Next() {
		var t PaperTrade
		if err := rows.Scan(&t.TradeID, &t.OrderID, &t.Symbol, &t.Side, &t.Price, &t.Quantity, &t.Fee, &t.FeeAsset, &t.MarketObservedAt, &t.CreatedAt); err != nil {
			return nil, err
		}
		t.Mode, t.Role, t.PriceSource = "paper", "taker", "INDEX"
		out = append(out, t)
	}
	return out, rows.Err()
}

func resetReplayTx(ctx context.Context, tx *sql.Tx, userID, key, hash string) (ResetResponse, bool, error) {
	var oldHash string
	var raw []byte
	err := tx.QueryRowContext(ctx, `SELECT request_hash,response_json FROM paper_reset_idempotency WHERE user_id=$1 AND idempotency_key=$2 AND created_at>now()-interval '24 hours'`, userID, key).Scan(&oldHash, &raw)
	if err == sql.ErrNoRows {
		return ResetResponse{}, false, nil
	}
	if err != nil {
		return ResetResponse{}, false, err
	}
	if oldHash != hash {
		return ResetResponse{}, false, apiErr(http.StatusConflict, "IDEMPOTENCY_CONFLICT", "相同 Idempotency-Key 的请求内容不同")
	}
	var out ResetResponse
	if err = json.Unmarshal(raw, &out); err != nil {
		return ResetResponse{}, false, fmt.Errorf("reset response decode")
	}
	return out, true, nil
}
