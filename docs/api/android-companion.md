# 安卓对接：本批后端契约

基线 `1342bc5`。聊天室、首页运营位、公开储备查询沿用现有接口，没有重写。下面只列安卓需要的路径。金额是十进制字符串。未配置时响应里的 `configured` / `available` / `submitted` 为 false，不表示已开通、已提交或已到账。

网关：除注明「登录」外，下列 GET 不需要 Token。登录接口使用 `Authorization: Bearer <accessToken>`，网关注入 `X-User-Id`。错误体为 `{code, message, requestId}`；部分未配置响应在同一层带上 `configured` / `submitted`。

## 1. 聊天室（已有，未改协议）

| 方法 | 路径 | 认证 |
|---|---|---|
| GET | `/chat/rooms` | 无 |
| GET | `/chat/rooms/{room}/messages?before=&limit=` | 无 |
| POST | `/chat/rooms/{room}/messages` | 登录 |
| POST | `/chat/messages/{id}/report` | 登录 |

功能开关 `chat` 关闭时返回 403 `FEATURE_DISABLED`。房间 id：`general`、`spot.{SYMBOL}`、`futures.{SYMBOL}`。历史按 id 倒序，`before` 为上一页最小 id，`limit` 默认 50，上限为后台 `historyLimitMax`（默认 100）。响应 `{items, hasMore}`。消息 `{id, room, sender, text, createdAt, mine?}`。`sender` 是打码 uid。匿名没有 `mine`。

发消息体 `{clientId, text}`。`clientId` 必填且幂等。须登录；默认还要 `kycLevel >= 1`（后台 `minKycLevel` 可调到 0）。限流、禁言、链接/联系方式拦截沿用现有规则，可能返回 `CHAT_KYC_REQUIRED`、`CHAT_RATE_LIMITED`（带 `retryAfterMs`）、`CHAT_MUTED`、`CHAT_BANNED`。

WebSocket：`GET /ws`（可带 `?token=`）。订阅频道 `chat:{room}`。帧与行情相同：`{channel, ts, data}`。`data` 为 `{type:"message", message}` 或 `{type:"delete", id}`。广播帧没有 `mine`，用 POST 返回的 `id` 认自己的消息。

后台：`/admin/v1/chat/*`（`content:edit`，写操作要 TOTP 和 reason）。

## 2. 学院

| 方法 | 路径 | 认证 |
|---|---|---|
| GET | `/academy/categories` | 无 |
| GET | `/academy/articles?category=&locale=&before=&limit=` | 无 |
| GET | `/academy/articles/{id}` | 无 |
| GET | `/market/search?q=` | 无 |

没有文章时 `items` 为 `[]`，不是示例文章。只返回 `status=published` 且分类启用的文章。正文 `format` 为 `plain`，按纯文本展示。列表不含 `body`。`limit` 默认 20，最大 50。

搜索：`q` 1–64 字。响应 `{query, symbols, articles}`。`symbols` 来自已有交易对；`articles` 只含已发布标题/摘要。草稿不会出现。

后台（`content:edit` + TOTP + reason）：`/admin/v1/academy/categories`、`/admin/v1/academy/articles`，以及 `POST .../articles/{id}/publish|unpublish`。新建文章是 `draft`。本仓库不预置、不发布文章。

## 3. 卡片

| 方法 | 路径 | 认证 |
|---|---|---|
| GET | `/cards/product` | 无 |
| GET | `/cards/application` | 登录 |
| POST | `/cards/applications` | 登录 |

未批准发行方时三者都是：

```json
{"configured":false,"submitted":false,"status":"unconfigured","product":null,"issuer":null,"application":null}
```

此时 POST 为 **409** `CARD_UNCONFIGURED`，`submitted:false`，**不写申请行**。不要把 409 显示成已提交。

只有后台把发行方草案交给另一名管理员批准后，公开产品才会带 `issuer.legalName` / `registrationRef`。本任务不填写、不批准任何发行方。批准后的 POST 才会插入 `submitted`，且不会自动变成 `approved`。

后台：`GET/PUT /admin/v1/cards/product`，`POST /admin/v1/cards/product/submit|approve|reject`。批准人不能是起草人或提交人，且发行方名称和登记号都必须非空。

## 4. 首页运营位（已有）

`GET /home/placements?slots=popup,carousel&platform=&locale=&appVersion=` 无认证。

响应 `{slots:{popup:[], carousel:[]}, serverTime}`。未配置时数组为空，不会填假图。`popup` / `carousel` 没有 `imageUrl` 的行不会下发。

