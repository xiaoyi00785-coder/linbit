#!/usr/bin/env bash
# Smoke for the earn/loan product admin APIs: RBAC, 2FA, audit, product CRUD,
# stock limits (incl. TERM_QUOTA_EXCEEDED on a real borrow) and rate changes
# through dual approval + notice period + scheduler + rollback.
set -euo pipefail
cd "$(dirname "$0")/.."

if [ "${SMOKE_INNER:-}" != "1" ]; then
  echo "== booting devstack =="
  ROOT="$(pwd)"
  cd tools/devstack
  exec go run . -root "$ROOT" -exec "SMOKE_INNER=1 bash scripts/smoke-earn-admin.sh"
fi

GATEWAY_URL="${GATEWAY_URL:-http://localhost:8080}"
ASSET_URL="${ASSET_URL:-http://localhost:8082}"
ACCOUNT_URL="${ACCOUNT_URL:-http://localhost:8081}"
FUTURES_URL="${FUTURES_URL:-${FUTURES_INTERNAL_URL:-http://localhost:8089}}"
GROWTH_URL="${GROWTH_URL:-http://localhost:8091}"
EARN_URL="${EARN_URL:-http://localhost:8092}"
ADMIN_URL="${ADMIN_URL:-http://localhost:8088}"
INTERNAL_TOKEN="${INTERNAL_TOKEN:-}"
SA_USER="${ADMIN_BOOTSTRAP_USERNAME:-}"
SA_PASS="${ADMIN_BOOTSTRAP_PASSWORD:-}"
SA_TOTP="${ADMIN_BOOTSTRAP_TOTP_SECRET:-}"
PASSWORD="Str0ng-Passw0rd!"
APASS="Adm1n-Smoke-Passw0rd!"
TS="$(date +%s%N)"
SMOKE_NAME="EARN-ADMIN"
. scripts/lib-growth-smoke.sh
[ -n "$SA_USER" ] && [ -n "$SA_PASS" ] && [ -n "$SA_TOTP" ] || fail "ADMIN_BOOTSTRAP_* not set"

cj() { jq -r '.code // .error.code // empty' <<<"$1"; }
AH() { curl -sS -o /dev/null -w '%{http_code}' -X "$2" "$ADMIN_URL$3" -H 'Content-Type: application/json' -H "Authorization: Bearer $1" ${4:+-d "$4"}; }
# totp codes are single-use per 30s window: wait for a fresh window when needed
pass() { printf "  ok  %s\n" "$1"; }
fresh() { local s; s=$(( 30 - $(date +%s) % 30 )); [ "$s" -lt 30 ] && sleep "$s"; return 0; }

say "admins"
SA="$(alogin "$SA_USER" "$SA_PASS" "$SA_TOTP")"; [ -n "$SA" ] || fail "super admin login"
mk() {
  local R sec; R="$(A "$SA" POST /admin/v1/admins "{\"username\":\"$1${TS: -9}\",\"password\":\"$APASS\",\"roles\":[\"$2\"],\"totpCode\":\"$(totp "$SA_TOTP")\"}")"
  sec="$(jq -r '.totpSecret // empty' <<<"$R")"; [ -n "$sec" ] || fail "create $1: $(cj "$R")"
  printf -v "$1_SECRET" '%s' "$sec"
  printf -v "$1_TOK" '%s' "$(alogin "$1${TS: -9}" "$APASS" "$sec")"
}
mk ops operations; fresh; mk fin2 finance_second; fresh; mk sup support; fresh; mk sa2 super_admin; fresh; mk aud auditor
pass "operations / finance_second / support / super_admin#2 / auditor created"

say "RBAC"
[ "$(AH "$sup_TOK" GET /admin/v1/earn/products)" = 403 ] || fail "support can list earn products"
[ "$(AH "$aud_TOK" GET /admin/v1/earn/products)" = 200 ] || fail "auditor cannot view"
[ "$(AH "$aud_TOK" PATCH /admin/v1/earn/products/simple-USDT '{"hot":true}')" = 403 ] || fail "auditor can edit"
[ "$(AH "$fin2_TOK" POST /admin/v1/loans/collaterals '{"asset":"SOL","cap":"1"}')" = 403 ] || fail "finance can edit collaterals"
[ "$(AH "$ops_TOK" GET /admin/v1/loans/terms)" = 200 ] || fail "operations cannot view terms"
pass "support 403; auditor view-only; finance cannot edit products"

