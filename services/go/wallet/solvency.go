package main

// solvency.go：每日偿付检查用的平台托管余额。
// 真实链（testnet/mainnet）：热钱包、冷/储备地址和用户充值地址的链上余额，按币种跨网络相加。
// 任一 RPC 失败放进 errors。调用方把不完整读数当成检查失败，不能当成充足。
// MockChain 没有链：托管余额 = 已完成充值 − 已离开的提现（processing/completed）。

import (
	"context"
	"log"
	"net/http"
	"sort"

	"github.com/linbit/platform/services/go/common"
	"github.com/shopspring/decimal"
)

type holdingsResult struct {
	Mode     string            `json:"mode"`
	Holdings map[string]string `json:"holdings"`
	Errors   []string          `json:"errors"`
}

// Holdings reads the custody balance per asset from the chain.
func (rc *RealChain) Holdings(ctx context.Context) (map[string]decimal.Decimal, []string) {
	out := map[string]decimal.Decimal{}
	var errs []string
	names := make([]string, 0, len(rc.nets))
	for n := range rc.nets {
		names = append(names, n)
	}
	sort.Strings(names)
	for _, name := range names {
		cn := rc.nets[name]
		for asset, tok := range cn.tokens {
			addrs := []string{cn.hot.Address}
			if cn.cold != "" && cn.cold != cn.hot.Address {
				addrs = append(addrs, cn.cold)
			}
			deps, err := rc.store.DepositAddressesFor(ctx, name, asset)
			if err != nil {
				errs = append(errs, name+"/"+asset+": deposit addresses: "+err.Error())
				continue
			}
			addrs = append(addrs, deps...)
			seen := map[string]bool{}
			for _, a := range addrs {
				if a == "" || seen[a] {
					continue
				}
				seen[a] = true
				var b decimal.Decimal
				if tok == nil {
					b, err = cn.client.NativeBalance(ctx, a)
				} else {
					b, err = cn.client.TokenBalance(ctx, *tok, a)
				}
				if err != nil {
					errs = append(errs, name+"/"+asset+": balance of "+a+" unavailable")
					continue
				}
				out[asset] = out[asset].Add(b)
			}
		}
	}
	return out, errs
}

// MockHoldings is the simulated custody balance for MockChain.
func (s *Store) MockHoldings(ctx context.Context) (map[string]decimal.Decimal, error) {
	rows, err := s.db.QueryContext(ctx, `
		SELECT asset, SUM(amt)::text FROM (
		  SELECT asset, amount AS amt FROM deposits WHERE status = 'completed'
		  UNION ALL
		  SELECT asset, -frozen_amount FROM withdrawals WHERE status IN ('processing','completed')
		) t GROUP BY asset`)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	out := map[string]decimal.Decimal{}
	for rows.Next() {
		var a, v string
		if err := rows.Scan(&a, &v); err != nil {
			return nil, err
		}
		out[a] = decimal.RequireFromString(v)
	}
	return out, rows.Err()
}

// GET /internal/solvency/holdings
func (s *Server) handleHoldings(w http.ResponseWriter, r *http.Request) {
	res := holdingsResult{Holdings: map[string]string{}, Errors: []string{}}
	var h map[string]decimal.Decimal
	if s.real != nil {
		res.Mode = s.real.Kind()
		var errs []string
		h, errs = s.real.Holdings(r.Context())
		res.Errors = append(res.Errors, errs...)
	} else {
		res.Mode = "mock"
		var err error
		if h, err = s.store.MockHoldings(r.Context()); err != nil {
			log.Printf("mock holdings: %v", err)
			common.WriteError(w, http.StatusInternalServerError, "INTERNAL", "服务器内部错误")
			return
		}
	}
	for a, v := range h {
		res.Holdings[a] = v.String()
	}
	common.WriteJSON(w, http.StatusOK, res)
}