后台：`/admin/v1/home/placements`（`content:edit` + TOTP + reason）。槽位还包括 `splash`、`grid`、`promo_bar`、`search_hint`。`GET /home/banners` 仍是 carousel 别名。

## 5. 储备证明

`GET /market/proof-of-reserves` 无认证。没有 `published` 快照时：

```json
{"available":false,"snapshotAt":null,"items":[]}
```

这不是空证明。有已发布快照时 `available:true`，`items[]` 为 `{asset, liabilities, reserves, ratio}`，金额为字符串。

新建不再直接公开。`POST /admin/v1/proof-of-reserves` 写入 `draft`。随后：

- `POST /admin/v1/proof-of-reserves/{id}/submit`
- `POST /admin/v1/proof-of-reserves/{id}/approve`
- `POST /admin/v1/proof-of-reserves/{id}/reject`

`id` 仍是 `snapshotAt` 的 Unix 毫秒。批准人必须不同于 `createdBy` 和 `submittedBy`。都要 `content:edit`、TOTP、reason。本任务不创建、不提交、不批准任何快照。

迁移 `00011`：旧行保持 `published`（它们本来就公开），新行默认 `draft`。迁移不插入数据。

## 6. 现货成交额与盘口

`GET /market/ticker` 的 `quoteVolume` 是本所 24h 成交的 `price * quantity` 之和（计价币）。没有本所成交时为 0，**不会**改用外部指数成交额。

若进程重启后内存成交窗口是空的，但 1 分钟成交 K 线里仍有成交量，ticker 用这些 K 线补 24h 成交额。指数 K 线不参与。本环境访问 `api.cowa.click` 返回 HTTP 451，不能据此判断生产当前有没有成交；字段映射本身按上述规则，不是把有成交写成 0 的固定错误。

盘口：

- `GET /market/depth?symbol=&limit=`：`{symbol, lastUpdateId, bids, asks}`，元素为 `["price","qty"]`。空数组表示撮合簿上没有挂单（做市未报价或已撤），不是字段名错误。
- `GET /futures/market/depth?symbol=&limit=`：同样 `bids`/`asks`。查询失败现在返回 500，不再伪装成空盘口。空数组表示没有 `new`/`partially_filled` 的限价单。

## 7. 美元折合

`GET /market/usd-rate` 无认证。

```json
{"pair":"USDTUSD","available":false,"rate":null,"source":"","updatedAt":null,"stale":false,"precision":8,"rounding":"half_away_from_zero","unit":"USD per 1 USDT","pegAssumed":false}
```

`rate` 是 **1 USDT 兑多少 USD**，来自 Coinbase `USDT-USD`，失败再试 Kraken `USDTUSD`。必须落在 `INDEX_USDT_USD_MIN`/`MAX`（默认 0.97–1.03）内，否则该来源作废。没有合格报价时 `available:false`，**不会**回退成 1。8 位小数，远离 0 的一半向上（half away from zero）。成功时 `source` 是主机名，`updatedAt` 是交易所时间（Kraken 用拉取时间），超过 1 分钟会刷新；刷新失败且报价不超过 15 分钟则 `stale:true`。

`GET /market/ticker` 每条增加 `usd`：`{available, price, rate, source, updatedAt, stale, precision, rounding, convention, pegAssumed}`。仅 `*USDT` 且有正的 `lastPrice` 和有效汇率时 `available:true`，`price = lastPrice * rate`（同样 8 位）。否则 `price:null`，`reason` 为 `rate_unavailable`、`quote_not_usdt` 或 `no_price`。WebSocket ticker 不带 `usd`，用 REST 或 `/market/usd-rate`。

`GET /market/fiat-rates?base=USDT|USDC|USD`：`rates` 是 **1 USD** 的法币汇率（来源 `FIAT_RATES_URLS`），`ratesBasis:"USD"`，`pegAssumed:false`。不要把 `rates.CNY` 直接乘 USDT 价格。两边都有时才有 `quotePerUsdt`（法币 per 1 USDT = 法币 per USD × USDTUSD）。汇率源全失败且没有上次成功值时 `available:false`、`rates:null`，不再返回写死的汇率表。

## 8. 登录、客服、买币、盈亏、充值账户

### Google

`GET /auth/providers` 无认证。未配置：`providers[0] = {id:"google", configured:false, clientIds:[]}`。

