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

## 行情 WS + REST 接入（feat/ui-p0-flows）

- `apps/web/lib/market.ts`：单例 WebSocket（`/ws`，可用 `NEXT_PUBLIC_WS_URL` 覆盖；`NEXT_PUBLIC_API_BASE=/gw` 时走 `/gw/ws` 代理），
  订阅 `ticker:`/`depth:`/`kline:<sym>:<iv>`/`trades:`；断线指数退避（1s→15s）重连并重订阅，20s ping、响应服务端 ping、60s 无帧重连。
- 首屏先取 REST 快照（`/market/ticker|depth|klines|trades|symbols`），WS 增量合并；depth 订阅首帧按全量、后续按增量（量 0 删除，`lastUpdateId` 去重）。
- REST 与 WS 都不可用时回退演示数据，UI 标注「● 实时 / ○ 演示」。
- 现货页 K 线（KLinePro `feed` 属性）、订单簿、最新成交、顶部 ticker，行情页/首页现货列表（BTC/ETH）已接入；价格/数量按 `/market/symbols` 的 `pricePrecision`/`quantityPrecision` 定点格式化。
- 合约页仍为演示数据（后端尚无合约行情频道）。
- 提现：网络列表与 Gas 估算来自 `GET /wallet/withdraw/networks`，记录来自 `GET /wallet/withdraw/records`；USDT 最低 12（前端取 max(接口, 12)），仅收链上 Gas。

### 与文档不一致（已于 main c5143eb 修复 ✅）
- ✅ `GET /market/depth` / ticker 价格与成交额已按交易对精度补零。
- ✅ `GET /market/ticker?symbol=X` 返回单对象（前端仍兼容数组）。
- ✅ `/wallet/withdraw/networks` USDT 各网络 `minWithdraw` 统一为 12；前端已改为直接使用接口值（12 仅作 mock 回退）。
- ✅ websocket.md 已更新连接地址与周期。

## 账户模块对接（feat/ui-p0-flows）

已对接真实接口（`apps/web/lib/account.ts`，5xx/网络错误回退 mock）：注册两步（`/auth/code/send` → `/auth/register`，邮箱或手机 E.164 + 国家区号）、登录（`challengeToken` → `/auth/login/verify`，totp/sms/email，`/auth/login/code` 重发）、找回密码（发码 → 输入验证码 → `/auth/password/reset`）、设备列表/移除、提现地址簿增删 + 白名单开关（`/account/code/send purpose=address` 或 TOTP）、`PATCH /account/profile`。

与 openapi 的差异 / 待确认：
- 找回密码无独立“校验验证码”接口，第 2 步仅前端校验长度，验证码在 `/auth/password/reset` 时才校验（错误时退回第 2 步）。
- `Device` 无 `current` 字段；前端以 `cid:<本地 deviceId>` 比对识别本机（依赖 deviceId 前缀约定）。
- `/auth/code/send` 等的响应体为内联类型，未抽成 schema（`SendCodeResponse`）。
- 头像只支持 URL（无上传接口）。
