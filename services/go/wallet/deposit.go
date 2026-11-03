package main

import (
	"context"
	"fmt"
	"log"
)

// deposit.go implements deposit scanning and crediting. The scanner reads the
// simulated chain, refreshes confirmation counts, and credits a deposit exactly
// once it has enough confirmations. Idempotency is guaranteed on two levels:
// the (network, tx_hash, log_index) unique key locally, and the idempotent
// asset /internal/deposit endpoint (keyed on the same ref).

// defaultDepositConfirmations is used when a (asset, network) pair has no entry
// in the registry (should not happen for allocated addresses).
const defaultDepositConfirmations = 12

// Scanner credits confirmed deposits.
type Scanner struct {
	store          *Store
	nets           NetworkRegistry
	asset          *AssetClient
	creditBelowMin bool
}

// NewScanner builds a deposit Scanner.
func NewScanner(store *Store, nets NetworkRegistry, asset *AssetClient, creditBelowMin bool) *Scanner {
	return &Scanner{store: store, nets: nets, asset: asset, creditBelowMin: creditBelowMin}
}

// depositRef is the idempotency key shared with the asset service.
func depositRef(network, txHash string, logIndex int64) string {
	return fmt.Sprintf("dep:%s:%s:%d", network, txHash, logIndex)
}

// ScanAll scans every network that currently has simulated transactions and
// returns the number of deposits credited this pass.
func (sc *Scanner) ScanAll(ctx context.Context) (int, error) {
	networks, err := sc.store.ActiveChainNetworks(ctx)
	if err != nil {
		return 0, err
	}
	total := 0
	for _, n := range networks {
		credited, err := sc.ScanNetwork(ctx, n)
		if err != nil {
			log.Printf("scan %s: %v", n, err)
			continue
		}
		total += credited
	}
	return total, nil
}

// ScanNetwork refreshes confirmations and credits any newly-confirmed deposits on
// one network. Returns the number credited this pass.
func (sc *Scanner) ScanNetwork(ctx context.Context, network string) (int, error) {
	txs, err := sc.store.MatchedChainTxs(ctx, network)
	if err != nil {
		return 0, err
	}
	credited := 0
	for _, t := range txs {
		status, _, err := sc.store.DepositStatus(ctx, network, t.TxHash, t.LogIndex)
		if err != nil {
			return credited, err
		}
		if status == depCompleted {
			continue // already credited; nothing to do
		}

		// Always record/refresh the confirming row so records show progress.
		if err := sc.store.UpsertDepositConfirming(ctx, Deposit{
			UserID: t.UserID, Asset: t.Asset, Network: network, Address: t.Address,
			Amount: t.Amount, TxHash: t.TxHash, LogIndex: t.LogIndex, Confirmations: t.Confirmations,
		}); err != nil {
			return credited, err
		}

		net, ok := sc.nets.Lookup(t.Asset, network)
		required := int64(defaultDepositConfirmations)
		if ok {
			required = int64(net.Confirmations)
		}
		if t.Confirmations < required {
			continue // not enough confirmations yet — do not credit
		}
		if ok && !sc.creditBelowMin && t.Amount.LessThan(net.MinDeposit) {
			log.Printf("deposit below min (%s %s < %s); not crediting tx=%s", t.Amount, t.Asset, net.MinDeposit, t.TxHash)
			continue
		}

		// Credit via the asset service's idempotent double-entry endpoint.
		ref := depositRef(network, t.TxHash, t.LogIndex)
		if err := sc.asset.Deposit(ctx, t.UserID, t.Asset, t.Amount, ref); err != nil {
			log.Printf("credit deposit %s: %v", ref, err)
			continue // retried next pass
		}
		applied, err := sc.store.MarkDepositCompleted(ctx, network, t.TxHash, t.LogIndex, t.Confirmations)
		if err != nil {
			return credited, err
		}
		if applied {
			credited++
		}
	}
	return credited, nil
}
