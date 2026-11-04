package main

import (
	"bufio"
	"context"
	"errors"
	"fmt"
	"log"
	"os"
	"strconv"
	"strings"
	"time"

	"github.com/linbit/platform/services/go/common"
	"github.com/linbit/platform/services/go/wallet/chainkit"
	"github.com/shopspring/decimal"
)

// realchain.go is the ChainAdapter for real networks (WALLET_CHAIN=testnet or
// mainnet). Deposit addresses are derived from account xpubs only (the wallet
// service never holds a private key); every signature comes from the hot
// signer, which runs as a separate container (wallet-signer) behind an HTTP
// interface so it can later move to its own host unchanged.
//
// Deposit discovery (Sync) writes observed transfers into chain_txs at their
// real block heights and the tip into chain_state, so the existing Scanner
// credits them exactly as it does for the mock chain.

// AddressAllocator is implemented by adapters that allocate HD-indexed
// addresses (needs a context and can fail, unlike ChainAdapter.NewAddress).
type AddressAllocator interface {
	AllocateAddress(ctx context.Context, net Network) (addr, memo string, hdIndex *int64, err error)
}

// ChainSender is implemented by adapters that build, sign and broadcast a
// withdrawal themselves (instead of Signer + Broadcast).
type ChainSender interface {
	SendWithdrawal(ctx context.Context, net Network, to string, amount decimal.Decimal) (string, error)
}

// TxConfirmer reports confirmations of a broadcast tx by hash.
type TxConfirmer interface {
	TxConfirmations(ctx context.Context, network, txHash string) (confs int64, failed bool, err error)
}

// AddressValidator checks a withdrawal destination for a network.
type AddressValidator interface {
	ValidateAddress(network, addr string) error
}

// chainNet is one configured network (e.g. ERC20 = Sepolia in testnet mode).
type chainNet struct {
	name    string // registry network name (TRC20, ERC20, ...)
	chain   chainkit.Chain
	client  chainkit.Client
	native  string // native asset symbol (TRX, ETH, BNB, BTC, SOL)
	hrp     string
	tokens  map[string]*chainkit.Token // asset -> token (nil = native)
	hot     chainkit.KeyRef
	cold    string
	xpub    *chainkit.ExtKey // deposit account key (nil for SOL)
	solList []string         // SOL deposit addresses by index
}

// RealChain implements ChainAdapter against real networks.
type RealChain struct {
	mode   string
	store  *Store
	signer chainkit.HotSigner
	nets   map[string]*chainNet
}

type netDefaults struct {
	chain           chainkit.Chain
	native          string
	testRPC         string
	chainID         int64 // testnet chain id (EVM)
	mainChainID     int64
	testnetTokens   map[string]string // asset -> "contract:decimals"
	nativeAssetName string            // registry asset that is the native coin, if any
}

// Testnet defaults: TRON Nile, Sepolia, BSC testnet, Arbitrum Sepolia, BTC
// testnet and Solana devnet. Everything can be overridden from env.
var realNetDefaults = map[string]netDefaults{
	"TRC20": {chain: chainkit.ChainTron, native: "TRX", testRPC: "https://nile.trongrid.io",
		testnetTokens: map[string]string{"USDT": "TXYZopYRdj2D9XRtbG411XZZ3kM5VkAeBf:6"}},
	"ERC20": {chain: chainkit.ChainEVM, native: "ETH", testRPC: "https://ethereum-sepolia-rpc.publicnode.com",
		chainID: 11155111, mainChainID: 1, nativeAssetName: "ETH",
		testnetTokens: map[string]string{"USDC": "0x1c7D4B196Cb0C7B01d743Fbc6116a902379C7238:6",
			// Aave V3 Sepolia test USDT (mintable at app.aave.com testnet faucet).
			"USDT": "0xaA8E23Fb1079EA71e0a56F48a2aA51851D8433D0:6"}},
	"BEP20": {chain: chainkit.ChainEVM, native: "BNB", testRPC: "https://bsc-testnet-rpc.publicnode.com",
		chainID: 97, mainChainID: 56,
		testnetTokens: map[string]string{"USDT": "0x337610d27c682E347C9cD60BD4b3b107C9d34dDd:18"}},
	"ARBITRUM": {chain: chainkit.ChainEVM, native: "ETH", testRPC: "https://sepolia-rollup.arbitrum.io/rpc",
		chainID: 421614, mainChainID: 42161,
		testnetTokens: map[string]string{"USDC": "0x75faf114eafb1BDbe2F0316DF893fd58CE46AA4d:6"}},
	"BTC": {chain: chainkit.ChainBTC, native: "BTC", testRPC: "https://blockstream.info/testnet/api", nativeAssetName: "BTC"},
	"SOL": {chain: chainkit.ChainSol, native: "SOL", testRPC: "https://api.devnet.solana.com", nativeAssetName: "SOL",
		testnetTokens: map[string]string{"USDC": "4zMMC9srt5Ri5X14GAgXhaHii3GnPAEERYPJgZJDncDU:6"}},
}

