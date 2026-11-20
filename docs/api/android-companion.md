# 安卓对接：本批后端契约

本文件描述 **companion 分支已实现的契约**，不是生产已上线证明。运维尚未核对生产 SHA 和 goose。对 `api.cowa.click` / `www` 的 GET 曾返回 Cloudflare 403 error 1010，这不能当成应用 404 或未实现。生产是否已部署以运维只读核对为准。

基线实现从 `1342bc5` 累加。金额是十进制字符串。未配置时 `configured` / `available` / `submitted` 为 false，不表示已开通、已提交或已到账。

## 上线状态（代码已实现 / 生产未验证）

| 能力 | 分支代码 | 生产 |
|---|---|---|
| `GET /market/usd-rate`、REST ticker `usd`、WebSocket `ticker:{symbol}` 的 `usd` | 已实现，同一对象，USDT 不按 1 美元 | 未由本轮验证部署 |
| `GET/PUT/DELETE /assets/deposit-account` | 已实现，仅 `funding`/`spot` | 未由本轮验证部署 |
| 聊天室读取改为必须登录 | 本轮起 REST 与 `chat:{room}` 订阅都要登录 | 旧生产行为曾是游客可读；以部署后的 SHA 为准 |
| 学院、卡片未配置、储备草稿审核、活动未配置、只读 API Key | 已实现 | 未由本轮验证部署 |

网关：除注明「登录」外，下列 GET 不需要 Token。登录接口使用 `Authorization: Bearer <accessToken>`，网关注入 `X-User-Id`。错误体为 `{code, message, requestId}`；部分未配置响应在同一层带上 `configured` / `submitted`。

## 1. 聊天室（登录后查看）

2026-11-08 起，房间列表、历史消息和 `chat:{room}` 订阅都要登录。这替换了此前「游客可读」的规则。公开行情 `/market/*` 与 `/ws` 上的 `ticker`/`depth`/`trades`/`kline` 仍然匿名。旧客户端若匿名拉取聊天室，会收到 401，而不是空列表。

| 方法 | 路径 | 认证 |
|---|---|---|
| GET | `/chat/rooms` | 登录 |
| GET | `/chat/rooms/{room}/messages?before=&limit=` | 登录 |
| POST | `/chat/rooms/{room}/messages` | 登录 |
| POST | `/chat/messages/{id}/report` | 登录 |

功能开关 `chat` 关闭时返回 403 `FEATURE_DISABLED`。房间 id：`general`、`spot.{SYMBOL}`、`futures.{SYMBOL}`。历史按 id 倒序，`before` 为上一页最小 id，`limit` 默认 50，上限为后台 `historyLimitMax`（默认 100）。响应 `{items, hasMore}`。消息 `{id, room, sender, text, createdAt, mine?}`。`sender` 是打码 uid。匿名没有 `mine`。

发消息体 `{clientId, text}`。`clientId` 必填且幂等。须登录；默认还要 `kycLevel >= 1`（后台 `minKycLevel` 可调到 0）。限流、禁言、链接/联系方式拦截沿用现有规则，可能返回 `CHAT_KYC_REQUIRED`、`CHAT_RATE_LIMITED`（带 `retryAfterMs`）、`CHAT_MUTED`、`CHAT_BANNED`。

WebSocket：`GET /ws`。行情频道仍可匿名。订阅 `chat:{room}` 必须在握手时带 `Authorization: Bearer` 或 `?token=`，否则错误码 `AUTH_REQUIRED`，不推送历史。帧与行情相同：`{channel, ts, data}`。`data` 为 `{type:"message", message}` 或 `{type:"delete", id}`。广播帧没有 `mine`，用 POST 返回的 `id` 认自己的消息。发言仍受 KYC、限流、禁言和内容规则约束。

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

`GET /market/ticker` 的 `quoteVolume` 是本所 24h 成交的 `price * quantity` 之和（计价币）。没有本所成交时为 0，不会改用外部指数成交额。`volume` 是本所基础币成交量，同样规则。生产列表里这两项为 0，且 `priceSource` 为 `index`，表示最新价来自外部指数、24h 窗口里没有本所成交；聚合代码在这种情况下明确把成交量留成 0。这不是字段映射错误。

若进程重启后内存成交窗口是空的，但 1 分钟成交 K 线里仍有成交量，ticker 用这些 K 线补 24h 成交额。指数 K 线不参与。

`GET /market/ticker` 不带 `symbol` 时只返回加密现货。股票和现货贵金属用 `include=tradfi` 或 `kind=stock|metal`，或直接 `?symbol=AAPLUSDT`。每条 ticker 带 `market`（spot|futures）和 `kind`（crypto|stock|metal|commodity）。WebSocket `ticker:{symbol}` 使用同一对象，按代码订阅仍可收到 TradFi 交易对。`GET /market/categories?market=spot` 同样去掉 TradFi 成员，贵金属、股票、大宗商品分类因此为空；`kind` 标在分类上。`nonEmpty=true` 会去掉空分类。

