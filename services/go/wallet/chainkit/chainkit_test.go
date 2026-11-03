package chainkit

import (
	"context"
	"crypto/ed25519"
	"encoding/hex"
	"encoding/json"
	"io"
	"math/big"
	"net/http"
	"net/http/httptest"
	"path/filepath"
	"strings"
	"testing"

	"github.com/decred/dcrd/dcrec/secp256k1/v4"
	"github.com/decred/dcrd/dcrec/secp256k1/v4/ecdsa"
	"github.com/shopspring/decimal"
)

func init() { scryptN = 1 << 10 } // fast tests

// rawKeySigner signs with a fixed test key (never used outside tests).
type rawKeySigner struct{ priv *secp256k1.PrivateKey }

func (r rawKeySigner) PublicKey(context.Context, Chain, uint32, uint32) ([]byte, error) {
	return r.priv.PubKey().SerializeCompressed(), nil
}
func (r rawKeySigner) Sign(_ context.Context, req SignRequest) ([]byte, error) {
	switch req.Chain {
	case ChainEVM:
		return compactToRSV(ecdsa.SignCompact(r.priv, Keccak256(req.Payload), false), 0), nil
	case ChainBTC:
		return ecdsa.Sign(r.priv, dsha256(req.Payload)).Serialize(), nil
	}
	return nil, nil
}

func keyFromHex(t *testing.T, h string) *secp256k1.PrivateKey {
	b, err := hex.DecodeString(h)
	if err != nil {
		t.Fatal(err)
	}
	return secp256k1.PrivKeyFromBytes(b)
}

func TestBIP32PublicDerivationVector1(t *testing.T) {
	seed, _ := hex.DecodeString("000102030405060708090a0b0c0d0e0f")
	m, err := NewMaster(seed, false)
	if err != nil {
		t.Fatal(err)
	}
	if got := m.Neuter().String(); got != "xpub661MyMwAqRbcFtXgS5sYJABqqG9YLmC4Q1Rdap9gSE8NqtwybGhePY2gZ29ESFjqJoCu1Rupje8YtGqsefD265TMg7usUDFdp6W1EGMcet8" {
		t.Fatalf("master xpub %s", got)
	}
	k, _ := m.Derive(0+HardenedOffset, 1, 2+HardenedOffset)
	xpub := k.Neuter().String()
	if xpub != "xpub6D4BDPcP2GT577Vvch3R8wDkScZWzQzMMUm3PWbmWvVJrZwQY4VUNgqFJPMM3No2dFDFGTsxxpG5uJh7n7epu4trkrX7x7DogT5Uv6fcLW5" {
		t.Fatalf("m/0'/1/2' xpub %s", xpub)
	}
	pub, err := ParseExtKey(xpub)
	if err != nil {
		t.Fatal(err)
	}
	c, err := pub.Child(2)
	if err != nil {
		t.Fatal(err)
	}
	if c.String() != "xpub6FHa3pjLCk84BayeJxFW2SP4XRrFd1JYnxeLeU8EqN3vDfZmbqBqaGJAyiLjTAwm6ZLRQUMv1ZACTj37sR62cfN7fe5JnJ7dh8zL4fiyLHV" {
		t.Fatalf("public child %s", c.String())
	}
	if _, err := pub.Child(HardenedOffset); err == nil {
		t.Fatal("hardened from public must fail")
	}
}

