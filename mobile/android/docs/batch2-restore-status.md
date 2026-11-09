# Android EasiCoin 还原：第二批实况

## 结论

这是第二批增量实现，不是「100% 已还原」验收。只在 `/workspace/linbit-android` 本机编写和构建，没有启动云端代理，没有改 `/workspace/linbit`，没有向参考 App 提交订单、划转、提现、领奖、安全变更或退出登录。

运行时代码（排除自动生成的 OpenAPI 注释）中的「即将 / 开发中 / 敬请 / 后续批次 / 上线后 / ComingSoon / PlaceholderScreen」扫描无命中。**去掉占位文案不等于缺失的服务已经实现**，下面列明服务缺口。

## 已落实的功能

- 扫一扫：运行时相机权限、真实相机预览、离开页面/进入后台释放相机、相册选择和二维码解码、手电筒开关、结果确认/复制。不自动打开二维码 URL，也不自动发起任何资金动作。
- Android 登录/注册：参考 Android 没有 Apple 按钮，已移除；通行密钥保留真实原流程。Google 和游客客服不再伪装成将来可用：分别明确显示未配置 OAuth / 客服需要登录的实际限制。
- 行情：默认美元折合价；账户 `/account/preferences` GET/PUT 与 supportedQuoteCurrencies 对接，选择后统一驱动首页、行情、现货和资产折合价；取不到非美元汇率不伪造数字。TradFi 列宽按权重分配，名称单行省略、价格/偏离列不挤占为零。
- 行情「…」：增加 Widgets；可选择币种，请求系统添加桌面行情快照小组件。标注快照时间，打开 App 时更新，**不宣称后台实时刷新**。搜索页补学院 Tab，但学院内容接口不存在。
- 现货：更多菜单四列布局，交易偏好/充值/划转/价格预警/真实添加自选/本地新手教学；游客私有操作进登录；折合价按美元默认和账户币种偏好显示。
- 合约：开仓高级止盈止损本地草稿弹层，独立选择止盈/止损标记价或最新价，随原有复核后的下单请求传递对应 triggerPriceType；跟单和卡券入口连到已有真实页面，模拟交易改接已有独立 paper 账户 Host；历史按钮保留 48dp 触控区、图形保持 20dp。
- 资产：盈亏连真实 `/futures/pnl/today`，显示已实现、资金费、手续费、强平费和风险准备金补偿；保留错误/重试、会话隔离。快捷买币呈现无可用渠道的明确服务状态，不制造报价、支付商或订单。
- 我的：独立推荐/热门网格主页，再进入用户中心；安全、认证、子账户和账号切换按真实返回栈返回。偏好设置保留真实主题切换、持久化涨跌色和账户计价货币；通知 Tab 连接已有通知偏好 API，而不是静态 FCM 提示。
- 福利中心：正常空态文案，不再显示「敬请期待」。事件合约暂停态明确阻止下单；没有产品配置时禁止下单。删除未被使用的 PlaceholderScreen 和行情 Placeholder。
- 视觉：使用参考 APK 的通用无品牌空态/资产/菜单素材，来源见 `batch2-reference-art.md`；Android 浅色交易配色从参考截图取样。币种图标沿用上一批真实来源，没有生成币种图标。

## 逐页对比与未还原差异

截图目录：`/workspace/linbit-verify/batch2/`。`ours-*.png` 是本地 emulator-5554 真机模拟器截图；`mock-*.png` 是生产 Compose 组件 + 明确的本地 loopback 假数据渲染，不是实际账户，也不是全屏设备截图。`compare-*.png` 仅并列展示，不是像素一致性证明。

