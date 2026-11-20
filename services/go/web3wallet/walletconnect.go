package main

import (
	"database/sql"
	"encoding/hex"
	"encoding/json"
	"errors"
	"net/http"
	"strings"

	"github.com/btcsuite/btcd/btcec/v2"
	"github.com/ethereum/go-ethereum/common"
	"github.com/ethereum/go-ethereum/core/types"
	"github.com/ethereum/go-ethereum/crypto"
	"github.com/ethereum/go-ethereum/rlp"
	commonlib "github.com/linbit/platform/services/go/common"
)

// WalletConnect v2 is a pairing + signing protocol with a relay the client
// speaks to directly over WebSocket. The server's job is twofold:
//   1. Hand the client the project ID it needs to open a relay session.
//   2. Remember paired dapps so the user can see them across reinstalls, and
//      sign requested messages / transactions on their behalf (since the
//      mnemonic lives server-side).

func (s *Server) handleWCConfig(w http.ResponseWriter, r *http.Request, uid string) {
	commonlib.WriteJSON(w, http.StatusOK, map[string]any{
		"projectId":  s.cfg.WalletConnectProjectID,
		"relayUrl":   "wss://relay.walletconnect.com",
		"available":  s.cfg.WalletConnectProjectID != "",
	})
}

type wcPair struct {
	Topic      string                 `json:"topic"`
	PeerName   string                 `json:"peerName"`
	PeerURL    string                 `json:"peerUrl"`
	PeerIcon   string                 `json:"peerIcon"`
	Namespaces map[string]interface{} `json:"namespaces"`
}

func (s *Server) handleWCPair(w http.ResponseWriter, r *http.Request, uid string) {
	var req wcPair
	if err := commonlib.DecodeJSON(r, &req); err != nil {
		writeErr(w, bad("WEB3_BAD_REQUEST", "请求格式错误"))
		return
	}
	if strings.TrimSpace(req.Topic) == "" {
		writeErr(w, bad("WEB3_TOPIC_REQUIRED", "topic 必填"))
		return
	}
	ns, _ := json.Marshal(req.Namespaces)
	_, err := s.db.ExecContext(r.Context(),
		`INSERT INTO web3_wc_sessions (uid, topic, peer_name, peer_url, peer_icon, namespaces)
		 VALUES ($1,$2,$3,$4,$5,$6)
		 ON CONFLICT (topic) DO UPDATE SET uid=$1, peer_name=$3, peer_url=$4, peer_icon=$5, namespaces=$6, status='active', disconnected_at=NULL`,
		uid, req.Topic, req.PeerName, req.PeerURL, req.PeerIcon, ns)
	if err != nil {
		writeErr(w, err)
		return
	}
	commonlib.WriteJSON(w, http.StatusOK, map[string]any{"ok": true})
}

func (s *Server) handleWCList(w http.ResponseWriter, r *http.Request, uid string) {
	rows, err := s.db.QueryContext(r.Context(),
		`SELECT topic, peer_name, peer_url, peer_icon, namespaces, status, created_at
		 FROM web3_wc_sessions WHERE uid=$1 AND status='active' ORDER BY created_at DESC`, uid)
	if err != nil {
		writeErr(w, err)
		return
	}
	defer rows.Close()
	type row struct {
		Topic      string          `json:"topic"`
		PeerName   string          `json:"peerName"`
		PeerURL    string          `json:"peerUrl"`
		PeerIcon   string          `json:"peerIcon"`
		Namespaces json.RawMessage `json:"namespaces"`
		Status     string          `json:"status"`
		CreatedAt  string          `json:"createdAt"`
	}
	var out []row
	for rows.Next() {
		var r row
		if err := rows.Scan(&r.Topic, &r.PeerName, &r.PeerURL, &r.PeerIcon, &r.Namespaces, &r.Status, &r.CreatedAt); err != nil {
			writeErr(w, err)
			return
		}
		out = append(out, r)
	}
	commonlib.WriteJSON(w, http.StatusOK, map[string]any{"items": out})
}

func (s *Server) handleWCDisconnect(w http.ResponseWriter, r *http.Request, uid string) {
	topic := r.PathValue("topic")
	res, err := s.db.ExecContext(r.Context(),
		`UPDATE web3_wc_sessions SET status='disconnected', disconnected_at=NOW() WHERE uid=$1 AND topic=$2`, uid, topic)
	if err != nil {
		writeErr(w, err)
		return
	}
	if n, _ := res.RowsAffected(); n == 0 {
		writeErr(w, notFound("WEB3_WC_NOT_FOUND", "会话不存在"))
		return
	}
	commonlib.WriteJSON(w, http.StatusOK, map[string]any{"ok": true})
}

// ----------------------------- signing -----------------------------

// signReq covers the three EIP-1193 methods a dapp will route through us:
//   personal_sign           → sign an arbitrary message
//   eth_signTypedData_v4    → sign an EIP-712 typed-data blob
//   eth_sendTransaction     → build + sign + broadcast an EVM tx
// Each method has its own shape; we use a loose map and pull the fields we
// need per branch.
type signReq struct {
	Chain  string          `json:"chain"`
	Method string          `json:"method"`
	Params json.RawMessage `json:"params"`
	Topic  string          `json:"topic"` // WC session this belongs to (for audit)
	PIN    string          `json:"pin"`
}

