# Earn 字段含义 / 入口方案 / 契约差距

## 字段含义（正式）
| 字段 | 含义 |
|---|---|
| `apr` | 基础年化（十进制）。固定产品计息用此值。 |
| `bonusApr` + `bonusQuota` | 活期**档位利率**：仅对本金中 ≤bonusQuota 部分用 bonusApr，其余用 apr。**不是** apr 上再叠加，也**不是**整笔总 APR。 |
| `aprMax` | 服务端展示上限，通常 `max(apr, bonusApr)`。前端聚合/卡片用 `EarnApr.ceiling`（优先 aprMax；否则仅当 bonusApr>apr 时作档位上限）。**禁止** `bonusApr` 单独替代 APR。 |
| `kind` | `simple` / `fixed` / `onchain`（`flexible`=simple 兼容）。未知 kind → UNKNOWN，跳过该行，不整表崩溃。 |
| `hot` / `newcomer` | 运营标记；精选与「热销/新手」仅据此。 |

## 参考四入口 vs 已支持
| 参考 028 | 映射 | Android |
|---|---|---|
| Simple Earn | **活期+定期**（simple/flexible/fixed） | 筛选「简单赚币」 |
| On-chain Earn | onchain | 筛选「链上赚币」 |
| Wealth Hub | **无** earn/wealth 契约 | **不造入口** |
| Crypto Loans | `GET /loans/market` 公开只读 | 「质押借币」只读条款/质押币种；**禁止**借款/还款/抵押提交 |

## 后端待确认（供父转交，勿改生产配置）
1. Wealth Hub / VIP 是否另有服务或配置开关？无则移动端保持隐藏。
2. Flash Sale 倒计时/进度（029）是否新增 `campaignEndsAt` / 展示 sold/totalQuota？现产品 JSON 无倒计时字段。
3. OpenAPI `LoanMarket.terms` 补齐 `days`/`annualRate`/`outstanding`（生产已返回）。
4. `flexible` 别名是否长期保留于公开 API。

## 非本轮
- 不改生产产品/收益/配置；不合并部署；无真实申赎借贷。
- 模拟器由其他 worker 处理，本仓不碰进程/GUI。
