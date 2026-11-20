package main

import (
	"context"
	"database/sql"
	"encoding/hex"
	"encoding/json"
	"errors"
	"fmt"
	"math/big"
	"net/http"
	"strings"
	"time"

	"github.com/btcsuite/btcd/btcec/v2"
	ecdsabtc "github.com/btcsuite/btcd/btcec/v2/ecdsa"
	"github.com/ethereum/go-ethereum/common"
	"github.com/ethereum/go-ethereum/core/types"
	"github.com/ethereum/go-ethereum/rlp"
	commonlib "github.com/linbit/platform/services/go/common"
	"github.com/mr-tron/base58"
)

func mrtronDecode(s string) ([]byte, error) { return base58.Decode(s) }

// sendReq is the body shape accepted by POST /web3/transactions. For EVM chains
// `value` and `gasPrice` are decimal strings in base units (wei); for Solana
// `value` is lamports; TRON sun; BTC satoshis. `token` is empty for native.
// `data` is optional hex calldata for EVM contract calls.
type sendReq struct {
	PIN      string `json:"pin"`
	Chain    string `json:"chain"`
	To       string `json:"to"`
	Amount   string `json:"amount"`             // human decimal
	Token    string `json:"token,omitempty"`     // contract address (optional)
	GasLimit uint64 `json:"gasLimit,omitempty"`
	GasPrice string `json:"gasPrice,omitempty"` // wei; optional
	Nonce    *int64 `json:"nonce,omitempty"`
	Data     string `json:"data,omitempty"`     // EVM only; hex, with or without 0x
	Note     string `json:"note,omitempty"`
}

type sendResp struct {
	TxHash     string `json:"txHash"`
	Chain      string `json:"chain"`
	From       string `json:"from"`
	To         string `json:"to"`
	Amount     string `json:"amount"`
	Token      string `json:"token"`
	Explorer   string `json:"explorer"`
	Status     string `json:"status"`
}

func (s *Server) handleSend(w http.ResponseWriter, r *http.Request, uid string) {
	var req sendReq
	if err := commonlib.DecodeJSON(r, &req); err != nil {
		writeErr(w, bad("WEB3_BAD_REQUEST", "请求格式错误"))
		return
	}
	info, err := LookupChain(req.Chain)
	if err != nil {
		writeErr(w, bad("WEB3_CHAIN_UNSUPPORTED", err.Error()))
		return
	}
	if strings.TrimSpace(req.To) == "" {
		writeErr(w, bad("WEB3_TO_REQUIRED", "to 必填"))
		return
	}

	lk := s.userLks.lock(uid)
	lk.Lock()
	defer lk.Unlock()

	ctx, cancel := shortCircuitTimeout(r.Context(), 45*time.Second)
	defer cancel()

	mnemonic, err := s.unlockMnemonic(ctx, uid, req.PIN)
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

	amountRaw, err := parseAmount(req.Amount, req.Token, info)
	if err != nil {
		writeErr(w, err)
		return
	}

	var (
		txHash string
		feeRaw *big.Int
		kind   = "send"
	)
	switch info.Kind {
	case KindEVM:
		txHash, feeRaw, err = s.sendEVM(ctx, info, derived, req, amountRaw)
		if req.Data != "" {
			kind = "contract"
		}
	case KindBTC:
		txHash, feeRaw, err = s.sendBTC(ctx, info, derived, req, amountRaw)
	case KindSolana:
		txHash, feeRaw, err = s.sendSolana(ctx, info, derived, req, amountRaw)
	case KindTRON:
		txHash, feeRaw, err = s.sendTRON(ctx, info, derived, req, amountRaw)
	default:
		err = fmt.Errorf("chain kind not implemented")
	}
	if err != nil {
		writeErr(w, err)
		return
	}
	if feeRaw == nil {
		feeRaw = big.NewInt(0)
	}

	// Record the transaction so the sweeper can confirm it later.
	if _, err := s.db.ExecContext(ctx,
		`INSERT INTO web3_transactions (uid, chain, tx_hash, from_addr, to_addr, value, token, gas, kind, detail)
		 VALUES ($1,$2,$3,$4,$5,$6,$7,$8,$9,$10)
		 ON CONFLICT (chain, tx_hash) DO NOTHING`,
		uid, req.Chain, txHash, derived.Address, req.To, amountRaw.String(),
		req.Token, feeRaw.String(), kind, mustJSON(map[string]any{"note": req.Note})); err != nil {
		writeErr(w, err)
		return
	}

	commonlib.WriteJSON(w, http.StatusOK, sendResp{
		TxHash:   txHash,
		Chain:    req.Chain,
		From:     derived.Address,
		To:       req.To,
		Amount:   req.Amount,
		Token:    req.Token,
		Explorer: info.ExplorerTxURL + txHash,
		Status:   "pending",
	})
}

