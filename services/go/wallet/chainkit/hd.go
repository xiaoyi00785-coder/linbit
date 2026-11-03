package chainkit

import (
	"bytes"
	"crypto/hmac"
	"crypto/sha256"
	"crypto/sha512"
	"encoding/binary"
	"errors"
	"fmt"

	"github.com/decred/dcrd/dcrec/secp256k1/v4"
	"golang.org/x/crypto/ripemd160" //nolint:staticcheck // required by hash160
)

// BIP32 version bytes.
var (
	verXpub = []byte{0x04, 0x88, 0xB2, 0x1E}
	verXprv = []byte{0x04, 0x88, 0xAD, 0xE4}
	verTpub = []byte{0x04, 0x35, 0x87, 0xCF}
	verTprv = []byte{0x04, 0x35, 0x83, 0x94}
)

// HardenedOffset marks a hardened BIP32 index.
const HardenedOffset = uint32(0x80000000)

// ExtKey is a BIP32 extended key. priv is nil for a public-only key.
type ExtKey struct {
	priv      []byte // 32 bytes or nil
	pub       []byte // 33-byte compressed
	chainCode []byte
	depth     byte
	parentFP  []byte
	index     uint32
	testnet   bool
}

// IsPrivate reports whether the key holds a private component.
func (k *ExtKey) IsPrivate() bool { return k.priv != nil }

// PubKey returns the compressed public key.
func (k *ExtKey) PubKey() []byte { return append([]byte{}, k.pub...) }

// Hash160 is RIPEMD160(SHA256(b)).
func Hash160(b []byte) []byte {
	s := sha256.Sum256(b)
	r := ripemd160.New()
	r.Write(s[:])
	return r.Sum(nil)
}

// NewMaster derives the BIP32 master key from a seed.
func NewMaster(seed []byte, testnet bool) (*ExtKey, error) {
	if len(seed) < 16 || len(seed) > 64 {
		return nil, errors.New("hd: seed must be 16..64 bytes")
	}
	m := hmac.New(sha512.New, []byte("Bitcoin seed"))
	m.Write(seed)
	I := m.Sum(nil)
	priv := I[:32]
	var sk secp256k1.ModNScalar
	if overflow := sk.SetByteSlice(priv); overflow || sk.IsZero() {
		return nil, errors.New("hd: invalid master key")
	}
	return &ExtKey{priv: priv, pub: secp256k1.PrivKeyFromBytes(priv).PubKey().SerializeCompressed(),
		chainCode: I[32:], parentFP: []byte{0, 0, 0, 0}, testnet: testnet}, nil
}

// Child derives child i (hardened if i >= HardenedOffset; private keys only).
func (k *ExtKey) Child(i uint32) (*ExtKey, error) {
	if k.depth == 255 {
		return nil, errors.New("hd: depth limit")
	}
	var data []byte
	if i >= HardenedOffset {
		if k.priv == nil {
			return nil, errors.New("hd: hardened derivation from a public key")
		}
		data = append([]byte{0}, k.priv...)
	} else {
		data = append([]byte{}, k.pub...)
	}
	var ib [4]byte
	binary.BigEndian.PutUint32(ib[:], i)
	data = append(data, ib[:]...)
	m := hmac.New(sha512.New, k.chainCode)
	m.Write(data)
	I := m.Sum(nil)
	var il secp256k1.ModNScalar
	if overflow := il.SetByteSlice(I[:32]); overflow {
		return nil, errors.New("hd: invalid child, use next index")
	}
	child := &ExtKey{chainCode: I[32:], depth: k.depth + 1, parentFP: Hash160(k.pub)[:4], index: i, testnet: k.testnet}
	if k.priv != nil {
		var kp secp256k1.ModNScalar
		kp.SetByteSlice(k.priv)
		kp.Add(&il)
		if kp.IsZero() {
			return nil, errors.New("hd: invalid child, use next index")
		}
		b := kp.Bytes()
		child.priv = b[:]
		child.pub = secp256k1.PrivKeyFromBytes(child.priv).PubKey().SerializeCompressed()
		return child, nil
	}
	parent, err := secp256k1.ParsePubKey(k.pub)
	if err != nil {
		return nil, err
	}
	var pj, ij, sum secp256k1.JacobianPoint
	parent.AsJacobian(&pj)
	secp256k1.ScalarBaseMultNonConst(&il, &ij)
	secp256k1.AddNonConst(&ij, &pj, &sum)
	if (sum.X.IsZero() && sum.Y.IsZero()) || sum.Z.IsZero() {
		return nil, errors.New("hd: invalid child, use next index")
	}
	sum.ToAffine()
	child.pub = secp256k1.NewPublicKey(&sum.X, &sum.Y).SerializeCompressed()
	return child, nil
}

// Derive walks a path of indices.
func (k *ExtKey) Derive(path ...uint32) (*ExtKey, error) {
	cur := k
	for _, i := range path {
		var err error
		if cur, err = cur.Child(i); err != nil {
			return nil, err
		}
	}
	return cur, nil
}

// Neuter returns the public-only version of the key.
func (k *ExtKey) Neuter() *ExtKey {
	c := *k
	c.priv = nil
	return &c
}

// String serialises the key (xpub/tpub, or xprv/tprv when private).
// Callers must never log the private form.
func (k *ExtKey) String() string {
	var ver []byte
	switch {
	case k.priv != nil && k.testnet:
		ver = verTprv
	case k.priv != nil:
		ver = verXprv
	case k.testnet:
		ver = verTpub
	default:
		ver = verXpub
	}
	var b bytes.Buffer
	b.Write(ver)
	b.WriteByte(k.depth)
	b.Write(k.parentFP)
	var ib [4]byte
	binary.BigEndian.PutUint32(ib[:], k.index)
	b.Write(ib[:])
	b.Write(k.chainCode)
	if k.priv != nil {
		b.WriteByte(0)
		b.Write(k.priv)
	} else {
		b.Write(k.pub)
	}
	return Base58CheckEncode(b.Bytes())
}

// ParseExtKey parses an xpub/tpub (or xprv/tprv).
func ParseExtKey(s string) (*ExtKey, error) {
	b, err := Base58CheckDecode(s)
	if err != nil {
		return nil, err
	}
	if len(b) != 78 {
		return nil, fmt.Errorf("hd: bad extended key length %d", len(b))
	}
	ver := b[:4]
	k := &ExtKey{depth: b[4], parentFP: b[5:9], index: binary.BigEndian.Uint32(b[9:13]), chainCode: b[13:45]}
	key := b[45:78]
	switch {
	case bytes.Equal(ver, verXpub), bytes.Equal(ver, verTpub):
		if _, err := secp256k1.ParsePubKey(key); err != nil {
			return nil, err
		}
		k.pub = key
	case bytes.Equal(ver, verXprv), bytes.Equal(ver, verTprv):
		if key[0] != 0 {
			return nil, errors.New("hd: bad private key prefix")
		}
		k.priv = key[1:]
		k.pub = secp256k1.PrivKeyFromBytes(k.priv).PubKey().SerializeCompressed()
	default:
		return nil, errors.New("hd: unsupported version bytes")
	}
	k.testnet = bytes.Equal(ver, verTpub) || bytes.Equal(ver, verTprv)
	return k, nil
}
