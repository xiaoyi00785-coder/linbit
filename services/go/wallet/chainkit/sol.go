package chainkit

import (
	"bytes"
	"context"
	"encoding/base64"
	"encoding/binary"
	"errors"

	"github.com/shopspring/decimal"
)

// SolClient talks to a Solana JSON-RPC node (devnet by default).
type SolClient struct{ rpc jsonRPC }

// NewSolClient builds a client.
func NewSolClient(url string) *SolClient { return &SolClient{rpc: jsonRPC{url: url}} }

// Chain implements Client.
func (c *SolClient) Chain() Chain { return ChainSol }

var (
	solSystemProgram = make([]byte, 32)
	solTokenProgram  = mustSol("TokenkegQfeZyiNwAJbNbGKPFXCWuBvf9Ss623VQ5DA")
)

func mustSol(a string) []byte {
	b, err := ParseSolAddress(a)
	if err != nil {
		panic(err)
	}
	return b
}

const solCommitment = "confirmed"

// Tip implements Client (slot at "confirmed" commitment).
func (c *SolClient) Tip(ctx context.Context) (int64, error) {
	var s int64
	err := c.rpc.call(ctx, "getSlot", []interface{}{map[string]string{"commitment": solCommitment}}, &s)
	return s, err
}

// TransferCost implements Client: one signature fee.
func (c *SolClient) TransferCost(context.Context, *Token) (decimal.Decimal, error) {
	return decimal.New(5000, -9), nil
}

// NativeBalance implements Client.
func (c *SolClient) NativeBalance(ctx context.Context, addr string) (decimal.Decimal, error) {
	var r struct {
		Value int64 `json:"value"`
	}
	if err := c.rpc.call(ctx, "getBalance", []interface{}{addr, map[string]string{"commitment": solCommitment}}, &r); err != nil {
		return decimal.Zero, err
	}
	return decimal.New(r.Value, -9), nil
}

type solTokenAccount struct {
	Pubkey  string
	Amount  decimal.Decimal // base units
	Decimal int32
}

func (c *SolClient) tokenAccounts(ctx context.Context, owner, mint string) ([]solTokenAccount, error) {
	var r struct {
		Value []struct {
			Pubkey  string `json:"pubkey"`
			Account struct {
				Data struct {
					Parsed struct {
						Info struct {
							TokenAmount struct {
								Amount   string `json:"amount"`
								Decimals int32  `json:"decimals"`
							} `json:"tokenAmount"`
						} `json:"info"`
					} `json:"parsed"`
				} `json:"data"`
			} `json:"account"`
		} `json:"value"`
	}
	if err := c.rpc.call(ctx, "getTokenAccountsByOwner", []interface{}{owner, map[string]string{"mint": mint},
		map[string]string{"encoding": "jsonParsed", "commitment": solCommitment}}, &r); err != nil {
		return nil, err
	}
	var out []solTokenAccount
	for _, v := range r.Value {
		ta := v.Account.Data.Parsed.Info.TokenAmount
		amt, _ := decimal.NewFromString(ta.Amount)
		out = append(out, solTokenAccount{Pubkey: v.Pubkey, Amount: amt, Decimal: ta.Decimals})
	}
	return out, nil
}

// TokenBalance implements Client.
func (c *SolClient) TokenBalance(ctx context.Context, t Token, addr string) (decimal.Decimal, error) {
	accts, err := c.tokenAccounts(ctx, addr, t.Contract)
	if err != nil {
		return decimal.Zero, err
	}
	sum := decimal.Zero
	for _, a := range accts {
		sum = sum.Add(a.Amount)
	}
	return sum.Shift(-t.Decimals), nil
}

// ---- message building ----

type solMeta struct {
	key              []byte
	signer, writable bool
}

type solIx struct {
	program  []byte
	accounts []solMeta
	data     []byte
}

