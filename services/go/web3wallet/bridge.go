package main

import (
	"fmt"
	"net/http"
	"strings"

	"github.com/linbit/platform/services/go/common"
)

// LI.FI is the go-to cross-chain aggregator with permissive terms; we hit the
// public endpoint by default and switch to the key-authenticated one when a
// LIFI_API_KEY is configured.
type bridgeReq struct {
	FromChain  string `json:"fromChain"`
	ToChain    string `json:"toChain"`
	FromToken  string `json:"fromToken"`
	ToToken    string `json:"toToken"`
	Amount     string `json:"amount"`
	FromAddress string `json:"fromAddress,omitempty"`
	PIN        string `json:"pin,omitempty"`
}

func (s *Server) handleBridgeQuote(w http.ResponseWriter, r *http.Request, uid string) {
	var req bridgeReq
	if err := common.DecodeJSON(r, &req); err != nil {
		writeErr(w, bad("WEB3_BAD_REQUEST", "请求格式错误"))
		return
	}
	fromAddr := req.FromAddress
	if fromAddr == "" {
		addr, err := s.loadAddress(r.Context(), uid, req.FromChain)
		if err != nil {
			writeErr(w, err)
			return
		}
		fromAddr = addr
	}
	toAddr, err := s.loadAddress(r.Context(), uid, req.ToChain)
	if err != nil {
		writeErr(w, err)
		return
	}
	u := fmt.Sprintf("https://li.quest/v1/quote?fromChain=%s&toChain=%s&fromToken=%s&toToken=%s&fromAmount=%s&fromAddress=%s&toAddress=%s",
		lifiChain(req.FromChain), lifiChain(req.ToChain),
		req.FromToken, req.ToToken, req.Amount, fromAddr, toAddr)
	var out any
	if err := s.httpGetJSON(r.Context(), u, s.lifiHeaders(), &out); err != nil {
		writeErr(w, unavailable("WEB3_BRIDGE_UPSTREAM", err.Error()))
		return
	}
	common.WriteJSON(w, http.StatusOK, out)
}

func (s *Server) handleBridgeExecute(w http.ResponseWriter, r *http.Request, uid string) {
	var req bridgeReq
	if err := common.DecodeJSON(r, &req); err != nil {
		writeErr(w, bad("WEB3_BAD_REQUEST", "请求格式错误"))
		return
	}
	info, err := LookupChain(req.FromChain)
	if err != nil || info.Kind != KindEVM {
		writeErr(w, bad("WEB3_CHAIN_UNSUPPORTED", "bridge 仅支持 EVM 发起链"))
		return
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
	derived, err := DeriveFor(mnemonic, req.FromChain)
	if err != nil {
		writeErr(w, err)
		return
	}
	defer zero(derived.privKey)
	toAddr, err := s.loadAddress(r.Context(), uid, req.ToChain)
	if err != nil {
		writeErr(w, err)
		return
	}
	u := fmt.Sprintf("https://li.quest/v1/quote?fromChain=%s&toChain=%s&fromToken=%s&toToken=%s&fromAmount=%s&fromAddress=%s&toAddress=%s",
		lifiChain(req.FromChain), lifiChain(req.ToChain),
		req.FromToken, req.ToToken, req.Amount, derived.Address, toAddr)
	var quote struct {
		TransactionRequest struct {
			From     string `json:"from"`
			To       string `json:"to"`
			Data     string `json:"data"`
			Value    string `json:"value"`
			GasPrice string `json:"gasPrice"`
			GasLimit string `json:"gasLimit"`
		} `json:"transactionRequest"`
	}
	if err := s.httpGetJSON(r.Context(), u, s.lifiHeaders(), &quote); err != nil {
		writeErr(w, unavailable("WEB3_BRIDGE_UPSTREAM", err.Error()))
		return
	}
	gasLimit := parseHexOrDec(quote.TransactionRequest.GasLimit, 400000)
	hash, _, err := s.signAndSendRaw(r.Context(), info, derived,
		quote.TransactionRequest.To, quote.TransactionRequest.Data,
		quote.TransactionRequest.Value, quote.TransactionRequest.GasPrice, gasLimit)
	if err != nil {
		writeErr(w, err)
		return
	}
	_, _ = s.db.ExecContext(r.Context(),
		`INSERT INTO web3_transactions (uid, chain, tx_hash, from_addr, to_addr, value, token, gas, kind, detail)
		 VALUES ($1,$2,$3,$4,$5,$6,'',0,'bridge',$7) ON CONFLICT (chain, tx_hash) DO NOTHING`,
		uid, req.FromChain, hash, derived.Address, quote.TransactionRequest.To, strings.TrimSpace(quote.TransactionRequest.Value),
		mustJSON(map[string]any{"toChain": req.ToChain, "toToken": req.ToToken, "amount": req.Amount, "toAddress": toAddr}))
	common.WriteJSON(w, http.StatusOK, map[string]any{"txHash": hash, "explorer": info.ExplorerTxURL + hash})
}

func (s *Server) lifiHeaders() map[string]string {
	h := map[string]string{"Accept": "application/json"}
	if s.cfg.LifiKey != "" {
		h["x-lifi-api-key"] = s.cfg.LifiKey
	}
	return h
}

func lifiChain(name string) string {
	switch name {
	case ChainETH:
		return "eth"
	case ChainBSC:
		return "bsc"
	case ChainPolygon:
		return "pol"
	case ChainArbitrum:
		return "arb"
	case ChainBase:
		return "bas"
	case ChainSolana:
		return "sol"
	}
	return name
}

func parseHexOrDec(s string, def uint64) uint64 {
	if s == "" {
		return def
	}
	s = strings.TrimSpace(s)
	if strings.HasPrefix(s, "0x") {
		var n uint64
		_, err := fmtSscan("0x"+strings.TrimPrefix(s, "0x"), &n)
		if err == nil {
			return n
		}
	}
	var n uint64
	_, err := fmtSscan(s, &n)
	if err == nil {
		return n
	}
	return def
}
