# Linbit 后端服务（规划）

本目录是 Linbit 交易所**后端服务的占位与规划**。当前前端（`apps/web`）通过
`@linbit/api-client` 直接对接 Prism mock（`docs/api/openapi.yaml`）。后端按下述划分逐步落地时，
各服务对外契约应继续以 `docs/api/openapi.yaml` 与 `docs/api/websocket.md` 为准。

## 技术选型

- **Go**：承载绝大多数业务服务（IO 密集、生态成熟、团队上手快）。
- **Rust**：仅用于对延迟/吞吐极敏感的撮合引擎（matching-engine）。

## 服务划分

### Go 业务服务（`services/go/*`）

| 服务 | 职责 | 对应 API（openapi.yaml tag） |
| --- | --- | --- |
| `gateway` | API 网关：鉴权、限流、路由、聚合、WebSocket 接入 | 全部（统一入口） |
| `account` | 注册/登录/刷新/登出、2FA(TOTP)、账户信息 | Auth、2FA、Account |
| `kyc` | KYC 资料提交与审核状态 | KYC |
| `wallet` | 充值地址、充值记录、提现申请与记录、链上交互 | Wallet |
| `asset` | 资产余额、冻结/解冻、内部划转、估值 | Assets |
| `order` | 现货下单/撤单、当前委托、历史订单、成交查询 | Spot |
| `market-data` | 行情聚合：ticker/depth/kline/trades、交易对列表、WS 推送 | Market |
| `admin` | 运营后台：审核、风控配置、公告、对账 | （内部，不公开） |
| `risk` | 风控：限额、反洗钱、异常行为拦截、熔断 | （内部，旁路/同步校验） |

### Rust 核心服务（`services/rust/*`）

| 服务 | 职责 |
| --- | --- |
| `matching-engine` | 撮合引擎：订单簿维护、价格/时间优先撮合、成交与盘口事件输出。`order` 服务提交订单，撮合结果经事件流回灌至 `order` / `asset` / `market-data`。 |

## 典型调用链

```
client ──HTTP/WS──> gateway ──> account / order / asset / market-data ...
                                   │
                        order ──> matching-engine (Rust) ──事件──> asset / market-data
                                   │
                              risk（同步校验 / 旁路监控）
```

## 约定

- 对外 HTTP 契约以 `docs/api/openapi.yaml` 为单一事实来源（SSOT），可据此生成
  Go / Kotlin / Swift 客户端与服务端 stub。
- 服务间通信建议 gRPC + 事件流（如 Kafka/NATS），撮合事件走低延迟通道。
- 金额一律用字符串/定点数传输，避免浮点精度丢失（与 openapi.yaml 中的 `string` 金额字段一致）。

## 现货链路 · 第一步（已落地：gateway / account / asset）

本阶段用 Go 实现了现货链路的第一步，三个服务各为独立 Go module，共享 `services/go/common`
（JWT、统一错误模型、可选 Redis）：

| 模块 | 路径 | 端口 | 职责 |
| --- | --- | --- | --- |
| `gateway` | `services/go/gateway` | 8080 | 反向代理 + JWT 鉴权中间件 + 公开路由白名单；拦截 `/internal/*` |
| `account` | `services/go/account` | 8081 | 注册、登录（JWT HS256）、刷新/登出、2FA(TOTP) 绑定/启用/关闭、`/account` |
| `asset`   | `services/go/asset`   | 8082 | 资产余额、冻结/解冻、复式记账流水（事务 + 行锁、借贷平衡、余额非负），PostgreSQL |

### 设计要点

- **鉴权**：`account` 登录签发 HS256 access/refresh 令牌（密钥 `JWT_SECRET` 来自环境变量）。
  `gateway` 是统一鉴权入口：校验 Bearer access token，剥离客户端伪造的 `X-User-Id`，再注入
  由令牌 `sub`（即 uid）得到的 `X-User-Id` 转发给上游。`account` 的受保护接口自身也会再次校验
  JWT（纵深防御）；`asset` 仅信任网关注入的 `X-User-Id`，故 **asset 只应部署在网关之后**。
- **2FA**：`/2fa/totp/bind` 生成密钥与 otpauth URI，`/2fa/totp/verify` 校验并启用；启用后
  登录必须携带有效 `totpCode`，否则 401。
- **密码**：bcrypt 哈希存储。
- **复式记账**：每笔操作（credit/freeze/unfreeze）在单个数据库事务内完成——先 `SELECT … FOR UPDATE`
  锁定余额行，再更新 `free`/`locked` 并写入**借贷平衡**的日记账（`ledger_transactions` +
  `ledger_entries`）。余额非负由应用层检查与数据库 `CHECK (free >= 0 / locked >= 0)` 双重保证。
