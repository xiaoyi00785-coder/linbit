package main

import (
	"context"
	"database/sql"
	"net/http"
	"strconv"
	"strings"
	"time"

	"github.com/shopspring/decimal"
)

type CreatePriceAlert struct {
	ClientAlertID string   `json:"clientAlertId"`
	Market        string   `json:"market"`
	Symbol        string   `json:"symbol"`
	Condition     string   `json:"condition"`
	Threshold     string   `json:"threshold"`
	PriceSource   string   `json:"priceSource"`
	TriggerMode   string   `json:"triggerMode"`
	Channels      []string `json:"channels"`
	Enabled       *bool    `json:"enabled"`
	Note          string   `json:"note"`
}

type UpdatePriceAlert struct {
	Revision  *int64   `json:"revision"`
	Threshold *string  `json:"threshold"`
	Condition *string  `json:"condition"`
	Enabled   *bool    `json:"enabled"`
	Channels  []string `json:"channels"`
	Note      *string  `json:"note"`
}

func validateAlert(in CreatePriceAlert) (CreatePriceAlert, decimal.Decimal, error) {
	if in.Market == "" {
		in.Market = "spot"
	}
	if in.PriceSource == "" {
		in.PriceSource = "INDEX"
	}
	if in.TriggerMode == "" {
		in.TriggerMode = "once"
	}
	if in.Channels == nil {
		in.Channels = []string{"in_app"}
	}
	in.ClientAlertID, in.Symbol = strings.TrimSpace(in.ClientAlertID), strings.ToUpper(strings.TrimSpace(in.Symbol))
	if !validClientID.MatchString(in.ClientAlertID) || len(in.Note) > 120 {
		return in, decimal.Zero, apiErr(http.StatusBadRequest, "VALIDATION_ERROR", "clientAlertId 或 note 无效")
	}
	if in.Market != "spot" || (in.Condition != "gte" && in.Condition != "lte") || in.PriceSource != "INDEX" || in.TriggerMode != "once" {
		return in, decimal.Zero, apiErr(http.StatusBadRequest, "VALIDATION_ERROR", "P0 仅支持 spot/INDEX/gte/lte/once")
	}
	if len(in.Channels) != 1 || in.Channels[0] != "in_app" {
		return in, decimal.Zero, apiErr(http.StatusBadRequest, "VALIDATION_ERROR", "P0 仅支持 in_app")
	}
	threshold, ok := positiveDecimal(in.Threshold)
	if !ok {
		return in, decimal.Zero, apiErr(http.StatusBadRequest, "INVALID_PRICE", "threshold 必须为正十进制")
	}
	return in, threshold, nil
}

