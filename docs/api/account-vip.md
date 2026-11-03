# VIP 申请与审核

VIP 仅为身份标识，无手续费档位。

## 用户端（经 gateway）
- `GET /vip/application` → `{uid, vip, status: none|pending|approved|rejected, application, conditions, channels}`
- `POST /vip/applications` `{channel: telegram|whatsapp|wechat|email|phone, contact}`
  - 409 `APPLICATION_PENDING`：已有待审申请；409 `ALREADY_VIP`：已是 VIP。被驳回后可重新申请。

## 管理端
- `GET /admin/v1/vip/applications?status=` 权限 `vip:view`
- `POST /admin/v1/vip/applications/{id}/approve` `{totpCode}` 权限 `vip:review`
- `POST /admin/v1/vip/applications/{id}/reject` `{totpCode, reason}`（reason 必填）
  - 重复审核返回 `NOT_PENDING`；全部决定写入审计（module=vip），并给用户发送系统通知。

## 内部
- `GET /internal/vip/applications`、`POST /internal/vip/applications/{id}/review` `{decision, reason, reviewer}`（INTERNAL_TOKEN）
