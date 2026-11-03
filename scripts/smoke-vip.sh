#!/usr/bin/env bash
# Smoke for VIP applications: apply (UID implicit, channel + contact), one open
# application at a time, admin RBAC, 2FA-gated approve/reject with audit,
# re-apply after rejection, ALREADY_VIP after approval, user notification.
set -euo pipefail
cd "$(dirname "$0")/.."

if [ "${SMOKE_INNER:-}" != "1" ]; then
  echo "== booting devstack =="
  ROOT="$(pwd)"
  cd tools/devstack
  exec go run . -root "$ROOT" -exec "SMOKE_INNER=1 bash scripts/smoke-vip.sh"
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
SMOKE_NAME="VIP"
. scripts/lib-growth-smoke.sh
[ -n "$SA_USER" ] && [ -n "$SA_PASS" ] && [ -n "$SA_TOTP" ] || fail "ADMIN_BOOTSTRAP_* not set"

APASS="Adm1n-Smoke-Passw0rd!"
pass() { printf "  ok  %s\n" "$1"; }
cj() { jq -r '.code // .error.code // empty' <<<"$1"; }
fresh() { local s; s=$(( 30 - $(date +%s) % 30 )); [ "$s" -lt 30 ] && sleep "$s"; return 0; }
AH() { curl -sS -o /dev/null -w '%{http_code}' -X "$2" "$ADMIN_URL$3" -H 'Content-Type: application/json' -H "Authorization: Bearer $1" ${4:+-d "$4"}; }

say "apply"
read -r T U <<<"$(register "vip-$TS@example.com")"
G="$(ok "$(api "$T" GET /vip/application)" get)"
[ "$(jq -r .status <<<"$G")" = none ] && [ "$(jq -r .vip <<<"$G")" = false ] && [ "$(jq -r .uid <<<"$G")" = "$U" ] || fail "initial: $G"
[ "$(jq -r .conditions.minTotalAssetsUSDT <<<"$G")" = 500000 ] || fail "conditions: $G"
R="$(api "$T" POST /vip/applications '{"channel":"carrier-pigeon","contact":"x"}')"; [ "$(code "$R")" = 400 ] || fail "bad channel accepted"
R="$(api "$T" POST /vip/applications '{"channel":"telegram","contact":" "}')"; [ "$(code "$R")" = 400 ] || fail "empty contact accepted"
G="$(ok "$(api "$T" POST /vip/applications '{"channel":"Telegram","contact":"@smoke_vip"}')" apply)"
[ "$(jq -r .status <<<"$G")" = pending ] && [ "$(jq -r .application.channel <<<"$G")" = telegram ] || fail "apply: $G"
R="$(api "$T" POST /vip/applications '{"channel":"telegram","contact":"@again"}')"
[ "$(code "$R")" = 409 ] && [ "$(errcode "$R")" = APPLICATION_PENDING ] || fail "double apply: $R"
VID="$(jq -r .application.id <<<"$G")"
pass "validation; pending application $VID; second open application refused"

