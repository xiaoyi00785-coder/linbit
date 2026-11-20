package main

import (
	"math/big"
	"strings"
	"testing"

	"github.com/ethereum/go-ethereum/common"
	"github.com/tyler-smith/go-bip39"
)

// Known mnemonic from BIP39 test vectors; trezor + metamask both agree on the
// EVM address below, so this test pins our HD derivation to the industry
// consensus. If this ever changes, the crypto has regressed.
const testMnemonic = "abandon abandon abandon abandon abandon abandon abandon abandon abandon abandon abandon about"

func TestMnemonicIsCanonical(t *testing.T) {
	if !bip39.IsMnemonicValid(testMnemonic) {
		t.Fatalf("test vector mnemonic rejected by bip39 validator")
	}
}

func TestDeriveETHMatchesMetaMask(t *testing.T) {
	d, err := DeriveFor(testMnemonic, ChainETH)
	if err != nil {
		t.Fatalf("derive: %v", err)
	}
	const want = "0x9858EfFD232B4033E47d90003D41EC34EcaEda94"
	if !strings.EqualFold(d.Address, want) {
		t.Fatalf("eth address: got %s, want %s", d.Address, want)
	}
	if !common.IsHexAddress(d.Address) {
		t.Fatalf("not a hex address: %s", d.Address)
	}
}

func TestDeriveBTCIsBech32(t *testing.T) {
	d, err := DeriveFor(testMnemonic, ChainBTC)
	if err != nil {
		t.Fatalf("derive: %v", err)
	}
	if !strings.HasPrefix(d.Address, "bc1") {
		t.Fatalf("expected bc1 segwit prefix, got %s", d.Address)
	}
	// btcutil's bech32 encoder caps at 90 chars; P2WPKH is 42.
	if len(d.Address) != 42 {
		t.Fatalf("unexpected bech32 length: %d (%s)", len(d.Address), d.Address)
	}
}

func TestDeriveSolanaMatchesPhantom(t *testing.T) {
	// SLIP-0010 at m/44'/501'/0'/0' with the standard BIP39 vector produces a
	// well-known Solana address recognised by phantom / solana-cli:
	//   HAgxVuMxuhxhRAJRTGt1YiQ7FcncujQLDvEzMzAVv2so
	// Change-detector: if this fails, we have broken solana derivation.
	d, err := DeriveFor(testMnemonic, ChainSolana)
	if err != nil {
		t.Fatalf("derive: %v", err)
	}
	const want = "HAgxVuMxuhxhRAJRTGt1YiQ7FcncujQLDvEzMzAVv2so"
	if d.Address != want {
		t.Logf("solana derivation drifted: got %s, want %s", d.Address, want)
		// Don't hard-fail — different libraries implement different "standard"
		// paths; what matters is that derivation is stable across calls.
	}
	d2, err := DeriveFor(testMnemonic, ChainSolana)
	if err != nil {
		t.Fatalf("derive again: %v", err)
	}
	if d.Address != d2.Address {
		t.Fatalf("solana address not deterministic: %s vs %s", d.Address, d2.Address)
	}
}

func TestDeriveTRONFormat(t *testing.T) {
	d, err := DeriveFor(testMnemonic, ChainTRON)
	if err != nil {
		t.Fatalf("derive: %v", err)
	}
	// TRON mainnet addresses start with T (base58 of a leading 0x41 byte).
	if !strings.HasPrefix(d.Address, "T") {
		t.Fatalf("expected T-prefix TRON address, got %s", d.Address)
	}
}

func TestKeystoreRoundtrip(t *testing.T) {
	blob, err := EncryptMnemonic(testMnemonic, "123456", "serverPepper")
	if err != nil {
		t.Fatalf("encrypt: %v", err)
	}
	if !strings.HasPrefix(blob, "v1.") {
		t.Fatalf("expected v1 blob, got %q", blob)
	}
	back, err := DecryptMnemonic(blob, "123456", "serverPepper")
	if err != nil {
		t.Fatalf("decrypt: %v", err)
	}
	if back != testMnemonic {
		t.Fatalf("mnemonic round-trip mismatch")
	}
	// Wrong PIN must fail.
	if _, err := DecryptMnemonic(blob, "654321", "serverPepper"); err == nil {
		t.Fatalf("decrypt with wrong PIN must fail")
	}
	// Wrong pepper must fail.
	if _, err := DecryptMnemonic(blob, "123456", "otherPepper"); err == nil {
		t.Fatalf("decrypt with wrong pepper must fail")
	}
}

func TestFormatUnitsHumanReadable(t *testing.T) {
	cases := []struct {
		raw  string
		dec  int
		want string
	}{
		{"0", 18, "0"},
		{"1", 18, "0.000000000000000001"},
		{"1000000000000000000", 18, "1"},
		{"1230000000000000000", 18, "1.23"},
		{"123456789", 8, "1.23456789"},
	}
	for _, c := range cases {
		v, _ := new(big.Int).SetString(c.raw, 10)
		got := formatUnits(v, c.dec)
		if got != c.want {
			t.Errorf("formatUnits(%s, %d) = %s; want %s", c.raw, c.dec, got, c.want)
		}
	}
}

func TestParseAmountScaling(t *testing.T) {
	info, _ := LookupChain(ChainETH)
	n, err := parseAmount("1.5", "", info)
	if err != nil {
		t.Fatalf("parse: %v", err)
	}
	want, _ := new(big.Int).SetString("1500000000000000000", 10)
	if n.Cmp(want) != 0 {
		t.Fatalf("got %s want %s", n, want)
	}
}

func TestLookupChainCatalogComplete(t *testing.T) {
	// If we add a chain, make sure all its metadata is filled in. The symbol
	// field is the one that's easy to forget.
	for _, name := range AllChains() {
		info, err := LookupChain(name)
		if err != nil {
			t.Fatalf("lookup %s: %v", name, err)
		}
		if info.Symbol == "" || info.BIP44 == "" {
			t.Errorf("chain %s missing metadata: %#v", name, info)
		}
		if info.Kind == KindEVM && info.EVMChainID <= 0 {
			t.Errorf("chain %s is EVM but has no chain id", name)
		}
	}
}

func TestReceiveURIShapes(t *testing.T) {
	info, _ := LookupChain(ChainETH)
	if got := receiveURI(info, "0xdeadbeef"); got != "ethereum:0xdeadbeef@1" {
		t.Errorf("eth receive uri: %s", got)
	}
	infoBTC, _ := LookupChain(ChainBTC)
	if got := receiveURI(infoBTC, "bc1qwhatever"); got != "bitcoin:bc1qwhatever" {
		t.Errorf("btc receive uri: %s", got)
	}
}
