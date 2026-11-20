# Linbit — Ops Checklist

Last audit: 2026-11-20 against prod host **74.113.233.169** (Kamatera Tokyo,
12 vCPU / 8 GB / 100 GB, Ubuntu 24.04).

The source of truth for every runtime variable the mainnet stack reads is
[`deploy/mainnet/.env.mainnet.example`](../../deploy/mainnet/.env.mainnet.example)
(the repo root keeps `.env.example` for dev). Each section here is **what the operator
has to do by hand**; the env var it fills in is called out in `monospace` so
you can grep.

---

## 0. Where things live on the box

| Path | What |
| --- | --- |
| `/opt/linbit/` | symlink → the current release tree (`/opt/linbit-<rev>`) |
| `/opt/linbit/.env` | runtime environment, NOT in git — survives deploys |
| `/opt/linbit/secrets/` | file-based secrets mounted into containers |
| `/opt/linbit/docker-compose.override.yml` | host-specific overrides (web/partner image refs) |
| `/data/kyc-docs/` | docker volume `kycdocs`, included in backups |
| `/data/backups/linbit/` | pg_dump snapshots, pre-deploy copies |
| `/root/b.sh` | the bare build loop the old ops used |
| `/root/deploy.sh` | **new** — install via `scp docs/ops/deploy.sh /root/` |
| `/root/deploy-logs/` | one build log per revision |
| `~/.linbit-ops/n '<cmd>'` | SSH helper on the laptop (key login) |

Compose command (keep this literal — every piece matters):

```bash
docker compose -p linbit \
  -f docker-compose.yml \
  -f docker-compose.override.yml \
  -f deploy/mainnet/docker-compose.mainnet.yml \
  --profile chain <subcommand>
```

---

## 1. Google Sign-In (`/auth/google` → `GOOGLE_UNCONFIGURED`)

The backend reads **`GOOGLE_OAUTH_CLIENT_IDS`** (CSV of allowed `aud` values),
*not* a single `GOOGLE_CLIENT_ID`. The brief used the old name — the code
([`services/go/account/config.go:137`](../../services/go/account/config.go))
names the plural form.

### Create the credentials

1. <https://console.cloud.google.com/apis/credentials> → project → **Create Credentials → OAuth client ID**.
2. One client **per platform** (same project):
   - **iOS** — Bundle ID `click.cowa.linbit` (used by the Apple app).
   - **Web application** — Authorized JS origins `https://www.cowa.click`, `https://cowa.click`; Authorized redirect URIs `https://www.cowa.click/auth/google/callback`.
   - **Android** — package `click.cowa.linbit`, SHA-1 of the signing cert
     (`keytool -list -v -keystore release.keystore -alias <alias>`).
3. Collect every `client_id` the Google console issued.

### Install on the server

```bash
# dry-run first — never write with `set -e` pipes; the >> is unforgiving
~/.linbit-ops/n 'grep -n ^GOOGLE_OAUTH_CLIENT_IDS /opt/linbit/.env || \
  echo "GOOGLE_OAUTH_CLIENT_IDS=ios-id.apps.googleusercontent.com,web-id.apps.googleusercontent.com,android-id.apps.googleusercontent.com" >> /opt/linbit/.env'

~/.linbit-ops/n 'cd /opt/linbit && docker compose -p linbit \
  -f docker-compose.yml -f docker-compose.override.yml \
  -f deploy/mainnet/docker-compose.mainnet.yml \
  --profile chain up -d --no-deps --no-build account'
```

### Smoke test

```bash
curl -s https://api.cowa.click/auth/google \
  -H 'content-type: application/json' \
  -d '{"idToken":"deliberately-bogus"}' | jq .
# expected: code != GOOGLE_UNCONFIGURED  (GOOGLE_TOKEN_INVALID is fine — it means the client ID was picked up)
```

---

## 2. Apple Push Notifications (APNS)

**Not implemented in code yet.** The `.env.sample` reserves the variable names
so the first push build can pick them up without an env round-trip.

### Preferred: APNs auth key (`.p8`)

1. <https://developer.apple.com/account/resources/authkeys/list> → **+** → tick
   "Apple Push Notifications service (APNs)", name it `linbit-apns`.
