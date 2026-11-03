package chainkit

import (
	"context"
	"crypto/ed25519"
	"testing"
)

// Reserve (cold-from-HD) uses SOL branch 2; it used to fail with
// "sol index out of range" and crash the wallet on startup.
func TestSolKeyBranches(t *testing.T) {
	k := &Keystore{seed: make([]byte, 64), testnet: true}
	seen := map[string]bool{}
	for _, b := range []uint32{BranchDeposit, BranchHot, BranchReserve} {
		pk, err := NewLocalHotSigner(k).PublicKey(context.Background(), ChainSol, b, 0)
		if err != nil {
			t.Fatalf("branch %d: %v", b, err)
		}
		seen[string(pk)] = true
	}
	if len(seen) != 3 {
		t.Fatal("deposit/hot/reserve must derive distinct SOL keys")
	}
	if _, err := k.solKey(BranchReserve+1, 0); err == nil {
		t.Fatal("branch > reserve must fail")
	}
	// The exported deposit list must stay identical (branch 0).
	dep, _, err := k.SolAddresses(2)
	if err != nil || len(dep) != 2 {
		t.Fatal(err)
	}
	p0, _ := k.solKey(BranchDeposit, 0)
	if dep[0] != SolAddress(p0.Public().(ed25519.PublicKey)) {
		t.Fatal("deposit export mismatch")
	}
}