func envKey(parts ...string) string { return strings.ToUpper(strings.Join(parts, "_")) }

func parseToken(asset, spec string) (*chainkit.Token, error) {
	i := strings.LastIndex(spec, ":")
	if i <= 0 {
		return nil, fmt.Errorf("token %s: want contract:decimals", asset)
	}
	d, err := strconv.Atoi(spec[i+1:])
	if err != nil || d < 0 || d > 36 {
		return nil, fmt.Errorf("token %s: bad decimals", asset)
	}
	return &chainkit.Token{Symbol: asset, Contract: spec[:i], Decimals: int32(d)}, nil
}

// NewRealChain configures every registry network from env. A network whose
// asset has no token contract (e.g. Sepolia USDT) is left disabled: addresses
// cannot be allocated and withdrawals fail with unknown network.
func NewRealChain(ctx context.Context, mode string, store *Store, reg NetworkRegistry, signer chainkit.HotSigner) (*RealChain, error) {
	// On mainnet testnet defaults (RPCs, chain IDs, token contracts) are never
	// used, whatever WALLET_CHAIN says.
	testnet := mode == "testnet" && !common.IsMainnet()
	rc := &RealChain{mode: mode, store: store, signer: signer, nets: map[string]*chainNet{}}
	xpubs := map[chainkit.Chain]*chainkit.ExtKey{}
	var solList []string
	for name, d := range realNetDefaults {
		nets := reg.byPair
		var assets []string
		for _, n := range nets {
			if n.Network == name {
				assets = append(assets, n.Asset)
			}
		}
		if len(assets) == 0 {
			continue
		}
		rpc := common.Env(envKey("WALLET_RPC", name), "")
		if rpc == "" && testnet {
			rpc = d.testRPC
		}
		if rpc == "" {
			return nil, fmt.Errorf("%s: WALLET_RPC_%s is required in %s mode", name, name, mode)
		}
		cn := &chainNet{name: name, chain: d.chain, native: d.native, tokens: map[string]*chainkit.Token{},
			cold: common.Env(envKey("WALLET_COLD_ADDRESS", name), "")}
		switch d.chain {
		case chainkit.ChainEVM:
			id := d.mainChainID
			if testnet {
				id = d.chainID
			}
			if v := common.EnvInt(envKey("WALLET_CHAINID", name), 0); v > 0 {
				id = int64(v)
			}
			cn.client = chainkit.NewEVMClient(rpc, id)
		case chainkit.ChainTron:
			cn.client = chainkit.NewTronClient(rpc, common.Env("WALLET_TRON_API_KEY", ""))
		case chainkit.ChainBTC:
			cn.hrp = "bc"
			if testnet {
				cn.hrp = "tb"
			}
			cn.client = chainkit.NewBTCClient(rpc, cn.hrp)
		case chainkit.ChainSol:
			cn.client = chainkit.NewSolClient(rpc)
		}
		for _, a := range assets {
			if a == d.nativeAssetName {
				cn.tokens[a] = nil
				continue
			}
			spec := common.Env(envKey("WALLET_TOKEN", a, name), "")
			if spec == "" {
				spec = tradfiTokenSpecs[pairKey(a, name)]
			}
			if spec == "" && testnet {
				spec = d.testnetTokens[a]
			}
			if spec == "" {
				log.Printf("wallet: %s/%s has no token contract (WALLET_TOKEN_%s_%s); disabled", a, name, a, name)
				continue
			}
			t, err := parseToken(a, spec)
			if err != nil {
				return nil, err
			}
			if err := chainkit.ValidateAddress(d.chain, t.Contract, cn.hrp); err != nil {
				return nil, fmt.Errorf("%s/%s contract: %w", a, name, err)
			}
			cn.tokens[a] = t
		}
		if cn.cold == "" && common.IsMainnet() {
			return nil, fmt.Errorf("%s: WALLET_COLD_ADDRESS_%s is required on mainnet (no HD-derived cold fallback)", name, name)
		}
		if cn.cold != "" {
			if err := chainkit.ValidateAddress(d.chain, cn.cold, cn.hrp); err != nil {
				return nil, fmt.Errorf("WALLET_COLD_ADDRESS_%s: %w", name, err)
			}
		}
		// Deposit key source: account xpub (EVM/TRON/BTC) or address list (SOL).
		if d.chain == chainkit.ChainSol {
			if solList == nil {
				l, err := readSolList(common.Env("WALLET_SOL_ADDRESSES_FILE", ""))
				if err != nil {
					return nil, err
				}
				solList = l
			}
			cn.solList = solList
		} else {
			if xpubs[d.chain] == nil {
				raw := common.Env(envKey("WALLET_XPUB", string(d.chain)), "")
				if raw == "" {
					return nil, fmt.Errorf("WALLET_XPUB_%s is required (export with: signer xpub)", strings.ToUpper(string(d.chain)))
				}
				k, err := chainkit.ParseExtKey(raw)
				if err != nil {
					return nil, fmt.Errorf("WALLET_XPUB_%s: %w", strings.ToUpper(string(d.chain)), err)
				}
				if k.IsPrivate() {
					return nil, fmt.Errorf("WALLET_XPUB_%s must be a public key (xpub/tpub), never an xprv", strings.ToUpper(string(d.chain)))
				}
				xpubs[d.chain] = k
			}
			cn.xpub = xpubs[d.chain]
		}
		rc.nets[name] = cn
	}
	if err := rc.bindSigner(ctx); err != nil {
		return nil, err
	}
	return rc, nil
}

