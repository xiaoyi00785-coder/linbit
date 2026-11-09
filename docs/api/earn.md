# 理财服务（端口 8092）

加密借贷和理财。资金经 asset `/internal/system-transfer` 划转，对系统账户
`loan`、`loan_collateral`、`loan_interest`、`earn`、`earn_interest` 使用幂等 ref。
价格取合约指数（`<ASSET>USDT`）。错误体 `{ "code": "...", "message": "..." }`。

## 借贷

公开：`GET /loans/market`、`GET /loans/quote`。其余要用户 token。

| 方法 | 路径 | 说明 |
|---|---|---|
| GET | `/loans/market` | 期限（`flexible`、`7d`、`30d`）的小时/年化利率，抵押品的单用户上限和指数价，LTV 阈值，限额 |
| GET | `/loans/quote?collateralAsset=BTC&amount=10000&term=flexible[&collateralAmount=]` | 抵押品（默认按初始 LTV）、ltv、hourlyInterest、termInterest、liquidationPrice |
| POST | `/loans` | `{collateralAsset, amount, collateralAmount?, term?}` 锁定抵押、贷出本金 |
| GET | `/loans?status=active\|closed` | 列表，含实时 ltv / warning / overdue / dueAt |
| GET | `/loans/summary` | 在借笔数、总债务、抵押价值 |
| GET | `/loans/records?type=borrow\|repay\|collateral\|renew\|liquidate` | 事件历史 |
| GET | `/loans/{id}` | 单笔 |
| POST | `/loans/{id}/repay` | `{amount}` 或 `{all:true}`；先付利息再付本金（定期到期前含提前还款罚息）；还清释放抵押 |
| POST | `/loans/{id}/collateral` | `{amount}` 追加抵押 |
| POST | `/loans/{id}/renew` | 仅定期：付掉已计利息，到期日延长一个期限 |

利息按小时预收：第 n 小时开始时收取 `ceil8(principal × hourlyRate)`；到期时刻及之后开始的小时按 `LOAN_OVERDUE_RATE_MULTIPLIER` 倍收取。ticker 在 `LOAN_WARN_LTV` 预警，在 `LOAN_LIQUIDATION_LTV` 或逾期满 `LOAN_OVERDUE_LIQUIDATION_DAYS` 时强平：扣下 `(debt × (1 + LOAN_LIQUIDATION_FEE_RATE)) / price` 的抵押，退回剩余。

错误：`UNSUPPORTED_COLLATERAL`、`INVALID_TERM`、`AMOUNT_TOO_SMALL`、`LTV_TOO_HIGH`、`LOAN_LIMIT_EXCEEDED`、`COLLATERAL_LIMIT_EXCEEDED`、`PRICE_UNAVAILABLE`（503）、`LOAN_NOT_FOUND`、`LOAN_NOT_ACTIVE`、`TERM_NOT_RENEWABLE`、`INSUFFICIENT_BALANCE`（来自 asset）。

仅开发（`DEV_MODE=true`）：`POST /internal/dev/earn/advance {hours}` 拨服务时钟并跑一次 tick。

## 理财产品

公开：`GET /earn/products`、`GET /earn/products/{id}`。

| 方法 | 路径 | 说明 |
|---|---|---|
| GET | `/earn/products?kind=simple\|fixed\|onchain&asset=&hot=true` | 目录（apr、aprMax、bonusApr/bonusQuota、termDays、unlockDays、minAmount、perUserMax、totalQuota/remaining、newcomer、redeemRule） |
| GET | `/earn/products/{id}` | 单个产品 |
| POST | `/earn/products/{id}/subscribe` | `{amount}` 扣现货。定期 → 订单（`expectedInterest`、`maturityAt`）；活期/链上 → 持仓，次日 UTC 起息（`interestStartsAt`） |
| POST | `/earn/products/{id}/redeem` | `{amount}` 或 `{all:true}`；活期立即到账；链上立即到账，或进入 `unlocking` 共 unlockDays（该段无收益）；定期 → `REDEEM_NOT_ALLOWED` |
| POST | `/earn/products/{id}/auto` | `{enabled}` 仅活期：每个 tick 把该币闲置现货申购 |
| GET | `/earn/positions?kind=` | 持仓（amount、pending、totalInterest、estDailyInterest、auto）+ `unlocking` |
| GET | `/earn/orders?status=holding\|matured` | 定期订单 |
| GET | `/earn/records?type=subscribe\|redeem\|interest\|maturity\|unlock` | 历史 |
| GET | `/earn/summary` | totalValueUsdt、totalInterestUsdt、recentInterestUsdt（前一个 UTC 日 0 点起）、按币 |
| POST | `/internal/earn/products` | 运维 upsert 产品（internal token） |

