package chainkit

import (
	"bytes"
	"context"
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"errors"
	"fmt"
	"net/url"
	"strings"

	"github.com/shopspring/decimal"
)

// TronClient talks to a TRON full node HTTP API (e.g. https://nile.trongrid.io).
// Deposit discovery uses TronGrid's v1 account-history API on the same host.
type TronClient struct {
	base    string
	headers map[string]string
	// FeeLimitSun caps energy burn for TRC20 transfers (default 30 TRX).
	FeeLimitSun int64
}

// NewTronClient builds a client; apiKey is optional (TRON-PRO-API-KEY).
func NewTronClient(base, apiKey string) *TronClient {
	h := map[string]string{}
	if apiKey != "" {
		h["TRON-PRO-API-KEY"] = apiKey
	}
	return &TronClient{base: strings.TrimRight(base, "/"), headers: h, FeeLimitSun: 30_000_000}
}

// Chain implements Client.
func (c *TronClient) Chain() Chain { return ChainTron }

func (c *TronClient) post(ctx context.Context, path string, in, out interface{}) error {
	return postJSON(ctx, c.base+path, c.headers, in, out)
}

// Tip implements Client.
func (c *TronClient) Tip(ctx context.Context) (int64, error) {
	var b struct {
		Header struct {
			Raw struct {
				Number int64 `json:"number"`
			} `json:"raw_data"`
		} `json:"block_header"`
	}
	if err := c.post(ctx, "/wallet/getnowblock", map[string]interface{}{}, &b); err != nil {
		return 0, err
	}
	return b.Header.Raw.Number, nil
}

// TransferCost implements Client: a conservative TRX budget (bandwidth for
// TRX, the fee limit for TRC20 energy) used for gas top-ups.
func (c *TronClient) TransferCost(_ context.Context, token *Token) (decimal.Decimal, error) {
	if token == nil {
		return decimal.RequireFromString("1.1"), nil
	}
	return decimal.New(c.FeeLimitSun, -6), nil
}

// NativeBalance implements Client.
func (c *TronClient) NativeBalance(ctx context.Context, addr string) (decimal.Decimal, error) {
	var a struct {
		Balance int64 `json:"balance"`
	}
	if err := c.post(ctx, "/wallet/getaccount", map[string]interface{}{"address": addr, "visible": true}, &a); err != nil {
		return decimal.Zero, err
	}
	return decimal.New(a.Balance, -6), nil
}

func tronAddrParam(addr string) (string, error) {
	b, err := ParseTronAddress(addr)
	if err != nil {
		return "", err
	}
	return hex.EncodeToString(pad32(b[1:])), nil
}

// TokenBalance implements Client.
func (c *TronClient) TokenBalance(ctx context.Context, t Token, addr string) (decimal.Decimal, error) {
	p, err := tronAddrParam(addr)
	if err != nil {
		return decimal.Zero, err
	}
	var r struct {
		Result []string `json:"constant_result"`
	}
	if err := c.post(ctx, "/wallet/triggerconstantcontract", map[string]interface{}{
		"owner_address": addr, "contract_address": t.Contract, "function_selector": "balanceOf(address)",
		"parameter": p, "visible": true}, &r); err != nil {
		return decimal.Zero, err
	}
	if len(r.Result) == 0 {
		return decimal.Zero, errors.New("tron: empty balanceOf result")
	}
	return decimal.NewFromBigInt(hexBig(r.Result[0]), -t.Decimals), nil
}

// tronTx is a node-built unsigned transaction.
type tronTx struct {
	TxID       string          `json:"txID"`
	RawData    json.RawMessage `json:"raw_data"`
	RawDataHex string          `json:"raw_data_hex"`
	Visible    bool            `json:"visible"`
	Signature  []string        `json:"signature,omitempty"`
}

// Send implements Client. The node builds the transaction; we verify that
// txID == sha256(raw_data) before asking the signer.
func (c *TronClient) Send(ctx context.Context, s HotSigner, from KeyRef, to string, token *Token, amount decimal.Decimal) (string, error) {
	if _, err := ParseTronAddress(to); err != nil {
		return "", err
	}
	var tx tronTx
	if token == nil {
		if amount.IsZero() { // sweep: keep 0.3 TRX for bandwidth
			bal, err := c.NativeBalance(ctx, from.Address)
			if err != nil {
				return "", err
			}
			amount = bal.Sub(decimal.RequireFromString("0.3"))
		}
		u, err := toUnits(amount, 6)
		if err != nil {
			return "", err
		}
		var r struct {
			tronTx
			Error string `json:"Error"`
		}
		if err := c.post(ctx, "/wallet/createtransaction", map[string]interface{}{
			"owner_address": from.Address, "to_address": to, "amount": u.IntPart(), "visible": true}, &r); err != nil {
			return "", err
		}
		if r.Error != "" {
			return "", fmt.Errorf("tron: %s", r.Error)
		}
		tx = r.tronTx
	} else {
		u, err := toUnits(amount, token.Decimals)
		if err != nil {
			return "", err
		}
		p, err := tronAddrParam(to)
		if err != nil {
			return "", err
		}
		var r struct {
			Result struct {
				Result  bool   `json:"result"`
				Message string `json:"message"`
			} `json:"result"`
			Transaction tronTx `json:"transaction"`
		}
		if err := c.post(ctx, "/wallet/triggersmartcontract", map[string]interface{}{
			"owner_address": from.Address, "contract_address": token.Contract,
			"function_selector": "transfer(address,uint256)",
			"parameter":         p + hex.EncodeToString(pad32(u.BigInt().Bytes())),
			"fee_limit":         c.FeeLimitSun, "call_value": 0, "visible": true}, &r); err != nil {
			return "", err
		}
		if !r.Result.Result {
			msg, _ := hex.DecodeString(r.Result.Message)
			return "", fmt.Errorf("tron: trigger failed: %s", msg)
		}
		tx = r.Transaction
	}
	raw, err := hex.DecodeString(tx.RawDataHex)
	if err != nil || len(raw) == 0 {
		return "", errors.New("tron: node returned no raw_data_hex")
	}
	id := sha256.Sum256(raw)
	if want, _ := hex.DecodeString(tx.TxID); !bytes.Equal(want, id[:]) {
		return "", errors.New("tron: txID does not match raw_data")
	}
	sig, err := s.Sign(ctx, SignRequest{Chain: ChainTron, Branch: from.Branch, Index: from.Index, Payload: raw})
	if err != nil {
		return "", err
	}
	tx.Signature = []string{hex.EncodeToString(sig)}
	var br struct {
		Result  bool   `json:"result"`
		Code    string `json:"code"`
		Message string `json:"message"`
	}
	if err := c.post(ctx, "/wallet/broadcasttransaction", tx, &br); err != nil {
		return "", err
	}
	if !br.Result {
		msg, _ := hex.DecodeString(br.Message)
		return "", fmt.Errorf("tron: broadcast %s: %s", br.Code, msg)
	}
	return tx.TxID, nil
}

