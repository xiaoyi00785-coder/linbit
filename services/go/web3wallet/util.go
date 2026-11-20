package main

import (
	"context"
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"strconv"
	"strings"
)

// Tiny helpers that are called from several files: kept here so each caller
// doesn't invent its own.

func itoa(n int) string { return strconv.Itoa(n) }

func jsonInto(r *http.Request, v any) {
	b, _ := json.Marshal(v)
	r.Body = io.NopCloser(strings.NewReader(string(b)))
	r.ContentLength = int64(len(b))
	r.Header.Set("Content-Type", "application/json")
}

// jsonUnmarshalString extracts a single JSON string from a RawMessage.
func jsonUnmarshalString(raw json.RawMessage, dst *string) error {
	return json.Unmarshal(raw, dst)
}

// fmtSscan wraps fmt.Sscanf so callers can type-check at the call site without
// repeating the format. Supports integer and float destinations.
func fmtSscan(s string, dst any) (int, error) {
	switch d := dst.(type) {
	case *float64:
		return fmt.Sscanf(s, "%f", d)
	case *uint64:
		if strings.HasPrefix(s, "0x") || strings.HasPrefix(s, "0X") {
			return fmt.Sscanf(s[2:], "%x", d)
		}
		return fmt.Sscanf(s, "%d", d)
	}
	return 0, fmt.Errorf("unsupported dst type")
}

// contextCtx is a local alias used in signAndSendRaw so the signature stays
// short in long parameter lists.
type contextCtx = context.Context
