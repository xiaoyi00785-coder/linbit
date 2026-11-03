#!/usr/bin/env bash
#
# Smoke for account security settings: overview, 2FA error split
# (TWOFA_REQUIRED 403 vs INVALID_TOTP 401), fund password (+lockout/reset),
# anti-phishing, password change (revokes other sessions + withdrawal lock),
# email/phone change (old + new codes), sessions, passkey ceremonies (begin)
# and the admin security policy (/admin/v1, 2FA + reason + audit).
#
#   bash scripts/smoke-security.sh            # boots the devstack itself
#   SMOKE_INNER=1 GATEWAY_URL=... bash scripts/smoke-security.sh   # running stack
#
# Side-by-side devstacks: set DEVSTACK_*_PORT (e.g. +900: DEVSTACK_GATEWAY_PORT=8980
# ... DEVSTACK_PG_PORT=6333).
set -euo pipefail
cd "$(dirname "$0")/.."

if [ "${SMOKE_INNER:-}" != "1" ]; then
  ROOT="$(pwd)"
  export WEBAUTHN_RP_ID="${WEBAUTHN_RP_ID:-localhost}" WEBAUTHN_RP_ORIGINS="${WEBAUTHN_RP_ORIGINS:-http://localhost:3000}"
  cd tools/devstack
  exec go run . -root "$ROOT" -exec "SMOKE_INNER=1 bash scripts/smoke-security.sh"
fi

GATEWAY_URL="${GATEWAY_URL:-http://localhost:8080}"
ACCOUNT_URL="${ACCOUNT_URL:-http://localhost:8081}"
ASSET_URL="${ASSET_URL:-http://localhost:8082}"
ADMIN_URL="${ADMIN_URL:-http://localhost:8088}"
FUTURES_URL="${FUTURES_INTERNAL_URL:-}"
GROWTH_URL="${GROWTH_URL:-}"
INTERNAL_TOKEN="${INTERNAL_TOKEN:?INTERNAL_TOKEN required}"
SA_USER="${ADMIN_BOOTSTRAP_USERNAME:?}"
SA_PASS="${ADMIN_BOOTSTRAP_PASSWORD:?}"
SA_TOTP="${ADMIN_BOOTSTRAP_TOTP_SECRET:?}"
PASSWORD="Str0ng-Passw0rd!"
NEWPASS="N3w-Str0ng-Passw0rd!"
TS="$(date +%s)"
SMOKE_NAME="SECURITY"
# shellcheck source=lib-growth-smoke.sh
. scripts/lib-growth-smoke.sh

expect_err() { # expect_err <resp> <http> <code> <label>
  [ "$(code "$1")" = "$2" ] && [ "$(errcode "$1")" = "$3" ] || fail "$4: want $2 $3, got $(code "$1") $(body "$1")"
  printf '  ok  %s -> %s %s\n' "$4" "$2" "$3"
}
pass() { printf '  ok  %s\n' "$1"; }
lastcode() { # lastcode <target> <purpose>
  curl -sS -G "$ACCOUNT_URL/internal/dev/last-code" --data-urlencode "target=$1" \
    --data-urlencode "purpose=$2" -H "X-Internal-Token: $INTERNAL_TOKEN" | jq -r '.code // empty'
}
# apid <token> <device-id> <METHOD> <path> [json]  (request as a specific device)
apid() {
  curl -sS -w '\n%{http_code}' -X "$3" "$GATEWAY_URL$4" -H 'Content-Type: application/json' \
    -H "Authorization: Bearer $1" -H "X-Device-Id: $2" ${5:+-d "$5"}
}
seccode() { # seccode <token> <channel> <target> -> code (purpose=security)
  ok "$(api "$1" POST /account/code/send "{\"purpose\":\"security\",\"channel\":\"$2\"}")" "send security code" >/dev/null
  lastcode "$3" security
}

