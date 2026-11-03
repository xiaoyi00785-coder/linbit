#!/usr/bin/env bash
#
# End-to-end account smoke test for the Linbit account service:
#
#   phone country-code validation (400) -> phone register (E.164) with SMS code
#   -> two-step login on a NEW device (challenge -> SMS code -> tokens) -> device
#   list (current-device flag) + remove -> forgot password (verify-code ->
#   one-time resetToken -> reset, revokes sessions) -> withdrawal address book
#   add/list/delete + whitelist toggle (code-gated) -> change nickname & avatar
#   (URL patch + multipart upload + controlled retrieval + type rejection).
#
# Verification codes are read back through the account service's dev-only
# internal endpoint (DEV_MODE=true), standing in for a real email/SMS provider.
#
# Two ways to run it (mirrors scripts/smoke-trade.sh):
#
#   1. No stack running (default, no Docker): boots the devstack, re-runs itself
#      inside it, tears everything down afterwards.
#
#        bash scripts/smoke-account.sh
#
#   2. Against a running stack: export GATEWAY_URL, ACCOUNT_URL, INTERNAL_TOKEN
#      and set SMOKE_INNER=1.
#
set -euo pipefail
cd "$(dirname "$0")/.."

if [ "${SMOKE_INNER:-}" != "1" ]; then
  echo "== booting devstack (embedded postgres + miniredis + services) =="
  ROOT="$(pwd)"
  cd tools/devstack
  exec go run . -root "$ROOT" -exec "SMOKE_INNER=1 bash scripts/smoke-account.sh"
fi

GATEWAY_URL="${GATEWAY_URL:-http://localhost:8080}"
ACCOUNT_URL="${ACCOUNT_URL:-http://localhost:8081}"
INTERNAL_TOKEN="${INTERNAL_TOKEN:-}"
PASSWORD="Str0ng-Passw0rd!"
NEWPASSWORD="N3w-Str0ng-Pass!"

say()  { printf '\n\033[1;34m== %s ==\033[0m\n' "$1"; }
fail() { printf '\033[1;31mSMOKE-ACCOUNT FAILED: %s\033[0m\n' "$1" >&2; exit 1; }
ok()   { printf '  ok  %s\n' "$1"; }

command -v jq >/dev/null 2>&1 || fail "jq is required for smoke-account.sh"
[ -n "$INTERNAL_TOKEN" ] || fail "INTERNAL_TOKEN not set (needed to read dev codes)"

# last_code <target> <purpose> — read the most recent code via the dev endpoint.
last_code() {
  curl -sS -G "$ACCOUNT_URL/internal/dev/last-code" \
    --data-urlencode "target=$1" --data-urlencode "purpose=$2" \
    -H "X-Internal-Token: $INTERNAL_TOKEN" | jq -r '.code // empty'
}

# send_code <purpose> <json-body> — public send-code endpoint via gateway.
send_code() {
  curl -sS -o /dev/null -w '%{http_code}' -X POST "$GATEWAY_URL/auth/code/send" \
    -H 'Content-Type: application/json' -d "$2"
}

api() { # api <method> <path> <token> <json|''>
  local m="$1" p="$2" t="$3" b="${4:-}"
  if [ -n "$b" ]; then
    curl -sS -X "$m" "$GATEWAY_URL$p" -H 'Content-Type: application/json' \
      -H "Authorization: Bearer $t" -d "$b"
  else
    curl -sS -X "$m" "$GATEWAY_URL$p" -H "Authorization: Bearer $t"
  fi
}

TS="$(date +%s)"
PHONE="+8613${TS: -9}"
echo "phone=$PHONE"

say "1/7 phone validation (400) + register (E.164) with SMS code"
# A phone without a country code must be a clear 400 at every entry (not 401).
NOCC="$(send_code register "{\"purpose\":\"register\",\"phone\":\"13800138000\"}")"
[ "$NOCC" = "400" ] || fail "send-code without country code returned $NOCC, want 400"
ok "send-code missing country code -> 400"
LOGIN_NOCC="$(curl -sS -o /dev/null -w '%{http_code}' -X POST "$GATEWAY_URL/auth/login" \
  -H 'Content-Type: application/json' -d "{\"phone\":\"13800138000\",\"password\":\"x\"}")"
