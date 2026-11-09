# Batch8：赠金对象 / 12周期 / 公告 / USD完整元信息

## 契约依据与部署边界

只读 `be357384719872b1e8b765c69de95fd1b1c774e8` 的 `docs/api/android-companion.md`、`docs/api/openapi.yaml` 与 `services/go/market-data/usdrate.go`。最新补充 `a6257c29e2c2d73439624e086eb2ca9c53098621` 的同两份文档，取代旧冻结点，仍未合并/部署；没有 merge 后端或修改 Go 代码。

注意：冻结 OpenAPI 仍遗留 bonus.available 单字符串，companion §11 明确旧 spec 错，应按对象读取。依用户/后端确认的正式文档修正本地双 spec，不倒逼后端改回标量。USD JSON 类型由冻结 usdrate.go 核对：时间 RFC3339/null、价格/汇率十进制字符串、precision integer、pegAssumed boolean。

## 实质改动

- 双 spec `docs/api/openapi.yaml`、`mobile/openapi.yaml`：FuturesBonusAvailable 对象、UsdRate schema 与公开 GET、WsUsd 完整字段、12项 KlineInterval。使用既有 OpenAPI Generator 7.10.0 生成相关 DTO；只保留语义相关生成变更，排除381个无关格式差异及无关 social 模型。生成字段在 spec 中，不会下次生成被覆盖。
- 福利中心登录态增加两种可用合约赠金独立展示：trial 与 deduction；BigDecimal 保持任意精度，未知/解析失败为 `--`，服务端零才是 0。不是转出余额，不相加，不制造余额；游客不读私人赠金。
- 图表菜单及事件 K线条统一12项：1m/3m/5m/15m/30m/1h/2h/4h/6h/12h/1d/1w。事件结算 periods 不变，不增加结算10m或1M；事件周期条可横滑，画线/指标按钮固定可达。
- 图表切换先清旧周期数据，取消/旧协程不落结果；服务端未部署新增周期时显示实际错误/空态。修复现货/合约共用图表重试原先同值赋值无效的问题，改成实际重新发起读取。
- 首页已有公告/快讯/学院入口：公告接 GET `/announcements`，保留服务端排序，标题、时间、发丝分隔线；点 numeric id 用 GET `/announcements/{id}` 读取详情。无效/404/读取错误不是空成功，有重试；详情 dispose 取消、旧响应/ID不匹配不显示。全屏详情覆盖保留首页原组件/滚动，不改外部URL或伪造快讯。
- USD REST/WS 共用 ApiWsUsd 完整保留 available/price/rate/source/updatedAt/stale/precision/rounding/convention/pegAssumed/reason；ApiUsdRate 也由生成类型统一。WS明确不可用或缺price不再沿用旧USD价；无有效rate/price显示 --，拒绝 pegAssumed:true，不把非USDT报价按USDT换算。
- `/market/usd-rate` 未部署时只能用 ticker 的真实 rate 与来源元信息兜底；不回退1。折合回退按已下发 precision/half_away_from_zero 先舍入USD，再做显示精度，使用 BigDecimal/HALF_UP，含负数半值远离零。
- 计价货币弹层新增只读美元来源、更新时间（设备本地时区标签）、precision/rounding/unit、缓存/锚定状态；缺值 --，可滚动，不修改真实用户偏好。
- claimable 仍 String 计数，completed integer；没有根据旧生产 number 反改 DTO。

## 图片证据

目录 `/workspace/linbit-verify/batch4/`：

- `batch8-welfare-left-reference-right-stage.png`：左旧 `ref-welfare-1.png`，右本次福利生产页＋内存赠金/空值。非同状态，非真实余额；自有礼盒与参考3D头图仍有历史差异。
- `batch8-announcements-left-reference-right-stage.png`：左旧 `ref-home-login-3.png` 公告区域裁切，右本次生产公告组件、本地测试标题。不是同内容一比一对比。
- `batch8-announcement-detail-stage.png`：单方生产详情；无同状态参考，纯内存测试正文，当前按纯文本安全展示，不声称富文本参考一致。
- `batch8-kline-periods-stage.png`、`batch8-kline-unsupported-stage.png`：单方生产图表/菜单；没有对应周期菜单参考。合成OHLC与本地错误夹具，不是生产数据或错误实测。
- `batch8-usd-rate-info-stage.png`、`batch8-usd-unavailable-stage.png`：单方生产元信息组件，来源 example.invalid 与历史时间均为测试夹具，无同状态参考。
- 原图 `mock-b8-*.png`；本次均 Robolectric 离屏，不是设备截图。ADB/KVM 阻塞不改变权限、wipe或账号。

## 残余与下一批

- 最新 a6257c2 正式通知：可选 link `{type:"support_ticket",ticketId:"12"}`，仅新客服回复；历史/其他无 link 不猜正文，不用外部URL/deepLink。下一批按 ticketId 打开登录工单，再 POST notifications/read 精确该通知id；GET工单不自动标系统通知，跨用户404不可标，不全量已读。此次不实现，不再列为后端缺契约。account迁移17由运维处理。
- 合约资产卡仍未串接独立 bonus 读接口，福利中心本批已呈现；资产卡缺口不能说后端无赠金数据。
- 无独立快讯、偏好周期配置、参考完整工具菜单、参考公告详情/元信息页等证据，不伪造。
- 运维内网 usd-rate/deposit-account 404、迁移未上与公网 WAF403/error1010分开，不据候选声称生产上线，不绕过；不开放合约直充。

## 最终验证与远端

- JDK17、SDK、Gradle `--max-workers=2 -Dorg.gradle.jvmargs=-Xmx2g`。
- `:app:assembleDebug :app:testDebugUnitTest :app:lintDebug` 最终成功；477 项：297 通过、180 条件跳过、0 失败、0 错误。不把跳过计通过。
- 本批新增契约7、公告状态/取消4、生产组件/回环API6，共17项全部执行通过。另覆盖赠金页token版本变化后原页重读，防私人赠金状态被session重置后停在loading。
- lint 0 error/fatal，42 warning、32 information；成功不等于没有告警。
- 双spec目标schema/bonus引用一致及 HomeNewcomer.claimable:string 静态断言通过。源码用生成器生成，模型保留String十进制与RFC3339字符串，不使用Double。
- 最终日志 `/workspace/linbit-verify/batch4/batch8-validation-final.log`；计数 `batch8-test-summary.json`。报告 `mobile/android/app/build/reports/tests/testDebugUnitTest/`、`lint-results-debug.html`。
- 上批0e88546本批查询有2 success、1 in_progress、0 failure，无可得失败日志；当前提交远端head/checks见交付报告，不以本地成功代替远端绿、不轮询等待。