say "users"
EA="seca+$TS@example.com"
curl -sS -o /dev/null -X POST "$GATEWAY_URL/auth/code/send" -H 'Content-Type: application/json' -d "{\"purpose\":\"register\",\"email\":\"$EA\"}"
REG="$(curl -sS -X POST "$GATEWAY_URL/auth/register" -H 'Content-Type: application/json' \
  -d "{\"email\":\"$EA\",\"password\":\"$PASSWORD\",\"code\":\"$(lastcode "$EA" register)\"}")"
TA="$(jq -r .accessToken <<<"$REG")"; RA="$(jq -r .refreshToken <<<"$REG")"; UA="$(jwt_sub "$TA")"
[ -n "$TA" ] && [ "$TA" != null ] || fail "register A: $REG"
read -r TB UB < <(register "secb+$TS@example.com")
credit "$UA" USDT 1000
ok "$(api "$TA" POST /assets/transfer "{\"clientId\":\"sec-$TS-f\",\"asset\":\"USDT\",\"amount\":\"500\",\"from\":\"spot\",\"to\":\"funding\"}")" "spot->funding" >/dev/null
IT() { api "$TA" POST /wallet/internal-transfer "{\"clientId\":\"sec-$TS-$1\",\"asset\":\"USDT\",\"amount\":\"1\",\"toUid\":\"$UB\"$2}"; }

say "1. overview (fresh)"
OV="$(ok "$(api "$TA" GET /account/security)" overview)"
jq -e '.twoFactor.enabled==false and .fundPasswordSet==false and .antiPhishingSet==false and .passkeysCount==0
  and .withdrawLockedUntil==null and (.email|test("\\*")) and (.recentDevices|length)>=1 and .policy.withdrawLockHours==24' <<<"$OV" >/dev/null || fail "overview: $OV"
pass "overview shape"

say "2. 2FA error split (code-based account)"
expect_err "$(IT a0 '')" 403 TWOFA_REQUIRED "itx without code"
ok "$(api "$TA" POST /account/code/send '{"purpose":"withdraw"}')" "withdraw code" >/dev/null
expect_err "$(IT a1 ',"code":"000000"')" 401 INVALID_TOTP "itx wrong code"
ok "$(api "$TA" POST /account/code/send '{"purpose":"withdraw"}')" "withdraw code" >/dev/null
ok "$(IT a2 ",\"code\":\"$(lastcode "$EA" withdraw)\"")" "itx with right code (no fund pw yet, not mandatory)" >/dev/null
pass "code path ok"

say "3. bind TOTP; split with TOTP"
SECRET="$(ok "$(api "$TA" POST /2fa/totp/bind '{}')" bind | jq -r .secret)"
ok "$(api "$TA" POST /2fa/totp/verify "{\"code\":\"$(totp "$SECRET")\"}")" "totp verify" >/dev/null
expect_err "$(IT b0 '')" 403 TWOFA_REQUIRED "itx no totp"
expect_err "$(IT b1 ',"totpCode":"000000"')" 401 INVALID_TOTP "itx wrong totp"
expect_err "$(api "$TA" PUT /account/anti-phishing '{"antiPhishingCode":"Lb2026x","totpCode":"000000"}')" 401 INVALID_TOTP "anti-phishing wrong totp"
expect_err "$(api "$TA" PUT /account/anti-phishing '{"antiPhishingCode":"Lb2026x"}')" 403 TWOFA_REQUIRED "anti-phishing no totp"

