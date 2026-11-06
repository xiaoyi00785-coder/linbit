# iOS 生产页面覆盖矩阵（#104）
> 分母以 MainTab + FeaturePage + 资产/合约关键子路由 + Auth 为准；**screens-pr 31 张不是全量**（screens.txt 展开约 303 张 / 155 screen 键）。

## 生产入口矩阵（摘要）
| 入口 | 名称 | screens-pr 覆盖 | 参考 | 备注 |
|------|------|-----------------|------|------|
| MainTab.home | 首页 | home-light/dark | ref/#1 | 路由默认 tab |
| MainTab.markets | 行情 | markets-light |  | shot markets |
| MainTab.futures | 合约 | futures-light/dark | ref/11 | shot futures |
| MainTab.spot | 现货 | spot-light | ref/28 | shot spot |
| MainTab.assets | 资产 | assets-light |  | shot assets |
| FeaturePage.earn | 理财 | earn-light |  | shot earn |
| FeaturePage.strategy | 策略/网格 | 未入 screens-pr |  | shot strategy / grid-create 在 screens.txt |
| FeaturePage.swap | 闪兑 | swap-light 缺产物 |  | shot swap；9a70ed0 包缺文件 |
| FeaturePage.chat | 聊天室 | 未入 pr |  | flag chat |
| FeaturePage.subAccounts | 子账户 | 未入 pr |  |  |
| FeaturePage.welfare | 福利中心 | 未入 pr |  |  |
| FeaturePage.copyTrading | 跟单 | copy-list-light |  | shot copy-list |
| FeaturePage.tradfi | TradFi | tradfi-list-light |  | shot tradfi-list |
| FeaturePage.loans | 质押借币 | loan-market-light |  | shot loan-market |
| FeaturePage.partner | 合伙人 | partner-none-light |  | shot partner-none |
| FeaturePage.coupons | 卡券 | coupon-issued-light |  | shot coupon-issued |
| FeaturePage.notifications | 通知 | notif-list-light |  | shot notif-list |
| FeaturePage.security | 账户安全 | security-light |  | shot security |
| FeaturePage.kyc | 身份认证 | kyc-none-light |  | shot kyc-none |
| FeaturePage.support | 客服工单 | support-list；detail 新扩 |  | prefix support-* |
| FeaturePage.futuresHistory | 合约历史 | screens.txt |  |  |
| AssetsRoute.deposit | 充值表单 | assets-deposit 新扩无产物 |  | fixture DEBUG |
| AssetsRoute.withdraw | 提现表单 | assets-withdraw 新扩无产物 |  | fixture DEBUG |
| FuturesTop.模拟交易 | 模拟交易 | 无 |  | 无 OpenAPI paper |
| FuturesTools.价格预警 | 价格预警 | 无 |  | 无 alert CRUD |
| Auth.passkey | 通行密钥登录 | login-* 有入口 | ref2/3 | 本地接线待 push |
| Auth.apple/google | 社交登录 | login-* |  | 无 OAuth 契约 |

## screens 分母（务必分开，勿混用）

| 分母 | 当前值 | 含义 |
|---|---|---|
| screens.txt **行** | **172** | 配置行条目（含同 key 重复行） |
| screens.txt **unique keys** | **160** | 去重后的 screen 键；其中重复 key 12 个：futures-algo-amend-plan, futures-algo-amend-tpsl, futures-amend, futures-kind, futures-leverage, futures-mode, futures-mode-hedge, futures-orders-plan, futures-orders-tpsl, futures-tools, futures-tpsl, futures-tpsl-partial |
| screens.txt **展开张数估计** | **313** | 按行主题列：`both`→2，`light`/`dark`→1（CI 实际还可能再滤） |
| screens-pr **行/键** | **33 / 33** | PR 冒烟清单 |
| screens-pr **展开张数估计** | **36** | |
| screens-adapt **行** | **8** | 手动 adapt 清单 |
| adapt **已下载 PNG** | **5**（750×1334 / **仅 375pt**） | 缺清单内 deposit/withdraw/support-detail；见 `2026-11-05-adapt-device-evidence.md` |
| PR 冒烟已下载 PNG（tip 2300fb6） | **28**（1179×2556） | 单机型非 xxxl，**不能**当多宽度适配 |

