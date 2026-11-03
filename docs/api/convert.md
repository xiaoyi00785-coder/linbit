# 闪兑 Convert API

Instant conversion between spot (funding) balances, priced from the market-data
index price with a platform spread. The platform (`system:convert` ledger
account) is the counterparty. Implemented in the **asset** service; the gateway
proxies `/convert/*`.

## Pricing

- `rate = indexRate × (10000 − spreadBps) / 10000`; `indexRate = price(from) / price(to)` in USDT.
- USDT and USDC are priced at 1. Stable↔stable converts 1:1 with spread 0.
- Non-USDT pairs (e.g. BTC→ETH) are crossed through the USDT index prices (`route: via_usdt`).
- Index missing or older than `CONVERT_PRICE_MAX_AGE_SECONDS` (default 15) → `503 PRICE_UNAVAILABLE` (fail closed).
- Fee is `0`; the spread is the only revenue. Rounding always favours the platform:
  given `fromAmount`, `toAmount` is truncated to the target precision; given `toAmount`, `fromAmount` is rounded up.
- Per-trade limit is checked on the USDT value of the from side: `CONVERT_MIN_USDT` (10) ≤ value ≤ `CONVERT_MAX_USDT` (50000).

Env defaults (seed the DB config on first boot; afterwards admin edits win):
`CONVERT_SPREAD_BPS=20`, `CONVERT_MIN_USDT=10`, `CONVERT_MAX_USDT=50000`,
`CONVERT_QUOTE_TTL_SECONDS=10`, `CONVERT_PRICE_MAX_AGE_SECONDS=15`, `MARKET_URL`.

## User endpoints (gateway)

| Method | Path | Auth | Notes |
|---|---|---|---|
| GET | `/convert/pairs` | public | assets, pairs, limits, spread, fee, TTL |
| POST | `/convert/quote` | user | `{from, to, fromAmount}` or `{from, to, toAmount}` |
| POST | `/convert/accept` | user | `{quoteId}`, atomic and idempotent |
| GET | `/convert/orders` | user | `?page=1&pageSize=20&asset=BTC` |

Quote response:

```json
{"quoteId":"cq_…","from":"USDT","to":"BTC","fromAmount":"1000","toAmount":"0.01996",
 "rate":"0.00001996","inverseRate":"50100.2004","indexRate":"0.00002","spreadBps":20,
 "fee":"0","feeAsset":"BTC","expiresAt":1793723975000,"ttlSeconds":10}
```

Accept response: `{"order": ConvertOrder, "replay": false}`. Re-submitting the same
`quoteId` returns the same order with `replay: true` and moves no funds.

ConvertOrder: `{id, quoteId, from, to, fromAmount, toAmount, rate, indexRate, spreadBps, fee, usdtValue, status:"filled", createdAt(ms)}`.