say "4. fund password"
expect_err "$(api "$TA" POST /account/fund-password "{\"fundPassword\":\"12ab\",\"totpCode\":\"$(totp "$SECRET")\"}")" 400 VALIDATION_ERROR "fund pw format"
ok "$(api "$TA" POST /account/fund-password "{\"fundPassword\":\"135790\",\"totpCode\":\"$(totp "$SECRET")\"}")" "set fund pw" >/dev/null
expect_err "$(api "$TA" POST /account/fund-password "{\"fundPassword\":\"135790\",\"totpCode\":\"$(totp "$SECRET")\"}")" 409 FUND_PASSWORD_EXISTS "set twice"
expect_err "$(IT c0 ",\"totpCode\":\"$(totp "$SECRET")\"")" 400 FUND_PASSWORD_REQUIRED "itx without fund pw"
expect_err "$(IT c1 ",\"totpCode\":\"$(totp "$SECRET")\",\"fundPassword\":\"000000\"")" 400 FUND_PASSWORD_INVALID "itx wrong fund pw"
ok "$(IT c2 ",\"totpCode\":\"$(totp "$SECRET")\",\"fundPassword\":\"135790\"")" "itx right fund pw" >/dev/null
ok "$(api "$TA" PUT /account/fund-password "{\"oldFundPassword\":\"135790\",\"fundPassword\":\"246801\",\"totpCode\":\"$(totp "$SECRET")\"}")" "change fund pw" >/dev/null
expect_err "$(IT c3 ",\"totpCode\":\"$(totp "$SECRET")\",\"fundPassword\":\"246801\"")" 403 WITHDRAW_LOCKED "locked after fund pw change"
OV="$(ok "$(api "$TA" GET /account/security)" overview)"
jq -e '.fundPasswordSet and .withdrawLockedUntil!=null' <<<"$OV" >/dev/null || fail "overview after fund pw: $OV"
pass "withdraw lock visible in overview"

say "5. admin security policy (/admin/v1)"
SA="$(alogin "$SA_USER" "$SA_PASS" "$SA_TOTP")"; [ -n "$SA" ] || fail "admin login"
R="$(A "$SA" GET /admin/v1/security-settings)"
jq -e '.withdrawLockHours==24 and .fundPasswordMaxAttempts==5 and .fundPasswordForWithdraw' <<<"$R" >/dev/null || fail "get settings: $R"
R="$(A "$SA" PUT /admin/v1/security-settings '{"withdrawLockHours":0,"totpCode":"000000","reason":"smoke"}')"
[ "$(jq -r .code <<<"$R")" = TOTP_INVALID ] || fail "admin put bad totp: $R"
R="$(A "$SA" PUT /admin/v1/security-settings "{\"withdrawLockHours\":0,\"totpCode\":\"$(totp "$SA_TOTP")\"}")"
[ "$(jq -r .code <<<"$R")" = REASON_REQUIRED ] || fail "admin put no reason: $R"
R="$(A "$SA" PUT /admin/v1/security-settings "{\"withdrawLockHours\":0,\"totpCode\":\"$(totp "$SA_TOTP")\",\"reason\":\"smoke\"}")"
jq -e '.before.withdrawLockHours==24 and .after.withdrawLockHours==0' <<<"$R" >/dev/null || fail "admin put: $R"
R="$(A "$SA" DELETE "/admin/v1/users/$UA/withdraw-lock" "{\"totpCode\":\"$(totp "$SA_TOTP")\",\"reason\":\"smoke unlock\"}")"
jq -e '.after.withdrawLockedUntil==null' <<<"$R" >/dev/null || fail "admin unlock: $R"
ok "$(IT d0 ",\"totpCode\":\"$(totp "$SECRET")\",\"fundPassword\":\"246801\"")" "itx after admin unlock" >/dev/null
pass "admin policy + unlock"

say "6. fund password lockout (5 tries) + reset"
for i in 1 2 3 4; do
  expect_err "$(IT e$i ",\"totpCode\":\"$(totp "$SECRET")\",\"fundPassword\":\"111111\"")" 400 FUND_PASSWORD_INVALID "wrong #$i"
done
expect_err "$(IT e5 ",\"totpCode\":\"$(totp "$SECRET")\",\"fundPassword\":\"111111\"")" 423 FUND_PASSWORD_LOCKED "wrong #5 locks"
expect_err "$(IT e6 ",\"totpCode\":\"$(totp "$SECRET")\",\"fundPassword\":\"246801\"")" 423 FUND_PASSWORD_LOCKED "right pw while locked"
ok "$(api "$TA" POST /account/fund-password/reset "{\"fundPassword\":\"975310\",\"totpCode\":\"$(totp "$SECRET")\"}")" "reset fund pw" >/dev/null
ok "$(IT e7 ",\"totpCode\":\"$(totp "$SECRET")\",\"fundPassword\":\"975310\"")" "itx after reset (lock hours=0)" >/dev/null

