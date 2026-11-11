package common

// httpx.go：统一 JSON 响应。错误体对齐 docs/api/openapi.yaml 的 Error。

import (
	"crypto/rand"
	"encoding/hex"
	"encoding/json"
	"net/http"
)

// APIError 是对外错误体，字段对齐 openapi 的 Error。
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

// WriteError 写出 APIError，并带一个生成的 request id。
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

// DecodeJSON 严格解码 JSON 请求体。未知字段会失败。
func DecodeJSON(r *http.Request, v interface{}) error {
	dec := json.NewDecoder(r.Body)
	dec.DisallowUnknownFields()
	return dec.Decode(v)
}
