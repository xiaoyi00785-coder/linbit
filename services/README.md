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

> 各子目录当前仅含 `.gitkeep` 占位，实现代码落地后替换为对应的 Go module / Rust crate。
