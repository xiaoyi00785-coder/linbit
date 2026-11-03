# Strategy API: spot grid bots (services/go/strategy, :8090)

Spot only (no DCA / recurring buys). Every order goes through the order service (`POST /spot/orders` as the user), so balances, fees and matching are the same as for manual orders. The gateway proxies `/strategy/*`. `/strategy/config` and `/strategy/grid/square` are public; everything else needs a user token.

## Grid model

- `mode`: `arithmetic` (equal price steps) or `geometric` (equal ratio). Levels L0…Ln between `lowerPrice` and `upperPrice`, `gridCount` = n. Prices are rounded to the symbol's tick size.
- Per-grid quantity q = investment / (Σ buy levels below P + count of sell levels above P × P × (1 + `GRID_BUY_BUFFER`)), floored to the step size. Every order must meet minQty / minNotional, otherwise `GRID_TOO_DENSE` / `INVESTMENT_TOO_LOW`.
- Start: one market buy for the base needed by the sell side, then limit sells on levels above P and limit buys below P (the level nearest P is left empty).
- Fill handling (poll loop, `GRID_POLL_MS`): buy filled at Li → sell at Li+1; sell filled at Lj → buy at Lj−1. Each completed sell books grid profit = sell proceeds − cost of the matching buy − fees.
- Stop: `stopLossPrice` (last ≤ SL) / `takeProfitPrice` (last ≥ TP), manual stop, external cancel of a bot order (`order_canceled`), or a failed placement (`place_failed`). Stopping cancels open orders, books partial fills, and if `sellBaseOnStop` (default `GRID_SELL_ON_STOP`) market-sells the base. It is resumable (`stopping` state) after a restart. If a start fails before any order was placed, the bot is discarded.
- Stats: value, gridProfit, unrealizedPnl, totalProfit, profitRate, gridApr, cycles, runtimeSeconds, maxDrawdown, maxDrawdown7d, fees. Snapshots every `GRID_SNAPSHOT_SECONDS` feed the drawdown and the curve.

## Endpoints

| Method | Path | Auth |
|---|---|---|
| GET | /strategy/config | public (limits and defaults) |
| GET | /strategy/grid/square?sort=default\|roi\|runtime&page=&pageSize= | public (running bots, anonymized, runtime ≥ `GRID_SQUARE_MIN_RUN_SECONDS`) |
| GET | /strategy/grid/preview?symbol=&mode=&lowerPrice=&upperPrice=&gridCount=&investment= | user |
| POST | /strategy/grid `{symbol, mode, lowerPrice, upperPrice, gridCount, investment, stopLossPrice?, takeProfitPrice?, sellBaseOnStop?}` | user |
| GET | /strategy/grid?status=running\|stopped\|all | user |
| GET | /strategy/grid/{id} | user (404 for other users) |
| GET | /strategy/grid/{id}/orders?status= | user |
| POST | /strategy/grid/{id}/stop `{sellBase?}` | user |
| POST | /internal/dev/grid/tick | dev (DEV_MODE + internal token) |

Order service endpoints used internally (X-Internal-Token): `GET /internal/orders/{id}?userId=`, `GET /internal/symbols/{symbol}`.

Errors: INVALID_MODE, SYMBOL_NOT_FOUND, SYMBOL_NOT_TRADING, INVALID_RANGE, INVALID_GRID_COUNT, INVALID_INVESTMENT, INVESTMENT_TOO_LOW, GRID_TOO_DENSE, NO_PRICE, PRICE_OUT_OF_RANGE, INVALID_STOP_LOSS, INVALID_TAKE_PROFIT, TOO_MANY_BOTS, INSUFFICIENT_BALANCE, ORDER_REJECTED, NO_LIQUIDITY, BOT_NOT_FOUND (404), BOT_NOT_RUNNING.

## Configurable defaults (environment variables)

| Variable | Default | Meaning |
|---|---|---|
| PORT | 8090 | Port |
| GRID_POLL_MS | 1000 | Order / price poll interval |
| GRID_MAX_BOTS_PER_USER | 10 | Running bots per user |
| GRID_MIN_GRIDS / GRID_MAX_GRIDS | 2 / 200 | Grid count range |
| GRID_MIN_INVESTMENT | 10 | Minimum investment (quote) |
| GRID_FEE_RATE | 0.001 | Fee used for previews only (real fees come from fills) |
| GRID_BUY_BUFFER | 0.003 | Slippage buffer for the initial market buy |
| GRID_MAX_RANGE_RATIO | 100 | Max upper/lower ratio |
| GRID_SNAPSHOT_SECONDS | 60 | Snapshot interval |
| GRID_SQUARE_MIN_RUN_SECONDS | 3600 | Min runtime to appear on the square |
| GRID_SELL_ON_STOP | true | Default for sellBaseOnStop |

Not implemented: copying a grid from the square, and per-bot copier counts.

## 策略广场一键复制

- `GET /strategy/grid/square` 每项新增 `copyCount`（被复制次数）；`sort` 新增 `copies`（按复制数降序）。
- `POST /strategy/grid/square/{id}/copy`（需登录）`{"investment":"300","stopLossPrice"?,"takeProfitPrice"?,"sellBaseOnStop"?}`
  - 复制源策略的交易对、模式、价格区间与网格数，投入金额与止盈止损由复制者设置；返回新策略详情及 `copiedFrom`。
  - 只能复制广场上展示的策略（运行中且运行时长 ≥ 广场门槛），否则 404；复制自己的策略返回 `SELF_COPY`。
  - 其它校验（余额、最多运行数、价格区间）与创建网格一致。成功后源策略 `copyCount` +1。
