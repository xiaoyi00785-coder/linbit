package chainkit

import (
	"bytes"
	"context"
	"encoding/binary"
	"encoding/hex"
	"errors"
	"fmt"
	"math"
	"strings"

	"github.com/shopspring/decimal"
)

// BTCClient talks to an Esplora REST API (blockstream.info/testnet/api,
// mempool.space/testnet/api, or a self-hosted electrs). Only native segwit
// v0 (P2WPKH/P2WSH) destinations are supported.
type BTCClient struct {
	base string
	HRP  string // "tb" testnet, "bc" mainnet
	// FeeTarget is the confirmation target (blocks) used for fee estimation.
	FeeTarget string
}

// NewBTCClient builds a client.
func NewBTCClient(base, hrp string) *BTCClient {
	return &BTCClient{base: strings.TrimRight(base, "/"), HRP: hrp, FeeTarget: "3"}
}

// Chain implements Client.
func (c *BTCClient) Chain() Chain { return ChainBTC }

func (c *BTCClient) get(ctx context.Context, path string, out interface{}) error {
	return postJSON(ctx, c.base+path, nil, nil, out)
}

// Tip implements Client.
func (c *BTCClient) Tip(ctx context.Context) (int64, error) {
	var h int64
	err := c.get(ctx, "/blocks/tip/height", &h)
	return h, err
}

func (c *BTCClient) feeRate(ctx context.Context) (float64, error) {
	var m map[string]float64
	if err := c.get(ctx, "/fee-estimates", &m); err != nil {
		return 0, err
	}
	r := m[c.FeeTarget]
	if r < 1 {
		r = 1
	}
	return r, nil
}

func p2wpkhVsize(in, out int) int64 { return int64(math.Ceil(10.5 + 68*float64(in) + 31*float64(out))) }

// TransferCost implements Client (1-in/2-out P2WPKH).
func (c *BTCClient) TransferCost(ctx context.Context, _ *Token) (decimal.Decimal, error) {
	r, err := c.feeRate(ctx)
	if err != nil {
		return decimal.Zero, err
	}
	return decimal.New(int64(math.Ceil(r*float64(p2wpkhVsize(1, 2)))), -8), nil
}

type utxo struct {
	TxID   string `json:"txid"`
	Vout   uint32 `json:"vout"`
	Value  int64  `json:"value"`
	Status struct {
		Confirmed bool `json:"confirmed"`
	} `json:"status"`
}

func (c *BTCClient) utxos(ctx context.Context, addr string) ([]utxo, error) {
	var u []utxo
	err := c.get(ctx, "/address/"+addr+"/utxo", &u)
	return u, err
}

// NativeBalance implements Client (confirmed UTXOs only).
func (c *BTCClient) NativeBalance(ctx context.Context, addr string) (decimal.Decimal, error) {
	u, err := c.utxos(ctx, addr)
	if err != nil {
		return decimal.Zero, err
	}
	var s int64
	for _, x := range u {
		if x.Status.Confirmed {
			s += x.Value
		}
	}
	return decimal.New(s, -8), nil
}

// TokenBalance implements Client.
func (c *BTCClient) TokenBalance(context.Context, Token, string) (decimal.Decimal, error) {
	return decimal.Zero, errors.New("btc: no tokens")
}

// BTCInput is a P2WPKH outpoint being spent.
type BTCInput struct {
	TxID   string // big-endian hex as shown by explorers
	Vout   uint32
	Value  int64
	PubKey []byte // 33-byte compressed key owning the output
}

// BTCOutput is a payment.
type BTCOutput struct {
	Script []byte
	Value  int64
}

// BTCScript returns the scriptPubKey for a segwit v0 address.
func BTCScript(hrp, addr string) ([]byte, error) {
	prog, err := SegwitDecode(hrp, addr)
	if err != nil {
		return nil, err
	}
	return append([]byte{0x00, byte(len(prog))}, prog...), nil
}

func le32(v uint32) []byte { b := make([]byte, 4); binary.LittleEndian.PutUint32(b, v); return b }
func le64(v int64) []byte {
	b := make([]byte, 8)
	binary.LittleEndian.PutUint64(b, uint64(v))
	return b
}

