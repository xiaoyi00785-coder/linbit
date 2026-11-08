# 第三批：前端预期的后端字段

已按 `cursor/android-backend-companion-43c2@140dc2f` 的 `docs/api/android-companion.md` 对接（只读，未合并）：

| 功能 | 路径 | 前端使用字段 |
|---|---|---|
| 默认充值账户 | GET/PUT/DELETE `/assets/deposit-account` | account, source(user/platform), platformDefault, supported；PUT `{account}` |
| Google 登录 | GET `/auth/providers`；POST `/auth/google` | providers[].id/configured/clientIds；body `{idToken,deviceId,deviceName}`；错误码 GOOGLE_UNCONFIGURED / GOOGLE_UNAVAILABLE |
| 游客客服 | GET `/support/guest` | configured, available, mode(external/unconfigured), entryUrl |
| 法币买币 | GET `/fiat/buy` | configured, available, reason(unconfigured/provider_not_integrated)；报价/下单接口未调用（固定 503） |
| 历史盈亏 | GET `/futures/pnl/history`、`/spot/pnl/history` | points[{date,pnl,realizedPnl,funding,fee,liquidationFee,insuranceCover,tradeCount}], totals；现货 quoteVolume, tradeCount, nonUsdtTradeCount, realizedPnlAvailable |
| 学院 | `/academy/categories`、`/academy/articles`、`/academy/articles/{id}`、`/market/search?q=` | categories[{id,name}]；articles[{id,title,summary,category,publishedAt}]；详情 body(plain) |
| 卡片 | `/cards/product`、`/cards/application`、POST `/cards/applications` | configured, submitted, status, product{name,network,currency,fees,regions,termsUrl}, issuer{legalName,registrationRef}, application{id,status,createdAt}；409 CARD_UNCONFIGURED |

**契约里没有、前端已留接入点的（需后端新增）：**

| 功能 | 预留路径 | 字段 |
|---|---|---|
| 充值返现 | GET `/campaigns/deposit-cashback`；POST `/campaigns/deposit-cashback/tiers/{id}/join` | title, subtitle, startAt, endAt(ms), tiers[{id,depositAmount,bonusAmount,dailyFuturesVolume,asset,joined}], rules[string], eligible |
| 交易抽奖 | GET `/campaigns/trade-lottery`、`/campaigns/trade-lottery/entries?status=pending|drawn` | title, startAt, endAt, prizes[{asset,amount}], winners[打码字符串], rules[{title,body}], availableDraws；entries[{orderId,symbol,closedAt,status,prize}] |
| API Key | GET/POST `/account/api-keys`，DELETE `/account/api-keys/{id}` | items[{id,label,apiKey,permissions,ipWhitelist,createdAt}]；POST `{label,permissions:["read"],ipWhitelist}` → `{item,secret}`（secret 只返回一次）。建议创建要二次验证 |
| 邀请好友 | 复用 `/partner/summary|links|team|commissions` | 若邀请返佣与合伙人分开计算，需要独立接口 |

聊天室、首页运营位、储备证明、USD 折合（`/market/usd-rate`、ticker.usd）本批**未改动**安卓端对接，留作下一批。