say "admin RBAC + reject"
SA="$(alogin "$SA_USER" "$SA_PASS" "$SA_TOTP")"; [ -n "$SA" ] || fail "super admin login"
mk() {
  local R sec; R="$(A "$SA" POST /admin/v1/admins "{\"username\":\"$1${TS: -9}\",\"password\":\"$APASS\",\"roles\":[\"$2\"],\"totpCode\":\"$(totp "$SA_TOTP")\"}")"
  sec="$(jq -r '.totpSecret // empty' <<<"$R")"; [ -n "$sec" ] || fail "create $1: $R"
  printf -v "$1_SECRET" '%s' "$sec"; printf -v "$1_TOK" '%s' "$(alogin "$1${TS: -9}" "$APASS" "$sec")"
}
fresh; mk ops operations; fresh; mk sup support; fresh; mk kyc kyc_reviewer
[ "$(AH "$kyc_TOK" GET /admin/v1/vip/applications)" = 403 ] || fail "kyc reviewer can view vip"
[ "$(AH "$sup_TOK" GET /admin/v1/vip/applications)" = 200 ] || fail "support cannot view"
[ "$(AH "$sup_TOK" POST "/admin/v1/vip/applications/$VID/approve" '{}')" = 403 ] || fail "support can approve"
L="$(A "$ops_TOK" GET "/admin/v1/vip/applications?status=pending&pageSize=100")"
[ "$(jq -r --arg v "$VID" '.items[] | select(.id==$v) | .uid' <<<"$L")" = "$U" ] || fail "pending list: $L"
[ "$(cj "$(A "$ops_TOK" POST "/admin/v1/vip/applications/$VID/reject" "{\"totpCode\":\"$(totp "$ops_SECRET")\"}")")" = REASON_REQUIRED ] || fail "reject without reason"
[ "$(cj "$(A "$ops_TOK" POST "/admin/v1/vip/applications/$VID/reject" '{"reason":"资产未达标","totpCode":"000000"}')")" = TOTP_INVALID ] || fail "reject without 2FA"
R="$(A "$ops_TOK" POST "/admin/v1/vip/applications/$VID/reject" "{\"reason\":\"资产未达标\",\"totpCode\":\"$(totp "$ops_SECRET")\"}")"
[ "$(jq -r .status <<<"$R")" = rejected ] || fail "reject: $R"
G="$(ok "$(api "$T" GET /vip/application)" get)"
[ "$(jq -r .status <<<"$G")" = rejected ] && [ "$(jq -r .application.reason <<<"$G")" = "资产未达标" ] || fail "user view after reject: $G"
pass "kyc_reviewer 403; support view-only; reason + 2FA required; rejection visible to user"

say "re-apply + approve"
G="$(ok "$(api "$T" POST /vip/applications '{"channel":"whatsapp","contact":"+15550001111"}')" reapply)"
VID2="$(jq -r .application.id <<<"$G")"; [ "$VID2" != "$VID" ] || fail "re-apply reused id"
fresh
R="$(A "$ops_TOK" POST "/admin/v1/vip/applications/$VID2/approve" "{\"totpCode\":\"$(totp "$ops_SECRET")\"}")"
[ "$(jq -r .status <<<"$R")" = approved ] || fail "approve: $R"
fresh
[ "$(cj "$(A "$ops_TOK" POST "/admin/v1/vip/applications/$VID2/approve" "{\"totpCode\":\"$(totp "$ops_SECRET")\"}")")" = NOT_PENDING ] || fail "double approve"
G="$(ok "$(api "$T" GET /vip/application)" get)"
[ "$(jq -r .vip <<<"$G")" = true ] || fail "not vip: $G"
R="$(api "$T" POST /vip/applications '{"channel":"telegram","contact":"@x"}')"
[ "$(code "$R")" = 409 ] && [ "$(errcode "$R")" = ALREADY_VIP ] || fail "apply when vip: $R"
for i in $(seq 1 30); do
  [ "$(ok "$(api "$T" GET '/notifications?kind=system')" n | jq -r '.items[0].title')" = "VIP 申请已通过" ] && break; sleep 0.2
done
[ "$(ok "$(api "$T" GET '/notifications?kind=system')" n | jq -r '.items[0].title')" = "VIP 申请已通过" ] || fail "no approval notice"
[ "$(A "$SA" GET "/admin/v1/audit-logs?module=vip&size=50" | jq -r '[.items[]|select(.action=="approve" and .result=="ok")][0].result')" = ok ] || fail "approve not audited"
[ "$(A "$SA" GET "/admin/v1/audit-logs?module=vip&size=50" | jq -r '[.items[]|select(.action=="reject" and .result=="ok")][0].reason')" = "资产未达标" ] || fail "reject not audited"
pass "re-apply after reject; approve -> vip=true; ALREADY_VIP; notice sent; decisions audited"

printf '\n\033[1;32mSMOKE-VIP PASSED\033[0m — apply -> RBAC -> reject(2FA, reason) -> re-apply -> approve -> notice -> audit\n'
