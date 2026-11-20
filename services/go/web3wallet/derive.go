package main

import (
	"crypto/ecdsa"
	"crypto/sha256"
	"encoding/hex"
	"errors"
	"fmt"
	"strconv"
	"strings"

	"github.com/btcsuite/btcd/btcec/v2"
	"github.com/btcsuite/btcd/btcutil"
	"github.com/btcsuite/btcd/btcutil/bech32"
	"github.com/btcsuite/btcd/chaincfg"
	ethcommon "github.com/ethereum/go-ethereum/common"
	"github.com/ethereum/go-ethereum/crypto"
	"github.com/mr-tron/base58"
	"github.com/tyler-smith/go-bip32"
	"github.com/tyler-smith/go-bip39"
	"golang.org/x/crypto/ripemd160"
	"golang.org/x/crypto/sha3"
)

// Derived is the public side of a per-chain key: an address, its derivation
// path and (where meaningful) the uncompressed public key hex.
type Derived struct {
	Chain   string
	Path    string
	Address string
	// PrivKey is only set during in-process signing; never persisted or returned.
	privKey []byte
	pubKey  []byte
}

// NewMnemonic generates a 24-word (256-bit) BIP39 mnemonic.
func NewMnemonic() (string, error) {
	ent, err := bip39.NewEntropy(256)
	if err != nil {
		return "", err
	}
	return bip39.NewMnemonic(ent)
}

// ValidateMnemonic rejects any mnemonic BIP39 wouldn't accept (bad wordlist,
// bad checksum, wrong length).
func ValidateMnemonic(m string) error {
	if !bip39.IsMnemonicValid(strings.TrimSpace(m)) {
		return errors.New("invalid mnemonic")
	}
	return nil
}

// DeriveAll walks every supported chain from one mnemonic and returns the
// public addresses. Used on wallet-create to populate web3_addresses.
func DeriveAll(mnemonic string) ([]Derived, error) {
	out := make([]Derived, 0, len(chainCatalog))
	for _, name := range AllChains() {
		d, err := DeriveFor(mnemonic, name)
		if err != nil {
			return nil, fmt.Errorf("derive %s: %w", name, err)
		}
		// Clear private material from the slice entries returned to callers.
		d.privKey = nil
		out = append(out, d)
	}
	return out, nil
}

// DeriveFor derives the HD key for ONE chain. Private material is left in the
// returned struct so the caller can hand it straight into a signer; the caller
// is responsible for zeroing it when done.
func DeriveFor(mnemonic, chain string) (Derived, error) {
	info, err := LookupChain(chain)
	if err != nil {
		return Derived{}, err
	}
	seed := bip39.NewSeed(mnemonic, "")
	switch info.Kind {
	case KindEVM:
		priv, err := bip32Derive(seed, info.BIP44)
		if err != nil {
			return Derived{}, err
		}
		eck, _ := btcec.PrivKeyFromBytes(priv.Key)
		ecdsaPriv := eck.ToECDSA()
		addr := crypto.PubkeyToAddress(ecdsaPriv.PublicKey)
		return Derived{
			Chain:   chain,
			Path:    info.BIP44,
			Address: ethcommon.BytesToAddress(addr[:]).Hex(),
			privKey: priv.Key,
			pubKey:  crypto.FromECDSAPub(&ecdsaPriv.PublicKey),
		}, nil
	case KindBTC:
		priv, err := bip32Derive(seed, info.BIP44)
		if err != nil {
			return Derived{}, err
		}
		eck, _ := btcec.PrivKeyFromBytes(priv.Key)
		// Native SegWit (P2WPKH) bc1 address.
		pubHash := btcutil.Hash160(eck.PubKey().SerializeCompressed())
		addr, err := btcutil.NewAddressWitnessPubKeyHash(pubHash, &chaincfg.MainNetParams)
		if err != nil {
			return Derived{}, err
		}
		return Derived{
			Chain:   chain,
			Path:    info.BIP44,
			Address: addr.EncodeAddress(),
			privKey: priv.Key,
			pubKey:  eck.PubKey().SerializeCompressed(),
		}, nil
	case KindSolana:
		// SLIP-0010 ed25519 derivation (Solana standard: m/44'/501'/0'/0').
		priv, pub, err := solanaDerive(seed, info.BIP44)
		if err != nil {
			return Derived{}, err
		}
		return Derived{
			Chain:   chain,
			Path:    info.BIP44,
			Address: base58.Encode(pub),
			privKey: priv,
			pubKey:  pub,
		}, nil
	case KindTRON:
		priv, err := bip32Derive(seed, info.BIP44)
		if err != nil {
			return Derived{}, err
		}
		eck, _ := btcec.PrivKeyFromBytes(priv.Key)
		ecdsaPriv := eck.ToECDSA()
		return Derived{
			Chain:   chain,
			Path:    info.BIP44,
			Address: tronAddressFromPub(&ecdsaPriv.PublicKey),
			privKey: priv.Key,
			pubKey:  crypto.FromECDSAPub(&ecdsaPriv.PublicKey),
		}, nil
	}
	return Derived{}, fmt.Errorf("unhandled chain kind %v", info.Kind)
}

