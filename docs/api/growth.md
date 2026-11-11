# 增长服务 API（`services/go/growth`，:8091）

用户增长。Gateway 挂在 `/partner`、`/referral`、`/points`、`/rewards`、`/activities`（未标 public 的要 JWT）。和其他服务一样跟内部游标 feed：

| Feed | 来源 |
|---|---|
| `GET /internal/users/feed?after&limit`、`GET /internal/users/profile?uid=a,b` | account（注册邀请码、KYC 等级） |
| `GET /internal/trades/feed?after&limit` | order（现货成交，按边） |
| `GET /internal/trades/feed?after&limit` | futures（按边的成交；跟单子账户映射到跟随者；`bonusFee` = 体验金支付的手续费） |
| `GET /internal/ops/feed?after&limit` | asset（钱包操作：deposit、futures_out、…） |

派奖走 asset `POST /internal/system-transfer`（平台账户 `rebate`、`marketing`、`earn`、`earn_interest`、`loan`、`loan_collateral`、`loan_interest`；按 `ref` 幂等）。

## 邀请归属

`POST /auth/register` 接受 `inviteCode`。每个用户有一个 8 位邀请码；邀请人在注册时定死，只有一级。

## 合伙人（`/partner`）

| 方法 | 路径 | 说明 |
|---|---|---|
| GET | `/partner/rules` | **public**。档位、级差、结算 |
| POST | `/partner/apply` | `{contact, channel, scale}`；`GROWTH_PARTNER_AUTO_APPROVE=false` 时待后台审核；驳回后可再申请。错误 `INVALID_APPLICATION`、`PARTNER_EXISTS` |
| GET | `/partner/me` | `{status: none\|pending\|active\|rejected\|disabled, tier, futuresRate, spotRate, partner{reviewNote…}}` |
| GET | `/partner/summary` | tier、`volume30d`、`nextTier{gap}`、`directs`、`directPartners`、`unsettledEstimate`、`yesterday`、`month`、`total`、`overrideTotal`、`pendingPayout` |
| GET | `/partner/commissions?kind=futures\|spot\|override\|referral&page&pageSize` | 已结算行（每个来源用户每天） |
| GET | `/partner/commissions/daily?days=30` | 按日、按 kind 合计 |
| GET | `/partner/team?type=all\|user\|partner&page&pageSize` | 直客，含 30 日成交量、手续费、佣金 |
| GET | `/partner/links` | `{code, url, registerUrl}` |

非合伙人访问 summary / commissions / team 得到 `403 NOT_PARTNER`。

档位（按直客近 30 日合约名义，每次结算重算）：

| 档 | 阈值（USDT） | 合约 | 现货 |
|---|---|---|---|
| normal | 已通过 | 30% | 20% |
| silver | ≥ 5,000,000 | 40% | 25% |
| gold | ≥ 20,000,000 | 45% | 30% |
| diamond | ≥ 50,000,000 | 50% | 35% |

佣金基数 = 实际支付的手续费（减去体验金和手续费券覆盖的部分），单位 USDT。基础币收取的现货手续费按成交价折算；只计 `GROWTH_USD_QUOTES` 交易对。级差：合伙人的邀请人若是活跃合伙人，拿直客佣金的 10%；该邀请人的邀请人同样（平台出资，直客合伙人往上最多 2 级；每级都是直客佣金的 10%，第 3 级及以上为 0）。`/partner/rules` 返回 `overrideDepth`（2）和 `overrideLevels`（3，含直客本人）。每日 `GROWTH_SETTLE_HOUR_UTC`（16 = 00:00 UTC+8）结算进资金账户（`ref growth_comm_<settlement>_<uid>`）；终身有效。

## 邀请返佣（`/referral`）

| 方法 | 路径 | 说明 |
|---|---|---|
| GET | `/referral/me` | code、url、rate（10%）、validDays（365）、invited、verified、totalRewards、partnerStatus、invitedBy |
| GET | `/referral/invitees` | 打码 uid、registeredAt、kycLevel、rewardUntil、active、rewards |
| GET | `/referral/rewards` | 已结算的邀请行 |

邀请人已是活跃合伙人时按合伙人比例计，两边不叠加。跟单分润不是手续费，不计佣。

## 内部 / 开发

- `GET /internal/referrals/{uid}`（后台查询）。
- 开发（DEV_MODE）：`POST /internal/dev/growth/backdate {uid, days}` 把注册时间往前拨（测 1 年窗口）。
- `GET /internal/partners`，`POST /internal/partners/{uid}/review {decision approve|reject|disable, reason, reviewer}`（admin 服务）。
- `POST /internal/dev/growth/sync`，`POST /internal/dev/growth/settle[?cutoff=RFC3339]`（仅 DEV_MODE）。

