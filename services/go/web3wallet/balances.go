package main

import (
	"context"
	"encoding/hex"
	"encoding/json"
	"fmt"
	"math/big"
	"net/http"
	"strings"
	"sync"
	"time"

	"github.com/linbit/platform/services/go/common"
)

// Balance is one row in the balances response: native or token holding on a
// specific chain. Raw is the integer base-unit amount; Amount is the human
// decimal for convenience.
type Balance struct {
	Chain    string `json:"chain"`
	Address  string `json:"address"`
	Symbol   string `json:"symbol"`
	Contract string `json:"contract"`
	Decimals int    `json:"decimals"`
	Raw      string `json:"raw"`
	Amount   string `json:"amount"`
}

func (s *Server) handleBalances(w http.ResponseWriter, r *http.Request, uid string) {
	ctx, cancel := shortCircuitTimeout(r.Context(), 30*time.Second)
	defer cancel()

	addrs, err := s.loadAllAddresses(ctx, uid)
	if err != nil {
		writeErr(w, err)
		return
	}
	custom, err := s.loadUserTokens(ctx, uid)
	if err != nil {
		writeErr(w, err)
		return
	}

	// Build the list of (chain, contract, addr) tuples to query. Natives always
	// query; token contracts query only on the chain they live on.
	type q struct {
		chain    string
		addr     string
		contract string
		symbol   string
		decimals int
	}
	var jobs []q
	for chain, addr := range addrs {
		info, err := LookupChain(chain)
		if err != nil {
			continue
		}
		jobs = append(jobs, q{chain, addr, "", info.Symbol, info.Decimals})
	}
	for _, t := range custom {
		addr, ok := addrs[t.Chain]
		if !ok {
			continue
		}
		jobs = append(jobs, q{t.Chain, addr, t.Contract, t.Symbol, t.Decimals})
	}

	results := make([]Balance, len(jobs))
	var wg sync.WaitGroup
	wg.Add(len(jobs))
	for i, j := range jobs {
		i, j := i, j
		go func() {
			defer wg.Done()
			raw, err := s.queryBalance(ctx, j.chain, j.addr, j.contract, j.decimals)
			if err != nil {
				// Non-fatal: return 0 and let the client surface the partial result.
				raw = big.NewInt(0)
			}
			results[i] = Balance{
				Chain:    j.chain,
				Address:  j.addr,
				Symbol:   j.symbol,
				Contract: j.contract,
				Decimals: j.decimals,
				Raw:      raw.String(),
				Amount:   formatUnits(raw, j.decimals),
			}
		}()
	}
	wg.Wait()
	common.WriteJSON(w, http.StatusOK, map[string]any{"items": results})
}

// queryBalance dispatches to the right chain-kind handler. Returns a base-unit
// integer (wei for EVM, lamports for SOL, sun for TRON, sats for BTC).
func (s *Server) queryBalance(ctx context.Context, chain, addr, contract string, decimals int) (*big.Int, error) {
	info, err := LookupChain(chain)
	if err != nil {
		return nil, err
	}
	switch info.Kind {
	case KindEVM:
		if contract == "" {
			return s.evmNativeBalance(ctx, chain, addr)
		}
		return s.evmERC20Balance(ctx, chain, addr, contract)
	case KindBTC:
		return s.btcBalance(ctx, addr)
	case KindSolana:
		if contract == "" {
			return s.solanaBalance(ctx, addr)
		}
		return s.solanaTokenBalance(ctx, addr, contract)
	case KindTRON:
		if contract == "" {
			return s.tronBalance(ctx, addr)
		}
		return s.tronTRC20Balance(ctx, addr, contract)
	}
	return nil, fmt.Errorf("unsupported chain %s", chain)
}

// ------------------------------- EVM -------------------------------

func (s *Server) evmNativeBalance(ctx context.Context, chain, addr string) (*big.Int, error) {
	raw, err := s.rpcCall(ctx, chain, "eth_getBalance", []interface{}{addr, "latest"})
	if err != nil {
		return nil, err
	}
	return hexToBigInt(raw)
}

