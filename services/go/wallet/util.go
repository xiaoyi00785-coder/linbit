package main

import (
	"net/http"
	"strconv"
	"strings"
)

// itoa is a short alias for strconv.Itoa, used to build positional SQL params.
func itoa(n int) string { return strconv.Itoa(n) }

// pgTextArray joins statuses for use with `= ANY(string_to_array($n, ','))`.
// Status values never contain commas.
func pgTextArray(vals []string) string { return strings.Join(vals, ",") }

// nullStr returns the string value or nil (for COALESCE-style optional updates).
func nullStr(s *string) interface{} {
	if s == nil {
		return nil
	}
	return *s
}

// nullInt returns the int64 value or nil.
func nullInt(n *int64) interface{} {
	if n == nil {
		return nil
	}
	return *n
}

// atoiDefault parses s, falling back to def and clamping to [min, max].
func atoiDefault(s string, def, min, max int) int {
	if s == "" {
		return def
	}
	n, err := strconv.Atoi(s)
	if err != nil || n < min {
		return def
	}
	if n > max {
		return max
	}
	return n
}

// apiStatus maps an internal withdrawal status onto the openapi Withdrawal enum
// (pending/processing/completed/failed/canceled).
func apiStatus(internal string) string {
	switch internal {
	case wStatusReview:
		return "pending"
	case wStatusApproved, wStatusProcessing:
		return "processing"
	case wStatusCompleted:
		return "completed"
	case wStatusFailed:
		return "failed"
	case wStatusRejected, wStatusCanceled:
		return "canceled"
	default:
		return internal
	}
}

// parseWithdrawalID accepts either a raw numeric id or the "wd_<n>" form.
func parseWithdrawalID(s string) (int64, bool) {
	s = strings.TrimSpace(s)
	s = strings.TrimPrefix(s, "wd_")
	n, err := strconv.ParseInt(s, 10, 64)
	if err != nil || n <= 0 {
		return 0, false
	}
	return n, true
}

// methodNotAllowed is a tiny helper for routes that only accept one method.
func methodNotAllowed(w http.ResponseWriter) {
	http.Error(w, "method not allowed", http.StatusMethodNotAllowed)
}