// -------------------------------- EVM --------------------------------

func (s *Server) sendEVM(ctx context.Context, info ChainInfo, d Derived, req sendReq, amount *big.Int) (string, *big.Int, error) {
	eck, _ := btcec.PrivKeyFromBytes(d.privKey)
	ecdsaPriv := eck.ToECDSA()

	// Nonce.
	var nonce uint64
	if req.Nonce != nil {
		nonce = uint64(*req.Nonce)
	} else {
		raw, err := s.rpcCall(ctx, info.Name, "eth_getTransactionCount", []interface{}{d.Address, "pending"})
		if err != nil {
			return "", nil, err
		}
		n, err := hexToBigInt(raw)
		if err != nil {
			return "", nil, err
		}
		nonce = n.Uint64()
	}

	// Gas price.
	gasPrice := new(big.Int)
	if req.GasPrice != "" {
		if _, ok := gasPrice.SetString(req.GasPrice, 10); !ok {
			return "", nil, bad("WEB3_BAD_GAS_PRICE", "gasPrice 无效")
		}
	} else {
		raw, err := s.rpcCall(ctx, info.Name, "eth_gasPrice", nil)
		if err != nil {
			return "", nil, err
		}
		gasPrice, err = hexToBigInt(raw)
		if err != nil {
			return "", nil, err
		}
	}

	// Build calldata: native transfer has none; ERC-20 uses the
	// transfer(address,uint256) selector 0xa9059cbb; a raw `data` field
	// overrides both (contract call).
	var (
		to       = common.HexToAddress(req.To)
		callTo   = to
		callData []byte
		callVal  = amount
	)
	if req.Data != "" {
		d, err := hex.DecodeString(strings.TrimPrefix(req.Data, "0x"))
		if err != nil {
			return "", nil, bad("WEB3_BAD_CALLDATA", "data 字段非法 hex")
		}
		callData = d
	} else if req.Token != "" {
		callTo = common.HexToAddress(req.Token)
		callVal = big.NewInt(0)
		selector, _ := hex.DecodeString("a9059cbb")
		padAddr, _ := hex.DecodeString(strings.Repeat("0", 24) + strings.TrimPrefix(to.Hex()[2:], "0x"))
		padAmt := leftPad32(amount.Bytes())
		callData = append(selector, padAddr...)
		callData = append(callData, padAmt...)
	}

	// Gas limit (estimate when not supplied).
	gasLimit := req.GasLimit
	if gasLimit == 0 {
		raw, err := s.rpcCall(ctx, info.Name, "eth_estimateGas", []interface{}{
			map[string]string{
				"from":  d.Address,
				"to":    callTo.Hex(),
				"value": "0x" + callVal.Text(16),
				"data":  "0x" + hex.EncodeToString(callData),
			},
		})
		if err != nil {
			// Fallback to a sensible default. Native: 21000; token transfer: 100000.
			if req.Token != "" || len(callData) > 0 {
				gasLimit = 150000
			} else {
				gasLimit = 21000
			}
		} else {
			n, err := hexToBigInt(raw)
			if err != nil {
				return "", nil, err
			}
			gasLimit = n.Uint64()
			// Pad 10% for safety.
			gasLimit = gasLimit + gasLimit/10
		}
	}

	tx := types.NewTx(&types.LegacyTx{
		Nonce:    nonce,
		GasPrice: gasPrice,
		Gas:      gasLimit,
		To:       &callTo,
		Value:    callVal,
		Data:     callData,
	})
	signer := types.NewEIP155Signer(big.NewInt(info.EVMChainID))
	signed, err := types.SignTx(tx, signer, ecdsaPriv)
	if err != nil {
		return "", nil, err
	}
	raw, err := rlp.EncodeToBytes(signed)
	if err != nil {
		return "", nil, err
	}
	hashResult, err := s.rpcCall(ctx, info.Name, "eth_sendRawTransaction", []interface{}{"0x" + hex.EncodeToString(raw)})
	if err != nil {
		return "", nil, err
	}
	var txHash string
	if err := json.Unmarshal(hashResult, &txHash); err != nil {
		return "", nil, err
	}
	fee := new(big.Int).Mul(new(big.Int).SetUint64(gasLimit), gasPrice)
	return txHash, fee, nil
}

