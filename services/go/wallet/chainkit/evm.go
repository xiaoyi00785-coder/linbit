package chainkit

import (
	"context"
	"encoding/hex"
	"errors"
	"fmt"
	"math/big"
	"strings"

	"github.com/shopspring/decimal"
)

// EVMClient talks to an Ethereum-compatible JSON-RPC node (Sepolia, BSC
// testnet, Arbitrum Sepolia, ...). Transactions are legacy EIP-155.
type EVMClient struct {
	rpc     jsonRPC
	ChainID int64
	// MaxScan caps blocks scanned per Incoming call.
	MaxScan int64
}

// NewEVMClient builds a client.
func NewEVMClient(url string, chainID int64) *EVMClient {
	return &EVMClient{rpc: jsonRPC{url: url}, ChainID: chainID, MaxScan: 500}
}

// Chain implements Client.
func (c *EVMClient) Chain() Chain { return ChainEVM }

func hexBig(s string) *big.Int {
	n, _ := new(big.Int).SetString(strings.TrimPrefix(s, "0x"), 16)
	if n == nil {
		return new(big.Int)
	}
	return n
}

func (c *EVMClient) qty(ctx context.Context, method string, params ...interface{}) (*big.Int, error) {
	var s string
	if err := c.rpc.call(ctx, method, params, &s); err != nil {
		return nil, err
	}
	return hexBig(s), nil
}

// ChainIDRemote reads eth_chainId (used to verify configuration).
func (c *EVMClient) ChainIDRemote(ctx context.Context) (int64, error) {
	n, err := c.qty(ctx, "eth_chainId")
	if err != nil {
		return 0, err
	}
	return n.Int64(), nil
}

// Tip implements Client.
func (c *EVMClient) Tip(ctx context.Context) (int64, error) {
	n, err := c.qty(ctx, "eth_blockNumber")
	if err != nil {
		return 0, err
	}
	return n.Int64(), nil
}

const (
	evmGasNative = 21000
	evmGasToken  = 100000
)

func (c *EVMClient) gasPrice(ctx context.Context) (*big.Int, error) {
	p, err := c.qty(ctx, "eth_gasPrice")
	if err != nil {
		return nil, err
	}
	// +25% headroom so the tx is not stuck behind a rising base fee.
	return p.Add(p, new(big.Int).Div(p, big.NewInt(4))), nil
}

// TransferCost implements Client.
func (c *EVMClient) TransferCost(ctx context.Context, token *Token) (decimal.Decimal, error) {
	p, err := c.gasPrice(ctx)
	if err != nil {
		return decimal.Zero, err
	}
	gas := int64(evmGasNative)
	if token != nil {
		gas = evmGasToken
	}
	return decimal.NewFromBigInt(new(big.Int).Mul(p, big.NewInt(gas)), -18), nil
}

// NativeBalance implements Client.
func (c *EVMClient) NativeBalance(ctx context.Context, addr string) (decimal.Decimal, error) {
	n, err := c.qty(ctx, "eth_getBalance", addr, "latest")
	if err != nil {
		return decimal.Zero, err
	}
	return decimal.NewFromBigInt(n, -18), nil
}

func pad32(b []byte) []byte { return append(make([]byte, 32-len(b)), b...) }

// TokenBalance implements Client.
func (c *EVMClient) TokenBalance(ctx context.Context, t Token, addr string) (decimal.Decimal, error) {
	a, err := ParseEVMAddress(addr)
	if err != nil {
		return decimal.Zero, err
	}
	data := "0x70a08231" + hex.EncodeToString(pad32(a))
	n, err := c.qty(ctx, "eth_call", map[string]string{"to": t.Contract, "data": data}, "latest")
	if err != nil {
		return decimal.Zero, err
	}
	return decimal.NewFromBigInt(n, -t.Decimals), nil
}

