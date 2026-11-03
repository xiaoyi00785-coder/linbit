// Package chainkit holds the real-chain building blocks for the wallet:
// BIP32 public derivation (deposit addresses come from an xpub only),
// address encoders, an encrypted local keystore, the HotSigner interface,
// minimal RPC clients for TRON / EVM / BTC (esplora) / Solana, and the
// transaction builders those chains need. No key material is ever logged.
package chainkit

import (
	"bytes"
	"crypto/sha256"
	"errors"
	"math/big"
	"strings"
)

const b58Alphabet = "123456789ABCDEFGHJKLMNPQRSTUVWXYZabcdefghijkmnopqrstuvwxyz"

var b58Index = func() [256]int {
	var t [256]int
	for i := range t {
		t[i] = -1
	}
	for i, c := range b58Alphabet {
		t[c] = i
	}
	return t
}()

// Base58Encode encodes bytes with the Bitcoin alphabet.
func Base58Encode(b []byte) string {
	x := new(big.Int).SetBytes(b)
	mod, base := new(big.Int), big.NewInt(58)
	var out []byte
	for x.Sign() > 0 {
		x.DivMod(x, base, mod)
		out = append(out, b58Alphabet[mod.Int64()])
	}
	for _, c := range b {
		if c != 0 {
			break
		}
		out = append(out, '1')
	}
	for i, j := 0, len(out)-1; i < j; i, j = i+1, j-1 {
		out[i], out[j] = out[j], out[i]
	}
	return string(out)
}

// Base58Decode decodes a Bitcoin-alphabet string.
func Base58Decode(s string) ([]byte, error) {
	x := new(big.Int)
	base := big.NewInt(58)
	for i := 0; i < len(s); i++ {
		v := b58Index[s[i]]
		if v < 0 {
			return nil, errors.New("base58: invalid character")
		}
		x.Mul(x, base)
		x.Add(x, big.NewInt(int64(v)))
	}
	out := x.Bytes()
	zeros := 0
	for zeros < len(s) && s[zeros] == '1' {
		zeros++
	}
	return append(make([]byte, zeros), out...), nil
}

func dsha256(b []byte) []byte {
	a := sha256.Sum256(b)
	c := sha256.Sum256(a[:])
	return c[:]
}

// Base58CheckEncode appends a 4-byte double-SHA256 checksum.
func Base58CheckEncode(payload []byte) string {
	return Base58Encode(append(append([]byte{}, payload...), dsha256(payload)[:4]...))
}

// Base58CheckDecode verifies and strips the checksum.
func Base58CheckDecode(s string) ([]byte, error) {
	b, err := Base58Decode(s)
	if err != nil {
		return nil, err
	}
	if len(b) < 5 {
		return nil, errors.New("base58check: too short")
	}
	p, sum := b[:len(b)-4], b[len(b)-4:]
	if !bytes.Equal(dsha256(p)[:4], sum) {
		return nil, errors.New("base58check: bad checksum")
	}
	return p, nil
}

// ---- bech32 (BIP173, segwit v0) ----

const bech32Charset = "qpzry9x8gf2tvdw0s3jn54khce6mua7l"

func bech32Polymod(values []byte) uint32 {
	gen := []uint32{0x3b6a57b2, 0x26508e6d, 0x1ea119fa, 0x3d4233dd, 0x2a1462b3}
	chk := uint32(1)
	for _, v := range values {
		b := chk >> 25
		chk = (chk&0x1ffffff)<<5 ^ uint32(v)
		for i := 0; i < 5; i++ {
			if (b>>uint(i))&1 == 1 {
				chk ^= gen[i]
			}
		}
	}
	return chk
}

func bech32HrpExpand(hrp string) []byte {
	var out []byte
	for i := 0; i < len(hrp); i++ {
		out = append(out, hrp[i]>>5)
	}
	out = append(out, 0)
	for i := 0; i < len(hrp); i++ {
		out = append(out, hrp[i]&31)
	}
	return out
}

