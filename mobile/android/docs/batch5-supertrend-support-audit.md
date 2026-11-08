# 本批：Supertrend + USD 生成契约 + 自有客服核查

## 范围与授权

仅在助手电脑现有 `/workspace/linbit-android` 本地开发，无云端代理。按用户本轮明确授权推送现有工作分支；不合并 main、不强推、不部署。Google 登录、法币购买、卡片外部配置延后。

## 推送第一阶段

已核验远端原 head 为 `3250aff19b48277b7116a791cf69e46ec0233a9e`，它是本地 HEAD 的祖先。仅有两项未推送提交 `c0e98aa`、`87353cf`，已正常快进推送 `feat/android-easicoin-restore`（PR #110），并用 ls-remote 确认远端为 `87353cf3dac6c19c8d2ef05ef90407d9cd3fd2c2`。没有混入其他工作流提交。

## Supertrend 实做

- 基于 `/market/klines` 的真实 OHLC，客户端计算 Wilder ATR + Supertrend 上下轨与方向；默认 ATR 10 / 倍数 3。
- 两个可操作入口：图表底部 Supertrend 标签、指标设置弹层；多/空分别使用涨/跌色，中间断开，不跨方向空段连线。
- 用完整返回历史计算，最后只显示 60 根，不在截断后重新初始化趋势；不足预热期显示 `--`，上游畸形数据不产生可用指标。
- 切币或切周期清空旧图，不把旧币种 K 线显示成新币种；按已保存参考把绘图区由 150dp 调为 220dp。
- 已保存参考只能证明 Supertrend 标签存在，尚未采到参考的激活态/具体参数。本实现不能声称参考参数一致或像素级一致。
- 画线工具仍是下一批真实缺口，本批不冒充完成。

## USD 契约防生成覆盖

在 `docs/api/openapi.yaml` 与 `mobile/openapi.yaml` 同步新增共用 `WsUsd` schema 与可选 `Ticker.usd`，使用项目指定 OpenAPI Generator 7.10.0 重新生成 `ApiTicker`、`ApiWsUsd`，不是只手工补模型字段。WS 的 `WsUsd` 改为共用生成类型别名；旧 ticker 无 usd 仍可解码，不假定 USDT 恒等 USD。生成脚本同步规范尾部空白；REST/WS 同载荷一致性有测试。

## CI 真实根因

旧 head `3250aff` 的两项失败日志（job 128844586282、128844569472）均定位为 Android lint：`CoinIcons.normalize(): String` 错误标记 `@DrawableRes`，触发 SupportAnnotationUsage。不是后端、WAF 或本轮聊天室权限引起，也不能笼统称全绿。本批去掉该函数的错误注解，不关闭 lint、不跳过失败规则。旧 head 的成功项与失败项属于旧流水线；新提交远端流水线状态另行报告。

## 自有客服链路核查（代码级，不是生产发送验证）

- 首页客服/帮助中心 → `MorePage.SUPPORT` → `SupportTicketsScreen`；登录后工单页，无第三方供应商前置依赖。游客未获工单身份，仍要求登录。
- 用户：GET/POST `/support/tickets`、GET `/support/tickets/{id}`、POST `/support/tickets/{id}/messages`；附图有上传与私有附件读取链路。前端展示 user/support 角色、客服姓名、时间、历史正文/附件与关闭状态。
- 管理员：`POST /admin/v1/support/tickets/{id}/messages` 经 account 内部服务记录 support 消息、状态 replied、last_support_at；代码生成一条“客服已回复您的工单”系统站内通知。
- 工单列表读取 `ticket.unread` 显示红点；GET 详情在 account 服务更新 user_read_at，返回列表会 reload。独立消息中心读取系统通知、未读数与已读接口，通知点击能展开并标记已读。
- 真实差异：详情仅首次进入读取，没有前台自动轮询/客服 WS；用户停留详情页不会自动接收新回复，重进可重新 GET。系统通知没有结构化 ticketId/deepLink，通知点击不会直接定位工单；工单已读与对应系统通知已读是两套状态，不能宣称自动联动。
- 后端同事报告已在隔离测试验证“管理员回复→工单未读→详情已读”，此为后端报告，不当作本代理实际生产端到端验证。后续应优先补客户端回复刷新和通知定位；如需结构化 ticketId/deepLink，请后端提供字段，不在客户端从中文正文猜 ID。
- 本轮没有真实创建/回复/关闭工单或发送聊天消息，也没有修改未读状态。

## 联调与设备限制

- 运维本轮反馈生产测试为 WAF 403/error1010；不绕过，不再把历史404当作当前事实。
- 后端新候选 `3391af945e2504e0c27d442abe202735d544b4b5` / PR #111 未冻结未部署，未合并。后续按该候选适配 bonus.available 的 trial/deduction 字符串对象、新 K 线周期、公告等；newcomer.claimable 类型仍待后端复核，不能定版。
- 默认充值仅 funding/spot，合约直充不开放；事件仍无独立上下 payout/10m，不能假装已支持。
- KVM 只读诊断：`/dev/kvm` 为 root:pulse-access 0660，当前 box 用户不在该组且无读写权限。既有 AVD 的正常启动已报告硬件加速权限失败；未提权、未 chmod666、未 wipe，也未退出任何账号。真实设备验证尚未完成，需要宿主给当前助手进程正常受支持的 KVM 访问能力。

## 本地最终验证与图片

assembleDebug、testDebugUnitTest、lintDebug 均通过。单测报告 445 项，265 通过、180 条件跳过、0 失败、0 错误。Supertrend 数学测试7项、组件视觉测试1项、USD契约测试3项均执行通过。

- `/workspace/linbit-verify/batch4/compare-event-supertrend.png`：左旧参考 MA 激活图表裁剪，右本次生产组件离屏 Supertrend 激活，明确非同状态、本地合成 OHLC。
- `/workspace/linbit-verify/batch4/mock-event-supertrend.png`：右图完整原图。
- `/workspace/linbit-verify/batch4/supertrend-final-validation.log`：构建/测试/lint 日志。

本批止于一项可验证前端缺口与必要CI/契约修复，画线工具、客服自动刷新/通知定位以及新后端候选字段适配留待下一批；不把这些标为完成。
