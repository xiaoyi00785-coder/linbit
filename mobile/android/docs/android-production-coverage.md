# Android 生产覆盖与验收分母

状态是**已建立可复核生产 mock 证据**或**未验**，不再以“某宿主至少一张 PNG”算整页/整流程通过。旧 36/36 结论已撤销。

本批生产套件读取 `PRODUCTION_EVIDENCE_OUT` 外部输出中的 `junit-summary.json`；此 SHA 在本地未提交验证时为父 SHA，必须结合 `run.properties` 的 `verifiedWorkingTree=local-uncommitted-validation` 阅读，不得用旧 SHA 父 CI 代表本批修改。默认套件及历史预览不是本批 enabled 证据。所有像素均为 app-content-only，不是整机 full-screen；图元描述需要和 png JSON 同时阅读。
截图范围、mock 响应和 JUnit 均须单独留存。合成 fixture 的历史 round1-7 仅作为组件预览，不能计作生产视觉通过。
真机由用户验收；离屏与 instrumentation 不替代真机。

## 证据等级

- `P`：通过真实生产 Host/NavHost 或其明确记录的生产子页入口，数据由隔离 MockWebServer 提供，使用真实 inset 和实际窗口布局。`P`不等于从 AppRoot 全路径点击、不排斥离屏限制或字段级业务覆盖；两 host 分身不在范围内不能算补齐。
- `W`：真实生产 ModalBottomSheet/Dialog 窗口可独立捕获，且与内容截图分开标记；不能遇到限制就降级 inline sheet。
- `V`：已 Read 对参考图核对配色、字号/字重、基线、间距及必要适配组合；不是测试 pass 的同义词。
- `C`：实际内容矩形/裁切机器校验，按轮次新的PNG逐张 Read；有 ref 的列表与字重/间距已人工看，但每个页面都有直接参考和所有适配未齐，完整视觉 V 仍 U。
- `U`：未验、当前环境不可验或仍缺功能。请勿转换为通过。
- `S`：模拟器/离屏器 limitation，必须提供可在支持环境跑的 instrumentation 用例。

以下完整命名条目是本批评审基线，不宣称具有伪精确的全部通过率；生产弹层、业务状态、Tab 与适配不能混为一个数字。

## A. 根级与功能宿主（28 个入口）

| 分区 | 项 | 生产入口 | 当前证据 / 未查与占位 |
|---|---|---|---|
| MainTab 5 | Home；Markets；Futures；Spot；Assets | `app/AppRoot.kt:87-120`；`ui/components/TabBar.kt:37-42` | 全 U（尚待 P/V）。Markets 聊天室与 TradFi、模拟盘、资产部分功能仍占位；Futures/Spot 的子页不能归并进根截图。 |
| Spot 内部 4 | 现货交易、策略、理财、闪兑 | `feature/trade/SpotTradeScreen.kt:117-125` | 现货 U；策略/理财本批 P(empty)；闪兑正常、空、暂停 P；quote/final-review 生产路径可达，独立离屏Window实测；PixelCopy/真机视觉 U。 |
| Futures 内部 4 | 合约、事件合约、TradFi、模拟盘 | `feature/trade/FuturesTradeScreen.kt:98-130` | 前 3 U；模拟盘明确后端契约缺失，不完成。 |
| 交易全屏 2 | 现货历史、合约历史 | `app/AppRoot.kt:153-156` | U。不能拿全 503 首 Tab 代替各历史 Tab、数据/改单/撤单。 |
| More 8 | 菜单、福利、跟单、通知、卡券、合伙人、TradFi、工单 | `feature/more/MoreCommon.kt:38-90` | 全 U；跟单/工单需要下层子路由。 |
| Profile 5 | HUB、账号切换、安全、KYC、子账户 | `feature/profile/UserCenterScreens.kt:36-59` | 安全本轮补账户密码、防钓鱼码、邮箱、手机、资金密码；Passkey仍占位。KYC、账号/子账户必须看创建和提交反馈态。 |

## B. 子路由与承载页（逐项九 Auth + 七 Assets + 八 Security）

