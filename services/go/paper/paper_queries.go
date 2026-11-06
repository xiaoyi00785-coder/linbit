package main

import (
	"context"
	"net/http"
	"strconv"
	"strings"
)

func parsePage(rawPage, rawSize string) (int, int, error) {
	page, pageSize := 1, 20
	var err error
	if rawPage != "" {
		page, err = strconv.Atoi(rawPage)
		if err != nil || page < 1 {
			return 0, 0, apiErr(http.StatusBadRequest, "VALIDATION_ERROR", "page 必须从 1 开始")
		}
	}
	if rawSize != "" {
		pageSize, err = strconv.Atoi(rawSize)
		if err != nil || pageSize < 1 || pageSize > 100 {
			return 0, 0, apiErr(http.StatusBadRequest, "VALIDATION_ERROR", "pageSize 范围 1-100")
		}
	}
	return page, pageSize, nil
}
func optionalGeneration(raw string) (*int64, error) {
	if raw == "" {
		return nil, nil
	}
	n, err := strconv.ParseInt(raw, 10, 64)
	if err != nil || n < 1 {
		return nil, apiErr(http.StatusBadRequest, "VALIDATION_ERROR", "generation 必须为正整数")
	}
	return &n, nil
}
func (s *Store) ListOrders(ctx context.Context, userID, symbol, status string, generation int64, page, pageSize int) (map[string]any, error) {
	if generation == 0 {
		if err := s.db.QueryRowContext(ctx, `SELECT generation FROM paper_accounts WHERE user_id=$1`, userID).Scan(&generation); err != nil {
			return nil, err
		}
	}
	if status != "" && status != "filled" {
		return map[string]any{"items": []PaperOrder{}, "generation": generation, "page": page, "pageSize": pageSize, "total": 0}, nil
	}
	args := []any{userID}
	where := `FROM paper_orders WHERE user_id=$1`
	if generation > 0 {
		args = append(args, generation)
		where += " AND generation=$" + strconv.Itoa(len(args))
	}
	if symbol != "" {
		args = append(args, strings.ToUpper(symbol))
		where += " AND symbol=$" + strconv.Itoa(len(args))
	}
	if status != "" {
		args = append(args, status)
		where += " AND status=$" + strconv.Itoa(len(args))
	}
	var total int
	if err := s.db.QueryRowContext(ctx, `SELECT count(*) `+where, args...).Scan(&total); err != nil {
		return nil, err
	}
	args = append(args, pageSize, (page-1)*pageSize)
	rows, err := s.db.QueryContext(ctx, `SELECT `+orderColumns+" "+where+` ORDER BY created_at DESC,order_id DESC LIMIT $`+strconv.Itoa(len(args)-1)+` OFFSET $`+strconv.Itoa(len(args)), args...)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	items := []PaperOrder{}
	for rows.Next() {
		o, err := scanOrder(rows)
		if err != nil {
			return nil, err
		}
		items = append(items, o)
	}
	if err := rows.Err(); err != nil {
		return nil, err
	}
	out := map[string]any{"items": items, "page": page, "pageSize": pageSize, "total": total}
	out["generation"] = generation
	return out, nil
}

type tradeFilter struct {
	userID         string
	symbol         string
	generation     int64
	page, pageSize int
}

func (s *Store) ListTrades(ctx context.Context, f tradeFilter) (map[string]any, error) {
	if f.generation == 0 {
		if err := s.db.QueryRowContext(ctx, `SELECT generation FROM paper_accounts WHERE user_id=$1`, f.userID).Scan(&f.generation); err != nil {
			return nil, err
		}
	}
	args := []any{f.userID}
	where := `FROM paper_trades WHERE user_id=$1`
	if f.generation > 0 {
		args = append(args, f.generation)
		where += " AND generation=$" + strconv.Itoa(len(args))
	}
	if f.symbol != "" {
		args = append(args, strings.ToUpper(f.symbol))
		where += " AND symbol=$" + strconv.Itoa(len(args))
	}
	var total int
	if err := s.db.QueryRowContext(ctx, `SELECT count(*) `+where, args...).Scan(&total); err != nil {
		return nil, err
	}
	args = append(args, f.pageSize, (f.page-1)*f.pageSize)
	rows, err := s.db.QueryContext(ctx, `SELECT trade_id,order_id,symbol,side,price::text,quantity::text,fee::text,fee_asset,market_observed_at,created_at `+where+` ORDER BY created_at DESC,trade_id DESC LIMIT $`+strconv.Itoa(len(args)-1)+` OFFSET $`+strconv.Itoa(len(args)), args...)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	items := []PaperTrade{}
	for rows.Next() {
		var t PaperTrade
		if err := rows.Scan(&t.TradeID, &t.OrderID, &t.Symbol, &t.Side, &t.Price, &t.Quantity, &t.Fee, &t.FeeAsset, &t.MarketObservedAt, &t.CreatedAt); err != nil {
			return nil, err
		}
		t.Mode, t.Role, t.PriceSource = "paper", "taker", "INDEX"
		items = append(items, t)
	}
	if err := rows.Err(); err != nil {
		return nil, err
	}
	out := map[string]any{"items": items, "page": f.page, "pageSize": f.pageSize, "total": total}
	if f.generation > 0 {
		out["generation"] = f.generation
	}
	// Fill the response generation for unfiltered lists using the most recent
	// returned execution, not a separate mutable account read.
	if f.generation == 0 && len(items) > 0 {
		var gen int64
		if err := s.db.QueryRowContext(ctx, `SELECT generation FROM paper_trades WHERE trade_id=$1`, items[0].TradeID).Scan(&gen); err == nil {
			out["generation"] = gen
		}
	}
	return out, nil
}
