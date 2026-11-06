package main

import (
	"context"
	"database/sql"
	"errors"
	"fmt"
	"log"
	"strconv"
	"time"

	"github.com/shopspring/decimal"
)

func crossed(condition string, previous *decimal.Decimal, threshold, current decimal.Decimal) bool {
	if condition == "gte" {
		return current.GreaterThanOrEqual(threshold) && (previous == nil || previous.LessThan(threshold))
	}
	return current.LessThanOrEqual(threshold) && (previous == nil || previous.GreaterThan(threshold))
}

type alertCandidate struct {
	id, uid, threshold, condition string
	previous                      sql.NullString
}

func (s *Server) evaluateSymbol(ctx context.Context, symbol string) error {
	symbolInfo, err := s.market.Symbol(ctx, symbol)
	if err != nil || symbolInfo.Status != "trading" {
		return err
	}
	quote, err := s.market.Index(ctx, symbol)
	if err != nil {
		var api *apiError
		if errors.As(err, &api) {
			return nil // stale/unavailable: do not advance the crossing baseline
		}
		return err
	}
	// Advance through a stable keyset until no active alerts remain. A single
	// page is still bounded, but IDs after 500 are evaluated in this same round;
	// the next round never restarts a symbol at only its first 500 records.
	const pageSize = 500
	after := ""
	for {
		rows, err := s.store.db.QueryContext(ctx, `SELECT alert_id,user_id,threshold::text,condition,last_observed_price::text FROM price_alerts WHERE symbol=$1 AND status='active' AND enabled=true AND alert_id>$2 ORDER BY alert_id LIMIT $3`, symbol, after, pageSize)
		if err != nil {
			return err
		}
		var candidates []alertCandidate
		for rows.Next() {
			var c alertCandidate
			if err := rows.Scan(&c.id, &c.uid, &c.threshold, &c.condition, &c.previous); err != nil {
				rows.Close()
				return err
			}
			candidates = append(candidates, c)
		}
		rows.Close()
		if err := rows.Err(); err != nil {
			return err
		}
		if len(candidates) == 0 {
			return nil
		}
		for _, c := range candidates {
			if err := s.evaluateOne(ctx, c.id, c.uid, c.threshold, c.condition, c.previous, quote); err != nil {
				return err
			}
		}
		after = candidates[len(candidates)-1].id
		if len(candidates) < pageSize {
			return nil
		}
	}
}

