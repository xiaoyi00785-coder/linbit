package main

import (
	"errors"
	"github.com/linbit/platform/services/go/common"
	"net/http"
	"strconv"
	"time"

	"github.com/jackc/pgx/v5/pgconn"
)

type apiError struct {
	Status  int
	Code    string
	Message string
}

func (e *apiError) Error() string                   { return e.Message }
func apiErr(status int, code, message string) error { return &apiError{status, code, message} }

type alertWriteRateLimit struct{ retryAfterSec int }

func (e *alertWriteRateLimit) Error() string {
	return "价格预警写操作过于频繁，请稍后再试"
}
func alertRateLimited(wait time.Duration) error {
	seconds := int((wait + time.Second - 1) / time.Second)
	if seconds < 1 {
		seconds = 1
	}
	return &alertWriteRateLimit{retryAfterSec: seconds}
}
func writeErr(w http.ResponseWriter, err error) {
	var limited *alertWriteRateLimit
	if errors.As(err, &limited) {
		seconds := strconv.Itoa(limited.retryAfterSec)
		w.Header().Set("Retry-After", seconds)
		common.WriteJSON(w, http.StatusTooManyRequests, map[string]any{"code": "RATE_LIMITED", "message": limited.Error(), "requestId": common.RequestID(), "retryAfterSec": limited.retryAfterSec})
		return
	}
	var e *apiError
	if errors.As(err, &e) {
		common.WriteError(w, e.Status, e.Code, e.Message)
		return
	}
	var dbErr *pgconn.PgError
	if errors.As(err, &dbErr) {
		if dbErr.Code == "57014" || dbErr.Code == "53200" || dbErr.Code == "53400" {
			common.WriteJSON(w, http.StatusServiceUnavailable, map[string]string{"code": "ALERT_EVALUATOR_UNAVAILABLE", "message": "价格预警扫描超时，将在下轮继续", "requestId": common.RequestID()})
			return
		}
	}
	common.WriteError(w, http.StatusInternalServerError, "INTERNAL", "服务器内部错误")
}