say "7. anti-phishing"
ok "$(api "$TA" PUT /account/anti-phishing "{\"antiPhishingCode\":\"Lb2026x\",\"totpCode\":\"$(totp "$SECRET")\"}")" "set anti-phishing" >/dev/null
[ -n "$(seccode "$TA" email "$EA")" ] || fail "code still delivered with anti-phishing prefix"
jq -e '.antiPhishingSet and .antiPhishingHint=="L******"' <<<"$(ok "$(api "$TA" GET /account/security)" ov)" >/dev/null || fail "anti-phishing overview"
pass "anti-phishing"

say "8. sessions + password change (revokes others)"
S="$(ok "$(apid "$TA" dev-main GET /account/sessions)" sessions)"
jq -e '(.items|length)>=1' <<<"$S" >/dev/null || fail "sessions: $S"
expect_err "$(apid "$TA" dev-main POST /account/password/change "{\"oldPassword\":\"wrong-pass-1\",\"newPassword\":\"$NEWPASS\",\"totpCode\":\"$(totp "$SECRET")\"}")" 400 OLD_PASSWORD_INVALID "wrong old pw"
expect_err "$(apid "$TA" dev-main POST /account/password/change "{\"oldPassword\":\"$PASSWORD\",\"newPassword\":\"$NEWPASS\",\"totpCode\":\"000000\"}")" 401 INVALID_TOTP "pw change wrong totp"
R="$(ok "$(apid "$TA" dev-main POST /account/password/change "{\"oldPassword\":\"$PASSWORD\",\"newPassword\":\"$NEWPASS\",\"totpCode\":\"$(totp "$SECRET")\"}")" "change pw")"
jq -e '.ok and .revokedSessions>=1' <<<"$R" >/dev/null || fail "pw change: $R"
C="$(curl -sS -o /dev/null -w '%{http_code}' -X POST "$GATEWAY_URL/auth/refresh" -H 'Content-Type: application/json' -d "{\"refreshToken\":\"$RA\"}")"
[ "$C" = 401 ] || fail "old session refresh should be 401, got $C"
pass "other session revoked"
# Password change locks for withdrawLockHours (0 now): restore 24h and verify lock applies.
A "$SA" PUT /admin/v1/security-settings "{\"withdrawLockHours\":24,\"totpCode\":\"$(totp "$SA_TOTP")\",\"reason\":\"smoke restore\"}" >/dev/null
ok "$(api "$TA" POST /2fa/totp/disable "{\"code\":\"$(totp "$SECRET")\"}")" "disable 2fa" >/dev/null
ok "$(api "$TA" POST /account/code/send '{"purpose":"withdraw"}')" "withdraw code" >/dev/null
expect_err "$(IT f0 ",\"code\":\"$(lastcode "$EA" withdraw)\",\"fundPassword\":\"975310\"")" 403 WITHDRAW_LOCKED "locked after 2FA disable"
R2="$(ok "$(apid "$TA" dev-main POST /account/sessions/revoke-others)" revoke-others)"
jq -e 'has("revoked")' <<<"$R2" >/dev/null || fail "revoke-others: $R2"
SID="$(ok "$(apid "$TA" dev-main GET /account/sessions)" sessions | jq -r '.items[0].id // empty')"
if [ -n "$SID" ]; then
  [ "$(code "$(apid "$TA" dev-main DELETE "/account/sessions/$SID")")" = 204 ] || fail "delete session"
fi
expect_err "$(apid "$TA" dev-main DELETE /account/sessions/999999999)" 404 NOT_FOUND "delete unknown session"