// ERC-20 balanceOf(address) selector is 0x70a08231 followed by the 32-byte
// zero-padded address. The JSON-RPC eth_call returns a 32-byte hex integer.
func (s *Server) evmERC20Balance(ctx context.Context, chain, addr, contract string) (*big.Int, error) {
	data := "0x70a08231" + hexZeroPadAddress(addr)
	raw, err := s.rpcCall(ctx, chain, "eth_call",
		[]interface{}{map[string]string{"to": contract, "data": data}, "latest"})
	if err != nil {
		return nil, err
	}
	return hexToBigInt(raw)
}

// ------------------------------- BTC -------------------------------

type blockstreamAddr struct {
	ChainStats struct {
		FundedTxoSum uint64 `json:"funded_txo_sum"`
		SpentTxoSum  uint64 `json:"spent_txo_sum"`
	} `json:"chain_stats"`
	MempoolStats struct {
		FundedTxoSum int64 `json:"funded_txo_sum"`
		SpentTxoSum  int64 `json:"spent_txo_sum"`
	} `json:"mempool_stats"`
}

func (s *Server) btcBalance(ctx context.Context, addr string) (*big.Int, error) {
	rpc := s.cfg.RPCs[ChainBTC]
	if rpc.Primary == "" {
		return nil, unavailable("WEB3_RPC_NOT_CONFIGURED", "BTC RPC 未配置")
	}
	var out blockstreamAddr
	if err := s.httpGetJSON(ctx, strings.TrimRight(rpc.Primary, "/")+"/address/"+addr, nil, &out); err != nil {
		return nil, err
	}
	conf := int64(out.ChainStats.FundedTxoSum) - int64(out.ChainStats.SpentTxoSum)
	mem := out.MempoolStats.FundedTxoSum - out.MempoolStats.SpentTxoSum
	return big.NewInt(conf + mem), nil
}

// ------------------------------- Solana -------------------------------

func (s *Server) solanaBalance(ctx context.Context, addr string) (*big.Int, error) {
	raw, err := s.rpcCall(ctx, ChainSolana, "getBalance", []interface{}{addr})
	if err != nil {
		return nil, err
	}
	var env struct {
		Value uint64 `json:"value"`
	}
	if err := json.Unmarshal(raw, &env); err != nil {
		return nil, err
	}
	return new(big.Int).SetUint64(env.Value), nil
}

func (s *Server) solanaTokenBalance(ctx context.Context, addr, mint string) (*big.Int, error) {
	raw, err := s.rpcCall(ctx, ChainSolana, "getTokenAccountsByOwner",
		[]interface{}{addr,
			map[string]string{"mint": mint},
			map[string]string{"encoding": "jsonParsed"},
		})
	if err != nil {
		return nil, err
	}
	var env struct {
		Value []struct {
			Account struct {
				Data struct {
					Parsed struct {
						Info struct {
							TokenAmount struct {
								Amount string `json:"amount"`
							} `json:"tokenAmount"`
						} `json:"info"`
					} `json:"parsed"`
				} `json:"data"`
			} `json:"account"`
		} `json:"value"`
	}
	if err := json.Unmarshal(raw, &env); err != nil {
		return nil, err
	}
	total := new(big.Int)
	for _, v := range env.Value {
		n, ok := new(big.Int).SetString(v.Account.Data.Parsed.Info.TokenAmount.Amount, 10)
		if ok {
			total.Add(total, n)
		}
	}
	return total, nil
}

// ------------------------------- TRON -------------------------------

func (s *Server) tronBalance(ctx context.Context, addr string) (*big.Int, error) {
	rpc := s.cfg.RPCs[ChainTRON]
	if rpc.Primary == "" {
		return nil, unavailable("WEB3_RPC_NOT_CONFIGURED", "TRON RPC 未配置")
	}
	var out struct {
		Balance int64 `json:"balance"`
	}
	headers := map[string]string{}
	if k := strings.TrimSpace(s.cfg.RPCs[ChainTRON].Primary); k != "" {
		// TronGrid allows an unauthenticated read; pass API key when present via env.
	}
	body := map[string]interface{}{"address": addr, "visible": true}
	if err := s.httpPostJSON(ctx, strings.TrimRight(rpc.Primary, "/")+"/wallet/getaccount", headers, body, &out); err != nil {
		return nil, err
	}
	return big.NewInt(out.Balance), nil
}

