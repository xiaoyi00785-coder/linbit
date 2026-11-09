# Earn 契约/配置差距（对照参考 028/029 + 生产公开目录 + 本地 812afc9→本提交）

## 依据
- 参考：`ref-capture/auth/028_earn_list`（package=`io.easiex.app`）、`029_earn_btc_detail`（Flash Sale / 倒计时 / 进度条）
- 生产只读：`GET https://api.cowa.click/earn/products` → **20** 条（2026-11-09）：`simple×6` + `fixed×8` + `onchain×6`；`GET /loans/market` → 200
- 后端：`services/go/earn`（kind CHECK `simple|fixed|onchain`；种子迁移 00002；admin 产品管理齐全）
- 测试夹具少 ≠ 生产少：夹具 1–2 条仅证据用；生产目录如上

## 已确认前端漏显（本轮修）
1. **Kind 枚举缺失**：OpenAPI/Android 仅 `flexible|fixed`，生产返回 `simple|onchain` → kotlinx 反序列化整表失败，UI 表现为「产品很少/空」。已扩枚举并加 `EarnProductKindDecodeTest`。
2. **分类入口错位**：把参考四宫格误做成「活期/定期」。参考为 Simple Earn / On-chain / Wealth Hub / Loans；后端 earn 对应 **simple / onchain / fixed(列表内)**。UI 改为「简单赚币 / 链上赚币 / 理财宝」仅在有产品时显示。
3. **推荐伪「热销」**：曾用 `bonusApr` 判定热销/精选，种子几乎全部活期带 bonusApr。改为精选仅 `hot|newcomer`；标签 `hot→热销`、`newcomer→新手`、`bonusApr→加息`，无活动字段不伪标 Flash Sale。
4. **APR 聚合**：资产行/卡片优先 `aprMax ?: bonusApr ?: apr`（与服务端 aprMax= max(apr,bonusApr) 一致）。

## 后端/契约已有能力（勿称「缺 API」）
| 能力 | 状态 |
|---|---|
| 三分类产品 simple/fixed/onchain | **有**（公开列表无分页，全量 online） |
| hot / newcomer / bonusApr / bonusQuota / aprMax / unlockDays / redeemRule | **有** |
| 产品管理 admin + 利率审批 | **有**（`/admin/v1/earn/products*`、`/internal/earn/products*`） |
| 质押借币 `/loans/*` | **有**（与理财同 earn 进程） |

## 准确缺口（需后端/产品确认，本轮不造）
| 缺口 | 说明 | 建议提问 |
|---|---|---|
| Wealth Hub / VIP 尊享 | 参考四宫格有；Web legacy 有 `/earn/vip`；**earn OpenAPI 无独立 wealth 产品 kind** | 是否另有服务/开关？还是运营配置页？ |
| Flash Sale 文案 + 倒计时 + 认购进度（029） | 参考有 Ended / 00:00:00:00 / 0/0 BTC；**产品 JSON 无 countdown/campaign 字段** | 是否复用 `hot`+配额，或需 `campaignEndsAt`/`sold` 展示进度？ |
| OpenAPI 与实现不一致 | 文档/生成模型曾写 `flexible`；实现与 earn.md 为 `simple` | 网关是否曾映射？现以服务为准并修正契约 |
| Android Loans 客户端 | `/loans/market` 生产可用，**Android 无 loans API/屏** | 是否排期移动端借币？未接前 **不造** Loans 入口 |
| 营销倒计时 | 无字段 | 有字段后再做；禁止前端假倒计时 |

## 导航/金额
- 理财深链：资产/更多 → Spot 顶栏 Earn（已有）
- 申购金额：现有 NumberBox + 校验 min/perUserMax；029 的 Max/可用余额依赖登录资产接口，未假造余额

## 模拟交易参考
仍缺 `io.easiex.app` Demo Trading 主界面整页；不阻塞本轮理财可做项。