2. **Download the `.p8` once** (Apple won't let you download it a second time).
3. Record **Key ID** (shown on the key page) and **Team ID**
   (top-right of the developer console).

### Install on the server

```bash
~/.linbit-ops/n 'mkdir -p /opt/linbit/secrets/apns && chmod 700 /opt/linbit/secrets/apns'
scp -o ControlPath=$HOME/.linbit-ops/cm-%r@%h \
  AuthKey_XXXXXXXXXX.p8 \
  root@74.113.233.169:/opt/linbit/secrets/apns/
~/.linbit-ops/n 'chmod 600 /opt/linbit/secrets/apns/AuthKey_*.p8'

# Append to .env (fill in the real values)
cat <<'EOF' | ~/.linbit-ops/n 'cat >> /opt/linbit/.env'

# --- APNS ---
APNS_KEY_ID=XXXXXXXXXX
APNS_TEAM_ID=XXXXXXXXXX
APNS_KEY_PATH=/opt/linbit/secrets/apns/AuthKey_XXXXXXXXXX.p8
APNS_BUNDLE_ID=click.cowa.linbit
APNS_ENV=production
EOF
```

### Code TODO (a separate PR, not this one)

- New module `services/go/push/` or `services/go/account/apns.go` using
  [`github.com/sideshow/apns2`](https://github.com/sideshow/apns2).
- Store per-user device tokens on `/account/push/register` (iOS calls this
  from `application(_:didRegisterForRemoteNotificationsWithDeviceToken:)`).
- Replace the current Resend-only notification path with a fan-out:
  email + APNS when the user has a registered token.
- Smoke test: `POST /internal/dev/push/test {userId}` (dev-mode only).

### Fallback: `.p12` cert

Only use this if `.p8` is genuinely impossible (e.g. no admin rights in the
Apple dev account). Certs expire yearly and need re-uploading; `.p8` keys
don't. The env vars for the cert path (`APNS_CERT_PATH`, `APNS_CERT_PASS`)
are NOT reserved — avoid this path.

---

## 3. HTML email templates (Resend)

Current state: all transactional mails go through `services/go/account/email.go`
and are **plain text**. The spec calls for branded HTML with text fallback for
six events:

| Template | Trigger |
| --- | --- |
| `register.html` | account created (verify email) |
| `login_2fa.html` | second-factor code |
| `reset.html` | password / fund-password reset link |
| `deposit_success.html` | deposit credited |
| `withdrawal_success.html` | withdraw confirmed on-chain |
| `security_alert.html` | new device, 2FA disable, large withdraw |

### Where the code should live

```
services/go/account/
├── email.go             # existing sender
└── templates/
    ├── _layout.html     # header/footer, Linbit wordmark, dark-mode-safe CSS
    ├── register.html    # extends _layout
    ├── ...
    └── text/            # mirror .txt fallbacks for text/plain parts
```

Use Go `html/template`; register each pair via Resend's `html` + `text`
fields so clients that strip HTML still render something readable.

### Testing

1. Set `RESEND_API_KEY` to a sandbox key (Resend gives test keys in-console).
2. `go run ./services/go/account/cmd/testmail -to you@example.com -kind register`
   (needs a tiny CLI harness — write once, keep in `tools/`).
3. Screenshot each render on Gmail web, Gmail iOS, Apple Mail macOS.

This is a code change → **separate PR**, not shipped here.

---

## 4. Telegram bot

Two bots for two jobs — don't conflate them.

### 4a. User-facing login + binding bot

1. On Telegram → **@BotFather** → `/newbot`.
   - Name: `Linbit Notify`
   - Username: `linbit_notify_bot` (must end in `_bot`; try
     `@linbit_cowa_bot` if the first is taken).
2. Copy the token BotFather prints.
3. `/setdomain` in BotFather → send the bot's username → send the login-widget
   origin: `www.cowa.click` (and any other host serving the widget).
4. (optional) `/setprivacy` → **disable** if you want the bot to receive
   non-command messages in groups; leave enabled otherwise.

Install:

```bash
~/.linbit-ops/n 'sed -i "s|^TELEGRAM_BOT_TOKEN=.*|TELEGRAM_BOT_TOKEN=<paste>|" /opt/linbit/.env'
~/.linbit-ops/n 'cd /opt/linbit && docker compose -p linbit \
  -f docker-compose.yml -f docker-compose.override.yml \
  -f deploy/mainnet/docker-compose.mainnet.yml \
  --profile chain up -d --no-deps --no-build account'
```

Smoke test — PR #130 (`feat/account-deletion-tg`) must be merged first:

```bash
curl -s https://api.cowa.click/account/telegram \
  -H 'authorization: Bearer <access-token>' | jq .
# expected: {"bound":false,"available":true}
```

### 4b. Ops-alert bot (optional, step 6 below)

Separate bot, token in `TELEGRAM_BOT_TOKEN_ALERT`, chat id in
`TELEGRAM_CHAT_ID`. Alertmanager posts to
`https://api.telegram.org/bot<TOKEN>/sendMessage`. Don't reuse the user-login
token — rotating that kills alerts too.

---

## 5. Deploy automation (`/root/deploy.sh`)

Install it:

```bash
scp -o ControlPath=$HOME/.linbit-ops/cm-%r@%h \
  docs/ops/deploy.sh root@74.113.233.169:/root/deploy.sh
~/.linbit-ops/n 'chmod +x /root/deploy.sh'
```

Usage:

```bash
# Deploy the current main of account + gateway
~/.linbit-ops/n '/root/deploy.sh account gateway'

# Deploy a feature branch's HEAD
~/.linbit-ops/n '/root/deploy.sh --ref feat/account-deletion-tg account'

# Deploy a specific SHA, everything
~/.linbit-ops/n '/root/deploy.sh --ref 67208e7c --all'
```

What it does, in order:

1. Refuses to run if root disk free < 10 %.
2. `git archive` a shallow clone of the ref into `/opt/linbit-<sha>`.
3. Symlinks `.env` and `secrets/` from the current tree so runtime state
   survives.
4. Flips the `/opt/linbit` symlink.
5. Builds each service with 3-retry, same loop as `/root/b.sh`.
6. `compose up -d --no-deps --no-build <service>` (sibling services keep
   running on their current image).
7. Polls `http://127.0.0.1:<port>/healthz` for 60 s per service.
8. On ANY failure: flips the symlink back, restarts the services from the
   previous image (`linbit/<svc>:keep-pre-<rev>`), exits non-zero.
9. Appends a line to `/opt/linbit/REVISION.components`.

Rollback manually (if you need to pin to a known-good rev):

```bash
~/.linbit-ops/n 'ln -sfn /opt/linbit-<old-rev> /opt/linbit && \
  cd /opt/linbit && docker compose -p linbit \
    -f docker-compose.yml -f docker-compose.override.yml \
    -f deploy/mainnet/docker-compose.mainnet.yml --profile chain \
    up -d --no-deps --no-build <svc>'
```

### Limits we deliberately did NOT handle

- No pg_dump — the operator should still run the pre-deploy backup
  (`/data/backups/linbit/predeploy-<ts>`) when a migration is in the diff.
- No migration gating — migrations live in each service's startup; the
  healthcheck catches a broken migrate, but a slow one can time out.
- The `keep-pre-<rev>` tag is only taken for services already running; a
  brand-new service rollback falls back to `docker compose up` of whatever
  the override file points at.

---

## 6. Monitoring (optional, recommended)

Add a second compose file, kept outside the main stack so a monitor crash
never reaches the exchange:

```
/opt/linbit/
└── docker-compose.monitor.yml
```

Stack:

- **prometheus** (`prom/prometheus:v2.55`), config in
  `/opt/linbit-monitor/prometheus.yml`, scrapes each service's `/metrics`.
- **grafana** (`grafana/grafana:11.2`), bind to `127.0.0.1:3001`, nginx
  terminates TLS at `metrics-<slug>.cowa.click`.
- **loki** + **promtail** for container logs
  (`/var/lib/docker/containers/*/*-json.log`).
- **alertmanager** → Telegram webhook from §4b.

Starter dashboards:

| Panel | Query |
| --- | --- |
| per-service QPS | `sum by (job)(rate(http_requests_total[1m]))` |
| p95 latency | `histogram_quantile(0.95, sum by (le,job)(rate(http_request_duration_seconds_bucket[5m])))` |
| 5xx rate | `sum by (job)(rate(http_requests_total{status=~"5.."}[1m]))` |
| container CPU | `rate(container_cpu_usage_seconds_total{name=~"linbit-.*"}[1m])` |
| container RSS | `container_memory_working_set_bytes{name=~"linbit-.*"}` |
| disk free | `node_filesystem_avail_bytes{mountpoint="/"}` |
| ingress bandwidth (pinned near 600 KB/s) | `rate(node_network_receive_bytes_total[1m])` |

Alerts to start with:

- Any service `/healthz` down > 2 min.
- 5xx rate > 1 req/s for 5 min.
- Postgres connections > 80% of `max_connections`.
- Root disk free < 15 %.
- Bandwidth sustained > 80 % of the 5 Mbps cap for 10 min
  (symptom of a new feed turning on by accident).

Compose file + sample `prometheus.yml` are **not** in this PR; see the
follow-up branch `ops/monitor-stack` once the ops bot token is issued.

---

## 7. Config reconciliation

Current `/opt/linbit/.env` (as of 2026-11-20, redacted) is missing:

| Variable | Why it matters |
| --- | --- |
| `GOOGLE_OAUTH_CLIENT_IDS` | `/auth/google` returns `GOOGLE_UNCONFIGURED` without it |
| `GOOGLE_OAUTH_JWKS_URL` | has a sane default; add if you ever point to a different JWKS mirror |
| `APNS_*` (5 vars) | pre-provisioned so the push PR drops in |
| `ACCOUNT_DELETION_SWEEP_SECONDS` | default 600; only override to speed up tests |
| `DCA_*` (4 vars) | all have defaults; pin them in prod for explicit review |
| `STAKING_TICK_MS` | default 60000 |
| `SPOT_MAKER_FEE_RATE` / `SPOT_TAKER_FEE_RATE` | defaults exist but prod should pin them |
| `AVATAR_PUBLIC_BASE` | needed once we move avatars behind a CDN |

The rest of the mainnet example matches the current file. To diff:

```bash
~/.linbit-ops/n 'grep -oE "^[A-Z_][A-Z0-9_]*" /opt/linbit/.env | sort -u' > /tmp/env.live
grep -oE '^[A-Z_][A-Z0-9_]*' deploy/mainnet/.env.mainnet.example | sort -u > /tmp/env.sample
diff /tmp/env.live /tmp/env.sample
```

---

## 8. Dependency summary (don't lose track)

| External thing | Who provisions | What breaks without it |
| --- | --- | --- |
| Postgres `linbit` DB | self-hosted, docker volume `pgdata` | everything |
| Redis | self-hosted, docker volume `redisdata` | sessions, rate-limiters |
| Resend API key | ops in Resend dashboard | all email (register, 2FA, security) |
| Google OAuth client IDs | ops in Google Cloud Console | `/auth/google` |
| APNS `.p8` key | ops in Apple Developer | iOS push (once code lands) |
| Telegram bot token (user) | ops via @BotFather | `/account/bind-telegram`, login widget |
| Telegram bot token (alerts) | ops via @BotFather | alertmanager → TG |
| Wallet RPC endpoints (BTC/ETH/BSC/SOL/TRX/ARB) | ops in provider dashboards | deposits/withdraws on that chain |
| HD xpubs (BTC/EVM/TRON) | finance (cold-signer) | deposit address derivation |
| Cold-wallet addresses | finance | sweeper target |
| S3 bucket (optional) | ops in AWS/R2 | only if `STORAGE_BACKEND=s3` |
| DNS: `cowa.click`, `*.cowa.click` | Cloudflare | public reachability, geo-block |
| TLS: `*.cowa.click` | Cloudflare managed | HTTPS |
| iOS code-signing cert | mobile team | App Store builds |
| Google Play upload key | mobile team | Play Store builds |

---

## 9. Routine checks (daily / weekly)

```bash
# daily — root disk + build cache
~/.linbit-ops/n 'df -h / && docker system df'

# weekly — prune builder cache older than 24h (safe)
~/.linbit-ops/n 'docker builder prune -f --filter until=168h'

# weekly — verify every container is up and healthy
~/.linbit-ops/n 'docker compose -p linbit ps --format "{{.Name}} {{.Status}}"'

# weekly — tail the last deploy log
~/.linbit-ops/n 'ls -lt /root/deploy-logs | head -5'
```

PG backups (nightly cron is already wired per the old deploy notes — if
missing, add):

```cron
15 2 * * * docker exec linbit-postgres-1 pg_dump -U linbit linbit | \
  gzip > /data/backups/linbit/linbit-$(date +\%F).sql.gz
```