`POST /auth/google` 无认证，体 `{idToken, deviceId?, deviceName?}`。未设置 `GOOGLE_OAUTH_CLIENT_IDS` 时 **503** `GOOGLE_UNCONFIGURED`，`configured:false`。配置后校验 Google RS256 ID token（iss、aud、exp、`email_verified`）。成功返回与密码登录相同的 `{accessToken, refreshToken, tokenType, expiresIn}`。JWKS 临时失败为 503 `GOOGLE_UNAVAILABLE`，不会发会话。本环境不创建账号。

### 游客客服

`GET /support/guest` 无认证。默认 `{configured:false, available:false, mode:"unconfigured", entryUrl:null, loginRequiredForTickets:true, ticketsPath:"/support/tickets"}`。工单仍要登录：`/support/tickets`。仅当 `SUPPORT_GUEST_PROVIDER` 与不含 query 的 https `SUPPORT_GUEST_ENTRY_URL` 都合法时，`mode:"external"` 并返回入口 URL。服务端不代连供应商。

### 法币买币

`GET /fiat/buy` 无认证。默认 `{configured:false, available:false, reason:"unconfigured", quotes:false, orders:false, charged:false}`。即使配置了供应商名称和 https API 基址，`available` 仍为 false，`reason:"provider_not_integrated"`。

`POST /fiat/buy/quotes`、`POST /fiat/buy/orders` 要登录，一律 **503** `FIAT_UNAVAILABLE`，`charged:false`，`orderCreated:false`。没有报价、订单或扣款。

### 历史盈亏

`GET /futures/pnl/history?from=&to=` 登录。`from`/`to` 为 RFC3339 或 `YYYY-MM-DD`（日期 `to` 含当天）。默认近 30 天，最长 366 天。按 UTC 日返回 `{asset:"USDT", realizedPnlAvailable:true, points[], totals}`。点为 `{date, pnl, realizedPnl, funding, fee, liquidationFee, insuranceCover, tradeCount}`，8 位小数，来自 `fut_bills` / `fut_trades`。没有流水的日子是 0，不是示例曲线。今日汇总仍是 `GET /futures/pnl/today`。

`GET /spot/pnl/history?from=&to=` 登录。`realizedPnlAvailable:false`，`realizedPnl:null`。只统计 USDT 计价成交额和笔数；非 USDT 计价只计 `nonUsdtTradeCount`，不折合。手续费仅在全部为 USDT 时给出。

### 默认充值账户

`GET /assets/deposit-account` 登录。`{account, source, platformDefault, supported:["funding","spot"]}`。`source` 为 `user` 或 `platform`。没有个人设置时等于后台 `deposit_account`（默认 funding）。**`supported` 不含 `futures`。**

`PUT /assets/deposit-account` 体 `{account:"funding"|"spot"}`。`account:"futures"` 返回 **409** `DEPOSIT_ACCOUNT_UNSUPPORTED`，不写偏好。`DELETE /assets/deposit-account` 恢复跟随平台。只影响之后链上充值入账账户，不划转余额、不到账。

合约账户不能作为充值目标：确认数足够后，钱包只调用资产服务 `POST /internal/deposit`，入账在资金或现货的复式账本（`wallet_ops` 按 `dep:{network}:{tx}:{log}` 幂等）。合约可用保证金在另一张 `fut_accounts` 上，今天只能通过先扣资金/现货再记一笔 `transfer_in` 划进去。没有一条事务能只给合约加余额、失败整体回滚、并且重放不会记两次。因此不把 `futures` 放进 `supported`，也不把充值先记到资金账户再自动划转。

`GET /market/ticker` 的 WebSocket `ticker:{symbol}` 帧现在带同一个 `usd` 对象。推送只读内存缓存，不在每笔成交上请求 Coinbase/Kraken。缓存由后台每分钟刷新；没有合格报价时 `available:false`，`pegAssumed:false`。

`GET /assets/overview` 增加 `depositAccountSource`、`platformDepositAccount`。原 `depositAccount` 改为当前生效账户。

## 环境变量（不要填密钥值）

| 变量 | 空值含义 |
|---|---|
| `GOOGLE_OAUTH_CLIENT_IDS` | Google 登录不可用 |
| `GOOGLE_OAUTH_JWKS_URL` | 默认 Google 证书地址 |
| `SUPPORT_GUEST_PROVIDER` / `SUPPORT_GUEST_ENTRY_URL` | 游客客服不可用 |
| `FIAT_ONRAMP_PROVIDER` / `FIAT_ONRAMP_API_BASE` | 法币买币不可用；设了也不会下单 |
| `INDEX_USDT_USD_MIN` / `INDEX_USDT_USD_MAX` | USDTUSD 合法区间 |
| `COINBASE_API_URL` / `KRAKEN_API_URL` | USDTUSD 来源 |
| `FIAT_RATES_URLS` | 美元法币汇率来源 |