| 分区 | 页面/子路由 | 生产依据 | 当前状态 |
|---|---|---|---|
| Auth 9 | LOGIN；PASSWORD；VERIFY_LOGIN；REGISTER；VERIFY_REGISTER；SET_PASSWORD；FORGOT；VERIFY_RESET；RESET | `feature/auth/AuthFlow.kt:63-73,83-119` | 生产共有 9 个声明目的，其中 LOGIN/REGISTER 为两个入口；登录三步、注册三步、找回三步；九个 destination 各自保留，不隐藏。注册/找回的 P 不含实际开通账户；仅到最终表单，不提交。 |
| AssetsRoute 7 | Transfer；CoinPicker(Deposit/Withdraw 两 mode)；Deposit；Withdraw；AddressBook；AddAddress；WithdrawRecords | `data/assets/AssetsNav.kt:8-17`；`feature/assets/AssetsHost.kt:24-77` | Deposit 正常/空/错误 P；Withdraw 空网络/错误 P，正常安全确认可达但独立离屏Window按manifest，PixelCopy/真机U。其余 U，直到对应 P/V。CoinPicker 两个 mode 不合并。 |
| Security 8 | CENTER；BIND；DISABLE；PASSWORD；ANTI_PHISHING；EMAIL；PHONE；FUND_PASSWORD | `feature/profile/SecurityScreens.kt:30-52` | 密码/防钓鱼/邮箱/手机/资金密码已实现 OpenAPI 与生产复核，表单 P；离屏独立Window实测，PixelCopy/真机 U。Passkey仍占位；BIND 的 QR 只能采用明确无效测试数据作布局，不可称真实绑定通过。 |
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
| 闪兑 | 兑出/兑入、报价有效期、暂停币对、接受报价结果 | `SpotSubpages.kt:722-845` | 正常/空/暂停 P；报价→最终币额 Sheet 可达，accept 未提交；独立离屏Window按manifest，PixelCopy/真机U，接受报价结果 U；保留 canAccept/quote revision 资金闸。 |
| 资产 | 提现方式；资产筛选；搜索；快捷买币/盈亏入口 | `AssetsScreen.kt:72-82,138-167,245-265` | U；快捷买币/盈亏仍占位；理财/策略/TradFi 顶栏已接真实 Host，但这里没有把它们标为快捷入口或弹窗P。 |
| 充值 | 网络；自动到账账户选择 | `DepositScreens.kt:141-175,287-301` | 正常/空/明确错误 P；正确地址是在线网络 mock 的无效演示值，不得转账。弹层及 AppRoot 点击 U，不拿合成预览代替。 |
| 提现 | 网络；链上/站内最终安全验证（2FA 或非 2FA） | `WithdrawScreen.kt:77-138,261-379` | 正常安全确认可达，独立离屏Window按manifest，PixelCopy/真机U；空网络/错误 P；真实提交/最终成功 U。 |
| 地址簿 | 白名单验证；改白名单；新增选择币种/网络；撤销 pending 提现 | `AddressBookScreen.kt:70-187,289-300,372-419` | U。 |
| More | 卡券激活确认；合伙人申请；跟单；跟单二次确认；成为带单员；停止跟单；关闭工单 | `CouponsScreen.kt:94-132`；`PartnerScreen.kt:262-277`；`CopyTradingScreen.kt:311-365`；`SupportTicketsScreen.kt:309-314` | U。 |
| Profile | 修改昵称；退出；移除本机账号；创建子账户；2FA 关闭确认 | `UserCenterScreens.kt:76-158,179-223`；`SubAccountsScreen.kt:42-88`；`SecurityScreens.kt:182-197` | U。 |
| 安全（新） | 登录密码/防钓鱼/邮箱/手机/资金密码安全确认 | `SecurityFeatureScreens.kt`、`SecurityContactScreens.kt` | 表单 P；独立离屏Window按本批实际manifest，PixelCopy/真机U。契约按生成模型 loopback 测试，真机PixelCopy/V U，禁止 UI 自动发码/绑定。 |

## D. 状态与适配横切分母

