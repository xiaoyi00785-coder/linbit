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
| AssetsRoute 7 | Transfer；CoinPicker(Deposit/Withdraw 两 mode)；Deposit；Withdraw；AddressBook；AddAddress；WithdrawRecords | `data/assets/AssetsNav.kt:8-17`；`feature/assets/AssetsHost.kt:24-77` | Deposit 正常/空/错误 P；Withdraw 空网络/错误 P，正常安全确认可达但独立离屏Window按manifest，PixelCopy/真机U。Transfer 已有 AppRoot 320大字/360暗色 P/W 的确认、失败重试、readback 证据（见文末）；**其他子路由仍 U**。CoinPicker 两个 mode 不合并。 |
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
| 提现 | 网络；链上/站内最终安全验证（2FA 或非 2FA） | `WithdrawScreen.kt:77-138,261-379` | 正常安全确认可达，独立离屏Window按manifest，PixelCopy/真机U；空网络/错误 P；链上已开2FA及pending撤销均有AppRoot两适配local闭环 P/W：见下方独立批次。pending不是链上到账，撤销不含completed/processing的成功断言。站内仍U。 |
| 资产划转 | 现货→合约最终复核/失败重试/成功结果 | `TransferScreen.kt`；`AssetsViewModel.kt` `transferFutures` / `refreshAcceptedTransferBalances` | AppRoot P/W：正向in及反向out（合约→现货）各有320dp fontScale1.3浅色、360dp暗色；fake token与127.0.0.1精确route/body POST；独立Window、取消0写、503重试、防重/会话隔离、spot/futures读回见下方独立批次；真机和其他设备V/C仍U，不混算funding。 |
| 地址簿 | 新增label/网络；白名单验证/edit；删除；撤销 pending 提现 | `AddressBookScreen.kt`；`AssetsViewModel.kt` | 新增/label-memo编辑及本批pending撤销各两适配P/W见独立XML；单条名单PATCH、DELETE、全局开关仍U，pending撤销不包含processing/completed可撤声明。 |
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

## 2026-11-06 USDT现货→合约划转限定批次

本批在生产已有 OpenAPI `POST /futures/transfer` 上补前端确认、同步防重和结果承载，不声称此前功能全缺。实际缺口是原按钮直接提交、无独立最终复核、无会话间事务保护，也没有可持久读的服务端结果。生产 `AppRoot` 现在把同一个 `AssetsViewModel` 和 window callback 传给原 `AssetsHost/TransferScreen`。表单通过后才打开真实生产 `LbSheet`；取消不发请求。`AssetsViewModel.transferFutures` 提交前同步置忙，另在按钮按下端立刻显示忙碌，避免Compose回调后的竞态重复。session/generation切换取消旧事务并拒绝旧结果。

503时原复核Window、金额和错误保留并允许重试；成功按真实 `status/ref` 展示，不能缺省假造completed。独立后续现货/合约GET均成功，才显示“余额已按服务端读回更新”及90/30；读回失败显示暂未更新，不伪造。反向`out`沿用实现但**未在本批验证**，不推断通过。

**独立先存证据：** `/workspace/android-verification/transfer-fixed-xml/` = 5 tests / 0 failures / 0 errors / 0 skipped。其中真正`AppRoot`两条取消0写+两条在途防重/503重试/成功/readback为4条（两适配各两条），VM重复调用与旧会话迟到结果隔离另1条，不能把两个总数混算单个Root总数。PNG及manifest在同层 `transfer-fixed-evidence/`，共8张表单/独立Window复核/失败重试/成功结果。每个尺寸的两条测试实际完成所有阶段才计入；中间旧失败XML与图没有计为通过。