[ "$LOGIN_NOCC" = "400" ] || fail "login without country code returned $LOGIN_NOCC, want 400"
ok "login missing country code -> 400 (not 401)"
CODE_HTTP="$(send_code register "{\"purpose\":\"register\",\"phone\":\"$PHONE\",\"countryCode\":\"86\"}")"
[ "$CODE_HTTP" = "200" ] || fail "send register code returned $CODE_HTTP"
REG_CODE="$(last_code "$PHONE" register)"
[ -n "$REG_CODE" ] || fail "no register code recorded"
ok "register code = $REG_CODE"
REG="$(curl -sS -X POST "$GATEWAY_URL/auth/register" -H 'Content-Type: application/json' \
  -d "{\"phone\":\"$PHONE\",\"countryCode\":\"86\",\"password\":\"$PASSWORD\",\"code\":\"$REG_CODE\"}")"
echo "$REG"
[ -n "$(printf '%s' "$REG" | jq -r '.accessToken // empty')" ] || fail "register did not return accessToken"
ok "registered"

say "2/7 two-step login on a NEW device (challenge -> SMS code -> tokens)"
LOGIN="$(curl -sS -X POST "$GATEWAY_URL/auth/login" -H 'Content-Type: application/json' \
  -d "{\"phone\":\"$PHONE\",\"password\":\"$PASSWORD\",\"deviceId\":\"acct-dev-1\",\"deviceName\":\"Pixel\"}")"
echo "$LOGIN"
CHALLENGE="$(printf '%s' "$LOGIN" | jq -r '.challengeToken // empty')"
[ -n "$CHALLENGE" ] || fail "new-device login did not return a challenge"
[ "$(printf '%s' "$LOGIN" | jq -r '.methods[0]')" = "sms" ] || fail "challenge method is not sms"
ok "challenge issued (sms)"
LOGIN_CODE="$(last_code "$PHONE" login)"
[ -n "$LOGIN_CODE" ] || fail "no login code recorded"
VERIFY="$(curl -sS -X POST "$GATEWAY_URL/auth/login/verify" -H 'Content-Type: application/json' \
  -d "{\"challengeToken\":\"$CHALLENGE\",\"code\":\"$LOGIN_CODE\"}")"
echo "$VERIFY"
ACCESS="$(printf '%s' "$VERIFY" | jq -r '.accessToken // empty')"
[ -n "$ACCESS" ] || fail "login verify did not return accessToken"
ok "second factor verified, tokens issued"

# A repeat login from the now-trusted device must skip the challenge.
LOGIN2="$(curl -sS -X POST "$GATEWAY_URL/auth/login" -H 'Content-Type: application/json' \
  -d "{\"phone\":\"$PHONE\",\"password\":\"$PASSWORD\",\"deviceId\":\"acct-dev-1\"}")"
[ -n "$(printf '%s' "$LOGIN2" | jq -r '.accessToken // empty')" ] || fail "known-device login should skip challenge"
ok "known device logs in directly"

say "3/7 device list (current flag) + remove"
# Pass X-Device-Id so the current-device fingerprint matches acct-dev-1.
DEVICES="$(curl -sS "$GATEWAY_URL/account/devices" -H "Authorization: Bearer $ACCESS" -H "X-Device-Id: acct-dev-1")"
echo "$DEVICES"
DEV_COUNT="$(printf '%s' "$DEVICES" | jq -r '.items | length')"
[ "$DEV_COUNT" -ge 2 ] || fail "expected >=2 devices, got $DEV_COUNT"
ok "devices listed ($DEV_COUNT)"
CUR_COUNT="$(printf '%s' "$DEVICES" | jq -r '[.items[] | select(.current==true)] | length')"
[ "$CUR_COUNT" = "1" ] || fail "expected exactly 1 current device, got $CUR_COUNT"
CUR_ID="$(printf '%s' "$DEVICES" | jq -r '.items[] | select(.current==true) | .deviceId')"
[ "$CUR_ID" = "cid:acct-dev-1" ] || fail "current device = $CUR_ID, want cid:acct-dev-1"
ok "current device flagged ($CUR_ID)"
# Remove the auto-trusted register device (keep acct-dev-1 trusted for step 4).
RM_ID="$(printf '%s' "$DEVICES" | jq -r '.items[] | select(.deviceId != "cid:acct-dev-1") | .deviceId' | head -1)"
[ -n "$RM_ID" ] || fail "could not find register device to remove"
RM_HTTP="$(curl -sS -o /dev/null -w '%{http_code}' -X DELETE "$GATEWAY_URL/account/devices/$RM_ID" \
  -H "Authorization: Bearer $ACCESS")"
