# Batch 4 后端缺口

## 闪兑（convert）
- `GET /convert/pairs` 仅返回 `minUsdt`/`maxUsdt`（USDT 计价）。参考 App 对每个币种显示单笔限额（如 BTC `0.000003~10.5`），后端缺少按币种的 `minAmount`/`maxAmount` 字段。当前：兑出币为 USDT 时显示 min~max，其他币种显示「请输入数量」。需要后端在 pair 中补 `fromMin`/`fromMax`。
- 未登录参考汇率来自公开现货行情 last 价推算（仅展示），后端无公开 indicative rate 接口。
- 闪兑记录页无筛选（参考有筛选图标）；`GET /convert/orders` 无 asset/时间筛选参数。

## 首页
- 参考登录态榜单下方有「公告 / 快讯 / 学院」。候选已有 `/announcements` 及详情接口，Batch8 Android 已接首页公告区列表→详情（公开 GET）；不能再列为后端缺接口。无独立快讯契约；学院取 `/academy/articles?limit=5`。候选未部署，不能据此声称生产可用。
- 榜单 TradFi 分组用 `/tradfi/assets` 的 symbol 过滤合约行情。
- 首页轮播运营位为空时本地兜底为 福利中心 / 跟单 / 策略交易 三张（自绘矢量配图，指向 App 内真实页面）；有后端 carousel 图片时用后端图片。

## 现货交易页
- 生产 `GET /market/depth?symbol=BTCUSDT` 返回 `bids:[] asks:[]`（2026-11-08 13:00 EST 实测），盘口显示真实空态「--」；参考盘口有 5 档深度与 B/S 比例。需撮合/做市深度上线。
- 盘口「≈ $」依赖 `/market/usd-rate`，生产未部署时显示 `$--`。

## 合约交易页
- 生产合约盘口同样无深度（显示「--」）；杠杆上限由 `maxLeverage` 下发（生产 20x，参考 100x/200x），属后端配置差异。
- 杠杆弹层「最大仓位」取风险档位 `maxNotional`，缺档位时显示 `--`。

## 资产页
- `GET /futures/account` 无体验金、抵扣金字段；`/futures/bonus.available` 已在 Batch8 福利中心按两字段展示。合约资产卡尚未串接该独立只读接口，仍显示 `--`，属于后续前端接入，不应说后端无赠金接口。
- 零余额币种列表：资金账户取 `/wallet/networks` 的 asset 去重，现货取行情 USDT 交易对 base；余额为真实 0。
- 币种详情页：参考点币种进入详情（本轮未截到参考详情页），我们点币种进入充值页。

## 资产（续）/ 登录态实机
- 币种全称：`/wallet/networks`、`/market/symbols`、`/assets/balances` 均无全称字段 → 资产列表第二行显示币种代码（参考显示 Bitcoin / Ether 等全称）。需后端在资产元数据中提供 `name`。
- 测试账号偏好计价为 CNY，生产 `/market/usd-rate` 未部署 → 所有折合显示 `≈ ¥--`（参考显示 `≈ $0.00`）。

## 资产页 理财 / 策略 / 币种详情
- 策略账户：后端无「策略账户」余额/保证金率接口，权益与未实现盈亏由 /strategy/grids 汇总，保证金率显示 "--"。
- 理财「借贷订单」：后端无借贷产品，入口未放。
- 币种详情右上「记录」：无按币种过滤的资金流水接口，暂未放图标。

## 福利中心
- `GET /home/newcomer` 的 `claimable` 旧观察为数字；冻结候选 be357384 正式为字符串计数 `"0"/"1"`，不是金额；`completed` 为 integer。Android DTO 已是 String，宽松 number 仅兼容旧生产，不反改正式类型。
- `GET /futures/bonus` 的 `available` 正式冻结契约是 `{trial, deduction}` 十进制字符串对象；Batch8 已同步双 spec、生成对象 DTO 并在福利中心独立展示；缺失/错误显示 `--`，真实零显示 0，不相加冒充资产余额。不能要求后端改回标量。
- 无「每日任务」接口：每日任务标签显示「暂无每日任务」。
- 新人任务仅有单一有效期（days），参考的 7/30/90 天分档无后端来源，只显示实际天数。
- 无理财/现货类新人任务：对应筛选为空态。
- 福利中心 FAQ 为客户端静态文案（规则说明），非后端数据。
- 头图使用自有礼盒图标，非参考 3D 头图。