func readSolList(path string) ([]string, error) {
	if path == "" {
		return nil, errors.New("WALLET_SOL_ADDRESSES_FILE is required for SOL (export with: signer sol-addresses)")
	}
	f, err := os.Open(path)
	if err != nil {
		return nil, err
	}
	defer f.Close()
	var out []string
	sc := bufio.NewScanner(f)
	for sc.Scan() {
		a := strings.TrimSpace(sc.Text())
		if a == "" || strings.HasPrefix(a, "#") {
			continue
		}
		if err := chainkit.ValidateAddress(chainkit.ChainSol, a, ""); err != nil {
			return nil, fmt.Errorf("sol address list line %d: %w", len(out)+1, err)
		}
		out = append(out, a)
	}
	return out, sc.Err()
}

// bindSigner resolves hot addresses and checks the xpub / SOL list matches the
// signer's keystore (index 0), so a mismatched config can never hand out
// addresses whose keys nobody holds.
func (rc *RealChain) bindSigner(ctx context.Context) error {
	for _, cn := range rc.nets {
		hotPub, err := rc.signer.PublicKey(ctx, cn.chain, chainkit.BranchHot, 0)
		if err != nil {
			return fmt.Errorf("signer: %w", err)
		}
		cn.hot.Branch, cn.hot.Index = chainkit.BranchHot, 0
		if cn.hot.Address, err = chainkit.AddressFromPub(cn.chain, hotPub, cn.hrp); err != nil {
			return err
		}
		dep0, err := rc.signer.PublicKey(ctx, cn.chain, chainkit.BranchDeposit, 0)
		if err != nil {
			return err
		}
		want, _ := chainkit.AddressFromPub(cn.chain, dep0, cn.hrp)
		got, err := cn.depositAddress(0)
		if err != nil {
			return err
		}
		if got != want {
			return fmt.Errorf("%s: deposit key source (xpub / SOL list) does not match the signer keystore", cn.name)
		}
		// No multisig for now: unless an explicit WALLET_COLD_ADDRESS_<NET> is
		// set, excess above the hot cap goes to a reserve address derived from
		// the same HD seed (branch 2, index 0). WALLET_COLD_FROM_HD=false
		// disables it (everything stays in the hot wallet).
		if cn.cold == "" && !common.IsMainnet() && !strings.EqualFold(common.Env("WALLET_COLD_FROM_HD", "true"), "false") {
			rp, err := rc.signer.PublicKey(ctx, cn.chain, chainkit.BranchReserve, 0)
			if err != nil {
				return fmt.Errorf("signer reserve key: %w", err)
			}
			if cn.cold, err = chainkit.AddressFromPub(cn.chain, rp, cn.hrp); err != nil {
				return err
			}
		}
		log.Printf("wallet: %s (%s) hot=%s cold/reserve=%q", cn.name, cn.chain, cn.hot.Address, cn.cold)
	}
	return nil
}