Error codes: `VALIDATION_ERROR`, `PAIR_NOT_SUPPORTED`, `AMOUNT_TOO_SMALL`,
`AMOUNT_TOO_LARGE`, `PRICE_UNAVAILABLE` (503), `PAIR_PAUSED_DEPEG` (503, see below), `QUOTE_NOT_FOUND` (404; including
someone else's quote), `QUOTE_EXPIRED` (409), `INSUFFICIENT_BALANCE`.

## Ledger

On accept, one DB transaction: lock the quote → claim ref `convert:<quoteId>` →
user free −from / +to → journal (user ↔ `system:convert`) → order row. The
`system:convert` net position per asset is exposed as `convertPosition` in the
solvency liabilities and admin solvency lines (negative = the platform owes
that asset to users; user balances already include it, so holdings must cover it).

## USDC depeg guard

USDC↔USDT (both directions) is paused while `|USDCUSDT index − 1| > depegBps`
(default 50 = 0.5%, config `depegBps` 1–1000). A missing or stale index
(older than `CONVERT_PRICE_MAX_AGE_SECONDS`) also counts as paused. Quote and
accept fail with `PAIR_PAUSED_DEPEG` (503); `/convert/pairs` shows
`paused`/`pauseReason` (`depeg` | `price_unavailable`) on the two pairs.
USDC↔other assets are not affected. A monitor (every
`CONVERT_DEPEG_CHECK_SECONDS`, default 5) and every quote re-evaluate the
state; it resumes automatically. Every transition upserts
`convert_pair_state`, writes a `convert_events` row (`pair_paused` /
`pair_resumed`) and sends a Telegram alert.

## Spot hedging

The convert desk (`system:convert`) is hedged on the platform's own spot book
by a **dedicated hedge account** (`CONVERT_HEDGE_USER_ID`, default
`system:hedge`):

- `system:convert` – ledger counterparty of every convert (its balance is the
  desk position; negative = owed to users).
- `system:hedge` – trades spot (BTC/ETH/SOL/BNB/TRX vs USDT). It is funded by
  journal transfers *from* `system:convert` (ref `hedge_fund:<hedgeId>`, never
  more than the desk holds — no balance is created). It must differ from the
  market-maker account (`mm:main`) and from `system:convert`; the service
  refuses to start otherwise, so a hedge order can never self-trade against MM
  quotes. Like all `system:*` users it is excluded from user liabilities.
- Net exposure per asset = convert position + hedge balance. Solvency and
  `/admin/v1/convert/position` report `convertPosition`, `hedgeBalance`,
  `netPosition`.

Per cycle (every `CONVERT_HEDGE_INTERVAL_SECONDS`, default 5; 0 = loop off)
for each hedge-enabled asset with |net| ≥ `hedgeMinQty`: place one IOC limit
order at index ± `hedge.slippageBps` (default 50; buy price rounded down, sell
rounded up), capped by `hedge.maxOrderUsdt` (20000) and the rolling 1-hour
`hedge.maxHourUsdt` (200000). Thin depth fills what is there; the rest is
retried with exponential backoff (`CONVERT_HEDGE_BACKOFF_SECONDS` 5 →
`CONVERT_HEDGE_BACKOFF_MAX_SECONDS` 300). If unhedged value stays above
`hedge.unhedgedAlertUsdt` (5000) for more than `hedge.unhedgedAlertMinutes`
(10) a Telegram alert + `unhedged_alert` event fire once per episode.
Rejected/failed orders write `hedge_rejected`/`hedge_failed` events and alert
(max once per 10 min per asset).

Switches: global `hedge.enabled` (unset = env `CONVERT_HEDGE_ENABLED`, which
defaults to ON only when `WALLET_CHAIN=testnet`) and per-asset
`hedgeEnabled`. Default per-asset minimums: BTC 0.0002, ETH 0.005, SOL 0.1,
BNB 0.02, TRX 50.

## Admin (`/admin/v1`)

| Method | Path | Permission |
|---|---|---|
| GET | `/convert/config` | symbol:view |
| PUT | `/convert/config` | symbol:edit + 2FA (`totpCode`), audited high level |
| GET | `/convert/orders?userId=&asset=&page=&pageSize=` | ledger:view |
| GET | `/convert/position` | ledger:view |
| GET | `/convert/hedges?asset=&status=&page=&pageSize=` | ledger:view |
| GET | `/convert/events?kind=&page=&pageSize=` | ledger:view |

PUT body: `{spreadBps (0–500)?, minUsdt?, maxUsdt?, depegBps (1–1000)?,
hedge?:{enabled?, slippageBps (1–500)?, maxOrderUsdt?, maxHourUsdt? (≥ maxOrder),
unhedgedAlertUsdt?, unhedgedAlertMinutes (1–1440)?},
assets?:[{asset, enabled?, hedgeEnabled?, hedgeMinQty?}], reason (required), totpCode}`.

`/convert/hedges` → `{items:[{id, asset, symbol, side, quantity, limitPrice,
indexPrice, netBefore, orderId, filledQty, quoteAmount,
status: pending|filled|partial|unfilled|rejected|failed, error, createdAt}], total,
account, state:{<asset>:{failures, nextRetryAt, unhedgedUsdt, unhedgedSince, alerted, lastStatus, lastError}}}`.
`/convert/position` → `{account, hedgeAccount, hedgeEnabled, depeg, items:[{asset,
position, hedgeBalance, netPosition, hedge}]}`.

Internal (asset service, `X-Internal-Token`): `POST /internal/convert/hedge/run`
runs one forced cycle (ops / smoke).
Pairs are enabled per asset: a pair is available when both assets are enabled.

Smoke: `bash scripts/smoke-convert.sh`.