- **初始化资产账户**：注册成功后 `account` 调用 `asset` 的内部接口 `POST /internal/accounts`
  初始化账户并种入默认资产（USDT/BTC/ETH，零余额）；该调用为尽力而为，`asset` 在首次查询余额时
  也会惰性补种，保证健壮性。
- **迁移**：各服务启动时用 goose（内嵌 SQL）自动迁移；两服务共库但使用各自的版本表
  （`goose_account_version` / `goose_asset_version`），避免并发迁移竞争。
- **Redis 为可选依赖**：设置 `REDIS_URL` 则连接并 ping，否则直接跳过。

### 环境变量

复制 `.env.example` 为 `.env` 填写真实值（`.env` 已被 `.gitignore` 忽略，**切勿提交**）。关键变量：
`JWT_SECRET`（gateway 与 account 共享）、`INTERNAL_TOKEN`（保护 asset 的 `/internal/*`）、
`DATABASE_URL`、可选 `REDIS_URL`。

### 本机运行（无需 Docker）

本机没有 Docker，使用 `tools/devstack` 启动器：它用 `embedded-postgres` 拉起**真实 PostgreSQL**、
用 `miniredis` 提供内存 Redis，并编译运行三个服务（首次会从 Maven 下载 postgres 二进制并缓存）。

```bash
# 端到端冒烟：注册 → 登录 → （经网关）查余额；脚本会自动起/停 devstack
bash scripts/smoke.sh

# 手动把整套栈跑起来（Ctrl-C 停止），会打印 GATEWAY_URL 等环境变量
cd tools/devstack && go run . -root ../..
```

### 测试

```bash
# 全部模块 go vet + go test（需要数据库的测试在无 DATABASE_URL 时自动 skip）
bash scripts/test-all.sh

# 连同需要数据库的测试一起跑（账本/并发行锁/2FA/刷新轮换等），在 devstack 的真实 PG 上执行
cd tools/devstack && go run . -root ../.. -exec "bash scripts/test-all.sh"
```

### Docker（环境具备 Docker 时）

根目录 `docker-compose.yml` 定义了 `postgres`、`redis` 与三个服务（各自 Dockerfile，构建上下文为仓库根
以便复制共享的 `common` 模块）：

```bash
cp .env.example .env   # 填入真实密钥
docker compose --env-file .env up --build
```

## 现货链路 · 第二步（已落地：order / matching-engine / 成交结算）

第二步接上现货交易主链：Go 的 `order` 服务负责下单/撤单/查询与资金冻结和成交结算，
Rust 的 `matching-engine` 负责撮合，二者通过**可替换的消息接口**通信。

| 模块 | 路径 | 端口 | 职责 |
| --- | --- | --- | --- |
| `order` | `services/go/order` | 8084 | 下单（限价/市价，GTC/IOC/FOK/PostOnly）、撤单、当前/历史委托、成交查询；按交易对校验；冻结/解冻资金；成交结算（含手续费、幂等） |
| `matching-engine` | `services/rust/matching-engine` | 8083 | 每交易对单线程、内存订单簿、价格/时间优先；WAL（先落盘再处理）+定期快照+启动恢复；输出成交与深度事件 |

### 下单与资金冻结

- 路径与 `docs/api/openapi.yaml` 的 Spot 标签一致：`POST /spot/orders`、`DELETE /spot/orders/{orderId}`、
  `GET /spot/orders/open`、`GET /spot/orders/history`、`GET /spot/trades`，经网关以 `/spot/*` 转发到 `order`。
- **交易对配置**存于 `symbols` 表（迁移内置默认 `BTCUSDT`、`ETHUSDT`），含价格精度、数量精度、
  最小下单数量与最小下单额，下单时据此校验。
- **下单即冻结**（调用 asset 内部接口，撮合前完成，保证不会用不存在的钱成交）：
  买入限价冻结报价币 `price×quantity`；卖出冻结基础币 `quantity`；**市价买按金额**冻结
  `quoteOrderQty`（报价币）；市价卖冻结基础币。
- **leftover 解冻**：订单到终态（filled/canceled/rejected）时，按已成交实际消耗与冻结额之差一次性解冻
  （由 `orders.unfrozen` 保证只解冻一次）。**限价买成交价更优**时多冻结的部分即在此释放；IOC/FOK/撤单
  的未成交剩余同样在此释放。

### 撮合引擎

