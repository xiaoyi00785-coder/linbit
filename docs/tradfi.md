# TradFi：代币化股票与贵金属

> ⚠️ **合规：律师审核待定（lawyer review pending）。** 本功能在法务出具意见前不得在主网开启。
> 默认受限地区列表（US 及美属地区 PR/GU/VI/AS/MP/UM + OFAC 全面制裁 CU/IR/KP/SY）只是技术默认值，
> 不构成法律意见；克里米亚 / 顿涅茨克 / 卢甘斯克等地区无法用国家代码表达，需要另行处理（如 KYC 地址审核）。

## 范围

| 类别 | 标的 | 市场 | 开关 |
|---|---|---|---|
| 股票 stocks | AAPL TSLA NVDA MSFT GOOGL AMZN META COIN SPY（xStocks / Ondo 代币） | 现货 `<X>USDT`，24h | `tokenizedStocks` |
| 贵金属 metals | PAXG（ERC20）、XAUT（TRC20） | 现货 `PAXGUSDT` `XAUTUSDT` | `metals` |
| 贵金属 / 商品 | XAG、WTI | 永续 `XAGUSDT` `WTIUSDT`（futures 引擎） | `metals` |

两个开关默认关闭。

## 参考价与偏离

- 参考价 = Binance USDⓈ-M 永续 `/fapi/v1/ticker/price`（`BINANCE_FAPI_URL`），OKX `SWAP` 备用；TradFi 标的**不回退**到现货指数。
  映射（`refBinance` / `refOkx`）在后台按资产配置：AAPL → `AAPLUSDT` / `AAPL-USDT-SWAP`，PAXG/XAUT → `XAUUSDT` / `XAU-USDT-SWAP`，XAG → `XAGUSDT`，WTI → `CLUSDT` / `CL-USDT-SWAP`。
- 做市（marketmaker）围绕参考价报价。
- market-data 每 5s 比较现货最新成交价与参考价；偏离 > 阈值（全局默认 200 bps，可按资产覆盖）时 `widened=true`，做市价差 × `widenFactor`（默认 3），并发 Telegram 告警（冷却 `alertCooldownSec`，默认 900s）；恢复时再发一次。
- 无成交或无参考价时不放宽。

## 合规拦截（网关）

`CF-IPCountry` 或用户最新 KYC 申请的国家（ISO-2）在受限列表 → `403 REGION_RESTRICTED`；开关关闭 → `403 FEATURE_DISABLED`；
门禁数据不可用 → TradFi 请求 `503 SERVICE_UNAVAILABLE`（fail closed，加密货币请求不受影响）。
拦截范围：`/tradfi/*`、`POST /spot/orders`、`POST /futures/orders`（TradFi 标的）、`GET /wallet/deposit/address`、`POST /wallet/withdraw`（TradFi 资产）。
撤单、平仓不拦截（已持仓用户可以退出）。策略 / 网格 / 闪兑 / 跟单路径目前**未**拦截（TradFi 标的不在这些产品中配置即可）。

**部署前提**：`CF-IPCountry` 只有在源站只允许 Cloudflare 回源时才可信。

## 充提

- 发行方代币存于 `tradfi_tokens`（asset, issuer, network, contract_address, decimals, 充/提开关, min_withdraw, withdraw_fee）。种子行**没有合约地址**、充提全关；运营在后台填入**核实过的**地址后才能开启（否则 `ADDRESS_REQUIRED`）。
- wallet 启动时把“有地址且开启”的代币合并到网络注册表（网络名 SOL / ERC20 / TRC20），真实链模式下作为 `WALLET_TOKEN_<ASSET>_<NET>` 的缺省值（env 优先）。
  **新开启需要重启 wallet**；关闭即时生效（每次请求实时读库，`403 NETWORK_DISABLED`）。
- 限制：xStocks 为 Solana SPL、Ondo 为 ERC-20，均可能带转账白名单 / 合规钩子；上线前需在测试网验证归集与提现。
- 同一底层资产的不同发行方代币按 1:1 视为同一平台资产（如 AAPL），运营需评估赎回差异。

## 虚拟资产

测试网允许做市使用虚拟种子（`MM_SEED_BASE_ASSETS` 已包含股票与 PAXG/XAUT）；主网由 `common.TestnetSeedAllowed()` 守卫，只能用真实持有的代币。

## 后台

`/admin/v1/tradfi/{assets,assets/{asset},tokens,settings,deviation}`：读需要 `symbol:view`；写需要 `symbol:edit` + 2FA + reason，审计模块 `tradfi`（high，含 before/after）。

## 迁移

| 服务 | 文件 |
|---|---|
| market-data | `00010_tradfi.sql`（tradfi_assets / tradfi_tokens / tradfi_settings + 分类 stocks / metals / commodities） |
| order | `00010_spot_tradfi.sql`（11 个现货交易对） |
| marketmaker | `00010_mm_tradfi.sql` |
| futures | `00010_tradfi_perps.sql`（XAGUSDT、WTIUSDT + 风险档） |

撮合引擎 `SYMBOLS` 需包含 11 个现货交易对（docker-compose / devstack 已更新）。