上述每个入口还须记录：正常数据、未登录、空、网络/后端错误、输入校验、进行中、最终确认，以及可达成功/失败反馈。**只验证当前风险流程适用的状态，不能减成每页 1 图。**

截图 manifest 必须包含测试、入口、路径、App content 宽高与系统 inset、实际 PNG 宽高、真实窗口/非窗口标志、Mock API 场景、设计参考、视觉检查结论。

当前适配目标为关键页 320/360/412dp、大字体、明暗，而不是固定 390dp 一批通过。

round1 保留 **21=16 passed+0 failed+5 skipped**；round2 独立脚本见 `production-evidence.sh` 与外部 `junit-summary.json`，以同一次结果计数，不把各次Debug结果拼接。round1实际content为1170×2364px；round2适配实际dp按各自真实host记，不沿用390常量。

round1 **21=16 passed+0 failed+5 skipped**，18张PNG；round2独立 summary 为本轮唯一计数，不得与旧默认XML拼接。
round1启用项只为 `zh-rCN-w390dp-h844dp-xxhdpi` **浅色、fontScale=1**；实际 content 为 1170×2364px（390×788dp），配置 h844 不能代替可用 content 高度。其余宽度/暗色/大字体/RTL/IME/旋转均 U。
`OffscreenVisualEvidenceTest` 固定配置并 `requiredSize(390,844)` 的历史 PNG 只能标为合成预览，不计入 P/V。
`LayoutAdaptSmokeTest` 的合成历史/事件骨架增资不映射为对应生产页面通过。

`ProductionEvidenceHarness` 从 Activity decor 按 `android.R.id.content.getGlobalVisibleRect` 裁取同尺寸区域，校验非零/边界/无裁切，记录 systemBar/navigation/IME insets；没有叠加错误文字、status/navigation bar UI 或 inline sheet。

## E. 占位及后端依赖清单

| 功能 | 当前状态 | 依据 / 后续优先级 |
|---|---|---|
| 模拟盘、价格预警 | 尚未实现；**正式 OpenAPI 仍无 user CRUD**，不得标完成 | `FuturesTradeScreen.kt:129-140`；More 预警导航仍需对齐现有完整路由。仅准备草案 `/home/box/linbit-handoff/reviews/scope-sim-price-alerts.md`，不称后端已就绪。 |
| 安全：Passkey | 占位，不能拿本批其他安全契约当完成功能 | `SecurityScreens.kt`；platform CredentialManager begin/finish与列表/撤销尚需独立安全实现。 |
| 安全：邮箱/手机/资金密码 | 正式 OpenAPI + Retrofit +生产 Host 已接；P只覆盖表单与mock body，发送错误/SMS即使禁用/真实绑定及窗 W/V 未完成 | `SecurityContactScreens.kt`；双验证码严格区分bind_new/security，2FA开启另需totp。 |
| 安全：登录密码/防钓鱼码 | 本批实现已接 PUT/POST，二次验证、确认和刷新；需 mock/W/V 与真机回读 | `SecurityFeatureScreens.kt`；`LinbitApi.kt`；`mobile/openapi.yaml:637-663,786-806`。 |
| 资产：理财/策略/TradFi | 已接生产 AssetFeatureHost，非 Toast，P仅为只读/错误/资产点击到理财的 mock 路径，资金动作仍 U | `AssetsScreen.kt`、`AssetFeatureHost.kt`、`app/AppRoot.kt`。 |
| 资产：快捷买币、盈亏分析 | 仍为占位，不能默默从范围删除 | `AssetsScreen.kt:245-248`；尚无完整生产宿主与状态验收。 |
| 首页搜索、扫码、Card；More 搜索、聊天室和行情 TradFi；授权登录三方/客服 | 未接 Toast | `AppRoot.kt:59-68`；`AuthFlow.kt:134-143`；逐条对接已有页或保留阻断。 |
| 提现/闪兑/停网格 | 已有生产安全闸；仍需隔离服务的状态与最终后端契约核验 | 本批不改变确认、防重、防旧 quote；截图确认不是提交成功。 |

