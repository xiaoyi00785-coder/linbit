# Batch9：系统通知 support_ticket link + 合约资产卡独立赠金

## 通知 → 工单

- 双 spec `Notification.link`：`{type:support_ticket,ticketId:string}`，非 URL。
- 生成 `ApiNotification.link` / `ApiNotificationLink`。
- `supportTicketId` 仅接受 SUPPORT_TICKET + 全数字 id；无 link/未知/非法不猜正文。
- 通知点击：有合法 link → 登录后进工单详情；成功进入后才 `POST /notifications/read {ids:[该通知]}`。
- 404/未登录/切账号/旧响应：不误标；普通打开工单不带 mark id，不自动清系统通知。
- MoreHost 从通知进入 SUPPORT 后返回通知列表。

## 合约资产卡赠金

- `AssetsViewModel` 独立请求 `GET /futures/bonus`，`bonus.available.trial/deduction` 进 `AssetsUiState.bonus`。
- FuturesCard 用 `bonusAmount` 展示；空值 `--`；不计入 `totalUsdt`/权益。
- 切账号清空余额与赠金（首次 session collect 不抹证据态）；页面 `LaunchedEffect(sessionVersion)` 负责重载。

## 测试

- `NotificationSupportLinkTest`、`NotificationViewModelTest` 精确 ids、跨条不误标。
- `AssetsBonusFlowTest` 回环串接/清空/不计余额。
- `Batch9EvidenceTest` 离屏夹具出图（非生产）。
- `CompanionContractTest.notificationLinkIsSupportTicketOnlyAndNotUrl`。
