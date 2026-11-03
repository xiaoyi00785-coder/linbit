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
`AMOUNT_TOO_LARGE`, `PRICE_UNAVAILABLE` (503), `QUOTE_NOT_FOUND` (404; including
someone else's quote), `QUOTE_EXPIRED` (409), `INSUFFICIENT_BALANCE`.

## Ledger

On accept, one DB transaction: lock the quote → claim ref `convert:<quoteId>` →
user free −from / +to → journal (user ↔ `system:convert`) → order row. The
`system:convert` net position per asset is exposed as `convertPosition` in the
solvency liabilities and admin solvency lines (negative = the platform owes
that asset to users; user balances already include it, so holdings must cover it).

## Admin (`/admin/v1`)

| Method | Path | Permission |
|---|---|---|
| GET | `/convert/config` | symbol:view |
| PUT | `/convert/config` | symbol:edit + 2FA (`totpCode`), audited high level |
| GET | `/convert/orders?userId=&asset=&page=&pageSize=` | ledger:view |
| GET | `/convert/position` | ledger:view |

PUT body: `{spreadBps (0–500)?, minUsdt?, maxUsdt?, assets?:[{asset, enabled}], reason, totpCode}`.
Pairs are enabled per asset: a pair is available when both assets are enabled.

Smoke: `bash scripts/smoke-convert.sh`.
