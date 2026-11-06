# Android 生产覆盖与验收分母

状态是**已建立可复核生产 mock 证据**或**未验**，不再以“某宿主至少一张 PNG”算整页/整流程通过。旧 36/36 结论已撤销。

本批生产套件读取 `PRODUCTION_EVIDENCE_OUT` 外部输出中的 `junit-summary.json`；此 SHA 在本地未提交验证时为父 SHA，必须结合 `run.properties` 的 `verifiedWorkingTree=local-uncommitted-validation` 阅读，不得用旧 SHA 父 CI 代表本批修改。默认套件及历史预览不是本批 enabled 证据。所有像素均为 app-content-only，不是整机 full-screen；图元描述需要和 png JSON 同时阅读。
截图范围、mock 响应和 JUnit 均须单独留存。合成 fixture 的历史 round1-7 仅作为组件预览，不能计作生产视觉通过。
真机由用户验收；离屏与 instrumentation 不替代真机。

## 证据等级

- `P`：通过真实生产 Host/NavHost 或其明确记录的生产子页入口，数据由隔离 MockWebServer 提供，使用真实 inset 和实际窗口布局。`P`不等于从 AppRoot 全路径点击、不排斥离屏限制或字段级业务覆盖；两 host 分身不在范围内不能算补齐。
- `W`：真实生产 ModalBottomSheet/Dialog 窗口可独立捕获，且与内容截图分开标记；不能遇到限制就降级 inline sheet。
- `V`：已 Read 对参考图核对配色、字号/字重、基线、间距及必要适配组合；不是测试 pass 的同义词。
- `C`：本批完成实际内容矩形/裁切机器校验，18 张全部逐张 Read；有 ref 的列表与字重/间距已人工看，但每个页面都有直接参考和所有适配未齐，完整视觉 V 仍 U。
- `U`：未验、当前环境不可验或仍缺功能。请勿转换为通过。
- `S`：模拟器/离屏器 limitation，必须提供可在支持环境跑的 instrumentation 用例。

以下完整命名条目是本批评审基线，不宣称具有伪精确的全部通过率；生产弹层、业务状态、Tab 与适配不能混为一个数字。

## A. 根级与功能宿主（28 个入口）

| 分区 | 项 | 生产入口 | 当前证据 / 未查与占位 |
|---|---|---|---|
| MainTab 5 | Home；Markets；Futures；Spot；Assets | `app/AppRoot.kt:87-120`；`ui/components/TabBar.kt:37-42` | 全 U（尚待 P/V）。Markets 聊天室与 TradFi、模拟盘、资产部分功能仍占位；Futures/Spot 的子页不能归并进根截图。 |
| Spot 内部 4 | 现货交易、策略、理财、闪兑 | `feature/trade/SpotTradeScreen.kt:117-125` | 现货/策略/理财 U；闪兑正常、空、暂停 P；quote/final-review 生产路径可达，独立窗口 S，真机视觉 U。 |
| Futures 内部 4 | 合约、事件合约、TradFi、模拟盘 | `feature/trade/FuturesTradeScreen.kt:98-130` | 前 3 U；模拟盘明确后端契约缺失，不完成。 |
| 交易全屏 2 | 现货历史、合约历史 | `app/AppRoot.kt:153-156` | U。不能拿全 503 首 Tab 代替各历史 Tab、数据/改单/撤单。 |
| More 8 | 菜单、福利、跟单、通知、卡券、合伙人、TradFi、工单 | `feature/more/MoreCommon.kt:38-90` | 全 U；跟单/工单需要下层子路由。 |
| Profile 5 | HUB、账号切换、安全、KYC、子账户 | `feature/profile/UserCenterScreens.kt:36-59` | 安全在本轮只补账户密码及防钓鱼码；其余仍 U。KYC、账号/子账户必须看创建和提交反馈态。 |

## B. 子路由与承载页（逐项九 Auth + 七 Assets + 五 Security）

