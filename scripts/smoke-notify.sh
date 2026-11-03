#!/usr/bin/env bash
# Smoke for the message center: in-app notifications (kinds, unread counts,
# per-item / all read, isolation), the email channel (preferences; security is
# always emailed; delivery status via the dev mock), the new-device login hook,
# the internal producer API, and admin broadcast / single-user send (2FA+audit).
set -euo pipefail
cd "$(dirname "$0")/.."

if [ "${SMOKE_INNER:-}" != "1" ]; then
  echo "== booting devstack =="
  ROOT="$(pwd)"
  cd tools/devstack
  exec go run . -root "$ROOT" -exec "SMOKE_INNER=1 bash scripts/smoke-notify.sh"
fi

GATEWAY_URL="${GATEWAY_URL:-http://localhost:8080}"
ASSET_URL="${ASSET_URL:-http://localhost:8082}"
ACCOUNT_URL="${ACCOUNT_URL:-http://localhost:8081}"
ADMIN_URL="${ADMIN_URL:-http://localhost:8088}"
INTERNAL_TOKEN="${INTERNAL_TOKEN:-}"
SA_USER="${ADMIN_BOOTSTRAP_USERNAME:-}"
SA_PASS="${ADMIN_BOOTSTRAP_PASSWORD:-}"
SA_TOTP="${ADMIN_BOOTSTRAP_TOTP_SECRET:-}"
PASSWORD="Str0ng-Passw0rd!"
TS="$(date +%s%N)"
SMOKE_NAME="NOTIFY"
. scripts/lib-growth-smoke.sh
[ -n "$SA_USER" ] && [ -n "$SA_PASS" ] && [ -n "$SA_TOTP" ] || fail "ADMIN_BOOTSTRAP_* not set"

pass() { printf "  ok  %s\n" "$1"; }
cj() { jq -r '.code // .error.code // empty' <<<"$1"; }
fresh() { local s; s=$(( 30 - $(date +%s) % 30 )); [ "$s" -lt 30 ] && sleep "$s"; return 0; }
notify() { internal "$ACCOUNT_URL" POST /internal/notifications "$1"; }
# latest <token> <kind> <field>
latest() { ok "$(api "$1" GET "/notifications?kind=$2")" "list $2" | jq -r ".items[0].$3"; }
wait_email() { # wait_email <token> <kind> <want>
  local i st
  for i in $(seq 1 30); do st="$(latest "$1" "$2" emailStatus)"; [ "$st" = "$3" ] && return 0; sleep 0.2; done
  fail "email status for $2: got $st, want $3"
}

say "register"
EMAIL="notify-$TS@example.com"
read -r T U <<<"$(register "$EMAIL")"
read -r T2 U2 <<<"$(register "notify2-$TS@example.com")"
L="$(ok "$(api "$T" GET /notifications)" list)"
[ "$(jq -r .unread <<<"$L")" = 0 ] && [ "$(jq -r .total <<<"$L")" = 0 ] || fail "fresh inbox not empty: $L"
P="$(ok "$(api "$T" GET /notifications/settings)" prefs)"
[ "$(jq -c '[.emailSystem,.emailTrade,.emailActivity,.emailSecurity]' <<<"$P")" = '[true,true,false,true]' ] || fail "default prefs: $P"
pass "empty inbox; default email prefs system/trade on, activity off, security forced"

say "new-device login -> security notice + email"
LOGIN="$(curl -sS -X POST "$GATEWAY_URL/auth/login" -H 'Content-Type: application/json' \
  -d "{\"email\":\"$EMAIL\",\"password\":\"$PASSWORD\",\"deviceId\":\"notify-dev-$TS\",\"deviceName\":\"Smoke-Phone\"}")"
CH="$(jq -r '.challengeToken // empty' <<<"$LOGIN")"; [ -n "$CH" ] || fail "no challenge: $LOGIN"
LC="$(curl -sS -G "$ACCOUNT_URL/internal/dev/last-code" --data-urlencode "target=$EMAIL" --data-urlencode "purpose=login" \
  -H "X-Internal-Token: $INTERNAL_TOKEN" | jq -r '.code // empty')"
V="$(curl -sS -X POST "$GATEWAY_URL/auth/login/verify" -H 'Content-Type: application/json' -d "{\"challengeToken\":\"$CH\",\"code\":\"$LC\"}")"
T="$(jq -r '.accessToken // empty' <<<"$V")"; [ -n "$T" ] || fail "verify: $V"
for i in $(seq 1 30); do [ "$(latest "$T" security title)" = "新设备登录提醒" ] && break; sleep 0.2; done
[ "$(latest "$T" security title)" = "新设备登录提醒" ] || fail "no new-device notice"
latest "$T" security body | grep -q "Smoke-Phone" || fail "notice lacks device name"
wait_email "$T" security sent
pass "new-device notice stored and emailed (status sent)"