func shortVec(n int) []byte {
	var out []byte
	for {
		b := byte(n & 0x7f)
		n >>= 7
		if n == 0 {
			return append(out, b)
		}
		out = append(out, b|0x80)
	}
}

// SolMessage compiles a legacy message. feePayer is the first signer.
func SolMessage(feePayer []byte, blockhash []byte, ixs []solIx) ([]byte, [][]byte, error) {
	var keys []solMeta
	add := func(m solMeta) {
		for i := range keys {
			if bytes.Equal(keys[i].key, m.key) {
				keys[i].signer = keys[i].signer || m.signer
				keys[i].writable = keys[i].writable || m.writable
				return
			}
		}
		keys = append(keys, m)
	}
	add(solMeta{key: feePayer, signer: true, writable: true})
	for _, ix := range ixs {
		for _, a := range ix.accounts {
			add(a)
		}
		add(solMeta{key: ix.program})
	}
	rank := func(m solMeta) int {
		switch {
		case m.signer && m.writable:
			return 0
		case m.signer:
			return 1
		case m.writable:
			return 2
		}
		return 3
	}
	ordered := []solMeta{keys[0]}
	for r := 0; r < 4; r++ {
		for _, k := range keys[1:] {
			if rank(k) == r {
				ordered = append(ordered, k)
			}
		}
	}
	var nSig, nROSig, nROUnsig byte
	var signers [][]byte
	for _, k := range ordered {
		switch rank(k) {
		case 0:
			nSig++
			signers = append(signers, k.key)
		case 1:
			nSig++
			nROSig++
			signers = append(signers, k.key)
		case 3:
			nROUnsig++
		}
	}
	idx := func(key []byte) byte {
		for i, k := range ordered {
			if bytes.Equal(k.key, key) {
				return byte(i)
			}
		}
		panic("sol: key not found")
	}
	if len(ordered) > 255 || len(blockhash) != 32 {
		return nil, nil, errors.New("sol: bad message")
	}
	var b bytes.Buffer
	b.Write([]byte{nSig, nROSig, nROUnsig})
	b.Write(shortVec(len(ordered)))
	for _, k := range ordered {
		b.Write(k.key)
	}
	b.Write(blockhash)
	b.Write(shortVec(len(ixs)))
	for _, ix := range ixs {
		b.WriteByte(idx(ix.program))
		b.Write(shortVec(len(ix.accounts)))
		for _, a := range ix.accounts {
			b.WriteByte(idx(a.key))
		}
		b.Write(shortVec(len(ix.data)))
		b.Write(ix.data)
	}
	return b.Bytes(), signers, nil
}

func solTransferIx(from, to []byte, lamports uint64) solIx {
	data := make([]byte, 12)
	binary.LittleEndian.PutUint32(data, 2)
	binary.LittleEndian.PutUint64(data[4:], lamports)
	return solIx{program: solSystemProgram, data: data,
		accounts: []solMeta{{key: from, signer: true, writable: true}, {key: to, writable: true}}}
}

func solTransferCheckedIx(src, mint, dst, owner []byte, amount uint64, decimals byte) solIx {
	data := make([]byte, 10)
	data[0] = 12
	binary.LittleEndian.PutUint64(data[1:], amount)
	data[9] = decimals
	return solIx{program: solTokenProgram, data: data, accounts: []solMeta{
		{key: src, writable: true}, {key: mint}, {key: dst, writable: true}, {key: owner, signer: true}}}
}

// Send implements Client. amount==0 for native sends the whole balance minus fee.
func (c *SolClient) Send(ctx context.Context, s HotSigner, from KeyRef, to string, token *Token, amount decimal.Decimal) (string, error) {
	return c.SendSponsored(ctx, s, from, from, to, token, amount)
}

