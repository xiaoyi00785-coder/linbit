package main

import (
	"context"
	"database/sql"
	"fmt"
	"time"
)

// ConsumeAlertWrite applies one durable fixed-window bucket to every admitted
// alert CRUD write, including duplicate and missing-resource attempts. The row
// lock shares the counter across service replicas and restarts; it is
// deliberately independent of the 300-second once-alert trigger cooldown.
// An over-limit attempt returns its remaining wait without increasing the
// stored count. Admission commits independently before request validation so
// malformed, conflicting, duplicate and missing-resource writes are throttled too.
func (s *Store) ConsumeAlertWrite(ctx context.Context, tx *sql.Tx, userID string, now time.Time, limit int, window time.Duration) (time.Duration, error) {
	if limit < 1 || window <= 0 {
		return 0, fmt.Errorf("invalid alert write limit configuration")
	}
	now = now.UTC()
	var started time.Time
	var count int
	if _, err := tx.ExecContext(ctx, `INSERT INTO price_alert_write_rate_limits (user_id,window_started_at,request_count) VALUES ($1,$2,0) ON CONFLICT (user_id) DO NOTHING`, userID, now.Truncate(window)); err != nil {
		return 0, err
	}
	if err := tx.QueryRowContext(ctx, `SELECT window_started_at,request_count FROM price_alert_write_rate_limits WHERE user_id=$1 FOR UPDATE`, userID).Scan(&started, &count); err != nil {
		return 0, err
	}
	if !now.Before(started.Add(window)) {
		started = now.Truncate(window)
		count = 0
	}
	if count >= limit {
		wait := started.Add(window).Sub(now)
		if wait < 0 {
			wait = 0
		}
		return wait, nil
	}
	if _, err := tx.ExecContext(ctx, `INSERT INTO price_alert_write_rate_limits (user_id,window_started_at,request_count) VALUES ($1,$2,$3) ON CONFLICT (user_id) DO UPDATE SET window_started_at=EXCLUDED.window_started_at,request_count=EXCLUDED.request_count`, userID, started, count+1); err != nil {
		return 0, err
	}
	return 0, nil
}

// AdmitAlertWrite durably accounts for one authenticated CRUD attempt. A 404 or
// idempotent replay consumes the same budget as a successful mutation.
func (s *Store) AdmitAlertWrite(ctx context.Context, userID string, now time.Time, limit int, window time.Duration) error {
	tx, err := s.db.BeginTx(ctx, nil)
	if err != nil {
		return err
	}
	defer tx.Rollback()
	wait, err := s.ConsumeAlertWrite(ctx, tx, userID, now, limit, window)
	if err != nil {
		return err
	}
	if err := tx.Commit(); err != nil {
		return err
	}
	if wait > 0 {
		return alertRateLimited(wait)
	}
	return nil
}