| 分区 | 页面/子路由 | 生产依据 | 当前状态 |
|---|---|---|---|
| Auth 9 | LOGIN；PASSWORD；VERIFY_LOGIN；REGISTER；VERIFY_REGISTER；SET_PASSWORD；FORGOT；VERIFY_RESET；RESET | `feature/auth/AuthFlow.kt:63-73,83-119` | 生产共有 9 个声明目的，其中 LOGIN/REGISTER 为两个入口；登录三步、注册三步、找回三步；九个 destination 各自保留，不隐藏。注册/找回的 P 不含实际开通账户；仅到最终表单，不提交。 |
| AssetsRoute 7 | Transfer；CoinPicker(Deposit/Withdraw 两 mode)；Deposit；Withdraw；AddressBook；AddAddress；WithdrawRecords | `data/assets/AssetsNav.kt:8-17`；`feature/assets/AssetsHost.kt:24-77` | Deposit 正常/空/错误 P；Withdraw 空网络/错误 P，正常安全确认可达但独立窗 S。其余 U，直到对应 P/V。CoinPicker 两个 mode 不合并。 |
| Security 5（新增） | CENTER；BIND；DISABLE；PASSWORD；ANTI_PHISHING | `feature/profile/SecurityScreens.kt:30-43` | 密码/防钓鱼码已实现 OpenAPI 与二次安全确认，生产 mock 表单 P；独立弹层像素 S，真机 U。邮箱/手机/Passkey/资金密码仍占位；BIND 的 QR 只能采用明确无效测试数据作布局，不可称真实绑定通过。 |
| KYC 2 | 状态；基础/高级表单（包括 Lv1/Lv2、提交错误/成功） | `feature/profile/KycScreens.kt:45-55,115-175` | U；表单必须独立，不能只看状态卡。 |
| Copy 2 | 带单员详情；我的跟单列表/停止流 | `feature/more/CopyTradingScreen.kt:142-187,243-307` | U；列表截屏不能代替详情和停止确认。 |
| Support 3 | 列表；新建；详情/回复 | `feature/more/SupportTicketsScreen.kt:163-183,187-315` | U；还需关闭工单结果和冲突态。 |

注：这里按“流程节点”而不是旧的把 9 个目的地标成 8 个的总和，Auth 仍应在详表逐一保留 LOGIN、PASSWORD、VERIFY_LOGIN、REGISTER、VERIFY_REGISTER、SET_PASSWORD、FORGOT、VERIFY_RESET、RESET 九条生产目的地，绝不隐藏任一条。

## C. 生产 Sheet / Dialog（按实际交互逐项；不与业务状态计数）

任何未列入“本轮 P/W”的项均为 U。历史 LocalLbSheetInlineEvidence 图只是组件预览，不能算这些项通过。

| 宿主 | 交互/弹层 | 生产依据 | 当前状态 |
|---|---|---|---|
| Auth | 国家/地区；注册邀请码展开；注册退出挽留；收不到验证码帮助 | `AuthFlow.kt:131-151,225-233,240-304,398-411` | U；仅验证码帮助对生产路径停到窗口限制 S；注册挽留/国家选择 U。第三方登录/客服 Toast 尚未接功能。 |
| 现货交易 | 币对；订单类型；盘口汇总；价格精度；主题；手续费；下单确认；订单区筛选 | `SpotTradeScreen.kt:108-141,181-184,235-276` | U；更多按钮仍 Toast，现货撤单需补确认和反馈态。 |
| 合约交易 | 币对；委托类型；盘口；精度；主题；仓位模式；杠杆；开平确认；工具 | `FuturesTradeScreen.kt:160-173,375-400` | U。 |
| 仓位/委托 | 市价平仓；撤普通单；撤条件单；一键平仓；TPSL；调保证金；改单；计算器 | `FuturesTradeScreen.kt:402-411` | U；每项均需正常填写、校验、最终确认/结果 mock，且不得发真实交易。 |
| 事件 | 规则；看涨/看跌确认及结果 | `EventContractScreen.kt:529-583` | U。 |
| 历史 | 币对筛选（允许全部）；条件单/止盈止损改单；撤单流 | `TradeHistoryScreens.kt:268-284,365-385,610-645` | U。 |
| 策略 | 创建网格；复制策略；停止网格（撤单及卖底仓选择） | `SpotSubpages.kt:484-550,554-630` | U；保留现有停网格防重与确认。 |
| 理财 | 申购；赎回；持仓/订单/流水 | `SpotSubpages.kt:633-716` | U。 |
| 闪兑 | 兑出/兑入、报价有效期、暂停币对、接受报价结果 | `SpotSubpages.kt:722-845` | 正常/空/暂停 P；报价→最终币额 Sheet 可达，accept 未提交；独立窗 S，接受报价结果 U；保留 canAccept/quote revision 资金闸。 |
| 资产 | 提现方式；资产筛选；搜索；快捷买币/盈亏入口 | `AssetsScreen.kt:72-82,138-167,245-265` | U；快捷买币/盈亏及理财/策略/TradFi 顶栏断链。 |
| 充值 | 网络；自动到账账户选择 | `DepositScreens.kt:141-175,287-301` | 正常/空/明确错误 P；正确地址是在线网络 mock 的无效演示值，不得转账。弹层及 AppRoot 点击 U，不拿合成预览代替。 |
| 提现 | 网络；链上/站内最终安全验证（2FA 或非 2FA） | `WithdrawScreen.kt:77-138,261-379` | 正常安全确认可达，独立窗 S；空网络/错误 P；真实提交/最终成功 U。 |
| 地址簿 | 白名单验证；改白名单；新增选择币种/网络；撤销 pending 提现 | `AddressBookScreen.kt:70-187,289-300,372-419` | U。 |
| More | 卡券激活确认；合伙人申请；跟单；跟单二次确认；成为带单员；停止跟单；关闭工单 | `CouponsScreen.kt:94-132`；`PartnerScreen.kt:262-277`；`CopyTradingScreen.kt:311-365`；`SupportTicketsScreen.kt:309-314` | U。 |
| Profile | 修改昵称；退出；移除本机账号；创建子账户；2FA 关闭确认 | `UserCenterScreens.kt:76-158,179-223`；`SubAccountsScreen.kt:42-88`；`SecurityScreens.kt:182-197` | U。 |
| 安全（新） | 登录密码安全确认；防钓鱼码安全确认 | `SecurityFeatureScreens.kt` | 表单 P；独立弹层像素 S。契约按生成模型 loopback 测试，真机 W/V U。 |