func (cn *chainNet) depositAddress(i int64) (string, error) {
	if i < 0 || i >= 1<<31 {
		return "", errors.New("hd index out of range")
	}
	if cn.chain == chainkit.ChainSol {
		if int(i) >= len(cn.solList) {
			return "", fmt.Errorf("SOL deposit address pool exhausted (%d); export more with signer sol-addresses", len(cn.solList))
		}
		return cn.solList[i], nil
	}
	k, err := cn.xpub.Derive(chainkit.BranchDeposit, uint32(i))
	if err != nil {
		return "", err
	}
	return chainkit.AddressFromPub(cn.chain, k.PubKey(), cn.hrp)
}

func (rc *RealChain) lookup(network, asset string) (*chainNet, *chainkit.Token, error) {
	cn := rc.nets[network]
	if cn == nil {
		return nil, nil, fmt.Errorf("network %s not configured", network)
	}
	t, ok := cn.tokens[asset]
	if !ok {
		return nil, nil, fmt.Errorf("%s/%s not enabled", asset, network)
	}
	return cn, t, nil
}

// Kind implements ChainAdapter.
func (rc *RealChain) Kind() string { return rc.mode }

// NewAddress implements ChainAdapter; real addresses go through AllocateAddress.
func (rc *RealChain) NewAddress(Network, string) (string, string) { return "", "" }

// AllocateAddress implements AddressAllocator.
func (rc *RealChain) AllocateAddress(ctx context.Context, net Network) (string, string, *int64, error) {
	cn, _, err := rc.lookup(net.Network, net.Asset)
	if err != nil {
		return "", "", nil, err
	}
	idx, err := rc.store.NextHDIndex(ctx)
	if err != nil {
		return "", "", nil, err
	}
	addr, err := cn.depositAddress(idx)
	return addr, "", &idx, err
}

// EstimateGas implements ChainAdapter: the configured user-facing gas charge.
func (rc *RealChain) EstimateGas(_ context.Context, net Network) (decimal.Decimal, error) {
	return net.GasFeeEstimate, nil
}

// Broadcast is not used for real chains (see SendWithdrawal).
func (rc *RealChain) Broadcast(context.Context, Network, SignedTx) (string, int64, error) {
	return "", 0, errors.New("real chain: use SendWithdrawal")
}

// Confirmations implements ChainAdapter from the synced tip.
func (rc *RealChain) Confirmations(ctx context.Context, network string, since int64) (int64, error) {
	h, err := rc.store.ChainHeight(ctx, network)
	if err != nil || since <= 0 || h < since {
		return 0, err
	}
	return h - since + 1, nil
}