| 页面 | 参考来源 | 本批证据 | 尚存差异 / 未验收 |
|---|---|---|---|
| 扫码 | auth/047_scan_error.png（参考只捕获错误态） | ours-scan-permission、ours-camera-live、ours-photo-picker、ours-qr-decoded | 参考成功扫码业务路由未捕获；当前对识别内容只确认/复制，不会自动填资金表单。已用纯测试文本二维码验证相册解码 |
| 现货交易 | guest/022_spot_trade.png，auth/032_spot_trade.png | ours-spot | 登录状态不同；盘口服务确实返回空 bids/asks，不能伪造深度。文字、行距和下单确认仍需逐状态验收 |
| 现货更多 | guest/043_spot_more_actual.png | ours-spot-tools | 已四列，菜单图标用参考资源；新手教学是本地中文内容，不是参考的完整教程站 |
| 合约交易 | auth/034_futures_trade.png | ours-futures | 游客与参考已登录状态不同，杠杆读取各自账户设置；当前 Linbit 盘口为空，涨跌率和参考数据不一致。历史图标显示尺寸已修正 |
| 高级止盈止损 | 本批未成功取得参考弹层 | ours-advanced-tpsl | 草稿与请求字段已接通，未进行真实资金下单，不宣称完整成交链验收 |
| 资产资金 | auth/002_assets_funding.png | mock-funding-assets | 本地假数据，不是实际账户；币种列表、记录入口和非空持仓状态未完整复刻；不宣称完成所有资金流程 |
| 盈亏 / 快捷买币 | 无对应完整参考截图 | mock-pnl、mock-quickbuy-unavailable | 盈亏仅今日合约汇总；快捷买币服务契约缺失，未完成交易能力 |
| 我的 | auth/021_profile.png（含引导遮罩） | mock-profile | 假账户；推荐结构已还原，但热门入口未与参考完整一一对应，邀请/充值返现/交易抽奖/API Key 等未完成；原引导遮罩未实现 |
| 用户中心偏好 | auth/043_preferences.png | mock-preferences | 已中文设置行与真实可用设置；缺参考「主题设置」独立子页、默认充值账户服务设置；仅中文符合本任务文案要求 |
| 行情菜单 / Widgets | batch1/ref_live_markets_more.png | ours-market-more、ours-widgets | 小组件为明确标注的快照，不是参考 Widgets 的所有规格/后台更新模式 |
| 搜索学院 | guest/020_search_keyboard_hidden.png | ours-academy | 有 Tab 和明确服务缺失态，没有文章/搜索内容能力 |
| TradFi | batch1/ours_markets_tradfi.png（旧截图） | ours-tradfi；单一模拟器宽度已检查 | 多种屏宽、长名称、暂停/正常有数据状态未完整验收 |
| 福利/事件合约 | auth/024_rewards.png；事件参考状态缺失 | 本批代码与回归 | 只修本批占位/暂停保护，不宣称福利全部领取链、事件全部交易态与参考一致 |

## 缺少或未验证的后端能力

1. Google OAuth Android 客户端配置、Google ID token 交换/登录注册接口；游客客服/帮助中心的公开服务配置与实时聊天。
2. 法币买币的支付渠道、报价、买币订单和支付状态接口。没有用现货下单或充值假冒快捷买币。
3. 学院文章、分类和搜索接口。
4. 日期范围盈亏曲线、历史资产净值、现货盈亏分析；现有接口只覆盖今日合约汇总。
5. 历史资金费率、历史标记价格、风险准备金余额/历史的接口。对应常用功能明确显示缺口，不生成假数据。
6. 默认充值到账账户的设置契约；当前真实充值仍进资金账户。
7. 参考 Profile 中邀请、返现、抽奖、API Key 等页面及完整 UX 还未完成；通知目前接邮件/站内偏好，不等于 FCM 推送集成完成。
8. Linbit 公开 `/market/depth?symbol=BTCUSDT&limit=20` 和 `/futures/market/depth?symbol=BTCUSDT&limit=20` 本次均 HTTP 200 且 bids/asks 为空，证据见 public-spot-depth.txt / public-futures-depth.txt。

## 验证范围

`assembleDebug` 与默认完整 `testDebugUnitTest` 通过：402 项，0 失败/错误，158 项按既有开关跳过，实际执行 244 项；额外启用的 Batch2VisualEvidenceTest 五项通过。最终日志见 verification-final.log 和 final-mock-evidence.log。默认测试中原有大量截图/证据测试按开关跳过，不能把「无失败」写成全部场景均执行。没有进行真实下单、划转、提现、领奖、改安全或注销；没有 Linbit 登录凭据，因此已登录截图使用明确标注的 loopback 测试数据。

## 代码交付

本地分支仍为 `feat/android-adapt-event2`，没有合并 main、没有 force push。本次没有执行外部推送/PR 更新；需要主代理核对原始用户对本次提交推送的明确授权后再继续。不得从本地有令牌这一事实推导出外发权限。
