# Linbit WebSocket API

实时行情与用户数据推送协议。本文件与 `openapi.yaml` 配套，字段命名保持一致，便于生成
各端（TypeScript / Kotlin / Swift）客户端。

## 连接

WebSocket 经 gateway 的 `/ws` 路由转发到 market-data 服务，与 REST 同一入口。

| 环境 | 地址 |
| --- | --- |
| 本地（devstack / gateway） | `ws://localhost:8080/ws` |
| 生产 | `wss://<host>/ws` |
| 本地 mock（占位，Prism 不提供 WS） | `ws://localhost:4010/ws` |

- 单条连接可订阅多个频道。
- 公开行情频道无需鉴权；用户频道（`orders`、`balances`）需在订阅时携带有效 `accessToken`。
- 所有消息均为 JSON 文本帧。

## 消息信封

客户端 → 服务端（请求）：

```json
{
  "id": "client-generated-id",
  "op": "subscribe",
  "args": ["ticker:BTCUSDT"]
}
```

- `op`：`subscribe` | `unsubscribe` | `auth` | `ping`
- `args`：频道字符串数组，格式为 `<channel>:<symbol>`（用户频道无 symbol）
- `id`：可选，回执中原样返回，便于关联请求

服务端 → 客户端（回执 / 推送）：

```json
{
  "id": "client-generated-id",
  "event": "subscribed",
  "channel": "ticker:BTCUSDT"
}
```

- 推送数据帧：`{ "channel": "...", "ts": 1762070400123, "data": { ... } }`
- 错误帧：`{ "event": "error", "code": "SUB_INVALID", "message": "..." }`，错误码与
  REST 的 `Error.code` 共用同一套命名。

## 鉴权

订阅用户频道前先发送 `auth`：

```json
{ "op": "auth", "args": ["eyJhbGciOiJIUzI1Ni...accessToken"] }
```

成功回执：

```json
{ "event": "authed", "uid": "80000001" }
```

失败返回 `{ "event": "error", "code": "AUTH_UNAUTHORIZED", "message": "令牌无效" }`。

## 心跳

- 客户端每 20s 发送 `{ "op": "ping" }`，服务端回 `{ "event": "pong", "ts": 1762070400123 }`。
- 服务端也会周期下发 `{ "event": "ping" }`，客户端应回 `{ "op": "pong" }`。
- 60s 内无任何帧则连接被关闭，客户端需重连并重新订阅。

## 行情频道（公开）

### `ticker:<symbol>` — 24h 行情快照

```json
{
  "channel": "ticker:BTCUSDT",
  "ts": 1762070400123,
  "data": {
    "symbol": "BTCUSDT",
    "lastPrice": "76771.90",
    "priceChangePercent": "-1.71",
    "high": "78878.00",
    "low": "76705.00",
    "volume": "371.65",
    "quoteVolume": "28915681.30"
  }
}
```

### `depth:<symbol>` — 订单簿增量

```json
{
  "channel": "depth:BTCUSDT",
  "ts": 1762070400123,
  "data": {
    "symbol": "BTCUSDT",
    "lastUpdateId": 123456790,
    "bids": [["76768.40", "0.51230"], ["76768.30", "0.00000"]],
    "asks": [["76768.50", "0.33200"]]
  }
}
```

- 数量为 `"0"` 表示该价位被移除。
- 首帧为全量快照（`lastUpdateId` 基准），后续为增量。

### `kline:<symbol>:<interval>` — K 线

`interval` 取值：`1m,5m,15m,30m,1h,4h,1d,1w`（与服务端实现一致）。

```json
{
  "channel": "kline:BTCUSDT:15m",
  "ts": 1762070400123,
  "data": {
    "time": 1762070400,
    "open": "76500.00",
    "high": "76820.00",
    "low": "76410.00",
    "close": "76768.40",
    "volume": "123.45",
    "closed": false
  }
}
```

- `closed=false` 表示该 K 线仍在更新，`true` 表示已收线。

### `trades:<symbol>` — 实时成交

```json
{
  "channel": "trades:BTCUSDT",
  "ts": 1762070400123,
  "data": {
    "tradeId": "5000000123",
    "price": "76768.40",
    "quantity": "0.01230",
    "side": "buy",
    "time": 1762070400120
  }
}
```

### `mark:<symbol>` — 合约标记价格（USDT 本位永续）

由合约服务每个风控 tick（约 1s）推送，订阅时先回放最近一帧。`<symbol>` 为合约代码（如 `BTCUSDT`，见 `GET /futures/market/contracts`）。标记价格不可用（指数断流）时停止推送。

```json
{"channel":"mark:BTCUSDT","ts":1767225600000,"data":{"symbol":"BTCUSDT","markPrice":"67000.1","indexPrice":"67001.5","fundingRate":"0.0001","nextFundingTime":1767254400000}}
```