func (s *Store) CreateAlert(ctx context.Context, userID string, in CreatePriceAlert, market Market, clock time.Time) (PriceAlert, error) {
	in, threshold, err := validateAlert(in)
	if err != nil {
		return PriceAlert{}, err
	}
	hash := requestHash(in)
	clock = clock.UTC()
	tx, err := s.db.BeginTx(ctx, nil)
	if err != nil {
		return PriceAlert{}, err
	}
	defer tx.Rollback()
	// Serialize a user's idempotent creation and quota admission before reading
	// an existing clientAlertId; concurrent replay must not become SQL 23505.
	if _, err := tx.ExecContext(ctx, `SELECT pg_advisory_xact_lock(hashtext($1),918273)`, userID); err != nil {
		return PriceAlert{}, err
	}
	var oldHash string
	var old PriceAlert
	err = tx.QueryRowContext(ctx, `SELECT request_hash,alert_id FROM price_alerts WHERE user_id=$1 AND client_alert_id=$2 FOR UPDATE`, userID, in.ClientAlertID).Scan(&oldHash, &old.AlertID)
	if err == nil {
		if oldHash != hash {
			return PriceAlert{}, apiErr(http.StatusConflict, "IDEMPOTENCY_CONFLICT", "相同 clientAlertId 的请求内容不同")
		}
		old, err = alertByIDTx(ctx, tx, userID, old.AlertID, false)
		return old, err
	}
	if err != sql.ErrNoRows {
		return PriceAlert{}, err
	}
	symbol, err := market.Symbol(ctx, in.Symbol)
	if err != nil {
		return PriceAlert{}, err
	}
	if symbol.Status != "trading" {
		return PriceAlert{}, apiErr(http.StatusBadRequest, "SYMBOL_PAUSED", "交易对已下线")
	}
	if !threshold.Equal(threshold.Round(int32(symbol.PricePrecision))) {
		return PriceAlert{}, apiErr(http.StatusBadRequest, "INVALID_PRICE", "threshold 超出交易对价格精度")
	}
	enabled := true
	if in.Enabled != nil {
		enabled = *in.Enabled
	}
	status := "active"
	if !enabled {
		status = "disabled"
	}
	var count, history int
	if err := tx.QueryRowContext(ctx, `SELECT count(*) FILTER (WHERE status='active'),count(*) FROM price_alerts WHERE user_id=$1 AND status<>'deleted'`, userID).Scan(&count, &history); err != nil {
		return PriceAlert{}, err
	}
	if history >= 100 || (status == "active" && count >= 50) {
		return PriceAlert{}, apiErr(http.StatusConflict, "ALERT_LIMIT_EXCEEDED", "每用户最多 50 条激活、100 条历史预警")
	}
	now := clock
	a := PriceAlert{AlertID: randomID("al"), ClientAlertID: in.ClientAlertID, Market: "spot", Symbol: in.Symbol, Condition: in.Condition, Threshold: threshold.String(), PriceSource: "INDEX", TriggerMode: "once", Channels: []string{"in_app"}, Enabled: enabled, Status: status, Revision: 1, DeliveryStatus: "none", Note: in.Note, CreatedAt: now, UpdatedAt: now}
	a.Limit.ActivePerUser = 50
	_, err = tx.ExecContext(ctx, `INSERT INTO price_alerts (alert_id,user_id,client_alert_id,request_hash,market,symbol,condition,threshold,enabled,status,note,created_at,updated_at) VALUES ($1,$2,$3,$4,'spot',$5,$6,$7,$8,$9,$10,$11,$11)`, a.AlertID, userID, in.ClientAlertID, hash, in.Symbol, in.Condition, threshold.String(), enabled, status, in.Note, now)
	if err != nil {
		return PriceAlert{}, err
	}
	return a, tx.Commit()
}

const alertColumns = `alert_id,client_alert_id,symbol,condition,threshold::text,enabled,status,revision,last_observed_price::text,last_evaluated_at,triggered_price::text,triggered_at,notification_id,delivery_status,cooldown_until,coalesce(note,''),created_at,updated_at`