func (s *Server) evaluateOne(ctx context.Context, alertID, userID, thresholdText, condition string, old sql.NullString, quote Quote) error {
	tx, err := s.store.db.BeginTx(ctx, nil)
	if err != nil {
		return err
	}
	defer tx.Rollback()
	var claimed bool
	if err := tx.QueryRowContext(ctx, `SELECT pg_try_advisory_xact_lock(918273918, hashtext($1))`, alertID).Scan(&claimed); err != nil {
		return err
	}
	if !claimed {
		return nil
	}
	var epoch int64
	var status, persistedThreshold, persistedCondition string
	var enabled bool
	var previous sql.NullString
	err = tx.QueryRowContext(ctx, `SELECT evaluation_epoch,status,enabled,threshold::text,condition,last_observed_price::text FROM price_alerts WHERE alert_id=$1 AND user_id=$2 FOR UPDATE`, alertID, userID).Scan(&epoch, &status, &enabled, &persistedThreshold, &persistedCondition, &previous)
	if err == sql.ErrNoRows {
		return nil
	}
	if err != nil {
		return err
	}
	if status != "active" || !enabled || thresholdText != persistedThreshold || condition != persistedCondition {
		return nil
	}
	threshold := decimal.RequireFromString(persistedThreshold)
	var previousPrice *decimal.Decimal
	if previous.Valid {
		p := decimal.RequireFromString(previous.String)
		previousPrice = &p
	}
	now := time.Now().UTC()
	if !crossed(persistedCondition, previousPrice, threshold, quote.Price) {
		if _, err := tx.ExecContext(ctx, `UPDATE price_alerts SET last_observed_price=$2,last_evaluated_at=$3 WHERE alert_id=$1 AND evaluation_epoch=$4`, alertID, quote.Price.String(), now, epoch); err != nil {
			return err
		}
		return tx.Commit()
	}
	if _, err := tx.ExecContext(ctx, `SELECT pg_advisory_xact_lock(hashtext($1),918274)`, userID); err != nil {
		return err
	}
	var blocked bool
	if err := tx.QueryRowContext(ctx, `SELECT EXISTS(SELECT 1 FROM price_alerts WHERE user_id=$1 AND alert_id<>$2 AND triggered_at>=$3)`, userID, alertID, now.Add(-s.cfg.AlertCooldown)).Scan(&blocked); err != nil {
		return err
	}
	var ownCooldown sql.NullTime
	if err := tx.QueryRowContext(ctx, `SELECT cooldown_until FROM price_alerts WHERE alert_id=$1`, alertID).Scan(&ownCooldown); err != nil {
		return err
	}
	if blocked || (ownCooldown.Valid && now.Before(ownCooldown.Time)) {
		if _, err := tx.ExecContext(ctx, `UPDATE price_alerts SET last_evaluated_at=$2,cooldown_until=$3 WHERE alert_id=$1 AND evaluation_epoch=$4`, alertID, now, now.Add(s.cfg.AlertCooldown), epoch); err != nil {
			return err
		}
		return tx.Commit()
	}
	result, err := tx.ExecContext(ctx, `UPDATE price_alerts SET status='triggering',enabled=false,last_observed_price=$2,last_evaluated_at=$3,triggered_price=$2,triggered_at=$3,delivery_status='pending',cooldown_until=$4,revision=revision+1,updated_at=$3 WHERE alert_id=$1 AND evaluation_epoch=$5 AND status='active' AND enabled=true`, alertID, quote.Price.String(), now, now.Add(s.cfg.AlertCooldown), epoch)
	if err != nil {
		return err
	}
	if n, _ := result.RowsAffected(); n == 0 {
		return nil
	}
	if _, err := tx.ExecContext(ctx, `INSERT INTO price_alert_trigger_history (alert_id,evaluation_epoch,symbol,threshold,condition,previous_price,observed_price,observed_at,created_at) VALUES ($1,$2,$3,$4,$5,$6,$7,$8,$9)`, alertID, epoch, quote.Symbol, threshold.String(), persistedCondition, previous, quote.Price.String(), quote.ObservedAt, now); err != nil {
		return err
	}
	if _, err := tx.ExecContext(ctx, `INSERT INTO price_alert_outbox (alert_id,evaluation_epoch,user_id,symbol,condition,threshold,observed_price,observed_at,next_attempt_at,created_at,updated_at) VALUES ($1,$2,$3,$4,$5,$6,$7,$8,$9,$9,$9)`, alertID, epoch, userID, quote.Symbol, persistedCondition, threshold.String(), quote.Price.String(), quote.ObservedAt, now); err != nil {
		return err
	}
	if _, err := tx.ExecContext(ctx, `UPDATE price_alerts SET status='triggered' WHERE alert_id=$1 AND evaluation_epoch=$2 AND status='triggering'`, alertID, epoch); err != nil {
		return err
	}
	return tx.Commit()
}