规则：活期/链上按该 UTC 日开始时持有的本金，每个完整 UTC 日支付 `floor8(yearly / 365)`，yearly = `min(p, bonusQuota) × bonusApr + rest × apr`。定期在到期（下一个 tick）连本金支付 `floor8(amount × apr × termDays / 365)`。新人产品：仅限没有任何非新人定期订单的用户，上限 `perUserMax`。

错误：`PRODUCT_NOT_FOUND`、`PRODUCT_OFFLINE`、`AMOUNT_TOO_SMALL`、`QUOTA_EXCEEDED`、`USER_LIMIT_EXCEEDED`、`NEWCOMER_ONLY`、`REDEEM_NOT_ALLOWED`、`INSUFFICIENT_POSITION`、`AUTO_NOT_SUPPORTED`、`INSUFFICIENT_BALANCE`（来自 asset）。

种子目录（迁移 00002）照 UI 上的产品和 APR；活期在第一档奖励额度内用较高 APR（USDT/USDC 10000、BTC 0.5、ETH 5、SOL 100、DOGE 50000），超出用较低 APR。

## 配置

| 变量 | 默认 |
|---|---|
| EARN_TICK_MS | 60000 |
| LOAN_ASSET | USDT |
| LOAN_RATE_FLEXIBLE / LOAN_RATE_7D / LOAN_RATE_30D | 0.000003847 / 0.0000042808 / 0.0000068493（小时） |
| LOAN_USER_LIMIT | 1200000 |
| LOAN_MIN_AMOUNT | 10 |
| LOAN_INITIAL_LTV / LOAN_WARN_LTV / LOAN_LIQUIDATION_LTV | 0.80 / 0.88 / 0.95 |
| LOAN_COLLATERALS | BTC:10,ETH:50（币:单用户上限） |
| LOAN_OVERDUE_RATE_MULTIPLIER | 3 |
| LOAN_OVERDUE_LIQUIDATION_DAYS | 7 |
| LOAN_LIQUIDATION_FEE_RATE | 0.03 |
| LOAN_EARLY_REPAY_PENALTY_RATE | 0.0005 |

## 产品管理（内部接口，仅供 admin 服务调用，需 `X-Internal-Token`）

借币期限与抵押币种已从环境变量迁移到数据库（`e_loan_terms`、`e_loan_collaterals`，迁移 00003）。
启动时用环境变量默认值补种（`ON CONFLICT DO NOTHING`，不会覆盖后台修改）。所有写接口返回 `{before, after}`。

| 方法 | 路径 | 说明 |
|---|---|---|
| GET | `/internal/earn/products[/{id}]` | 含下架产品、已售、剩余额度 |
| PATCH | `/internal/earn/products/{id}` | title / minAmount / unlockDays / newcomer / hot / status / sort（不含利率） |
| DELETE | `/internal/earn/products/{id}` | 仅限从未有持仓/订单的产品，否则 409 `PRODUCT_IN_USE`（应改为下架） |
| PUT | `/internal/earn/products/{id}/rate` | `{apr?, bonusApr?}`，[0,1]，≤6 位小数；bonusApr 仅活期。**只由已审批的变更调用** |
| PUT | `/internal/earn/products/{id}/quota` | `{totalQuota?, perUserMax?, bonusQuota?, clear:[...]}`；低于已售返回 409 `QUOTA_BELOW_SOLD` |
| GET | `/internal/loans/terms` | 含 outstanding / remaining |
| PATCH | `/internal/loans/terms/{term}` | `{status: online|offline}`；下架后新借款返回 `TERM_OFFLINE` |
| PUT | `/internal/loans/terms/{term}/rate` | `{hourlyRate}`，[0,0.001]，≤12 位小数。**只由已审批的变更调用** |
| PUT | `/internal/loans/terms/{term}/quota` | `{totalQuota}` 或 `{clear:true}`；超额借款返回 `TERM_QUOTA_EXCEEDED` |
| GET/POST | `/internal/loans/collaterals` | POST upsert `{asset, cap, status?, sort?}` |
| DELETE | `/internal/loans/collaterals/{asset}` | 有借款订单时 409 `COLLATERAL_IN_USE` |

利率调整只影响生效后的新借款；已有借款（含续期）沿用借入时锁定的利率。理财利率调整从生效时刻起按新 APR 计息。
