# 钱包真实链接入（测试网优先）

## 架构
- `wallet` 服务：只持有**公钥**（xpub / SOL 地址列表），负责派生充值地址、扫链入账、提现广播、归集（sweep）。
- `wallet-signer` 容器：唯一持有热钱包种子（scrypt + AES-GCM 加密 keystore），通过带 Bearer Token 的 HTTP 接口签名，**不对外发布端口**。目前与主服务同机独立容器；以后迁到独立服务器只需修改 `WALLET_SIGNER_URL`。
- 钱包方案（当前决定）：**单一软件 HD 钱包**，专用新助记词（signer `init` 生成，永不打印），充值地址由 xpub 派生（分支 0），热钱包为分支 1 / 索引 0，由 signer 签名。
- 冷钱包/多签：保留接口，**暂不实现、不启用**（不做 Safe / TRON 多签）。默认把超过热钱包上限的部分归集到同一 HD 钱包派生的**储备地址**（分支 2 / 索引 0）；如设置 `WALLET_COLD_ADDRESS_<NET>` 则用该地址；`WALLET_COLD_FROM_HD=false` 关闭（全部留在热钱包）。注意储备地址与热钱包同一种子、同一 signer，安全性不等同真正冷钱包。
- 热钱包上限：`WALLET_HOT_CAP_<ASSET>_<NET>`（如 `WALLET_HOT_CAP_USDT_TRC20=50000`），为空表示不设上限。
- 注意：signer 会对任何持有有效 token 的请求签名，token 等同热钱包权限，请妥善保管并定期轮换。

## 支持网络（`WALLET_CHAIN=testnet`）
| 网络 | 测试网 | 默认 RPC |
|---|---|---|
| TRC20 | TRON Nile | https://nile.trongrid.io |
| ERC20 | Sepolia | https://ethereum-sepolia-rpc.publicnode.com |
| BEP20 | BSC testnet | https://bsc-testnet-rpc.publicnode.com |
| ARBITRUM | Arbitrum Sepolia | https://sepolia-rollup.arbitrum.io/rpc |
| SOL | devnet | https://api.devnet.solana.com |
| BTC | testnet | https://mempool.space/testnet/api |

可用 `WALLET_RPC_<NET>`、`WALLET_CHAINID_<NET>`、`WALLET_TOKEN_<ASSET>_<NET>=合约:精度` 覆盖。`WALLET_CHAIN=mainnet` 时 RPC 和代币合约必须显式配置。默认 `WALLET_CHAIN=mock`（模拟链）。

## 初始化步骤
1. 准备口令文件 `./secrets/signer_passphrase`（已在 .gitignore），设置 `WALLET_SIGNER_TOKEN`（≥24 位随机串）。
2. `docker compose --profile chain run --rm wallet-signer init`（测试网；主网加 `-mainnet`）。备份 keystore 卷与口令（分开保存）。
3. `docker compose --profile chain run --rm wallet-signer xpub` → 将输出的 `WALLET_XPUB_EVM/TRON/BTC` 写入 .env。
4. `docker compose --profile chain run --rm wallet-signer sol-addresses -n 10000 -o /data/sol.txt`，将文件提供给 wallet 并设置 `WALLET_SOL_ADDRESSES_FILE`。
5. 设置 `WALLET_CHAIN=testnet`、热钱包上限 `WALLET_HOT_CAP_<ASSET>_<NET>`，可选 `WALLET_SWEEP_ENABLED=true`、`WALLET_SWEEP_MIN_<ASSET>_<NET>`。
6. 启动时 wallet 会校验 xpub 与 signer 公钥一致，不一致拒绝启动。
7. 用水龙头给 signer 日志里打印的热钱包地址充测试币（gas 补充需要）。

私钥/助记词永远不会被打印；`xpub` 只输出公钥。

## 流程
- 充值：worker 周期执行 sync（扫链写 chain_txs）→ scan（确认数达标入账）。
- 提现：先将工单 approved→processing 占位再广播，避免重复支付；结果不明确时保持 processing 人工对账。
- 归集：代币归集前先从热钱包补 gas（×1.2），再从充值地址转到热钱包；超出热钱包上限部分进入冷钱包。
