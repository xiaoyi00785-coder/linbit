# Paper trading + price alerts (P0)

独立模拟现货账本与后台一次性价格预警。**不部署即为关闭**；`/app/features` 的 `paperTrading`、`priceAlerts` 默认均为 `false`，服务缺失 feature row 或 DB 错误时 fail closed。

- Bearer 主账户登录；不信任客户端提供的 `X-User-Id`，禁止 API-key/sub-account session。
- `paper_*` 独立表，懒创建 10000 USDT；market only、仅新鲜外部 INDEX，全成/全拒、无 fee/slippage。
- 每 UTC 日一次 reset，确认字 `RESET_PAPER_ACCOUNT` 与 `Idempotency-Key`；DB 保留 24h 原响应。旧 generation 保留审计，未指定 generation 的列表只读当前账期。
- 价格预警：spot INDEX、gte/lte、once、evaluation epoch、独立 300s 冷却、durable outbox；只通过 account 写站内信，唯一 `(user_id,dedup_key)` 防重复，不发 email/APNs/FCM。
- 通过受限的 `GET /internal/index` 读取 INDEX；响应 `source:"INDEX"` 与 `provider` 分别标明逻辑源与实际 venue，绝不接受 LAST/local fallback。index stale/missing 拒单并暂停推进触发 baseline。
- 不调用真实 balances/ledger/order/matching，不支持充值/提现/互转/合约。

## Migration / release prerequisites

服务 `migrations/00001_paper_price_alerts.sql` 只创建新的 paper/alert 表；account 的 `migrations/00013_notification_dedup.sql` 增加 notification 幂等键。**必须在打开 feature 之前同时部署这两个 migration**（paper auto-migrates its schema；paper 自身使用独立 `goose_paper_version`）。
生产 compose 保留 `PAPER_TRADING_ENABLED:"false"` 与 `PRICE_ALERTS_ENABLED:"false"` 直接 kill switches。应由 ops 先跑 migration，再经 app features（或显式替换 kill switch）开启；当前 PR 不部署、不启用、不合 main。

## Local checks

普通 `go test ./...` 在无 `DATABASE_URL` 时跳过 DB 集成；使用临时 PostgreSQL，`DATABASE_URL=... go test -count=1 ./...` 覆盖资金隔离、reset replay、stale INDEX、alert once/revision、通知 outbox 和 feature-disabled 403。`tools/devstack` 可以整条 gateway 启动服务，dev 本地新增 feature 后走全部 11 端点。