func convertBits(data []byte, from, to uint, pad bool) ([]byte, error) {
	acc, bits := uint32(0), uint(0)
	maxv := uint32(1)<<to - 1
	var out []byte
	for _, v := range data {
		if uint32(v)>>from != 0 {
			return nil, errors.New("convertbits: invalid value")
		}
		acc = acc<<from | uint32(v)
		bits += from
		for bits >= to {
			bits -= to
			out = append(out, byte(acc>>bits&maxv))
		}
	}
	if pad {
		if bits > 0 {
			out = append(out, byte(acc<<(to-bits)&maxv))
		}
	} else if bits >= from || (acc<<(to-bits))&maxv != 0 {
		return nil, errors.New("convertbits: invalid padding")
	}
	return out, nil
}

// SegwitEncode encodes a segwit v0 program (e.g. P2WPKH hash160) for hrp.
func SegwitEncode(hrp string, version byte, program []byte) (string, error) {
	conv, err := convertBits(program, 8, 5, true)
	if err != nil {
		return "", err
	}
	data := append([]byte{version}, conv...)
	values := append(bech32HrpExpand(hrp), data...)
	pm := bech32Polymod(append(values, 0, 0, 0, 0, 0, 0)) ^ 1
	var sb strings.Builder
	sb.WriteString(hrp)
	sb.WriteByte('1')
	for _, d := range data {
		sb.WriteByte(bech32Charset[d])
	}
	for i := 0; i < 6; i++ {
		sb.WriteByte(bech32Charset[(pm>>uint(5*(5-i)))&31])
	}
	return sb.String(), nil
}

// SegwitDecode decodes a segwit v0 address, returning the witness program.
func SegwitDecode(hrp, addr string) ([]byte, error) {
	addr = strings.ToLower(addr)
	pos := strings.LastIndexByte(addr, '1')
	if pos < 1 || addr[:pos] != hrp || len(addr)-pos-1 < 7 {
		return nil, errors.New("bech32: bad hrp or length")
	}
	var data []byte
	for i := pos + 1; i < len(addr); i++ {
		d := strings.IndexByte(bech32Charset, addr[i])
		if d < 0 {
			return nil, errors.New("bech32: invalid character")
		}
		data = append(data, byte(d))
	}
	if bech32Polymod(append(bech32HrpExpand(hrp), data...)) != 1 {
		return nil, errors.New("bech32: bad checksum")
	}
	data = data[:len(data)-6]
	if len(data) < 1 || data[0] != 0 {
		return nil, errors.New("bech32: only segwit v0 supported")
	}
	prog, err := convertBits(data[1:], 5, 8, false)
	if err != nil {
		return nil, err
	}
	if len(prog) != 20 && len(prog) != 32 {
		return nil, errors.New("bech32: bad program length")
	}
	return prog, nil
}

// ---- RLP (enough for legacy EVM transactions) ----

// RLP encodes a nested structure of []byte, *big.Int, uint64 and []interface{}.
func RLP(v interface{}) []byte {
	switch x := v.(type) {
	case []byte:
		if len(x) == 1 && x[0] < 0x80 {
			return []byte{x[0]}
		}
		return append(rlpHeader(0x80, len(x)), x...)
	case *big.Int:
		if x == nil || x.Sign() == 0 {
			return []byte{0x80}
		}
		return RLP(x.Bytes())
	case uint64:
		return RLP(new(big.Int).SetUint64(x))
	case []interface{}:
		var body []byte
		for _, e := range x {
			body = append(body, RLP(e)...)
		}
		return append(rlpHeader(0xc0, len(body)), body...)
	}
	panic("rlp: unsupported type")
}

func rlpHeader(base byte, n int) []byte {
	if n < 56 {
		return []byte{base + byte(n)}
	}
	l := big.NewInt(int64(n)).Bytes()
	return append([]byte{base + 55 + byte(len(l))}, l...)
}