## 9. 充值返现与交易抽奖

真实活动和奖品未知，两条都保持未配置，不插入参与、中奖或订单。

`GET /campaigns/deposit-cashback` 可匿名。可选 Bearer 只用于将来的个人 `joined`，现在没有档位。

```json
{"configured":false,"available":false,"eligible":false,"title":"","subtitle":"","startAt":null,"endAt":null,"tiers":[],"rules":[]}
```

`startAt`/`endAt` 在将来有活动时用 Unix 毫秒。`rules` 是字符串数组。`POST /campaigns/deposit-cashback/tiers/{id}/join` 要登录，**409** `CAMPAIGN_UNCONFIGURED`，`joined:false`，不写记录。

`GET /campaigns/trade-lottery` 可匿名。

```json
{"configured":false,"available":false,"title":"","startAt":null,"endAt":null,"prizes":[],"winners":[],"rules":[],"availableDraws":0}
```

`rules` 项在有活动时为 `{title, body}`。`winners` 只会出现打码字符串，当前为空。`availableDraws` 为 0，不是个人抽奖次数。`GET /campaigns/trade-lottery/entries?status=pending|drawn` 要登录，`{items:[], configured:false}`。没有开奖或派奖接口。

邀请返佣继续用已有合伙人接口：`GET /partner/summary`、`/partner/links`、`/partner/team`、`/partner/commissions`。不另建邀请接口。

## 10. API Key

沿用子账户密钥的校验方式：请求头 `X-LB-APIKEY` 与 `X-LB-SECRET`，服务端只存 `sha256(secret)`。现有验签是比对哈希，不是用明文做 HMAC，所以只存哈希不会破坏校验。

| 方法 | 路径 | 认证 |
|---|---|---|
| GET | `/account/api-keys` | 登录 |
| POST | `/account/api-keys` | 登录，且必须已开启 TOTP |
| DELETE | `/account/api-keys/{id}` | 登录，仅能删自己的 |

POST 体 `{label, permissions:["read"], ipWhitelist, totpCode}`。`permissions` 出现 `read` 以外的值返回 400 `PERMISSION_NOT_ALLOWED`。未开 TOTP 为 403 `TWOFA_REQUIRED`，验证码错误为 401 `INVALID_TOTP`。成功 **201** `{item, secret}`。`secret` 只在这一次出现，列表和日志都没有它。`item` 为 `{id, label, apiKey, permissions:["read"], ipWhitelist, createdAt}`，`createdAt` 是毫秒，`id` 是数字字符串。密钥前缀 `lbu_`，256 位随机。最多 10 把。

`ipWhitelist` 最多 20 条 IP 或 CIDR，空名单表示不限制。网关只在对等地址属于 `TRUSTED_PROXY_CIDRS` 或带有效 `X-Linbit-Edge-Auth` 时采信转发的客户端 IP，其余用 TCP 对等地址。撤销后下一次校验即失败。`read` 只能 GET/HEAD `/assets`、`/spot`、`/futures`，不能下单、划转或提币。

## 11. 与安卓页面的字段差异

对照 `feat/android-easicoin-restore` `427b123` 的页面，不是改那条分支：

- 学院文章 `id` 改为 JSON 字符串，并同时给 `category`（等于 `categoryId`）和 `publishedAt`（UTC，`YYYY-MM-DDTHH:MM:SSZ`）。页面把 `id` 当成字符串，日期用 `publishedAt`。
- 卡片在未配置时 `product` 为 null。页面还预留了 `name/network/currency/fees/regions/termsUrl`，没有发行方资料，这些字段不会编出来。
- 创建 API Key 的页面请求体没有 `totpCode`，服务端仍然要求。未带验证码不能创建。
- 充值账户页面有「合约账户」文案，但只渲染 `supported` 里的值。当前数组没有 `futures`。

迁移增加 `account/00016_user_api_keys.sql`（空表）。没有活动表，也没有把旧余额迁到合约。

迁移（服务启动时 goose，本任务不在生产执行）：`market-data/00010_academy.sql`、`00011_por_review.sql`，`account/00014_oauth_identities.sql`、`00015_cards.sql`、`00016_user_api_keys.sql`，`asset/00009_user_deposit_account.sql`。表都是空的，除储备状态列外不改现有公开数据。
