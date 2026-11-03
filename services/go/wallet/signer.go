package main

import (
	"crypto/sha256"
	"encoding/hex"
	"errors"
	"fmt"
	"sort"
	"strings"

	"github.com/shopspring/decimal"
)

// signer.go is the pluggable signing layer. Real deployments back these with a
// hot-wallet MPC cluster and a cold-wallet 2/3 multisig; here only the interface
// and deterministic mock implementations exist — no real private keys are held
// and nothing is broadcast to a real chain.

// SignRequest is an unsigned withdrawal transaction.
type SignRequest struct {
	Asset       string
	Network     string
	FromAddress string
	ToAddress   string
	Amount      decimal.Decimal
	GasFee      decimal.Decimal
	Nonce       string // unique per withdrawal (the withdrawal id)
}

// SignedTx is the opaque result of signing: an (unbroadcast) raw transaction and
// the signatures that authorised it.
type SignedTx struct {
	Raw        string   `json:"raw"`
	Signatures []string `json:"signatures"`
}

// Signer authorises a withdrawal transaction.
type Signer interface {
	// Kind identifies the signer ("hot-mpc", "cold-2of3", ...).
	Kind() string
	// Sign returns a signed transaction or an error if authorisation fails.
	Sign(req SignRequest) (SignedTx, error)
}

// signingMessage is the canonical byte string every party signs. Mock only.
func signingMessage(req SignRequest) string {
	return strings.Join([]string{
		req.Network, req.Asset, req.FromAddress, req.ToAddress,
		req.Amount.String(), req.GasFee.String(), req.Nonce,
	}, "|")
}

// partySignature is a deterministic mock signature: H(secret | message). A real
// implementation would use the party's private key.
func partySignature(secret, message string) string {
	sum := sha256.Sum256([]byte(secret + "\x00" + message))
	return hex.EncodeToString(sum[:])
}

// ---- hot wallet (MPC) mock ----

// HotMPCSigner mocks an MPC hot wallet: the threshold signing happens inside the
// cluster, so from the caller's perspective it yields a single signature.
type HotMPCSigner struct {
	secret string
}

// NewHotMPCSigner builds a hot-wallet signer with the given cluster secret.
func NewHotMPCSigner(secret string) *HotMPCSigner {
	if secret == "" {
		secret = "hot-mpc-mock-secret"
	}
	return &HotMPCSigner{secret: secret}
}

// Kind implements Signer.
func (s *HotMPCSigner) Kind() string { return "hot-mpc" }

// Sign implements Signer.
func (s *HotMPCSigner) Sign(req SignRequest) (SignedTx, error) {
	if req.ToAddress == "" || req.FromAddress == "" || req.Nonce == "" {
		return SignedTx{}, errors.New("sign: incomplete request")
	}
	msg := signingMessage(req)
	sig := partySignature(s.secret, msg)
	return SignedTx{
		Raw:        fmt.Sprintf("hot:%s:%s", req.Nonce, sig[:16]),
		Signatures: []string{sig},
	}, nil
}

// ---- cold wallet 2/3 multisig mock ----

// ErrNotEnoughSignatures is returned when fewer than the threshold of distinct
// valid party signatures are supplied.
var ErrNotEnoughSignatures = errors.New("not enough valid signatures to meet multisig threshold")

// PartySig is one cosigner's contribution to a multisig transaction.
type PartySig struct {
	Party int    // party index (0-based)
	Sig   string // signature produced by PartySign
}

// MultisigSigner mocks an m-of-n cold wallet. The built-in cold wallet is 2/3.
type MultisigSigner struct {
	secrets   []string // one per party (n parties)
	threshold int
}

// NewColdMultisigSigner builds a 2/3 cold-wallet signer (threshold 2, 3 parties).
func NewColdMultisigSigner(secrets []string) *MultisigSigner {
	if len(secrets) != 3 {
		secrets = []string{"cold-party-0", "cold-party-1", "cold-party-2"}
	}
	return &MultisigSigner{secrets: secrets, threshold: 2}
}

// Kind implements Signer.
func (m *MultisigSigner) Kind() string {
	return fmt.Sprintf("cold-%dof%d", m.threshold, len(m.secrets))
}

// Parties returns the number of cosigners.
func (m *MultisigSigner) Parties() int { return len(m.secrets) }

// Threshold returns the minimum number of distinct valid signatures required.
func (m *MultisigSigner) Threshold() int { return m.threshold }

// PartySign produces party i's signature over req. In production each party signs
// on its own isolated device; here it is deterministic for testing.
func (m *MultisigSigner) PartySign(party int, req SignRequest) (PartySig, error) {
	if party < 0 || party >= len(m.secrets) {
		return PartySig{}, fmt.Errorf("party %d out of range", party)
	}
	return PartySig{Party: party, Sig: partySignature(m.secrets[party], signingMessage(req))}, nil
}

// Combine verifies the collected signatures and, if at least `threshold` distinct
// parties produced a valid signature, returns the signed transaction. Duplicate
// parties count once; invalid signatures are ignored.
func (m *MultisigSigner) Combine(req SignRequest, sigs []PartySig) (SignedTx, error) {
	msg := signingMessage(req)
	valid := map[int]string{}
	for _, ps := range sigs {
		if ps.Party < 0 || ps.Party >= len(m.secrets) {
			continue
		}
		if ps.Sig == partySignature(m.secrets[ps.Party], msg) {
			valid[ps.Party] = ps.Sig
		}
	}
	if len(valid) < m.threshold {
		return SignedTx{}, ErrNotEnoughSignatures
	}
	parties := make([]int, 0, len(valid))
	for p := range valid {
		parties = append(parties, p)
	}
	sort.Ints(parties)
	out := make([]string, 0, len(parties))
	for _, p := range parties {
		out = append(out, valid[p])
	}
	return SignedTx{
		Raw:        fmt.Sprintf("cold:%s:%dof%d", req.Nonce, len(valid), len(m.secrets)),
		Signatures: out,
	}, nil
}

// Sign implements Signer by internally collecting signatures from the first
// `threshold` parties (mock orchestration of the cold-wallet signing ceremony).
func (m *MultisigSigner) Sign(req SignRequest) (SignedTx, error) {
	sigs := make([]PartySig, 0, m.threshold)
	for i := 0; i < m.threshold; i++ {
		ps, err := m.PartySign(i, req)
		if err != nil {
			return SignedTx{}, err
		}
		sigs = append(sigs, ps)
	}
	return m.Combine(req, sigs)
}