## F. round1 历史逐项结果（不能代表round2；以各轮外部 enabled XML / 人工视觉复核为准）

| case | 实际目标 | Mock/结果 |
|---|---|---|
| Auth login / password / verify | 三步生产 AuthFlow/NavHost，loopback challenge | P；3 张 app-content PNG，local invalid token，无登录 verify 或 token；AppRoot 入口/暗色/真机 U |
| Auth register / verify / set-password | 注册到密码表单 | P；3 张，使用无效 OTP=000000，未调用 `/auth/register` |
| Auth forgot / verify-reset / reset | 找回密码到最终表单 | P；4 张，`INVALID_RESET_TOKEN`；不调用 password reset |
| Auth 九目的地与三流程 API 失败、输入错误、已提交反馈 | 每个状态生产证据 | 九目的地有 P，错误/负载/完成状态 U，不将多步通过等同账户流程成功 |
| Auth modal help / country / exit | 真实弹层 | 帮助 Sheet 生产语义可达；离屏Window分批实测；PixelCopy/device U，country/exit U；PixelCopy instrumentation 只编译 |
| Assets deposit normal/empty/error | 直接生产 AssetsHost + 明确 GET | P；3 张；没有把 ref2/15 错当充值参考 |
| Assets withdraw normal/error/confirm | Host/form/2FA sheet | 空网络/错误 P；正常安全确认含真实网络+余额，并 assert 无 POST；独立离屏Window按manifest，PixelCopy/真机U |
| Assets convert normal/empty/paused/confirm | 直接生产 ConvertSubpage + GET/quote mock | normal/empty/paused P；最终币额 Sheet 独立Dialog Canvas与Window回调可达；PixelCopy/真机U；禁止 accept；保留 quote revision |
| Security password / anti-phishing | 真实 SecurityHost + 二次确认；表单 P | 2 张表单，mock 无敏感提交；独立离屏Window按manifest，PixelCopy/真机U。六项安全契约测试含 fail-closed 校验与精确 header/body |
| Earn/Strategy/TradFi + 三安全表单 | 本批新增生产Host；只读空/503/form/review与mock精确body | P/C；Window见本轮独立capture，PixelCopy/device U；申购/赎回/网格写链U；Passkey占位 |

| 逐图视觉 V | ref2/3、10、11 对应登录/密码/OTP | round1视觉：**C / U(full visual pass)**；已 Read 18 张，并核对 ref2/3、10、11 布局；尺寸/底部无裁切已核，但没有系统性像素/真机比对足够据以给 V 全过。没有直接参考的页面仅 P；round1暗色/小屏/大字体/真机未执行；round2按实际组合另表，不再覆盖全部页面 |

## F2. round2 当前结果与分母

本轮XML **44=43 passed+0 failed+1 skipped；44 PNG**（安全契约10、UI/状态34）。唯一S是真实认证help Window不能Canvas attach，其他真实Window仅计offscreen独立Dialog Canvas；八项设备PixelCopy另计全NOT RUN。round2并没有用44反推44个功能全完成。新适配只覆盖 Earn empty、Email form、Email review Window：

- 320dp浅色fontScale1.3，xxhdpi；真实App content为960×2364px（320×788dp）。
- 360dp深色fontScale1.0，xxhdpi；真实App content为1080×2364px（360×788dp）。
- 412dp浅色fontScale1.0，xxhdpi；真实App content为1236×2577px（412×859dp）。
- 390dp浅色fontScale1.0基准及旧路径回归，1170×2364px。

实际读取者必须逐张Read本次文件，不把XML passed或没有异常当V。所有Window声明必须manifest productionDialogWindow=true且canvas记独立Dialog；PixelCopy八项只编译从未执行。新的P不等于全功能或全适配，320深色/360大字/412深色、横屏、IME、RTL仍U。三安全mock body与UI review不证明真实发码、绑定或改密成功。申购/赎回/网格mutation未验收。快捷买币/盈亏、模拟盘/预警、Passkey继续U。

## G. 本批评判方式

