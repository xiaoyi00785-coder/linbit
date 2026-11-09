package main

// 模拟合约 HTTP。账户、下单、强平都走 Store 的 paper_fut_*，FutSource 只读真实行情。

import (
	"bytes"
	"context"
	"encoding/json"
	"log"
	"net/http"
	"strconv"
	"strings"
	"time"

	"github.com/shopspring/decimal"
)

func (s *Server) futSource() FutSource {
	if s.futures != nil {
		return s.futures
	}
	return NewLiveFutures(s.cfg.FuturesURL)
}

func (s *Server) handleFutAccount(w http.ResponseWriter, r *http.Request) {
	uid, ok := s.feature(w, r, "paperTrading")
	if !ok {
		return
	}
	out, err := s.store.FutAccountView(r.Context(), uid, s.futSource())
	if err != nil {
		writeErr(w, err)
		return
	}
	commonJSON(w, http.StatusOK, out)
}

func (s *Server) handleFutClaim(w http.ResponseWriter, r *http.Request) {
	uid, ok := s.feature(w, r, "paperTrading")
	if !ok {
		return
	}
	var in struct {
		Amount string `json:"amount"`
	}
	if !decode(w, r, &in) {
		return
	}
	amt, err := decimal.NewFromString(strings.TrimSpace(in.Amount))
	if err != nil {
		writeErr(w, apiErr(400, "VALIDATION_ERROR", "amount 必须是十进制数字"))
		return
	}
	out, err := s.store.FutClaim(r.Context(), uid, amt)
	if err != nil {
		writeErr(w, err)
		return
	}
	s.pushFut(uid)
	commonJSON(w, http.StatusOK, out)
}

func (s *Server) handleFutReset(w http.ResponseWriter, r *http.Request) {
	uid, ok := s.feature(w, r, "paperTrading")
	if !ok {
		return
	}
	var in struct {
		Confirmation string `json:"confirmation"`
	}
	if !decode(w, r, &in) {
		return
	}
	if err := s.store.FutReset(r.Context(), uid, in.Confirmation); err != nil {
		writeErr(w, err)
		return
	}
	s.pushFut(uid)
	commonJSON(w, http.StatusOK, map[string]any{"reset": true, "asset": "USDX", "balance": paperFutInitial})
}

func (s *Server) handleFutContracts(w http.ResponseWriter, r *http.Request) {
	uid, ok := s.feature(w, r, "paperTrading")
	if !ok {
		return
	}
	_ = uid
	specs, err := s.futSource().Specs(r.Context())
	if err != nil {
		writeErr(w, apiErr(503, "MARKET_UNAVAILABLE", "无法读取真实合约规格"))
		return
	}
	commonJSON(w, http.StatusOK, map[string]any{"contracts": specs, "quoteAsset": "USDX", "copyTrading": false})
}

func (s *Server) handleFutPlace(w http.ResponseWriter, r *http.Request) {
	uid, ok := s.feature(w, r, "paperTrading")
	if !ok {
		return
	}
	var raw struct {
		Symbol, Side, PositionSide, Action, Type, SizeMode, MarginMode, ClientOrderID string
		Price, TriggerPrice, Quantity, Cost, Notional, TP, SL                         string
		Leverage                                                                      int
	}
	if !decode(w, r, &raw) {
		return
	}
	in := futOrderIn{
		Symbol: raw.Symbol, Side: raw.Side, PositionSide: raw.PositionSide, Action: raw.Action,
		Type: raw.Type, SizeMode: raw.SizeMode, MarginMode: raw.MarginMode, Leverage: raw.Leverage,
		ClientOrderID: raw.ClientOrderID,
	}
	var err error
	if in.Price, err = optDec(raw.Price); err != nil {
		writeErr(w, err)
		return
	}
	if in.Trigger, err = optDec(raw.TriggerPrice); err != nil {
		writeErr(w, err)
		return
	}
	if in.Quantity, err = optDec(raw.Quantity); err != nil {
		writeErr(w, err)
		return
	}
	if in.Cost, err = optDec(raw.Cost); err != nil {
		writeErr(w, err)
		return
	}
	if in.Notional, err = optDec(raw.Notional); err != nil {
		writeErr(w, err)
		return
	}
	if raw.TP != "" {
		in.HasTP = true
		if in.TP, err = optDec(raw.TP); err != nil {
			writeErr(w, err)
			return
		}
	}
	if raw.SL != "" {
		in.HasSL = true
		if in.SL, err = optDec(raw.SL); err != nil {
			writeErr(w, err)
			return
		}
	}
	out, err := s.store.PlaceFutOrder(r.Context(), uid, in, s.futSource())
	if err != nil {
		writeErr(w, err)
		return
	}
	s.pushFut(uid)
	commonJSON(w, http.StatusCreated, out)
}

