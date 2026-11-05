# Linbit Android

原生 Android 客户端（Kotlin + Jetpack Compose）。与 `mobile/ios` 对齐：同一套设计令牌、同一份 OpenAPI、同一个应用 ID（`click.cowa.linbit`）。

## 技术栈

| 层 | 选型 |
| --- | --- |
| UI | Jetpack Compose + Material 3（自绘组件为主，样式来自 `mobile/tokens`），浅色 / 深色 |
| 架构 | 单 Activity + Compose；Hilt 依赖注入；ViewModel + StateFlow；协程 |
| 网络 | Retrofit + OkHttp + kotlinx.serialization；`AuthInterceptor`（公共头 + Bearer）、`TokenAuthenticator`（401 刷新并重放）、统一错误信封 `ApiFailure` |
| 实时行情 | OkHttp WebSocket：单连接多订阅、指数退避重连、心跳 + 看门狗（`core/realtime/MarketSocket`） |
| 存储 | EncryptedSharedPreferences（令牌 / 设备 ID）、DataStore（主题等非敏感偏好） |
| 数字 | **全部 `BigDecimal`**，绝不使用 Double；服务端 8 位小数字符串（手续费、已实现盈亏、资金费、账单）原样显示、保留末尾 0（`Fmt.exact`） |
| 模型 | 由 `mobile/openapi.yaml` 生成（`mobile/tools/gen_models_android.sh`，openapi-generator + kotlinx_serialization，生成目录 `core/generated`，勿手改） |
| minSdk / target | 26 / 35 |

## 目录

```
mobile/android
├── app/src/main/kotlin/click/cowa/linbit
│   ├── app/            Application、MainActivity、AppRoot（Tab + 全屏覆盖层 + 启动动画）
│   ├── core/           network（API 客户端/拦截器/错误）、realtime（WS）、auth、storage、util（Dec/Fmt）、generated（OpenAPI 模型）
│   ├── data/           MarketStore（行情仓库，REST + WS）、Watchlist（自选）
│   ├── di/             Hilt 模块
│   ├── feature/        home、markets、auth（登录/注册/找回密码）、user、placeholder
│   └── ui/             theme（Tokens.kt 由 tokens.json 生成）、components（Tab 栏、按钮、输入框、Toast…）、splash
├── brand/              最终版 Linbit Logo（SVG，单一来源，见 brand/README.md）
├── icons/              Android 专用补充图标（mobile/icons 里没有的，如 Apple/Google/通行密钥）
└── app/src/main/res/   drawable 下的 VectorDrawable 由 mobile/tools/gen_android_vectors.py 生成（仓库内无 PNG）
```

## 构建

```bash
# 需要 JDK 17 与 Android SDK（ANDROID_HOME），platform 35 / build-tools 35.0.0
cd mobile/android
./gradlew assembleDebug testDebugUnitTest lint   # gradle-wrapper.jar 已入库（Gradle 8.9，distributionSha256Sum 已固定）
```

CI（`.github/workflows/android.yml`）先用 `gradle/actions/wrapper-validation` 校验 wrapper，再用 `./gradlew assembleDebug testDebugUnitTest lint` 构建。

### 重新生成资源

```bash
python3 mobile/tools/gen_android_tokens.py     # mobile/tokens/tokens.json → ui/theme/Tokens.kt
python3 mobile/tools/gen_android_vectors.py    # mobile/icons + mobile/android/{icons,brand} → res/drawable、启动图标、启动页图标
mobile/tools/gen_models_android.sh             # mobile/openapi.yaml → core/generated（需要 openapi-generator-cli）
```

CI 会重新运行前两个生成器并检查工作区无差异，防止生成物与源文件漂移。

## 当前已实现（里程碑 1）

- 主题（浅/深）、共享组件、悬浮胶囊 Tab 栏（首页 / 行情 / 合约 / 现货 / 资产）
- 启动动画：Android 12+ 系统 SplashScreen（品牌绿底 + Logo）→ Compose `SplashOverlay` 接力（回弹 → 光环 → 淡出，1.2s，对应 iOS SplashView）
- 自适应启动图标（#abe127 底 + Logo 前景 + 主题图标 monochrome 层）
- 登录 / 注册 / 找回密码全流程：账户输入（邮箱域名联想、国家区号）、密码、验证码（登录二次验证 TOTP/短信/邮件）、设置密码、邀请码、挽留弹窗
- 首页：总资产、新手任务、红包横幅、功能宫格、卡片轮播、理财宝、榜单（自选/热门/涨幅/跌幅/成交额 × 现货/合约）
- 行情：自选 / 市场 / 聊天室 Tab，现货与合约列表（REST + WS 实时），分类筛选与排序
- 现货交易：交易对头与选择、盘口（深度 WS + 轮询兜底、精度聚合、默认/买/卖模式）、限价 / 市价 / 计划委托 / 只做 Maker、止盈止损、百分比滑杆、确认弹层、当前委托 / 条件单 / 撤单 / 全部撤销
- 合约交易：合约 / 事件合约 / TradFi / 模拟交易 顶部 Tab；全仓 / 逐仓 + 合仓 / 分仓、杠杆调整、开仓（成本 / 数量口径）/ 平仓、计划委托、止盈止损、预估强平价、资金费率与倒计时、仓位卡片（WS 标记价实时盈亏）、普通 / 止盈止损 / 计划委托列表、今日盈亏、卡券
- 单元测试：`Fmt`/`Dec`（小数格式化）、`WsMessageParser`（WS 消息解析）、`AccountId`/`PasswordRule`

## 路线图（按优先级，对应 `mobile/ios/Linbit/Features`）

1. **交易补全**：K 线（TradingView 风格）、历史委托 / 成交 / 历史仓位 / 平台盈亏、改单、仓位止盈止损与保证金调整、价格预警、计算器、策略 / 理财 / 闪兑子 Tab（强平价等 8 位小数字段原样显示）
3. **资产**：总览、资金 / 现货 / 合约账户、划转、账单流水（8 位小数）
4. **充值 / 提现**：网络选择、地址簿、提现（2FA / 风控 24h 限制）、历史记录
5. **安全中心**：谷歌验证器 2FA 绑定、通行密钥、修改密码、设备管理、防钓鱼码
6. **KYC 实名认证**：L1 / L2 流程（相机、证件上传）
7. **个人中心**：完整页面、账户切换（多账号）、偏好设置（语言 / 计价币种 / 涨跌颜色）
8. **理财**：产品列表、申购 / 赎回、收益记录
9. **通知与客服**：站内信、FCM 推送、在线客服 / 工单
10. **社交登录**：Apple / Google（`CredentialManager`）、通行密钥登录
11. **其余**：闪兑、子账户、跟单、策略交易（网格）、TradFi、聊天室、福利中心 / 红包、搜索、扫码

## 需要产品 / 运维确认

- Google Play 开发者账号、发布签名密钥（`*.jks` 永不入库，走 CI Secret）
- Firebase 项目（FCM 推送、可选 Crashlytics）
- Google / Apple 登录的 OAuth Client ID
