package main

import (
	"context"
	"database/sql"
	"errors"
	"fmt"
	"time"

	"github.com/shopspring/decimal"
)

var errNoRows = sql.ErrNoRows

// ---- engine_cursors ----

// EnsureCursor inserts a cursor row for symbol if one doesn't exist yet.
func (s *Store) EnsureCursor(ctx context.Context, symbol string) error {
	_, err := s.db.ExecContext(ctx,
		`INSERT INTO engine_cursors (symbol, last_seq) VALUES ($1, 0) ON CONFLICT (symbol) DO NOTHING`,
		symbol)
	return err
}

// GetCursor returns the last consumed seq for symbol.
func (s *Store) GetCursor(ctx context.Context, symbol string) (int64, error) {
	var seq int64
	err := s.db.QueryRowContext(ctx,
		`SELECT last_seq FROM engine_cursors WHERE symbol = $1`, symbol).Scan(&seq)
	return seq, err
}

// ClaimCursor takes the symbol's consume lease with SELECT ... FOR UPDATE.
// ok is false when another replica's lease is still in the future. The caller
// must not settle that symbol until it wins a later claim. ttl is how long
// this owner keeps the lease; renew it by claiming again or by AdvanceCursor.
func (s *Store) ClaimCursor(ctx context.Context, symbol, owner string, ttl time.Duration) (seq int64, ok bool, err error) {
	if owner == "" {
		return 0, false, fmt.Errorf("cursor lease owner is empty")
	}
	secs := leaseSeconds(ttl)
	tx, err := s.db.BeginTx(ctx, nil)
	if err != nil {
		return 0, false, err
	}
	defer tx.Rollback()
	var curOwner sql.NullString
	var until sql.NullTime
	err = tx.QueryRowContext(ctx, `
		SELECT last_seq, lease_owner, lease_until
		FROM engine_cursors WHERE symbol = $1 FOR UPDATE`, symbol).Scan(&seq, &curOwner, &until)
	if err != nil {
		return 0, false, err
	}
	heldByOther := curOwner.Valid && curOwner.String != "" && curOwner.String != owner &&
		until.Valid && until.Time.After(time.Now())
	if heldByOther {
		return seq, false, nil
	}
	if _, err = tx.ExecContext(ctx, `
		UPDATE engine_cursors
		SET lease_owner = $2, lease_until = now() + make_interval(secs => $3), updated_at = now()
		WHERE symbol = $1`, symbol, owner, secs); err != nil {
		return 0, false, err
	}
	if err = tx.Commit(); err != nil {
		return 0, false, err
	}
	return seq, true, nil
}

// AdvanceCursor advances the cursor when this owner still holds the row and
// seq is ahead. The lease may already have lapsed: expiry only lets another
// replica claim, and a slow settle must still be able to record the cursor it
// owns. A lost lease (owner changed) returns an error so the caller does not
// ack the engine past a cursor it no longer owns.
func (s *Store) AdvanceCursor(ctx context.Context, symbol string, seq int64, owner string, ttl time.Duration) error {
	res, err := s.db.ExecContext(ctx, `
		UPDATE engine_cursors
		SET last_seq = $2,
		    lease_until = now() + make_interval(secs => $4),
		    updated_at = now()
		WHERE symbol = $1 AND last_seq < $2 AND lease_owner = $3`,
		symbol, seq, owner, leaseSeconds(ttl))
	if err != nil {
		return err
	}
	n, _ := res.RowsAffected()
	if n == 0 {
		return fmt.Errorf("cursor %s not advanced (lost lease or seq already applied)", symbol)
	}
	return nil
}

func leaseSeconds(ttl time.Duration) int64 {
	secs := int64(ttl / time.Second)
	if secs < 1 {
		return 1
	}
	return secs
}

// ---- orders: stream columns ----

// MarkEngineSeen sets engine_seen = true for the order. Idempotent.
func (s *Store) MarkEngineSeen(ctx context.Context, orderID string) error {
	_, err := s.db.ExecContext(ctx,
		`UPDATE orders SET engine_seen = true, updated_at = now() WHERE order_id = $1 AND NOT engine_seen`,
		orderID)
	return err
}

