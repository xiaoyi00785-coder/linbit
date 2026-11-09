package main

// 价格预警写限流。固定窗口，行锁在副本和重启之间共享，和 300 秒的触发冷却不是同一套计数。
// 超限只返回剩余等待，不增加计数。准入在校验请求之前提交，畸形、冲突、重复、不存在的写也占额度。

import (
	"context"
	"database/sql"
	"fmt"
	"time"
)

// ConsumeAlertWrite 对一次已准入的预警写操作记一格。超限返回剩余等待且不增加计数。limit<1 或 window<=0 直接报配置错误。
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

// AdmitAlertWrite 给一次已登录的写操作入账并提交。404 和幂等重放与成功修改占同样额度。
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
