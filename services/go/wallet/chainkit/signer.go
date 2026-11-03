package chainkit

import (
	"bytes"
	"context"
	"crypto/ed25519"
	"crypto/sha256"
	"crypto/subtle"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"net/http"
	"time"

	"github.com/decred/dcrd/dcrec/secp256k1/v4"
	"github.com/decred/dcrd/dcrec/secp256k1/v4/ecdsa"
)

// SignRequest asks the hot signer to sign a chain-specific payload with the
// key at (Branch, Index). The signer hashes the payload itself, so a future
// standalone signer can decode and policy-check the transaction before
// signing:
//   - evm:  payload = EIP-155 unsigned RLP; digest keccak256;     sig r||s||recid
//   - tron: payload = raw_data bytes;       digest sha256;        sig r||s||(27+recid)
//   - btc:  payload = BIP143 preimage;      digest double-sha256; sig DER
//   - sol:  payload = message bytes;        ed25519 signature
type SignRequest struct {
	Chain   Chain  `json:"chain"`
	Branch  uint32 `json:"branch"`
	Index   uint32 `json:"index"`
	Payload []byte `json:"payload"`
}

// HotSigner is the hot-wallet signing boundary. Today it runs as a separate
// container next to the wallet (RemoteHotSigner over HTTP); the same interface
// can move to a dedicated host or an MPC/HSM implementation later.
type HotSigner interface {
	// PublicKey returns the public key at (chain, branch, index):
	// 33-byte compressed secp256k1, or 32-byte ed25519 for sol.
	PublicKey(ctx context.Context, c Chain, branch, index uint32) ([]byte, error)
	Sign(ctx context.Context, req SignRequest) ([]byte, error)
}

// LocalHotSigner signs with an unlocked encrypted keystore in-process.
type LocalHotSigner struct{ ks *Keystore }

// NewLocalHotSigner wraps an unlocked keystore.
func NewLocalHotSigner(ks *Keystore) *LocalHotSigner { return &LocalHotSigner{ks: ks} }

// PublicKey implements HotSigner.
func (s *LocalHotSigner) PublicKey(_ context.Context, c Chain, branch, index uint32) ([]byte, error) {
	if c == ChainSol {
		k, err := s.ks.solKey(branch, index)
		if err != nil {
			return nil, err
		}
		return []byte(k.Public().(ed25519.PublicKey)), nil
	}
	k, err := s.ks.secpKey(c, branch, index)
	if err != nil {
		return nil, err
	}
	return k.PubKey(), nil
}

// Sign implements HotSigner.
func (s *LocalHotSigner) Sign(_ context.Context, req SignRequest) ([]byte, error) {
	if len(req.Payload) == 0 {
		return nil, errors.New("signer: empty payload")
	}
	if req.Chain == ChainSol {
		k, err := s.ks.solKey(req.Branch, req.Index)
		if err != nil {
			return nil, err
		}
		return ed25519.Sign(k, req.Payload), nil
	}
	ek, err := s.ks.secpKey(req.Chain, req.Branch, req.Index)
	if err != nil {
		return nil, err
	}
	priv := secp256k1.PrivKeyFromBytes(ek.priv)
	defer priv.Zero()
	switch req.Chain {
	case ChainEVM:
		return compactToRSV(ecdsa.SignCompact(priv, Keccak256(req.Payload), false), 0), nil
	case ChainTron:
		h := sha256.Sum256(req.Payload)
		return compactToRSV(ecdsa.SignCompact(priv, h[:], false), 27), nil
	case ChainBTC:
		return ecdsa.Sign(priv, dsha256(req.Payload)).Serialize(), nil
	}
	return nil, fmt.Errorf("signer: unsupported chain %q", req.Chain)
}

// compactToRSV turns decred's [27+recid]||r||s into r||s||(base+recid).
func compactToRSV(c []byte, base byte) []byte {
	out := make([]byte, 65)
	copy(out, c[1:])
	out[64] = c[0] - 27 + base
	return out
}

// ---- HTTP transport (wallet <-> signer container) ----