## 积分嘉年华

公开：`GET /points/campaigns`，`GET /points/campaign[?campaign=key]`（进行中，否则下一档即将开始，否则最近已结束；含规则和兑换表）。
用户：`POST /points/enroll {campaign?}`，`GET /points/me`（余额、今日任务进度、已兑换档；任务 00:00 UTC 重置），`GET /points/history`，`POST /points/redeem {points}`，`GET /points/redemptions`。
错误：CAMPAIGN_ENDED、CAMPAIGN_NOT_STARTED、NOT_ENROLLED、INVALID_TIER、ALREADY_REDEEMED（每期每档一次）、INSUFFICIENT_POINTS。
只在已报名且处于活动期内计分；期结束后余额显示 0。
内部：`GET|POST /internal/points/campaigns`（后台 `points/campaigns`，reward:edit + 2FA）。

## 卡券中心

`GET /coupons[?status=issued|active|expired]`（激活中的券带合约赠金实时余额 `bonus.remaining`），`GET /coupons/summary`，`POST /coupons/{id}/activate` → futures `POST /internal/bonus/grant`（ref `coupon:<id>`）。体验金券要求合约自有资金 ≥ amount（`INSUFFICIENT_OWN_FUNDS`）。
错误：COUPON_NOT_FOUND、COUPON_NOT_ACTIVATABLE。内部：`GET /internal/coupons?userId=`。

## 新人礼包 / 邀请 KYC 奖励 / 活动中心

`GET /rewards/newcomer`（KYC 状态、报名窗口、8 个任务，`status` 为 locked|in_progress|claimable|claimed|expired），
`POST /rewards/newcomer/enroll`（KYC ≥ GROWTH_NEWCOMER_KYC_LEVEL，每用户一次；窗口为报名起 GROWTH_NEWCOMER_DAYS），
`POST /rewards/newcomer/tasks/{key}/claim` → 卡券中心发券（source `newcomer:<task>:<uid>`）。
任务：`bind`（邮箱 + 手机都已验证 → 抵扣金），`first_deposit`（窗口内一笔 USD 充值 ≥ min，随后划入合约 → 体验金），`volume_1..6`（报名后累计合约名义，可叠加 → 体验金）。
错误：KYC_REQUIRED（403）、ALREADY_ENROLLED、NOT_ENROLLED、TASK_NOT_FOUND、TASK_NOT_COMPLETED、ALREADY_CLAIMED。
邀请：被邀请人在注册后 GROWTH_INVITE_KYC_DAYS 内达到 GROWTH_INVITE_KYC_LEVEL 时，邀请人和被邀请人各得一张 GROWTH_INVITE_KYC_REWARD 体验金券（每次 sync 检查，幂等）。
公开 `GET /activities`：新人 + 邀请（长期）以及每一期积分及其状态（第 5 期 = 已结束）。
开发（account）：`POST /internal/dev/account/bind-phone {uid, phone}`。

## 配置

| 变量 | 默认 |
|---|---|
| GROWTH_SYNC_MS | 2000 |
| GROWTH_SETTLE_HOUR_UTC | 16 |
| GROWTH_DAY_UTC_OFFSET_HOURS | 8 |
| GROWTH_PAYOUT_ASSET | USDT |
| GROWTH_USD_QUOTES | USDT,USDC |
| GROWTH_INVITE_BASE_URL | https://www.cowa.click/r/（主网）。邀请链接 = base + code；web 的 `/r/{code}` → `/register?inviteCode={code}`；registerUrl = base 去掉 `r/` 再加 `register?inviteCode=` |
| GROWTH_PARTNER_TIERS | normal:0:0.30:0.20,silver:5000000:0.40:0.25,gold:20000000:0.45:0.30,diamond:50000000:0.50:0.35（key:minFutures30dVolume:futuresRate:spotRate） |
| GROWTH_TIER_WINDOW_DAYS | 30 |
| GROWTH_PARTNER_OVERRIDE_RATE | 0.10 |
| GROWTH_PARTNER_OVERRIDE_DEPTH | 2（直客合伙人之上的额外级差层数；第 3 级及以上为 0） |
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
| GROWTH_NEWCOMER_BIND_REWARD | 5（抵扣金） |
| GROWTH_NEWCOMER_DEPOSIT_MIN | 50 |
| GROWTH_NEWCOMER_DEPOSIT_REWARD | 5（体验金） |
| GROWTH_NEWCOMER_VOLUME_TIERS | 10000:5,50000:10,100000:15,300000:30,500000:50,1000000:100（体验金） |
| GROWTH_INVITE_KYC_REWARD | 20（体验金，双方各一张） |
| GROWTH_INVITE_KYC_LEVEL | 1 |
| GROWTH_INVITE_KYC_DAYS | 30 |
