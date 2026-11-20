package main

import (
	"encoding/hex"
	"fmt"
	"math/big"
	"net/http"
	"net/url"
	"strings"

	"github.com/btcsuite/btcd/btcec/v2"
	"github.com/ethereum/go-ethereum/common"
	"github.com/ethereum/go-ethereum/core/types"
	"github.com/ethereum/go-ethereum/rlp"
	commonlib "github.com/linbit/platform/services/go/common"
)

// 1inch v6 aggregator; chainId→base URL.
func oneInchBase(chainID int64) string {
	return fmt.Sprintf("https://api.1inch.dev/swap/v6.0/%d", chainID)
}

type quoteReq struct {
	Chain  string `json:"chain"`
	FromToken string `json:"fromToken"`
	ToToken   string `json:"toToken"`
	Amount    string `json:"amount"` // base units (wei)
}

func (s *Server) handleSwapQuote(w http.ResponseWriter, r *http.Request, uid string) {
	var req quoteReq
	if err := commonlib.DecodeJSON(r, &req); err != nil {
		writeErr(w, bad("WEB3_BAD_REQUEST", "请求格式错误"))
		return
	}
	info, err := LookupChain(req.Chain)
	if err != nil || info.Kind != KindEVM {
		writeErr(w, bad("WEB3_CHAIN_UNSUPPORTED", "swap 仅支持 EVM 链"))
		return
	}
	if s.cfg.OneInchKey == "" {
		writeErr(w, unavailable("WEB3_SWAP_UNCONFIGURED", "swap 服务未配置"))
		return
	}
	u := fmt.Sprintf("%s/quote?src=%s&dst=%s&amount=%s",
		oneInchBase(info.EVMChainID),
		url.QueryEscape(req.FromToken), url.QueryEscape(req.ToToken), url.QueryEscape(req.Amount))
	var out any
	if err := s.httpGetJSON(r.Context(), u, s.oneInchHeaders(), &out); err != nil {
		writeErr(w, unavailable("WEB3_SWAP_UPSTREAM", err.Error()))
		return
	}
	commonlib.WriteJSON(w, http.StatusOK, out)
}

type execReq struct {
	Chain     string `json:"chain"`
	FromToken string `json:"fromToken"`
	ToToken   string `json:"toToken"`
	Amount    string `json:"amount"`
	Slippage  string `json:"slippage"` // percent, e.g. "1" for 1%
	PIN       string `json:"pin"`
}

