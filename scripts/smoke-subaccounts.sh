#!/usr/bin/env bash
# Smoke for sub-accounts: parent 2FA gate, create/limit/dup, idempotent spot
# transfers with ledger, asset + order visibility, API keys (permissions, IP
# allowlist, never wallet/parent endpoints), sub login (off by default, own
# password + 2FA, fenced session), freeze/unfreeze, admin list/settings/freeze,
# sub excluded from withdraw verification.
set -euo pipefail
cd "$(dirname "$0")/.."

if [ "${SMOKE_INNER:-}" != "1" ]; then
  echo "== booting devstack =="
  ROOT="$(pwd)"
  cd tools/devstack
  exec go run . -root "$ROOT" -exec "SMOKE_INNER=1 bash scripts/smoke-subaccounts.sh"
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
SMOKE_NAME="SUBACCOUNTS"
. scripts/lib-growth-smoke.sh
[ -n "$SA_USER" ] && [ -n "$SA_PASS" ] && [ -n "$SA_TOTP" ] || fail "ADMIN_BOOTSTRAP_* not set"
pass() { printf "  ok  %s\n" "$1"; }
fresh() { local s; s=$(( 30 - $(date +%s) % 30 )); [ "$s" -lt 30 ] && sleep "$s"; return 0; }
K() { # K <key> <secret> <METHOD> <path> [json] [ip]
  curl -sS -w '\n%{http_code}' -X "$3" "$GATEWAY_URL$4" -H 'Content-Type: application/json' \
    -H "X-LB-APIKEY: $1" -H "X-LB-SECRET: $2" ${6:+-H "X-Real-IP: $6"} ${5:+-d "$5"}
}
expect() { # expect <resp> <http> [errcode] <label>
  local c e; c="$(code "$1")"; e="$(errcode "$1")"
  if [ $# -eq 4 ]; then [ "$c" = "$2" ] && [ "$e" = "$3" ] || fail "$4: want $2 $3, got $c $(body "$1")"
  else [ "$c" = "$2" ] || fail "$3: want $2, got $c $(body "$1")"; fi
}

say "parent 2FA gate + create"
read -r PT PU <<<"$(register "subparent-$TS@example.com")"
expect "$(api "$PT" POST /sub-accounts '{"username":"desk01"}')" 403 TWO_FACTOR_REQUIRED "create without 2FA"
PSEC="$(ok "$(api "$PT" POST /2fa/totp/bind '{}')" bind | jq -r .secret)"
ok "$(api "$PT" POST /2fa/totp/verify "{\"code\":\"$(totp "$PSEC")\"}")" "totp verify" >/dev/null
expect "$(api "$PT" POST /sub-accounts '{"username":"Bad Name"}')" 400 VALIDATION_ERROR "bad username"
S="$(ok "$(api "$PT" POST /sub-accounts '{"username":"desk01","nickname":"MM desk","note":"smoke"}')" create)"
SU="$(jq -r .uid <<<"$S")"; SEMAIL="$(jq -r .loginEmail <<<"$S")"
[ "$(jq -r .parentUid <<<"$S")" = "$PU" ] && [ "$(jq -r .loginEnabled <<<"$S")" = false ] || fail "create shape: $S"
expect "$(api "$PT" POST /sub-accounts '{"username":"desk01"}')" 409 SUB_ACCOUNT_EXISTS "duplicate username"
L="$(ok "$(api "$PT" GET /sub-accounts)" list)"; [ "$(jq -r .total <<<"$L")" = 1 ] || fail "list: $L"
pass "2FA required; sub $SU ($SEMAIL); dup refused; login off by default"

say "admin settings: limit"
SA="$(alogin "$SA_USER" "$SA_PASS" "$SA_TOTP")"; [ -n "$SA" ] || fail "admin login"
ST="$(A "$SA" GET /admin/v1/sub-accounts/settings)"; [ "$(jq -r .maxPerParent <<<"$ST")" = 20 ] || fail "default settings: $ST"
put_limit() { A "$SA" PUT /admin/v1/sub-accounts/settings "{\"enabled\":true,\"maxPerParent\":$1,\"requireParent2fa\":true,\"allowSubLogin\":true,\"maxApiKeysPerSub\":10,\"reason\":\"smoke\",\"totpCode\":\"$(totp "$SA_TOTP")\"}"; }
R="$(put_limit 2)"; [ "$(jq -r .maxPerParent <<<"$R")" = 2 ] || fail "set limit: $R"
ok "$(api "$PT" POST /sub-accounts '{"username":"desk02"}')" create2 >/dev/null
expect "$(api "$PT" POST /sub-accounts '{"username":"desk03"}')" 409 SUB_ACCOUNT_LIMIT "limit"
fresh; R="$(put_limit 20)"; [ "$(jq -r .maxPerParent <<<"$R")" = 20 ] || fail "restore limit: $R"
pass "limit configurable in admin (2 → 409 SUB_ACCOUNT_LIMIT), restored to 20"

say "transfers"
credit "$PU" USDT 1000
X='{"clientId":"c1-'$TS'","subAccountId":"'$SU'","direction":"to_sub","asset":"USDT","amount":"100"}'
T1="$(ok "$(api "$PT" POST /sub-accounts/transfer "$X")" transfer)"
[ "$(jq -r .status <<<"$T1")" = success ] || fail "transfer: $T1"
T2="$(ok "$(api "$PT" POST /sub-accounts/transfer "$X")" replay)"
[ "$(jq -r .transferId <<<"$T2")" = "$(jq -r .transferId <<<"$T1")" ] || fail "replay not idempotent: $T2"
expect "$(api "$PT" POST /sub-accounts/transfer "${X/\"100\"/\"50\"}")" 409 CLIENT_ID_REUSED "clientId reuse"
expect "$(api "$PT" POST /sub-accounts/transfer '{"clientId":"c2-'$TS'","subAccountId":"'$SU'","direction":"to_parent","asset":"USDT","amount":"5000"}')" 400 INSUFFICIENT_BALANCE "overdraw"
eq "$(spot_bal "$PT")" 900 "parent balance"
SA_ASSETS="$(ok "$(api "$PT" GET "/sub-accounts/$SU/assets")" "sub assets")"
eq "$(jq -r '[.balances[]|select(.asset=="USDT")|.free][0]' <<<"$SA_ASSETS")" 100 "sub balance"
ALL="$(ok "$(api "$PT" GET /sub-accounts/assets)" "all sub assets")"; jq -e '.items and .totals' <<<"$ALL" >/dev/null || fail "summary: $ALL"
XL="$(ok "$(api "$PT" GET "/sub-accounts/transfers?subAccountId=$SU")" "transfer list")"
[ "$(jq -r '[.items[]|select(.status=="success")]|length' <<<"$XL")" = 1 ] || fail "transfer list: $XL"
pass "to_sub 100 ok; replay same transferId; reuse 409; overdraw 400; balances + history visible"

say "API keys"
fresh
expect "$(api "$PT" POST "/sub-accounts/$SU/api-keys" '{"label":"x","permissions":["withdraw"],"totpCode":"'"$(totp "$PSEC")"'"}')" 400 PERMISSION_NOT_ALLOWED "withdraw perm"
RK="$(ok "$(api "$PT" POST "/sub-accounts/$SU/api-keys" '{"label":"ro","permissions":["read"],"totpCode":"'"$(totp "$PSEC")"'"}')" "read key")"
RKEY="$(jq -r .apiKey <<<"$RK")"; RSEC="$(jq -r .secret <<<"$RK")"; [ ${#RSEC} -eq 64 ] || fail "secret shape"
TK="$(ok "$(api "$PT" POST "/sub-accounts/$SU/api-keys" '{"label":"spot","permissions":["spot_trade"],"totpCode":"'"$(totp "$PSEC")"'"}')" "spot key")"
TKEY="$(jq -r .apiKey <<<"$TK")"; TSEC="$(jq -r .secret <<<"$TK")"
IK="$(ok "$(api "$PT" POST "/sub-accounts/$SU/api-keys" '{"label":"ip","permissions":["read"],"ipAllowlist":["9.9.9.9"],"totpCode":"'"$(totp "$PSEC")"'"}')" "ip key")"
B="$(ok "$(K "$RKEY" "$RSEC" GET "/assets/balances?pageSize=100")" "key balances")"
eq "$(jq -r '[.items[]|select(.asset=="USDT")|.free][0]' <<<"$B")" 100 "key sees sub balance"
ORD='{"symbol":"BTCUSDT","side":"buy","type":"limit","price":"1000","quantity":"0.01","timeInForce":"GTC"}'
expect "$(K "$RKEY" "$RSEC" POST /spot/orders "$ORD")" 403 API_KEY_FORBIDDEN "read key trade"
for p in "POST /wallet/withdraw" "GET /wallet/deposit/address?asset=USDT" "GET /sub-accounts" "POST /sub-accounts/transfer" "GET /account/withdraw-addresses" "GET /account/me"; do
  expect "$(K "$TKEY" "$TSEC" ${p% *} "${p#* }" '{}')" 403 API_KEY_FORBIDDEN "key -> $p"
done
expect "$(K "$(jq -r .apiKey <<<"$IK")" "$(jq -r .secret <<<"$IK")" GET /assets/balances "" 1.1.1.1)" 403 API_KEY_IP_DENIED "ip allowlist"
expect "$(K "$RKEY" wrong GET /assets/balances)" 401 API_KEY_INVALID "bad secret"
O="$(ok "$(K "$TKEY" "$TSEC" POST /spot/orders "$ORD")" "spot key order")"
OH="$(ok "$(api "$PT" GET "/sub-accounts/$SU/orders?type=open")" "sub orders")"
[ "$(jq -r '.items|length' <<<"$OH")" -ge 1 ] || fail "parent can't see sub order: $OH"
KL="$(ok "$(api "$PT" GET "/sub-accounts/$SU/api-keys")" "key list")"; [ "$(jq -r '[.items[]|.secret // empty]|length' <<<"$KL")" = 0 ] || fail "secret leaked in list"
ok "$(api "$PT" DELETE "/sub-accounts/$SU/api-keys/$RKEY")" revoke >/dev/null
expect "$(K "$RKEY" "$RSEC" GET /assets/balances)" 401 API_KEY_INVALID "revoked key"
pass "perm=withdraw refused; read/spot scopes enforced; wallet/parent endpoints 403; IP allowlist; parent sees sub orders; revoke"

say "sub login"
LOGIN() { curl -sS -w '\n%{http_code}' -X POST "$GATEWAY_URL/auth/login" -H 'Content-Type: application/json' -d "{\"email\":\"$SEMAIL\",\"password\":\"Sub-Passw0rd-1\",\"deviceId\":\"smoke-sub\"}"; }
R="$(LOGIN)"; [ "$(code "$R")" = 401 ] || [ "$(code "$R")" = 403 ] || fail "login before enable: $R"
fresh
EN="$(ok "$(api "$PT" PATCH "/sub-accounts/$SU" '{"loginEnabled":true,"password":"Sub-Passw0rd-1","totpCode":"'"$(totp "$PSEC")"'"}')" "enable login")"
SSEC="$(jq -r .loginTotp.secret <<<"$EN")"; [ -n "$SSEC" ] && [ "$SSEC" != null ] || fail "no sub totp: $EN"
CH="$(ok "$(LOGIN)" login)"; [ "$(jq -r '.challengeToken // empty' <<<"$CH")" != "" ] || fail "sub login must require 2FA: $CH"
ST="$(ok "$(curl -sS -w '\n%{http_code}' -X POST "$GATEWAY_URL/auth/login/verify" -H 'Content-Type: application/json' -d "{\"challengeToken\":\"$(jq -r .challengeToken <<<"$CH")\",\"totpCode\":\"$(totp "$SSEC")\"}")" "login verify" | jq -r .accessToken)"
[ "$(jwt_sub "$ST")" = "$SU" ] || fail "sub token subject"
for p in "GET /sub-accounts" "POST /sub-accounts" "POST /wallet/withdraw" "GET /wallet/deposit/address?asset=USDT" "POST /account/withdraw-addresses" "POST /2fa/totp/disable"; do
  expect "$(api "$ST" ${p% *} "${p#* }" '{}')" 403 SUB_ACCOUNT_FORBIDDEN "sub session -> $p"
done
expect "$(api "$ST" POST /sub-accounts/transfer '{"clientId":"s1-'$TS'","subAccountId":"'$SU'","direction":"to_sub","asset":"USDT","amount":"1"}')" 403 SUB_ACCOUNT_FORBIDDEN "sub pulls from parent"
R="$(ok "$(api "$ST" POST /sub-accounts/transfer '{"clientId":"s2-'$TS'","direction":"to_parent","asset":"USDT","amount":"10"}')" "sub to parent")"
[ "$(jq -r .status <<<"$R")" = success ] || fail "sub->parent: $R"
eq "$(spot_bal "$PT")" 910 "parent after sub->parent"
V="$(internal "$ACCOUNT_URL" POST /internal/withdraw/verify "{\"uid\":\"$SU\",\"totpCode\":\"$(totp "$SSEC")\"}")"
[ "$(jq -r .ok <<<"$V")" = false ] || fail "sub passed withdraw verify: $V"
ok "$(api "$PT" PATCH "/sub-accounts/$SU" '{"loginEnabled":false,"totpCode":"'"$(totp "$PSEC")"'"}')" "disable login" >/dev/null
expect "$(LOGIN)" 403 SUB_LOGIN_DISABLED "login after disable (correct password)"
pass "disabled by default; own password + TOTP; session fenced; only to_parent; withdraw verify refuses sub"

say "freeze"
ok "$(api "$PT" POST "/sub-accounts/$SU/freeze" '{"reason":"smoke"}')" freeze >/dev/null
expect "$(api "$PT" POST /sub-accounts/transfer '{"clientId":"c3-'$TS'","subAccountId":"'$SU'","direction":"to_sub","asset":"USDT","amount":"1"}')" 403 SUB_ACCOUNT_FROZEN "fund frozen sub"
expect "$(K "$TKEY" "$TSEC" GET /assets/balances)" 403 ACCOUNT_FROZEN "frozen key"
ok "$(api "$PT" POST "/sub-accounts/$SU/unfreeze" '{}')" unfreeze >/dev/null
ok "$(K "$TKEY" "$TSEC" GET /assets/balances)" "key after unfreeze" >/dev/null
pass "freeze blocks funding + keys; unfreeze restores"

say "admin view + freeze"
AL="$(A "$SA" GET "/admin/v1/sub-accounts?parentUid=$PU")"; [ "$(jq -r .total <<<"$AL")" = 2 ] || fail "admin list: $AL"
fresh
R="$(A "$SA" POST "/admin/v1/sub-accounts/$SU/freeze" "{\"reason\":\"smoke\",\"totpCode\":\"$(totp "$SA_TOTP")\"}")"
[ "$(jq -r .status <<<"$R")" = frozen ] || fail "admin freeze: $R"
S2="$(ok "$(api "$PT" GET "/sub-accounts/$SU")" get)"; [ "$(jq -r .status <<<"$S2")" = frozen ] || fail "parent sees freeze: $S2"
pass "admin lists by parent and freezes (2FA + audit)"

printf '\n\033[1;32mSMOKE-SUBACCOUNTS PASSED\033[0m\n'
