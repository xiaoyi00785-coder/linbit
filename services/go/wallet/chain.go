package main

import (
	"context"
	"crypto/sha256"
	"encoding/hex"
	"fmt"

	"github.com/shopspring/decimal"
)

// chain.go is the pluggable chain-adapter layer. ChainAdapter abstracts every
// on-chain interaction the wallet needs; MockChain implements it against an
// in-database simulated chain (block height + a table of pending deposit
// transactions). No real node, RPC, or key material is involved — deposits and
// new blocks are produced only through the dev-only endpoints.

// ChainDeposit is one simulated on-chain deposit output observed by the scanner.
type ChainDeposit struct {
	Network       string
	Address       string
	Asset         string
	Amount        decimal.Decimal
	TxHash        string
	LogIndex      int64
	Confirmations int64
}

// ChainAdapter abstracts address generation, gas estimation, broadcasting,
// confirmation lookups and deposit scanning.
type ChainAdapter interface {
	Kind() string
	// NewAddress derives a (reusable) deposit address for a user on a network.
	NewAddress(net Network, userID string) (address, memo string)
	// EstimateGas returns the current on-chain gas estimate for a network.
	EstimateGas(ctx context.Context, net Network) (decimal.Decimal, error)
	// Broadcast submits a signed transaction, returning its hash and the height
	// of the block it was included in.
	Broadcast(ctx context.Context, net Network, signed SignedTx) (txHash string, includedHeight int64, err error)
	// Confirmations reports how many confirmations a tx included at sinceHeight has.
	Confirmations(ctx context.Context, network string, sinceHeight int64) (int64, error)
	// Scan returns deposits on a network that now have at least `confirmations`.
	Scan(ctx context.Context, network string, confirmations int) ([]ChainDeposit, error)
}

// MockChain is the local simulated-chain adapter backed by the wallet database.
type MockChain struct {
	store *Store
}

// NewMockChain builds a MockChain over the given store.
func NewMockChain(store *Store) *MockChain { return &MockChain{store: store} }

// Kind implements ChainAdapter.
func (c *MockChain) Kind() string { return "mock" }

// NewAddress derives a deterministic, reusable address from the user + network.
// A real adapter would call the node / HD-wallet; here it is a hash with the
// network's address prefix so the value looks plausible and is stable per user.
func (c *MockChain) NewAddress(net Network, userID string) (string, string) {
	sum := sha256.Sum256([]byte(net.Network + "|" + net.Asset + "|" + userID))
	body := hex.EncodeToString(sum[:])[:34]
	prefix := net.AddressPrefix
	if prefix == "" {
		prefix = "addr"
	}
	return prefix + body, ""
}

// EstimateGas returns the configured estimate. A real adapter would query the
// network's current gas price; the mock keeps it stable for deterministic tests.
func (c *MockChain) EstimateGas(_ context.Context, net Network) (decimal.Decimal, error) {
	return net.GasFeeEstimate, nil
}

// Broadcast "mines" the signed transaction into the next block and returns its
// hash and inclusion height.
func (c *MockChain) Broadcast(ctx context.Context, net Network, signed SignedTx) (string, int64, error) {
	height, err := c.store.MineBlock(ctx, net.Network)
	if err != nil {
		return "", 0, err
	}
	sum := sha256.Sum256([]byte(signed.Raw))
	txHash := "0x" + hex.EncodeToString(sum[:])
	return txHash, height, nil
}

// Confirmations returns currentHeight - sinceHeight + 1 (0 if not yet mined).
func (c *MockChain) Confirmations(ctx context.Context, network string, sinceHeight int64) (int64, error) {
	height, err := c.store.ChainHeight(ctx, network)
	if err != nil {
		return 0, err
	}
	if sinceHeight <= 0 || height < sinceHeight {
		return 0, nil
	}
	return height - sinceHeight + 1, nil
}

// Scan returns pending deposit txs on a network whose confirmations now meet the
// threshold.
func (c *MockChain) Scan(ctx context.Context, network string, confirmations int) ([]ChainDeposit, error) {
	return c.store.ConfirmableChainTxs(ctx, network, confirmations)
}

// InjectDeposit records a simulated incoming deposit and mines it into a block.
// Dev-only: it is how tests/smoke scripts feed the scanner. Idempotent on
// (network, txHash, logIndex).
func (c *MockChain) InjectDeposit(ctx context.Context, network, address, asset string, amount decimal.Decimal, txHash string, logIndex int64) (bool, error) {
	return c.store.InjectChainTx(ctx, network, address, asset, amount, txHash, logIndex)
}

// Advance mines `blocks` empty blocks on a network, returning the new height.
func (c *MockChain) Advance(ctx context.Context, network string, blocks int64) (int64, error) {
	return c.store.AdvanceChain(ctx, network, blocks)
}

var _ ChainAdapter = (*MockChain)(nil)

// shortHash returns a short deterministic hash for display/logging.
func shortHash(s string) string {
	sum := sha256.Sum256([]byte(s))
	return fmt.Sprintf("%x", sum[:6])
}
