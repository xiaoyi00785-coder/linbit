package main

// chain.go：链适配接口，以及只给开发用的 MockChain（高度和待入账交易都在库里）。
// 真实节点、RPC 和签名不在这里。真实模式见 realchain.go：chainkit RPC + 外部签名进程。

import (
	"context"
	"crypto/sha256"
	"encoding/hex"
	"fmt"

	"github.com/shopspring/decimal"
)

// ChainDeposit 是扫块看到的一笔充值输出。金额是 decimal，不是 float。
type ChainDeposit struct {
	Network       string
	Address       string
	Asset         string
	Amount        decimal.Decimal
	TxHash        string
	LogIndex      int64
	Confirmations int64
}

// ChainAdapter 抽象地址、Gas 估算、广播、确认数和扫块。MockChain 与 RealChain 都实现它。
type ChainAdapter interface {
	Kind() string
	// NewAddress 给用户在该网络上派生可复用的充值地址。真实链改走 AllocateAddress。
	NewAddress(net Network, userID string) (address, memo string)
	// EstimateGas 返回该网络当前的链上 Gas 估计。mock 直接用配置值。
	EstimateGas(ctx context.Context, net Network) (decimal.Decimal, error)
	// Broadcast 提交已签名交易，返回哈希和所在高度。真实链不用这条，改走 SendWithdrawal。
	Broadcast(ctx context.Context, net Network, signed SignedTx) (txHash string, includedHeight int64, err error)
	// Confirmations 返回自 sinceHeight 起的确认数。
	Confirmations(ctx context.Context, network string, sinceHeight int64) (int64, error)
	// Scan 返回该网络上确认数已达到阈值的充值。
	Scan(ctx context.Context, network string, confirmations int) ([]ChainDeposit, error)
}

// MockChain 是开发用模拟链，数据在钱包库里。主网拒绝 WALLET_CHAIN=mock。
type MockChain struct {
	store *Store
}

// NewMockChain 用给定存储构造模拟链。
func NewMockChain(store *Store) *MockChain { return &MockChain{store: store} }

// Kind 返回 "mock"。
func (c *MockChain) Kind() string { return "mock" }

// NewAddress 用用户和网络做确定性哈希，加上地址前缀。同一用户稳定复用。真实链不走这里。
func (c *MockChain) NewAddress(net Network, userID string) (string, string) {
	sum := sha256.Sum256([]byte(net.Network + "|" + net.Asset + "|" + userID))
	body := hex.EncodeToString(sum[:])[:34]
	prefix := net.AddressPrefix
	if prefix == "" {
		prefix = "addr"
	}
	return prefix + body, ""
}

// EstimateGas 返回配置里的 Gas。mock 保持稳定，方便测试复现。
func (c *MockChain) EstimateGas(_ context.Context, net Network) (decimal.Decimal, error) {
	return net.GasFeeEstimate, nil
}

// Broadcast 把签名交易“挖”进下一块，返回哈希和高度。并不上真实链。
func (c *MockChain) Broadcast(ctx context.Context, net Network, signed SignedTx) (string, int64, error) {
	height, err := c.store.MineBlock(ctx, net.Network)
	if err != nil {
		return "", 0, err
	}
	sum := sha256.Sum256([]byte(signed.Raw))
	txHash := "0x" + hex.EncodeToString(sum[:])
	return txHash, height, nil
}

// Confirmations 返回 currentHeight - sinceHeight + 1；还没挖到则为 0。
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

// Scan 返回该网络上确认数已达标、且打到已分配地址的模拟充值。
func (c *MockChain) Scan(ctx context.Context, network string, confirmations int) ([]ChainDeposit, error) {
	return c.store.ConfirmableChainTxs(ctx, network, confirmations)
}

// InjectDeposit 记一笔模拟充值并挖进块里。仅开发/测试喂扫块器用。
// 按 (network, txHash, logIndex) 幂等，重复注入不另挖块。
func (c *MockChain) InjectDeposit(ctx context.Context, network, address, asset string, amount decimal.Decimal, txHash string, logIndex int64) (bool, error) {
	return c.store.InjectChainTx(ctx, network, address, asset, amount, txHash, logIndex)
}

// Advance 在该网络上挖 blocks 个空块，返回新高度。仅开发用来凑确认数。
func (c *MockChain) Advance(ctx context.Context, network string, blocks int64) (int64, error) {
	return c.store.AdvanceChain(ctx, network, blocks)
}

var _ ChainAdapter = (*MockChain)(nil)

// shortHash 给日志用的短哈希，不是链上交易号。
func shortHash(s string) string {
	sum := sha256.Sum256([]byte(s))
	return fmt.Sprintf("%x", sum[:6])
}