1. 真正启用 Offscreen 新生产套件，独立复制 XML、manifest 与 PNG 后才运行默认总套件。
2. App 位图采取根 `android.R.id.content` 的实际尺寸，布局与位图必须是同一矩形；罗列出 status/navigation/IME 值。禁止强制 844dp 视图再拍 788dp。
3. ModalBottomSheet/Dialog 只接受真实 Production `DialogWindowProvider.window`，从Modal作用域捕获；Robolectric独立Dialog Canvas不等同instrumentation PixelCopy。无法证明时Assume为S，不retry inline。`ProductionDialogPixelCopyTest`八项 device/PixelCopy均NOT RUN，不自比设备通过。
4. MockWebServer 只允许 loopback；显式按方法+path+query 响应。未配置路由直接失败，不能全部 503 后仍主张正常页面证据。
5. 资金、开通账户、绑定 2FA、KYC 均只到最终表单/确认层；测试输配若带提交动作则必须在 Mock API 中 fail closed。这些图片不证明任何真实交易或身份流程已完成。

## 2026-11-06 Root follow-up (verified current working tree; subsequent validation below)

The prior four enabled AppRoot failures are now superseded by the independent `tpsl-root-fixed-xml` result: **4 tests / 0 failures / 0 errors / 0 skipped**, at 320dp/fontScale1.3/light and 360dp/dark. Both cases per configuration use the real production `AppRoot` floating bottom tab -> `FuturesTrade` history icon -> `FuturesHistoryScreen` -> TPSL -> final `撤销止盈止损？` Window. No Host substitute, overlay, callback replacement or new route is used. The `historyKind=0` hypothesis was discarded after counting actual owner invocations in the saved `tpsl-root-diagnostics-*` traces.

**Production fix:** at 320dp/fontScale1.3 the unrestricted four-trial-tab row squeezed its trailing history icon to a zero-width, offscreen hit rectangle (`l=r=924px` in the saved semantic diagnostic). Both direct Click and physical-center Touch therefore had historyFactory=0 after idle and after one second, proving it was not an untested coroutine wait. `FuturesTradeContent` now grants the original history action a 48dp touch target (original 20dp image), allows only the orders/positions tabs to horizontally scroll, and retains the original `onHistory` callback. A test asserts the real semantics hit region is at least 48dp on both axes. At 360 the sample immediately reached the actual history, so this was a narrow large-font layout fix rather than an owner/navigation rewrite.

**Harness fixes:** the fixture lacked exact Request paths used by a selected `BTCUSDT` history host: `GET /futures/orders?symbol=BTCUSDT&status=open&limit=100` and `GET /futures/tpsl-orders?symbol=BTCUSDT&status=active&limit=50`; its previous no-symbol route withheld a valid response. The intermediate 360 loading PNG was a frame captured before this rejected request became the visible API error, not a production perpetually-loading defect. The final harness records the exact current-symbol read, waits for the real `撤销` action before screenshots, and checks two isolated exact numeric-id loopback DELETEs for a failed confirmation and retry. No remote endpoint, funds/order/security/alarm write or general futures mutation permission was added.

`tpsl-root-fixed-evidence` has nine independent content/Dialog PNGs and matching manifests. Their manifests explicitly map the successful `Verified production AppRoot` route. Read results: both sizes show a real BTCUSDT TPSL row and `撤销`, both show the independent review Window with `确认撤销` and `取消`, and 320 after-retry displays `AFTERREADBACK` under the selected filters. Required touch dimensions are not inferred from PNG appearance. These are offscreen Canvas/Android Robolectric captures, not device install/reference-identical acceptance.

The immutable four isolated state tests remain **4 / 0 / 0 / 0** in `tpsl-root-v2-state-unit`: obsolete filter, duplicate-confirmation/retry, obsolete pagination and session rejection. They are not replayed as Root evidence or combined with its totals. Sequential `:app:compileDebugKotlin`, `:app:compileReleaseKotlin`, `:app:lintDebug` and default `:app:testDebugUnitTest` results for this follow-up are in `tpsl-root-fixed-build.jsonl` and its newly copied default XML; no enabled Root results are overwritten by the default suite. New default total is **342 tests / 0 failures / 0 errors / 118 skipped**; the 8 Root-enabled skips remain disabled in default rather than accepted passes.