`GET /futures/market/ticker` 默认只返回加密永续。`XAGUSDT`、`WTIUSDT` 仍是期货引擎里的 TradFi 永续（`fut_contracts`、风险档、资金费和网关地区校验都还在），用 `?symbol=`、`include=tradfi` 或 `kind=metal|commodity` 读取。下单路径不改。目录仍以 `GET /tradfi/assets` 为准。

盘口：

- `GET /market/depth?symbol=&limit=`：`{symbol, lastUpdateId, bids, asks}`，元素为 `["price","qty"]`。空数组表示撮合簿上没有挂单（做市未报价或已撤），不是字段名错误。
- `GET /futures/market/depth?symbol=&limit=`：同样 `bids`/`asks`。查询失败现在返回 500，不再伪装成空盘口。空数组表示没有 `new`/`partially_filled` 的限价单。

## 7. 美元折合

`GET /market/usd-rate` 无认证。

```json
{"pair":"USDTUSD","available":false,"rate":null,"source":"","updatedAt":null,"stale":false,"precision":8,"rounding":"half_away_from_zero","unit":"USD per 1 USDT","pegAssumed":false}
```

`rate` 是 **1 USDT 兑多少 USD**，来自 Coinbase `USDT-USD`，失败再试 Kraken `USDTUSD`。必须落在 `INDEX_USDT_USD_MIN`/`MAX`（默认 0.97–1.03）内，否则该来源作废。没有合格报价时 `available:false`，**不会**回退成 1。8 位小数，远离 0 的一半向上（half away from zero）。成功时 `source` 是主机名，`updatedAt` 是交易所时间（Kraken 用拉取时间），超过 1 分钟会刷新；刷新失败且报价不超过 15 分钟则 `stale:true`。

`GET /market/ticker` 与 WebSocket 频道 `ticker:{symbol}` 使用同一个 `usd` 对象：`{available, price, rate, source, updatedAt, stale, precision, rounding, convention, pegAssumed}`。仅 `*USDT` 且有正的 `lastPrice` 和有效汇率时 `available:true`，`price = lastPrice * rate`（同样 8 位）。否则 `price:null`，`reason` 为 `rate_unavailable`、`quote_not_usdt` 或 `no_price`。推送只读内存缓存，不在每笔成交上请求外部汇率；后台约每分钟刷新。缓存为空时 `available:false`，不会把最新价当成美元价。

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

## 13. 模拟永续（USDX）

现有 `/paper/orders` 仍是现货 USDT 市价，行为不变。模拟永续是另一套表，登录后走 `/paper/futures/*`，不写真实余额、真实撮合或 `fut_accounts`。

账户 `GET /paper/futures/account` 返回虚拟 USDX 的 `balance`、`available`、`frozen`、`unrealizedPnl`，以及 `generation`、`initialBalance` 和 `isolated:true`。合仓（`one_way`）下反向开仓先按标记价平掉原仓并结算已实现盈亏，只有超出的数量才开到另一边。分仓（`hedge`）的多空仓位互不影响。强平价：多仓 `(entry*qty - margin) / (qty*(1-mmr))`，空仓 `(entry*qty + margin) / (qty*(1+mmr))`。逐仓的 margin 是仓位保证金，全仓用钱包余额。资金费只按真实 funding-history 的结算时间和费率逐仓位入账，同一条记录重复执行不会再记。`POST /paper/futures/account/claim` 领取资金，单笔不超过 `claimCap`（100000），余额不超过 `maxBalance`（1000000），60 秒一次。`POST /paper/futures/account/reset` 要 `confirmation:"RESET_PAPER_FUTURES"`。

`GET /paper/futures/contracts` 只列 BTC/ETH/BNB/ARB/XRP/SOL-USDX。tick、最小量、杠杆上限、维持保证金率和 `riskTiers` 来自真实 `GET /futures/market/contracts` 与 `/futures/market/risk-tiers`。标记价和指数价来自真实 ticker。没有合约或标记价时 `available:false`，并给出 `unavailableReason`。资金费率只在真实 `funding-history` 有记录时 `fundingAvailable:true`，否则不结算、不编造费率。

下单 `POST /paper/futures/orders`：`action` 为 `open` 或 `close`；`type` 为 `market`、`limit`、`plan`、`post_only`；`sizeMode` 为 `quantity`、`cost` 或 `notional`；`marginMode` 为 `cross` 或 `isolated`。合仓/分仓用 `POST /paper/futures/position-mode`。止盈止损可随单或 `POST /paper/futures/positions/{symbol}/tpsl`。平仓 `POST /paper/futures/positions/{symbol}/close`，一键平仓 `POST /paper/futures/positions/close-all`。委托、成交、流水分别是 `/paper/futures/orders`、`/trades`、`/ledger`。强平由标记价触发，同一仓位版本只记一次。