历史口头「167 行 / 155 键」为加 `futures-guide` 等之前；**以本表当前值为准**。


## 断链 / ComingSoon / 假 Toast（契约结论）
| 入口 | 现状 | OpenAPI | 处理 |
|------|------|---------|------|
| 模拟交易 | ComingSoon | 无 paper/demo path | 不能造假盘 |
| 价格预警 | Toast 标明缺接口 | 无 alert CRUD | 不能造假 |
| Apple/Google | Toast 标明缺 OAuth | 无 oauth path | 不能造假 |
| 快捷买币/盈亏分析/现货兑换 | Toast 即将开放 | 需核对支付/分析 API | 未本批 |
| 合约工具:交易偏好 | Toast | preferences=quoteCurrency | 如实 Toast |
| 合约工具:计算器/标记价/保险金/新手教学 | 已接线 | 见本批证据 | 已实现 |
| 现货 K 线/更多 | Toast 即将开放 | 现货图路由未接 | 未本批 |
| 资产 tab≥4 | Toast | 理财/策略等应用 Feature | 可改导航到 FeaturePage |
| Auth 顶栏客服 | Toast 即将开放 | 有 supportTickets | 本批改打开 support |
| swap-light 缺文件 | CI 产物 | screens-pr 末位 + cancel-in-progress 截断 | 本批前移 swap |

## 可复现测试
- `python3 mobile/ci/tests/test_ios_contract_guards.py`
- CI：push/PR → screens-pr；`workflow_dispatch`+adapt_layout → screens-adapt
- 充提 fixture 仅 `ShotGate.launchShot`；Release `ShotGate.enabled=false`

## Batch (tip 2300fb6 local, unpushed)

### Evidence-backed this batch
| Entry | OpenAPI / parity | iOS status |
|---|---|---|
| 现货 K 线 | `GET /market/klines` (Android SpotTradeViewModel, Web market) | `MarketAPI.klines` + `SpotChartPanel` toggle on SpotTradeView |
| 合约计算器 | Local Android `TradeCalculator` (no fund API) | `FuturesCalculatorSheet` + tools → onCalculator |
| 历史标记价格 | `GET /futures/market/mark-history` | `FuturesMarkHistoryView` + FeaturePage + tools |
| 风险准备金 | `GET /futures/market/insurance-fund` | `FuturesInsuranceFundView` + FeaturePage + tools |
| 合约今日盈亏 | `GET /futures/pnl/today` | Already on Futures trade TodayPnlCard (prior) |

### Explicitly NOT done (missing service / same as Android soon)
| Entry | Evidence | iOS |
|---|---|---|
| 快捷买币 | No OpenAPI; Android AssetsScreen `onSoon` | Honest toast |
| 资产「盈亏分析」 | No dedicated API; `/futures/pnl/today` ≠ assets analysis | Honest toast |
| 交易偏好 | `/account/preferences` = quoteCurrency only | Honest toast |
| 价格预警 | No alert CRUD in OpenAPI | Honest toast |
| 模拟盘独立账户 | No paper-trading API | ComingSoon / tab stub |
| Apple/Google OAuth | No OAuth paths | Disclose toast |

### Tests
- `mobile/ci/tests/test_ios_contract_guards.py` = **Python source-text guards**, not Swift XCTest / not device Passkey runs.

## screens.txt 分母：167 vs 155（键 / 行）

- `screens.txt` **行条目 167** = 配置里每一行截图指令（含同一 key 的重复行）。
- **unique keys 155** = 去重后的页面键；其中 12 个 futures 相关 key 各出现 2 次（如 `futures-leverage`×2 … `futures-tpsl-partial`×2），故 155 + 12 = 167。
- **张（PNG）** 与键/行不是同一分母：CI 通常按主题再产 light/dark，screens-pr 本 tip 产物实测 **28 PNG / 28 keys**；adapt 本 tip 产物 **5 PNG（xxxl）**。
- 先前口头「~155」指 unique keys；「167」指行条目。矩阵分母应用时需标明用的是键还是行还是张。