// SendSponsored transfers from `owner` while `payer` pays the fee; used for
// sweeps so deposit addresses never need a SOL top-up.
func (c *SolClient) SendSponsored(ctx context.Context, s HotSigner, payer, owner KeyRef, to string, token *Token, amount decimal.Decimal) (string, error) {
	toB, err := ParseSolAddress(to)
	if err != nil {
		return "", err
	}
	ownerB, err := ParseSolAddress(owner.Address)
	if err != nil {
		return "", err
	}
	payerB, err := ParseSolAddress(payer.Address)
	if err != nil {
		return "", err
	}
	var ix solIx
	if token == nil {
		var lamports int64
		if amount.IsZero() {
			bal, err := c.NativeBalance(ctx, owner.Address)
			if err != nil {
				return "", err
			}
			lamports = bal.Shift(9).IntPart()
			if bytes.Equal(payerB, ownerB) {
				lamports -= 5000
			}
		} else {
			u, err := toUnits(amount, 9)
			if err != nil {
				return "", err
			}
			lamports = u.IntPart()
		}
		if lamports <= 0 {
			return "", errors.New("sol: nothing to send")
		}
		ix = solTransferIx(ownerB, toB, uint64(lamports))
	} else {
		u, err := toUnits(amount, token.Decimals)
		if err != nil {
			return "", err
		}
		src, err := c.tokenAccounts(ctx, owner.Address, token.Contract)
		if err != nil {
			return "", err
		}
		if len(src) == 0 {
			return "", errors.New("sol: sender has no token account")
		}
		dst, err := c.tokenAccounts(ctx, to, token.Contract)
		if err != nil {
			return "", err
		}
		if len(dst) == 0 {
			return "", errors.New("sol: recipient has no token account for this mint")
		}
		ix = solTransferCheckedIx(mustSol(src[0].Pubkey), mustSol(token.Contract), mustSol(dst[0].Pubkey), ownerB,
			uint64(u.IntPart()), byte(token.Decimals))
	}
	var bh struct {
		Value struct {
			Blockhash string `json:"blockhash"`
		} `json:"value"`
	}
	if err := c.rpc.call(ctx, "getLatestBlockhash", []interface{}{map[string]string{"commitment": solCommitment}}, &bh); err != nil {
		return "", err
	}
	hash, err := ParseSolAddress(bh.Value.Blockhash)
	if err != nil {
		return "", err
	}
	msg, signers, err := SolMessage(payerB, hash, []solIx{ix})
	if err != nil {
		return "", err
	}
	var wire bytes.Buffer
	wire.Write(shortVec(len(signers)))
	for _, sk := range signers {
		ref := owner
		if bytes.Equal(sk, payerB) {
			ref = payer
		}
		sig, err := s.Sign(ctx, SignRequest{Chain: ChainSol, Branch: ref.Branch, Index: ref.Index, Payload: msg})
		if err != nil {
			return "", err
		}
		if len(sig) != 64 {
			return "", errors.New("sol: bad signature")
		}
		wire.Write(sig)
	}
	wire.Write(msg)
	var sigOut string
	if err := c.rpc.call(ctx, "sendTransaction", []interface{}{base64.StdEncoding.EncodeToString(wire.Bytes()),
		map[string]string{"encoding": "base64", "preflightCommitment": solCommitment}}, &sigOut); err != nil {
		return "", err
	}
	return sigOut, nil
}

// Status implements Client.
func (c *SolClient) Status(ctx context.Context, txHash string) (TxStatus, error) {
	var r struct {
		Value []*struct {
			Slot int64       `json:"slot"`
			Err  interface{} `json:"err"`
		} `json:"value"`
	}
	if err := c.rpc.call(ctx, "getSignatureStatuses", []interface{}{[]string{txHash},
		map[string]bool{"searchTransactionHistory": true}}, &r); err != nil {
		return TxStatus{}, err
	}
	if len(r.Value) == 0 || r.Value[0] == nil {
		return TxStatus{}, nil
	}
	return TxStatus{Found: true, Height: r.Value[0].Slot, Success: r.Value[0].Err == nil}, nil
}