// Status implements Client.
func (c *TronClient) Status(ctx context.Context, txHash string) (TxStatus, error) {
	var r struct {
		ID          string `json:"id"`
		BlockNumber int64  `json:"blockNumber"`
		Result      string `json:"result"`
		Receipt     struct {
			Result string `json:"result"`
		} `json:"receipt"`
	}
	if err := c.post(ctx, "/wallet/gettransactioninfobyid", map[string]string{"value": txHash}, &r); err != nil {
		return TxStatus{}, err
	}
	if r.ID == "" || r.BlockNumber == 0 {
		return TxStatus{}, nil
	}
	ok := r.Result != "FAILED" && (r.Receipt.Result == "" || r.Receipt.Result == "SUCCESS")
	return TxStatus{Found: true, Height: r.BlockNumber, Success: ok}, nil
}

// Incoming implements Client using TronGrid's account history. `from` is
// unused for paging (history is newest-first, capped at 50 per address);
// duplicates are de-duplicated by the caller on (tx, logIndex).
func (c *TronClient) Incoming(ctx context.Context, addrs []string, token *Token, from int64) ([]Incoming, int64, error) {
	tip, err := c.Tip(ctx)
	if err != nil {
		return nil, from, err
	}
	var out []Incoming
	for _, a := range addrs {
		if token != nil {
			var r struct {
				Data []struct {
					TxID  string `json:"transaction_id"`
					To    string `json:"to"`
					Value string `json:"value"`
					Type  string `json:"type"`
				} `json:"data"`
			}
			q := url.Values{"only_to": {"true"}, "limit": {"50"}, "contract_address": {token.Contract}}
			if err := postJSON(ctx, c.base+"/v1/accounts/"+a+"/transactions/trc20?"+q.Encode(), c.headers, nil, &r); err != nil {
				return out, from, err
			}
			for _, d := range r.Data {
				if d.To != a || d.Type != "Transfer" {
					continue
				}
				st, err := c.Status(ctx, d.TxID)
				if err != nil || !st.Found || !st.Success {
					continue
				}
				v, err := decimal.NewFromString(d.Value)
				if err != nil {
					continue
				}
				out = append(out, Incoming{Address: a, Amount: v.Shift(-token.Decimals), TxHash: d.TxID, Height: st.Height})
			}
			continue
		}
		var r struct {
			Data []struct {
				TxID        string `json:"txID"`
				BlockNumber int64  `json:"blockNumber"`
				Ret         []struct {
					ContractRet string `json:"contractRet"`
				} `json:"ret"`
				Raw struct {
					Contract []struct {
						Type      string `json:"type"`
						Parameter struct {
							Value struct {
								Amount    int64  `json:"amount"`
								ToAddress string `json:"to_address"`
							} `json:"value"`
						} `json:"parameter"`
					} `json:"contract"`
				} `json:"raw_data"`
			} `json:"data"`
		}
		q := url.Values{"only_to": {"true"}, "limit": {"50"}}
		if err := postJSON(ctx, c.base+"/v1/accounts/"+a+"/transactions?"+q.Encode(), c.headers, nil, &r); err != nil {
			return out, from, err
		}
		want, _ := ParseTronAddress(a)
		for _, d := range r.Data {
			if len(d.Raw.Contract) != 1 || d.Raw.Contract[0].Type != "TransferContract" ||
				(len(d.Ret) > 0 && d.Ret[0].ContractRet != "SUCCESS") {
				continue
			}
			v := d.Raw.Contract[0].Parameter.Value
			to, _ := hex.DecodeString(v.ToAddress)
			if !bytes.Equal(to, want) || v.Amount <= 0 {
				continue
			}
			out = append(out, Incoming{Address: a, Amount: decimal.New(v.Amount, -6), TxHash: d.TxID, Height: d.BlockNumber})
		}
	}
	return out, tip + 1, nil
}

var _ Client = (*TronClient)(nil)
