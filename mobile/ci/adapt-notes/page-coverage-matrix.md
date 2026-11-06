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

## screens 分母
- screens.txt：167 张图 / 155 screen 键
- screens-pr：28 张图 / 28 screen 键（含充提/客服详情扩展）
- 显式 shot case：141；support-* 走前缀
- screens.txt 中无独立 case、靠前缀/默认：support-*、home（默认 tab）、splash-*

## 断链 / ComingSoon / 假 Toast（契约结论）
| 入口 | 现状 | OpenAPI | 处理 |
|------|------|---------|------|
| 模拟交易 | ComingSoon | 无 paper/demo path | 不能造假盘 |
| 价格预警 | Toast 标明缺接口 | 无 alert CRUD | 不能造假 |
| Apple/Google | Toast 标明缺 OAuth | 无 oauth path | 不能造假 |
| 快捷买币/盈亏分析/现货兑换 | Toast 即将开放 | 需核对支付/分析 API | 未本批 |
| 合约工具:交易偏好/计算器/历史标记价/风险准备金/新手教学 | Toast 即将开放 | 部分可能无独立 path | 未本批 |
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