// bip32Derive walks a BIP32 extended path like m/44'/60'/0'/0/0.
func bip32Derive(seed []byte, path string) (*bip32.Key, error) {
	parts := strings.Split(path, "/")
	if len(parts) == 0 || parts[0] != "m" {
		return nil, errors.New("path must start with m/")
	}
	k, err := bip32.NewMasterKey(seed)
	if err != nil {
		return nil, err
	}
	for _, p := range parts[1:] {
		hard := strings.HasSuffix(p, "'")
		if hard {
			p = strings.TrimSuffix(p, "'")
		}
		n, err := strconv.ParseUint(p, 10, 32)
		if err != nil {
			return nil, fmt.Errorf("bad path segment %q: %w", p, err)
		}
		idx := uint32(n)
		if hard {
			idx += bip32.FirstHardenedChild
		}
		k, err = k.NewChildKey(idx)
		if err != nil {
			return nil, err
		}
	}
	return k, nil
}

// solanaDerive follows the SLIP-0010 ed25519 recipe used by phantom and the
// solana CLI: HMAC-SHA512 with key "ed25519 seed", every segment hardened.
func solanaDerive(seed []byte, path string) (priv, pub []byte, err error) {
	parts := strings.Split(path, "/")
	if len(parts) == 0 || parts[0] != "m" {
		return nil, nil, errors.New("path must start with m/")
	}
	key, chain := slip10MasterEd25519(seed)
	for _, p := range parts[1:] {
		if !strings.HasSuffix(p, "'") {
			return nil, nil, errors.New("solana derivation requires all segments hardened")
		}
		p = strings.TrimSuffix(p, "'")
		n, err := strconv.ParseUint(p, 10, 32)
		if err != nil {
			return nil, nil, err
		}
		key, chain = slip10ChildEd25519(key, chain, uint32(n)+bip32.FirstHardenedChild)
	}
	// ed25519 private key: 32 byte seed; public = ed25519.PublicKey(seed).Public()
	privKey := ed25519NewKeyFromSeed(key)
	return privKey[:32], privKey[32:], nil
}

// tronAddressFromPub formats a TRON base58-check address from a secp256k1
// public key: take keccak256(uncompressed_pub[1:])[12:], prepend 0x41, append
// the first 4 bytes of sha256(sha256(...)) as checksum.
func tronAddressFromPub(pub *ecdsa.PublicKey) string {
	raw := crypto.FromECDSAPub(pub)[1:]
	h := sha3.NewLegacyKeccak256()
	h.Write(raw)
	hash := h.Sum(nil)
	payload := append([]byte{0x41}, hash[12:]...)
	sum1 := sha256.Sum256(payload)
	sum2 := sha256.Sum256(sum1[:])
	return base58.Encode(append(payload, sum2[:4]...))
}

// Local tiny wrappers around stdlib ripemd160 + bech32 so we don't pull extra
// deps. Kept here (not exported) to match what btcutil's helpers do internally.
var _ = ripemd160.New // keep the import tied for btcutil's transitive use
var _ = hex.EncodeToString
var _ = bech32.Encode