// SignerHandler exposes a HotSigner over HTTP, guarded by a bearer token.
func SignerHandler(s HotSigner, token string) http.Handler {
	mux := http.NewServeMux()
	auth := func(h http.HandlerFunc) http.HandlerFunc {
		return func(w http.ResponseWriter, r *http.Request) {
			got := r.Header.Get("Authorization")
			if token == "" || subtle.ConstantTimeCompare([]byte(got), []byte("Bearer "+token)) != 1 {
				http.Error(w, `{"error":"unauthorized"}`, http.StatusUnauthorized)
				return
			}
			h(w, r)
		}
	}
	mux.HandleFunc("GET /healthz", func(w http.ResponseWriter, _ *http.Request) { w.Write([]byte("ok")) })
	mux.HandleFunc("POST /v1/pubkey", auth(func(w http.ResponseWriter, r *http.Request) {
		var req SignRequest
		if err := json.NewDecoder(io.LimitReader(r.Body, 1<<20)).Decode(&req); err != nil {
			http.Error(w, `{"error":"bad request"}`, http.StatusBadRequest)
			return
		}
		pk, err := s.PublicKey(r.Context(), req.Chain, req.Branch, req.Index)
		writeSignerResult(w, pk, err)
	}))
	mux.HandleFunc("POST /v1/sign", auth(func(w http.ResponseWriter, r *http.Request) {
		var req SignRequest
		if err := json.NewDecoder(io.LimitReader(r.Body, 1<<20)).Decode(&req); err != nil {
			http.Error(w, `{"error":"bad request"}`, http.StatusBadRequest)
			return
		}
		sig, err := s.Sign(r.Context(), req)
		writeSignerResult(w, sig, err)
	}))
	return mux
}

func writeSignerResult(w http.ResponseWriter, b []byte, err error) {
	w.Header().Set("Content-Type", "application/json")
	if err != nil {
		w.WriteHeader(http.StatusUnprocessableEntity)
		json.NewEncoder(w).Encode(map[string]string{"error": err.Error()})
		return
	}
	json.NewEncoder(w).Encode(map[string][]byte{"result": b})
}

// RemoteHotSigner is the wallet-side client of SignerHandler.
type RemoteHotSigner struct {
	base, token string
	http        *http.Client
}

// NewRemoteHotSigner builds a client for a signer at base (e.g. http://wallet-signer:8090).
func NewRemoteHotSigner(base, token string) *RemoteHotSigner {
	return &RemoteHotSigner{base: base, token: token, http: &http.Client{Timeout: 15 * time.Second}}
}

func (r *RemoteHotSigner) call(ctx context.Context, path string, req SignRequest) ([]byte, error) {
	body, _ := json.Marshal(req)
	hr, err := http.NewRequestWithContext(ctx, http.MethodPost, r.base+path, bytes.NewReader(body))
	if err != nil {
		return nil, err
	}
	hr.Header.Set("Authorization", "Bearer "+r.token)
	hr.Header.Set("Content-Type", "application/json")
	resp, err := r.http.Do(hr)
	if err != nil {
		return nil, fmt.Errorf("signer: %w", err)
	}
	defer resp.Body.Close()
	var out struct {
		Result []byte `json:"result"`
		Error  string `json:"error"`
	}
	if err := json.NewDecoder(io.LimitReader(resp.Body, 1<<20)).Decode(&out); err != nil {
		return nil, fmt.Errorf("signer: status %d", resp.StatusCode)
	}
	if resp.StatusCode != http.StatusOK {
		return nil, fmt.Errorf("signer: %s", out.Error)
	}
	return out.Result, nil
}

// PublicKey implements HotSigner.
func (r *RemoteHotSigner) PublicKey(ctx context.Context, c Chain, branch, index uint32) ([]byte, error) {
	return r.call(ctx, "/v1/pubkey", SignRequest{Chain: c, Branch: branch, Index: index})
}

// Sign implements HotSigner.
func (r *RemoteHotSigner) Sign(ctx context.Context, req SignRequest) ([]byte, error) {
	return r.call(ctx, "/v1/sign", req)
}