// Scan implements ChainAdapter.
func (rc *RealChain) Scan(ctx context.Context, network string, confirmations int) ([]ChainDeposit, error) {
	return rc.store.ConfirmableChainTxs(ctx, network, confirmations)
}

// ValidateAddress implements AddressValidator.
func (rc *RealChain) ValidateAddress(network, addr string) error {
	cn := rc.nets[network]
	if cn == nil {
		return fmt.Errorf("network %s not configured", network)
	}
	return chainkit.ValidateAddress(cn.chain, addr, cn.hrp)
}

// SendWithdrawal implements ChainSender: pays from the hot wallet.
func (rc *RealChain) SendWithdrawal(ctx context.Context, net Network, to string, amount decimal.Decimal) (string, error) {
	cn, tok, err := rc.lookup(net.Network, net.Asset)
	if err != nil {
		return "", err
	}
	if amount.Sign() <= 0 { // 0 means "sweep all" to the clients
		return "", errors.New("withdrawal amount must be positive")
	}
	if err := chainkit.ValidateAddress(cn.chain, to, cn.hrp); err != nil {
		return "", err
	}
	return cn.client.Send(ctx, rc.signer, cn.hot, to, tok, amount)
}

// TxConfirmations implements TxConfirmer.
func (rc *RealChain) TxConfirmations(ctx context.Context, network, txHash string) (int64, bool, error) {
	cn := rc.nets[network]
	if cn == nil {
		return 0, false, fmt.Errorf("network %s not configured", network)
	}
	st, err := cn.client.Status(ctx, txHash)
	if err != nil || !st.Found {
		return 0, false, err
	}
	if !st.Success {
		return 0, true, nil
	}
	tip, err := cn.client.Tip(ctx)
	if err != nil || st.Height <= 0 || tip < st.Height {
		return 0, false, err
	}
	return tip - st.Height + 1, false, nil
}

// Sync discovers incoming deposits on every enabled (network, asset) and
// records them plus the current tip. Errors on one network do not stop others.
func (rc *RealChain) Sync(ctx context.Context) (int, error) {
	found := 0
	var firstErr error
	for _, cn := range rc.nets {
		tip, err := cn.client.Tip(ctx)
		if err != nil {
			log.Printf("sync %s: tip: %v", cn.name, err)
			firstErr = errors.Join(firstErr, err)
			continue
		}
		for asset, tok := range cn.tokens {
			n, err := rc.syncAsset(ctx, cn, asset, tok, tip)
			found += n
			if err != nil {
				log.Printf("sync %s/%s: %v", asset, cn.name, err)
				firstErr = errors.Join(firstErr, err)
			}
		}
		if err := rc.store.SetChainHeight(ctx, cn.name, tip); err != nil {
			return found, err
		}
	}
	return found, firstErr
}

func (rc *RealChain) syncAsset(ctx context.Context, cn *chainNet, asset string, tok *chainkit.Token, tip int64) (int, error) {
	addrs, err := rc.store.DepositAddressesFor(ctx, cn.name, asset)
	if err != nil || len(addrs) == 0 {
		return 0, err
	}
	from, ok, err := rc.store.Cursor(ctx, cn.name, asset)
	if err != nil {
		return 0, err
	}
	if !ok {
		from = 0 // client picks a sensible start (recent blocks / full history)
	}
	in, next, err := cn.client.Incoming(ctx, addrs, tok, from)
	if err != nil {
		return 0, err
	}
	n := 0
	for _, t := range in {
		if t.Amount.Sign() <= 0 {
			continue
		}
		if t.Height <= 0 { // unconfirmed: pick it up on a later pass
			continue
		}
		ins, err := rc.store.RecordChainTx(ctx, cn.name, t.Address, asset, t.Amount, t.TxHash, t.LogIndex, t.Height)
		if err != nil {
			return n, err
		}
		if ins {
			n++
		}
	}
	return n, rc.store.SetCursor(ctx, cn.name, asset, next)
}