func scanAlert(scanner interface{ Scan(...any) error }) (PriceAlert, error) {
	var a PriceAlert
	var last, trigger sql.NullString
	var evaluated, triggered, cooldown sql.NullTime
	var notification sql.NullString
	err := scanner.Scan(&a.AlertID, &a.ClientAlertID, &a.Symbol, &a.Condition, &a.Threshold, &a.Enabled, &a.Status, &a.Revision, &last, &evaluated, &trigger, &triggered, &notification, &a.DeliveryStatus, &cooldown, &a.Note, &a.CreatedAt, &a.UpdatedAt)
	if err != nil {
		return PriceAlert{}, err
	}
	a.Market, a.PriceSource, a.TriggerMode, a.Channels = "spot", "INDEX", "once", []string{"in_app"}
	a.Limit.ActivePerUser = 50
	if last.Valid {
		a.LastObservedPrice = &last.String
	}
	if evaluated.Valid {
		a.LastEvaluatedAt = tptr(evaluated.Time)
	}
	if trigger.Valid {
		a.TriggeredPrice = &trigger.String
	}
	if triggered.Valid {
		a.TriggeredAt = tptr(triggered.Time)
	}
	if notification.Valid {
		a.NotificationID = &notification.String
	}
	if cooldown.Valid {
		a.CooldownUntil = tptr(cooldown.Time)
	}
	return a, nil
}
func alertByIDTx(ctx context.Context, q interface {
	QueryRowContext(context.Context, string, ...any) *sql.Row
}, userID, alertID string, includeDeleted bool) (PriceAlert, error) {
	where := `SELECT ` + alertColumns + ` FROM price_alerts WHERE user_id=$1 AND alert_id=$2`
	if !includeDeleted {
		where += ` AND status<>'deleted'`
	}
	a, err := scanAlert(q.QueryRowContext(ctx, where, userID, alertID))
	if err == sql.ErrNoRows {
		return PriceAlert{}, apiErr(http.StatusNotFound, "ALERT_NOT_FOUND", "价格预警不存在")
	}
	return a, err
}
func (s *Store) ListAlerts(ctx context.Context, userID, symbol, status string, page, pageSize int) (map[string]any, error) {
	if status == "deleted" || (status != "" && status != "active" && status != "disabled" && status != "triggered") {
		return nil, apiErr(http.StatusBadRequest, "VALIDATION_ERROR", "status 仅支持 active/triggered/disabled")
	}
	args := []any{userID}
	where := ` FROM price_alerts WHERE user_id=$1 AND status<>'deleted'`
	if status != "" {
		args = append(args, status)
		where += " AND status=$" + strconv.Itoa(len(args))
	}
	if symbol != "" {
		args = append(args, strings.ToUpper(symbol))
		where += " AND symbol=$" + strconv.Itoa(len(args))
	}
	var total int
	if err := s.db.QueryRowContext(ctx, `SELECT count(*)`+where, args...).Scan(&total); err != nil {
		return nil, err
	}
	args = append(args, pageSize, (page-1)*pageSize)
	rows, err := s.db.QueryContext(ctx, `SELECT `+alertColumns+where+` ORDER BY created_at DESC,alert_id DESC LIMIT $`+strconv.Itoa(len(args)-1)+` OFFSET $`+strconv.Itoa(len(args)), args...)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	out := []PriceAlert{}
	for rows.Next() {
		a, err := scanAlert(rows)
		if err != nil {
			return nil, err
		}
		out = append(out, a)
	}
	if err := rows.Err(); err != nil {
		return nil, err
	}
	return map[string]any{"items": out, "page": page, "pageSize": pageSize, "total": total}, nil
}

