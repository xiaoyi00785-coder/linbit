package main

import (
	"context"
	"testing"
)

// TestDepositAddressSharedPerChain: one address per (user, network) shared by
// every asset on that chain; legacy per-asset addresses stay watched; sweep
// candidates are produced per (address, asset) actually deposited.
func TestDepositAddressSharedPerChain(t *testing.T) {
	store, db := newTestStore(t)
	ctx := context.Background()
	user, other := testUser(), testUser()+"o"
	netName := "EVM" + testUser()

	calls := 0
	gen := func(a string, i int64) func() (string, string, *int64, error) {
		return func() (string, string, *int64, error) { calls++; v := i; return a, "", &v, nil }
	}
	u1, err := store.UpsertDepositAddress(ctx, user, "USDT", netName, gen("0xaaa", 7))
	if err != nil {
		t.Fatal(err)
	}
	u2, err := store.UpsertDepositAddress(ctx, user, "USDC", netName, gen("0xbbb", 8))
	if err != nil {
		t.Fatal(err)
	}
	if u1.Address != "0xaaa" || u2.Address != "0xaaa" || calls != 1 {
		t.Fatalf("want shared 0xaaa with 1 generate, got %q %q calls=%d", u1.Address, u2.Address, calls)
	}
	var hd int64
	if err := db.QueryRow(`SELECT hd_index FROM deposit_addresses WHERE user_id=$1 AND asset='USDC'`, user).Scan(&hd); err != nil || hd != 7 {
		t.Fatalf("USDC hd_index = %d/%v, want 7", hd, err)
	}
	// Repeat request is stable.
	if again, _ := store.UpsertDepositAddress(ctx, user, "USDC", netName, gen("0xccc", 9)); again.Address != "0xaaa" || calls != 1 {
		t.Fatalf("repeat = %q calls=%d", again.Address, calls)
	}
	// Another user gets their own address.
	if o, _ := store.UpsertDepositAddress(ctx, other, "USDT", netName, gen("0xddd", 10)); o.Address != "0xddd" {
		t.Fatalf("other user = %q", o.Address)
	}
	// Legacy per-asset address issued before the change is still watched.
	if _, err := db.Exec(`INSERT INTO deposit_addresses (user_id, asset, network, address, memo, hd_index) VALUES ($1,'ETH',$2,'0xlegacy','',11)`, user, netName); err != nil {
		t.Fatal(err)
	}
	addrs, err := store.DepositAddressesFor(ctx, netName, "USDC")
	if err != nil {
		t.Fatal(err)
	}
	want := map[string]bool{"0xaaa": true, "0xddd": true, "0xlegacy": true}
	if len(addrs) != 3 {
		t.Fatalf("watched = %v", addrs)
	}
	for _, a := range addrs {
		if !want[a] {
			t.Fatalf("unexpected watched %q", a)
		}
	}

	// A simulated deposit to the shared address matches exactly one owner.
	if _, err := store.InjectChainTx(ctx, netName, "0xaaa", "USDC", d("5"), "0xtx1", 0); err != nil {
		t.Fatal(err)
	}
	m, err := store.MatchedChainTxs(ctx, netName)
	if err != nil || len(m) != 1 {
		t.Fatalf("matched = %d/%v, want 1", len(m), err)
	}

	// Sweep candidates are per deposited (address, asset).
	if _, err := db.Exec(`DELETE FROM sweeps WHERE network=$1`, netName); err != nil {
		t.Fatal(err)
	}
	for i, a := range []string{"USDT", "USDC"} {
		if _, err := db.Exec(`INSERT INTO deposits (user_id, asset, network, address, amount, tx_hash, log_index, status, credited_at)
			VALUES ($1,$2,$3,'0xaaa',10,$4,0,'completed',now())`, user, a, netName, "0xdep"+a+string(rune('0'+i))); err != nil {
			t.Fatal(err)
		}
	}
	cands, err := store.SweepCandidates(ctx, 100)
	if err != nil {
		t.Fatal(err)
	}
	got := map[string]int64{}
	for _, c := range cands {
		if c.Network == netName {
			got[c.Asset+"@"+c.Address] = c.HDIndex
		}
	}
	if len(got) != 2 || got["USDT@0xaaa"] != 7 || got["USDC@0xaaa"] != 7 {
		t.Fatalf("sweep candidates = %v", got)
	}
}
