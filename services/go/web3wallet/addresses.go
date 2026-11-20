package main

import (
	"context"
	"database/sql"
	"errors"
	"fmt"
	"net/http"
	"net/url"

	"github.com/linbit/platform/services/go/common"
)

type addressRow struct {
	Chain   string `json:"chain"`
	Address string `json:"address"`
	Path    string `json:"derivationPath"`
}

func (s *Server) handleAddresses(w http.ResponseWriter, r *http.Request, uid string) {
	rows, err := s.db.QueryContext(r.Context(),
		`SELECT chain, address, derivation_path FROM web3_addresses WHERE uid=$1 ORDER BY chain`, uid)
	if err != nil {
		writeErr(w, err)
		return
	}
	defer rows.Close()
	var out []addressRow
	for rows.Next() {
		var a addressRow
		if err := rows.Scan(&a.Chain, &a.Address, &a.Path); err != nil {
			writeErr(w, err)
			return
		}
		out = append(out, a)
	}
	if err := rows.Err(); err != nil {
		writeErr(w, err)
		return
	}
	common.WriteJSON(w, http.StatusOK, map[string]any{"items": out})
}

type receiveResp struct {
	Chain   string `json:"chain"`
	Address string `json:"address"`
	URI     string `json:"uri"`   // BIP21-style URI the client can turn into a QR
	QRData  string `json:"qr"`    // same string, named for the Flutter widget
	Memo    string `json:"memo"`  // some chains need a destination tag / memo; empty elsewhere
}

func (s *Server) handleReceive(w http.ResponseWriter, r *http.Request, uid string) {
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
	resp := receiveResp{
		Chain:   chain,
		Address: addr,
		URI:     receiveURI(info, addr),
	}
	resp.QRData = resp.URI
	common.WriteJSON(w, http.StatusOK, resp)
}

func receiveURI(info ChainInfo, addr string) string {
	switch info.Kind {
	case KindEVM:
		// EIP-681: ethereum:<addr>@<chainId>
		return fmt.Sprintf("ethereum:%s@%d", addr, info.EVMChainID)
	case KindBTC:
		return "bitcoin:" + addr
	case KindSolana:
		return "solana:" + addr
	case KindTRON:
		return "tron:" + addr
	}
	return addr
}

func (s *Server) loadAddress(ctx context.Context, uid, chain string) (string, error) {
	var addr string
	err := s.db.QueryRowContext(ctx,
		`SELECT address FROM web3_addresses WHERE uid=$1 AND chain=$2`, uid, chain).Scan(&addr)
	if err != nil {
		if errors.Is(err, sql.ErrNoRows) {
			return "", notFound("WEB3_ADDRESS_NOT_FOUND", "该链尚未派生地址")
		}
		return "", err
	}
	return addr, nil
}

// convenience: tokenAddressURL safe-formats {addr, contract} for BIP681 token
// transfers (not used by receive, used by the UI's "copy token-address" row).
func tokenReceiveURI(info ChainInfo, addr, contract string) string {
	if info.Kind == KindEVM && contract != "" {
		return fmt.Sprintf("ethereum:%s/transfer?address=%s@%d", contract, addr, info.EVMChainID)
	}
	return fmt.Sprintf("%s?token=%s", receiveURI(info, addr), url.QueryEscape(contract))
}