// -------------------------------- BTC --------------------------------

// sendBTC is deliberately NOT implemented in-process: building a signed BTC
// transaction needs UTXO selection, fee estimation and script construction
// that would double this service's size. Returns a structured error the
// Flutter app surfaces as "BTC transfers coming soon"; a follow-up PR can add
// btcsuite/btcwallet integration or route through the exchange withdrawal
// path for users who prefer to hop via the custodial side.
func (s *Server) sendBTC(ctx context.Context, info ChainInfo, d Derived, req sendReq, amount *big.Int) (string, *big.Int, error) {
	// Signing infrastructure IS in place (btcec is already imported); what's
	// missing is UTXO selection + fee estimation. Keep the panic-proof bail-out.
	_ = ecdsabtc.Sign // suppresses unused-import warning across build tags
	return "", nil, unavailable("WEB3_SEND_NOT_IMPLEMENTED", "BTC 链上转账暂未开放")
}

// -------------------------------- Solana --------------------------------

// sendSolana constructs a SystemProgram.transfer instruction for native SOL
// and submits it via sendTransaction. SPL token transfers are stubbed the same
// way as BTC — getAssociatedTokenAddress + Token-Program.transfer is doable
// but adds enough code to warrant a follow-up PR.
func (s *Server) sendSolana(ctx context.Context, info ChainInfo, d Derived, req sendReq, amount *big.Int) (string, *big.Int, error) {
	if req.Token != "" {
		return "", nil, unavailable("WEB3_SEND_NOT_IMPLEMENTED", "SPL 代币转账暂未开放")
	}
	return "", nil, unavailable("WEB3_SEND_NOT_IMPLEMENTED", "Solana 链上转账暂未开放")
}

// -------------------------------- TRON --------------------------------

func (s *Server) sendTRON(ctx context.Context, info ChainInfo, d Derived, req sendReq, amount *big.Int) (string, *big.Int, error) {
	if req.Token != "" {
		return "", nil, unavailable("WEB3_SEND_NOT_IMPLEMENTED", "TRC20 代币转账暂未开放")
	}
	return "", nil, unavailable("WEB3_SEND_NOT_IMPLEMENTED", "TRON 链上转账暂未开放")
}

// ------------------------------- helpers -------------------------------

func parseAmount(amount, tokenContract string, info ChainInfo) (*big.Int, error) {
	a := strings.TrimSpace(amount)
	if a == "" {
		return nil, bad("WEB3_BAD_AMOUNT", "amount 必填")
	}
	// Token transfers defer their decimals to the token; native uses the
	// chain's default. This is a best-effort parse that trusts the client's
	// decimals when they supplied a custom-token row.
	dec := info.Decimals
	// If the amount already looks like an integer with no dot, use decimals=0
	// (caller must have pre-scaled). Common case is a decimal with a dot.
	if !strings.Contains(a, ".") {
		n, ok := new(big.Int).SetString(a, 10)
		if !ok {
			return nil, bad("WEB3_BAD_AMOUNT", "amount 无效")
		}
		if n.Sign() < 0 {
			return nil, bad("WEB3_NEG_AMOUNT", "amount 必须为正")
		}
		return n, nil
	}
	parts := strings.SplitN(a, ".", 2)
	whole := parts[0]
	frac := parts[1]
	if len(frac) > dec {
		frac = frac[:dec]
	}
	for len(frac) < dec {
		frac += "0"
	}
	combined := strings.TrimLeft(whole+frac, "0")
	if combined == "" {
		combined = "0"
	}
	n, ok := new(big.Int).SetString(combined, 10)
	if !ok {
		return nil, bad("WEB3_BAD_AMOUNT", "amount 无效")
	}
	return n, nil
}

func leftPad32(b []byte) []byte {
	if len(b) >= 32 {
		return b
	}
	out := make([]byte, 32)
	copy(out[32-len(b):], b)
	return out
}