say "producer API + preferences"
[ "$(curl -sS -o /dev/null -w '%{http_code}' -X POST "$ACCOUNT_URL/internal/notifications" -d '{}')" = 401 ] || fail "internal without token"
[ "$(cj "$(notify "{\"uid\":\"$U\",\"kind\":\"spam\",\"title\":\"x\",\"body\":\"y\"}")")" = VALIDATION_ERROR ] || fail "bad kind accepted"
[ "$(cj "$(notify '{"uid":"nope","kind":"trade","title":"x","body":"y"}')")" = USER_NOT_FOUND ] || fail "unknown user"
notify "{\"uid\":\"$U\",\"kind\":\"trade\",\"title\":\"充值到账\",\"body\":\"100 USDT 已到账\"}" >/dev/null
wait_email "$T" trade sent
notify "{\"uid\":\"$U\",\"kind\":\"activity\",\"title\":\"新活动\",\"body\":\"欢迎参加\"}" >/dev/null
[ "$(latest "$T" activity emailStatus)" = none ] || fail "activity emailed by default"
R="$(api "$T" PUT /notifications/settings '{"emailSecurity":false}')"
[ "$(code "$R")" = 400 ] && [ "$(errcode "$R")" = SECURITY_EMAIL_REQUIRED ] || fail "security email switched off: $R"
P="$(ok "$(api "$T" PUT /notifications/settings '{"emailTrade":false,"emailActivity":true}')" prefs)"
[ "$(jq -c '[.emailSystem,.emailTrade,.emailActivity]' <<<"$P")" = '[true,false,true]' ] || fail "prefs update: $P"
notify "{\"uid\":\"$U\",\"kind\":\"trade\",\"title\":\"提现成功\",\"body\":\"50 USDT\"}" >/dev/null
[ "$(latest "$T" trade emailStatus)" = none ] || fail "trade emailed after opt-out"
notify "{\"uid\":\"$U\",\"kind\":\"trade\",\"title\":\"强制邮件\",\"body\":\"z\",\"email\":true}" >/dev/null
wait_email "$T" trade sent
pass "401/400/404 guarded; prefs respected; security cannot opt out; email:true forces"

say "unread counts + read"
C="$(ok "$(api "$T" GET /notifications/unread-count)" unread)"
[ "$(jq -r .unread <<<"$C")" = 5 ] && [ "$(jq -r .unreadByKind.trade <<<"$C")" = 3 ] || fail "unread counts: $C"
[ "$(ok "$(api "$T" GET '/notifications?kind=trade&pageSize=2')" page | jq '.items|length')" = 2 ] || fail "pagination"
ID="$(latest "$T" activity id)"
[ "$(ok "$(api "$T2" POST /notifications/read "{\"ids\":[\"$ID\"]}")" iso | jq -r .updated)" = 0 ] || fail "other user marked my notice"
[ "$(ok "$(api "$T" POST /notifications/read "{\"ids\":[\"$ID\"]}")" read1 | jq -r .updated)" = 1 ] || fail "read one"
[ "$(latest "$T" activity read)" = true ] || fail "read flag"
[ "$(ok "$(api "$T" POST /notifications/read '{"all":true,"kind":"trade"}')" readk | jq -r .updated)" = 3 ] || fail "read all trade"
[ "$(code "$(api "$T" POST /notifications/read '{}')")" = 400 ] || fail "empty read accepted"
ok "$(api "$T" POST /notifications/read '{"all":true}')" readall >/dev/null
[ "$(ok "$(api "$T" GET /notifications/unread-count)" u | jq -r .unread)" = 0 ] || fail "unread after read-all"
pass "counts by kind; pagination; per-item, per-kind and all read; isolated per user"

say "admin broadcast / single send (2FA + audit)"
SA="$(alogin "$SA_USER" "$SA_PASS" "$SA_TOTP")"; [ -n "$SA" ] || fail "super admin login"
fresh
R="$(A "$SA" POST /admin/v1/notifications/broadcast '{"kind":"system","title":"系统维护","body":"今晚维护","totpCode":"000000"}')"
[ "$(cj "$R")" = TOTP_INVALID ] || fail "broadcast without 2FA: $R"
R="$(A "$SA" POST /admin/v1/notifications/broadcast "{\"kind\":\"system\",\"title\":\"系统维护 $TS\",\"body\":\"今晚维护\",\"totpCode\":\"$(totp "$SA_TOTP")\"}")"
[ "$(jq -r '.recipients // 0' <<<"$R")" -ge 2 ] || fail "broadcast: $R"
[ "$(latest "$T2" system title)" = "系统维护 $TS" ] || fail "user2 missing broadcast"
[ "$(latest "$T" system emailStatus)" = none ] || fail "broadcast emailed"
fresh
R="$(A "$SA" POST "/admin/v1/users/$U/notifications" "{\"kind\":\"system\",\"title\":\"账户提示\",\"body\":\"请完成认证\",\"email\":true,\"totpCode\":\"$(totp "$SA_TOTP")\"}")"
[ -n "$(jq -r '.id // empty' <<<"$R")" ] || fail "single send: $R"
wait_email "$T" system sent
[ "$(A "$SA" GET "/admin/v1/audit-logs?module=notification&action=broadcast" | jq -r '.items[0].result')" = ok ] || fail "broadcast not audited"
[ "$(A "$SA" GET "/admin/v1/audit-logs?module=notification&action=send" | jq -r '.items[0].result')" = ok ] || fail "send not audited"
pass "broadcast in-app to all users; single send with email; both 2FA-gated and audited"

printf '\n\033[1;32mSMOKE-NOTIFY PASSED\033[0m — inbox -> new-device hook -> email prefs -> unread/read -> admin broadcast/send -> audit\n'
