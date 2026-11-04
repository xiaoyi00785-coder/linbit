package main

import (
	"context"
	"encoding/json"
	"net/http/httptest"
	"testing"

	"github.com/shopspring/decimal"
)

// An admin testnet credit must be findable in the admin ledger by the bare
// ref the operator used, by the prefixed key, and by tx type.
func TestAdminLedgerFindsTestnetCreditByRef(t *testing.T) {
	creditEnv(t)
	store, _ := newTestStore(t)
	uid := testUser()
	ref := "qa-ledger-" + uid
	if _, err := store.AdminTestnetCredit(context.Background(), uid, "USDT", decimal.NewFromInt(5), ref); err != nil {
		t.Fatal(err)
	}
	h := NewServer(Config{InternalToken: "secret"}, store).Routes()
	for _, q := range []string{"ref=" + ref, "ref=admin_credit:" + ref, "txType=admin_testnet_credit&userId=" + uid} {
		r := httptest.NewRequest("GET", "/internal/admin/ledger?"+q, nil)
		r.Header.Set("X-Internal-Token", "secret")
		w := httptest.NewRecorder()
		h.ServeHTTP(w, r)
		var p struct {
			Total int `json:"total"`
			Items []struct {
				TxType string `json:"txType"`
			} `json:"items"`
		}
		_ = json.Unmarshal(w.Body.Bytes(), &p)
		if w.Code != 200 || p.Total != 1 || len(p.Items) != 1 {
			t.Fatalf("%s: code=%d body=%s", q, w.Code, w.Body.String())
		}
	}
}
