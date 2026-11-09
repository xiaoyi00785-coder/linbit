# 自有客服详情：前台刷新与响应隔离

## 本批边界与行为

- 自有 GET `/support/tickets/{id}`，RESUMED 时立即读，单次完成后间隔 10 秒再读；同一详情重入/重试共享在途请求，不并发堆积。
- PAUSE/STOP/DESTROY、页面 dispose、切换工单、sessionVersion 变化均取消详情请求和计时器；旧工单/旧会话/旧轮询租约响应均不得落状态。恢复前台重新启动一条刷新链。
- 发送/关闭/创建仍只由用户点击触发。本次没有真实发送、创建、关闭工单或消息；状态测试中 fake reply 为纯内存方法调用，没有 HTTP POST。
- 写操作开始取消旧读取并提升 mutationVersion，避免旧 GET 覆盖写结果；写响应也校验会话；已切换工单的旧回复/关闭结果不覆盖新工单，旧回复回调仅清理它原先的草稿。
- 后台刷新不替换页面为 loading；失败保留已有历史并显示刷新警告，下次成功消除。相同响应不会发出新 StateFlow 值。
- 草稿文本、待上传附件和 ScrollState 不随详情响应重建；不自动滚到底部。保证组件存活及前后台切换的保留，不承诺离开销毁页面后或进程死亡的持久化。
- GET 详情在服务端会更新工单 user_read_at，因此只在前台启动。取消 HTTP 不能回滚已经到达服务器的已读操作；未做真实账号验证。
- 系统通知仍没有正式 ticketId/deepLink 契约，不从通知正文猜 ID；工单已读与系统通知已读是两套状态，未伪造联动。

## 可执行验证与图

- `SupportRefreshTest`：在途去重/无变化、取消、非协作旧响应、会话切换、失败保留后恢复、旧 GET 与纯内存 reply 竞态、切换工单后旧写响应不得重放 A。
- `SupportLifecycleEvidenceTest`：生产详情组件 + 自定义 LifecycleOwner；STARTED 不请求、RESUMED 请求/刷新、PAUSE 停止、恢复仅一条、dispose 停止、前台 token 版本变化后恢复一条；文本输入、内存 PNG 附件及非零滚动在相同/新增回复刷新后保留；verify 不调用创建/回复/关闭接口。
- 阶段图 `/workspace/linbit-verify/batch4/support-detail-refresh-stage.png`；原图 `mock-support-detail-refresh.png`。Robolectric 离屏生产组件 + Mockito 纯内存工单/消息/图片；非设备截图、非真实 API 数据。无对应客服详情同状态参考，故只给我们的阶段图，不借用系统通知/群聊参考，不声称一比一还原。
- 真设备验证仍受 /dev/kvm 访问权限阻塞；未提权、未 chmod、未 wipe、未退出已有账号。

## 冻结后端核查与下一批（仅代码/契约，不是生产上线声明）

冻结点 `be357384719872b1e8b765c69de95fd1b1c774e8` / PR111 未合并、未部署；本批不 merge 服务器分支。

| 项目 | Android 代码现状 | 结论/待办 |
|---|---|---|
| newcomer.claimable | 生成 `ApiHomeNewcomer.claimable: String?` | 正式字符串计数 `"0"/"1"`，completed integer；保持，宽松 number 仅旧兼容 |
| bonus.available | 生成 `ApiFuturesBonus200Response.available: String?` | 与正式 `{trial,deduction}` 十进制字符串对象不符。下一批同步双 spec、生成模型与福利消费方；不是后端缺字段 |
| K线周期 | `feature/trade/CandleChart.kt ALL_INTERVALS` 8 项 | 下一批加契约支持 3m/2h/6h/12h；完整 12 项 1m/3m/5m/15m/30m/1h/2h/4h/6h/12h/1d/1w。无 1M，无事件 10m，不宣称旧生产已支持 |
| 公告 | LinbitApi 无 announcements GET，首页仍未接公告 | 冻结文档已有 `/announcements` 与详情；属于前端下一批接入，不得再说后端没有公告。独立快讯没有正式契约 |
| REST/WS USD | 双 spec 共用 WsUsd，REST Ticker.usd 与 WS typealias 共用 ApiWsUsd（available/price/rate/stale） | 基本价格共用已实现；source/updatedAt/precision/rounding/convention/pegAssumed/reason 等元信息未完整建模，下一批同步正式 schema 后生成，不手改生成物 |
| 工单通知定位 | 通知生成模型无 ticketId/deepLink | 后端需正式结构化字段、通知类型及已读联动约定；未猜字段 |
| 充值 | 不开放 futures 直充 | 冻结候选仅 funding/spot，合约直充 409 |

运维提供的内网现状：`/market/usd-rate` 404、asset deposit-account 404，6 个迁移未上；公网 403/error1010 是 WAF，不能当接口 404，也不绕过。冻结契约适配与生产部署验证严格分开。本批独立客服代码工作不扩大到交易、账户安全或后端权限。

## 最终本地验证

JDK17、Android SDK，Gradle `--max-workers=2 -Dorg.gradle.jvmargs=-Xmx2g`：

- `:app:assembleDebug :app:testDebugUnitTest :app:lintDebug` 最终复验成功。
- JUnit XML：460 项，280 通过、180 条件跳过、0 失败、0 错误。本批客服状态 7 项 + 生命周期组件 1 项全部执行通过；跳过项不算通过。
- lint：0 error/fatal，42 warning、32 information；成功不等于零告警。
- 完整日志 `/workspace/linbit-verify/batch4/support-refresh-validation-final.log`，计数 `support-refresh-test-summary.json`；报告目录 `mobile/android/app/build/reports/tests/testDebugUnitTest/`、`mobile/android/app/build/reports/lint-results-debug.html`。
- 先前专项测试发现 runTest 结束时无限排空未取消轮询，已显式清理 VM scope 并复验；终止的开发轮次不计成功。
- 上批 1612e21 的三条远端检查本次查询仍为 in_progress，无已可得失败日志；不声称远端全绿。当前提交推送后的 head/checks 以阶段交付报告为准，不轮询等待。
