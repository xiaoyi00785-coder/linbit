package main

import (
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"net/http"
	"strings"
	"sync/atomic"
	"time"
)

// jsonrpc2Req is the request shape for EVM / Solana / TRON JSON-RPC endpoints.
type jsonrpc2Req struct {
	Jsonrpc string        `json:"jsonrpc"`
	ID      int64         `json:"id"`
	Method  string        `json:"method"`
	Params  []interface{} `json:"params"`
}

type jsonrpc2Resp struct {
	Jsonrpc string          `json:"jsonrpc"`
	ID      int64           `json:"id"`
	Result  json.RawMessage `json:"result"`
	Error   *struct {
		Code    int    `json:"code"`
		Message string `json:"message"`
	} `json:"error,omitempty"`
}

var rpcSeq int64

// rpcCall posts one JSON-RPC2 call to the given chain's configured RPC
// endpoint, falling through to each fallback on transport error. A non-nil
// RPC `error` field is returned to the caller as an error — upstream reverts
// (insufficient funds, nonce too low) surface as this.
func (s *Server) rpcCall(ctx context.Context, chain, method string, params []interface{}) (json.RawMessage, error) {
	rpc, ok := s.cfg.RPCs[chain]
	if !ok || rpc.Primary == "" {
		return nil, unavailable("WEB3_RPC_NOT_CONFIGURED", "该链 RPC 未配置")
	}
	id := atomic.AddInt64(&rpcSeq, 1)
	body, err := json.Marshal(jsonrpc2Req{Jsonrpc: "2.0", ID: id, Method: method, Params: params})
	if err != nil {
		return nil, err
	}
	var lastErr error
	for _, url := range rpc.Endpoints() {
		raw, err := s.rpcPost(ctx, url, body)
		if err != nil {
			lastErr = err
			continue
		}
		var out jsonrpc2Resp
		if err := json.Unmarshal(raw, &out); err != nil {
			lastErr = err
			continue
		}
		if out.Error != nil {
			// Semantic error (reverted call, bad nonce). Don't try fallbacks —
			// they'd return the same answer. Surface to caller as-is.
			return nil, fmt.Errorf("rpc %s: %s (%d)", method, out.Error.Message, out.Error.Code)
		}
		return out.Result, nil
	}
	if lastErr == nil {
		lastErr = errors.New("no rpc endpoints configured")
	}
	return nil, unavailable("WEB3_RPC_FAILED", "RPC 节点不可用: "+lastErr.Error())
}

func (s *Server) rpcPost(ctx context.Context, url string, body []byte) ([]byte, error) {
	req, err := http.NewRequestWithContext(ctx, http.MethodPost, url, bytes.NewReader(body))
	if err != nil {
		return nil, err
	}
	req.Header.Set("Content-Type", "application/json")
	req.Header.Set("Accept", "application/json")
	resp, err := s.http.Do(req)
	if err != nil {
		return nil, err
	}
	defer resp.Body.Close()
	if resp.StatusCode/100 != 2 {
		b, _ := io.ReadAll(io.LimitReader(resp.Body, 1024))
		return nil, fmt.Errorf("http %d: %s", resp.StatusCode, strings.TrimSpace(string(b)))
	}
	return io.ReadAll(io.LimitReader(resp.Body, 1<<20))
}

// httpGetJSON is the simpler helper used by the explorer / NFT / swap / bridge
// REST calls below. It carries context, handles non-2xx uniformly and caps the
// body to 2 MiB.
func (s *Server) httpGetJSON(ctx context.Context, url string, headers map[string]string, out interface{}) error {
	req, err := http.NewRequestWithContext(ctx, http.MethodGet, url, nil)
	if err != nil {
		return err
	}
	req.Header.Set("Accept", "application/json")
	for k, v := range headers {
		req.Header.Set(k, v)
	}
	resp, err := s.http.Do(req)
	if err != nil {
		return err
	}
	defer resp.Body.Close()
	body, err := io.ReadAll(io.LimitReader(resp.Body, 2<<20))
	if err != nil {
		return err
	}
	if resp.StatusCode/100 != 2 {
		return fmt.Errorf("http %d: %s", resp.StatusCode, strings.TrimSpace(string(body)))
	}
	if out == nil {
		return nil
	}
	return json.Unmarshal(body, out)
}

func (s *Server) httpPostJSON(ctx context.Context, url string, headers map[string]string, in, out interface{}) error {
	body, err := json.Marshal(in)
	if err != nil {
		return err
	}
	req, err := http.NewRequestWithContext(ctx, http.MethodPost, url, bytes.NewReader(body))
	if err != nil {
		return err
	}
	req.Header.Set("Content-Type", "application/json")
	req.Header.Set("Accept", "application/json")
	for k, v := range headers {
		req.Header.Set(k, v)
	}
	resp, err := s.http.Do(req)
	if err != nil {
		return err
	}
	defer resp.Body.Close()
	respBody, err := io.ReadAll(io.LimitReader(resp.Body, 2<<20))
	if err != nil {
		return err
	}
	if resp.StatusCode/100 != 2 {
		return fmt.Errorf("http %d: %s", resp.StatusCode, strings.TrimSpace(string(respBody)))
	}
	if out == nil {
		return nil
	}
	return json.Unmarshal(respBody, out)
}

// shortCircuitTimeout wraps the parent context with a per-call deadline so a
// slow external API can't pin a handler forever.
func shortCircuitTimeout(ctx context.Context, d time.Duration) (context.Context, context.CancelFunc) {
	if _, ok := ctx.Deadline(); ok {
		return ctx, func() {}
	}
	return context.WithTimeout(ctx, d)
}
