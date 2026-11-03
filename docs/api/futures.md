# Futures API: USDT-margined perpetuals (services/go/futures, :8089)

Gateway: `/futures/market/*` is public. The rest of `/futures/*` needs a Bearer token, and the gateway injects `X-User-Id`. `/internal/*` is not exposed through the gateway.

## Contracts (seeded in migrations/00001_init.sql, per the UI on feat/ui-p0-flows)

| Symbol | Max leverage | Tick | Min qty | Max qty |
|---|---|---|---|---|
| BTCUSDT | 200 | 0.1 | 0.0001 | 100 |
| ETHUSDT | 200 | 0.01 | 0.001 | 2000 |
| BNBUSDT | 100 | 0.01 | 0.01 | 5000 |
| SOLUSDT | 100 | 0.01 | 0.01 | 50000 |
| XRPUSDT | 100 | 0.0001 | 1 | 2000000 |
| DOGEUSDT | 100 | 0.00001 | 10 | 20000000 |
| ARBUSDT | 75 | 0.0001 | 1 | 2000000 |
| TONUSDT | 75 | 0.001 | 0.1 | 500000 |
| LINKUSDT | 75 | 0.001 | 0.1 | 200000 |
| AVAXUSDT | 75 | 0.001 | 0.1 | 200000 |

- Leverage runs from 1 to the max leverage. Margin mode is cross or isolated. Position mode is one-way (net).
- Maker and taker fees are both 0.05% (0.0005), charged on open and on close against the fill value (price × qty). Fees go to `system:fee`.
- The maintenance margin rate is 0.4% for every pair.

## Prices

- **Index:** the latest spot price from the market service. If there is no price, or it is older than `FUTURES_INDEX_STALE_SECONDS`, the service fails closed: new orders return `MARK_PRICE_UNAVAILABLE`, and liquidations and TP/SL pause.
- **Mark:** the median of three values:
  - index × (1 + last funding rate × time to next funding / funding interval)
  - index
  - last futures trade price
- **Funding:** settled every `FUTURES_FUNDING_HOURS` hours at 00/08/16 UTC.
  - rate = premium + clamp(interest − premium, ±clamp), where premium is the average of (mark − index)/index over the period. The result is capped at ±cap.
  - A positive rate means longs pay shorts. The amount is position value × rate. Settlement is idempotent per symbol and time.

## Margin and risk

- **Cross:**
  - available = balance − order margin − Σ IM + min(0, Σ uPnL)
  - Liquidation happens when balance + Σ uPnL ≤ Σ MM, where MM = |qty| × mark × MMR.
- **Isolated:**
  - Liquidation happens when isolated margin + uPnL ≤ MM.
  - Adding margin is always allowed. Removing it must leave margin + min(0, uPnL) ≥ IM at the mark price.
- **Liquidation:**
  - The position is taken over by `system:insurance` at the mark price.
  - The liquidation fee is min(remaining equity, value × liquidation fee rate) and goes to the insurance fund.
  - Any loss beyond the remaining equity is covered by the insurance fund. If the fund goes negative, the service logs an ALERT.
  - ADL and position tiers are not implemented yet.
- **TP/SL:**
  - You can attach TP/SL to an order, or set it on a position.
  - When the mark price crosses the trigger, a reduce-only market close is sent. If there is no liquidity, it retries every `FUTURES_TPSL_RETRY_SECONDS`.
- **Market order protection:** fills are only allowed within mark × (1 ± `FUTURES_MARKET_PRICE_BAND`). Any remainder is cancelled.

## Endpoints

### Public

`GET /futures/market/` followed by:
- `contracts`
- `ticker?symbol=`
- `depth?symbol=`
- `trades?symbol=`
- `klines?symbol=&interval=`
- `funding-history?symbol=`
- `mark-history?symbol=`
- `insurance-fund`

### Private

- `GET /futures/account`
- `POST /futures/transfer {direction:"in"|"out", amount}` (spot ↔ futures, USDT)
- `GET /futures/transfers`
- `GET|POST /futures/leverage {symbol, leverage, marginMode}`
  - Returns `HAS_OPEN_ORDERS` or `HAS_POSITION` when a mode change is blocked.
- `POST /futures/orders {symbol, side, type:"limit"|"market", timeInForce:"GTC"|"IOC", price, qty, reduceOnly, tpPrice, slPrice}`
- `GET /futures/orders?symbol=&status=open`
- `DELETE /futures/orders/{id}`
- `GET /futures/positions`
- `POST /futures/positions/{symbol}/tpsl {tpPrice, slPrice}`
  - Send "" to clear a value.
- `POST /futures/positions/{symbol}/margin {amount}`
  - Positive adds margin, negative removes it.
- `POST /futures/positions/{symbol}/close`
- `GET /futures/bills?kind=`
  - `kind` is one of `transfer_in`, `transfer_out`, `fee`, `realized_pnl`, `funding`, `liquidation_fee`, `insurance_cover`.
- `GET /futures/trades`

### Dev only

These need `DEV_MODE=true` and the `X-Internal-Token` header.

- `POST /internal/dev/index {symbol, price}`
- `POST /internal/dev/tick`
- `POST /internal/dev/funding {symbol, rate}`
- `GET /internal/dev/audit` (checks that Σ equity = net transfers)

## Configurable defaults (environment variables)

| Variable | Default | Meaning |
|---|---|---|
| PORT | 8089 | Port |
| FUTURES_DEFAULT_LEVERAGE | 20 | Default leverage for new pairs |
| FUTURES_DEFAULT_MARGIN_MODE | cross | Default margin mode |
| FUTURES_MARKET_PRICE_BAND | 0.05 | Market order protection band |
| FUTURES_TICK_MS | 1000 | Risk / price loop interval |
| FUTURES_MARK_SAMPLE_SECONDS | 60 | Mark price sampling interval |
| FUTURES_INDEX_STALE_SECONDS | 30 | Index staleness threshold (fail closed) |
| FUTURES_FUNDING_HOURS | 8 | Funding interval |
| FUTURES_FUNDING_INTEREST | 0.0001 | Interest rate per period |
| FUTURES_FUNDING_CLAMP | 0.0005 | Clamp |
| FUTURES_FUNDING_CAP | 0.0075 | Rate cap per period |
| FUTURES_LIQUIDATION_FEE_RATE | (empty = contract MMR) | Liquidation fee rate |
| FUTURES_MIN_TRANSFER | 0.01 | Minimum transfer |
| FUTURES_TPSL_RETRY_SECONDS | 5 | TP/SL retry interval when there is no liquidity |
| FUTURES_TRANSFER_RETRY_SECONDS | 5 | Transfer outbox retry interval |

The insurance fund starts at 0. Fee income accumulates in `system:fee` and is not swept automatically.