// ERC20TransferData encodes transfer(address,uint256).
func ERC20TransferData(to []byte, amount *big.Int) []byte {
	out := []byte{0xa9, 0x05, 0x9c, 0xbb}
	out = append(out, pad32(to)...)
	return append(out, pad32(amount.Bytes())...)
}

// EVMTx is a legacy transaction.
type EVMTx struct {
	Nonce    uint64
	GasPrice *big.Int
	Gas      uint64
	To       []byte
	Value    *big.Int
	Data     []byte
}

// SigningPayload is the EIP-155 RLP the signer hashes with keccak256.
func (t EVMTx) SigningPayload(chainID int64) []byte {
	return RLP([]interface{}{t.Nonce, t.GasPrice, t.Gas, t.To, t.Value, t.Data, big.NewInt(chainID), uint64(0), uint64(0)})
}

// Encode returns the signed raw transaction given r||s||recid.
func (t EVMTx) Encode(chainID int64, sig []byte) ([]byte, error) {
	if len(sig) != 65 || sig[64] > 1 {
		return nil, errors.New("evm: bad signature")
	}
	v := new(big.Int).SetInt64(chainID*2 + 35 + int64(sig[64]))
	r := new(big.Int).SetBytes(sig[:32])
	s := new(big.Int).SetBytes(sig[32:64])
	return RLP([]interface{}{t.Nonce, t.GasPrice, t.Gas, t.To, t.Value, t.Data, v, r, s}), nil
}

// Send implements Client.
func (c *EVMClient) Send(ctx context.Context, s HotSigner, from KeyRef, to string, token *Token, amount decimal.Decimal) (string, error) {
	toB, err := ParseEVMAddress(to)
	if err != nil {
		return "", err
	}
	nonce, err := c.qty(ctx, "eth_getTransactionCount", from.Address, "pending")
	if err != nil {
		return "", err
	}
	gp, err := c.gasPrice(ctx)
	if err != nil {
		return "", err
	}
	tx := EVMTx{Nonce: nonce.Uint64(), GasPrice: gp, Value: new(big.Int)}
	if token == nil {
		tx.To, tx.Gas = toB, evmGasNative
		if amount.IsZero() { // sweep: whole balance minus gas
			bal, err := c.qty(ctx, "eth_getBalance", from.Address, "latest")
			if err != nil {
				return "", err
			}
			tx.Value = bal.Sub(bal, new(big.Int).Mul(gp, big.NewInt(evmGasNative)))
			if tx.Value.Sign() <= 0 {
				return "", errors.New("evm: balance does not cover gas")
			}
		} else {
			u, err := toUnits(amount, 18)
			if err != nil {
				return "", err
			}
			tx.Value = u.BigInt()
		}
	} else {
		u, err := toUnits(amount, token.Decimals)
		if err != nil {
			return "", err
		}
		contract, err := ParseEVMAddress(token.Contract)
		if err != nil {
			return "", err
		}
		tx.To, tx.Data, tx.Gas = contract, ERC20TransferData(toB, u.BigInt()), evmGasToken
		if est, err := c.qty(ctx, "eth_estimateGas", map[string]string{
			"from": from.Address, "to": token.Contract, "data": "0x" + hex.EncodeToString(tx.Data)}); err == nil && est.Uint64() > 0 {
			tx.Gas = est.Uint64() * 13 / 10
		}
	}
	sig, err := s.Sign(ctx, SignRequest{Chain: ChainEVM, Branch: from.Branch, Index: from.Index, Payload: tx.SigningPayload(c.ChainID)})
	if err != nil {
		return "", err
	}
	raw, err := tx.Encode(c.ChainID, sig)
	if err != nil {
		return "", err
	}
	var hash string
	if err := c.rpc.call(ctx, "eth_sendRawTransaction", []interface{}{"0x" + hex.EncodeToString(raw)}, &hash); err != nil {
		return "", err
	}
	return hash, nil
}

