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
  - Liquidation happens when balance + Σ uPnL ≤ Σ MM (tiered MM, see below).
- **Isolated:**
  - Liquidation happens when isolated margin + uPnL ≤ MM.
  - Adding margin is always allowed. Removing it must leave margin + min(0, uPnL) ≥ IM at the mark price.
- **Liquidation:**
  - The position is taken over by `system:insurance` at the mark price.
  - The liquidation fee is min(remaining equity, value × liquidation fee rate) and goes to the insurance fund.
  - Any loss beyond the remaining equity is covered by the insurance fund. If the fund goes negative, the service logs an ALERT.
- **Tiered maintenance margin** (`fut_risk_tiers`, migration 00002; returned as `riskTiers` on each contract):
  - MM = notional × mmr(tier) − maintenanceAmount(tier), where the tier is picked by the position's notional at mark. maintenanceAmount keeps MM continuous across tiers.
  - Each tier has a max leverage. An order (non reduce-only) is rejected with `POSITION_LIMIT` when |position + same-side open orders + order| × price exceeds the largest notional allowed at the current leverage. Raising leverage on an open position is rejected the same way.
  - Liquidation prices use the tier at the current notional.
  - Seeded brackets (USDT notional / MMR / max leverage):
    - 200x contracts (BTC, ETH): ≤50k 0.4% 200x; ≤500k 0.5% 100x; ≤2M 1% 50x; ≤10M 2.5% 20x; ≤50M 5% 10x; ≤200M 10% 5x.
    - 100x contracts (BNB, SOL, XRP, DOGE): ≤20k 0.4% 100x; ≤200k 1% 50x; ≤1M 2.5% 20x; ≤5M 5% 10x; ≤20M 10% 5x.
    - 75x contracts (ARB, TON, LINK, AVAX): ≤10k 0.4% 75x; ≤100k 1% 50x; ≤500k 2.5% 20x; ≤2M 5% 10x; ≤10M 10% 5x.
- **ADL (auto-deleveraging)**, `FUTURES_ADL_ENABLED=true`:
  - Triggered when a liquidated position is bankrupt (equity < 0 at mark) and the deficit is larger than the insurance fund balance.
  - The uncovered part is pushed into the close price: the position is closed at the bankruptcy price (mark shifted by uncovered / |qty|; for cross, the deficit is split across positions by notional) against opposing user positions, in order of score = ROE × leverage (in profit) or ROE ÷ leverage (in loss).
  - Counterparties get a filled reduce-only order with `source="adl"` (no fee); remaining reduce-only orders are cancelled if their position is gone. Rows go to `fut_adl`; `recentAdl` is shown on `/futures/market/insurance-fund`.
  - Any quantity with no opposing user position falls back to the insurance fund.
  - Positions carry `adlRank` 1–5 (5 = first in the queue).
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

## Copy trading (futures only)

- **Trader**: any user with futures equity ≥ `FUTURES_COPY_TRADER_MIN_EQUITY` can apply with a profit share in **0–50%** (`profitShare` "0.2" or "20%"). Status `pending → active → closed`. Closing stops every follow.
- **Follow**: `amount` moves from the follower's futures wallet into an isolated sub-account `copy:<followId>` (bill `copy_transfer_out/in`). Modes:
  - `ratio` (default 1): mirror qty = trader Δqty × ratio × follow equity / trader equity.
  - `fixed`: each open uses `fixedMargin` × leverage / price.
  - `maxPerTrade` caps margin per open; opens are also capped by what the sub-account can afford, and floored to the contract step (skips are logged with a reason).
  - `tpRatio` / `slRatio` are ROE ratios turned into TP/SL prices `entry·(1±ratio/leverage)` on each mirrored position.
