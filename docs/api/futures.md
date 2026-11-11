# 合约 API：USDT 本位永续（`services/go/futures`，:8089）

Gateway：`/futures/market/*` 公开。其余 `/futures/*` 要 Bearer，gateway 注入 `X-User-Id`。`/internal/*` 不经 gateway。

## 合约（种子在 `migrations/00001_init.sql`，按 feat/ui-p0-flows 的 UI）

| 交易对 | 最大杠杆 | 最小价格变动 | 最小数量 | 最大数量 |
|---|---|---|---|---|
| BTCUSDT | 200 | 0.1 | 0.0001 | 100 |
| ETHUSDT | 200 | 0.01 | 0.001 | 2000 |
| BNBUSDT | 100 | 0.01 | 0.01 | 5000 |
| SOLUSDT | 100 | 0.01 | 0.01 | 50000 |
| XRPUSDT | 100 | 0.0001 | 1 | 2000000 |
| DOGEUSDT | 100 | 0.00001 | 10 | 20000000 |
| ARBUSDT | 75 | 0.0001 | 1 | 2000000 |
| TONUSDT | 75 | 0.001 | 0.1 | 500000 |
| LINKUSDT | 75 | 0.001 | 0.1 | 200000 |
| AVAXUSDT | 75 | 0.001 | 0.1 | 200000 |

- 杠杆从 1 到该合约最大杠杆。保证金模式 cross 或 isolated。持仓模式单向（net）。
- Maker、taker 都是 0.05%（0.0005），开仓和平仓按成交额（price × qty）收取，进 `system:fee`。
- 每对维持保证金率 0.4%。

## 价格

- **指数：** market 服务最新现货价。无价格，或老于 `FUTURES_INDEX_STALE_SECONDS`，则失败关闭：新单返回 `MARK_PRICE_UNAVAILABLE`，强平和 TP/SL 暂停。
- **标记：** 三者取中位数：
  - index × (1 + 最近资金费率 × 距下次结算时间 / 资金费间隔)
  - index
  - 最近一笔合约成交价
- **资金费：** 每 `FUTURES_FUNDING_HOURS` 小时结算，UTC 00/08/16。
  - rate = premium + clamp(interest − premium, ±clamp)。premium 是该周期 (mark − index)/index 的平均。结果限制在 ±cap。
  - 正费率时多头付给空头。金额 = 仓位价值 × rate。按交易对和时间幂等。

## 保证金与风险

- **全仓：**
  - available = balance − order margin − Σ IM + min(0, Σ uPnL)
  - balance + Σ uPnL ≤ Σ MM 时强平（分档 MM，见下）。
- **逐仓：**
  - isolated margin + uPnL ≤ MM 时强平。
  - 增加保证金始终允许。减少后须 margin + min(0, uPnL) ≥ 标记价下的 IM。
- **强平：**
  - 仓位由 `system:insurance` 按标记价接管。
  - 强平费 = min(剩余权益, 价值 × 强平费率)，进保险基金。
  - 超出剩余权益的亏损由保险基金覆盖。基金变负时服务打 ALERT。
- **分档维持保证金**（`fut_risk_tiers`，迁移 00002；每个合约返回 `riskTiers`）：
  - MM = notional × mmr(档) − maintenanceAmount(档)。档按标记价名义价值选取。maintenanceAmount 保证跨档连续。
  - 每档有最大杠杆。非只减仓订单在 |持仓 + 同向挂单 + 本单| × price 超过当前杠杆允许的最大名义时拒绝，错误 `POSITION_LIMIT`。有仓时提高杠杆同样拒绝。
  - 强平价用当前名义所在档。
  - 种子档（USDT 名义 / MMR / 最大杠杆）：
    - 200x（BTC、ETH）：≤50k 0.4% 200x；≤500k 0.5% 100x；≤2M 1% 50x；≤10M 2.5% 20x；≤50M 5% 10x；≤200M 10% 5x。
    - 100x（BNB、SOL、XRP、DOGE）：≤20k 0.4% 100x；≤200k 1% 50x；≤1M 2.5% 20x；≤5M 5% 10x；≤20M 10% 5x。
    - 75x（ARB、TON、LINK、AVAX）：≤10k 0.4% 75x；≤100k 1% 50x；≤500k 2.5% 20x；≤2M 5% 10x；≤10M 10% 5x。
- **ADL（自动减仓）**，`FUTURES_ADL_ENABLED=true`：
  - 强平仓位穿仓（标记价权益 < 0）且亏损大于保险基金余额时触发。
  - 未覆盖部分推进平仓价：按破产价平掉（标记价偏移 uncovered / |qty|；全仓按名义把亏损分到各仓），对手是用户反向仓。排序 score：盈利时 ROE × leverage，亏损时 ROE ÷ leverage。
  - 对手方得到 `source="adl"` 的只减仓成交（无手续费）；仓位没了则剩余只减仓单撤销。写入 `fut_adl`；`/futures/market/insurance-fund` 展示 `recentAdl`。
  - 没有反向用户仓的数量退回保险基金。
  - 仓位带 `adlRank` 1–5（5 = 队列最前）。