## 事件合约
- 结算周期以 `/event/products.periods` 为准（5m/15m/30m/1h/4h/1d）；参考的 10m 无后端来源，不显示。
- 参考 K 线周期含 10m；`/market/klines` 无 10m，Batch8 按候选契约展示 1m/3m/5m/15m/30m/1h/2h/4h/6h/12h/1d/1w；无 1M/10m。部署前错误如实显示，不保留另一周期图当数据。
- 指标：MA / BOLL / EMA / Supertrend 由客户端用真实 K 线计算；Supertrend 使用 Wilder ATR 10 / 倍数 3（参考参数尚未采集确认），不需后端计算。已实现本地水平线、两点趋势线、选中删除/撤销/确认清空及缩放，按币种/周期隔离；仅参考画线入口可证实，具体工具菜单缺参考素材，不能称全套一比一。
- 「⋯」菜单：参考有 交易规则 / 历史数据 / 区间交易限额 / FAQ / 新手指引；后端无历史数据、区间限额、FAQ 内容接口，仅保留 交易规则（按 products 配置生成）与 新手指引。
- 看涨/看跌收益率共用 `payoutRatio`（后端无分方向赔率）。
- 冻结候选文档/实现已确认扣现货 USDT；只是代码级确认，不是生产交易验证。本批未下单。

## 新契约对接状态（历史观察与运维反馈，非 Batch8 生产实测）

| 接口 | 生产状态 | 安卓处理 |
|---|---|---|
| `GET /chat/rooms`、`/chat/rooms/{room}/messages`、`POST` 发言、`/ws chat:{room}` | 已部署（rooms 返回 general + spot.* ，消息为空） | 已接入：登录后查看群组/消息及发言；游客显示登录拦截，不请求聊天室接口或订阅 WS；登录成功返回聊天室（2026-11-08 用户确认替代旧游客可读规则）（clientId 幂等）、WS message/delete 帧、用发言返回 id 识别自己；FEATURE_DISABLED/KYC/禁言/频率/长度/链接/联系方式/敏感词/不可用 均显示中文错误 |
| `GET /home/placements` | 已部署，所有槽位为空 | 请求 popup,carousel,search_hint；carousel 有数据用后端，为空时显示三张自有配图兜底 banner（福利中心/跟单/策略交易，可手滑、自动轮播），popup 为空不弹，search_hint 为空时搜索框占位 BTC |
| `GET /market/proof-of-reserves` | 已部署，`available=false` | 储备金证明弹层如实显示「暂未发布」空态 |
| `GET /market/usd-rate` | **未部署（404）**，仅在分支 cursor/android-backend-companion-43c2 | 失败时不假设 1 USDT = 1 USD：折合价显示 `$--` |
| REST ticker `usd` 字段 / WS ticker `usd` | **未部署**（生产 ticker 无 usd） | Batch8 已完整解析 `available/price/rate/source/updatedAt/stale/precision/rounding/convention/pegAssumed/reason`；行情行优先显示 usd.price，usd-rate 缺失时用 ticker usd.rate 兜底，均缺失显示 `--` |
| `GET/PUT/DELETE /assets/deposit-account` | 运维最新内网反馈未部署（404）；公网 403/error1010 是 WAF，须区分来源，不绕过 | 后端当前候选仅 funding/spot；合约直充不得开放。前端仅按服务端 supported 启用候选；需 WAF 正常放行后只读联调，禁止绕过 |
| K 线周期 `ApiKlineInterval` | 历史生产仅8项；候选正式12项，未据此确认部署 | Batch8 UI/生成枚举按12项适配，新增3m/2h/6h/12h；不支持1M，错误不伪造为成功 |

后端待办：部署 companion 分支的 `/market/usd-rate` 与 ticker `usd` 字段；配置 placements 轮播位与储备金快照后客户端自动展示。

最新候选 a6257c29e2c2d73439624e086eb2ca9c53098621 取代 be357384，仍未合并/部署。通知 link 已有正式 support_ticket/ticketId 字符串契约，客户端下一批接入精确单条已读与跳转；不是继续等缺字段。account 迁移17交运维，本批不改后端。


## Batch9（2026-11-08 America/New_York）
- 通知 link→工单详情+精确已读：客户端已接 a6257c29 契约；迁移17/真实工单发送仍非本批。
- 合约资产卡已串独立 `/futures/bonus`；不开放合约直充。
- 真机/KVM 仍受限；远端 CI 对上一 head 已成功，本批推送后另查一次不轮询。