func TestAddressVectors(t *testing.T) {
	pub := keyFromHex(t, strings.Repeat("0", 63)+"1").PubKey().SerializeCompressed()
	if a, _ := EVMAddress(pub); a != "0x7E5F4552091A69125d5DfCb7b8C2659029395Bdf" {
		t.Fatalf("evm %s", a)
	}
	if a, _ := BTCP2WPKH(pub, "bc"); a != "bc1qw508d6qejxtdg4y5r3zarvary0c5xw7kv8f3t4" {
		t.Fatalf("btc %s", a)
	}
	if a, _ := BTCP2WPKH(pub, "tb"); a != "tb1qw508d6qejxtdg4y5r3zarvary0c5xw7kxpjzsx" {
		t.Fatalf("tb %s", a)
	}
	ta, _ := TronAddress(pub)
	if !strings.HasPrefix(ta, "T") || ValidateAddress(ChainTron, ta, "") != nil {
		t.Fatalf("tron %s", ta)
	}
	if ValidateAddress(ChainEVM, "0x7E5F4552091A69125d5DfCb7b8C2659029395BdF", "") == nil {
		t.Fatal("bad EIP-55 checksum accepted")
	}
	if ValidateAddress(ChainBTC, "tb1qw508d6qejxtdg4y5r3zarvary0c5xw7kxpjzsy", "tb") == nil {
		t.Fatal("bad bech32 checksum accepted")
	}
	if ValidateAddress(ChainSol, "11111111111111111111111111111111", "") != nil {
		t.Fatal("system program address rejected")
	}
}

func TestEIP155Vector(t *testing.T) {
	gp, _ := new(big.Int).SetString("20000000000", 10)
	val, _ := new(big.Int).SetString("1000000000000000000", 10)
	to, _ := hex.DecodeString(strings.Repeat("35", 20))
	tx := EVMTx{Nonce: 9, GasPrice: gp, Gas: 21000, To: to, Value: val}
	if h := hex.EncodeToString(Keccak256(tx.SigningPayload(1))); h != "daf5a779ae972f972197303d7b574746c7ef83eadac0f2791ad23db92e4c8e53" {
		t.Fatalf("signing hash %s", h)
	}
	s := rawKeySigner{keyFromHex(t, strings.Repeat("46", 32))}
	sig, _ := s.Sign(context.Background(), SignRequest{Chain: ChainEVM, Payload: tx.SigningPayload(1)})
	raw, err := tx.Encode(1, sig)
	if err != nil {
		t.Fatal(err)
	}
	want := "f86c098504a817c800825208943535353535353535353535353535353535353535880de0b6b3a76400008025a028ef61340bd939bc2195fe537567866003e1a15d3c71ff63e1590620aa636276a067cbe9d8997f761aecb703304b3800ccf555c9f3dc64214b297fb1966a3b6d83"
	if hex.EncodeToString(raw) != want {
		t.Fatalf("raw %x", raw)
	}
}

func TestKeystoreXpubMatchesSigner(t *testing.T) {
	path := filepath.Join(t.TempDir(), "hot.json")
	pass := []byte("correct horse battery")
	if _, err := CreateKeystore(path, []byte("short"), true); err == nil {
		t.Fatal("short passphrase accepted")
	}
	if _, err := CreateKeystore(path, pass, true); err != nil {
		t.Fatal(err)
	}
	if _, err := CreateKeystore(path, pass, true); err == nil {
		t.Fatal("overwrote an existing keystore")
	}
	if _, err := OpenKeystore(path, []byte("wrong passphrase!!")); err == nil {
		t.Fatal("wrong passphrase accepted")
	}
	ks, err := OpenKeystore(path, pass)
	if err != nil {
		t.Fatal(err)
	}
	if strings.Contains(ks.String(), "seed") || len(ks.String()) > 40 {
		t.Fatal("keystore String leaks")
	}
	signer := NewLocalHotSigner(ks)
	ctx := context.Background()
	for _, c := range []Chain{ChainEVM, ChainTron, ChainBTC} {
		x, err := ks.AccountXpub(c)
		if err != nil || !strings.HasPrefix(x, "tpub") {
			t.Fatalf("%s xpub %q %v", c, x, err)
		}
		acct, _ := ParseExtKey(x)
		for _, i := range []uint32{0, 7} {
			k, _ := acct.Derive(BranchDeposit, i)
			fromXpub, _ := AddressFromPub(c, k.PubKey(), "tb")
			pk, _ := signer.PublicKey(ctx, c, BranchDeposit, i)
			fromSigner, _ := AddressFromPub(c, pk, "tb")
			if fromXpub != fromSigner {
				t.Fatalf("%s/%d: xpub %s != signer %s", c, i, fromXpub, fromSigner)
			}
		}
	}
	dep, hot, err := ks.SolAddresses(3)
	if err != nil || len(dep) != 3 || dep[0] == dep[1] || hot == dep[0] {
		t.Fatalf("sol addresses %v %s %v", dep, hot, err)
	}
	pk, _ := signer.PublicKey(ctx, ChainSol, BranchDeposit, 2)
	if SolAddress(ed25519.PublicKey(pk)) != dep[2] {
		t.Fatal("sol signer key mismatch")
	}
	sig, _ := signer.Sign(ctx, SignRequest{Chain: ChainSol, Branch: BranchDeposit, Index: 2, Payload: []byte("m")})
	if !ed25519.Verify(pk, []byte("m"), sig) {
		t.Fatal("sol signature invalid")
	}
	// EVM signature recovers to the derived address.
	payload := []byte("payload")
	sig, _ = signer.Sign(ctx, SignRequest{Chain: ChainEVM, Branch: BranchHot, Index: 0, Payload: payload})
	compact := append([]byte{27 + sig[64] + 4}, sig[:64]...)
	rec, _, err := ecdsa.RecoverCompact(compact, Keccak256(payload))
	hp, _ := signer.PublicKey(ctx, ChainEVM, BranchHot, 0)
	if err != nil || hex.EncodeToString(rec.SerializeCompressed()) != hex.EncodeToString(hp) {
		t.Fatal("evm signature does not recover to hot key")
	}
	// TRON uses v = 27 + recid.
	sig, _ = signer.Sign(ctx, SignRequest{Chain: ChainTron, Payload: payload})
	if sig[64] != 27 && sig[64] != 28 {
		t.Fatalf("tron v %d", sig[64])
	}
}

