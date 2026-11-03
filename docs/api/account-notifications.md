# Message center (account service)

In-app notifications with an email channel. All user endpoints need a bearer
token and go through the gateway (`/notifications` → account).

Kinds: `system` | `trade` | `activity` | `security` (the UI tabs).

## User

| Method | Path | Notes |
|---|---|---|
| GET | `/notifications?kind=&page=&pageSize=` | `{items:[{id,kind,title,body,time(ms),read,emailStatus}], total, page, pageSize, unread, unreadByKind}`; newest first, pageSize ≤ 100 (default 20) |
| GET | `/notifications/unread-count` | `{unread, unreadByKind}` |
| POST | `/notifications/read` | `{ids:["12","13"]}` or `{all:true, kind?}` ("全部已读"); returns `{updated}`; only the caller's own notices |
| GET | `/notifications/settings` | `{emailSystem, emailTrade, emailActivity, emailSecurity}` |
| PUT | `/notifications/settings` | partial update; `emailSecurity:false` → 400 `SECURITY_EMAIL_REQUIRED` |

Defaults: system and trade emails on, activity off, security always on.
`emailStatus`: `none` (in-app only) · `pending` · `sent` · `failed` · `no_address` (user has no email; phone-only accounts).

## Producers (internal, `X-Internal-Token`)

- `POST /internal/notifications {uid, kind, title, body, email?}`: `email`
  true/false forces the channel; omitted uses the user's preferences. Go
  services call `common.Notify(common.Notice{...})` (async, best effort,
  needs `ACCOUNT_INTERNAL_URL` + `INTERNAL_TOKEN`).
- `POST /internal/notifications/broadcast {kind, title, body}`: in-app only, every active user.

Built-in hooks: new-device login and password reset (security, account);
deposit credited, withdrawal completed, and withdrawal rejected (trade, wallet);
KYC approved and rejected (security, kyc).

## Admin

- `POST /admin/v1/notifications/broadcast {kind,title,body,totpCode}`
- `POST /admin/v1/users/{uid}/notifications {kind,title,body,email?,totpCode}`

Both need `content:edit` and a fresh 2FA code, and are audited (module `notification`).

Email is sent via the account service's EmailSender (Resend when configured, otherwise the dev log mock).