func (s *Store) UpdateAlert(ctx context.Context, userID, alertID string, in UpdatePriceAlert, market Market, now time.Time) (PriceAlert, error) {
	if in.Revision == nil {
		return PriceAlert{}, apiErr(http.StatusBadRequest, "REVISION_REQUIRED", "revision 必填")
	}
	tx, err := s.db.BeginTx(ctx, nil)
	if err != nil {
		return PriceAlert{}, err
	}
	defer tx.Rollback()
	old, err := alertByIDTx(ctx, tx, userID, alertID, false)
	if err != nil {
		return PriceAlert{}, err
	}
	// Re-read with a row lock before applying the revision compare-and-set.
	if _, err := tx.ExecContext(ctx, `SELECT alert_id FROM price_alerts WHERE alert_id=$1 FOR UPDATE`, alertID); err != nil {
		return PriceAlert{}, err
	}
	old, err = alertByIDTx(ctx, tx, userID, alertID, false)
	if err != nil {
		return PriceAlert{}, err
	}
	if *in.Revision != old.Revision {
		return PriceAlert{}, apiErr(http.StatusConflict, "REVISION_CONFLICT", "预警已被修改或触发，请重新读取")
	}
	if in.Channels != nil && (len(in.Channels) != 1 || in.Channels[0] != "in_app") {
		return PriceAlert{}, apiErr(http.StatusBadRequest, "VALIDATION_ERROR", "P0 仅支持 in_app")
	}
	if in.Note != nil && len(*in.Note) > 120 {
		return PriceAlert{}, apiErr(http.StatusBadRequest, "VALIDATION_ERROR", "note 最长 120 字节")
	}
	threshold := decimal.RequireFromString(old.Threshold)
	if in.Threshold != nil {
		var ok bool
		threshold, ok = positiveDecimal(*in.Threshold)
		if !ok {
			return PriceAlert{}, apiErr(http.StatusBadRequest, "INVALID_PRICE", "threshold 必须为正")
		}
	}
	condition := old.Condition
	if in.Condition != nil && (*in.Condition == "gte" || *in.Condition == "lte") {
		condition = *in.Condition
	} else if in.Condition != nil {
		return PriceAlert{}, apiErr(http.StatusBadRequest, "VALIDATION_ERROR", "condition 仅支持 gte/lte")
	}
	symbol, err := market.Symbol(ctx, old.Symbol)
	if err != nil {
		return PriceAlert{}, err
	}
	if !threshold.Equal(threshold.Round(int32(symbol.PricePrecision))) {
		return PriceAlert{}, apiErr(http.StatusBadRequest, "INVALID_PRICE", "threshold 超出交易对价格精度")
	}
	enabled := old.Enabled
	if in.Enabled != nil {
		enabled = *in.Enabled
	}
	status := "disabled"
	if enabled {
		status = "active"
	}
	if status == "active" && old.Status != "active" {
		if _, err := tx.ExecContext(ctx, `SELECT pg_advisory_xact_lock(hashtext($1),918273)`, userID); err != nil {
			return PriceAlert{}, err
		}
		var count int
		if err := tx.QueryRowContext(ctx, `SELECT count(*) FROM price_alerts WHERE user_id=$1 AND status='active' AND alert_id<>$2`, userID, alertID).Scan(&count); err != nil {
			return PriceAlert{}, err
		}
		if count >= 50 {
			return PriceAlert{}, apiErr(http.StatusConflict, "ALERT_LIMIT_EXCEEDED", "每用户最多 50 条激活预警")
		}
	}
	note := old.Note
	if in.Note != nil {
		note = *in.Note
	}
	_, err = tx.ExecContext(ctx, `UPDATE price_alerts SET threshold=$3,condition=$4,enabled=$5,status=$6,note=$7,revision=revision+1,evaluation_epoch=evaluation_epoch+1,last_observed_price=NULL,last_evaluated_at=NULL,triggered_price=NULL,triggered_at=NULL,notification_id=NULL,delivery_status='none',updated_at=$2 WHERE user_id=$1 AND alert_id=$8 AND revision=$9`, userID, now.UTC(), threshold.String(), condition, enabled, status, note, alertID, *in.Revision)
	if err != nil {
		return PriceAlert{}, err
	}
	// Any older queued message is expired in the same revision transaction.
	if _, err := tx.ExecContext(ctx, `UPDATE price_alert_outbox SET state='expired',updated_at=$2 WHERE alert_id=$1 AND evaluation_epoch<(SELECT evaluation_epoch FROM price_alerts WHERE alert_id=$1) AND state='pending'`, alertID, now.UTC()); err != nil {
		return PriceAlert{}, err
	}
	a, err := alertByIDTx(ctx, tx, userID, alertID, false)
	if err != nil {
		return PriceAlert{}, err
	}
	return a, tx.Commit()
}

func (s *Store) DeleteAlert(ctx context.Context, userID, alertID string, revision *int64, now time.Time) error {
	tx, err := s.db.BeginTx(ctx, nil)
	if err != nil {
		return err
	}
	defer tx.Rollback()
	var current int64
	var status string
	err = tx.QueryRowContext(ctx, `SELECT revision,status FROM price_alerts WHERE user_id=$1 AND alert_id=$2 FOR UPDATE`, userID, alertID).Scan(&current, &status)
	if err == sql.ErrNoRows {
		return apiErr(http.StatusNotFound, "ALERT_NOT_FOUND", "价格预警不存在")
	}
	if err != nil {
		return err
	}
	if status == "deleted" {
		return nil
	}
	err = tx.QueryRowContext(ctx, `SELECT revision FROM price_alerts WHERE alert_id=$1`, alertID).Scan(&current)
	if err != nil {
		return err
	}
	if revision != nil && *revision != current {
		return apiErr(http.StatusConflict, "REVISION_CONFLICT", "预警已被修改或触发，请重新读取")
	}
	if _, err := tx.ExecContext(ctx, `UPDATE price_alerts SET status='deleted',enabled=false,revision=revision+1,evaluation_epoch=evaluation_epoch+1,deleted_at=$3,updated_at=$3 WHERE user_id=$1 AND alert_id=$2`, userID, alertID, now.UTC()); err != nil {
		return err
	}
	if _, err := tx.ExecContext(ctx, `UPDATE price_alert_outbox SET state='expired',updated_at=$2 WHERE alert_id=$1 AND state='pending'`, alertID, now.UTC()); err != nil {
		return err
	}
	return tx.Commit()
}