## Toast / 契约依赖精确表（审计 tip 6e96d41+local）

| 入口 | OpenAPI / 对端 | 结论 | iOS 状态 |
|---|---|---|---|
| 快捷买币 | 无 buy-crypto/OTC/ramp 路径；仅有 `/market/fiat-rates`；安卓 AssetsScreen soon | **缺契约** | 如实 Toast |
| 资产盈亏分析 | 仅 `/futures/pnl/today`（合约今日卡已用）；无资产维度分析 API；安卓 soon | **缺契约** | 如实 Toast |
| 交易偏好 | `/account/preferences` 仅 `quoteCurrency`（Web fiat.ts 同）；无下单偏好字段 | **现有接口不够** | 如实 Toast |
| 价格预警 | OpenAPI 无 alert CRUD；`alertCooldownSec` 属 TradFi 偏离告警；安卓 soon | **缺契约** | 如实 Toast |
| 模拟盘 | 无 paper/sim 用户端点；安卓 ComingSoon 同注 | **缺契约** | Tab ComingSoon |
| Apple/Google | 无 OAuth 路径 | **缺契约** | 披露 Toast |
| 新手教学 | 无 tutorial API；Web `pagedata.ts` 有仓位档位/标记价/资金费/保险基金/学院/风险提示静态文案；合约工具入口已约定 | **可本地静态页** | **已实现 FuturesBeginnerGuideView**（仅合约工具；学院 Tab 待产品） |
| 在线客服（非工单） | 有 `/support/tickets*` 工单；无 live-chat；flag `supportTickets` | **工单有、IM 无** | flag 开工单 else Toast |
| 新人领取/报名 | **有** `/rewards/newcomer/enroll` + `/tasks/{key}/claim` | **可完成** | **本地已接线**（未 push） |
| 现货「更多」 | 无独立菜单契约 | 产品未定 | Toast |
| 划转部分账户 | TransferView 对未开放账户 soon | 视账户枚举 | Toast |

不降低「全部完成」标准：有契约项必须接线；无契约项保持诚实缺口，不伪称完成。

## Batch (tip 6e96d41 + local commits, unpushed until CI terminal)

### Evidence-backed this batch
| Entry | Source | iOS status |
|---|---|---|
| 合约新手教学 | Web `apps/web/legacy/pagedata.ts` 静态 DOCS；工具入口「新手教学」 | `FuturesBeginnerGuideView` + FeaturePage.futuresGuide + tools + shot `futures-guide` |
| 新人报名/领取 | OpenAPI `/rewards/newcomer/enroll` + `/tasks/{key}/claim` | NewcomerView actionBusy + session 重校验 + 仅 claimable/completed；**测试仅 mock** |
| 提现防重 | 既有 submitting + reloadSeq | 补 `guard !submitting` |
| Passkey/K 线 | 既有 | resumeOnce 取消前续；candle/loadSeq 丢弃过期 |

### 产品问题（不自设范围）
1. 首页「学院」Tab 是否复用本合约新手教学内容，还是独立学院内容架构？
2. 是否需要单独的「现货新手」静态页，还是仅合约工具入口？
3. 安卓「新手教学」仍 soon：iOS 先发静态页是否接受两端暂时不一致？

### Tests
- `python3 mobile/ci/tests/test_ios_runtime_risks.py` = **Python 仿真 / 源码守卫**，≠ Swift XCTest；**不发起真实 enroll/claim/提现**。

