package common

import (
	"crypto/rand"
	"encoding/hex"
	"encoding/json"
	"net/http"
)

// APIError is the unified error model from docs/api/openapi.yaml (schema Error).
type APIError struct {
	Code      string `json:"code"`
	Message   string `json:"message"`
	RequestID string `json:"requestId,omitempty"`
}

// WriteJSON serialises v as JSON with the given status code.
func WriteJSON(w http.ResponseWriter, status int, v interface{}) {
	w.Header().Set("Content-Type", "application/json; charset=utf-8")
	w.WriteHeader(status)
	if v != nil {
		_ = json.NewEncoder(w).Encode(v)
	}
}

// WriteError emits an APIError envelope with a generated request id.
func WriteError(w http.ResponseWriter, status int, code, message string) {
	WriteJSON(w, status, APIError{Code: code, Message: message, RequestID: RequestID()})
}

// RequestID returns a short random correlation id (req_xxxx).
func RequestID() string {
	b := make([]byte, 6)
	if _, err := rand.Read(b); err != nil {
		return "req_000000"
	}
	return "req_" + hex.EncodeToString(b)
}

// DecodeJSON reads and strictly decodes a JSON request body into v.
func DecodeJSON(r *http.Request, v interface{}) error {
	dec := json.NewDecoder(r.Body)
	dec.DisallowUnknownFields()
	return dec.Decode(v)
}