- **TP/SL：** 可随单附带，或对仓位设置。标记价穿过触发价则发只减仓市价平仓。无流动性时每 `FUTURES_TPSL_RETRY_SECONDS` 重试。
- **市价保护：** 只在标记价 × (1 ± `FUTURES_MARKET_PRICE_BAND`) 内成交，剩余撤销。

## 接口

### 公开

`GET /futures/market/` 后接：

- `contracts`
- `ticker?symbol=`
- `depth?symbol=`
- `trades?symbol=`
- `klines?symbol=&interval=`
- `funding-history?symbol=`
- `mark-history?symbol=`
- `insurance-fund`

### 私有

- `GET /futures/account`
- `POST /futures/transfer {direction:"in"|"out", amount}`（现货 ↔ 合约，USDT）
- `GET /futures/transfers`
- `GET|POST /futures/leverage {symbol, leverage, marginMode}`
  - 模式切换被挡住时返回 `HAS_OPEN_ORDERS` 或 `HAS_POSITION`。
- `POST /futures/orders {symbol, side, type:"limit"|"market", timeInForce:"GTC"|"IOC", price, qty, reduceOnly, tpPrice, slPrice}`
- `GET /futures/orders?symbol=&status=open`
- `DELETE /futures/orders/{id}`
- `GET /futures/positions`
- `POST /futures/positions/{symbol}/tpsl {tpPrice, slPrice}`
  - 传 `""` 清除该值。
- `POST /futures/positions/{symbol}/margin {amount}`
  - 正数加保证金，负数减。
- `POST /futures/positions/{symbol}/close`
- `GET /futures/bills?kind=`
  - `kind`：`transfer_in`、`transfer_out`、`fee`、`realized_pnl`、`funding`、`liquidation_fee`、`insurance_cover`。
- `GET /futures/trades`

### 仅开发

需要 `DEV_MODE=true` 和头 `X-Internal-Token`。

- `POST /internal/dev/index {symbol, price}`
- `POST /internal/dev/tick`
- `POST /internal/dev/funding {symbol, rate}`
- `GET /internal/dev/audit`（检查 Σ equity = 净划转）

## 跟单（仅合约）

- **带单员：** 合约权益 ≥ `FUTURES_COPY_TRADER_MIN_EQUITY` 即可申请，分润 **0–50%**（`profitShare` 写 `"0.2"` 或 `"20%"`）。状态 `pending → active → closed`。关闭会停掉全部跟随。
- **跟随：** `amount` 从跟随者合约钱包划入隔离子账户 `copy:<followId>`（流水 `copy_transfer_out/in`）。模式：
  - `ratio`（默认 1）：镜像数量 = 带单员 Δqty × ratio × 跟随权益 / 带单员权益。
  - `fixed`：每次开仓用 `fixedMargin` × leverage / price。
  - `maxPerTrade` 限制单次开仓保证金；开仓还受子账户支付能力限制，并按合约步长向下取整（跳过会记原因）。
  - `tpRatio` / `slRatio` 是 ROE 比例，镜像仓的 TP/SL 价为 `entry·(1±ratio/leverage)`。
- **同步：** 带单员每笔改变仓位的成交写 `fut_copy_events`；跟随者用市价镜像开仓/加仓、按比例减仓、全部平仓/翻仓。跟随者仓位为空时才复制杠杆和保证金模式。
- **取消跟随：** 平掉镜像仓，结算未付分润，退回剩余余额。
- **分润：** 每个跟随在 `FUTURES_COPY_SETTLE_HOUR_UTC` **按日**结算，带高水位：share = max(0, 净已实现 − HWM) × rate。净已实现 = 已实现盈亏 + 手续费 + 资金费 + 强平相关项。亏损不收费，须先赚回。打到带单员**现货**账户（`fut_transfers.kind = copy_profit_share`）。按 (follow, day) 幂等。**分润不参与返佣**：它是划转，不是手续费，不进任何手续费/返佣基数。
- **统计**（`days` = 7/30/90）：pnl、roi、winRate、closedTrades、maxDrawdown、followers、aum、followerProfit、profitShareEarned、综合分、权益曲线。

