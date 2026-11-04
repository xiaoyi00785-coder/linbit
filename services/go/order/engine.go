package main

import (
	"bytes"
	"context"
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"strings"
	"time"
)

// Matching is the replaceable messaging boundary between the order service and
// the matching engine. The local implementation ([HTTPMatching]) speaks HTTP;
// a production deployment could implement the same interface over Kafka (submit
// to a command topic, consume the exec report) without touching call sites.
type Matching interface {
	Submit(ctx context.Context, cmd EngineCommand) (*ExecReport, error)
}

// EngineCommand is the JSON command sent to the engine. Field names/tags match
// the engine's serde contract (see services/rust/matching-engine/src/types.rs).
type EngineCommand struct {
	Cmd           string `json:"cmd"` // "place" | "cancel"
	Symbol        string `json:"symbol"`
	OrderID       string `json:"orderId"`
	UserID        string `json:"userId,omitempty"`
	Side          string `json:"side,omitempty"`
	Type          string `json:"type,omitempty"`
	TimeInForce   string `json:"timeInForce,omitempty"`
	Price         string `json:"price,omitempty"`
	Quantity      string `json:"quantity,omitempty"`
	QuoteOrderQty string `json:"quoteOrderQty,omitempty"`
}

// EngineTrade mirrors the engine's Trade event.
type EngineTrade struct {
	TradeID      string `json:"tradeId"`
	Symbol       string `json:"symbol"`
	Price        string `json:"price"`
	Quantity     string `json:"quantity"`
	TakerOrderID string `json:"takerOrderId"`
	MakerOrderID string `json:"makerOrderId"`
	TakerUserID  string `json:"takerUserId"`
	MakerUserID  string `json:"makerUserId"`
	TakerSide    string `json:"takerSide"`
}

// EngineOrderUpdate mirrors the engine's resting-order update.
type EngineOrderUpdate struct {
	OrderID        string `json:"orderId"`
	UserID         string `json:"userId"`
	Status         string `json:"status"`
	FilledQuantity string `json:"filledQuantity"`
}

// ExecReport is the engine's synchronous response to a command.
type ExecReport struct {
	Symbol         string              `json:"symbol"`
	OrderID        string              `json:"orderId"`
	Status         string              `json:"status"`
	FilledQuantity string              `json:"filledQuantity"`
	Trades         []EngineTrade       `json:"trades"`
	MakerUpdates   []EngineOrderUpdate `json:"makerUpdates"`
	RejectReason   *string             `json:"rejectReason"`
}

// HTTPMatching is the local HTTP implementation of Matching.
type HTTPMatching struct {
	url   string
	token string
	http  *http.Client
}

// NewHTTPMatching constructs an HTTP-backed matching client.
func NewHTTPMatching(url, token string) *HTTPMatching {
	return &HTTPMatching{
		url:   strings.TrimRight(url, "/"),
		token: token,
		http:  &http.Client{Timeout: 5 * time.Second},
	}
}

// Submit posts a command to the engine and decodes the exec report.
func (m *HTTPMatching) Submit(ctx context.Context, cmd EngineCommand) (*ExecReport, error) {
	body, err := json.Marshal(cmd)
	if err != nil {
		return nil, err
	}
	req, err := http.NewRequestWithContext(ctx, http.MethodPost, m.url+"/engine/commands", bytes.NewReader(body))
	if err != nil {
		return nil, err
	}
	req.Header.Set("Content-Type", "application/json")
	if m.token != "" {
		req.Header.Set("X-Internal-Token", m.token)
	}
	resp, err := m.http.Do(req)
	if err != nil {
		return nil, err
	}
	defer resp.Body.Close()
	data, _ := io.ReadAll(resp.Body)
	if resp.StatusCode != http.StatusOK {
		return nil, fmt.Errorf("engine returned %d: %s", resp.StatusCode, string(data))
	}
	var report ExecReport
	if err := json.Unmarshal(data, &report); err != nil {
		return nil, fmt.Errorf("decode exec report: %w", err)
	}
	return &report, nil
}

// BookTop is optionally implemented by a Matching backend that can report the
// best ask of a symbol's book. It prices market buys placed by base quantity.
type BookTop interface {
	BestAsk(ctx context.Context, symbol string) (string, error)
}

// BestAsk reads the engine's depth snapshot and returns the lowest ask price
// ("" when the ask side is empty).
func (m *HTTPMatching) BestAsk(ctx context.Context, symbol string) (string, error) {
	req, err := http.NewRequestWithContext(ctx, http.MethodGet, m.url+"/engine/depth?symbol="+symbol, nil)
	if err != nil {
		return "", err
	}
	if m.token != "" {
		req.Header.Set("X-Internal-Token", m.token)
	}
	resp, err := m.http.Do(req)
	if err != nil {
		return "", err
	}
	defer resp.Body.Close()
	if resp.StatusCode != http.StatusOK {
		return "", fmt.Errorf("engine depth returned %d", resp.StatusCode)
	}
	var snap struct {
		Asks []struct {
			Price string `json:"price"`
		} `json:"asks"`
	}
	if err := json.NewDecoder(resp.Body).Decode(&snap); err != nil {
		return "", err
	}
	if len(snap.Asks) == 0 {
		return "", nil
	}
	return snap.Asks[0].Price, nil
}