[ "$RM_HTTP" = "204" ] || fail "remove device returned $RM_HTTP"
ok "device removed ($RM_ID)"

say "4/7 forgot password -> verify-code -> one-time resetToken -> reset"
RS_HTTP="$(send_code reset_password "{\"purpose\":\"reset_password\",\"phone\":\"$PHONE\",\"countryCode\":\"86\"}")"
[ "$RS_HTTP" = "200" ] || fail "send reset code returned $RS_HTTP"
RESET_CODE="$(last_code "$PHONE" reset_password)"
[ -n "$RESET_CODE" ] || fail "no reset code recorded"
# Step 1: exchange the code for a short-lived resetToken.
VC="$(curl -sS -X POST "$GATEWAY_URL/auth/password/verify-code" -H 'Content-Type: application/json' \
  -d "{\"phone\":\"$PHONE\",\"countryCode\":\"86\",\"code\":\"$RESET_CODE\"}")"
echo "$VC"
RESET_TOKEN="$(printf '%s' "$VC" | jq -r '.resetToken // empty')"
[ -n "$RESET_TOKEN" ] || fail "verify-code did not return resetToken"
ok "resetToken issued"
# Step 2: set the new password with the resetToken.
RST_HTTP="$(curl -sS -o /dev/null -w '%{http_code}' -X POST "$GATEWAY_URL/auth/password/reset" \
  -H 'Content-Type: application/json' \
  -d "{\"resetToken\":\"$RESET_TOKEN\",\"newPassword\":\"$NEWPASSWORD\"}")"
[ "$RST_HTTP" = "204" ] || fail "password reset returned $RST_HTTP"
ok "password reset via resetToken"
# The resetToken is single-use: a replay must be rejected.
REPLAY_HTTP="$(curl -sS -o /dev/null -w '%{http_code}' -X POST "$GATEWAY_URL/auth/password/reset" \
  -H 'Content-Type: application/json' \
  -d "{\"resetToken\":\"$RESET_TOKEN\",\"newPassword\":\"$NEWPASSWORD\"}")"
[ "$REPLAY_HTTP" = "400" ] || fail "resetToken replay returned $REPLAY_HTTP, want 400"
ok "resetToken single-use enforced"
# Old password rejected.
OLD_HTTP="$(curl -sS -o /dev/null -w '%{http_code}' -X POST "$GATEWAY_URL/auth/login" \
  -H 'Content-Type: application/json' -d "{\"phone\":\"$PHONE\",\"password\":\"$PASSWORD\",\"deviceId\":\"acct-dev-1\"}")"
[ "$OLD_HTTP" = "401" ] || fail "login with old password returned $OLD_HTTP, want 401"
ok "old password rejected"
# New password works (acct-dev-1 is trusted -> direct tokens).
LOGIN3="$(curl -sS -X POST "$GATEWAY_URL/auth/login" -H 'Content-Type: application/json' \
  -d "{\"phone\":\"$PHONE\",\"password\":\"$NEWPASSWORD\",\"deviceId\":\"acct-dev-1\"}")"
ACCESS="$(printf '%s' "$LOGIN3" | jq -r '.accessToken // empty')"
[ -n "$ACCESS" ] || fail "login with new password failed: $LOGIN3"
ok "new password works"

say "5/7 withdrawal address book: add -> list (code-gated)"
# A sensitive op needs a code to the user's phone (purpose=address).
curl -sS -o /dev/null -X POST "$GATEWAY_URL/account/code/send" \
  -H 'Content-Type: application/json' -H "Authorization: Bearer $ACCESS" \
  -d '{"purpose":"address","channel":"sms"}'
ADDR_CODE="$(last_code "$PHONE" address)"
[ -n "$ADDR_CODE" ] || fail "no address code recorded"
CREATE="$(api POST /account/withdraw-addresses "$ACCESS" \
  "{\"asset\":\"USDT\",\"network\":\"TRC20\",\"address\":\"TXy1Z9DemoAddr0000000001\",\"label\":\"cold\",\"code\":\"$ADDR_CODE\"}")"
echo "$CREATE"
ADDR_ID="$(printf '%s' "$CREATE" | jq -r '.id // empty')"
[ -n "$ADDR_ID" ] || fail "address not created"
ok "address created id=$ADDR_ID"
LIST="$(api GET /account/withdraw-addresses "$ACCESS")"
[ "$(printf '%s' "$LIST" | jq -r '.items | length')" = "1" ] || fail "address list != 1"
ok "address listed"

