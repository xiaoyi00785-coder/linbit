# 后台 API — `services/go/admin`（:8088）

不经公开 gateway。认证：`POST /admin/v1/auth/login {username,password,totpCode}` → `{token}`；
请求带 `Authorization: Bearer <token>`。空闲超过 `ADMIN_SESSION_IDLE_MINUTES` 会话过期（`SESSION_EXPIRED`）。
每次写操作（以及失败/禁止的尝试）写入只追加的 `audit_logs`。

| 范围 | 接口 | 权限 |
|---|---|---|
| 认证 | `POST auth/login`、`POST auth/logout`、`GET me` | — |
| 管理员 | `GET/POST admins`、`PATCH admins/{id}`（角色/状态 + reason + 2FA；不删除） | admin:manage |
| 用户 | `GET users`、`GET users/{uid}`（`?reveal=true` 要 user:pii，写审计）、`POST users/{uid}/freeze\|unfreeze`（reason + 2FA） | user:view / user:edit |
| KYC | `GET kyc/applications[/{id}]`、`POST …/{id}/approve\|reject`、`GET kyc/documents/{id}`（写审计） | kyc:view / kyc:approve |
| 提现 | `GET withdrawals`、`POST withdrawals/{id}/approve`（comment + 2FA）、`…/reject`（reason + 2FA） | withdraw:first / withdraw:second |
| 流水 | `GET users/{uid}/balances`、`GET ledger` | ledger:view |
| 交易对 | `GET/POST symbols`、`PATCH symbols/{symbol}`（reason） | symbol:view / symbol:edit |
| 费率 | `GET fees`、`GET/POST fee-changes`、`POST fee-changes/{id}/approve\|reject\|rollback` | fee:view / fee:edit / fee:approve |
| 卡券 | `GET/POST coupons`、`POST coupons/{id}/approve\|reject` | reward:edit / coupon:approve |
| 奖励 | `GET/POST rewards`、`POST rewards/{id}/approve\|reject`（2FA） | reward:edit / reward:approve |
| 跟单 | `GET copy/applications?status=pending\|active\|rejected\|closed\|all&page&pageSize`、`POST copy/applications/{uid}/approve`（2FA）、`…/reject`（reason + 2FA）；代理到 futures `/internal/copy/applications`，审计 module `copy`（high） | copy:view / copy:review |
| 邀请 | `GET referrals/{uid}`：邀请人、邀请码、邀请人数、返佣窗口结束、合伙人记录、按 kind 的佣金合计 | user:view |
| 积分活动 | `GET points/campaigns`；`POST points/campaigns {key,title,startsAt,endsAt,rules?,totpCode}`（2FA，high 审计，重叠拒绝） | reward:view / reward:edit |
| 用户卡券 | `GET users/{uid}/coupons`：券及实时赠金余额 | user:view |
| 合伙人 | `GET partners?status=pending\|active\|rejected\|disabled&page&pageSize`、`POST partners/{uid}/approve`（2FA）、`…/reject`、`…/disable`（reason + 2FA）；代理到 growth `/internal/partners`，审计 module `partner`（high） | partner:view / partner:review |
| 内容 | `GET/POST announcements`；公开 `GET /announcements` | content:edit |
| 审计 | `GET audit-logs?adminId&module&action&level&result&target&from&to&page&size` | audit:view |

流程：
- 金额 ≥ `ADMIN_WITHDRAW_SECOND_REVIEW_USDT`（5000）、风险 > `ADMIN_WITHDRAW_SECOND_REVIEW_RISK`（40）或没有 USDT 价格时，提现进入二次复核；第二人必须是另一个人（`SAME_REVIEWER`）。
- 费率变更：super_admin 提交；需要另一名 super_admin **和** 一名 finance_second 批准；然后排期至少 `ADMIN_FEE_NOTICE_HOURS`（24h）之后，并发公开公告。回滚走同一流程新建一条变更。
- 卡券模板预算（面值 × 数量）> `ADMIN_COUPON_FINANCE_THRESHOLD`（10000）时等 finance。
- 跟单带单申请（`FUTURES_COPY_AUTO_APPROVE=false`，默认）停在 `pending` 直到审核；只能处理 pending（`NOT_PENDING`）。被驳回的申请人在 `GET /futures/copy/trader` 上看到 `reviewNote`，可以再申请。角色：operations（查看 + 审核）、risk/auditor（查看）、super_admin（全部）。
- 手工奖励 > `ADMIN_REWARD_FINANCE_THRESHOLD`（1000 USDT）等 finance_second；提交人不能批自己的单（`SELF_APPROVAL`）。

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