func TestRemoteSignerAuth(t *testing.T) {
	ks, err := CreateKeystore(filepath.Join(t.TempDir(), "k.json"), []byte("correct horse battery"), true)
	if err != nil {
		t.Fatal(err)
	}
	srv := httptest.NewServer(SignerHandler(NewLocalHotSigner(ks), "tok"))
	defer srv.Close()
	ctx := context.Background()
	if _, err := NewRemoteHotSigner(srv.URL, "bad").PublicKey(ctx, ChainEVM, 0, 0); err == nil {
		t.Fatal("bad token accepted")
	}
	r := NewRemoteHotSigner(srv.URL, "tok")
	pk, err := r.PublicKey(ctx, ChainEVM, 0, 1)
	local, _ := NewLocalHotSigner(ks).PublicKey(ctx, ChainEVM, 0, 1)
	if err != nil || hex.EncodeToString(pk) != hex.EncodeToString(local) {
		t.Fatalf("remote pubkey %v", err)
	}
	sig, err := r.Sign(ctx, SignRequest{Chain: ChainBTC, Payload: []byte("x")})
	if err != nil || len(sig) < 8 {
		t.Fatalf("remote sign %v", err)
	}
	if _, err := r.Sign(ctx, SignRequest{Chain: "doge", Payload: []byte("x")}); err == nil {
		t.Fatal("unknown chain signed")
	}
}

func TestBTCBuildAndSign(t *testing.T) {
	priv := keyFromHex(t, strings.Repeat("11", 32))
	pub := priv.PubKey().SerializeCompressed()
	dest, _ := BTCScript("tb", "tb1qw508d6qejxtdg4y5r3zarvary0c5xw7kxpjzsx")
	ins := []BTCInput{{TxID: strings.Repeat("ab", 32), Vout: 1, Value: 50000, PubKey: pub}}
	outs := []BTCOutput{{Script: dest, Value: 40000}}
	pre, err := BIP143Preimage(ins, outs, 0)
	if err != nil || len(pre) != 4+32+32+36+26+8+4+32+4+4 {
		t.Fatalf("preimage len %d %v", len(pre), err)
	}
	sig, _ := rawKeySigner{priv}.Sign(context.Background(), SignRequest{Chain: ChainBTC, Payload: pre})
	parsed, err := ecdsa.ParseDERSignature(sig)
	if err != nil || !parsed.Verify(dsha256(pre), priv.PubKey()) {
		t.Fatal("btc signature does not verify")
	}
	raw, err := SerializeBTC(ins, outs, [][]byte{sig})
	if err != nil || raw[4] != 0 || raw[5] != 1 {
		t.Fatal("not a segwit serialization")
	}
}

