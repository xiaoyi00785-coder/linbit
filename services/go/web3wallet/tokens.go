package main

import (
	"net/http"
	"strconv"
	"strings"

	"github.com/linbit/platform/services/go/common"
)

// Token is the normalized shape returned by /web3/tokens/search and used by
// /web3/wallets/balances. Contract is empty for the native asset.
type Token struct {
	Chain    string `json:"chain"`
	Contract string `json:"contract"`
	Symbol   string `json:"symbol"`
	Name     string `json:"name"`
	Decimals int    `json:"decimals"`
	LogoURL  string `json:"logoUrl"`
}

// builtinTokens is the top-of-the-market list the search endpoint falls back
// to when a third-party catalogue (CoinGecko) is unreachable. The list is
// deliberately short — the Flutter UI already lets the user paste a contract
// address, and anything beyond this takes custom-token input.
var builtinTokens = []Token{
	// Native assets.
	{ChainETH, "", "ETH", "Ether", 18, ""},
	{ChainBSC, "", "BNB", "BNB", 18, ""},
	{ChainPolygon, "", "MATIC", "Polygon", 18, ""},
	{ChainArbitrum, "", "ETH", "Ether", 18, ""},
	{ChainBase, "", "ETH", "Ether", 18, ""},
	{ChainBTC, "", "BTC", "Bitcoin", 8, ""},
	{ChainSolana, "", "SOL", "Solana", 9, ""},
	{ChainTRON, "", "TRX", "Tron", 6, ""},

	// Stablecoins + popular ERC-20s.
	{ChainETH, "0xdAC17F958D2ee523a2206206994597C13D831ec7", "USDT", "Tether USD", 6, ""},
	{ChainETH, "0xA0b86991c6218b36c1d19D4a2e9Eb0cE3606eB48", "USDC", "USD Coin", 6, ""},
	{ChainETH, "0x2260FAC5E5542a773Aa44fBCfeDf7C193bc2C599", "WBTC", "Wrapped BTC", 8, ""},
	{ChainBSC, "0x55d398326f99059fF775485246999027B3197955", "USDT", "Tether USD", 18, ""},
	{ChainBSC, "0x8AC76a51cc950d9822D68b83fE1Ad97B32Cd580d", "USDC", "USD Coin", 18, ""},
	{ChainPolygon, "0xc2132D05D31c914a87C6611C10748AEb04B58e8F", "USDT", "Tether USD", 6, ""},
	{ChainPolygon, "0x2791Bca1f2de4661ED88A30C99A7a9449Aa84174", "USDC", "USD Coin (PoS)", 6, ""},
	{ChainArbitrum, "0xFd086bC7CD5C481DCC9C85ebE478A1C0b69FCbb9", "USDT", "Tether USD", 6, ""},
	{ChainArbitrum, "0xaf88d065e77c8cC2239327C5EDb3A432268e5831", "USDC", "USD Coin", 6, ""},
	{ChainBase, "0x833589fCD6eDb6E08f4c7C32D4f71b54bdA02913", "USDC", "USD Coin", 6, ""},
	{ChainSolana, "Es9vMFrzaCERmJfrF4H2FYD4KCoNkY11McCe8BenwNYB", "USDT", "Tether USD", 6, ""},
	{ChainSolana, "EPjFWdd5AufqSSqeM2qN1xzybapC8G4wEGGkZwyTDt1v", "USDC", "USD Coin", 6, ""},
	{ChainTRON, "TR7NHqjeKQxGTCi8q8ZY4pL8otSzgjLj6t", "USDT", "Tether USD", 6, ""},
}

func (s *Server) handleTokenSearch(w http.ResponseWriter, r *http.Request, uid string) {
	q := strings.ToLower(strings.TrimSpace(r.URL.Query().Get("symbol")))
	chainFilter := strings.ToLower(strings.TrimSpace(r.URL.Query().Get("chain")))
	var out []Token
	for _, t := range builtinTokens {
		if chainFilter != "" && t.Chain != chainFilter {
			continue
		}
		if q != "" && !strings.Contains(strings.ToLower(t.Symbol), q) && !strings.Contains(strings.ToLower(t.Name), q) {
			continue
		}
		out = append(out, t)
	}
	// Also include the user's own custom tokens — they may want to pick an
	// already-added token to send.
	rows, err := s.db.QueryContext(r.Context(),
		`SELECT chain, contract, symbol, name, decimals, logo_url FROM web3_custom_tokens WHERE uid=$1 ORDER BY added_at DESC`, uid)
	if err != nil {
		writeErr(w, err)
		return
	}
	defer rows.Close()
	for rows.Next() {
		var t Token
		if err := rows.Scan(&t.Chain, &t.Contract, &t.Symbol, &t.Name, &t.Decimals, &t.LogoURL); err != nil {
			writeErr(w, err)
			return
		}
		if chainFilter != "" && t.Chain != chainFilter {
			continue
		}
		if q != "" && !strings.Contains(strings.ToLower(t.Symbol), q) && !strings.Contains(strings.ToLower(t.Name), q) {
			continue
		}
		out = append(out, t)
	}
	common.WriteJSON(w, http.StatusOK, map[string]any{"items": out})
}