say "product CRUD + 2FA + audit"
PID="smoke-$TS"
R="$(A "$ops_TOK" POST /admin/v1/earn/products "{\"id\":\"$PID\",\"kind\":\"fixed\",\"asset\":\"USDT\",\"title\":\"冒烟定期\",\"apr\":\"0.05\",\"termDays\":30,\"minAmount\":\"10\",\"reason\":\"x\",\"totpCode\":\"000000\"}")"
[ "$(cj "$R")" = TOTP_INVALID ] || fail "create without 2FA: $R"
R="$(A "$ops_TOK" POST /admin/v1/earn/products "{\"id\":\"$PID\",\"kind\":\"fixed\",\"asset\":\"USDT\",\"title\":\"冒烟定期\",\"apr\":\"5\",\"termDays\":30,\"minAmount\":\"10\",\"reason\":\"x\",\"totpCode\":\"$(totp "$ops_SECRET")\"}")"
[ "$(cj "$R")" = VALIDATION_ERROR ] || fail "apr 500% accepted: $R"
fresh
R="$(A "$ops_TOK" POST /admin/v1/earn/products "{\"id\":\"$PID\",\"kind\":\"fixed\",\"asset\":\"USDT\",\"title\":\"冒烟定期\",\"apr\":\"0.05\",\"termDays\":30,\"minAmount\":\"10\",\"reason\":\"新品\",\"totpCode\":\"$(totp "$ops_SECRET")\"}")"
[ -z "$(cj "$R")" ] || fail "create: $R"
P="$(A "$ops_TOK" GET "/admin/v1/earn/products/$PID")"
[ "$(jq -r .status <<<"$P")" = offline ] || fail "new product not offline by default: $P"
fresh
R="$(A "$ops_TOK" POST /admin/v1/earn/products "{\"id\":\"$PID\",\"kind\":\"fixed\",\"asset\":\"USDT\",\"title\":\"x\",\"apr\":\"0.9\",\"termDays\":30,\"minAmount\":\"10\",\"reason\":\"x\",\"totpCode\":\"$(totp "$ops_SECRET")\"}")"
[ "$(cj "$R")" = PRODUCT_EXISTS ] || fail "re-create overwrote: $R"
eq "$(A "$ops_TOK" GET "/admin/v1/earn/products/$PID" | jq -r .apr)" 0.05 "apr after re-create attempt"
fresh
R="$(A "$ops_TOK" PATCH "/admin/v1/earn/products/$PID" "{\"apr\":\"0.9\",\"reason\":\"x\",\"totpCode\":\"$(totp "$ops_SECRET")\"}")"
[ "$(cj "$R")" = RATE_CHANGE_REQUIRES_APPROVAL ] || fail "PATCH apr allowed: $R"
R="$(A "$ops_TOK" PATCH "/admin/v1/earn/products/$PID" "{\"title\":\"冒烟定期 30 天\",\"status\":\"online\",\"hot\":true,\"reason\":\"上架\",\"totpCode\":\"$(totp "$ops_SECRET")\"}")"
[ -z "$(cj "$R")" ] || fail "patch: $R"
[ "$(curl -sS "$GATEWAY_URL/earn/products/$PID" | jq -r .title)" = "冒烟定期 30 天" ] || fail "public product not updated"
fresh
R="$(A "$ops_TOK" PUT "/admin/v1/earn/products/$PID/quota" "{\"totalQuota\":\"100000\",\"perUserMax\":\"500\",\"reason\":\"限额\",\"totpCode\":\"$(totp "$ops_SECRET")\"}")"
[ -z "$(cj "$R")" ] || fail "quota: $R"
eq "$(A "$ops_TOK" GET "/admin/v1/earn/products/$PID" | jq -r .perUserMax)" 500 "perUserMax"
AL="$(A "$SA" GET "/admin/v1/audit-logs?module=earn_product&size=50")"
jq -e --arg t "earn_product:$PID" '[.items[] | select(.target==$t and .result=="ok")] | length >= 3' <<<"$AL" >/dev/null || fail "earn product writes not audited"
jq -e --arg t "earn_product:$PID" '[.items[] | select(.target==$t and .action=="update")][0] | .before != null and .after != null and .level=="high"' <<<"$AL" >/dev/null || fail "audit missing before/after: $(jq -c '.items[0]' <<<"$AL")"
fresh
R="$(A "$ops_TOK" DELETE "/admin/v1/earn/products/$PID" "{\"reason\":\"清理\",\"totpCode\":\"$(totp "$ops_SECRET")\"}")"
[ -z "$(cj "$R")" ] || fail "delete unused product: $R"
[ "$(AH "$ops_TOK" GET "/admin/v1/earn/products/$PID")" = 404 ] || fail "deleted product still there"
pass "create(offline)/no-overwrite/patch/quota/delete; 2FA enforced; rates blocked outside approval; audited with before/after"