func optDec(s string) (decimal.Decimal, error) {
	s = strings.TrimSpace(s)
	if s == "" {
		return decimal.Zero, nil
	}
	d, err := decimal.NewFromString(s)
	if err != nil {
		return decimal.Zero, apiErr(400, "VALIDATION_ERROR", "数字格式无效")
	}
	return d, nil
}

func (s *Server) handleFutOrders(w http.ResponseWriter, r *http.Request) {
	uid, ok := s.feature(w, r, "paperTrading")
	if !ok {
		return
	}
	status := r.URL.Query().Get("status")
	q := `SELECT order_id, symbol, side, position_side, action, order_type, COALESCE(price,0)::text, qty::text, filled_qty::text, status, created_at FROM paper_fut_orders WHERE user_id=$1`
	args := []any{uid}
	if status == "open" {
		q += ` AND status IN ('open','untriggered')`
	} else if status == "history" {
		q += ` AND status NOT IN ('open','untriggered')`
	}
	q += ` ORDER BY created_at DESC LIMIT 100`
	rows, err := s.store.db.QueryContext(r.Context(), q, args...)
	if err != nil {
		writeErr(w, err)
		return
	}
	defer rows.Close()
	items := []map[string]any{}
	for rows.Next() {
		var id, sym, side, ps, action, typ, price, qty, filled, st string
		var at time.Time
		if err := rows.Scan(&id, &sym, &side, &ps, &action, &typ, &price, &qty, &filled, &st, &at); err != nil {
			writeErr(w, err)
			return
		}
		items = append(items, map[string]any{"orderId": id, "symbol": sym, "side": side, "positionSide": ps, "action": action, "type": typ, "price": price, "quantity": qty, "filledQuantity": filled, "status": st, "createdAt": at})
	}
	commonJSON(w, http.StatusOK, map[string]any{"items": items})
}

func (s *Server) handleFutCancel(w http.ResponseWriter, r *http.Request) {
	uid, ok := s.feature(w, r, "paperTrading")
	if !ok {
		return
	}
	if err := s.store.CancelFutOrder(r.Context(), uid, r.PathValue("orderId")); err != nil {
		writeErr(w, err)
		return
	}
	s.pushFut(uid)
	commonJSON(w, http.StatusOK, map[string]any{"canceled": true})
}

func (s *Server) handleFutCancelAll(w http.ResponseWriter, r *http.Request) {
	uid, ok := s.feature(w, r, "paperTrading")
	if !ok {
		return
	}
	n, err := s.store.CancelAllFut(r.Context(), uid)
	if err != nil {
		writeErr(w, err)
		return
	}
	s.pushFut(uid)
	commonJSON(w, http.StatusOK, map[string]any{"canceled": n})
}

