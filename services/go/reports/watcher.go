package main

import (
	"context"
	"database/sql"
	"encoding/json"
	"log"
	"time"

	"github.com/linbit/platform/services/go/common"
	"github.com/shopspring/decimal"
)

const (
	depositsCursor    = "deposits_watermark"
	withdrawalsCursor = "withdrawals_watermark"
	tradesCursor      = "trades_watermark"
)

// RunDepositWatcher polls the shared deposits table for newly-completed
// deposits past the last watermark and, for each one:
//   - fans out a {type:"deposit",...} frame on the WebSocket activity stream
//   - matches the user's notification subscriptions and posts a Notice for each
//
// It also advances the withdrawals and trades watermarks so those events reach
// the WebSocket stream. Only completed rows are read: pending / confirming rows
// are skipped and picked up on the pass after they flip.
func (s *Server) RunDepositWatcher(ctx context.Context) {
	t := time.NewTicker(s.cfg.WatcherTick)
	defer t.Stop()
	for {
		s.depositTick(ctx)
		select {
		case <-ctx.Done():
			return
		case <-t.C:
		}
	}
}

func (s *Server) depositTick(ctx context.Context) {
	s.processDeposits(ctx)
	s.processWithdrawals(ctx)
	s.processTrades(ctx)
}

type depositEvent struct {
	id       int64
	uid      string
	asset    string
	network  string
	address  string
	amount   decimal.Decimal
	txHash   string
	creditAt time.Time
}

func (s *Server) processDeposits(ctx context.Context) {
	cur, err := s.store.Cursor(ctx, depositsCursor)
	if err != nil {
		log.Printf("reports: deposits cursor: %v", err)
		return
	}
	rows, err := s.db.QueryContext(ctx, `
		SELECT id, user_id, asset, network, address, amount, tx_hash, credited_at
		FROM deposits WHERE id > $1 AND status='completed' AND credited_at IS NOT NULL
		ORDER BY id ASC LIMIT 200`, cur)
	if err != nil {
		log.Printf("reports: deposits scan: %v", err)
		return
	}
	defer rows.Close()
	var events []depositEvent
	for rows.Next() {
		var e depositEvent
		var creditAt sql.NullTime
		if err := rows.Scan(&e.id, &e.uid, &e.asset, &e.network, &e.address, &e.amount, &e.txHash, &creditAt); err != nil {
			log.Printf("reports: deposits row: %v", err)
			return
		}
		if creditAt.Valid {
			e.creditAt = creditAt.Time
		}
		events = append(events, e)
	}
	if err := rows.Err(); err != nil {
		log.Printf("reports: deposits iter: %v", err)
		return
	}
	for _, e := range events {
		s.handleDeposit(ctx, e)
		if err := s.store.SetCursor(ctx, depositsCursor, e.id); err != nil {
			log.Printf("reports: deposits cursor set: %v", err)
			return
		}
	}
}

func (s *Server) handleDeposit(ctx context.Context, e depositEvent) {
	payload := map[string]any{
		"type":    "deposit",
		"asset":   e.asset,
		"network": e.network,
		"amount":  e.amount.String(),
		"txHash":  e.txHash,
		"address": e.address,
		"time":    e.creditAt.UnixMilli(),
	}
	raw, _ := json.Marshal(payload)
	s.hub.publish(e.uid, raw)

	subs, err := s.store.MatchDepositSubs(ctx, e.uid, e.asset, e.network, e.address, e.amount)
	if err != nil {
		log.Printf("reports: match subs: %v", err)
		return
	}
	for _, sub := range subs {
		title := "到账提醒"
		body := "您的 " + e.amount.String() + " " + e.asset + "（" + e.network + "）充值已到账。"
		email := false
		switch sub.Channel {
		case "email":
			email = true
		case "both":
			email = true
		}
		if email {
			t := true
			common.Notify(common.Notice{UID: e.uid, Kind: common.NoticeTrade, Title: title, Body: body, Email: &t})
		} else {
			f := false
			common.Notify(common.Notice{UID: e.uid, Kind: common.NoticeTrade, Title: title, Body: body, Email: &f})
		}
	}
}

func (s *Server) processWithdrawals(ctx context.Context) {
	cur, err := s.store.Cursor(ctx, withdrawalsCursor)
	if err != nil {
		return
	}
	rows, err := s.db.QueryContext(ctx, `
		SELECT id, user_id, asset, network, address, amount, status, updated_at
		FROM withdrawals WHERE id > $1 AND status IN ('completed','failed','canceled')
		ORDER BY id ASC LIMIT 200`, cur)
	if err != nil {
		log.Printf("reports: withdrawals scan: %v", err)
		return
	}
	defer rows.Close()
	type row struct {
		id       int64
		uid      string
		asset    string
		network  string
		address  string
		amount   decimal.Decimal
		status   string
		updatedAt time.Time
	}
	var items []row
	for rows.Next() {
		var r row
		if err := rows.Scan(&r.id, &r.uid, &r.asset, &r.network, &r.address, &r.amount, &r.status, &r.updatedAt); err != nil {
			return
		}
		items = append(items, r)
	}
	if err := rows.Err(); err != nil {
		return
	}
	for _, it := range items {
		payload := map[string]any{
			"type":    "withdrawal",
			"asset":   it.asset,
			"network": it.network,
			"address": it.address,
			"amount":  it.amount.String(),
			"status":  it.status,
			"time":    it.updatedAt.UnixMilli(),
		}
		raw, _ := json.Marshal(payload)
		s.hub.publish(it.uid, raw)
		if err := s.store.SetCursor(ctx, withdrawalsCursor, it.id); err != nil {
			return
		}
	}
}

func (s *Server) processTrades(ctx context.Context) {
	cur, err := s.store.Cursor(ctx, tradesCursor)
	if err != nil {
		return
	}
	rows, err := s.db.QueryContext(ctx, `
		SELECT id, user_id, symbol, side, price, quantity, created_at
		FROM trades WHERE id > $1 ORDER BY id ASC LIMIT 500`, cur)
	if err != nil {
		log.Printf("reports: trades scan: %v", err)
		return
	}
	defer rows.Close()
	type row struct {
		id    int64
		uid   string
		sym   string
		side  string
		price decimal.Decimal
		qty   decimal.Decimal
		at    time.Time
	}
	var items []row
	for rows.Next() {
		var r row
		if err := rows.Scan(&r.id, &r.uid, &r.sym, &r.side, &r.price, &r.qty, &r.at); err != nil {
			return
		}
		items = append(items, r)
	}
	if err := rows.Err(); err != nil {
		return
	}
	for _, it := range items {
		payload := map[string]any{
			"type":     "trade",
			"symbol":   it.sym,
			"side":     it.side,
			"price":    it.price.String(),
			"quantity": it.qty.String(),
			"time":     it.at.UnixMilli(),
		}
		raw, _ := json.Marshal(payload)
		s.hub.publish(it.uid, raw)
		if err := s.store.SetCursor(ctx, tradesCursor, it.id); err != nil {
			return
		}
	}
}