WebSocket 私有频道 `paper:positions`、`paper:orders`、`paper:balances` 必须登录。快照来自 paper 服务；没配 `PAPER_URL` 时不推伪造余额。模拟跟单本轮不做。

## 12. 本轮补齐与仍阻塞

K 线：`GET /market/klines` 与 `GET /futures/market/klines` 增加固定秒数周期 `3m`、`2h`、`6h`、`12h`，由成交按桶聚合，与 1m/5m 相同。不提供 `1M`（自然月，且与 `1m` 大小写易混）和事件合约的 `10m`。事件结算周期仍以 `ev_config.periods` 为准，默认 `5m,15m,30m,1h,4h,1d`。看涨看跌共用一个 `payoutRatio`。下单扣款是现货 USDT（`system:event`，幂等 ref `event:stake:{id}`），余额不足时订单 `rejected`，不从合约保证金扣。本轮不改扣款账户、不新增 10m、不分方向赔率。

`GET /home/newcomer` 的 `claimable` 是 JSON 字符串，值是可领取任务个数（无任务时为 `"0"`），不是 USDT 金额。金额仍看 `total` 和任务的 `amount`。类型以 OpenAPI `HomeNewcomer.claimable: string` 和安卓 `ApiHomeNewcomer.claimable: String` 为准。

`GET /futures/bonus` 的 `available` 在正式 OpenAPI（`docs/api/openapi.yaml`，并与 `mobile/openapi.yaml` 保持同一份）里是对象 `{trial, deduction}`，两项都是十进制字符串。实现返回同一对象，不把两种余额压成一个数。没有赠金时两项为 `"0"`。安卓 `ApiFuturesBonus200Response.available` 与 iOS `APIFuturesBonus200Response.available` 仍是已生成的 `String`，需要在各自移动端分支重新生成模型后才能按对象解析。本仓库不改那些生成文件。`ApiNotification` 同样还没有 `link`；正式响应已有可选 `link.type=support_ticket` 与 `link.ticketId`。

`GET /announcements` 与 `GET /announcements/{id}` 已是公开公告。没有单独的「快讯」接口。币种全称没有元数据来源，不编造 Bitcoin/Ether。闪兑限额仍是 `minUsdt`/`maxUsdt`（USDT 名义），没有按币种的独立 `fromMin`/`fromMax`。策略账户没有单独保证金率接口。单币资金流水没有公开过滤接口。这些不在本轮猜测规则。

自有客服仍是登录用户工单：`/support/tickets`。管理员经 `/admin/v1/support/tickets/{id}/messages` 回复。没有游客会话，避免无身份的人冒充用户。第三方客服 URL 仍只在明确配置后出现。

管理员回复会给工单所有者写一条 `kind:"system"` 通知。新回复的 `link` 为 `{type:"support_ticket", ticketId:"<id>"}`，`ticketId` 是数字字符串，与 `GET /support/tickets/{id}` 相同，且只出现在该用户自己的 `GET /notifications` 里。没有 `deepLink`，也不接受外部 URL。`link` 缺省表示这条通知没有可靠定位（历史行、广播、交易/安全通知，以及正文里碰巧写了工单号的旧消息）。客户端不得从正文猜工单。

工单未读和系统通知未读仍然分开。`GET /support/tickets/{id}` 只把该工单的 `unread` 清掉（`user_read_at`）。系统通知仍用登录用户的 `POST /notifications/read`，按通知 `id` 标记；`{all:true}` 只影响当前用户。没有「打开工单即标关联通知已读」的既有约定，因此不自动同步。

## 11. 与安卓页面的字段差异

对照 `feat/android-easicoin-restore` `427b123` 的页面，不是改那条分支：

- 学院文章 `id` 改为 JSON 字符串，并同时给 `category`（等于 `categoryId`）和 `publishedAt`（UTC，`YYYY-MM-DDTHH:MM:SSZ`）。页面把 `id` 当成字符串，日期用 `publishedAt`。
- 卡片在未配置时 `product` 为 null。页面还预留了 `name/network/currency/fees/regions/termsUrl`，没有发行方资料，这些字段不会编出来。
- 创建 API Key 的页面请求体没有 `totpCode`，服务端仍然要求。未带验证码不能创建。
- 充值账户页面有「合约账户」文案，但只渲染 `supported` 里的值。当前数组没有 `futures`。

迁移增加 `account/00016_user_api_keys.sql`（空表）。没有活动表，也没有把旧余额迁到合约。

迁移（服务启动时 goose，本任务不在生产执行）：`market-data/00010_academy.sql`、`00011_por_review.sql`，`account/00014_oauth_identities.sql`、`00015_cards.sql`、`00016_user_api_keys.sql`、`00017_notification_resource.sql`，`asset/00009_user_deposit_account.sql`。`00017` 只给 `notifications` 增加可空的 `resource_type`/`resource_id`，旧行保持 NULL。表都是空的，除储备状态列外不改现有公开数据。
