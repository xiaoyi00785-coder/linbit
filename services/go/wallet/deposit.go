package main

// deposit.go：扫 chain_txs 并入账。mock 注入和真实链 Sync 都写这张表。
// 确认数够了才入账，且只入一次。幂等两层：(network, tx_hash, log_index)，
// 以及资产服务 /internal/deposit，键为 dep:<network>:<tx>:<logIndex>。

import (
	"github.com/linbit/platform/services/go/common"

	"context"
	"fmt"
	"log"
)

// defaultDepositConfirmations：注册表没有该 (asset, network) 时用的确认数。已分配地址不应走到这里。
const defaultDepositConfirmations = 12

// Scanner 给确认数够了的充值入账。creditBelowMin 为假时，低于 MinDeposit 的只记账不入账。
type Scanner struct {
	store          *Store
	nets           NetworkRegistry
	asset          *AssetClient
	creditBelowMin bool
}

// NewScanner 组装扫块入账器。creditBelowMin 对应 WALLET_CREDIT_BELOW_MIN_DEPOSIT。
func NewScanner(store *Store, nets NetworkRegistry, asset *AssetClient, creditBelowMin bool) *Scanner {
	return &Scanner{store: store, nets: nets, asset: asset, creditBelowMin: creditBelowMin}
}

// depositRef 是和资产服务共用的幂等键：dep:<network>:<txHash>:<logIndex>。
func depositRef(network, txHash string, logIndex int64) string {
	return fmt.Sprintf("dep:%s:%s:%d", network, txHash, logIndex)
}

// ScanAll 扫当前有链上交易的每个网络，返回本轮新入账笔数。
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

// ScanNetwork 刷新一个网络的确认数，把新达到确认数的充值入账。返回本轮入账笔数。
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

		// 先记下确认中，充值记录才能看到进度。已完成的行不会被降级。
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

		// 经资产服务幂等入账。失败留到下一轮，同一 ref 不会记两次。
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
			common.Notify(common.Notice{UID: t.UserID, Kind: common.NoticeTrade, Title: "充值到账",
				Body: "您的 " + t.Amount.String() + " " + t.Asset + "（" + network + "）充值已到账。交易哈希：" + t.TxHash})
		}
	}
	return credited, nil
}