func (s *Server) deliverAlerts(ctx context.Context) error {
	rows, err := s.store.db.QueryContext(ctx, `SELECT alert_id,evaluation_epoch,user_id,symbol,condition,threshold::text,observed_price::text,observed_at,attempt_count FROM price_alert_outbox WHERE (state='pending' OR (state='delivered' AND EXISTS(SELECT 1 FROM price_alerts a WHERE a.alert_id=price_alert_outbox.alert_id AND a.evaluation_epoch=price_alert_outbox.evaluation_epoch AND a.delivery_status<>'delivered_in_app'))) AND next_attempt_at<=now() ORDER BY next_attempt_at,alert_id LIMIT 50`)
	if err != nil {
		return err
	}
	type job struct {
		id, uid, symbol, condition, threshold, price string
		epoch                                        int64
		observed                                     time.Time
		attempts                                     int
	}
	if _, err := s.store.db.ExecContext(ctx, `UPDATE price_alert_outbox SET state='pending',next_attempt_at=now(),updated_at=now(),last_error='delivery claim lease expired' WHERE state='delivering' AND updated_at<now()-interval '3 minutes'`); err != nil {
		return err
	}

	var jobs []job
	for rows.Next() {
		var j job
		if err := rows.Scan(&j.id, &j.epoch, &j.uid, &j.symbol, &j.condition, &j.threshold, &j.price, &j.observed, &j.attempts); err != nil {
			rows.Close()
			return err
		}
		jobs = append(jobs, j)
	}
	rows.Close()
	if err := rows.Err(); err != nil {
		return err
	}
	for _, j := range jobs {
		if enabled, err := s.features.Enabled(ctx, "priceAlerts"); err != nil {
			return err
		} else if !enabled {
			return nil
		}
		tx, err := s.store.db.BeginTx(ctx, nil)
		if err != nil {
			return err
		}
		var active bool
		var savedNotification sql.NullString
		if err := tx.QueryRowContext(ctx, `SELECT (o.state='pending' OR (o.state='delivered' AND a.delivery_status<>'delivered_in_app')) AND a.status='triggered' AND a.enabled=false AND a.evaluation_epoch=$2,a.notification_id FROM price_alert_outbox o JOIN price_alerts a ON a.alert_id=o.alert_id AND a.evaluation_epoch=o.evaluation_epoch WHERE o.alert_id=$1 AND o.evaluation_epoch=$2 FOR UPDATE OF o,a`, j.id, j.epoch).Scan(&active, &savedNotification); err == sql.ErrNoRows {
			tx.Rollback()
			continue
		} else if err != nil {
			tx.Rollback()
			return err
		}
		if active && savedNotification.Valid {
			if _, err := tx.ExecContext(ctx, `UPDATE price_alert_outbox SET state='delivered',notification_id=$3,updated_at=now() WHERE alert_id=$1 AND evaluation_epoch=$2`, j.id, j.epoch, savedNotification.String); err != nil {
				tx.Rollback()
				return err
			}
			if _, err := tx.ExecContext(ctx, `UPDATE price_alerts SET delivery_status='delivered_in_app',updated_at=now() WHERE alert_id=$1 AND evaluation_epoch=$2`, j.id, j.epoch); err != nil {
				tx.Rollback()
				return err
			}
			if err := tx.Commit(); err != nil {
				return err
			}
			continue
		}
		if !active {
			if _, err := tx.ExecContext(ctx, `UPDATE price_alert_outbox SET state='expired',updated_at=now() WHERE alert_id=$1 AND evaluation_epoch=$2 AND state='pending'`, j.id, j.epoch); err != nil {
				tx.Rollback()
				return err
			}
			if err := tx.Commit(); err != nil {
				return err
			}
			continue
		}
		if _, err := tx.ExecContext(ctx, `UPDATE price_alert_outbox SET state='delivering',attempt_count=attempt_count+1,updated_at=now() WHERE alert_id=$1 AND evaluation_epoch=$2`, j.id, j.epoch); err != nil {
			tx.Rollback()
			return err
		}
		if err := tx.Commit(); err != nil {
			return err
		}
		body := fmt.Sprintf("条件 %s %s；触发价 %s；价格源 INDEX；观测时间 %s。", j.condition, j.threshold, j.price, j.observed.UTC().Format(time.RFC3339))
		id, notifyErr := s.notifier.InApp(ctx, j.uid, j.symbol+" 价格预警", body, "price-alert:"+j.id+":"+strconv.FormatInt(j.epoch, 10))
		tx, err = s.store.db.BeginTx(ctx, nil)
		if err != nil {
			return err
		}
		var state string
		var currentEpoch int64
		var status string
		var enabledFlag bool
		err = tx.QueryRowContext(ctx, `SELECT o.state,a.evaluation_epoch,a.status,a.enabled FROM price_alert_outbox o JOIN price_alerts a ON a.alert_id=o.alert_id AND a.evaluation_epoch=$2 WHERE o.alert_id=$1 AND o.evaluation_epoch=$2 FOR UPDATE OF o,a`, j.id, j.epoch).Scan(&state, &currentEpoch, &status, &enabledFlag)
		if err == sql.ErrNoRows {
			tx.Rollback()
			continue
		}
		if err != nil {
			tx.Rollback()
			return err
		}
		if state != "delivering" || status != "triggered" || enabledFlag || currentEpoch != j.epoch {
			// Do not roll back a delivered account inbox notification after a
			// client read; mark the audit as superseded and never retry it.
			_, err = tx.ExecContext(ctx, `UPDATE price_alert_outbox SET state='expired',updated_at=now() WHERE alert_id=$1 AND evaluation_epoch=$2`, j.id, j.epoch)
			if err != nil {
				tx.Rollback()
				return err
			}
			if err := tx.Commit(); err != nil {
				return err
			}
			continue
		}
		if notifyErr != nil {
			delay := time.Duration(1<<min(j.attempts, 5)) * time.Second
			if j.attempts >= 5 {
				state = "failed"
			} else {
				state = "pending"
			}
			if _, err := tx.ExecContext(ctx, `UPDATE price_alert_outbox SET state=$3,last_error=$4,next_attempt_at=$5,updated_at=now() WHERE alert_id=$1 AND evaluation_epoch=$2`, j.id, j.epoch, state, notifyErr.Error(), time.Now().Add(delay)); err != nil {
				tx.Rollback()
				return err
			}
			if state == "failed" {
				if _, err := tx.ExecContext(ctx, `UPDATE price_alerts SET delivery_status='failed',updated_at=now() WHERE alert_id=$1 AND evaluation_epoch=$2`, j.id, j.epoch); err != nil {
					tx.Rollback()
					return err
				}
			}
		} else {
			if _, err := tx.ExecContext(ctx, `UPDATE price_alert_outbox SET state='delivered',notification_id=$3,updated_at=now() WHERE alert_id=$1 AND evaluation_epoch=$2`, j.id, j.epoch, id); err != nil {
				tx.Rollback()
				return err
			}
			if _, err := tx.ExecContext(ctx, `UPDATE price_alerts SET delivery_status='delivered_in_app',notification_id=$3,updated_at=now() WHERE alert_id=$1 AND evaluation_epoch=$2`, j.id, j.epoch, id); err != nil {
				tx.Rollback()
				return err
			}
		}
		if err := tx.Commit(); err != nil {
			return err
		}
	}
	return nil
}

