package chainkit

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

	"github.com/shopspring/decimal"
)

// Token is a fungible token contract (ERC20/BEP20/TRC20) or SPL mint.
type Token struct {
	Symbol   string
	Contract string
	Decimals int32
}

// KeyRef names a key held by the HotSigner.
type KeyRef struct {
	Branch, Index uint32
	Address       string
}

// Incoming is one observed transfer into a watched address.
type Incoming struct {
	Address  string
	Amount   decimal.Decimal
	TxHash   string
	LogIndex int64
	Height   int64
}

// TxStatus is the on-chain state of a broadcast transaction.
type TxStatus struct {
	Found   bool
	Height  int64
	Success bool
}

// Client is a real-chain RPC adapter for one network.
type Client interface {
	Chain() Chain
	// Tip returns the current block height (slot for Solana).
	Tip(ctx context.Context) (int64, error)
	// TransferCost estimates the native-coin cost of one transfer.
	TransferCost(ctx context.Context, token *Token) (decimal.Decimal, error)
	NativeBalance(ctx context.Context, addr string) (decimal.Decimal, error)
	TokenBalance(ctx context.Context, token Token, addr string) (decimal.Decimal, error)
	// Send transfers amount (native when token==nil) from `from` to `to`.
	Send(ctx context.Context, s HotSigner, from KeyRef, to string, token *Token, amount decimal.Decimal) (string, error)
	Status(ctx context.Context, txHash string) (TxStatus, error)
	// Incoming lists recent transfers into addrs (native when token==nil)
	// from height `from` on; it returns the height to resume from.
	Incoming(ctx context.Context, addrs []string, token *Token, from int64) ([]Incoming, int64, error)
}

// ---- shared HTTP / JSON-RPC helpers ----

var httpClient = &http.Client{Timeout: 20 * time.Second}

// ErrRPC wraps a node-side error.
var ErrRPC = errors.New("rpc error")

func postJSON(ctx context.Context, url string, headers map[string]string, in, out interface{}) error {
	var body io.Reader
	method := http.MethodGet
	if in != nil {
		b, _ := json.Marshal(in)
		body, method = bytes.NewReader(b), http.MethodPost
	}
	req, err := http.NewRequestWithContext(ctx, method, url, body)
	if err != nil {
		return err
	}
	req.Header.Set("Content-Type", "application/json")
	for k, v := range headers {
		req.Header.Set(k, v)
	}
	resp, err := httpClient.Do(req)
	if err != nil {
		return err
	}
	defer resp.Body.Close()
	b, _ := io.ReadAll(io.LimitReader(resp.Body, 16<<20))
	if resp.StatusCode/100 != 2 {
		return fmt.Errorf("%w: http %d: %.200s", ErrRPC, resp.StatusCode, b)
	}
	if out == nil {
		return nil
	}
	if s, ok := out.(*string); ok {
		*s = string(b)
		return nil
	}
	return json.Unmarshal(b, out)
}

var rpcID int64

type jsonRPC struct {
	url     string
	headers map[string]string
}

func (j jsonRPC) call(ctx context.Context, method string, params []interface{}, out interface{}) error {
	if params == nil {
		params = []interface{}{}
	}
	var resp struct {
		Result json.RawMessage `json:"result"`
		Error  *struct {
			Code    int    `json:"code"`
			Message string `json:"message"`
		} `json:"error"`
	}
	in := map[string]interface{}{"jsonrpc": "2.0", "id": atomic.AddInt64(&rpcID, 1), "method": method, "params": params}
	if err := postJSON(ctx, j.url, j.headers, in, &resp); err != nil {
		return err
	}
	if resp.Error != nil {
		return fmt.Errorf("%w: %s: %d %s", ErrRPC, method, resp.Error.Code, resp.Error.Message)
	}
	if out == nil {
		return nil
	}
	return json.Unmarshal(resp.Result, out)
}

// toUnits converts a decimal amount into integer base units.
func toUnits(d decimal.Decimal, decimals int32) (*decimal.Decimal, error) {
	u := d.Shift(decimals)
	if !u.Equal(u.Truncate(0)) {
		return nil, fmt.Errorf("amount %s has more than %d decimals", d, decimals)
	}
	if u.Sign() <= 0 {
		return nil, errors.New("amount must be positive")
	}
	return &u, nil
}

func fromUnits(u decimal.Decimal, decimals int32) decimal.Decimal { return u.Shift(-decimals) }

// newTextPost POSTs a text/plain body (Esplora /tx) and returns the response text.
func newTextPost(ctx context.Context, url, body string) (string, error) {
	req, err := http.NewRequestWithContext(ctx, http.MethodPost, url, strings.NewReader(body))
	if err != nil {
		return "", err
	}
	req.Header.Set("Content-Type", "text/plain")
	resp, err := httpClient.Do(req)
	if err != nil {
		return "", err
	}
	defer resp.Body.Close()
	b, _ := io.ReadAll(io.LimitReader(resp.Body, 4096))
	if resp.StatusCode/100 != 2 {
		return "", fmt.Errorf("%w: http %d: %.200s", ErrRPC, resp.StatusCode, b)
	}
	return strings.TrimSpace(string(b)), nil
}