Remaining next item: t83u/t89u existing scoped host/flow acceptance only after a closeout review of their current states, not a broader visual matrix or claim of complete Android. This narrow Root batch does not produce a debug APK for internal delivery, sign a release, run an install, or solve any real-device financial/security flow. No iOS scope was added.

## H. 下一批优先（不等催）

1. 在支持的环境运行并人工核验八个 `ProductionDialogPixelCopyTest`，同时生成真实 AppRoot 从生产导航进入的记录；没有设备结果就维持 S/U。
2. 验收资产->策略/TradFi点击，补真实正常持仓/产品列表与安全交互，不误把当前空/错状态覆盖当完成。
3. Passkey 分开实现平台凭据流程，其他安全项补390外/错误/发送/提交结果；真实绑定全部留给用户自行验收。
4. 对每个生产 Sheet/Dialog 补正常、空、错误、确认/最终反馈，以及 320/360/412dp、大字体、暗色；缺 OpenAPI 的功能保留明确阻断，不伪造。

## 2026-11 current Passkey / remaining-work supersession

The following current findings do not rewrite historical rounds above. They replace only stale current-state labels for paper, alerts, and Passkey. Historical `P/W/V/U` evidence labels remain historical and do not imply full-device acceptance.

### Current delivered API wiring (not wholesale work remaining)

- `9939015`: passkey login begin/finish, `androidx.credentials.CredentialManager.getCredential`, generation/session scoped token persistence; isolated wire, recovery, stale-result tests.
- `0944b43`: passkey register begin/finish, `CredentialManager.createCredential`, real security-center entry and server list readback after finish; lifecycle, duplicate click, cancellation, recovery, stale finish tests and host screenshots.
- This deletion batch: authenticated `DELETE /account/passkeys/{id}` behind a distinct final-review Sheet. It explicitly states that the deleted credential cannot then login to this account. Cancellation before request performs no DELETE. Failures retain the target for retry. Duplicate confirmation is blocked. After deletion, a current-session list GET is required before publishing success. Old session responses cannot read back or overwrite next-account list state.
- The current public DELETE request has **no second-factor argument**. Its Sheet says it is frontend confirmation only and does not claim server 2FA validation. If product policy requires deletion-specific security proof, a backend contract addition and an authorization review are required; inventing such an endpoint is prohibited.
- The public system native bridge remains available and compilable, but no system Credential Manager sheet, real authenticator, or RP/device ceremony was validated by these host tests. Missing RP assetlinks/final release-certificate association remains a real functional prerequisite; this batch makes no new probe and no deployment/config/signing modification. APK delivery is not conditional on my device ceremony.

### Actual remaining Android work (current source / host / production evidence scope)

- Final packaging/installable APK and release-signature owner metadata remain the Android-first delivery objective. Full-device presentation, OS Permission flows, full AppRoot click-path matrix, real third-party provider login and real sensitive submissions are not converted into success by local test totals.
- Existing Apple/Google/social-auth, scanner and server-search helper routes need real app/provider flows and verification, not Toast stubs. Remove "complete" assumptions based merely on an API contract path. Search/QR/Card/PNL/quick-buy and margins/TP-SL advanced shortcuts still need either complete scoped host/docs flows or a named backend/UX dependency.
- Existing in-app hosts (history/orders, earn/grid, copy-trader, notifications/settings, partner, coupon/support, KYC/subaccounts, address-book, transfers, withdraw) still need a case-by-case UI and API route/response/retry/readback audit. These are not declared absent wholesale; non-read-only money/KYC/binding writes remain explicitly outside my isolated implementation tests. Missing or partial flows must be reported individually before calling them delivered.
- Asset feature Hosts, including paper account and price-alert CRUD, **are already implemented**, with explicit feature gates and preconditioned isolated tests from `software3f0a...` through `598525a` baseline (see `git log`). They must not be labelled "no formal CRUD / not wired". Their final device/installable package coverage, push/delivery scheduling and production release linkage remain to report independently; no live alerts/notifications/security/funds writes were performed.
- Data/state regressions requiring additional app host/Dialog evidence include history filters, payout/withdrawal security results, grid stop/convert acceptance, support closure/conflicts/replies, KYC submit/reject, security send-code/results, and 320/360/412 dark/large-font complete V/C coverage. A PNG alone or an old 36/36 table is not full functionality sign-off.
- Endpoint recovery/session guards now cover Passkey login, registration, and deletion with loopback tests and native fake bridges. Before deletion-specific challenge UI is offered, the actual contract must expose and validate it server-side; until then only the current logged-in authorization plus explicit frontend confirmation exists.

