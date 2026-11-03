# Earn service (port 8092)

Crypto loans and savings products. Money moves through asset
`/internal/system-transfer` with idempotent refs against the system accounts
`loan`, `loan_collateral`, `loan_interest`, `earn`, `earn_interest`.
Prices come from the futures index (`<ASSET>USDT`). Errors are
`{ "code": "...", "message": "..." }`.

## Loans

Public: `GET /loans/market`, `GET /loans/quote`. Everything else needs a user token.

| Method | Path | Notes |
|---|---|---|
| GET | `/loans/market` | terms (`flexible`, `7d`, `30d`) with hourly/annual rate, collaterals with per-user cap and index price, LTV thresholds, limits |
| GET | `/loans/quote?collateralAsset=BTC&amount=10000&term=flexible[&collateralAmount=]` | collateral (default at initial LTV), ltv, hourlyInterest, termInterest, liquidationPrice |
| POST | `/loans` | `{collateralAsset, amount, collateralAmount?, term?}` locks collateral, credits the loan |
| GET | `/loans?status=active\|closed` | list with live ltv / warning / overdue / dueAt |
| GET | `/loans/summary` | active count, total debt, collateral value |
| GET | `/loans/records?type=borrow\|repay\|collateral\|renew\|liquidate` | event history |
| GET | `/loans/{id}` | one loan |
| POST | `/loans/{id}/repay` | `{amount}` or `{all:true}`; pays interest first, then principal (+ early-repay penalty on fixed terms before due); full repay releases collateral |
| POST | `/loans/{id}/collateral` | `{amount}` adds collateral |
| POST | `/loans/{id}/renew` | fixed terms only: pays accrued interest, extends due by one term |

Interest accrues hourly in advance: hour *n* is charged when it starts,
`ceil8(principal × hourlyRate)`; hours starting at/after due are charged at
`LOAN_OVERDUE_RATE_MULTIPLIER`×. The ticker warns at `LOAN_WARN_LTV` and
liquidates at `LOAN_LIQUIDATION_LTV` or after `LOAN_OVERDUE_LIQUIDATION_DAYS`
overdue: it seizes `(debt × (1 + LOAN_LIQUIDATION_FEE_RATE)) / price` of
collateral and returns the remainder.

Errors: `UNSUPPORTED_COLLATERAL`, `INVALID_TERM`, `AMOUNT_TOO_SMALL`,
`LTV_TOO_HIGH`, `LOAN_LIMIT_EXCEEDED`, `COLLATERAL_LIMIT_EXCEEDED`,
`PRICE_UNAVAILABLE` (503), `LOAN_NOT_FOUND`, `LOAN_NOT_ACTIVE`,
`TERM_NOT_RENEWABLE`, `INSUFFICIENT_BALANCE` (from asset).

Dev only (`DEV_MODE=true`): `POST /internal/dev/earn/advance {hours}` shifts
the service clock and runs one tick.

## Savings products

Public: `GET /earn/products`, `GET /earn/products/{id}`.

| Method | Path | Notes |
|---|---|---|
| GET | `/earn/products?kind=simple\|fixed\|onchain&asset=&hot=true` | catalogue (apr, aprMax, bonusApr/bonusQuota, termDays, unlockDays, minAmount, perUserMax, totalQuota/remaining, newcomer, redeemRule) |
| GET | `/earn/products/{id}` | one product |
| POST | `/earn/products/{id}/subscribe` | `{amount}` debits spot. fixed → order (`expectedInterest`, `maturityAt`); simple/onchain → holding, interest from the next UTC day (`interestStartsAt`) |
| POST | `/earn/products/{id}/redeem` | `{amount}` or `{all:true}`; simple → immediate; onchain → immediate or `unlocking` for unlockDays (no yield); fixed → `REDEEM_NOT_ALLOWED` |
| POST | `/earn/products/{id}/auto` | `{enabled}` simple only: idle spot balance of the asset is subscribed on every tick |
| GET | `/earn/positions?kind=` | holdings (amount, pending, totalInterest, estDailyInterest, auto) + `unlocking` |
| GET | `/earn/orders?status=holding\|matured` | fixed orders |
| GET | `/earn/records?type=subscribe\|redeem\|interest\|maturity\|unlock` | history |
| GET | `/earn/summary` | totalValueUsdt, totalInterestUsdt, recentInterestUsdt (since start of previous UTC day), per asset |
| POST | `/internal/earn/products` | ops upsert of a product (internal token) |

Rules: simple/onchain pay `floor8(yearly / 365)` per complete UTC day on the
principal held at the start of that day, where yearly = `min(p, bonusQuota) ×
bonusApr + rest × apr`. Fixed pays `floor8(amount × apr × termDays / 365)`
with principal at maturity (next tick). Newcomer products: only for users
without any non-newcomer fixed order, capped by `perUserMax`.

Errors: `PRODUCT_NOT_FOUND`, `PRODUCT_OFFLINE`, `AMOUNT_TOO_SMALL`,
`QUOTA_EXCEEDED`, `USER_LIMIT_EXCEEDED`, `NEWCOMER_ONLY`,
`REDEEM_NOT_ALLOWED`, `INSUFFICIENT_POSITION`, `AUTO_NOT_SUPPORTED`,
`INSUFFICIENT_BALANCE` (from asset).

The seed catalogue (migration 00002) copies the products and APRs shown in
the UI; simple products earn the upper APR on the first bonus quota (USDT/USDC
10000, BTC 0.5, ETH 5, SOL 100, DOGE 50000) and the lower APR above it.

## Config

| Env | Default |
|---|---|
| EARN_TICK_MS | 60000 |
| LOAN_ASSET | USDT |
| LOAN_RATE_FLEXIBLE / LOAN_RATE_7D / LOAN_RATE_30D | 0.000003847 / 0.0000042808 / 0.0000068493 (hourly) |
| LOAN_USER_LIMIT | 1200000 |
| LOAN_MIN_AMOUNT | 10 |
| LOAN_INITIAL_LTV / LOAN_WARN_LTV / LOAN_LIQUIDATION_LTV | 0.80 / 0.88 / 0.95 |
| LOAN_COLLATERALS | BTC:10,ETH:50 (asset:per-user cap) |
| LOAN_OVERDUE_RATE_MULTIPLIER | 3 |
| LOAN_OVERDUE_LIQUIDATION_DAYS | 7 |
| LOAN_LIQUIDATION_FEE_RATE | 0.03 |
| LOAN_EARLY_REPAY_PENALTY_RATE | 0.0005 |