func (s *Server) handleFutPositions(w http.ResponseWriter, r *http.Request) {
	uid, ok := s.feature(w, r, "paperTrading")
	if !ok {
		return
	}
	rows, err := s.store.db.QueryContext(r.Context(), `SELECT symbol, position_side, margin_mode, leverage, qty::text, entry_price::text, isolated_margin::text, realized_pnl::text, tp_trigger::text, sl_trigger::text FROM paper_fut_positions WHERE user_id=$1 AND qty <> 0`, uid)
	if err != nil {
		writeErr(w, err)
		return
	}
	defer rows.Close()
	items := []map[string]any{}
	src := s.futSource()
	for rows.Next() {
		var sym, ps, mode, qtyS, entryS, isoS, pnlS string
		var tp, sl sqlNull
		var lev int
		if err := rows.Scan(&sym, &ps, &mode, &lev, &qtyS, &entryS, &isoS, &pnlS, &tp, &sl); err != nil {
			writeErr(w, err)
			return
		}
		qty, _ := decimal.NewFromString(qtyS)
		entry, _ := decimal.NewFromString(entryS)
		iso, _ := decimal.NewFromString(isoS)
		item := map[string]any{"symbol": sym, "positionSide": ps, "marginMode": mode, "leverage": lev, "quantity": qtyS, "entryPrice": entryS, "margin": isoS, "realizedPnl": pnlS, "tpTrigger": tp.String, "slTrigger": sl.String}
		live := strings.TrimSuffix(sym, "USDX") + "USDT"
		mark, _, _, okq, _ := src.Quote(r.Context(), live)
		if okq && mark.IsPositive() {
			upnl := mark.Sub(entry).Mul(qty)
			item["markPrice"] = mark.String()
			item["unrealizedPnl"] = upnl.StringFixed(8)
			mmr := decimal.RequireFromString("0.004")
			specs, _ := src.Specs(r.Context())
			for _, sp := range specs {
				if sp.Symbol == sym && sp.MMR.IsPositive() {
					mmr = sp.MMR
				}
			}
			cushion := iso
			if mode == "cross" {
				var wallet decimal.Decimal
				_ = s.store.db.QueryRowContext(r.Context(), `SELECT balance FROM paper_fut_accounts WHERE user_id=$1`, uid).Scan(&wallet)
				cushion = wallet
			}
			item["liquidationPrice"] = liqPrice(entry, cushion, qty, mmr).StringFixed(8)
			margin := iso
			if margin.IsZero() {
				margin = entry.Mul(qty.Abs()).Div(decimal.NewFromInt(int64(lev)))
			}
			if margin.IsPositive() {
				item["roe"] = upnl.Div(margin).StringFixed(6)
			}
		} else {
			item["markPrice"] = nil
			item["liquidationPrice"] = nil
			item["priceAvailable"] = false
		}
		items = append(items, item)
	}
	commonJSON(w, http.StatusOK, map[string]any{"items": items})
}

type sqlNull struct{ String string }

// Scan 把 NULL 和无法识别的驱动类型收成空串，不把扫描失败往上抛。
func (n *sqlNull) Scan(v any) error {
	if v == nil {
		n.String = ""
		return nil
	}
	switch t := v.(type) {
	case string:
		n.String = t
	case []byte:
		n.String = string(t)
	default:
		n.String = ""
	}
	return nil
}

func (s *Server) handleFutClose(w http.ResponseWriter, r *http.Request) {
	uid, ok := s.feature(w, r, "paperTrading")
	if !ok {
		return
	}
	ps := r.URL.Query().Get("positionSide")
	if ps == "" {
		ps = "BOTH"
	}
	if err := s.store.ClosePosition(r.Context(), uid, r.PathValue("symbol"), ps, s.futSource()); err != nil {
		writeErr(w, err)
		return
	}
	s.pushFut(uid)
	commonJSON(w, http.StatusOK, map[string]any{"closed": true})
}

func (s *Server) handleFutCloseAll(w http.ResponseWriter, r *http.Request) {
	uid, ok := s.feature(w, r, "paperTrading")
	if !ok {
		return
	}
	n, err := s.store.CloseAll(r.Context(), uid, s.futSource())
	if err != nil {
		writeErr(w, err)
		return
	}
	s.pushFut(uid)
	commonJSON(w, http.StatusOK, map[string]any{"closed": n})
}