## 2026-11-06 futures history/TPSL limited continuation

This is a narrowly scoped Android continuation, not a declaration that all Android or the complete AppRoot is accepted. `TradeHistoryViewModel` now has request/action epochs, cancels old initial list jobs, guards all initial futures-list and spot-list publications against obsolete sessions/filters, and centralizes cancellation success callback so an action cannot reload or publish into another session. The plan/TPSL cancel row opens a separate final-review `LbSheet`; failure leaves it open for retry and success reloads the current filter. The amend sheet also shares the production dialog-window callback. The existing futures clock and floating bottom tabs now expose accessibility/test identifiers.

**Verified with real Retrofit + explicit loopback only:** a displaced list response cannot publish after a `TokenStore.sessionVersion` change. Four direct *production `FuturesHistoryScreen`* cases pass at 320dp/fontScale1.3/light and 360dp/dark: TPSL filter and independent cancellation review (dismiss performs zero DELETE); explicit 503 retains the review/error and reenables confirm; retry is one further exact `DELETE /futures/tpsl-orders/9001`; success reads the current `GET /futures/tpsl-orders?status=active&limit=50` and displays the readback symbol. See `TradeHistoryAlgoFlowTest` and `ProductionAlgoHistoryEvidenceTest` plus isolated XML and PNG/manifest evidence. These are real Host/Dialog canvas evidence, not device-credential/funds evidence. `MockEvidenceApi` only permits **declared, exact numeric-id plan/TPSL DELETE routes** in addition to its previous scope; any absent response remains rejected, and no remote endpoint was called.

**Still not verified / remaining:** The actual `AppRoot` bottom-futures-tab -> history-clock click reaches the production history host only when its real `FuturesTradeViewModel` and history owner are provided through the new production factories (defaults remain Hilt). Before that fix, the uninjectable test Hilt owner caused composition rollback. No page or overlay is substituted. The final independent `tpsl-root-v2-state` XML is authoritative for the two chains in each configuration; all four are explicitly failed, never skipped, if even one stage fails. A screenshot in `tpsl-root-v2-evidence` alone proves only its manifest stage and real configuration, not a completed cancellation/readback chain. A cancelled diag screenshot and earlier failure trees are not evidence. Four deterministic loopback state tests now execute (none is skipped): late obsolete-filter rejection, one duplicate-click in-flight DELETE with a retained failure and exactly one retried success callback, stale pagination rejection across a changed filter, and rejection across a new session. `loadMorePositions`/`loadMoreAlgos` now share the filter-generation and session-epoch checks; a new query cancels their job; final state is not allowed to publish after invalidation. Test `Held(response)` latches, rather than sleeps, sequence actual responses and verify Retrofit/OpenAPI query argument order. This does not claim device UI acceptance for all state races. Spot history and TPSL amend were not promoted to completed acceptance merely because shared helpers are used; no real funds/order security write was made. No exact matching history/TPSL reference exists in inspected ref18 pairs (leverage/withdraw-network); visual Read is real, not reference pixel acceptance.

Evidence directories (outside the repository): `/workspace/android-verification/tpsl-history-evidence/` and `/workspace/android-verification/tpsl-history-state/`. No exact-reference comparison was claimed, no real system Credential Manager ceremony or RP assetlinks problem was solved, and no installable APK was delivered by this batch.