## D. 状态与适配横切分母

上述每个入口还须记录：正常数据、未登录、空、网络/后端错误、输入校验、进行中、最终确认，以及可达成功/失败反馈。**只验证当前风险流程适用的状态，不能减成每页 1 图。**

截图 manifest 必须包含测试、入口、路径、App content 宽高与系统 inset、实际 PNG 宽高、真实窗口/非窗口标志、Mock API 场景、设计参考、视觉检查结论。

当前适配目标为关键页 320/360/412dp、大字体、明暗，而不是固定 390dp 一批通过。

本批 **21=16 passed+0 failed+5 skipped**（6 安全契约、15 UI/状态，五项 Window capture S）；18 张 PNG = 10 Auth、3 充值、3 闪兑（normal/empty/paused）、2 安全表单。每页业务正常、输入错误、API failure、提交结果各自保留，不能用 18 张推导整页视觉全过。

本批启用项只为 `zh-rCN-w390dp-h844dp-xxhdpi` **浅色、fontScale=1**；实际 content 为 1170×2364px（390×788dp），配置 h844 不能代替可用 content 高度。其余宽度/暗色/大字体/RTL/IME/旋转均 U。
`OffscreenVisualEvidenceTest` 固定配置并 `requiredSize(390,844)` 的历史 PNG 只能标为合成预览，不计入 P/V。
`LayoutAdaptSmokeTest` 的合成历史/事件骨架增资不映射为对应生产页面通过。

`ProductionEvidenceHarness` 从 Activity decor 按 `android.R.id.content.getGlobalVisibleRect` 裁取同尺寸区域，校验非零/边界/无裁切，记录 systemBar/navigation/IME insets；没有叠加错误文字、status/navigation bar UI 或 inline sheet。

## E. 占位及后端依赖清单

| 功能 | 当前状态 | 依据 / 后续优先级 |
|---|---|---|
| 模拟盘、价格预警 | 尚未实现；**正式 OpenAPI 仍无 user CRUD**，不得标完成 | `FuturesTradeScreen.kt:129-140`；More 预警导航仍需对齐现有完整路由。仅准备草案 `/home/box/linbit-handoff/reviews/scope-sim-price-alerts.md`，不称后端已就绪。 |
| 安全：邮箱/手机/Passkey/资金密码 | 占位；联系方式的 bind_new 与 security 双 code 以及 Passkey 平台集成均须独立实现 | `SecurityScreens.kt:63-70`；优先邮箱/手机和资金密码，勿把已有 generated model 当成 UI 已完成。 |
| 安全：登录密码/防钓鱼码 | 本批实现已接 PUT/POST，二次验证、确认和刷新；需 mock/W/V 与真机回读 | `SecurityFeatureScreens.kt`；`LinbitApi.kt`；`mobile/openapi.yaml:637-663,786-806`。 |
| 资产：理财/策略/TradFi、快捷买币、盈亏分析 | 断链 Toast | `AssetsScreen.kt:57,80-82,245-248`；已有功能页优先做实际导航，再单装快捷功能契约。 |
| 首页搜索、扫码、Card；More 搜索、聊天室和行情 TradFi；授权登录三方/客服 | 未接 Toast | `AppRoot.kt:59-68`；`AuthFlow.kt:134-143`；逐条对接已有页或保留阻断。 |
| 提现/闪兑/停网格 | 已有生产安全闸；仍需隔离服务的状态与最终后端契约核验 | 本批不改变确认、防重、防旧 quote；截图确认不是提交成功。 |