- 单一 `SymbolEngine` 拥有某交易对的完整内存订单簿，由该交易对唯一的撮合线程驱动，撮合路径**无锁**。
- 价格/时间优先：买盘取最高价、卖盘取最低价，同价位 FIFO。支持限价/市价、GTC/IOC/FOK/PostOnly 与撤单。
- **WAL 先落盘再处理**：每条被接受的命令先 `fsync` 到 `DATA_DIR/<symbol>/wal.log` 再应用；按
  `SNAPSHOT_EVERY` 周期写快照并截断 WAL；启动时加载快照并重放 WAL 尾部，精确复现崩溃前状态（含 tradeId）。
- 输出成交（Trade）与深度（Depth）事件。价格/数量在引擎内用定点 `i128` 整数运算，杜绝浮点误差。

### 可替换的服务间消息接口

- 事件输出经 `EventSink` trait 抽象：本地默认 `LogSink`/`HttpSink`（HTTP 内存 pub/sub stand-in），
  生产用 `KafkaSink`（`--features kafka` 的接口骨架，标注 rdkafka 接入点，不需要 broker 即可编译）。
- 命令通道：本地实现为一个最小阻塞 HTTP 服务（`POST /engine/commands`、`GET /engine/depth`），
  每交易对一线程 + channel，保证单线程撮合；`order` 侧的 `Matching` 接口（`HTTPMatching`）即对应实现，
  生产可换成从 Kafka 命令主题消费而不动调用点。

### 成交结算（复式记账 + 幂等）

- 每笔成交由 `order` 调用 asset 的 `POST /internal/settle`，在**单个数据库事务**内完成复式记账：
  买方锁定的报价币划给卖方、卖方锁定的基础币划给买方，maker/taker 手续费分别计入**平台手续费账户**。
- **幂等**：asset 侧 `settlements(trade_id)` 主键 + `order` 侧 `trades(trade_id, order_id)` 唯一约束，
  同一 tradeId 重复投递为无操作。
- **手续费可配置**（见下）：现货 maker/taker 默认各 0.1%，买方费从买到的基础币中扣、卖方费从卖得的报价币中扣。

### 手续费配置

所有费率均可配置（环境变量 / docker-compose），**不区分 VIP 等级**：

| 产品 | 环境变量 | 默认 | 说明 |
| --- | --- | --- | --- |
| 现货 maker | `SPOT_MAKER_FEE_RATE` | `0.001` | 0.1% |
| 现货 taker | `SPOT_TAKER_FEE_RATE` | `0.001` | 0.1% |
| 合约 maker | `CONTRACT_MAKER_FEE_RATE` | `0.0005` | 0.05%，开/平仓各收一次、按仓位价值（合约服务未落地，费率位已预留） |
| 合约 taker | `CONTRACT_TAKER_FEE_RATE` | `0.0005` | 0.05%，同上 |

> **提现费用**：提现不收平台手续费，仅收链上 Gas。`docs/api/openapi.yaml` 已预留
> `GET /wallet/withdraw/networks`（返回 `networks[].gasFeeEstimate` 等 Gas 估算）与 `Withdrawal.gasFee` 字段；
> 钱包服务尚未实现，此处仅为契约与配置占位。

### 本机运行（无需 Docker）

`tools/devstack` 现额外编译并拉起 **Rust 撮合引擎**（`cargo build`，WAL/快照写入临时目录，退出即清理）与
`order` 服务，启动顺序 asset → engine → order → account → gateway，并把 `ORDER_URL`、`ENGINE_URL`、
`INTERNAL_TOKEN`（仅本地开发用的固定令牌）导出给 `-exec` 的命令使用。

```bash
# 现货交易端到端冒烟：两用户注册登录 → 内部入账充值 → 一卖一买限价成交 →
# 用十进制精确校验双方余额与平台手续费账户
bash scripts/smoke-trade.sh

# 原第一步冒烟（注册 → 登录 → 查余额）仍然可用
bash scripts/smoke.sh
```

### 测试与基准

```bash
# Go 全模块 go vet + go test（含 order；需要 DB 的测试在无 DATABASE_URL 时自动 skip）
bash scripts/test-all.sh
# 连同需要 DB 的测试（asset 结算幂等 settle_test、order store 集成测试）在 devstack 真实 PG 上执行
cd tools/devstack && go run . -root ../.. -exec "bash scripts/test-all.sh"

# 撮合引擎单元 + 集成测试（价格时间优先、部分成交、各 TIF、撤单、WAL 恢复一致性）
cd services/rust/matching-engine && cargo test

# release 性能基准（输出每秒订单数、延迟 p50/p99/p999）
cd services/rust/matching-engine && cargo run --release --bin bench
```

### Docker