// ---- sweeps ----

// sweepDestination returns the hot address, or the cold address once the hot
// wallet already holds more than WALLET_HOT_CAP_<ASSET>_<NETWORK>.
func (rc *RealChain) sweepDestination(ctx context.Context, cn *chainNet, asset string, tok *chainkit.Token) string {
	if cn.cold == "" {
		return cn.hot.Address
	}
	capStr := common.Env(envKey("WALLET_HOT_CAP", asset, cn.name), "")
	if capStr == "" {
		return cn.hot.Address
	}
	capAmt, err := decimal.NewFromString(capStr)
	if err != nil {
		return cn.hot.Address
	}
	bal, err := rc.balance(ctx, cn, tok, cn.hot.Address)
	if err == nil && bal.GreaterThan(capAmt) {
		return cn.cold
	}
	return cn.hot.Address
}

func (rc *RealChain) balance(ctx context.Context, cn *chainNet, tok *chainkit.Token, addr string) (decimal.Decimal, error) {
	if tok == nil {
		return cn.client.NativeBalance(ctx, addr)
	}
	return cn.client.TokenBalance(ctx, *tok, addr)
}

// Sweep advances active sweeps and starts new ones for credited deposit
// addresses. Token sweeps on EVM/TRON first top up the deposit address with
// native gas from the hot wallet; SOL sweeps are fee-sponsored by the hot
// wallet; native BTC/ETH/TRX/SOL sweep the whole balance net of fees.
func (rc *RealChain) Sweep(ctx context.Context, minFor func(asset, network string) decimal.Decimal) (int, error) {
	active, err := rc.store.ActiveSweeps(ctx, 100)
	if err != nil {
		return 0, err
	}
	for _, sw := range active {
		rc.advanceSweep(ctx, sw)
	}
	cands, err := rc.store.SweepCandidates(ctx, 50)
	if err != nil {
		return 0, err
	}
	started := 0
	for _, c := range cands {
		cn, tok, err := rc.lookup(c.Network, c.Asset)
		if err != nil {
			continue
		}
		bal, err := rc.balance(ctx, cn, tok, c.Address)
		if err != nil {
			log.Printf("sweep %s %s: balance: %v", c.Network, c.Address, err)
			continue
		}
		if bal.Sign() <= 0 || bal.LessThan(minFor(c.Asset, c.Network)) {
			continue
		}
		if err := rc.startSweep(ctx, cn, tok, c, bal); err != nil {
			log.Printf("sweep %s %s: %v", c.Network, c.Address, err)
			continue
		}
		started++
	}
	return started, nil
}

func (rc *RealChain) startSweep(ctx context.Context, cn *chainNet, tok *chainkit.Token, c SweepCandidate, bal decimal.Decimal) error {
	sw := Sweep{Network: c.Network, Asset: c.Asset, Address: c.Address, HDIndex: c.HDIndex}
	needsGas := tok != nil && (cn.chain == chainkit.ChainEVM || cn.chain == chainkit.ChainTron)
	if needsGas {
		cost, err := cn.client.TransferCost(ctx, tok)
		if err != nil {
			return err
		}
		have, err := cn.client.NativeBalance(ctx, c.Address)
		if err != nil {
			return err
		}
		if have.LessThan(cost) {
			topup := cost.Mul(decimal.RequireFromString("1.2")).Sub(have)
			hash, err := cn.client.Send(ctx, rc.signer, cn.hot, c.Address, nil, topup)
			if err != nil {
				return fmt.Errorf("gas top-up: %w", err)
			}
			sw.Stage, sw.TopupTx = sweepTopupSent, hash
			_, err = rc.store.InsertSweep(ctx, sw)
			log.Printf("sweep %s %s: gas top-up %s %s tx=%s", c.Network, c.Address, topup, cn.native, hash)
			return err
		}
	}
	// Reserve the row first (unique active index) so two workers cannot both send.
	sw.Stage = sweepSent
	id, err := rc.store.InsertSweep(ctx, sw)
	if err != nil {
		return err
	}
	return rc.sendSweep(ctx, cn, tok, Sweep{ID: id, Network: c.Network, Asset: c.Asset, Address: c.Address, HDIndex: c.HDIndex}, bal)
}