type addTokenReq struct {
	Chain    string `json:"chain"`
	Contract string `json:"contract"`
	Symbol   string `json:"symbol"`
	Name     string `json:"name,omitempty"`
	Decimals int    `json:"decimals"`
	LogoURL  string `json:"logoUrl,omitempty"`
}

func (s *Server) handleAddCustomToken(w http.ResponseWriter, r *http.Request, uid string) {
	var req addTokenReq
	if err := common.DecodeJSON(r, &req); err != nil {
		writeErr(w, bad("WEB3_BAD_REQUEST", "请求格式错误"))
		return
	}
	if _, err := LookupChain(req.Chain); err != nil {
		writeErr(w, bad("WEB3_CHAIN_UNSUPPORTED", err.Error()))
		return
	}
	if strings.TrimSpace(req.Contract) == "" || strings.TrimSpace(req.Symbol) == "" {
		writeErr(w, bad("WEB3_BAD_TOKEN", "contract 和 symbol 必填"))
		return
	}
	if req.Decimals < 0 || req.Decimals > 36 {
		writeErr(w, bad("WEB3_BAD_DECIMALS", "小数位数非法"))
		return
	}
	var id int64
	err := s.db.QueryRowContext(r.Context(),
		`INSERT INTO web3_custom_tokens (uid, chain, contract, symbol, name, decimals, logo_url) VALUES ($1,$2,$3,$4,$5,$6,$7)
		 ON CONFLICT (uid, chain, contract) DO UPDATE SET symbol=EXCLUDED.symbol, name=EXCLUDED.name, decimals=EXCLUDED.decimals, logo_url=EXCLUDED.logo_url
		 RETURNING id`,
		uid, req.Chain, req.Contract, req.Symbol, req.Name, req.Decimals, req.LogoURL).Scan(&id)
	if err != nil {
		writeErr(w, err)
		return
	}
	common.WriteJSON(w, http.StatusOK, map[string]any{"id": id})
}

func (s *Server) handleListCustomTokens(w http.ResponseWriter, r *http.Request, uid string) {
	rows, err := s.db.QueryContext(r.Context(),
		`SELECT id, chain, contract, symbol, name, decimals, logo_url FROM web3_custom_tokens WHERE uid=$1 ORDER BY added_at DESC`, uid)
	if err != nil {
		writeErr(w, err)
		return
	}
	defer rows.Close()
	type row struct {
		ID int64 `json:"id"`
		Token
	}
	var out []row
	for rows.Next() {
		var r row
		if err := rows.Scan(&r.ID, &r.Chain, &r.Contract, &r.Symbol, &r.Name, &r.Decimals, &r.LogoURL); err != nil {
			writeErr(w, err)
			return
		}
		out = append(out, r)
	}
	common.WriteJSON(w, http.StatusOK, map[string]any{"items": out})
}

func (s *Server) handleRemoveCustomToken(w http.ResponseWriter, r *http.Request, uid string) {
	id, err := strconv.ParseInt(r.PathValue("id"), 10, 64)
	if err != nil {
		writeErr(w, bad("WEB3_BAD_ID", "id 无效"))
		return
	}
	res, err := s.db.ExecContext(r.Context(),
		`DELETE FROM web3_custom_tokens WHERE uid=$1 AND id=$2`, uid, id)
	if err != nil {
		writeErr(w, err)
		return
	}
	if n, _ := res.RowsAffected(); n == 0 {
		writeErr(w, notFound("WEB3_TOKEN_NOT_FOUND", "该代币不存在"))
		return
	}
	common.WriteJSON(w, http.StatusOK, map[string]any{"ok": true})
}