`docker-compose.yml` 增加了 `matching-engine`（Rust 镜像，WAL/快照挂载 `enginedata` 卷）与 `order`
服务，网关通过 `ORDER_URL` 暴露 `/spot/*`。

## 现货链路 · 第三步（已落地：market-data / 行情服务）

第三步接上行情链路：Go 的 `market-data` 服务消费撮合引擎输出的成交/深度事件，聚合各周期 K 线、
订单簿深度、24h ticker 与近期成交，提供 REST 查询与 WebSocket 推送。

| 模块 | 路径 | 端口 | 职责 |
| --- | --- | --- | --- |
| `market-data` | `services/go/market-data` | 8085 | 消费引擎成交/深度事件 → K 线/深度/ticker/成交聚合；REST（`/market/*`）+ WebSocket（`/ws`）；K 线落 PostgreSQL、重启可恢复 |

### 消费引擎事件（可替换消息接口，消费侧）

- 撮合引擎通过 `EventSink` trait 输出 `Trade`/`Depth` 事件（见第二步）。本服务是其**消费侧**的本地实现：
  引擎的 `HttpSink` 将每条事件以 JSON POST 到 `EVENT_SINK_URL`，由 `market-data` 的 `POST /internal/events`
  摄取（`Sink` 接口 + `HTTPEventSource` 本地实现）。生产环境可换成从 Kafka 同一事件流消费、调用同一 `Sink`，
  无需改动聚合逻辑。事件为尽力而为（引擎不重试、结算不依赖此路径），并按单调 `tradeId`/`sequence` 去重。
- 时序保证：引擎在 `process()` 内**同步**发布事件（阻塞到消费方返回），故订单接口返回时行情状态已更新。

### 聚合与查询

- **K 线**：支持 `1m/5m/15m/30m/1h/4h/1d/1w`（openapi `KlineInterval` 的超集，额外含 `30m`）。按成交时间分桶，
  同桶更新、跨桶收线；`1d` 对齐 UTC 零点、`1w` 对齐周一零点。
- **深度**：从引擎深度增量重建聚合盘口（量为 0 即删除该价位），REST 返回按价排序的买/卖盘与 `lastUpdateId`。
- **24h ticker**：滚动 24h 窗口计算最高/最低/成交量/成交额与涨跌幅，`lastPrice` 取最新成交价。
- **近期成交**：每交易对保留最近成交环形缓冲。
- **交易对列表**：读取 `order` 维护的共享 `symbols` 表（只读）。

### REST（与 openapi.yaml 一致，公开）

经网关 `/market/*`（公开路由，无需鉴权）：`GET /market/symbols`、`GET /market/ticker`、
`GET /market/depth`、`GET /market/klines`，以及与 WS `trades` 频道同形的 `GET /market/trades`（近期成交）。

### WebSocket（按 docs/api/websocket.md，公开）

经网关 `/ws`（`httputil.ReverseProxy` 透明转发 Upgrade）。标准库手写最小 RFC 6455 服务端，支持：
`subscribe`/`unsubscribe`（公开频道 `ticker:`、`depth:`、`kline:<sym>:<interval>`、`trades:`）、
订阅即下发首帧快照、心跳（应用层 `ping`/`pong` 双向 + 60s 空闲断开）。用户频道（orders/balances）由账户/订单服务提供，本服务拒绝。

### K 线持久化与重启恢复

- 每笔成交以 `(symbol, interval, open_time)` 为主键**异步 upsert** K 线（含进行中的当前 K 线），goose 迁移建表
  （独立版本表 `goose_market_version`）。启动时按交易对/周期回载最近 K 线到内存，REST/WS 立即可用；深度另从引擎
  `GET /engine/depth` 拉取全量快照以尽快恢复盘口。

### 本机运行与冒烟

```bash
# 行情端到端冒烟：两用户成交 → 引擎事件 → WebSocket 客户端(tools/wsprobe)收到
# ticker/depth/kline/trades 推送 → REST 精确校验 ticker/K线/深度/成交
bash scripts/smoke-market.sh

# 第一、二步冒烟仍可用
bash scripts/smoke.sh
bash scripts/smoke-trade.sh
```

`tools/devstack` 现额外编译并拉起 `market-data`，并给引擎注入 `EVENT_SINK_URL` 指向其 `/internal/events`；
`tools/wsprobe` 是一个零依赖的最小 WebSocket 客户端，供冒烟脚本验证推送。`scripts/test-all.sh` 已含 `market-data`
模块（含 K 线持久化/恢复的 DB 测试，在无 `DATABASE_URL` 时自动 skip）。

> 其余子目录（kyc/wallet/admin/risk）仍为占位，后续落地。