// handleSwapExecute asks 1inch for the swap calldata, then builds, signs and
// broadcasts the EVM transaction using the user's wallet. Returns the tx hash.
func (s *Server) handleSwapExecute(w http.ResponseWriter, r *http.Request, uid string) {
	var req execReq
	if err := commonlib.DecodeJSON(r, &req); err != nil {
		writeErr(w, bad("WEB3_BAD_REQUEST", "请求格式错误"))
		return
	}
	info, err := LookupChain(req.Chain)
	if err != nil || info.Kind != KindEVM {
		writeErr(w, bad("WEB3_CHAIN_UNSUPPORTED", "swap 仅支持 EVM 链"))
		return
	}
	if s.cfg.OneInchKey == "" {
		writeErr(w, unavailable("WEB3_SWAP_UNCONFIGURED", "swap 服务未配置"))
		return
	}
	if req.Slippage == "" {
		req.Slippage = "1"
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

	u := fmt.Sprintf("%s/swap?src=%s&dst=%s&amount=%s&from=%s&slippage=%s&disableEstimate=false",
		oneInchBase(info.EVMChainID),
		url.QueryEscape(req.FromToken), url.QueryEscape(req.ToToken),
		url.QueryEscape(req.Amount), url.QueryEscape(derived.Address), url.QueryEscape(req.Slippage))
	var quote struct {
		Tx struct {
			From     string `json:"from"`
			To       string `json:"to"`
			Data     string `json:"data"`
			Value    string `json:"value"`
			GasPrice string `json:"gasPrice"`
			Gas      uint64 `json:"gas"`
		} `json:"tx"`
	}
	if err := s.httpGetJSON(r.Context(), u, s.oneInchHeaders(), &quote); err != nil {
		writeErr(w, unavailable("WEB3_SWAP_UPSTREAM", err.Error()))
		return
	}
	hash, _, err := s.signAndSendRaw(r.Context(), info, derived, quote.Tx.To, quote.Tx.Data, quote.Tx.Value, quote.Tx.GasPrice, quote.Tx.Gas)
	if err != nil {
		writeErr(w, err)
		return
	}
	_, _ = s.db.ExecContext(r.Context(),
		`INSERT INTO web3_transactions (uid, chain, tx_hash, from_addr, to_addr, value, token, gas, kind, detail)
		 VALUES ($1,$2,$3,$4,$5,$6,'',0,'swap',$7) ON CONFLICT (chain, tx_hash) DO NOTHING`,
		uid, req.Chain, hash, derived.Address, quote.Tx.To, strings.TrimSpace(quote.Tx.Value),
		mustJSON(map[string]any{"fromToken": req.FromToken, "toToken": req.ToToken, "amount": req.Amount}))
	commonlib.WriteJSON(w, http.StatusOK, map[string]any{"txHash": hash, "explorer": info.ExplorerTxURL + hash})
}

// signAndSendRaw packages a pre-built contract call from an aggregator quote
// (swap or bridge) into a signed EVM transaction. All fields are strings so
// the caller doesn't have to round-trip through big.Int just to re-serialize.
func (s *Server) signAndSendRaw(ctx contextCtx, info ChainInfo, d Derived, to, data, value, gasPrice string, gas uint64) (string, *big.Int, error) {
	eck, _ := btcec.PrivKeyFromBytes(d.privKey)
	ecdsaPriv := eck.ToECDSA()

	raw, err := s.rpcCall(ctx, info.Name, "eth_getTransactionCount", []interface{}{d.Address, "pending"})
	if err != nil {
		return "", nil, err
	}
	n, err := hexToBigInt(raw)
	if err != nil {
		return "", nil, err
	}
	nonce := n.Uint64()

	valBig := new(big.Int)
	if value != "" {
		if strings.HasPrefix(value, "0x") {
			_, ok := valBig.SetString(strings.TrimPrefix(value, "0x"), 16)
			if !ok {
				return "", nil, bad("WEB3_BAD_VALUE", "value 非法")
			}
		} else {
			_, ok := valBig.SetString(value, 10)
			if !ok {
				return "", nil, bad("WEB3_BAD_VALUE", "value 非法")
			}
		}
	}
	gpBig := new(big.Int)
	if gasPrice != "" {
		if strings.HasPrefix(gasPrice, "0x") {
			gpBig.SetString(strings.TrimPrefix(gasPrice, "0x"), 16)
		} else {
			gpBig.SetString(gasPrice, 10)
		}
	} else {
		rawGP, err := s.rpcCall(ctx, info.Name, "eth_gasPrice", nil)
		if err != nil {
			return "", nil, err
		}
		gpBig, err = hexToBigInt(rawGP)
		if err != nil {
			return "", nil, err
		}
	}
	callData, err := hex.DecodeString(strings.TrimPrefix(data, "0x"))
	if err != nil {
		return "", nil, bad("WEB3_BAD_CALLDATA", "aggregator 返回的 data 非法 hex")
	}
	addr := common.HexToAddress(to)
	tx := types.NewTx(&types.LegacyTx{
		Nonce:    nonce,
		GasPrice: gpBig,
		Gas:      gas,
		To:       &addr,
		Value:    valBig,
		Data:     callData,
	})
	signer := types.NewEIP155Signer(big.NewInt(info.EVMChainID))
	signed, err := types.SignTx(tx, signer, ecdsaPriv)
	if err != nil {
		return "", nil, err
	}
	bts, err := rlp.EncodeToBytes(signed)
	if err != nil {
		return "", nil, err
	}
	hashResp, err := s.rpcCall(ctx, info.Name, "eth_sendRawTransaction", []interface{}{"0x" + hex.EncodeToString(bts)})
	if err != nil {
		return "", nil, err
	}
	var hash string
	if err := jsonUnmarshalString(hashResp, &hash); err != nil {
		return "", nil, err
	}
	fee := new(big.Int).Mul(new(big.Int).SetUint64(gas), gpBig)
	return hash, fee, nil
}

func (s *Server) oneInchHeaders() map[string]string {
	return map[string]string{
		"Authorization": "Bearer " + s.cfg.OneInchKey,
		"Accept":        "application/json",
	}
}
