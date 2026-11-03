# Admin (back-office) API — `services/go/admin` (:8088)

Not exposed through the public gateway. Auth: `POST /admin/v1/auth/login {username,password,totpCode}` → `{token}`;
send `Authorization: Bearer <token>`. Sessions expire after `ADMIN_SESSION_IDLE_MINUTES` idle (`SESSION_EXPIRED`).
Every write (and failed/forbidden attempts) is written to the append-only `audit_logs` table.

| Area | Endpoints | Permission |
|---|---|---|
| Auth | `POST auth/login`, `POST auth/logout`, `GET me` | — |
| Admins | `GET/POST admins`, `PATCH admins/{id}` (roles/status + reason + 2FA; no delete) | admin:manage |
| Users | `GET users`, `GET users/{uid}` (`?reveal=true` needs user:pii, audited), `POST users/{uid}/freeze\|unfreeze` (reason + 2FA) | user:view / user:edit |
| KYC | `GET kyc/applications[/{id}]`, `POST …/{id}/approve\|reject`, `GET kyc/documents/{id}` (audited) | kyc:view / kyc:approve |
| Withdrawals | `GET withdrawals`, `POST withdrawals/{id}/approve` (comment + 2FA), `…/reject` (reason + 2FA) | withdraw:first / withdraw:second |
| Ledger | `GET users/{uid}/balances`, `GET ledger` | ledger:view |
| Symbols | `GET/POST symbols`, `PATCH symbols/{symbol}` (reason) | symbol:view / symbol:edit |
| Fees | `GET fees`, `GET/POST fee-changes`, `POST fee-changes/{id}/approve\|reject\|rollback` | fee:view / fee:edit / fee:approve |
| Coupons | `GET/POST coupons`, `POST coupons/{id}/approve\|reject` | reward:edit / coupon:approve |
| Rewards | `GET/POST rewards`, `POST rewards/{id}/approve\|reject` (2FA) | reward:edit / reward:approve |
| Copy trading | `GET copy/applications?status=pending\|active\|rejected\|closed\|all&page&pageSize`, `POST copy/applications/{uid}/approve` (2FA), `…/reject` (reason + 2FA); proxied to futures `/internal/copy/applications`, audited as module `copy` (high) | copy:view / copy:review |
| Referrals | `GET referrals/{uid}` – inviter, invite code, invited count, referral window end, partner record, commission totals by kind | user:view |
| Points campaigns | `GET points/campaigns`; `POST points/campaigns {key,title,startsAt,endsAt,rules?,totpCode}` (2FA, high audit, overlap rejected) | reward:view / reward:edit |
| User coupons | `GET users/{uid}/coupons` – vouchers with live bonus balance | user:view |
| Partners | `GET partners?status=pending\|active\|rejected\|disabled&page&pageSize`, `POST partners/{uid}/approve` (2FA), `…/reject`, `…/disable` (reason + 2FA); proxied to growth `/internal/partners`, audited as module `partner` (high) | partner:view / partner:review |
| Content | `GET/POST announcements`; public `GET /announcements` | content:edit |
| Audit | `GET audit-logs?adminId&module&action&level&result&target&from&to&page&size` | audit:view |

Workflows:
- Withdrawal second review when amount ≥ `ADMIN_WITHDRAW_SECOND_REVIEW_USDT` (5000), risk > `ADMIN_WITHDRAW_SECOND_REVIEW_RISK` (40) or no USDT price; second reviewer must differ (`SAME_REVIEWER`).
- Fee change: submitted by super_admin; needs approvals from a different super_admin **and** a finance_second; then scheduled ≥ `ADMIN_FEE_NOTICE_HOURS` (24h) out with a public announcement. Rollback creates a new change through the same flow.
- Coupon template with budget (face × quantity) > `ADMIN_COUPON_FINANCE_THRESHOLD` (10000) waits for finance.
- Copy-trading trader applications (`FUTURES_COPY_AUTO_APPROVE=false`, default) stay `pending` until reviewed; only pending ones can be decided (`NOT_PENDING`). A rejected applicant sees `reviewNote` on `GET /futures/copy/trader` and may re-apply. Roles: operations (view + review), risk/auditor (view), super_admin (all).
- Manual reward > `ADMIN_REWARD_FINANCE_THRESHOLD` (1000 USDT) waits for finance_second; creator can never approve their own request (`SELF_APPROVAL`).

## 理财 / 借币产品管理

权限：`earn:view`（operations、finance_first、finance_second、risk、auditor、marketing、super_admin）；
`earn:edit`（operations、super_admin）。所有写操作需 `totpCode`（2FA）与 `reason`，按 high 级别写审计日志并记录上游返回的 before/after。

| 方法 | 路径 | 权限 |
|---|---|---|
| GET | `/admin/v1/earn/products[/{id}]` | earn:view |
| POST | `/admin/v1/earn/products` | earn:edit — 仅新建（已存在返回 409 `PRODUCT_EXISTS`），默认 `status=offline`，APR ∈ [0,1] |
| PATCH / DELETE | `/admin/v1/earn/products/{id}` | earn:edit — 不允许修改利率（`RATE_CHANGE_REQUIRES_APPROVAL`） |
| PUT | `/admin/v1/earn/products/{id}/quota` | earn:edit |
| GET | `/admin/v1/loans/terms` | earn:view |
| PATCH | `/admin/v1/loans/terms/{term}` · PUT `.../quota` | earn:edit |
| GET / POST | `/admin/v1/loans/collaterals` | view / edit |
| DELETE | `/admin/v1/loans/collaterals/{asset}` | earn:edit |

### 利率变更（双人审批，流程同手续费变更）

- `POST /admin/v1/rate-changes`（earn:edit）
  - `{"target":"earn_product","productId":"simple-USDT","apr":"0.02","bonusApr":"0.025","reason":"..."}`
  - `{"target":"loan_term","term":"flexible","hourlyRate":"0.000005","reason":"..."}`
  - 提交时记录当前值为 `before`。
- `GET /admin/v1/rate-changes?status=`（earn:view）
- `POST /admin/v1/rate-changes/{id}/approve|reject`：需 super_admin + finance_second 各一票，均不得为提交人，需 2FA。
  通过后发布公告（category `earn` / `loan`），`FeeNotice`（24h）后由调度器生效，审计 module 为 `earn_rate` / `loan_rate`。
- `POST /admin/v1/rate-changes/{id}/rollback`：以 `before` 生成新的变更请求，同样需双人审批。
- 利率变更与手续费变更互相隔离：`/fee-changes/*` 无法审批或回滚利率变更，反之亦然。