已实际Read八张最终图：320大字浅色、360暗色的左右页边距、账户方向卡及居中交换图标、标题/金额字重层次、按钮对齐、红错误与回执间距均可见可读，生产截图无合成inline替代。代码注释仅指向`#46 / iOS TransferView`，本工程未定位确切transfer参考图；因此为 **no-exact-reference，未宣称reference-identical或全V**。Canvas/Android真实生产Window离屏证据不替代用户真机PixelCopy；没有为APK添加人为前置。

本组有限剩余清单（不是泛泛重审全部Android）：
1. **提现剩余 U**：本批链上已开2FA的两适配local pending闭环已验；completed/processing/failed/canceled实体Root、非2FA发码（不发送真实验证码）、链上实际结算、spot后端配置分支与真机P未验。
2. **地址簿剩余 U**：已有create的local复核/失败重试/同id列表readback/防重/会话隔离；label/memo PATCH（含单条whitelisted），DELETE及全局withdraw-whitelist写仍U。
3. **地址簿内 pending 撤销记录 U**：保留为同组原范围单独动作，当前未验，不用本批划转mock替代；需本组完成时核真实契约并做local final review/readback（若真实API行为不同则据实记录）。


该段次序此后已由下文withdraw-root fixed有界批次推进；提现剩余见第1项，地址簿/撤销仍U，不扩散到其它资产/交易/安全范围。APK/真机由用户验收，不等iOS。顺序门通过：`:app:compileDebugKotlin`（一次误删使用中的import修后成功，如实留记录）→`:app:compileReleaseKotlin`→`:app:lintDebug`→`:app:testDebugUnitTest`。默认结果为347 tests / 0 failures / 0 errors / 122 skipped，其中该批Root四项为4 skipped，未计通过；`transfer-default-xml`已保存，独立`transfer-fixed-xml`才是该批passed依据。日志见`transfer-fixed-build.jsonl`与`/workspace/transfer-{debug-compile,release-compile,lint,default}.log`。

## 2026-11-06 118f withdraw-root fixed

`ProductionWithdrawFlowEvidenceTest`的真实AppRoot路径为HOME→资产Tab→提现方式→链上提币→USDT币种→既有表单→独立安全Window。两次适配（320dp/fontScale1.3/light、360dp/dark）各执行取消零写和失败惰性POST保留后重试的pending/readback闭环。独立XML在`withdraw-fixed-xml`，四Root 4/0/0/0；默认Root skip不算passed。默认`352 tests / 0 failures / 0 errors / 126 skipped`（新增VM 1项0跳、Root 4项跳），最后生产变更后四Root再次独立4/0/0/0且顺序`:app:compileDebugKotlin`→`:app:compileReleaseKotlin`→`:app:lintDebug`→`:app:testDebugUnitTest`全BUILD SUCCESSFUL。默认XML在`withdraw-default-xml`；独立XML/8 PNG+manifest和Read/no-exact-reference结论分别保存。真实Root测试还断言local总计一个held POST（VM busy），后续仅再多一次精确POST，并禁止`POST /account/code/send`。body仅USDT/TRC20/无效演示地址/10.00/fake TOTP，未增加远端或任何安全/提醒/撤销写。

原实装缺陷修复：重试等待态不再由粘滞的UI `submitting`把持；提交防重/取消/late old-session取消进入同一生产ViewModel。成功不是跳记录即完成：必须再次overview确定debit account，读取单一资金/现货余额与`page=1,pageSize=20`记录；同id记录和资产余额都存在才显示收据。提交HTTP202/pending、record pending、90.00 USDT读回分别显示；未证结算。readback不足时不会给completed，也不会允许再次POST。表单不再合并资金+现货作为提现可扣余额。`AssetsWithdrawFlowTest`验证在途重入false及旧session只允许一次POST/不发布旧receipt。