func (rc *RealChain) sendSweep(ctx context.Context, cn *chainNet, tok *chainkit.Token, sw Sweep, bal decimal.Decimal) error {
	dest := rc.sweepDestination(ctx, cn, sw.Asset, tok)
	from := chainkit.KeyRef{Branch: chainkit.BranchDeposit, Index: uint32(sw.HDIndex), Address: sw.Address}
	amount := bal
	if tok == nil {
		amount = decimal.Zero // native: whole balance minus fee
	}
	var hash string
	var err error
	if sc, ok := cn.client.(*chainkit.SolClient); ok {
		hash, err = sc.SendSponsored(ctx, rc.signer, cn.hot, from, dest, tok, amount)
	} else {
		hash, err = cn.client.Send(ctx, rc.signer, from, dest, tok, amount)
	}
	if err != nil {
		_ = rc.store.UpdateSweep(ctx, sw.ID, sweepFailed, "", err.Error(), bal, dest)
		return err
	}
	log.Printf("sweep %s %s -> %s tx=%s", sw.Network, sw.Address, dest, hash)
	return rc.store.UpdateSweep(ctx, sw.ID, sweepSent, hash, "", bal, dest)
}

func (rc *RealChain) advanceSweep(ctx context.Context, sw Sweep) {
	cn, tok, err := rc.lookup(sw.Network, sw.Asset)
	if err != nil {
		return
	}
	switch sw.Stage {
	case sweepTopupSent:
		st, err := cn.client.Status(ctx, sw.TopupTx)
		if err != nil || !st.Found {
			return
		}
		if !st.Success {
			_ = rc.store.UpdateSweep(ctx, sw.ID, sweepFailed, "", "gas top-up tx failed", decimal.Zero, "")
			return
		}
		bal, err := rc.balance(ctx, cn, tok, sw.Address)
		if err != nil || bal.Sign() <= 0 {
			return
		}
		if err := rc.store.UpdateSweep(ctx, sw.ID, sweepSent, "", "", bal, ""); err != nil {
			return
		}
		if err := rc.sendSweep(ctx, cn, tok, sw, bal); err != nil {
			log.Printf("sweep %d: %v", sw.ID, err)
		}
	case sweepSent:
		if sw.SweepTx == "" {
			// Claimed but the send result was never recorded; leave for ops after 1h.
			if time.Since(sw.UpdatedAt) > time.Hour {
				_ = rc.store.UpdateSweep(ctx, sw.ID, sweepFailed, "", "no tx recorded; check on chain", decimal.Zero, "")
			}
			return
		}
		st, err := cn.client.Status(ctx, sw.SweepTx)
		if err != nil || !st.Found {
			return
		}
		stage, msg := sweepDone, ""
		if !st.Success {
			stage, msg = sweepFailed, "sweep tx failed"
		}
		_ = rc.store.UpdateSweep(ctx, sw.ID, stage, "", msg, decimal.Zero, "")
	}
}

// HotWallets reports hot/cold addresses and hot balances (admin/ops view).
func (rc *RealChain) HotWallets(ctx context.Context) []map[string]string {
	var out []map[string]string
	for _, cn := range rc.nets {
		row := map[string]string{"network": cn.name, "chain": string(cn.chain), "hot": cn.hot.Address, "cold": cn.cold}
		if b, err := cn.client.NativeBalance(ctx, cn.hot.Address); err == nil {
			row[cn.native] = b.String()
		}
		for a, t := range cn.tokens {
			if t == nil {
				continue
			}
			if b, err := cn.client.TokenBalance(ctx, *t, cn.hot.Address); err == nil {
				row[a] = b.String()
			}
		}
		out = append(out, row)
	}
	return out
}

var _ ChainAdapter = (*RealChain)(nil)