type solParsedTx struct {
	Slot int64 `json:"slot"`
	Meta *struct {
		Err          interface{} `json:"err"`
		PreBalances  []int64     `json:"preBalances"`
		PostBalances []int64     `json:"postBalances"`
		PreToken     []solTokBal `json:"preTokenBalances"`
		PostToken    []solTokBal `json:"postTokenBalances"`
	} `json:"meta"`
	Transaction struct {
		Message struct {
			AccountKeys []struct {
				Pubkey string `json:"pubkey"`
			} `json:"accountKeys"`
		} `json:"message"`
	} `json:"transaction"`
}

type solTokBal struct {
	AccountIndex int    `json:"accountIndex"`
	Mint         string `json:"mint"`
	Owner        string `json:"owner"`
	Amount       struct {
		Amount string `json:"amount"`
	} `json:"uiTokenAmount"`
}

func tokAmount(list []solTokBal, owner, mint string) decimal.Decimal {
	sum := decimal.Zero
	for _, t := range list {
		if t.Owner == owner && t.Mint == mint {
			v, _ := decimal.NewFromString(t.Amount.Amount)
			sum = sum.Add(v)
		}
	}
	return sum
}

// Incoming implements Client (newest 25 signatures per address).
func (c *SolClient) Incoming(ctx context.Context, addrs []string, token *Token, from int64) ([]Incoming, int64, error) {
	tip, err := c.Tip(ctx)
	if err != nil {
		return nil, from, err
	}
	var out []Incoming
	for _, a := range addrs {
		watch := []string{a}
		if token != nil {
			accts, err := c.tokenAccounts(ctx, a, token.Contract)
			if err != nil {
				return out, from, err
			}
			watch = watch[:0]
			for _, t := range accts {
				watch = append(watch, t.Pubkey)
			}
		}
		for _, w := range watch {
			var sigs []struct {
				Signature string      `json:"signature"`
				Slot      int64       `json:"slot"`
				Err       interface{} `json:"err"`
			}
			if err := c.rpc.call(ctx, "getSignaturesForAddress", []interface{}{w,
				map[string]interface{}{"limit": 25, "commitment": solCommitment}}, &sigs); err != nil {
				return out, from, err
			}
			for _, sg := range sigs {
				if sg.Err != nil || sg.Slot < from-100000 {
					continue
				}
				var tx *solParsedTx
				if err := c.rpc.call(ctx, "getTransaction", []interface{}{sg.Signature, map[string]interface{}{
					"encoding": "jsonParsed", "maxSupportedTransactionVersion": 0, "commitment": solCommitment}}, &tx); err != nil {
					return out, from, err
				}
				if tx == nil || tx.Meta == nil || tx.Meta.Err != nil {
					continue
				}
				if token != nil {
					d := tokAmount(tx.Meta.PostToken, a, token.Contract).Sub(tokAmount(tx.Meta.PreToken, a, token.Contract))
					if d.Sign() > 0 {
						out = append(out, Incoming{Address: a, Amount: d.Shift(-token.Decimals), TxHash: sg.Signature, LogIndex: 1, Height: tx.Slot})
					}
					continue
				}
				for i, k := range tx.Transaction.Message.AccountKeys {
					if k.Pubkey == a && i < len(tx.Meta.PostBalances) && i < len(tx.Meta.PreBalances) {
						if d := tx.Meta.PostBalances[i] - tx.Meta.PreBalances[i]; d > 0 {
							out = append(out, Incoming{Address: a, Amount: decimal.New(d, -9), TxHash: sg.Signature, Height: tx.Slot})
						}
					}
				}
			}
		}
	}
	return out, tip + 1, nil
}

// SponsoredSender is implemented by chains whose sweeps can have the hot
// wallet pay the fee (no gas top-up needed).
type SponsoredSender interface {
	SendSponsored(ctx context.Context, s HotSigner, payer, owner KeyRef, to string, token *Token, amount decimal.Decimal) (string, error)
}

var (
	_ Client          = (*SolClient)(nil)
	_ SponsoredSender = (*SolClient)(nil)
)