实际Read八个图片/manifest（`withdraw-fixed-evidence`）：两适配表单/独立Window初始复核/503保留/结果。窗口报告真实`productionDialogWindow=true`、`dialogDecorViewMeasured=960x2532px/1080x2532px`；`pixelCopy=NOT_RUN`。320大字号表单可读并滚动到底部记录，360错误和pending暗夜对比清楚；manifest由`PENDING_HUMAN_READ`更新为已实际Read，PixelCopy/设备图仍NOT_RUN；它们是相同真实链不同滚动阶段的图，不是全V。ref中未找到可逐像素匹配的此精确提现参考，记**no-exact-reference**；不把图张数代替其余交易/资产/安全验收。

Keep U：地址簿单条白名单/DELETE/总开关（create和label/memo edit已由后续批次完成），pending提现撤销，站内转账，completed/processing/failed/canceled实体Root，非2FA发码/spot配置链和真机/真实资金。此批次未部署/未发真实验证码/未调用非loopback资金写。超过原10–15分钟预算，主要诊断是AppRoot的提现方式+币种两步选择、route队列队列次序及mock敏感写的前置拒绝；这些过程性失败XML未冒充passed，正式目录只保存最终4/0/0/0独立结果。

## 2026-11-06 1250 bounded address-create batch

