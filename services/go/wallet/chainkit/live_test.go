package chainkit

import (
	"context"
	"os"
	"path/filepath"
	"testing"
	"time"
)

// TestLiveTestnets talks to the public testnet endpoints (read-only: tips,
// chain ids, balances and deposit discovery for freshly derived addresses).
// Enabled with LINBIT_LIVE_TESTNET=1. Sending needs funded keys and is not
// exercised here.
func TestLiveTestnets(t *testing.T) {
	if os.Getenv("LINBIT_LIVE_TESTNET") != "1" {
		t.Skip("set LINBIT_LIVE_TESTNET=1 to run against public testnets")
	}
	ctx, cancel := context.WithTimeout(context.Background(), 2*time.Minute)
	defer cancel()
	ks, err := CreateKeystore(filepath.Join(t.TempDir(), "k.json"), []byte("live test passphrase"), true)
	if err != nil {
		t.Fatal(err)
	}
	s := NewLocalHotSigner(ks)
	addr := func(c Chain) string {
		pk, _ := s.PublicKey(ctx, c, BranchDeposit, 0)
		if c == ChainSol {
			return Base58Encode(pk)
		}
		a, _ := AddressFromPub(c, pk, "tb")
		return a
	}
	evm := map[string]struct {
		url string
		id  int64
		tok *Token
	}{
		"sepolia":          {"https://ethereum-sepolia-rpc.publicnode.com", 11155111, &Token{"USDC", "0x1c7D4B196Cb0C7B01d743Fbc6116a902379C7238", 6}},
		"bsc-testnet":      {"https://bsc-testnet-rpc.publicnode.com", 97, &Token{"USDT", "0x337610d27c682E347C9cD60BD4b3b107C9d34dDd", 18}},
		"arbitrum-sepolia": {"https://sepolia-rollup.arbitrum.io/rpc", 421614, &Token{"USDC", "0x75faf114eafb1BDbe2F0316DF893fd58CE46AA4d", 6}},
	}
	for name, e := range evm {
		c := NewEVMClient(e.url, e.id)
		id, err := c.ChainIDRemote(ctx)
		if err != nil || id != e.id {
			t.Fatalf("%s chain id %d %v", name, id, err)
		}
		tip, err := c.Tip(ctx)
		cost, err2 := c.TransferCost(ctx, e.tok)
		bal, err3 := c.TokenBalance(ctx, *e.tok, addr(ChainEVM))
		c.MaxScan = 5
		_, _, err4 := c.Incoming(ctx, []string{addr(ChainEVM)}, e.tok, tip-20)
		if err != nil || err2 != nil || err3 != nil || err4 != nil || tip == 0 {
			t.Fatalf("%s: %v %v %v %v", name, err, err2, err3, err4)
		}
		t.Logf("%s tip=%d tokenTransferCost=%s balance=%s", name, tip, cost, bal)
	}
	tron := NewTronClient("https://nile.trongrid.io", os.Getenv("TRON_API_KEY"))
	ttip, err := tron.Tip(ctx)
	if err != nil || ttip == 0 {
		t.Fatalf("tron tip %v", err)
	}
	nileUSDT := Token{"USDT", "TXYZopYRdj2D9XRtbG411XZZ3kM5VkAeBf", 6}
	if _, err := tron.TokenBalance(ctx, nileUSDT, addr(ChainTron)); err != nil {
		t.Fatalf("tron balanceOf: %v", err)
	}
	if _, _, err := tron.Incoming(ctx, []string{addr(ChainTron)}, &nileUSDT, 0); err != nil {
		t.Fatalf("tron incoming: %v", err)
	}
	t.Logf("tron nile tip=%d addr=%s", ttip, addr(ChainTron))
	btc := NewBTCClient("https://blockstream.info/testnet/api", "tb")
	btip, err := btc.Tip(ctx)
	if err != nil {
		t.Fatalf("btc tip %v", err)
	}
	if _, err := btc.TransferCost(ctx, nil); err != nil {
		t.Fatalf("btc fee %v", err)
	}
	if _, _, err := btc.Incoming(ctx, []string{addr(ChainBTC)}, nil, 0); err != nil {
		t.Fatalf("btc incoming %v", err)
	}
	t.Logf("btc testnet tip=%d addr=%s", btip, addr(ChainBTC))
	sol := NewSolClient("https://api.devnet.solana.com")
	stip, err := sol.Tip(ctx)
	if err != nil {
		t.Fatalf("sol slot %v", err)
	}
	devUSDC := Token{"USDC", "4zMMC9srt5Ri5X14GAgXhaHii3GnPAEERYPJgZJDncDU", 6}
	if _, err := sol.TokenBalance(ctx, devUSDC, addr(ChainSol)); err != nil {
		t.Fatalf("sol token balance %v", err)
	}
	if _, _, err := sol.Incoming(ctx, []string{addr(ChainSol)}, nil, stip); err != nil {
		t.Fatalf("sol incoming %v", err)
	}
	t.Logf("sol devnet slot=%d addr=%s", stip, addr(ChainSol))
}