func (s *Server) runAlertWorker(ctx context.Context) {
	ticker := time.NewTicker(s.cfg.EvaluationInterval)
	defer ticker.Stop()
	for {
		if enabled, err := s.features.Enabled(ctx, "priceAlerts"); err == nil && enabled {
			var symbols []string
			rows, err := s.store.db.QueryContext(ctx, `SELECT DISTINCT symbol FROM price_alerts WHERE status='active' AND enabled=true ORDER BY symbol`)
			if err != nil {
				log.Printf("price alert symbols: %v", err)
			} else {
				for rows.Next() {
					var sym string
					if rows.Scan(&sym) == nil {
						symbols = append(symbols, sym)
					}
				}
				rows.Close()
			}
			for _, sym := range symbols {
				if err := s.evaluateSymbol(ctx, sym); err != nil {
					log.Printf("price alert %s: %v", sym, err)
				}
			}
		} else if err != nil {
			log.Printf("price alert feature gate: %v", err)
		}
		select {
		case <-ctx.Done():
			return
		case <-ticker.C:
		}
	}
}
func (s *Server) runDeliveryWorker(ctx context.Context) {
	ticker := time.NewTicker(s.cfg.DeliveryInterval)
	defer ticker.Stop()
	for {
		if err := s.deliverAlerts(ctx); err != nil {
			log.Printf("price alert delivery: %v", err)
		}
		select {
		case <-ctx.Done():
			return
		case <-ticker.C:
		}
	}
}
