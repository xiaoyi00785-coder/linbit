package main

// 订单服务与撮合引擎的边界。本地实现是 HTTP；生产可以换成同一接口的 Kafka（命令主题进、成交回报出），调用点不用改。
// 成交回报里的 tradeId 是结算幂等键。

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

// Matching 提交一条命令并同步拿回成交回报。实现可以是 HTTP 或 Kafka，调用方只认这个接口。
type Matching interface {
	Submit(ctx context.Context, cmd EngineCommand) (*ExecReport, error)
}

// EngineCommand 的 json 字段名对齐撮合引擎的 serde（services/rust/matching-engine/src/types.rs）。cmd 是 place 或 cancel。
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

// EngineTrade 对应引擎的 Trade。TradeID 交给资产服务做幂等结算，重放同一 id 不会划第二次。
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

// EngineOrderUpdate 是引擎里仍挂着的 maker 单被这次成交改过的数量和状态。
type EngineOrderUpdate struct {
	OrderID        string `json:"orderId"`
	UserID         string `json:"userId"`
	Status         string `json:"status"`
	FilledQuantity string `json:"filledQuantity"`
}

// ExecReport 是命令的同步回报：taker 状态、成交列表、被碰到的 maker。结算只认这里的 tradeId。
type ExecReport struct {
	Symbol         string              `json:"symbol"`
	OrderID        string              `json:"orderId"`
	Status         string              `json:"status"`
	FilledQuantity string              `json:"filledQuantity"`
	Trades         []EngineTrade       `json:"trades"`
	MakerUpdates   []EngineOrderUpdate `json:"makerUpdates"`
	RejectReason   *string             `json:"rejectReason"`
}

// HTTPMatching 把命令 POST 到引擎。超时或非 2xx 把错误返回给下单，不会在本地假装成交。
type HTTPMatching struct {
	url   string
	token string
	http  *http.Client
}

// NewHTTPMatching 去掉 URL 末尾斜杠。空地址会在 Submit 时失败。
func NewHTTPMatching(url, token string) *HTTPMatching {
	return &HTTPMatching{
		url:   strings.TrimRight(url, "/"),
		token: token,
		http:  &http.Client{Timeout: 5 * time.Second},
	}
}

// Submit 把命令 POST 给引擎并解码成交回报。解码失败不当成空成交。
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

// BookTop 是能报最优卖价的撮合后端。按基础币数量下的市价买单用它算保护价；没实现这个接口就不能这样下。
type BookTop interface {
	BestAsk(ctx context.Context, symbol string) (string, error)
}

// BestAsk 返回深度快照卖盘第一条。空卖盘返回空串，调用方应拒单而不是用 0。
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
