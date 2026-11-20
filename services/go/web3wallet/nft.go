package main

import (
	"fmt"
	"net/http"
	"net/url"
	"strings"

	"github.com/linbit/platform/services/go/common"
)

// handleNFTs proxies through to Alchemy's NFT API v3 for EVM chains and to
// Moralis for Solana. Both are opt-in by API key; without one we return an
// empty list and `source: "unconfigured"` so the client renders the empty
// state cleanly.
func (s *Server) handleNFTs(w http.ResponseWriter, r *http.Request, uid string) {
	chain := r.URL.Query().Get("chain")
	if chain == "" {
		writeErr(w, bad("WEB3_CHAIN_REQUIRED", "chain 参数必填"))
		return
	}
	info, err := LookupChain(chain)
	if err != nil {
		writeErr(w, bad("WEB3_CHAIN_UNSUPPORTED", err.Error()))
		return
	}
	addr, err := s.loadAddress(r.Context(), uid, chain)
	if err != nil {
		writeErr(w, err)
		return
	}
	switch info.Kind {
	case KindEVM:
		if s.cfg.AlchemyNFTKey == "" {
			common.WriteJSON(w, http.StatusOK, map[string]any{"items": []any{}, "source": "unconfigured"})
			return
		}
		network := alchemyNetwork(chain)
		if network == "" {
			common.WriteJSON(w, http.StatusOK, map[string]any{"items": []any{}, "source": "unsupported"})
			return
		}
		u := fmt.Sprintf("https://%s.g.alchemy.com/nft/v3/%s/getNFTsForOwner?owner=%s&withMetadata=true&pageSize=50",
			network, url.PathEscape(s.cfg.AlchemyNFTKey), url.QueryEscape(addr))
		var out map[string]any
		if err := s.httpGetJSON(r.Context(), u, nil, &out); err != nil {
			writeErr(w, err)
			return
		}
		common.WriteJSON(w, http.StatusOK, map[string]any{"items": out["ownedNfts"], "source": "alchemy"})
	case KindSolana:
		if s.cfg.MoralisKey == "" {
			common.WriteJSON(w, http.StatusOK, map[string]any{"items": []any{}, "source": "unconfigured"})
			return
		}
		u := fmt.Sprintf("https://solana-gateway.moralis.io/account/mainnet/%s/nft", url.PathEscape(addr))
		var out any
		if err := s.httpGetJSON(r.Context(), u, map[string]string{"X-API-Key": s.cfg.MoralisKey}, &out); err != nil {
			writeErr(w, err)
			return
		}
		common.WriteJSON(w, http.StatusOK, map[string]any{"items": out, "source": "moralis"})
	default:
		common.WriteJSON(w, http.StatusOK, map[string]any{"items": []any{}, "source": "unsupported"})
	}
}

func alchemyNetwork(chain string) string {
	switch strings.ToLower(chain) {
	case ChainETH:
		return "eth-mainnet"
	case ChainPolygon:
		return "polygon-mainnet"
	case ChainArbitrum:
		return "arb-mainnet"
	case ChainBase:
		return "base-mainnet"
	case ChainBSC:
		return "bnb-mainnet"
	}
	return ""
}