func (s *Server) tronTRC20Balance(ctx context.Context, addr, contract string) (*big.Int, error) {
	// triggerconstantcontract with the balanceOf(address) selector.
	rpc := s.cfg.RPCs[ChainTRON]
	if rpc.Primary == "" {
		return nil, unavailable("WEB3_RPC_NOT_CONFIGURED", "TRON RPC 未配置")
	}
	// Convert the base58 address to the 20-byte hex form (strip 0x41 prefix).
	hexAddr, err := tronBase58ToHex20(addr)
	if err != nil {
		return nil, err
	}
	param := strings.Repeat("0", 24) + hexAddr
	body := map[string]interface{}{
		"owner_address":     addr,
		"contract_address":  contract,
		"function_selector": "balanceOf(address)",
		"parameter":         param,
		"visible":           true,
	}
	var out struct {
		ConstantResult []string `json:"constant_result"`
	}
	if err := s.httpPostJSON(ctx, strings.TrimRight(rpc.Primary, "/")+"/wallet/triggerconstantcontract", nil, body, &out); err != nil {
		return nil, err
	}
	if len(out.ConstantResult) == 0 {
		return big.NewInt(0), nil
	}
	v := new(big.Int)
	v.SetString(out.ConstantResult[0], 16)
	return v, nil
}

// ---------------------------- helpers ----------------------------

func (s *Server) loadAllAddresses(ctx context.Context, uid string) (map[string]string, error) {
	rows, err := s.db.QueryContext(ctx,
		`SELECT chain, address FROM web3_addresses WHERE uid=$1`, uid)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	out := map[string]string{}
	for rows.Next() {
		var c, a string
		if err := rows.Scan(&c, &a); err != nil {
			return nil, err
		}
		out[c] = a
	}
	return out, rows.Err()
}

func (s *Server) loadUserTokens(ctx context.Context, uid string) ([]Token, error) {
	rows, err := s.db.QueryContext(ctx,
		`SELECT chain, contract, symbol, name, decimals, logo_url FROM web3_custom_tokens WHERE uid=$1`, uid)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	var out []Token
	for rows.Next() {
		var t Token
		if err := rows.Scan(&t.Chain, &t.Contract, &t.Symbol, &t.Name, &t.Decimals, &t.LogoURL); err != nil {
			return nil, err
		}
		out = append(out, t)
	}
	return out, rows.Err()
}

// ---------------------------- utility ----------------------------

func hexToBigInt(raw json.RawMessage) (*big.Int, error) {
	var s string
	if err := json.Unmarshal(raw, &s); err != nil {
		return nil, err
	}
	s = strings.TrimPrefix(s, "0x")
	if s == "" {
		return big.NewInt(0), nil
	}
	n, ok := new(big.Int).SetString(s, 16)
	if !ok {
		return nil, fmt.Errorf("bad hex int %q", s)
	}
	return n, nil
}

func hexZeroPadAddress(addr string) string {
	addr = strings.TrimPrefix(strings.ToLower(addr), "0x")
	if len(addr) < 40 {
		addr = strings.Repeat("0", 40-len(addr)) + addr
	}
	return strings.Repeat("0", 24) + addr
}

func tronBase58ToHex20(addr string) (string, error) {
	// Decoded form is [0x41 || 20B || 4B checksum] = 25 bytes.
	dec, err := base58Decode(addr)
	if err != nil || len(dec) != 25 {
		return "", fmt.Errorf("bad tron address")
	}
	return hex.EncodeToString(dec[1:21]), nil
}

// formatUnits turns a base-unit integer into a human decimal string with up
// to `decimals` fractional digits, no trailing zeros.
func formatUnits(v *big.Int, decimals int) string {
	if v == nil || v.Sign() == 0 {
		return "0"
	}
	neg := v.Sign() < 0
	abs := new(big.Int).Abs(v)
	str := abs.String()
	if decimals <= 0 {
		if neg {
			return "-" + str
		}
		return str
	}
	for len(str) <= decimals {
		str = "0" + str
	}
	whole := str[:len(str)-decimals]
	frac := strings.TrimRight(str[len(str)-decimals:], "0")
	out := whole
	if frac != "" {
		out = whole + "." + frac
	}
	if neg {
		out = "-" + out
	}
	return out
}