say "loan stock limits"
internal "$FUTURES_URL" POST /internal/dev/index '{"symbol":"BTCUSDT","price":"50000"}' >/dev/null
read -r UT UID1 < <(register "earnadm-$TS@example.com")
credit "$UID1" BTC 1
fresh
R="$(A "$ops_TOK" PUT /admin/v1/loans/terms/7d/quota "{\"totalQuota\":\"1500\",\"reason\":\"控量\",\"totpCode\":\"$(totp "$ops_SECRET")\"}")"
[ -z "$(cj "$R")" ] || fail "term quota: $R"
pass "$(api "$UT" POST /loans '{"collateralAsset":"BTC","amount":"1000","term":"7d"}')" "borrow within quota" >/dev/null
r="$(api "$UT" POST /loans '{"collateralAsset":"BTC","amount":"1000","term":"7d"}')"
[ "$(errcode "$r")" = TERM_QUOTA_EXCEEDED ] || fail "over quota: $(body "$r")"
eq "$(A "$ops_TOK" GET /admin/v1/loans/terms | jq -r '.items[] | select(.term=="7d") | .remaining')" 500 "7d remaining"
fresh
R="$(A "$ops_TOK" PATCH /admin/v1/loans/terms/30d "{\"status\":\"offline\",\"reason\":\"下架\",\"totpCode\":\"$(totp "$ops_SECRET")\"}")"
[ -z "$(cj "$R")" ] || fail "term offline: $R"
r="$(api "$UT" POST /loans '{"collateralAsset":"BTC","amount":"1000","term":"30d"}')"
[ "$(errcode "$r")" = TERM_OFFLINE ] || fail "offline term borrowable: $(body "$r")"
fresh
R="$(A "$ops_TOK" DELETE /admin/v1/loans/collaterals/BTC "{\"reason\":\"x\",\"totpCode\":\"$(totp "$ops_SECRET")\"}")"
[ "$(cj "$R")" = COLLATERAL_IN_USE ] || fail "delete in-use collateral: $R"
R="$(A "$ops_TOK" POST /admin/v1/loans/collaterals "{\"asset\":\"BTC\",\"cap\":\"0.3\",\"reason\":\"降上限\",\"totpCode\":\"$(totp "$ops_SECRET")\"}")"
[ -z "$(cj "$R")" ] || fail "collateral cap: $R"
eq "$(A "$ops_TOK" GET /admin/v1/loans/collaterals | jq -r '.items[] | select(.asset=="BTC") | .cap')" 0.3 "BTC cap"
pass "term quota -> TERM_QUOTA_EXCEEDED; offline term -> TERM_OFFLINE; in-use collateral not deletable; cap updated"