// Status implements Client.
func (c *EVMClient) Status(ctx context.Context, txHash string) (TxStatus, error) {
	var r *struct {
		BlockNumber string `json:"blockNumber"`
		Status      string `json:"status"`
	}
	if err := c.rpc.call(ctx, "eth_getTransactionReceipt", []interface{}{txHash}, &r); err != nil {
		return TxStatus{}, err
	}
	if r == nil || r.BlockNumber == "" {
		return TxStatus{}, nil
	}
	return TxStatus{Found: true, Height: hexBig(r.BlockNumber).Int64(), Success: r.Status == "0x1"}, nil
}

var erc20TransferTopic = "0xddf252ad1be2c89b69c2b068fc378daa952ba7f163c4a11628f55a4df523b3ef"

// Incoming implements Client.
func (c *EVMClient) Incoming(ctx context.Context, addrs []string, token *Token, from int64) ([]Incoming, int64, error) {
	tip, err := c.Tip(ctx)
	if err != nil {
		return nil, from, err
	}
	if from <= 0 {
		from = tip - 50 // first run: start near the tip
		if from < 0 {
			from = 0
		}
	}
	to := tip
	if to-from+1 > c.MaxScan {
		to = from + c.MaxScan - 1
	}
	if to < from || len(addrs) == 0 {
		return nil, from, nil
	}
	watch := map[string]string{}
	for _, a := range addrs {
		watch[strings.ToLower(a)] = a
	}
	var out []Incoming
	if token != nil {
		var topics []interface{}
		for _, a := range addrs {
			b, err := ParseEVMAddress(a)
			if err != nil {
				continue
			}
			topics = append(topics, "0x"+hex.EncodeToString(pad32(b)))
		}
		var logs []struct {
			TxHash      string   `json:"transactionHash"`
			LogIndex    string   `json:"logIndex"`
			BlockNumber string   `json:"blockNumber"`
			Topics      []string `json:"topics"`
			Data        string   `json:"data"`
			Removed     bool     `json:"removed"`
		}
		filter := map[string]interface{}{
			"fromBlock": fmt.Sprintf("0x%x", from), "toBlock": fmt.Sprintf("0x%x", to),
			"address": token.Contract, "topics": []interface{}{erc20TransferTopic, nil, topics},
		}
		if err := c.rpc.call(ctx, "eth_getLogs", []interface{}{filter}, &logs); err != nil {
			return nil, from, err
		}
		for _, l := range logs {
			if l.Removed || len(l.Topics) < 3 {
				continue
			}
			addr := watch["0x"+strings.ToLower(l.Topics[2][26:])]
			if addr == "" {
				continue
			}
			out = append(out, Incoming{Address: addr, Amount: decimal.NewFromBigInt(hexBig(l.Data), -token.Decimals),
				TxHash: l.TxHash, LogIndex: hexBig(l.LogIndex).Int64(), Height: hexBig(l.BlockNumber).Int64()})
		}
		return out, to + 1, nil
	}
	for h := from; h <= to; h++ {
		var blk *struct {
			Transactions []struct {
				Hash  string  `json:"hash"`
				To    *string `json:"to"`
				Value string  `json:"value"`
			} `json:"transactions"`
		}
		if err := c.rpc.call(ctx, "eth_getBlockByNumber", []interface{}{fmt.Sprintf("0x%x", h), true}, &blk); err != nil {
			return out, h, err
		}
		if blk == nil {
			return out, h, nil
		}
		for _, tx := range blk.Transactions {
			if tx.To == nil {
				continue
			}
			addr := watch[strings.ToLower(*tx.To)]
			v := hexBig(tx.Value)
			if addr == "" || v.Sign() == 0 {
				continue
			}
			out = append(out, Incoming{Address: addr, Amount: decimal.NewFromBigInt(v, -18), TxHash: tx.Hash, Height: h})
		}
	}
	return out, to + 1, nil
}

var _ Client = (*EVMClient)(nil)
