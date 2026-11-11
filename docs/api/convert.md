# 闪兑 Convert API

现货（资金账户）余额即时兑换。价格取 market-data 指数，加平台点差。对手方是平台账本账户 `system:convert`。实现在 **asset**；gateway 代理 `/convert/*`。

## 定价

- `rate = indexRate × (10000 − spreadBps) / 10000`；`indexRate = price(from) / price(to)`，单位 USDT。
- USDT、USDC 定价为 1。稳定币互兑 1:1，点差 0。
- 非 USDT 对（如 BTC→ETH）经 USDT 指数交叉（`route: via_usdt`）。
- 指数缺失或老于 `CONVERT_PRICE_MAX_AGE_SECONDS`（默认 15）→ `503 PRICE_UNAVAILABLE`（失败关闭）。
- 手续费 `0`，收入只来自点差。舍入始终偏向平台：给定 `fromAmount` 时 `toAmount` 按目标精度截断；给定 `toAmount` 时 `fromAmount` 向上取整。
- 单笔按 from 侧 USDT 价值检查：`CONVERT_MIN_USDT`（10）≤ 价值 ≤ `CONVERT_MAX_USDT`（50000）。

首次启动用环境变量默认值写入 DB，之后以后台修改为准：
`CONVERT_SPREAD_BPS=20`，`CONVERT_MIN_USDT=10`，`CONVERT_MAX_USDT=50000`，
`CONVERT_QUOTE_TTL_SECONDS=10`，`CONVERT_PRICE_MAX_AGE_SECONDS=15`，`MARKET_URL`。

## 用户接口（gateway）

| 方法 | 路径 | 认证 | 说明 |
|---|---|---|---|
| GET | `/convert/pairs` | public | assets、pairs、limits、spread、fee、TTL |
| POST | `/convert/quote` | user | `{from, to, fromAmount}` 或 `{from, to, toAmount}` |
| POST | `/convert/accept` | user | `{quoteId}`，原子且幂等 |
| GET | `/convert/orders` | user | `?page=1&pageSize=20&asset=BTC` |

报价响应：

```json
{"quoteId":"cq_…","from":"USDT","to":"BTC","fromAmount":"1000","toAmount":"0.01996",
 "rate":"0.00001996","inverseRate":"50100.2004","indexRate":"0.00002","spreadBps":20,
 "fee":"0","feeAsset":"BTC","expiresAt":1793723975000,"ttlSeconds":10}
```

成交响应：`{"order": ConvertOrder, "replay": false}`。同一 `quoteId` 再提交返回同一订单且 `replay: true`，不动资金。

ConvertOrder：`{id, quoteId, from, to, fromAmount, toAmount, rate, indexRate, spreadBps, fee, usdtValue, status:"filled", createdAt(ms)}`。

错误码：`VALIDATION_ERROR`、`PAIR_NOT_SUPPORTED`、`AMOUNT_TOO_SMALL`、`AMOUNT_TOO_LARGE`、`PRICE_UNAVAILABLE`（503）、`PAIR_PAUSED_DEPEG`（503，见下）、`QUOTE_NOT_FOUND`（404，含别人的报价）、`QUOTE_EXPIRED`（409）、`INSUFFICIENT_BALANCE`。

## 账本

accept 在一个 DB 事务里：锁报价 → 占用 ref `convert:<quoteId>` → 用户 free −from / +to → 分录（用户 ↔ `system:convert`）→ 订单行。`system:convert` 各币净头寸以 `convertPosition` 出现在偿付负债和后台偿付行（负值 = 平台欠用户该币；用户余额已含这部分，持仓必须覆盖它）。

## USDC 定价

只有 USDT 固定按 1 计价。USDC 对非稳定币（USDC↔BTC/ETH/…）用实时 `USDCUSDT` 指数，脱锚不能经加密对套利。USDC 指数缺失或过期时，任何涉及 USDC 的报价和 accept 都返回 `PRICE_UNAVAILABLE`（503）；USDC↔USDT 则返回 `PAIR_PAUSED_DEPEG`（区间内稳定币路径仍是 rate 1 / spread 0）。

## USDC 脱锚保护

USDC↔USDT（双向）在 `|USDCUSDT index − 1| > depegBps`（默认 50 = 0.5%，配置 `depegBps` 1–1000）时暂停。指数缺失或过期（老于 `CONVERT_PRICE_MAX_AGE_SECONDS`）也算暂停。报价和 accept 失败码 `PAIR_PAUSED_DEPEG`（503）；`/convert/pairs` 在这两对上给出 `paused` / `pauseReason`（`depeg` | `price_unavailable`）。USDC 与其他资产不受影响。监视器每 `CONVERT_DEPEG_CHECK_SECONDS`（默认 5）以及每次报价都会重算，恢复自动。每次切换 upsert `convert_pair_state`，写 `convert_events`（`pair_paused` / `pair_resumed`），并发 Telegram。

## 现货对冲