say "6/7 whitelist toggle + address delete (code-gated)"
curl -sS -o /dev/null -X POST "$GATEWAY_URL/account/code/send" \
  -H 'Content-Type: application/json' -H "Authorization: Bearer $ACCESS" -d '{"purpose":"address"}'
WL_CODE="$(last_code "$PHONE" address)"
WL="$(api PUT /account/withdraw-whitelist "$ACCESS" "{\"enabled\":true,\"code\":\"$WL_CODE\"}")"
[ "$(printf '%s' "$WL" | jq -r '.enabled')" = "true" ] || fail "whitelist not enabled"
ok "whitelist enabled"
[ "$(api GET /account/withdraw-whitelist "$ACCESS" | jq -r '.enabled')" = "true" ] || fail "whitelist not persisted"
curl -sS -o /dev/null -X POST "$GATEWAY_URL/account/code/send" \
  -H 'Content-Type: application/json' -H "Authorization: Bearer $ACCESS" -d '{"purpose":"address"}'
DEL_CODE="$(last_code "$PHONE" address)"
DEL_HTTP="$(curl -sS -o /dev/null -w '%{http_code}' -X DELETE \
  "$GATEWAY_URL/account/withdraw-addresses/$ADDR_ID?code=$DEL_CODE" -H "Authorization: Bearer $ACCESS")"
[ "$DEL_HTTP" = "204" ] || fail "delete address returned $DEL_HTTP"
ok "address deleted"

say "7/7 change nickname & avatar (URL patch + multipart upload)"
PROFILE="$(api PATCH /account/profile "$ACCESS" \
  '{"nickname":"lb_Trader","avatarUrl":"https://cdn.example.com/avatar/lb.png"}')"
echo "$PROFILE"
[ "$(printf '%s' "$PROFILE" | jq -r '.nickname')" = "lb_Trader" ] || fail "nickname not updated"
[ "$(printf '%s' "$PROFILE" | jq -r '.avatarUrl')" = "https://cdn.example.com/avatar/lb.png" ] || fail "avatar not updated"
ok "profile updated"
# Invalid avatar URL rejected.
BAD_HTTP="$(curl -sS -o /dev/null -w '%{http_code}' -X PATCH "$GATEWAY_URL/account/profile" \
  -H 'Content-Type: application/json' -H "Authorization: Bearer $ACCESS" -d '{"avatarUrl":"javascript:alert(1)"}')"
[ "$BAD_HTTP" = "400" ] || fail "invalid avatar returned $BAD_HTTP, want 400"
ok "invalid avatar URL rejected"

# Multipart avatar upload: a minimal valid PNG is stored and retrievable.
PNG="$(mktemp)"; printf '\x89PNG\r\n\x1a\n' > "$PNG"; printf 'linbit-smoke-pixels' >> "$PNG"
UP="$(curl -sS -X POST "$GATEWAY_URL/account/avatar" -H "Authorization: Bearer $ACCESS" \
  -F "file=@$PNG;type=image/png;filename=me.png")"
echo "$UP"
AV_URL="$(printf '%s' "$UP" | jq -r '.avatarUrl // empty')"
[ -n "$AV_URL" ] || fail "avatar upload did not return avatarUrl: $UP"
case "$AV_URL" in /account/avatars/*) ;; *) fail "unexpected avatar url: $AV_URL" ;; esac
ok "avatar uploaded -> $AV_URL"
# Retrieve it through the controlled public route (via the gateway).
AV_HTTP="$(curl -sS -o /dev/null -w '%{http_code}' "$GATEWAY_URL$AV_URL")"
[ "$AV_HTTP" = "200" ] || fail "avatar retrieval returned $AV_HTTP"
ok "avatar served from controlled route"
# A non-image payload (sniffed) is rejected with 415.
TXT="$(mktemp)"; printf 'this is definitely not an image' > "$TXT"
BAD_UP="$(curl -sS -o /dev/null -w '%{http_code}' -X POST "$GATEWAY_URL/account/avatar" \
  -H "Authorization: Bearer $ACCESS" -F "file=@$TXT;type=image/png;filename=fake.png")"
[ "$BAD_UP" = "415" ] || fail "non-image upload returned $BAD_UP, want 415"
ok "non-image upload rejected (415)"
rm -f "$PNG" "$TXT"

printf '\n\033[1;32mSMOKE-ACCOUNT PASSED\033[0m — phone-400 -> register -> 2-step login -> devices(current) -> resetToken -> whitelist -> avatar upload\n'
