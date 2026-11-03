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
| 交易费率 | GET /fees（或交易对配置内返回 maker/taker）。现货 Maker/Taker 0.1%（按成交额，从所得币种扣除）；合约 Maker/Taker 0.05%（仓位价值 × 0.05%，开仓、平仓各收一次）；无 VIP 阶梯。前端暂用 apps/web/lib/fees.ts 常量 |
| KYC 证件上传 | POST /kyc/documents（multipart → fileId）；KycSubmission 增加 fileId 字段 |
| 卡券中心 | GET /coupons、POST /coupons/{id}/activate |
| 消息通知 | GET /notifications、POST /notifications/read |
| 公告 | GET /announcements |
| 设备管理 | GET /account/devices、DELETE /account/devices/{id} |
| 提现地址白名单 | GET/POST/DELETE /wallet/withdraw/addresses、白名单开关 |
| 合伙人中心（apps/partner） | /partner/rules、/partner/summary、/partner/commissions、/partner/team、/partner/apply |
| 跟单分润 | 交易员分润比例（0–50%）设置与每日结算记录 |


## 联调状态（2026-11-02，devstack @ e9a2166）

| 接口 | 前端 | 状态 |
|---|---|---|
| POST /spot/orders、DELETE /spot/orders/{orderId}、GET /spot/orders/open、/spot/orders/history、/spot/trades | `features/Spot.tsx` + `lib/services.ts` | ✅ 已联调（下单、撤单、列表），网关不可达时回退 mock |
| GET /wallet/withdraw/networks?asset= → `networks[].gasFeeEstimate / minWithdraw / gasFeeAsset` | `getWithdrawConfig` 字段改名 `gasFeeEstimate`（原 `networkFee`） | ⚠️ openapi 已定义，网关返回 404，前端回退 mock（Arbitrum ~1、BEP20 ~1、TRC20 ~2、ERC20 ~10 USDT）；USDT 最小提现取 max(minWithdraw, 12) |
| GET /wallet/withdraw/records | 提现记录 | ⚠️ 网关 404，回退 mock |
| 交易费率 | `lib/fees.ts` | 现货 0.1%（成交实测 taker 0.005 BTC → 0.000005 BTC），合约 0.05%；无费率接口 |

与 openapi 的差异：
- `/spot/orders/history` 会返回 `status=new` 的未成交订单（文档语义为历史订单，应只含 filled/canceled/rejected，或需说明）。
- 数量/价格返回未补齐 8 位小数（如 `"0.01"`、`"60000"`），文档示例为 `"0.01000000"`；前端按字符串原样展示。
- 网关未返回 CORS 头，本地联调通过 Next rewrites 同源代理：`NEXT_PUBLIC_API_BASE=/gw API_PROXY_TARGET=http://localhost:8080`。