func TestSolMessageLayout(t *testing.T) {
	payer := make([]byte, 32)
	payer[0] = 9
	owner := make([]byte, 32)
	owner[0] = 8
	to := make([]byte, 32)
	to[0] = 7
	msg, signers, err := SolMessage(payer, make([]byte, 32), []solIx{solTransferIx(owner, to, 5)})
	if err != nil {
		t.Fatal(err)
	}
	if msg[0] != 2 || msg[1] != 0 || msg[2] != 1 || len(signers) != 2 || signers[0][0] != 9 || signers[1][0] != 8 {
		t.Fatalf("header %v signers %d", msg[:3], len(signers))
	}
}

// fakeEVM is a minimal JSON-RPC node for Send/Incoming tests.
func fakeEVM(t *testing.T, sent *string) *httptest.Server {
	return httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		b, _ := io.ReadAll(r.Body)
		var req struct {
			ID     int64         `json:"id"`
			Method string        `json:"method"`
			Params []interface{} `json:"params"`
		}
		json.Unmarshal(b, &req)
		res := map[string]interface{}{
			"eth_getTransactionCount": "0x3", "eth_gasPrice": "0x3b9aca00", "eth_blockNumber": "0x64",
			"eth_estimateGas": "0xc350", "eth_getBalance": "0xde0b6b3a7640000",
			"eth_getLogs": []map[string]interface{}{{"transactionHash": "0xaa", "logIndex": "0x2", "blockNumber": "0x60",
				"topics": []string{erc20TransferTopic, "0x" + strings.Repeat("0", 64), "0x000000000000000000000000" + "7e5f4552091a69125d5dfcb7b8c2659029395bdf"},
				"data":   "0x" + strings.Repeat("0", 58) + "0f4240"}},
		}[req.Method]
		if req.Method == "eth_sendRawTransaction" {
			*sent = req.Params[0].(string)
			res = "0xhash"
		}
		json.NewEncoder(w).Encode(map[string]interface{}{"jsonrpc": "2.0", "id": req.ID, "result": res})
	}))
}

func TestEVMSendAndIncoming(t *testing.T) {
	var sent string
	srv := fakeEVM(t, &sent)
	defer srv.Close()
	c := NewEVMClient(srv.URL, 11155111)
	priv := keyFromHex(t, strings.Repeat("22", 32))
	from, _ := EVMAddress(priv.PubKey().SerializeCompressed())
	usdt := &Token{Symbol: "USDT", Contract: "0x7E5F4552091A69125d5DfCb7b8C2659029395Bdf", Decimals: 6}
	h, err := c.Send(context.Background(), rawKeySigner{priv}, KeyRef{Address: from}, "0x7E5F4552091A69125d5DfCb7b8C2659029395Bdf", usdt, decimal.RequireFromString("1.5"))
	if err != nil || h != "0xhash" || !strings.HasPrefix(sent, "0xf8") {
		t.Fatalf("send %s %v raw=%s", h, err, sent)
	}
	if !strings.Contains(sent, "a9059cbb") || !strings.Contains(sent, "16e360") { // 1.5e6
		t.Fatal("erc20 transfer data missing")
	}
	in, next, err := c.Incoming(context.Background(), []string{"0x7E5F4552091A69125d5DfCb7b8C2659029395Bdf"}, usdt, 90)
	if err != nil || len(in) != 1 || !in[0].Amount.Equal(decimal.NewFromInt(1)) || in[0].LogIndex != 2 || next != 101 {
		t.Fatalf("incoming %+v next=%d err=%v", in, next, err)
	}
	if _, err := c.Send(context.Background(), rawKeySigner{priv}, KeyRef{Address: from}, "0x1", nil, decimal.NewFromInt(1)); err == nil {
		t.Fatal("bad destination accepted")
	}
}
