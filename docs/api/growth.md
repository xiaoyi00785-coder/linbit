# Growth service API (`services/go/growth`, :8091)

User-growth programs. Routed by the gateway under `/partner`, `/referral`,
`/points`, `/rewards`, `/activities` (JWT required unless marked public).
The service follows other services through internal cursor feeds:

| Feed | Source |
|---|---|
| `GET /internal/users/feed?after&limit`, `GET /internal/users/profile?uid=a,b` | account (registration invite code, KYC level) |
| `GET /internal/trades/feed?after&limit` | order (spot fills, per side) |
| `GET /internal/trades/feed?after&limit` | futures (fills per side; copy sub-accounts mapped to the follower; `bonusFee` = fee paid by trial bonus) |
| `GET /internal/ops/feed?after&limit` | asset (wallet ops: deposit, futures_out, …) |

Payouts use asset `POST /internal/system-transfer` (platform accounts
`rebate`, `marketing`, `earn`, `earn_interest`, `loan`, `loan_collateral`,
`loan_interest`; idempotent on `ref`).

## Invite attribution
`POST /auth/register` accepts `inviteCode`. Every user gets an 8-character
code; the inviter is fixed at registration, one level only.

## Partner program (`/partner`)

| Method | Path | Notes |
|---|---|---|
| GET | `/partner/rules` | **public** – tiers, override, settlement |
| POST | `/partner/apply` | `{contact, channel, scale}`; pending until admin review (`GROWTH_PARTNER_AUTO_APPROVE=false`); re-apply allowed after rejection. Errors `INVALID_APPLICATION`, `PARTNER_EXISTS` |
| GET | `/partner/me` | `{status: none\|pending\|active\|rejected\|disabled, tier, futuresRate, spotRate, partner{reviewNote…}}` |
| GET | `/partner/summary` | tier, `volume30d`, `nextTier{gap}`, `directs`, `directPartners`, `unsettledEstimate`, `yesterday`, `month`, `total`, `overrideTotal`, `pendingPayout` |
| GET | `/partner/commissions?kind=futures\|spot\|override\|referral&page&pageSize` | settled rows (per source user per day) |
| GET | `/partner/commissions/daily?days=30` | per-day totals by kind |
| GET | `/partner/team?type=all\|user\|partner&page&pageSize` | directs with 30-day volume, fees, commission |
| GET | `/partner/links` | `{code, url, registerUrl}` |

Non-partners get `403 NOT_PARTNER` on summary / commissions / team.

Tiers (by directs' trailing-30-day futures notional, recomputed at each settlement):

| Tier | Threshold (USDT) | Futures | Spot |
|---|---|---|---|
| normal | approved | 30% | 20% |
| silver | ≥ 5,000,000 | 40% | 25% |
| gold | ≥ 20,000,000 | 45% | 30% |
| diamond | ≥ 50,000,000 | 50% | 35% |

Commission base = fee actually paid (minus trial-bonus and fee-coupon covered
parts), in USDT. Spot fees in the base asset are valued at the fill price;
only `GROWTH_USD_QUOTES` pairs count. Override: the partner's inviter, if an
active partner, earns 10% of the direct commission (platform-funded, two
levels in total). Settled daily at `GROWTH_SETTLE_HOUR_UTC` (16 = 00:00 UTC+8)
into the funding account (`ref growth_comm_<settlement>_<uid>`); valid for life.

## Referral (`/referral`)

| Method | Path | Notes |
|---|---|---|
| GET | `/referral/me` | code, url, rate (10%), validDays (365), invited, verified, totalRewards, partnerStatus, invitedBy |
| GET | `/referral/invitees` | masked uid, registeredAt, kycLevel, rewardUntil, active, rewards |
| GET | `/referral/rewards` | settled referral rows |

Inviters who are active partners earn partner rates instead – never both.
Copy-trading profit share is not a fee and earns nothing.

## Internal / dev
- `GET /internal/partners`, `POST /internal/partners/{uid}/review {decision approve|reject|disable, reason, reviewer}` (admin service).
- `POST /internal/dev/growth/sync`, `POST /internal/dev/growth/settle[?cutoff=RFC3339]` (DEV_MODE only).

## Configuration
| Env | Default |
|---|---|
| GROWTH_SYNC_MS | 2000 |
| GROWTH_SETTLE_HOUR_UTC | 16 |
| GROWTH_DAY_UTC_OFFSET_HOURS | 8 |
| GROWTH_PAYOUT_ASSET | USDT |
| GROWTH_USD_QUOTES | USDT,USDC |
| GROWTH_INVITE_BASE_URL | https://linbit.example.com/r/ |
| GROWTH_PARTNER_TIERS | normal:0:0.30:0.20,silver:5000000:0.40:0.25,gold:20000000:0.45:0.30,diamond:50000000:0.50:0.35 |
| GROWTH_TIER_WINDOW_DAYS | 30 |
| GROWTH_PARTNER_OVERRIDE_RATE | 0.10 |
| GROWTH_PARTNER_OVERRIDE_DEPTH | 1 |
| GROWTH_PARTNER_AUTO_APPROVE | false |
| GROWTH_REFERRAL_RATE | 0.10 |
| GROWTH_REFERRAL_DAYS | 365 |
