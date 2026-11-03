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
| Content | `GET/POST announcements`; public `GET /announcements` | content:edit |
| Audit | `GET audit-logs?adminId&module&action&level&result&target&from&to&page&size` | audit:view |

Workflows:
- Withdrawal second review when amount ≥ `ADMIN_WITHDRAW_SECOND_REVIEW_USDT` (5000), risk > `ADMIN_WITHDRAW_SECOND_REVIEW_RISK` (40) or no USDT price; second reviewer must differ (`SAME_REVIEWER`).
- Fee change: submitted by super_admin; needs approvals from a different super_admin **and** a finance_second; then scheduled ≥ `ADMIN_FEE_NOTICE_HOURS` (24h) out with a public announcement. Rollback creates a new change through the same flow.
- Coupon template with budget (face × quantity) > `ADMIN_COUPON_FINANCE_THRESHOLD` (10000) waits for finance.
- Manual reward > `ADMIN_REWARD_FINANCE_THRESHOLD` (1000 USDT) waits for finance_second; creator can never approve their own request (`SELF_APPROVAL`).