func (s *Server) handleFutTPSL(w http.ResponseWriter, r *http.Request) {
	uid, ok := s.feature(w, r, "paperTrading")
	if !ok {
		return
	}
	var raw struct {
		PositionSide, TP, SL string
	}
	if !decode(w, r, &raw) {
		return
	}
	ps := raw.PositionSide
	if ps == "" {
		ps = "BOTH"
	}
	tp, err := optDec(raw.TP)
	if err != nil {
		writeErr(w, err)
		return
	}
	sl, err := optDec(raw.SL)
	if err != nil {
		writeErr(w, err)
		return
	}
	if err := s.store.SetTPSL(r.Context(), uid, r.PathValue("symbol"), ps, tp, sl, raw.TP != "", raw.SL != ""); err != nil {
		writeErr(w, err)
		return
	}
	s.pushFut(uid)
	commonJSON(w, http.StatusOK, map[string]any{"updated": true})
}

func (s *Server) handleFutLeverage(w http.ResponseWriter, r *http.Request) {
	uid, ok := s.feature(w, r, "paperTrading")
	if !ok {
		return
	}
	var in struct {
		Leverage int `json:"leverage"`
	}
	if !decode(w, r, &in) {
		return
	}
	if err := s.store.SetFutLeverage(r.Context(), uid, r.PathValue("symbol"), in.Leverage, s.futSource()); err != nil {
		writeErr(w, err)
		return
	}
	commonJSON(w, http.StatusOK, map[string]any{"leverage": in.Leverage})
}

func (s *Server) handleFutMarginMode(w http.ResponseWriter, r *http.Request) {
	uid, ok := s.feature(w, r, "paperTrading")
	if !ok {
		return
	}
	var in struct {
		MarginMode string `json:"marginMode"`
	}
	if !decode(w, r, &in) {
		return
	}
	if err := s.store.SetMarginMode(r.Context(), uid, r.PathValue("symbol"), in.MarginMode); err != nil {
		writeErr(w, err)
		return
	}
	commonJSON(w, http.StatusOK, map[string]any{"marginMode": in.MarginMode})
}

func (s *Server) handleFutPositionMode(w http.ResponseWriter, r *http.Request) {
	uid, ok := s.feature(w, r, "paperTrading")
	if !ok {
		return
	}
	var in struct {
		PositionMode string `json:"positionMode"`
	}
	if !decode(w, r, &in) {
		return
	}
	if err := s.store.SetPositionMode(r.Context(), uid, in.PositionMode); err != nil {
		writeErr(w, err)
		return
	}
	commonJSON(w, http.StatusOK, map[string]any{"positionMode": in.PositionMode})
}

func (s *Server) handleFutTrades(w http.ResponseWriter, r *http.Request) {
	uid, ok := s.feature(w, r, "paperTrading")
	if !ok {
		return
	}
	rows, err := s.store.db.QueryContext(r.Context(), `SELECT trade_id, order_id, symbol, side, price::text, qty::text, realized_pnl::text, created_at FROM paper_fut_trades WHERE user_id=$1 ORDER BY created_at DESC LIMIT 100`, uid)
	if err != nil {
		writeErr(w, err)
		return
	}
	defer rows.Close()
	items := []map[string]any{}
	for rows.Next() {
		var id, oid, sym, side, px, qty, pnl string
		var at time.Time
		if err := rows.Scan(&id, &oid, &sym, &side, &px, &qty, &pnl, &at); err != nil {
			writeErr(w, err)
			return
		}
		items = append(items, map[string]any{"tradeId": id, "orderId": oid, "symbol": sym, "side": side, "price": px, "quantity": qty, "realizedPnl": pnl, "createdAt": at})
	}
	commonJSON(w, http.StatusOK, map[string]any{"items": items})
}

