package chainkit

import (
	"crypto/ed25519"
	"encoding/hex"
	"errors"
	"strings"

	"github.com/decred/dcrd/dcrec/secp256k1/v4"
	"golang.org/x/crypto/sha3"
)

// Chain identifies a chain family.
type Chain string

const (
	ChainTron Chain = "tron"
	ChainEVM  Chain = "evm"
	ChainBTC  Chain = "btc"
	ChainSol  Chain = "sol"
)

// Keccak256 is the legacy Keccak hash used by EVM chains.
func Keccak256(b ...[]byte) []byte {
	h := sha3.NewLegacyKeccak256()
	for _, x := range b {
		h.Write(x)
	}
	return h.Sum(nil)
}

func evmAddrBytes(pub []byte) ([]byte, error) {
	pk, err := secp256k1.ParsePubKey(pub)
	if err != nil {
		return nil, err
	}
	return Keccak256(pk.SerializeUncompressed()[1:])[12:], nil
}

// EVMAddress returns the EIP-55 checksummed address for a secp256k1 pubkey.
func EVMAddress(pub []byte) (string, error) {
	b, err := evmAddrBytes(pub)
	if err != nil {
		return "", err
	}
	return EVMChecksum(hex.EncodeToString(b)), nil
}

// EVMChecksum applies EIP-55 casing to a 40-hex-char address (with or without 0x).
func EVMChecksum(h string) string {
	h = strings.ToLower(strings.TrimPrefix(h, "0x"))
	sum := hex.EncodeToString(Keccak256([]byte(h)))
	out := []byte(h)
	for i := range out {
		if out[i] >= 'a' && sum[i] >= '8' {
			out[i] -= 32
		}
	}
	return "0x" + string(out)
}

// ParseEVMAddress validates a 0x address (EIP-55 checked when mixed case).
func ParseEVMAddress(a string) ([]byte, error) {
	if len(a) != 42 || !strings.HasPrefix(a, "0x") {
		return nil, errors.New("evm: bad address")
	}
	b, err := hex.DecodeString(a[2:])
	if err != nil {
		return nil, errors.New("evm: bad address")
	}
	body := a[2:]
	if body != strings.ToLower(body) && body != strings.ToUpper(body) && EVMChecksum(a) != a {
		return nil, errors.New("evm: bad checksum")
	}
	return b, nil
}

// TronAddress returns the base58 T... address for a secp256k1 pubkey.
func TronAddress(pub []byte) (string, error) {
	b, err := evmAddrBytes(pub)
	if err != nil {
		return "", err
	}
	return Base58CheckEncode(append([]byte{0x41}, b...)), nil
}

// ParseTronAddress returns the 21-byte (0x41-prefixed) address.
func ParseTronAddress(a string) ([]byte, error) {
	b, err := Base58CheckDecode(a)
	if err != nil || len(b) != 21 || b[0] != 0x41 {
		return nil, errors.New("tron: bad address")
	}
	return b, nil
}

// BTCP2WPKH returns the native segwit address for a compressed pubkey.
// hrp is "tb" (testnet) or "bc" (mainnet).
func BTCP2WPKH(pub []byte, hrp string) (string, error) {
	if len(pub) != 33 {
		return "", errors.New("btc: need compressed pubkey")
	}
	return SegwitEncode(hrp, 0, Hash160(pub))
}

// SolAddress is the base58 ed25519 public key.
func SolAddress(pub ed25519.PublicKey) string { return Base58Encode(pub) }

// ParseSolAddress decodes a 32-byte base58 Solana address.
func ParseSolAddress(a string) ([]byte, error) {
	b, err := Base58Decode(a)
	if err != nil || len(b) != 32 {
		return nil, errors.New("sol: bad address")
	}
	return b, nil
}

// AddressFromPub renders a secp256k1 pubkey for a chain.
func AddressFromPub(c Chain, pub []byte, btcHRP string) (string, error) {
	switch c {
	case ChainTron:
		return TronAddress(pub)
	case ChainEVM:
		return EVMAddress(pub)
	case ChainBTC:
		return BTCP2WPKH(pub, btcHRP)
	case ChainSol:
		if len(pub) != 32 {
			return "", errors.New("chainkit: sol pubkey must be 32 bytes")
		}
		return SolAddress(pub), nil
	}
	return "", errors.New("chainkit: unknown chain")
}

// ValidateAddress checks a user-supplied destination address for a chain.
func ValidateAddress(c Chain, a, btcHRP string) error {
	var err error
	switch c {
	case ChainTron:
		_, err = ParseTronAddress(a)
	case ChainEVM:
		_, err = ParseEVMAddress(a)
	case ChainBTC:
		_, err = SegwitDecode(btcHRP, a)
	case ChainSol:
		_, err = ParseSolAddress(a)
	default:
		err = errors.New("unknown chain")
	}
	return err
}
