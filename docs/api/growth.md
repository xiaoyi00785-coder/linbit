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
- `GET /internal/referrals/{uid}` (admin lookup).
- Dev (DEV_MODE): `POST /internal/dev/growth/backdate {uid, days}` shifts registration back (tests the 1-year window).
- `GET /internal/partners`, `POST /internal/partners/{uid}/review {decision approve|reject|disable, reason, reviewer}` (admin service).
- `POST /internal/dev/growth/sync`, `POST /internal/dev/growth/settle[?cutoff=RFC3339]` (DEV_MODE only).

## Configuration
## Points carnival (积分嘉年华)

Public: `GET /points/campaigns`, `GET /points/campaign[?campaign=key]` (active, else next upcoming, else latest ended; includes rules and redeem table).
User: `POST /points/enroll {campaign?}`, `GET /points/me` (balance, today's task progress, redeemed tiers; tasks reset 00:00 UTC),
`GET /points/history`, `POST /points/redeem {points}`, `GET /points/redemptions`.
Errors: CAMPAIGN_ENDED, CAMPAIGN_NOT_STARTED, NOT_ENROLLED, INVALID_TIER, ALREADY_REDEEMED (each tier once per period), INSUFFICIENT_POINTS.
Points are earned only while enrolled and inside the period; balance shows 0 after the period ends.
Internal: `GET|POST /internal/points/campaigns` (admin `points/campaigns`, reward:edit + 2FA).

## Coupon center (卡券中心)

`GET /coupons[?status=issued|active|expired]` (active vouchers carry the live futures bonus `bonus.remaining`), `GET /coupons/summary`,
`POST /coupons/{id}/activate` → futures `POST /internal/bonus/grant` (ref `coupon:<id>`). Trial vouchers need futures own funds ≥ amount (INSUFFICIENT_OWN_FUNDS).
Errors: COUPON_NOT_FOUND, COUPON_NOT_ACTIVATABLE. Internal: `GET /internal/coupons?userId=`.

## Newcomer gift package (新人礼包) / invite KYC reward / activity center

`GET /rewards/newcomer` (KYC state, enrollment window, 8 tasks with `status` locked|in_progress|claimable|claimed|expired),
`POST /rewards/newcomer/enroll` (KYC ≥ GROWTH_NEWCOMER_KYC_LEVEL, once per user; window GROWTH_NEWCOMER_DAYS from enrollment),
`POST /rewards/newcomer/tasks/{key}/claim` → voucher in the coupon center (source `newcomer:<task>:<uid>`).
Tasks: `bind` (email + phone verified → deduction), `first_deposit` (a USD deposit ≥ min inside the window followed by a transfer
to futures → trial), `volume_1..6` (cumulative futures notional since enrollment, stackable → trial).
Errors: KYC_REQUIRED (403), ALREADY_ENROLLED, NOT_ENROLLED, TASK_NOT_FOUND, TASK_NOT_COMPLETED, ALREADY_CLAIMED.
Invite: when an invitee reaches GROWTH_INVITE_KYC_LEVEL within GROWTH_INVITE_KYC_DAYS of registering, inviter and invitee each
get a GROWTH_INVITE_KYC_REWARD trial voucher (checked on every sync; idempotent).
Public `GET /activities`: newcomer + invite (long-term) and every points period with its status (period 5 = ended).
Dev (account): `POST /internal/dev/account/bind-phone {uid, phone}`.

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
| GROWTH_COUPON_ACTIVATE_DAYS | 30 |
| GROWTH_COUPON_VALID_DAYS | 14 |
| GROWTH_COUPON_REQUIRE_OWN_FUNDS | true |
| GROWTH_VALID_INVITEE_FEES | 100 |
| GROWTH_POINTS_VOLUME_TIERS | 2000:1,10000:1,100000:3,200000:3,300000:5,500000:8,1000000:12,3000000:35,10000000:100 |
| GROWTH_POINTS_DEPOSIT_UNIT | 100 |
| GROWTH_POINTS_DEPOSIT_DAILY_CAP | 10 |
| GROWTH_POINTS_INVITE_POINTS | 8 |
| GROWTH_POINTS_INVITE_DAILY_CAP | 80 |
| GROWTH_POINTS_REDEEM | 10:deduction:3,20:deduction:6,35:trial:10,50:deduction:10,75:trial:20,100:trial:30,200:deduction:75,300:trial:100 |
| GROWTH_NEWCOMER_DAYS | 7 |
| GROWTH_NEWCOMER_KYC_LEVEL | 1 |
| GROWTH_NEWCOMER_BIND_REWARD | 5 (deduction) |
| GROWTH_NEWCOMER_DEPOSIT_MIN | 50 |
| GROWTH_NEWCOMER_DEPOSIT_REWARD | 5 (trial) |
| GROWTH_NEWCOMER_VOLUME_TIERS | 10000:5,50000:10,100000:15,300000:30,500000:50,1000000:100 (trial) |
| GROWTH_INVITE_KYC_REWARD | 20 (trial, each side) |
| GROWTH_INVITE_KYC_LEVEL | 1 |
| GROWTH_INVITE_KYC_DAYS | 30 |