### 审校修正（本批本地，相对 7633820）
- 资金费：去掉「固定 8h UTC」绝对表述；以 `fundingIntervalHours` / `nextFundingTime` / 历史资金费率页为准；历史页展示品种结算周期。
- 新人 task.status 严格 OpenAPI：`locked|in_progress|claimable|claimed|expired`，**仅 claimable 可领**（不再用 completed/done）。
- 首页「学院」：`AcademyHomeSection` 展示 pagedata「linbit 学院」分类目录；合约细则标「合约指南」，不伪称完整学院。
- 现货：无独立现货指南页（无同源独立 DOCS）；仅学院新手入门收录「现货与合约区别」摘要。

## 设计 ref ↔ screen 键（已存在对照）

| screen 键 | 设计 ref | screens-pr | 视觉证据状态 |
|---|---|---|---|
| home | ref/1.jpg | 有（2300fb6 home-light/dark） | 已 Read：品牌绿体验金额、悬浮底栏、宫格；**非 tip 6e96d41/330c552** |
| login | ref2/3.jpg | 有 | adapt xxxl 有；配色黑主按钮 |
| futures | ref/11.jpg | 有 | adapt+pr 有；行情区域限制横幅属环境 |
| spot | ref/28.jpg | 有 | adapt+pr 有；盘口区域限制遮罩属环境 |
| assets | （资产总览对照） | 有 | adapt 未登录空态居中 OK；顶 Tab「策略」右裁属横滑预期 |
| password-register | ref2 注册 | 有 | adapt xxxl 有 |
| theme / 设置 | ref/14.jpg | 未入 pr 必拍 | screens.txt 有 security 等 |

` /workspace/ref ` 共 **49** 张（1–49.jpg）；多数交易弹层/历史对照未一一写入 screens-pr。

## screens-pr 未覆盖的 unique keys（129）

account-switch, assets-account-transfer, assets-address-book, assets-internal-transfer, assets-withdraw-records, chat, copy-become, copy-become-error, copy-detail, copy-empty, copy-error, copy-follow, copy-follow-error, copy-follows, copy-follows-empty, coupon-active, coupon-confirm, coupon-empty, coupon-error, coupon-expired, coupon-fail, earn-subscribe, event, event-confirm, futures-algo-amend-plan, futures-algo-amend-tpsl, futures-algo-history, futures-amend, futures-funding, futures-history, futures-kind, futures-liq, futures-mode, futures-mode-hedge, futures-orders-plan, futures-orders-tpsl, futures-plan, futures-pnl, futures-pnl-empty, futures-pnl-error, futures-position-history, futures-tiers, futures-tools, futures-tpsl-partial, grid-confirm, grid-create, home-avatar, kyc-approved, kyc-error, kyc-form, kyc-form2, kyc-form2-full, kyc-pending, kyc-rejected, loan-borrow-error, loan-closed, loan-collateral, loan-error, loan-loans, loan-loans-empty, loan-quote, loan-repay, notif-empty, notif-error, notif-settings, notif-settings-error, partner-active, partner-apply, partner-apply-error, partner-empty, partner-error, partner-pending, partner-rejected, partner-team, password-change, password-reset, security-2fa-disable, security-2fa-disable-error, security-antiphishing, security-antiphishing-error, security-avatar, security-avatar-crop, security-avatar-error, security-avatar-uploading, security-devices, security-devices-empty, security-devices-error, security-email, security-email-error, security-error, security-fund, security-fund-error, security-fund-reset, security-passkeys, security-passkeys-add, security-passkeys-empty, security-phone, security-profile, security-profile-error, security-sessions, splash-end, splash-mid, splash-start, spot-fee, spot-history-amount, spot-history-conditional, spot-market-amount, spot-plan, strategy, sub-create, subaccounts, support-create, support-create-error, support-create-ratelimit, support-detail-closed, support-detail-conflict, support-detail-error, support-detail-ratelimit, support-detail-replied, support-list-empty, support-list-error, swap-picker, tradfi-disabled, tradfi-empty, tradfi-error, tradfi-metal, tradfi-restricted, tradfi-unavailable, welfare

（完整键仍在 screens.txt / 需 workflow_dispatch 全量或补 pr 清单后才有 CI 视觉。）
