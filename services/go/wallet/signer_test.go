package main

import (
	"errors"
	"testing"

	"github.com/shopspring/decimal"
)

func testSignReq() SignRequest {
	return SignRequest{
		Asset: "USDT", Network: "TRC20",
		FromAddress: "platform-hot-trc20", ToAddress: "TDest0000000001",
		Amount: decimal.RequireFromString("100"), GasFee: decimal.RequireFromString("2"),
		Nonce: "withdraw:1",
	}
}

// TestMultisigTwoOfThree: 2 valid distinct signatures satisfy the 2/3 cold wallet.
func TestMultisigTwoOfThree(t *testing.T) {
	m := NewColdMultisigSigner(nil)
	if m.Threshold() != 2 || m.Parties() != 3 {
		t.Fatalf("cold wallet = %d-of-%d, want 2-of-3", m.Threshold(), m.Parties())
	}
	req := testSignReq()
	s0, _ := m.PartySign(0, req)
	s2, _ := m.PartySign(2, req)

	signed, err := m.Combine(req, []PartySig{s0, s2})
	if err != nil {
		t.Fatalf("2 valid sigs should combine: %v", err)
	}
	if len(signed.Signatures) != 2 {
		t.Fatalf("combined signatures = %d, want 2", len(signed.Signatures))
	}
}

// TestMultisigInsufficient: a single signature cannot authorise the transaction.
func TestMultisigInsufficient(t *testing.T) {
	m := NewColdMultisigSigner(nil)
	req := testSignReq()
	s0, _ := m.PartySign(0, req)
	if _, err := m.Combine(req, []PartySig{s0}); !errors.Is(err, ErrNotEnoughSignatures) {
		t.Fatalf("1 sig err = %v, want ErrNotEnoughSignatures", err)
	}
}

// TestMultisigDuplicatePartyCountsOnce: signing the same party twice must not be
// mistaken for two distinct signers.
func TestMultisigDuplicatePartyCountsOnce(t *testing.T) {
	m := NewColdMultisigSigner(nil)
	req := testSignReq()
	s0, _ := m.PartySign(0, req)
	if _, err := m.Combine(req, []PartySig{s0, s0}); !errors.Is(err, ErrNotEnoughSignatures) {
		t.Fatalf("duplicate party err = %v, want ErrNotEnoughSignatures", err)
	}
}

// TestMultisigInvalidSignatureIgnored: a forged signature does not count, so one
// valid + one forged still fails.
func TestMultisigInvalidSignatureIgnored(t *testing.T) {
	m := NewColdMultisigSigner(nil)
	req := testSignReq()
	s0, _ := m.PartySign(0, req)
	forged := PartySig{Party: 1, Sig: "deadbeef"}
	if _, err := m.Combine(req, []PartySig{s0, forged}); !errors.Is(err, ErrNotEnoughSignatures) {
		t.Fatalf("forged sig err = %v, want ErrNotEnoughSignatures", err)
	}
}

// TestHotMPCSigner is a light sanity check on the hot-wallet mock.
func TestHotMPCSigner(t *testing.T) {
	h := NewHotMPCSigner("secret")
	signed, err := h.Sign(testSignReq())
	if err != nil {
		t.Fatalf("hot sign: %v", err)
	}
	if len(signed.Signatures) != 1 || signed.Raw == "" {
		t.Fatalf("unexpected hot signed tx: %+v", signed)
	}
}
