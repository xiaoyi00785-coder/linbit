# tradfi_tokens 主网 runbook

`tradfi_tokens`（market-data 迁移 00006）只控制 tradfi 代币的**链上充提**：合约地址、精度、
deposit/withdraw 开关、最小提币与手续费。现货/合约交易配置在 `tradfi_assets`，本 runbook 不改它。

wallet 只在**启动时**加载 `contract_address<>'' AND (deposit_enabled OR withdraw_enabled)` 的行。
主网下还要求 `contract_verified`（迁移 00009）。改动后必须重启 wallet。

## 1. 查看
```sql
SELECT asset, issuer, network, contract_address, decimals, deposit_enabled, withdraw_enabled,
       contract_verified, verified_by, verified_at, updated_by, updated_at
FROM tradfi_tokens ORDER BY asset, issuer, network;
```

## 2. 关闭所有未验证代币的链上充提（不动 tradfi_assets）
```sql
BEGIN;
UPDATE tradfi_tokens
   SET deposit_enabled = false, withdraw_enabled = false,
       updated_by = 'mainnet-cutover', updated_at = now()
 WHERE NOT contract_verified AND (deposit_enabled OR withdraw_enabled);
-- 可选：清掉测试网留下的合约地址
UPDATE tradfi_tokens SET contract_address = '', updated_by = 'mainnet-cutover', updated_at = now()
 WHERE NOT contract_verified AND contract_address <> '';
COMMIT;
```
然后执行 `docker compose restart wallet`。

## 3. 验证并开启一个代币
先对照发行方公布的主网合约地址（xStocks/Ondo/Paxos/Tether 官方页面和区块浏览器）核对地址与 decimals：
```sql
UPDATE tradfi_tokens
   SET contract_address = '<主网合约>', decimals = <decimals>,
       contract_verified = true, verified_by = '<操作人>', verified_at = now(),
       deposit_enabled = true, withdraw_enabled = true,
       updated_by = '<操作人>', updated_at = now()
 WHERE asset = '<ASSET>' AND issuer = '<issuer>' AND network = '<NETWORK>';
```
然后重启 wallet。通过 admin API 修改合约地址会自动清除 `contract_verified`。

`deploy/mainnet/preflight.sh` 遇到已开启但未验证的行会 FAIL；对未验证但带合约地址，或 updated_by 含 test 的行会 WARN。