## F. 本批逐项结果（以本批外部 enabled XML / 人工视觉复核为准）

| case | 实际目标 | Mock/结果 |
|---|---|---|
| Auth login / password / verify | 三步生产 AuthFlow/NavHost，loopback challenge | P；3 张 app-content PNG，local invalid token，无登录 verify 或 token；AppRoot 入口/暗色/真机 U |
| Auth register / verify / set-password | 注册到密码表单 | P；3 张，使用无效 OTP=000000，未调用 `/auth/register` |
| Auth forgot / verify-reset / reset | 找回密码到最终表单 | P；4 张，`INVALID_RESET_TOKEN`；不调用 password reset |
| Auth 九目的地与三流程 API 失败、输入错误、已提交反馈 | 每个状态生产证据 | 九目的地有 P，错误/负载/完成状态 U，不将多步通过等同账户流程成功 |
| Auth modal help / country / exit | 真实弹层 | 帮助 Sheet 生产语义可达；独立 window W=S，country/exit U；PixelCopy instrumentation 只编译 |
| Assets deposit normal/empty/error | 直接生产 AssetsHost + 明确 GET | P；3 张；没有把 ref2/15 错当充值参考 |
| Assets withdraw normal/error/confirm | Host/form/2FA sheet | 空网络/错误 P；正常安全确认含真实网络+余额，并 assert 无 POST；独立窗 S |
| Assets convert normal/empty/paused/confirm | 直接生产 ConvertSubpage + GET/quote mock | normal/empty/paused P；最终币额 Sheet 不可离屏像素验 MA=S；禁止 accept；保留 quote revision |
| Security password / anti-phishing | 真实 SecurityHost + 二次确认；表单 P | 2 张表单，mock 无敏感提交；独立窗 S。六项安全契约测试含 fail-closed 校验与精确 header/body |
| 逐图视觉 V | ref2/3、10、11 对应登录/密码/OTP | 本批视觉：**C / U(full visual pass)**；已 Read 18 张，并核对 ref2/3、10、11 布局；尺寸/底部无裁切已核，但没有系统性像素/真机比对足够据以给 V 全过。没有直接参考的页面仅 P；暗色/小屏/大字体/真机从未执行 |

## G. 本批评判方式

1. 真正启用 Offscreen 新生产套件，独立复制 XML、manifest 与 PNG 后才运行默认总套件。
2. App 位图采取根 `android.R.id.content` 的实际尺寸，布局与位图必须是同一矩形；罗列出 status/navigation/IME 值。禁止强制 844dp 视图再拍 788dp。
3. ModalBottomSheet/Dialog 只接受真实 Production `DialogWindowProvider.window`；Robolectric 无法证明独立 window capture 时，用 Assume 将其明确标 S，不 retry inline、不替代主内容截图。最终像素归 `ProductionDialogPixelCopyTest` 的支持环境执行，NOT RUN 时不得标 W。
4. MockWebServer 只允许 loopback；显式按方法+path+query 响应。未配置路由直接失败，不能全部 503 后仍主张正常页面证据。
5. 资金、开通账户、绑定 2FA、KYC 均只到最终表单/确认层；测试输配若带提交动作则必须在 Mock API 中 fail closed。这些图片不证明任何真实交易或身份流程已完成。

## H. 下一批优先（不等催）

1. 在支持的环境运行并人工核验五个 `ProductionDialogPixelCopyTest`，同时生成真实 AppRoot 从生产导航进入的记录；没有设备结果就维持 S/U。
2. 用已有 OpenAPI 接通资产入口到理财/策略/TradFi，而不是更多 Toast；保留真实资金确认与防重。
3. 安全邮箱/手机复用 `bind_new` + security 双验证码；资金密码及 Passkey 分开实现，不把 layout invalid 数据当真实绑定。
4. 对每个生产 Sheet/Dialog 补正常、空、错误、确认/最终反馈，以及 320/360/412dp、大字体、暗色；缺 OpenAPI 的功能保留明确阻断，不伪造。