OpenAPI支持本批create和仍未验的PATCH/del三种实际写，不能把接口存在混合报成完成。本批只收**create**：`POST /account/withdraw-addresses`带USDT/TRC20/无效演示地址/Mock local label/false/fake TOTP，local；GET /account/withdraw-addresses`真实列表必须同id才能发布`AddressReceipt`，随后才回到真实列表再GET展示label+address。容许写入只此精确127.0.0.1 method/path/body；禁止发码、PATCH、DELETE、白名单开关及其他资金/提醒写。

原生产缺漏：原保存直接POST并Toast即回跳，没有独立复核、无强制列表读回；本地saving不是ViewModel/session跨界面防重。本批家庭VM在同步入口拒绝busy/accepted重复，按session generation cancel/隔离late result，accepted但列表不全不能repost。Root两适配（320/fontScale1.3、360/dark）通过Assets入口→提现方式→地址簿→新增→真实复核Window，取消0 POST、held503单次在途、错误保留再确认→201→GET列表可见记录。新增VM两项真实Retrofit分别验held重复/旧session和success同id readback/不重复create。

发现并修正复核内容在现有纵向Sheet里再套verticalScroll造成真独立Window 0x0/黑图：复用现有容器滚动，Root断言Window非零，并保存新的可Read窗口图片。期间早期匹配到表单自身label的假列表结果未沿用；正式断言同一生产Root已回AddressBook、真实list label/address、GET≥3且无REJECTED。无精确新增UI reference，明确no-exact-reference；8 PNG/manifest已Read，不是整本地址簿、编辑删除、设备PixelCopy或全V。

独立XML `address-create-xml`；图片/元数据 `address-create-evidence`。独立Root **4 tests / 0 failures / 0 errors / 0 skipped**；VM 2项0 skipped。默认实测 **358 tests / 0 failures / 0 errors / 130 skipped**（本批Root 4项默认Assume skipped）；顺序Debug compile→Release compile→lintDebug→默认全测全部BUILD SUCCESSFUL。默认XML `address-default-xml`。保持U：pending提现撤销、提现其他状态、站内、地址白名单/PATCH/DELETE与非2FA/真机。本批超出预算，真实诊断包括资产币种资料、同font输入tag、嵌套滚动窗口归零及网络回调整合，不把中间错误XML当passed。

## 2026-11-06 address label/memo PATCH limited batch

OpenAPI `PATCH /account/withdraw-addresses/{id}` 同样支持 `whitelisted`，但本批只允许 label、memo、totpCode 的精确 body；没有用同一Endpoint顺带修改名单。真实生产 `AppRoot` 通过资产→提现方式→地址簿→地址卡编辑进入表单与独立最终复核Window。编辑写入由家庭 `AssetsViewModel` 同步拒重，维护session transaction/generation；旧会话取消且不能发布receipt。取消不写；held503保留label、memo、fake TOTP和错误可重试。PATCH 200后必须重新GET列表，并要求同id且label/memo都等于最终输入才发布已读回receipt，随后展示该GET得到的列表；readback失败或值不匹配保持PATCH accepted且禁止再次POST/PATCH，不宣称修改完成。

**最终独立结果先保存：** `/workspace/android-verification/address-edit-xml` = 320dp/fontScale1.3/light及360dp/dark各取消零写和held503/重试/同id新值列表readback，合计 **4 tests / 0 failures / 0 errors / 0 skipped**。VM隔离XML另为 **4 / 0 / 0 / 0**：在途防重与旧session隔离、200后值不匹配阻止repost。`address-edit-evidence/` 存8张最终PNG、对应manifest和校验sha256。这些结果只涵盖本批，不把早期getter定义、缺按钮、错误fixture路径与窗口诊断try过算passed。

8张最终图已实际Read：表单与独立确认窗口能读清原/新label、memo、地址及验证输入；503保留已填数据与可重试按钮；结果窗口已离开编辑回到真实列表，显示新值和“已从服务端列表读回修改”。320大字的编辑按钮与滚动表单位置可读，360暗色文本/错误可见；真实Window manifest非零960×2532px/1080×2532px，非inline替代、非instrumentation PixelCopy。此精确编辑界面未定位精确参考：**no-exact-reference**，不宣称reference-identical或全V。真机/其它宽度/IME等仍U。

**准确未验边界：** 单条 `whitelisted` PATCH、地址 `DELETE`、全局 `/account/withdraw-whitelist` 写及读，pending提现撤销，站内提现、资金/提醒/发码等不在本批。全部仅 `127.0.0.1` 精确route/body的fake token/TOTP；没有远端写、部署、真实发送验证码、iOS或我方真机APK前置。

顺序Debug compile→Release compile→lintDebug→默认全测全部BUILD SUCCESSFUL，见 `address-edit-build.jsonl` 及 `/workspace/edit-{debug-compile,release-compile,lint,default}.log`；默认XML另存 `address-edit-default-xml`。默认实测 **364 tests / 0 failures / 0 errors / 134 skipped**。默认中4条Root Assume skip、`AssetsAddressFlowTest` 4条0 skip；skip不计Root通过，不沿用create旧358数字。

## 2026-11-06 bounded pending withdrawal-cancel batch

契约只使用实际存在的 `POST /wallet/withdraw/{id}/cancel`，id单段、无请求body；仅 `pending` 可申请撤销，200返回Withdrawal，409=该提现无法撤销（不存在或已进入处理）。没有发明2FA/短信/DELETE或其它接口。

真实入口为 AppRoot HOME→资产→提现方式→提现记录→pending记录撤销申请→原有独立最终复核Window。转换生命周期scope为家庭AssetsViewModel中无request-body的精确取消事务：同步busy防连点，同会话generation隔离旧读写；processing前置0写；503保留Window目标可重试。200后必须再GET overview确定原debit account，读取该余额和提现列表，并核实列表同id为canceled及资产余额存在才发布receipt，原账户余额显示来自真实GET（不是本地金额加法）。accepted后即使读回失败/值不对也禁重复POST；409仅通过进一步记录GET显示实际processing等当前状态，不声称撤销。本批不是链上/站内/其它状态结算。

独立Root `/workspace/android-verification/withdraw-cancel-xml`：320dp fontScale1.3 light、360dp dark各cancel0写+held503防重/保留重试/同id canceled记录/原账户余额readback，共 **4 / 0 / 0 / 0**。`withdraw-cancel-vm-xml`的 `AssetsWithdrawFlowTest` 合计原submit+新cancel **4 / 0 / 0 / 0**：非pending防重/held old-session隔离、accepted readback error禁止重发、409实际processing状态。独立XML先存，后默认套suite；enabled不是Root skip算pass。

`withdraw-cancel-evidence`存两适配record、独立true Window review、503 retained retry、result共8张图及metadata。实际Read初始pending、取消手段、503和最终同id已撤销/账户/100.00 USDT且不再pending；320无截断矛盾卡，360颜色可读。最终记录同id server readback映射到卡内，撤写不拔除旧resume UI。没有精确对应参考：**no-exact-reference**，不是reference-identical、完整V或真机PixelCopy。全部仅127.0.0.1精确id/空body route，fake token，无真实资金/安全/提醒写，无发码/远端；没有iOS或真机APK前置。

顺序compileDebug→compileRelease→lintDebug→default tests结果存 `withdraw-cancel-build.json`/日志与 `withdraw-cancel-default-xml`。默认Root四条为Assume skip，不算独立passed。边界U：地址删除、单条whitelisted PATCH、全局开关、反向`out`划转、processing/completed实际结算、非2FA发码和其它宽度/真机。本批有真实fixture队列诊断/最终图指出的stale pending卡而超出10–15分钟，均不以调试中图或失败XML冒充交付。

最终同源码顺序门：Debug compile、Release compile、lintDebug、default tests 全部BUILD SUCCESSFUL；默认 **371 tests / 0 failures / 0 errors / 138 skipped**。其中本批Root四条default Assume skip，VM含原submit共4条默认0 skip。日志 `/workspace/cancel-{debug-compile,release-compile,lint,default}.log`；实际通过只认先存的enabled XML。

## 有界批次：反向 out 合约→现货（独立Root/Window证据）

继续既有`TransferScreen→transferFutures(false, amount)→POST /futures/transfer`，不造另一个划转API。真实ROOT由首页→资产→划转→点击切向，不变更方向初始in。确认前/在途方向锁定、VM session+generation隔离late response；200只置accepted，再分别读`account=spot`和`/futures/account`，不将funding当spot。503仍可独立复核重试；readback失败仅会GET，新增`refreshAcceptedTransferBalances()`的只读防重入口，不允许重发同向或反向POST。

独立Root两条×两适配：取消零写、503 gate保留单次POST/按钮禁用→completed/ref→spot 100→110/futures20→10；精确允许body只有既有in及`{"direction":"out","amount":"10.00"}`和127.0.0.1登记的包。Mock登记200入队移除期间的`response=null`不能提前放行：必须是该精确method/path/body已登记，并仍有该route有效返回，任意新route/body仍拒绝；没有远端资金/安全/提醒写。fixture中funding 999只用于明确隔离。

**结果以最终XML目录为准：** Root `transfer-out-xml` **4 tests / 0 failures / 0 errors / 0 skipped**；VM `transfer-out-vm-xml`三项0跳（既有in会话隔离、out accepted仅重读不再POST及反向拒、out held/session late）；默认另行核实Root至少四项Assume skip。Root图8张实际Read：320浅色font1.3/360暗色的表单、独立复核、503保留、成功两账户读回。明确**no-exact-reference**，不是整个App或全V通过，也不是instrumentationdevice截图。

默认 XML `transfer-out-default-xml` 为 **377 tests / 0 failures / 0 errors / 142 skipped**（新增4 Root按Assume跳过，其他skip按原覆盖范围）。最终顺序门日志 `transfer-out-{debug,release,lint,default}.log`，enabled日志 `transfer-out-enabled.log`；每项Read为非精确参考的可读性检查，非V总分。

**本有限组剩余U**：地址DELETE、单条whitelisted PATCH、总withdraw-whitelist开关。这只是原地址组继续收口项，不是整个Android剩余清单；站内提现、地址其他写外的交易/安全/全页横切、其它宽度、真机PixelCopy等仍照原总表U。iOS未被扩展或等待，我方真机APK没有新增前置。顺序Debug compile→Release→lint→默认全测，不用根测试跳过冒充通过。