### `index:<symbol>` — 合约指数价格（`mark:<symbol>` 别名）

与 `mark:<symbol>` 同一帧数据（含 `indexPrice`），仅频道名不同；同频率推送、订阅时回放最近一帧。

```json
{"channel":"index:BTCUSDT","ts":1767225600000,"data":{"symbol":"BTCUSDT","markPrice":"67000.1","indexPrice":"67001.5","fundingRate":"0.0001","nextFundingTime":1767254400000}}
```

### `futures-ticker:<symbol>` — 合约 24h 行情

每个风控 tick（约 1s）推送，订阅时回放最近一帧。`data` 与 `GET /futures/market/ticker?symbol=` 返回对象完全一致（含 `lastPrice`/`priceSource`/`markPrice`/`indexPrice`/`fundingRate`/`nextFundingTime`/24h 统计/`openInterest` 及现货兼容别名 `high`/`low`/`volume`/`quoteVolume`/`priceChangePercent`）。24h 统计与持仓量最多缓存 5s，价格字段每帧实时。

`lastPrice` 取值顺序：最近 5s 内的成交价（`priceSource: "trade"`，`FUTURES_LAST_TRADE_FRESH_SECONDS`）→ 盘口中间价（`mid`）→ 指数价（`index`）→ 标记价（`mark`）。REST 同此规则。

```json
{"channel":"futures-ticker:BTCUSDT","ts":1767225600000,"data":{"symbol":"BTCUSDT","lastPrice":"67001.5","priceSource":"index","markPrice":"67000.1","indexPrice":"67001.5","fundingRate":"0.0001","lastFundingRate":"0.0001","nextFundingTime":1767254400000,"openPrice24h":"66000","highPrice24h":"67500","lowPrice24h":"65800","volume24h":"12.5","turnover24h":"832000.12","priceChangePercent24h":"0.015174","openInterest":"3.2","priceAvailable":true,"high":"67500","low":"65800","quoteVolume":"832000.12","volume":"12.5","priceChangePercent":"1.52"}}
```

### `futures-kline:<symbol>:<interval>` — 合约 K 线（当前未收盘根）

`interval`：`1m` `5m` `15m` `30m` `1h` `4h` `1d` `1w`（小写）。每个风控 tick 推送当前根，订阅时回放最近一帧。规则同 `GET /futures/market/klines`：该周期内有合约成交则为成交 K 线（含 `volume`/`turnover`），否则为指数 K 线（`source: "index"`，量为 0）。`time`/`openTime` 为开盘时间（毫秒）；`closed` 恒为 `false`，出现新的 `time` 即表示上一根已收盘。合约服务重启后的第一根只覆盖重启后的价格，客户端应与 REST 当前根合并 high/low。

```json
{"channel":"futures-kline:BTCUSDT:1m","ts":1767225600000,"data":{"symbol":"BTCUSDT","interval":"1m","time":1767225600000,"openTime":1767225600000,"open":"67000","high":"67010.5","low":"66990","close":"67001.5","volume":"0","turnover":"0","closed":false,"source":"index"}}
```

## 用户频道（需鉴权）

### `orders` — 订单更新

```json
{
  "channel": "orders",
  "ts": 1762070400123,
  "data": {
    "orderId": "1000000001",
    "clientOrderId": "cli-20261102-001",
    "symbol": "BTCUSDT",
    "side": "buy",
    "type": "limit",
    "price": "76768.40",
    "quantity": "0.01000000",
    "filledQuantity": "0.00500000",
    "status": "partially_filled",
    "createdAt": "2026-11-02T10:10:00Z"
  }
}
```

### `balances` — 余额变动

```json
{
  "channel": "balances",
  "ts": 1762070400123,
  "data": {
    "asset": "USDT",
    "free": "10520.33000000",
    "locked": "250.00000000",
    "total": "10770.33000000"
  }
}
```

## 完整会话示例

```text
→ { "op": "ping" }
← { "event": "pong", "ts": 1762070400001 }
→ { "id": "1", "op": "subscribe", "args": ["ticker:BTCUSDT", "kline:BTCUSDT:15m"] }
← { "id": "1", "event": "subscribed", "channel": "ticker:BTCUSDT" }
← { "id": "1", "event": "subscribed", "channel": "kline:BTCUSDT:15m" }
← { "channel": "ticker:BTCUSDT", "ts": 1762070400123, "data": { ... } }
→ { "op": "auth", "args": ["...accessToken"] }
← { "event": "authed", "uid": "80000001" }
→ { "id": "2", "op": "subscribe", "args": ["orders", "balances"] }
← { "id": "2", "event": "subscribed", "channel": "orders" }
← { "channel": "orders", "ts": 1762070401000, "data": { ... } }
→ { "id": "3", "op": "unsubscribe", "args": ["ticker:BTCUSDT"] }
← { "id": "3", "event": "unsubscribed", "channel": "ticker:BTCUSDT" }
```