闪兑台（`system:convert`）在本所现货簿上由专用对冲账户对冲（`CONVERT_HEDGE_USER_ID`，默认 `system:hedge`）：

- `system:convert`：每笔闪兑的账本对手方（余额即台子头寸；负 = 欠用户）。
- `system:hedge`：做现货（BTC/ETH/SOL/BNB/TRX 对 USDT）。资金从 `system:convert` 划入（ref `hedge_fund:<hedgeId>`，不超过台子持有，不凭空造余额）。必须不同于做市账户 `mm:main` 和 `system:convert`，否则服务拒绝启动，对冲单不会和做市报价自成交。与所有 `system:*` 用户一样，不计入用户负债。
- 每币净敞口 = 闪兑头寸 + 对冲余额。偿付和 `/admin/v1/convert/position` 报 `convertPosition`、`hedgeBalance`、`netPosition`。

每轮（`CONVERT_HEDGE_INTERVAL_SECONDS`，默认 5；0 = 循环关）对每个开启对冲且 |net| ≥ `hedgeMinQty` 的币：下一张 IOC 限价，价格为指数 ± `hedge.slippageBps`（默认 50；买价向下取整，卖价向上取整），受 `hedge.maxOrderUsdt`（20000）和滚动 1 小时 `hedge.maxHourUsdt`（200000）限制。深度不够成交多少算多少，剩余按指数退避重试（`CONVERT_HEDGE_BACKOFF_SECONDS` 5 → `CONVERT_HEDGE_BACKOFF_MAX_SECONDS` 300）。未对冲价值超过 `hedge.unhedgedAlertUsdt`（5000）且持续超过 `hedge.unhedgedAlertMinutes`（10）时，每个回合发一次 Telegram + `unhedged_alert`。对冲单是 IOC；订单服务仍报 `new` / `partially_filled` 时记为 `resting`（事件 `hedge_resting`），期间该币不再下新单，超过 `hedge.cancelAfterSeconds`（默认 30，1–3600）后撤销（status `canceled`，事件 `hedge_canceled`，Telegram）。未成交部分退回对冲账户，回到未对冲净额，后续轮次再对冲。

残余：手续费、低于 `hedgeMinQty` 的灰尘和舍入接受，不从其他账户补。`/admin/v1/convert/position` 按币给出 `residualUsdt`（= netPosition × index；USDT 为台子累计的 USDT 净额）。

拒绝或失败的订单写 `hedge_rejected` / `hedge_failed` 并告警（每币最多 10 分钟一次）。

开关：全局 `hedge.enabled`（未设时取环境变量 `CONVERT_HEDGE_ENABLED`，仅 `WALLET_CHAIN=testnet` 时默认开）以及每币 `hedgeEnabled`。默认每币最小量：BTC 0.0002、ETH 0.005、SOL 0.1、BNB 0.02、TRX 50。

## 后台（`/admin/v1`）

| 方法 | 路径 | 权限 |
|---|---|---|
| GET | `/convert/config` | symbol:view |
| PUT | `/convert/config` | symbol:edit + 2FA（`totpCode`），审计 high |
| GET | `/convert/orders?userId=&asset=&page=&pageSize=` | ledger:view |
| GET | `/convert/position` | ledger:view |
| GET | `/convert/hedges?asset=&status=&page=&pageSize=` | ledger:view |
| GET | `/convert/events?kind=&page=&pageSize=` | ledger:view |

PUT 体：`{spreadBps (0–500)?, minUsdt?, maxUsdt?, depegBps (1–1000)?, hedge?:{enabled?, slippageBps (1–500)?, maxOrderUsdt?, maxHourUsdt? (≥ maxOrder), unhedgedAlertUsdt?, unhedgedAlertMinutes (1–1440)?, cancelAfterSeconds (1–3600)?}, assets?:[{asset, enabled?, hedgeEnabled?, hedgeMinQty?}], reason, totpCode}`。

每次配置 PUT 都要 `reason`（含对冲开关）：缺或空白返回 `VALIDATION_ERROR`（在 2FA 之后检查，错误的 `totpCode` 仍返回 `TOTP_INVALID`）。reason 写入 high 级后台审计。

`/convert/hedges` → `{items:[{id, asset, symbol, side, quantity, limitPrice, indexPrice, netBefore, orderId, filledQty, quoteAmount, status: pending|resting|filled|partial|unfilled|canceled|rejected|failed, error, createdAt}], total, account, state:{<asset>:{failures, nextRetryAt, unhedgedUsdt, unhedgedSince, alerted, lastStatus, lastError}}}`。
`/convert/position` → `{account, hedgeAccount, hedgeEnabled, depeg, items:[{asset, position, hedgeBalance, netPosition, residualUsdt, hedge}]}`。

内部（asset，`X-Internal-Token`）：`POST /internal/convert/hedge/run` 强制跑一轮（运维 / smoke）。交易对按币启用：两端资产都启用才可用。

Smoke：`bash scripts/smoke-convert.sh`。
