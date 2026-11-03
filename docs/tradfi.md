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
撤单、平仓不拦截（已持仓用户可以退出）。

扩展拦截（同一套 FEATURE_DISABLED / REGION_RESTRICTED / fail-closed 规则）：
- `POST /strategy/grid`（`symbol`）网格创建；`POST /strategy/grid/square/{id}/copy` 复制网格——网关通过 strategy `GET /internal/strategy/grid/{id}/symbol`（X-Internal-Token）取源策略标的，查询失败 → 503，策略不存在 → 放行由上游 404。
- `POST /convert/quote`（`from`、`to` 任一为 TradFi 资产）；`POST /convert/accept`（`quoteId`）——网关查 asset `GET /internal/convert/quotes/{id}` 取报价两端资产。**该内部接口需随 feat/convert 合并时在 asset 服务补上**（返回 `{from,to}`，不存在 404）；未部署前 404 视为未知报价放行，此时仅靠 quote 阶段拦截（报价 TTL 内的开关变更为残余风险）。
- `POST /assets/transfer`、`POST /sub-accounts/transfer`（`asset`）账户间 / 子账户划转。`POST /futures/transfer` 仅 USDT，不涉及。
- 跟单：镜像开仓在 futures 服务内部发生，网关看不到。futures 在为跟随者开 / 加 TradFi 永续（XAG、WTI 等，以 market-data 门禁的 `futuresSymbols` 为准）前检查开关与跟随者 KYC 国家（futures 需配置 `KYC_URL`），不通过则记一条 `skipped` 跟单记录（reason `FEATURE_DISABLED` / `REGION_RESTRICTED` / `TRADFI_UNAVAILABLE`）。带单员自己的下单已在 `POST /futures/orders` 拦截。跟随 / 申请带单本身不拦截（与标的无关）。
- 网关 TradFi 检查在路由匹配之前执行，`/convert`、`/sub-accounts` 在本分支尚无路由，合并后即生效。

始终放行（退出路径）：撤单、平仓 / 减仓、`POST /strategy/grid/{id}/stop`、`DELETE /futures/copy/follows/{id}`、`POST /futures/copy/trader/close`、跟单镜像的减仓 / 平仓。

**部署前提 / 可信代理**：网关只在**直连对端 IP** 属于 `TRUSTED_PROXY_CIDRS`（逗号分隔 CIDR，留空 = Cloudflare 官方 IP 段）时才采信 `CF-IPCountry`；
否则（对端不可信、头缺失、`XX`、`T1`/Tor）一律视为国家未知，股票 / 贵金属**失败即拒绝**（`403 REGION_RESTRICTED`），加密货币不受影响。
网关前面有 nginx/Caddy 时，直连对端是代理而非 Cloudflare：把代理的地址（如 `127.0.0.1/32,::1/128` 或 docker 网桥 `172.16.0.0/12`）写入 `TRUSTED_PROXY_CIDRS`，
并让该代理只接受 Cloudflare 回源、原样转发 `CF-IPCountry`（否则任何人都能伪造国家）。
market-data 的运营位（placements）也读 `CF-IPCountry`，仅用于展示定向，不是合规门禁。

做市：market-data 在开关关闭时仍把 TradFi 交易对列为 trading，做市服务因此自行读取 `/internal/tradfi/gate`：
开关关闭（或门禁不可用，失败即关闭）→ 撤销该对全部做市挂单、状态 `disabled`、原因 `feature flag off (…)`。
现货交易对若做市账户无基础资产且没有基线（从未注资），不再只挂买单，而是撤单并 `paused`（`no base inventory …`），需先在后台注资基础资产。

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