func varInt(n int) []byte {
	switch {
	case n < 0xfd:
		return []byte{byte(n)}
	case n <= 0xffff:
		b := []byte{0xfd, 0, 0}
		binary.LittleEndian.PutUint16(b[1:], uint16(n))
		return b
	}
	b := []byte{0xfe, 0, 0, 0, 0}
	binary.LittleEndian.PutUint32(b[1:], uint32(n))
	return b
}

func outpoint(in BTCInput) ([]byte, error) {
	h, err := hex.DecodeString(in.TxID)
	if err != nil || len(h) != 32 {
		return nil, errors.New("btc: bad txid")
	}
	for i, j := 0, 31; i < j; i, j = i+1, j-1 {
		h[i], h[j] = h[j], h[i]
	}
	return append(h, le32(in.Vout)...), nil
}

const btcSequence = 0xfffffffd // opt-in RBF

// BIP143Preimage builds the segwit v0 SIGHASH_ALL preimage for input i.
func BIP143Preimage(ins []BTCInput, outs []BTCOutput, i int) ([]byte, error) {
	var prev, seq, outsB bytes.Buffer
	for _, in := range ins {
		op, err := outpoint(in)
		if err != nil {
			return nil, err
		}
		prev.Write(op)
		seq.Write(le32(btcSequence))
	}
	for _, o := range outs {
		outsB.Write(le64(o.Value))
		outsB.Write(varInt(len(o.Script)))
		outsB.Write(o.Script)
	}
	op, _ := outpoint(ins[i])
	var b bytes.Buffer
	b.Write(le32(2))
	b.Write(dsha256(prev.Bytes()))
	b.Write(dsha256(seq.Bytes()))
	b.Write(op)
	b.Write(append([]byte{0x19, 0x76, 0xa9, 0x14}, append(Hash160(ins[i].PubKey), 0x88, 0xac)...))
	b.Write(le64(ins[i].Value))
	b.Write(le32(btcSequence))
	b.Write(dsha256(outsB.Bytes()))
	b.Write(le32(0))
	b.Write(le32(1)) // SIGHASH_ALL
	return b.Bytes(), nil
}

// SerializeBTC builds the final segwit transaction; sigs are DER (no hashtype byte).
func SerializeBTC(ins []BTCInput, outs []BTCOutput, sigs [][]byte) ([]byte, error) {
	var b bytes.Buffer
	b.Write(le32(2))
	b.Write([]byte{0x00, 0x01})
	b.Write(varInt(len(ins)))
	for _, in := range ins {
		op, err := outpoint(in)
		if err != nil {
			return nil, err
		}
		b.Write(op)
		b.WriteByte(0)
		b.Write(le32(btcSequence))
	}
	b.Write(varInt(len(outs)))
	for _, o := range outs {
		b.Write(le64(o.Value))
		b.Write(varInt(len(o.Script)))
		b.Write(o.Script)
	}
	for i, in := range ins {
		sig := append(append([]byte{}, sigs[i]...), 0x01)
		b.WriteByte(2)
		b.Write(varInt(len(sig)))
		b.Write(sig)
		b.Write(varInt(len(in.PubKey)))
		b.Write(in.PubKey)
	}
	b.Write(le32(0))
	return b.Bytes(), nil
}

const btcDust = 546