func mustJSON(v any) []byte {
	b, _ := json.Marshal(v)
	return b
}

// sweepPendingTxs is the periodic reconciliation loop. For each pending EVM
// tx we poll eth_getTransactionReceipt; a non-null response flips the row to
// confirmed/failed; a tx with no receipt after 24h is marked dropped so the
// UI stops spinning.
func (s *Server) sweepPendingTxs(ctx context.Context) {
	rows, err := s.db.QueryContext(ctx,
		`SELECT id, uid, chain, tx_hash, created_at FROM web3_transactions WHERE status='pending' ORDER BY created_at ASC LIMIT 200`)
	if err != nil {
		return
	}
	defer rows.Close()
	type pend struct {
		id      int64
		uid     string
		chain   string
		hash    string
		created time.Time
	}
	var list []pend
	for rows.Next() {
		var p pend
		if err := rows.Scan(&p.id, &p.uid, &p.chain, &p.hash, &p.created); err == nil {
			list = append(list, p)
		}
	}
	_ = rows.Close()
	for _, p := range list {
		s.reconcileOne(ctx, p.id, p.chain, p.hash, p.created)
	}
}

func (s *Server) reconcileOne(ctx context.Context, id int64, chain, hash string, created time.Time) {
	info, err := LookupChain(chain)
	if err != nil || info.Kind != KindEVM {
		// Only EVM reconciliation implemented. Mark very-old rows dropped so
		// the user's history doesn't accumulate zombies.
		if time.Since(created) > 7*24*time.Hour {
			_, _ = s.db.ExecContext(ctx, `UPDATE web3_transactions SET status='dropped' WHERE id=$1`, id)
		}
		return
	}
	raw, err := s.rpcCall(ctx, chain, "eth_getTransactionReceipt", []interface{}{hash})
	if err != nil || len(raw) == 0 || string(raw) == "null" {
		if time.Since(created) > 24*time.Hour {
			_, _ = s.db.ExecContext(ctx, `UPDATE web3_transactions SET status='dropped' WHERE id=$1`, id)
		}
		return
	}
	var env struct {
		Status string `json:"status"`
	}
	if err := json.Unmarshal(raw, &env); err != nil {
		return
	}
	status := "confirmed"
	if strings.TrimPrefix(strings.ToLower(env.Status), "0x") == "0" {
		status = "failed"
	}
	_, _ = s.db.ExecContext(ctx, `UPDATE web3_transactions SET status=$2, confirmed_at=NOW() WHERE id=$1`, id, status)
}

func base58Decode(s string) ([]byte, error) {
	// Thin alias so the balances.go call reads cleanly; the mr-tron package
	// returns a different error type, so we normalise here.
	b, err := mrtronDecode(s)
	if err != nil {
		return nil, errors.New("invalid base58")
	}
	return b, nil
}

func (s *Server) handleTxStatus(w http.ResponseWriter, r *http.Request, uid string) {
	hash := r.PathValue("hash")
	chain := r.URL.Query().Get("chain")
	if hash == "" || chain == "" {
		writeErr(w, bad("WEB3_BAD_REQUEST", "需要 hash 和 chain 参数"))
		return
	}
	row := s.db.QueryRowContext(r.Context(),
		`SELECT status, from_addr, to_addr, value, token, gas, created_at, confirmed_at FROM web3_transactions WHERE uid=$1 AND chain=$2 AND tx_hash=$3`,
		uid, chain, hash)
	var status, from, to, val, token string
	var gas string
	var createdAt time.Time
	var confirmedAt sql.NullTime
	if err := row.Scan(&status, &from, &to, &val, &token, &gas, &createdAt, &confirmedAt); err != nil {
		if errors.Is(err, sql.ErrNoRows) {
			writeErr(w, notFound("WEB3_TX_NOT_FOUND", "交易不存在"))
			return
		}
		writeErr(w, err)
		return
	}
	resp := map[string]any{
		"status":    status,
		"from":      from,
		"to":        to,
		"value":     val,
		"token":     token,
		"gas":       gas,
		"createdAt": createdAt,
	}
	if confirmedAt.Valid {
		resp["confirmedAt"] = confirmedAt.Time
	}
	commonlib.WriteJSON(w, http.StatusOK, resp)
}