// RankUpdateFill is an idempotent fill update: it only advances status (never
// moves backwards in the terminal direction) and sets filled_quantity.
// Status rank: new < partially_filled < filled = canceled = rejected.
func (s *Store) RankUpdateFill(ctx context.Context, orderID string, filled decimal.Decimal, status string) error {
	_, err := s.db.ExecContext(ctx, `
		UPDATE orders
		SET    filled_quantity = $2,
		       status          = $3,
		       updated_at      = now()
		WHERE  order_id = $1
		  AND  CASE status
		           WHEN 'new'              THEN 0
		           WHEN 'partially_filled' THEN 1
		           ELSE                        2
		       END
		   <= CASE $3::text
		           WHEN 'new'              THEN 0
		           WHEN 'partially_filled' THEN 1
		           ELSE                        2
		       END`,
		orderID, filled.String(), status)
	return err
}

// ClaimRelease atomically flips release_state from 'none' to 'pending',
// returning true if this call won. The release worker completes the unfreeze
// and sets it to 'done'.
func (s *Store) ClaimRelease(ctx context.Context, orderID string) (bool, error) {
	res, err := s.db.ExecContext(ctx,
		`UPDATE orders SET release_state = 'pending', updated_at = now()
		 WHERE order_id = $1 AND release_state = 'none'`,
		orderID)
	if err != nil {
		return false, err
	}
	n, _ := res.RowsAffected()
	return n > 0, nil
}

// OrderByID loads an order by its id (any owner). Used by the stream consumer.
func (s *Store) OrderByID(ctx context.Context, orderID string) (*Order, error) {
	return s.scanOrder(s.db.QueryRowContext(ctx,
		`SELECT order_id, client_order_id, user_id, symbol, side, type, time_in_force,
		        price::text, quantity::text, quote_order_qty::text, filled_quantity::text,
		        status, frozen_asset, frozen_amount::text, unfrozen, created_at
		 FROM orders WHERE order_id = $1`, orderID))
}

// ExpiredUnseenOrders returns orders for symbol whose engine_deadline < watermark
// and that have not yet been seen by the stream (engine_seen = false).
func (s *Store) ExpiredUnseenOrders(ctx context.Context, symbol string, watermark int64) ([]*Order, error) {
	rows, err := s.db.QueryContext(ctx,
		`SELECT order_id, client_order_id, user_id, symbol, side, type, time_in_force,
		        price::text, quantity::text, quote_order_qty::text, filled_quantity::text,
		        status, frozen_asset, frozen_amount::text, unfrozen, created_at
		 FROM orders
		 WHERE symbol = $1
		   AND engine_deadline IS NOT NULL
		   AND engine_deadline < $2
		   AND NOT engine_seen
		   AND status IN ('new','partially_filled')`,
		symbol, watermark)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	var out []*Order
	for rows.Next() {
		o, err := s.scanOrder(rows)
		if err != nil {
			return nil, err
		}
		out = append(out, o)
	}
	return out, rows.Err()
}

// ---- trade_fees ----

// RecordTradeFee freezes the fee rate for an order+role. Idempotent; the first
// value wins (ON CONFLICT DO NOTHING).
func (s *Store) RecordTradeFee(ctx context.Context, orderID, role string, rate decimal.Decimal) error {
	_, err := s.db.ExecContext(ctx,
		`INSERT INTO trade_fees (order_id, role, rate) VALUES ($1, $2, $3) ON CONFLICT DO NOTHING`,
		orderID, role, rate.String())
	return err
}

// GetTradeFee returns the frozen fee rate for an order+role.
func (s *Store) GetTradeFee(ctx context.Context, orderID, role string) (decimal.Decimal, error) {
	var rateStr string
	err := s.db.QueryRowContext(ctx,
		`SELECT rate::text FROM trade_fees WHERE order_id = $1 AND role = $2`,
		orderID, role).Scan(&rateStr)
	if errors.Is(err, errNoRows) {
		return decimal.Zero, err
	}
	if err != nil {
		return decimal.Zero, err
	}
	return decimal.NewFromString(rateStr)
}

// ---- settlement_failures ----

// RecordSettlementFailure inserts a dead-letter record for a failed trade.
func (s *Store) RecordSettlementFailure(ctx context.Context, tradeID, orderID, userID, symbol, reason string) error {
	_, err := s.db.ExecContext(ctx,
		`INSERT INTO settlement_failures (trade_id, order_id, user_id, symbol, reason)
		 VALUES ($1, $2, $3, $4, $5)`,
		tradeID, orderID, userID, symbol, reason)
	return err
}
