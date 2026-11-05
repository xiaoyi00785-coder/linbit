# VIP 申请与审核

VIP 仅为身份标识，无手续费档位。

## 用户端（经 gateway）
- `GET /vip/application` → `{uid, vip, status: none|pending|approved|rejected, application, conditions, channels}`
- `POST /vip/applications` `{channel: telegram|whatsapp|wechat|email|phone, contact}`
  - 409 `APPLICATION_PENDING`：已有待审申请；409 `ALREADY_VIP`：已是 VIP（这两项不依赖资产/交易量服务）。被驳回后可重新申请。
  - **提交时服务端硬校验资格**（满足其一）：账户总资产 ≥ 500,000 USDT（资金 + 现货 + 合约钱包按指数价折算，asset `GET /internal/valuation`），或近 30 日交易量 ≥ 50,000,000 USDT（现货 USD 计价交易对 + 合约名义成交额，growth `GET /internal/growth/volume`）。
    - 403 `VIP_NOT_ELIGIBLE`：不满足。中文 `message` 含阈值与用户当前数值，另带 `eligibility {minTotalAssetsUSDT, min30dVolumeUSDT, totalAssetsUSDT, volume30dUSDT}`。
    - 503 `VIP_ELIGIBILITY_UNAVAILABLE`：资产或交易量无法获取（上游不可用 / 有资产无法折算且已知数值未达标）。fail closed，不创建申请，可稍后重试。
    - 阈值可配置：`VIP_MIN_TOTAL_ASSETS_USDT`（默认 500000）、`VIP_MIN_VOLUME_30D_USDT`（默认 50000000）；`GET /vip/application` 的 `conditions` 即实际执行值。`VIP_ELIGIBILITY_CHECK=off` 仅限开发环境，生产启动会被拒绝。
  - 通过校验后仍进入人工审核（1-3 个工作日）。

## 管理端
- `GET /admin/v1/vip/applications?status=` 权限 `vip:view`
- `POST /admin/v1/vip/applications/{id}/approve` `{totpCode}` 权限 `vip:review`
- `POST /admin/v1/vip/applications/{id}/reject` `{totpCode, reason}`（reason 必填）
  - 重复审核返回 `NOT_PENDING`；全部决定写入审计（module=vip），并给用户发送系统通知。

## 内部
- asset：`GET /internal/valuation?userId=` → `{userId, totalValueUsdt, complete}`（`complete=false` 表示有持仓资产没有可用指数价，总额只是下限）
- growth：`GET /internal/growth/volume?uid=&days=30` → `{uid, days, spotUsdt, futuresUsdt, totalUsdt}`
- `GET /internal/vip/applications`、`POST /internal/vip/applications/{id}/review` `{decision, reason, reviewer}`（INTERNAL_TOKEN）