// Send implements Client. amount==0 sweeps every confirmed UTXO (minus fee).
// Change returns to the sending address.
func (c *BTCClient) Send(ctx context.Context, s HotSigner, from KeyRef, to string, token *Token, amount decimal.Decimal) (string, error) {
	if token != nil {
		return "", errors.New("btc: no tokens")
	}
	dest, err := BTCScript(c.HRP, to)
	if err != nil {
		return "", err
	}
	pub, err := s.PublicKey(ctx, ChainBTC, from.Branch, from.Index)
	if err != nil {
		return "", err
	}
	if a, _ := BTCP2WPKH(pub, c.HRP); a != from.Address {
		return "", errors.New("btc: signer key does not match from address")
	}
	change, _ := BTCScript(c.HRP, from.Address)
	us, err := c.utxos(ctx, from.Address)
	if err != nil {
		return "", err
	}
	rate, err := c.feeRate(ctx)
	if err != nil {
		return "", err
	}
	sweep := amount.IsZero()
	var want int64
	if !sweep {
		u, err := toUnits(amount, 8)
		if err != nil {
			return "", err
		}
		want = u.IntPart()
	}
	var ins []BTCInput
	var total int64
	for _, u := range us {
		if !u.Status.Confirmed {
			continue
		}
		ins = append(ins, BTCInput{TxID: u.TxID, Vout: u.Vout, Value: u.Value, PubKey: pub})
		total += u.Value
		if !sweep && total >= want+int64(math.Ceil(rate*float64(p2wpkhVsize(len(ins), 2)))) {
			break
		}
	}
	if len(ins) == 0 {
		return "", errors.New("btc: no confirmed utxos")
	}
	var outs []BTCOutput
	if sweep {
		fee := int64(math.Ceil(rate * float64(p2wpkhVsize(len(ins), 1))))
		if total-fee < btcDust {
			return "", errors.New("btc: balance below dust after fee")
		}
		outs = []BTCOutput{{Script: dest, Value: total - fee}}
	} else {
		fee := int64(math.Ceil(rate * float64(p2wpkhVsize(len(ins), 2))))
		if total < want+fee {
			return "", fmt.Errorf("btc: insufficient funds (%d < %d)", total, want+fee)
		}
		outs = []BTCOutput{{Script: dest, Value: want}}
		if rest := total - want - fee; rest >= btcDust {
			outs = append(outs, BTCOutput{Script: change, Value: rest})
		}
	}
	sigs := make([][]byte, len(ins))
	for i := range ins {
		pre, err := BIP143Preimage(ins, outs, i)
		if err != nil {
			return "", err
		}
		if sigs[i], err = s.Sign(ctx, SignRequest{Chain: ChainBTC, Branch: from.Branch, Index: from.Index, Payload: pre}); err != nil {
			return "", err
		}
	}
	raw, err := SerializeBTC(ins, outs, sigs)
	if err != nil {
		return "", err
	}
	req, err := newTextPost(ctx, c.base+"/tx", hex.EncodeToString(raw))
	if err != nil {
		return "", err
	}
	return req, nil
}

// Status implements Client.
func (c *BTCClient) Status(ctx context.Context, txHash string) (TxStatus, error) {
	var st struct {
		Confirmed   bool  `json:"confirmed"`
		BlockHeight int64 `json:"block_height"`
	}
	if err := c.get(ctx, "/tx/"+txHash+"/status", &st); err != nil {
		if strings.Contains(err.Error(), "http 404") {
			return TxStatus{}, nil
		}
		return TxStatus{}, err
	}
	if !st.Confirmed {
		return TxStatus{}, nil
	}
	return TxStatus{Found: true, Height: st.BlockHeight, Success: true}, nil
}

// Incoming implements Client (the newest ~25 confirmed txs per address).
func (c *BTCClient) Incoming(ctx context.Context, addrs []string, _ *Token, from int64) ([]Incoming, int64, error) {
	tip, err := c.Tip(ctx)
	if err != nil {
		return nil, from, err
	}
	var out []Incoming
	for _, a := range addrs {
		var txs []struct {
			TxID string `json:"txid"`
			Vout []struct {
				Address string `json:"scriptpubkey_address"`
				Value   int64  `json:"value"`
			} `json:"vout"`
			Status struct {
				Confirmed   bool  `json:"confirmed"`
				BlockHeight int64 `json:"block_height"`
			} `json:"status"`
		}
		if err := c.get(ctx, "/address/"+a+"/txs", &txs); err != nil {
			return out, from, err
		}
		for _, t := range txs {
			if !t.Status.Confirmed {
				continue
			}
			for i, o := range t.Vout {
				if o.Address == a && o.Value > 0 {
					out = append(out, Incoming{Address: a, Amount: decimal.New(o.Value, -8), TxHash: t.TxID, LogIndex: int64(i), Height: t.Status.BlockHeight})
				}
			}
		}
	}
	return out, tip + 1, nil
}

var _ Client = (*BTCClient)(nil)