say "9. email + phone change (old + new codes)"
NE="secnew+$TS@example.com"
expect_err "$(api "$TA" POST /account/contact/code "{\"email\":\"secb+$TS@example.com\"}")" 409 EMAIL_EXISTS "email taken"
ok "$(api "$TA" POST /account/contact/code "{\"email\":\"$NE\"}")" "new email code" >/dev/null
NEWC="$(lastcode "$NE" bind_new)"
OLDC="$(seccode "$TA" email "$EA")"
R="$(api "$TA" PUT /account/email "{\"email\":\"$NE\",\"newCode\":\"$NEWC\",\"oldCode\":\"000000\"}")"; [ "$(code "$R")" != 200 ] || fail "wrong old code accepted"
OLDC="$(seccode "$TA" email "$EA")"
ok "$(api "$TA" POST /account/contact/code "{\"email\":\"$NE\"}")" "new email code" >/dev/null
NEWC="$(lastcode "$NE" bind_new)"
ok "$(api "$TA" PUT /account/email "{\"email\":\"$NE\",\"newCode\":\"$NEWC\",\"oldCode\":\"$OLDC\"}")" "change email" >/dev/null
PH="139${TS: -8}"
ok "$(api "$TA" POST /account/contact/code "{\"phone\":\"$PH\",\"countryCode\":\"+86\"}")" "phone code" >/dev/null
PC="$(lastcode "+86$PH" bind_new)"; [ -n "$PC" ] || PC="$(lastcode "$PH" bind_new)"
OLDC="$(seccode "$TA" email "$NE")"
ok "$(api "$TA" PUT /account/phone "{\"phone\":\"$PH\",\"countryCode\":\"+86\",\"newCode\":\"$PC\",\"oldCode\":\"$OLDC\"}")" "bind phone" >/dev/null
OV="$(ok "$(api "$TA" GET /account/security)" overview)"
jq -e '(.phone|test("\\*\\*\\*\\*")) and .phoneVerified and (.email|startswith("s"))' <<<"$OV" >/dev/null || fail "overview contacts: $OV"
pass "email + phone changed, phone masked"

say "10. passkeys (ceremony begin; RP=${WEBAUTHN_RP_ID:-unset})"
L="$(ok "$(api "$TA" GET /account/passkeys)" passkeys)"
jq -e '.items==[]' <<<"$L" >/dev/null || fail "passkeys list: $L"
if jq -e '.enabled' <<<"$L" >/dev/null; then
  ok "$(api "$TA" POST /account/code/send '{"purpose":"security"}')" "sec code" >/dev/null
  R="$(ok "$(api "$TA" POST /account/passkeys/register/begin "{\"code\":\"$(lastcode "+86$PH" security)\"}")" "register begin")"
  jq -e '(.sessionId|length)>10 and (.publicKey.challenge|length)>10 and .publicKey.rp.id!=null and .publicKey.authenticatorSelection.residentKey=="required"' <<<"$R" >/dev/null || fail "register begin: $R"
  expect_err "$(api "$TA" POST /account/passkeys/register/finish "{\"sessionId\":\"$(jq -r .sessionId <<<"$R")\",\"credential\":{\"id\":\"x\"}}")" 400 PASSKEY_INVALID "register finish garbage"
  R="$(curl -sS -X POST "$GATEWAY_URL/auth/passkey/login/begin" -H 'Content-Type: application/json')"
  jq -e '(.sessionId|length)>10 and (.publicKey.challenge|length)>10' <<<"$R" >/dev/null || fail "login begin (public): $R"
  C="$(curl -sS -o /dev/null -w '%{http_code}' -X POST "$GATEWAY_URL/auth/passkey/login/finish" -H 'Content-Type: application/json' -d '{"sessionId":"nope","credential":{"id":"x"}}')"
  [ "$C" = 400 ] || fail "login finish bad session: $C"
  pass "passkey ceremonies"
else
  pass "passkeys disabled (WEBAUTHN_RP_ID unset) — skipped"
fi

printf '\n\033[1;32mSMOKE-SECURITY PASSED\033[0m\n'