- **Sync**: every trader fill that changes a position writes `fut_copy_events`; followers mirror opens/adds, proportional reduces and full closes/flips with market orders. Leverage and margin mode are copied when the follower position is flat.
- **Unfollow**: closes mirrored positions, settles any outstanding share, and returns the remaining balance.
- **Profit share**: settled **daily** at `FUTURES_COPY_SETTLE_HOUR_UTC` per follow with a high-water mark: share = max(0, net realized − HWM) × rate, where net realized = realized PnL + fees + funding + liquidation items. Losses are never charged and must be earned back first. Paid to the trader's **spot** account (`fut_transfers.kind = copy_profit_share`). Idempotent per (follow, day). **Profit share is not rebate-eligible**: it is a transfer, not a fee, so it is never part of any fee/rebate base.
- **Stats** (`days` = 7/30/90): pnl, roi, winRate, closedTrades, maxDrawdown, followers, aum, followerProfit, profitShareEarned, composite score and an equity curve.

Endpoints:

| Method | Path | Auth |
|---|---|---|
| GET | /futures/copy/summary | public |
| GET | /futures/copy/traders?sort=composite\|roi\|followers\|winrate\|aum&days= | public |
| GET | /futures/copy/traders/{id}?days= | public |
| GET / POST / PATCH | /futures/copy/trader | user (my trader profile / apply / update share, bio) |
| POST | /futures/copy/trader/close | user |
| GET | /futures/copy/trader/followers | user |
| GET / POST | /futures/copy/follows (?status=all) | user |
| PATCH / DELETE | /futures/copy/follows/{id} | user (update params / unfollow) |
| GET | /futures/copy/follows/{id}/trades | user (mirrored trades + settlements) |
| POST | /internal/copy/traders/{id}/status {status} | internal token |
| POST | /internal/dev/copy/settle {period} | dev |

Errors: INVALID_PROFIT_SHARE, INVALID_NICKNAME, INVALID_BIO, ALREADY_TRADER, TRADER_EQUITY_TOO_LOW, TRADER_NOT_FOUND, TRADER_CLOSED, TRADER_NOT_ACTIVE, TRADER_FULL, CANNOT_FOLLOW_SELF, INVALID_AMOUNT, AMOUNT_TOO_SMALL, INSUFFICIENT_BALANCE, INVALID_COPY_MODE, INVALID_FIXED_MARGIN, INVALID_RATIO, INVALID_MAX_PER_TRADE, INVALID_TP_RATIO, INVALID_SL_RATIO, ALREADY_FOLLOWING, FOLLOW_NOT_FOUND, FOLLOW_NOT_ACTIVE, OPPOSITE_POSITION, INVALID_STATUS.

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
| FUTURES_ADL_ENABLED | true | Auto-deleveraging when the insurance fund cannot cover a bankruptcy |
| FUTURES_LIQUIDATION_FEE_RATE | (empty = contract MMR) | Liquidation fee rate |
| FUTURES_MIN_TRANSFER | 0.01 | Minimum transfer |
| FUTURES_TPSL_RETRY_SECONDS | 5 | TP/SL retry interval when there is no liquidity |
| FUTURES_TRANSFER_RETRY_SECONDS | 5 | Transfer outbox retry interval |
| FUTURES_COPY_MIN_AMOUNT | 50 | Minimum copy amount (USDT) moved into a follow sub-account |
| FUTURES_COPY_MAX_FOLLOWERS | 2000 | Max live followers per trader |
| FUTURES_COPY_DEFAULT_SHARE | 0.10 | Profit share when a trader does not set one (product range fixed at 0–50%) |
| FUTURES_COPY_TRADER_MIN_EQUITY | 100 | Futures equity required to apply as a trader |
| FUTURES_COPY_AUTO_APPROVE | false | Applications stay `pending` until approved/rejected in the admin console (`/admin/v1/copy/applications`); `true` = active immediately (dev only) |
| FUTURES_COPY_SETTLE_HOUR_UTC | 16 | Daily profit-share settlement hour (UTC; 16 = 00:00 UTC+8) |
| FUTURES_COPY_MAX_RATIO | 10 | Max ratio for ratio-mode follows |

The insurance fund starts at 0. Fee income accumulates in `system:fee` and is not swept automatically.
