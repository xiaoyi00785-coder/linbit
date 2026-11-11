# 消息中心（account）

站内通知，带邮件通道。用户接口都要 bearer，经 gateway（`/notifications` → account）。

类型：`system` | `trade` | `activity` | `security`（对应 UI 页签）。

## 用户

| 方法 | 路径 | 说明 |
|---|---|---|
| GET | `/notifications?kind=&page=&pageSize=` | `{items:[{id,kind,title,body,time(ms),read,emailStatus}], total, page, pageSize, unread, unreadByKind}`；新的在前，pageSize ≤ 100（默认 20） |
| GET | `/notifications/unread-count` | `{unread, unreadByKind}` |
| POST | `/notifications/read` | `{ids:["12","13"]}` 或 `{all:true, kind?}`（全部已读）；返回 `{updated}`；只能改调用者自己的通知 |
| GET | `/notifications/settings` | `{emailSystem, emailTrade, emailActivity, emailSecurity}` |
| PUT | `/notifications/settings` | 部分更新；`emailSecurity:false` → 400 `SECURITY_EMAIL_REQUIRED` |

默认：system、trade 邮件开，activity 关，security 始终开。
`emailStatus`：`none`（仅站内）· `pending` · `sent` · `failed` · `no_address`（无邮箱，仅手机号账户）。

## 生产方（内部，`X-Internal-Token`）

- `POST /internal/notifications {uid, kind, title, body, email?}`：`email` 为 true/false 时强制该通道；省略则按用户偏好。Go 服务调 `common.Notify(common.Notice{...})`（异步、尽力而为，需要 `ACCOUNT_INTERNAL_URL` + `INTERNAL_TOKEN`）。
- `POST /internal/notifications/broadcast {kind, title, body}`：仅站内，每个活跃用户。

内置钩子：新设备登录、重置密码（security，account）；充值入账、提现完成、提现拒绝（trade，wallet）；KYC 通过与驳回（security，kyc）。

## 后台

- `POST /admin/v1/notifications/broadcast {kind,title,body,totpCode}`
- `POST /admin/v1/users/{uid}/notifications {kind,title,body,email?,totpCode}`

都要 `content:edit` 和新的 2FA 码，并写审计（module `notification`）。

邮件走 account 的 EmailSender（配了 Resend 用 Resend，否则开发日志 mock）。