say "rate change: dual approval"
R="$(A "$ops_TOK" POST /admin/v1/rate-changes '{"target":"earn_product","productId":"simple-USDT","apr":"1.5","reason":"x"}')"
[ "$(cj "$R")" = VALIDATION_ERROR ] || fail "apr 150% accepted: $R"
R="$(A "$ops_TOK" POST /admin/v1/rate-changes '{"target":"earn_product","productId":"simple-USDT","apr":"0.02"}')"
[ "$(cj "$R")" = REASON_REQUIRED ] || fail "no reason: $R"
R="$(A "$ops_TOK" POST /admin/v1/rate-changes '{"target":"earn_product","productId":"simple-USDT","apr":"0.02","bonusApr":"0.025","reason":"活期加息"}')"
EC="$(jq -r .id <<<"$R")"; [ "$EC" != null ] && [ -n "$EC" ] || fail "submit earn rate: $R"
[ "$(jq -r .before.apr <<<"$R")" = 0.01 ] || fail "before not captured: $R"
R="$(A "$ops_TOK" POST /admin/v1/rate-changes '{"target":"loan_term","term":"flexible","hourlyRate":"0.000005","reason":"降息"}')"
LC="$(jq -r .id <<<"$R")"; [ -n "$LC" ] && [ "$LC" != null ] || fail "submit loan rate: $R"
[ "$(AH "$ops_TOK" POST "/admin/v1/rate-changes/$EC/approve" "{\"totpCode\":\"$(totp "$ops_SECRET")\"}")" = 403 ] || fail "operations approved"
[ "$(cj "$(A "$fin2_TOK" POST "/admin/v1/fee-changes/$EC/approve" "{\"totpCode\":\"$(totp "$fin2_SECRET")\"}")")" = NOT_FOUND ] || fail "rate change approvable via fee route"
R="$(A "$fin2_TOK" POST "/admin/v1/rate-changes/$EC/approve" '{"comment":"ok","totpCode":"000000"}')"
[ "$(cj "$R")" = TOTP_INVALID ] || fail "approve without 2FA: $R"
fresh
for c in "$EC" "$LC"; do
  R="$(A "$fin2_TOK" POST "/admin/v1/rate-changes/$c/approve" "{\"comment\":\"财务同意\",\"totpCode\":\"$(totp "$fin2_SECRET")\"}")"
  [ "$(jq -r .status <<<"$R")" = pending ] || fail "after 1 approval: $R"
  R="$(A "$sa2_TOK" POST "/admin/v1/rate-changes/$c/approve" "{\"comment\":\"同意\",\"totpCode\":\"$(totp "$sa2_SECRET")\"}")"
  [ "$(jq -r .status <<<"$R")" = scheduled ] || fail "after dual approval: $R"
  fresh
done
curl -sS "$ADMIN_URL/announcements" | jq -e '.items[] | select(.category=="earn") | select(.body|contains("simple-USDT"))' >/dev/null || fail "no earn announcement"
curl -sS "$ADMIN_URL/announcements" | jq -e '.items[] | select(.category=="loan") | select(.body|contains("0.000005"))' >/dev/null || fail "no loan announcement"
pass "self/ops approval blocked; fee route isolated; 2FA; super+finance_second -> scheduled; announcements published"

say "scheduler applies after notice"
[ "$(A "$SA" POST /admin/v1/dev/run-scheduler '{}' | jq -r .applied)" = 0 ] || fail "applied early"
eq "$(curl -sS "$GATEWAY_URL/earn/products/simple-USDT" | jq -r .apr)" 0.01 "apr before notice"
LATER="$(python3 -c 'import datetime as d;print((d.datetime.now(d.timezone.utc)+d.timedelta(hours=25)).strftime("%Y-%m-%dT%H:%M:%SZ"))')"
[ "$(A "$SA" POST /admin/v1/dev/run-scheduler "{\"now\":\"$LATER\"}" | jq -r .applied)" = 2 ] || fail "not applied after notice"
eq "$(curl -sS "$GATEWAY_URL/earn/products/simple-USDT" | jq -r .apr)" 0.02 "apr after apply"
eq "$(curl -sS "$GATEWAY_URL/earn/products/simple-USDT" | jq -r .bonusApr)" 0.025 "bonusApr after apply"
eq "$(A "$ops_TOK" GET /admin/v1/loans/terms | jq -r '.items[] | select(.term=="flexible") | .hourlyRate')" 0.000005 "loan hourly rate"
[ "$(A "$SA" GET "/admin/v1/audit-logs?module=earn_rate&action=apply_change" | jq -r '.items[0].admin')" = system ] || fail "earn apply not audited"
[ "$(A "$SA" GET "/admin/v1/audit-logs?module=loan_rate&action=apply_change" | jq -r '.items[0].result')" = ok ] || fail "loan apply not audited"
pass "applied only after 24h; public APR and loan rate updated; apply audited"

say "rollback"
R="$(A "$ops_TOK" POST "/admin/v1/rate-changes/$EC/rollback" '{"reason":"回滚"}')"
RB="$(jq -r .id <<<"$R")"; [ "$(jq -r .payload.apr <<<"$R")" = 0.01 ] || fail "rollback payload: $R"
[ "$(cj "$(A "$SA" POST "/admin/v1/fee-changes/$EC/rollback" '{"reason":"x"}')")" = NOT_FOUND ] || fail "rate change rollable via fee route"
R="$(A "$fin2_TOK" POST "/admin/v1/rate-changes/$RB/reject" '{"comment":"暂不回滚"}')"
[ "$(jq -r .status <<<"$R")" = rejected ] || fail "reject: $R"
[ "$(A "$aud_TOK" GET /admin/v1/rate-changes | jq '[.items[] | select(.kind=="fee")] | length')" = 0 ] || fail "fee changes leaked into rate list"
pass "rollback creates a new change needing approval; reject works; lists isolated"

printf '\n\033[1;32mSMOKE-EARN-ADMIN PASSED\033[0m — RBAC -> 2FA -> product CRUD -> stock limits -> rate dual approval -> scheduler -> rollback -> audit\n'