| 方法 | 路径 | 认证 |
|---|---|---|
| GET | /futures/copy/summary | public |
| GET | /futures/copy/traders?sort=composite\|roi\|followers\|winrate\|aum&days= | public |
| GET | /futures/copy/traders/{id}?days= | public |
| GET / POST / PATCH | /futures/copy/trader | user（我的带单资料 / 申请 / 改分润、简介） |
| POST | /futures/copy/trader/close | user |
| GET | /futures/copy/trader/followers | user |
| GET / POST | /futures/copy/follows (?status=all) | user |
| PATCH / DELETE | /futures/copy/follows/{id} | user（改参数 / 取消跟随） |
| GET | /futures/copy/follows/{id}/trades | user（镜像成交 + 结算） |
| POST | /internal/copy/traders/{id}/status {status} | internal token |
| POST | /internal/dev/copy/settle {period} | dev |

错误码：INVALID_PROFIT_SHARE、INVALID_NICKNAME、INVALID_BIO、ALREADY_TRADER、TRADER_EQUITY_TOO_LOW、TRADER_NOT_FOUND、TRADER_CLOSED、TRADER_NOT_ACTIVE、TRADER_FULL、CANNOT_FOLLOW_SELF、INVALID_AMOUNT、AMOUNT_TOO_SMALL、INSUFFICIENT_BALANCE、INVALID_COPY_MODE、INVALID_FIXED_MARGIN、INVALID_RATIO、INVALID_MAX_PER_TRADE、INVALID_TP_RATIO、INVALID_SL_RATIO、ALREADY_FOLLOWING、FOLLOW_NOT_FOUND、FOLLOW_NOT_ACTIVE、OPPOSITE_POSITION、INVALID_STATUS。

## 环境变量

| 变量 | 默认 | 含义 |
|---|---|---|
| PORT | 8089 | 端口 |
| FUTURES_DEFAULT_LEVERAGE | 20 | 新交易对默认杠杆 |
| FUTURES_DEFAULT_MARGIN_MODE | cross | 默认保证金模式 |
| FUTURES_MARKET_PRICE_BAND | 0.05 | 市价保护带 |
| FUTURES_TICK_MS | 1000 | 风控/价格循环间隔 |
| FUTURES_MARK_SAMPLE_SECONDS | 60 | 标记价采样间隔 |
| FUTURES_INSURANCE_SNAPSHOT_MINUTES | 60 | 保险基金余额快照间隔（fut_insurance_snapshots，公开历史） |
| FUTURES_INDEX_STALE_SECONDS | 30 | 指数过期阈值（失败关闭） |
| FUTURES_FUNDING_HOURS | 8 | 资金费间隔 |
| FUTURES_FUNDING_INTEREST | 0.0001 | 每期利率 |
| FUTURES_FUNDING_CLAMP | 0.0005 | clamp |
| FUTURES_FUNDING_CAP | 0.0075 | 每期费率上限 |
| FUTURES_ADL_ENABLED | true | 保险基金盖不住穿仓时自动减仓 |
| FUTURES_LIQUIDATION_FEE_RATE | （空 = 合约 MMR） | 强平费率 |
| FUTURES_MIN_TRANSFER | 0.01 | 最小划转 |
| FUTURES_TPSL_RETRY_SECONDS | 5 | 无流动性时 TP/SL 重试间隔 |
| FUTURES_TRANSFER_RETRY_SECONDS | 5 | 划转 outbox 重试间隔 |
| FUTURES_COPY_MIN_AMOUNT | 50 | 划入跟随子账户的最小金额（USDT） |
| FUTURES_COPY_MAX_FOLLOWERS | 2000 | 每个带单员的最大活跃跟随数 |
| FUTURES_COPY_DEFAULT_SHARE | 0.10 | 带单员未设时的分润（产品范围固定 0–50%） |
| FUTURES_COPY_TRADER_MIN_EQUITY | 100 | 申请带单所需合约权益 |
| FUTURES_COPY_AUTO_APPROVE | false | 申请保持 `pending`，直到后台 `/admin/v1/copy/applications` 通过或驳回；`true` = 立即 active（仅开发） |
| FUTURES_COPY_SETTLE_HOUR_UTC | 16 | 每日分润结算小时（UTC；16 = 00:00 UTC+8） |
| FUTURES_COPY_MAX_RATIO | 10 | 比例跟单的最大 ratio |

保险基金从 0 起。手续费积在 `system:fee`，不自动划入。

## 体验金 / 抵扣金

`GET /futures/bonus` → `{items, available:{trial, deduction}}`。内部：`GET /internal/bonus?userId=`，`POST /internal/bonus/grant {userId, ref, kind, amount, validDays, source, requireOwnFunds}`（按 ref 幂等；合约钱包余额 < amount 时 `INSUFFICIENT_OWN_FUNDS`）。
成交手续费先用有效赠金支付（抵扣金先于体验金，先到期的先用；流水 `bonus_fee`）；全仓支付的资金费可由体验金覆盖（`bonus_funding`）。赠金不是保证金，不能提现；赠金支付的手续费部分（成交 feed 里的 `bonusFee`）不计返佣。
