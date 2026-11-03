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

> 其余子目录（kyc/wallet/order/market-data/admin/risk、Rust 撮合引擎）仍为 `.gitkeep` 占位，后续落地。