func (s *Server) handleFutLedger(w http.ResponseWriter, r *http.Request) {
	uid, ok := s.feature(w, r, "paperTrading")
	if !ok {
		return
	}
	rows, err := s.store.db.QueryContext(r.Context(), `SELECT id, kind, amount::text, balance_after::text, ref, created_at FROM paper_fut_ledger WHERE user_id=$1 ORDER BY id DESC LIMIT 100`, uid)
	if err != nil {
		writeErr(w, err)
		return
	}
	defer rows.Close()
	items := []map[string]any{}
	for rows.Next() {
		var id int64
		var kind, amt, after, ref string
		var at time.Time
		if err := rows.Scan(&id, &kind, &amt, &after, &ref, &at); err != nil {
			writeErr(w, err)
			return
		}
		items = append(items, map[string]any{"id": strconv.FormatInt(id, 10), "kind": kind, "amount": amt, "balanceAfter": after, "ref": ref, "createdAt": at})
	}
	commonJSON(w, http.StatusOK, map[string]any{"items": items, "asset": "USDX"})
}

func (s *Server) handleFutSnapshot(w http.ResponseWriter, r *http.Request) {
	if !strings.EqualFold(r.Header.Get("X-Internal-Token"), s.cfg.InternalToken) || s.cfg.InternalToken == "" {
		writeErr(w, apiErr(401, "UNAUTHORIZED", "内部调用未授权"))
		return
	}
	uid := r.URL.Query().Get("userId")
	if uid == "" {
		writeErr(w, apiErr(400, "VALIDATION_ERROR", "userId 必填"))
		return
	}
	view, err := s.store.FutAccountView(r.Context(), uid, s.futSource())
	if err != nil {
		writeErr(w, err)
		return
	}
	commonJSON(w, http.StatusOK, map[string]any{"account": view, "channelHint": []string{"paper:positions", "paper:orders", "paper:balances"}})
}

func (s *Server) pushFut(uid string) {
	if s.cfg.MarketURL == "" || s.cfg.InternalToken == "" {
		return
	}
	ctx, cancel := context.WithTimeout(context.Background(), 2*time.Second)
	defer cancel()
	acct, err := s.store.FutAccountView(ctx, uid, s.futSource())
	if err != nil {
		log.Printf("paper futures snapshot: %v", err)
		return
	}
	positions, orders := s.store.futPushRows(ctx, uid)
	s.pushChannel(ctx, uid, "paper:balances", acct)
	s.pushChannel(ctx, uid, "paper:positions", map[string]any{"items": positions})
	s.pushChannel(ctx, uid, "paper:orders", map[string]any{"items": orders})
}

func (s *Server) pushChannel(ctx context.Context, uid, channel string, data any) {
	body, err := json.Marshal(map[string]any{"userId": uid, "channel": channel, "data": data})
	if err != nil {
		return
	}
	req, err := http.NewRequestWithContext(ctx, http.MethodPost, strings.TrimRight(s.cfg.MarketURL, "/")+"/internal/ws/push", bytes.NewReader(body))
	if err != nil {
		return
	}
	req.Header.Set("Content-Type", "application/json")
	req.Header.Set("X-Internal-Token", s.cfg.InternalToken)
	resp, err := http.DefaultClient.Do(req)
	if err != nil {
		log.Printf("paper futures push %s: %v", channel, err)
		return
	}
	resp.Body.Close()
}

func commonJSON(w http.ResponseWriter, status int, v any) {
	commonWrite(w, status, v)
}

func commonWrite(w http.ResponseWriter, status int, v any) {
	// local alias so this file does not depend on a renamed helper
	b, err := json.Marshal(v)
	if err != nil {
		http.Error(w, "encode", 500)
		return
	}
	w.Header().Set("Content-Type", "application/json")
	w.WriteHeader(status)
	_, _ = w.Write(b)
}
