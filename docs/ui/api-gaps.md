# UI P0 联调说明：已对接接口与缺口

适配层：`apps/web/lib/services.ts`（已有接口先调用 `@linbit/api-client`，失败回退 mock；缺口接口只走 mock）。页面上的 `API` / `Mock` 小标签表示当前数据来源。

## 已对接（openapi.yaml 已定义）
| 页面 | 接口 |
|---|---|
| /register | POST /auth/register |
| /login（含 2FA 步骤） | POST /auth/login（totpCode） |
| /withdraw | POST /wallet/withdraw（memo、totpCode）、GET /wallet/withdraw/records、GET /assets/balances |
| /setting/kyc/basic | POST /kyc |
| 行情 / 现货 / 资产（已有） | /market/*、/spot/orders/open、/assets/balances、WebSocket ticker/depth/kline（docs/api/websocket.md） |

## 缺口（需后端补充，当前为 mock）
| 用途 | 建议接口 |
|---|---|
| 注册 / 找回密码 / 提现验证码 | POST /auth/verification-code `{target, scene}`；RegisterRequest 增加 `code` |
| 找回密码 | POST /auth/password/reset |
| 登录是否需要 2FA | /auth/login 返回 `2FA_REQUIRED` 错误码或 `mfaRequired` 字段 |
| 提现最小额 / 手续费 / 上限 / memo 必填 | GET /wallet/withdraw/config?asset=&network=（mock：USDT 最小 12、最大 20,000、手续费 1） |
| KYC 证件上传 | POST /kyc/documents（multipart → fileId）；KycSubmission 增加 fileId 字段 |
| 卡券中心 | GET /coupons、POST /coupons/{id}/activate |
| 消息通知 | GET /notifications、POST /notifications/read |
| 公告 | GET /announcements |
| 设备管理 | GET /account/devices、DELETE /account/devices/{id} |
| 提现地址白名单 | GET/POST/DELETE /wallet/withdraw/addresses、白名单开关 |
| 合伙人中心（apps/partner） | /partner/rules、/partner/summary、/partner/commissions、/partner/team、/partner/apply |
| 跟单分润 | 交易员分润比例（0–50%）设置与每日结算记录 |
