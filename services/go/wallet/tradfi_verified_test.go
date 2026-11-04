package main

import "testing"

func TestTradfiVerifiedClause(t *testing.T) {
	if tradfiVerifiedClause(true) != " AND contract_verified" || tradfiVerifiedClause(false) != "" {
		t.Fatal("mainnet must require contract_verified")
	}
}