func (s *Server) handleSign(w http.ResponseWriter, r *http.Request, uid string) {
	var req signReq
	if err := commonlib.DecodeJSON(r, &req); err != nil {
		writeErr(w, bad("WEB3_BAD_REQUEST", "请求格式错误"))
		return
	}
	info, err := LookupChain(req.Chain)
	if err != nil || info.Kind != KindEVM {
		writeErr(w, bad("WEB3_CHAIN_UNSUPPORTED", "sign 目前仅支持 EVM 链"))
		return
	}
	// Audit: refuse signing requests for an unknown topic — defense against a
	// compromised client pushing arbitrary signatures outside a user-paired
	// session. Empty topic is allowed for in-app sign requests (settings
	// screen export flow).
	if req.Topic != "" {
		var one int
		err := s.db.QueryRowContext(r.Context(),
			`SELECT 1 FROM web3_wc_sessions WHERE uid=$1 AND topic=$2 AND status='active'`, uid, req.Topic).Scan(&one)
		if errors.Is(err, sql.ErrNoRows) {
			writeErr(w, notFound("WEB3_WC_NOT_FOUND", "会话不存在或已断开"))
			return
		} else if err != nil {
			writeErr(w, err)
			return
		}
	}

	lk := s.userLks.lock(uid)
	lk.Lock()
	defer lk.Unlock()
	mnemonic, err := s.unlockMnemonic(r.Context(), uid, req.PIN)
	if err != nil {
		writeErr(w, err)
		return
	}
	defer func() { mnemonic = "" }()
	derived, err := DeriveFor(mnemonic, req.Chain)
	if err != nil {
		writeErr(w, err)
		return
	}
	defer zero(derived.privKey)
	eck, _ := btcec.PrivKeyFromBytes(derived.privKey)
	ecdsaPriv := eck.ToECDSA()

	switch req.Method {
	case "personal_sign":
		var params []string
		if err := json.Unmarshal(req.Params, &params); err != nil || len(params) < 1 {
			writeErr(w, bad("WEB3_BAD_PARAMS", "params 需 [message, address]"))
			return
		}
		msg, err := hex.DecodeString(strings.TrimPrefix(params[0], "0x"))
		if err != nil {
			msg = []byte(params[0])
		}
		prefix := []byte("\x19Ethereum Signed Message:\n")
		fullMsg := append(prefix, []byte(itoa(len(msg)))...)
		fullMsg = append(fullMsg, msg...)
		hash := crypto.Keccak256(fullMsg)
		sig, err := crypto.Sign(hash, ecdsaPriv)
		if err != nil {
			writeErr(w, err)
			return
		}
		// Ethereum JSON-RPC expects v in {27,28} for personal_sign.
		sig[64] += 27
		commonlib.WriteJSON(w, http.StatusOK, map[string]any{"signature": "0x" + hex.EncodeToString(sig)})
		return

	case "eth_signTypedData_v4":
		// Full EIP-712 typed-data hashing is substantial code; we follow the
		// minimal spec: callers may also pass a pre-computed 32-byte digest in
		// params[1].digest, which we sign directly. Dapps using WalletConnect
		// v2 virtually always include the structured typed data, but exposing
		// the digest path lets a sophisticated client bypass the limitation.
		var params []json.RawMessage
		if err := json.Unmarshal(req.Params, &params); err != nil || len(params) < 2 {
			writeErr(w, bad("WEB3_BAD_PARAMS", "params 需 [address, typedDataJson]"))
			return
		}
		var wrap struct {
			Digest string `json:"digest"`
		}
		_ = json.Unmarshal(params[1], &wrap)
		if wrap.Digest == "" {
			writeErr(w, unavailable("WEB3_TYPED_DATA_NOT_IMPLEMENTED", "typed-data 结构化签名暂需客户端预先 keccak"))
			return
		}
		digest, err := hex.DecodeString(strings.TrimPrefix(wrap.Digest, "0x"))
		if err != nil || len(digest) != 32 {
			writeErr(w, bad("WEB3_BAD_DIGEST", "digest 需 32 字节 hex"))
			return
		}
		sig, err := crypto.Sign(digest, ecdsaPriv)
		if err != nil {
			writeErr(w, err)
			return
		}
		sig[64] += 27
		commonlib.WriteJSON(w, http.StatusOK, map[string]any{"signature": "0x" + hex.EncodeToString(sig)})
		return

	case "eth_sendTransaction":
		// Dapps send one tx object in params[0]. Reuse the swap-style
		// signAndSendRaw helper after pulling the fields out.
		var params []map[string]string
		if err := json.Unmarshal(req.Params, &params); err != nil || len(params) < 1 {
			writeErr(w, bad("WEB3_BAD_PARAMS", "params 需 [{to,data,value,gas,gasPrice}]"))
			return
		}
		tx := params[0]
		var gas uint64
		if g, ok := tx["gas"]; ok && g != "" {
			gas = parseHexOrDec(g, 0)
		}
		hash, _, err := s.signAndSendRaw(r.Context(), info, derived,
			tx["to"], tx["data"], tx["value"], tx["gasPrice"], gas)
		if err != nil {
			writeErr(w, err)
			return
		}
		_, _ = s.db.ExecContext(r.Context(),
			`INSERT INTO web3_transactions (uid, chain, tx_hash, from_addr, to_addr, value, token, gas, kind, detail)
			 VALUES ($1,$2,$3,$4,$5,$6,'',0,'walletconnect',$7) ON CONFLICT (chain, tx_hash) DO NOTHING`,
			uid, req.Chain, hash, derived.Address, tx["to"], strings.TrimSpace(tx["value"]),
			mustJSON(map[string]any{"topic": req.Topic}))
		commonlib.WriteJSON(w, http.StatusOK, map[string]any{"result": hash, "txHash": hash})
		return
	}

	writeErr(w, bad("WEB3_BAD_METHOD", "不支持的签名方法: "+req.Method))
}

// _ catches the types import so vet doesn't complain when the swap/bridge
// helpers are trimmed in isolation builds.
var _ = types.NewTx
var _ = rlp.EncodeToBytes
var _ = common.HexToAddress
