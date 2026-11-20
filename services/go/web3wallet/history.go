package main

import (
	"fmt"
	"net/http"
	"net/url"
	"strconv"

	"github.com/linbit/platform/services/go/common"
)

// handleHistory pulls transaction history for the requested chain from the
// matching block explorer API. For chains we don't have an explorer key for,
// it falls back to the service's own `web3_transactions` table so at least
// outbound tx sent from this wallet are visible.
func (s *Server) handleHistory(w http.ResponseWriter, r *http.Request, uid string) {
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
	limit, _ := strconv.Atoi(r.URL.Query().Get("limit"))
	if limit <= 0 || limit > 100 {
		limit = 25
	}
	addr, err := s.loadAddress(r.Context(), uid, chain)
	if err != nil {
		writeErr(w, err)
		return
	}
	if info.Kind == KindEVM {
		items, err := s.evmScanHistory(r, info, addr, limit)
		if err == nil {
			common.WriteJSON(w, http.StatusOK, map[string]any{"items": items, "source": "explorer"})
			return
		}
		// Fall through to the local mirror on error.
	}
	items, err := s.localHistory(r, uid, chain, limit)
	if err != nil {
		writeErr(w, err)
		return
	}
	common.WriteJSON(w, http.StatusOK, map[string]any{"items": items, "source": "local"})
}

type localTx struct {
	TxHash    string `json:"txHash"`
	Chain     string `json:"chain"`
	From      string `json:"from"`
	To        string `json:"to"`
	Value     string `json:"value"`
	Token     string `json:"token"`
	Status    string `json:"status"`
	Kind      string `json:"kind"`
	CreatedAt string `json:"createdAt"`
}

func (s *Server) localHistory(r *http.Request, uid, chain string, limit int) ([]localTx, error) {
	rows, err := s.db.QueryContext(r.Context(),
		`SELECT tx_hash, from_addr, to_addr, value, token, status, kind, created_at
		 FROM web3_transactions WHERE uid=$1 AND chain=$2 ORDER BY created_at DESC LIMIT $3`,
		uid, chain, limit)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	var out []localTx
	for rows.Next() {
		var t localTx
		var createdAt string
		if err := rows.Scan(&t.TxHash, &t.From, &t.To, &t.Value, &t.Token, &t.Status, &t.Kind, &createdAt); err != nil {
			return nil, err
		}
		t.Chain = chain
		t.CreatedAt = createdAt
		out = append(out, t)
	}
	return out, rows.Err()
}

func (s *Server) evmScanHistory(r *http.Request, info ChainInfo, addr string, limit int) ([]map[string]any, error) {
	base, apiKey := s.scanAPIFor(info.Name)
	if base == "" || apiKey == "" {
		return nil, fmt.Errorf("explorer not configured for %s", info.Name)
	}
	u := fmt.Sprintf("%s?module=account&action=txlist&address=%s&sort=desc&page=1&offset=%d&apikey=%s",
		base, url.QueryEscape(addr), limit, url.QueryEscape(apiKey))
	var resp struct {
		Status  string           `json:"status"`
		Message string           `json:"message"`
		Result  []map[string]any `json:"result"`
	}
	if err := s.httpGetJSON(r.Context(), u, nil, &resp); err != nil {
		return nil, err
	}
	if resp.Status != "1" && len(resp.Result) == 0 {
		return []map[string]any{}, nil
	}
	return resp.Result, nil
}

func (s *Server) scanAPIFor(chain string) (base, key string) {
	switch chain {
	case ChainETH:
		return "https://api.etherscan.io/api", s.cfg.EtherscanKey
	case ChainBSC:
		return "https://api.bscscan.com/api", s.cfg.BscscanKey
	case ChainPolygon:
		return "https://api.polygonscan.com/api", s.cfg.PolygonscanKey
	case ChainArbitrum:
		return "https://api.arbiscan.io/api", s.cfg.ArbiscanKey
	case ChainBase:
		return "https://api.basescan.org/api", s.cfg.BasescanKey
	}
	return "", ""
}
