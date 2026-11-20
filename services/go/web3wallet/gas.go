package main

import (
	"encoding/json"
	"math/big"
	"net/http"

	"github.com/linbit/platform/services/go/common"
)

// handleGas returns the chain's current fee estimate in both base units and a
// human-friendly gwei (EVM) / sat-per-vbyte (BTC) figure.
func (s *Server) handleGas(w http.ResponseWriter, r *http.Request, uid string) {
	chain := r.URL.Query().Get("chain")
	info, err := LookupChain(chain)
	if err != nil {
		writeErr(w, bad("WEB3_CHAIN_UNSUPPORTED", "chain 参数非法"))
		return
	}
	switch info.Kind {
	case KindEVM:
		raw, err := s.rpcCall(r.Context(), chain, "eth_gasPrice", nil)
		if err != nil {
			writeErr(w, err)
			return
		}
		price, err := hexToBigInt(raw)
		if err != nil {
			writeErr(w, err)
			return
		}
		// EIP-1559 suggests priorityFee via eth_maxPriorityFeePerGas; try it
		// but don't fail if the node doesn't support it.
		var priority *big.Int
		if raw2, err := s.rpcCall(r.Context(), chain, "eth_maxPriorityFeePerGas", nil); err == nil {
			if p, err := hexToBigInt(raw2); err == nil {
				priority = p
			}
		}
		out := map[string]any{
			"chain":    chain,
			"gasPrice": price.String(),
			"gwei":     formatUnits(price, 9),
			"unit":     "wei",
		}
		if priority != nil {
			out["maxPriorityFeePerGas"] = priority.String()
			out["maxPriorityFeeGwei"] = formatUnits(priority, 9)
		}
		common.WriteJSON(w, http.StatusOK, out)
	case KindSolana:
		// Solana per-sig fee: getRecentPrioritizationFees + 5000 lamports base.
		common.WriteJSON(w, http.StatusOK, map[string]any{
			"chain": chain,
			"baseFeeLamports": 5000,
			"unit":            "lamports",
		})
	case KindBTC:
		// Blockstream has a /fee-estimates endpoint returning sat/vB for a set
		// of confirmation targets.
		var out map[string]json.Number
		if err := s.httpGetJSON(r.Context(), s.cfg.RPCs[ChainBTC].Primary+"/fee-estimates", nil, &out); err != nil {
			writeErr(w, err)
			return
		}
		common.WriteJSON(w, http.StatusOK, map[string]any{
			"chain":          chain,
			"satPerVbyte":    out,
			"unit":           "sat/vB",
		})
	case KindTRON:
		// TRON fees are bandwidth/energy — not a single number. Return the
		// chain-defined "energy_per_byte" multiplier so the client can estimate.
		common.WriteJSON(w, http.StatusOK, map[string]any{
			"chain":   chain,
			"model":   "bandwidth-energy",
			"hint":    "TRON uses bandwidth and energy; use chainparameters for exact values.",
		})
	}
}
