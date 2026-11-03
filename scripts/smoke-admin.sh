#!/usr/bin/env bash
#
# End-to-end smoke test for the Linbit admin (back-office) service:
#
#   login (password + TOTP, lockout-safe failures) -> RBAC -> admin accounts
#   (create / disable, never delete) -> users (PII masking, reveal, freeze /
#   unfreeze) -> KYC review proxy -> two-person withdrawal review -> ledger /
#   balances -> symbols -> fee change (super_admin + finance_second dual
#   approval, 24h announcement, scheduled apply, rollback) -> coupons (budget
#   > threshold needs finance) -> rewards (> threshold needs finance review)
#   -> append-only audit log.
#
#   bash scripts/smoke-admin.sh                 # boots the devstack (no Docker)
#   SMOKE_INNER=1 bash scripts/smoke-admin.sh   # against a running stack
#
set -euo pipefail
cd "$(dirname "$0")/.."

if [ "${SMOKE_INNER:-}" != "1" ]; then
  echo "== booting devstack (embedded postgres + miniredis + services) =="
  ROOT="$(pwd)"
  cd tools/devstack
  exec go run . -root "$ROOT" -exec "SMOKE_INNER=1 bash scripts/smoke-admin.sh"
fi

GATEWAY_URL="${GATEWAY_URL:-http://localhost:8080}"
ACCOUNT_URL="${ACCOUNT_URL:-http://localhost:8081}"
WALLET_URL="${WALLET_URL:-http://localhost:8086}"
ADMIN_URL="${ADMIN_URL:-http://localhost:8088}"
INTERNAL_TOKEN="${INTERNAL_TOKEN:-}"
SA_USER="${ADMIN_BOOTSTRAP_USERNAME:-}"
SA_PASS="${ADMIN_BOOTSTRAP_PASSWORD:-}"
SA_TOTP="${ADMIN_BOOTSTRAP_TOTP_SECRET:-}"
PASSWORD="Str0ng-Passw0rd!"
APASS="Adm1n-Passw0rd!"

say()  { printf '\n\033[1;34m== %s ==\033[0m\n' "$1"; }
fail() { printf '\033[1;31mSMOKE-ADMIN FAILED: %s\033[0m\n' "$1" >&2; exit 1; }
ok()   { printf '  ok  %s\n' "$1"; }

command -v jq >/dev/null 2>&1 || fail "jq is required"
command -v python3 >/dev/null 2>&1 || fail "python3 is required (TOTP)"
[ -n "$INTERNAL_TOKEN" ] || fail "INTERNAL_TOKEN not set"
[ -n "$SA_USER" ] && [ -n "$SA_PASS" ] && [ -n "$SA_TOTP" ] || fail "ADMIN_BOOTSTRAP_* not set"

totp() { # totp <base32 secret> -> current 6-digit code (RFC 6238, SHA1, 30s)
  python3 - "$1" <<'PY'
import base64, hmac, struct, sys, time
s = sys.argv[1].strip().upper(); s += "=" * (-len(s) % 8)
k = base64.b32decode(s); c = struct.pack(">Q", int(time.time()) // 30)
h = hmac.new(k, c, "sha1").digest(); o = h[-1] & 15
print("%06d" % ((struct.unpack(">I", h[o:o+4])[0] & 0x7fffffff) % 1000000))
PY
}
code_of() { jq -r '.code // empty' <<<"$1"; }
# A() <token> <method> <path> [json] -> body
A() {
  if [ -n "${4:-}" ]; then
    curl -sS -X "$2" "$ADMIN_URL$3" -H 'Content-Type: application/json' -H "Authorization: Bearer $1" -d "$4"
  else
    curl -sS -X "$2" "$ADMIN_URL$3" -H "Authorization: Bearer $1"
  fi
}
AH() { # status code only
  curl -sS -o /dev/null -w '%{http_code}' -X "$2" "$ADMIN_URL$3" -H 'Content-Type: application/json' -H "Authorization: Bearer $1" ${4:+-d "$4"}
}
login() { # login <user> <pass> <secret> -> token
  curl -sS -X POST "$ADMIN_URL/admin/v1/auth/login" -H 'Content-Type: application/json' \
    -d "{\"username\":\"$1\",\"password\":\"$2\",\"totpCode\":\"$(totp "$3")\"}" | jq -r '.token // empty'
}
last_code() {
  curl -sS -G "$ACCOUNT_URL/internal/dev/last-code" --data-urlencode "target=$1" \
    --data-urlencode "purpose=$2" -H "X-Internal-Token: $INTERNAL_TOKEN" | jq -r '.code // empty'
}
api() {
  if [ -n "${4:-}" ]; then
    curl -sS -X "$1" "$GATEWAY_URL$2" -H 'Content-Type: application/json' -H "Authorization: Bearer $3" -d "$4"
  else
    curl -sS -X "$1" "$GATEWAY_URL$2" -H "Authorization: Bearer $3"
  fi
}
winternal() {
  curl -sS -X "$1" "$WALLET_URL$2" -H 'Content-Type: application/json' -H "X-Internal-Token: $INTERNAL_TOKEN" -d "${3:-}"
}

TS="$(date +%s)"

say "1/12 login: password + TOTP; failures audited"
[ "$(curl -sS -o /dev/null -w '%{http_code}' "$ADMIN_URL/admin/v1/me")" = "401" ] || fail "/me open without session"
R="$(curl -sS -X POST "$ADMIN_URL/admin/v1/auth/login" -H 'Content-Type: application/json' -d "{\"username\":\"$SA_USER\",\"password\":\"wrong-password\",\"totpCode\":\"000000\"}")"
[ "$(code_of "$R")" = "AUTH_INVALID_CREDENTIALS" ] || fail "bad password: $R"
R="$(curl -sS -X POST "$ADMIN_URL/admin/v1/auth/login" -H 'Content-Type: application/json' -d "{\"username\":\"$SA_USER\",\"password\":\"$SA_PASS\",\"totpCode\":\"000000\"}")"
[ "$(code_of "$R")" = "TOTP_INVALID" ] || fail "bad totp: $R"
SA="$(login "$SA_USER" "$SA_PASS" "$SA_TOTP")"; [ -n "$SA" ] || fail "super admin login failed"
[ "$(A "$SA" GET /admin/v1/me | jq -r '.roles[0]')" = "super_admin" ] || fail "me roles"
ok "bad password / bad TOTP rejected; super admin logged in"

say "2/12 create operators; RBAC enforced"
mk() { # mk <name> <role> -> sets <name>_SECRET and <name>_TOK
  local R; R="$(A "$SA" POST /admin/v1/admins "{\"username\":\"$1$TS\",\"password\":\"$APASS\",\"roles\":[\"$2\"],\"totpCode\":\"$(totp "$SA_TOTP")\"}")"
  local sec; sec="$(jq -r '.totpSecret // empty' <<<"$R")"; [ -n "$sec" ] || fail "create $1: $R"
  printf -v "${1}_SECRET" '%s' "$sec"
  printf -v "${1}_ID" '%s' "$(jq -r .admin.id <<<"$R")"
  local t; t="$(login "$1$TS" "$APASS" "$sec")"; [ -n "$t" ] || fail "login $1"
  printf -v "${1}_TOK" '%s' "$t"
}
mk sa2 super_admin; mk fin1 finance_first; mk fin2 finance_second; mk kycr kyc_reviewer; mk mkt marketing; mk aud auditor
R="$(A "$SA" POST /admin/v1/admins "{\"username\":\"weak$TS\",\"password\":\"short\",\"roles\":[\"support\"],\"totpCode\":\"$(totp "$SA_TOTP")\"}")"
[ "$(code_of "$R")" = "PASSWORD_TOO_WEAK" ] || fail "weak password accepted: $R"
R="$(A "$SA" POST /admin/v1/admins "{\"username\":\"bad$TS\",\"password\":\"$APASS\",\"roles\":[\"god\"],\"totpCode\":\"$(totp "$SA_TOTP")\"}")"
[ "$(code_of "$R")" = "VALIDATION_ERROR" ] || fail "unknown role accepted: $R"
[ "$(AH "$mkt_TOK" GET /admin/v1/users)" = "403" ] || fail "marketing can list users"
[ "$(AH "$kycr_TOK" GET /admin/v1/withdrawals)" = "403" ] || fail "kyc reviewer can see withdrawals"
[ "$(AH "$fin1_TOK" POST /admin/v1/admins '{}')" = "403" ] || fail "finance can create admins"
[ "$(AH "$aud_TOK" GET /admin/v1/audit-logs)" = "200" ] || fail "auditor cannot read audit logs"
ok "operators created; weak password / unknown role rejected; RBAC matrix enforced"

say "3/12 disable (not delete) an operator; its session dies"
R="$(A "$SA" PATCH "/admin/v1/admins/$aud_ID" "{\"status\":\"disabled\",\"reason\":\"smoke\",\"totpCode\":\"$(totp "$SA_TOTP")\"}")"
[ "$(jq -r .status <<<"$R")" = "disabled" ] || fail "disable: $R"
[ "$(AH "$aud_TOK" GET /admin/v1/me)" = "401" ] || fail "disabled admin session still valid"
[ -z "$(login "aud$TS" "$APASS" "$aud_SECRET")" ] || fail "disabled admin can log in"
[ "$(A "$SA" GET /admin/v1/admins | jq --arg u "aud$TS" '[.items[] | select(.username==$u)] | length')" = "1" ] || fail "disabled admin vanished"
[ "$(AH "$SA" DELETE "/admin/v1/admins/$aud_ID")" = "405" ] || fail "DELETE admin should not exist"
ok "disabled; session revoked; record kept; no delete route"

say "4/12 users: masked PII, audited reveal, freeze/unfreeze"
EMAIL="adm${TS}@example.com"
curl -sS -o /dev/null -X POST "$GATEWAY_URL/auth/code/send" -H 'Content-Type: application/json' -d "{\"purpose\":\"register\",\"email\":\"$EMAIL\"}"
TOKEN="$(curl -sS -X POST "$GATEWAY_URL/auth/register" -H 'Content-Type: application/json' \
  -d "{\"email\":\"$EMAIL\",\"password\":\"$PASSWORD\",\"code\":\"$(last_code "$EMAIL" register)\"}" | jq -r '.accessToken // empty')"
[ -n "$TOKEN" ] || fail "register"
UID_="$(api GET /account "$TOKEN" | jq -r .uid)"
L="$(A "$kycr_TOK" GET "/admin/v1/users?q=$EMAIL")"
[ "$(jq -r '.items[0].email' <<<"$L")" = "ad***@example.com" ] || fail "email not masked: $L"
[ "$(AH "$kycr_TOK" GET "/admin/v1/users/$UID_?reveal=true")" = "403" ] || fail "reveal without user:pii"
[ "$(A "$SA" GET "/admin/v1/users/$UID_?reveal=true" | jq -r .email)" = "$EMAIL" ] || fail "reveal for super admin"
R="$(A "$SA" POST "/admin/v1/users/$UID_/freeze" "{\"reason\":\"\",\"totpCode\":\"$(totp "$SA_TOTP")\"}")"
[ "$(code_of "$R")" = "REASON_REQUIRED" ] || fail "freeze without reason: $R"
R="$(A "$SA" POST "/admin/v1/users/$UID_/freeze" '{"reason":"风控调查","totpCode":"000000"}')"
[ "$(code_of "$R")" = "TOTP_INVALID" ] || fail "freeze with bad totp: $R"
R="$(A "$SA" POST "/admin/v1/users/$UID_/freeze" "{\"reason\":\"风控调查\",\"totpCode\":\"$(totp "$SA_TOTP")\"}")"
[ "$(jq -r .status <<<"$R")" = "frozen" ] || fail "freeze: $R"
R="$(curl -sS -X POST "$GATEWAY_URL/auth/login" -H 'Content-Type: application/json' -d "{\"email\":\"$EMAIL\",\"password\":\"$PASSWORD\"}")"
[ "$(code_of "$R")" = "ACCOUNT_FROZEN" ] || fail "frozen user can log in: $R"
R="$(A "$SA" POST "/admin/v1/users/$UID_/unfreeze" "{\"reason\":\"调查结束\",\"totpCode\":\"$(totp "$SA_TOTP")\"}")"
[ "$(jq -r .status <<<"$R")" = "active" ] || fail "unfreeze: $R"
ok "masked for kyc_reviewer; reveal needs user:pii; freeze needs reason+2FA and blocks login"

say "5/12 KYC review via admin (kyc_reviewer)"
[ "$(AH "$kycr_TOK" GET '/admin/v1/kyc/applications?status=pending')" = "200" ] || fail "kyc list"
[ "$(AH "$mkt_TOK" GET '/admin/v1/kyc/applications')" = "403" ] || fail "marketing sees kyc"
R="$(A "$kycr_TOK" POST /admin/v1/kyc/applications/999999/reject '{"reason":""}')"
[ "$(code_of "$R")" = "REASON_REQUIRED" ] || fail "kyc reject w/o reason: $R"
ok "kyc queue proxied; reject requires a reason"

say "6/12 two-person withdrawal review"
curl -sS -o /dev/null -X POST "$ACCOUNT_URL/internal/account/kyc-level" -H 'Content-Type: application/json' \
  -H "X-Internal-Token: $INTERNAL_TOKEN" -d "{\"uid\":\"$UID_\",\"level\":2}"
DEP="$(api GET "/wallet/deposit/address?asset=USDT&network=TRC20" "$TOKEN" | jq -r .address)"
winternal POST /internal/dev/chain/deposit "{\"network\":\"TRC20\",\"address\":\"$DEP\",\"asset\":\"USDT\",\"amount\":\"30000\",\"txHash\":\"0xadm$TS\",\"logIndex\":0}" >/dev/null
winternal POST /internal/dev/chain/advance '{"network":"TRC20","blocks":25}' >/dev/null
winternal POST /internal/scan '' >/dev/null
withdraw() {
  curl -sS -o /dev/null -X POST "$GATEWAY_URL/account/code/send" -H 'Content-Type: application/json' \
    -H "Authorization: Bearer $TOKEN" -d '{"purpose":"withdraw"}'
  api POST /wallet/withdraw "$TOKEN" "{\"asset\":\"USDT\",\"network\":\"TRC20\",\"address\":\"TAdmDest0001\",\"amount\":\"$1\",\"code\":\"$(last_code "$EMAIL" withdraw)\"}" | jq -r '.id // empty'
}
W1="$(withdraw 2000)"; W2="$(withdraw 6000)"; W3="$(withdraw 7000)"
[ -n "$W1" ] && [ -n "$W2" ] && [ -n "$W3" ] || fail "withdrawals not created"
Q="$(A "$fin1_TOK" GET /admin/v1/withdrawals)"
[ "$(jq -r --arg i "$W1" '.items[] | select(.id==$i) | .review.needsSecondReview' <<<"$Q")" = "false" ] || fail "2000 needs second? $Q"
[ "$(jq -r --arg i "$W2" '.items[] | select(.id==$i) | .review.needsSecondReview' <<<"$Q")" = "true" ] || fail "6000 needs no second? $Q"
R="$(A "$fin1_TOK" POST "/admin/v1/withdrawals/$W1/approve" "{\"comment\":\"\",\"totpCode\":\"$(totp "$fin1_SECRET")\"}")"
[ "$(code_of "$R")" = "COMMENT_REQUIRED" ] || fail "approve without comment: $R"
R="$(A "$fin1_TOK" POST "/admin/v1/withdrawals/$W1/approve" '{"comment":"ok","totpCode":"000000"}')"
[ "$(code_of "$R")" = "TOTP_INVALID" ] || fail "approve bad totp: $R"
R="$(A "$fin1_TOK" POST "/admin/v1/withdrawals/$W1/approve" "{\"comment\":\"核实无误\",\"totpCode\":\"$(totp "$fin1_SECRET")\"}")"
[ "$(jq -r .status <<<"$R")" = "processing" ] || fail "single-review approve: $R"
ok "<5000 USDT: one reviewer + comment + 2FA releases it"
R="$(A "$fin2_TOK" POST "/admin/v1/withdrawals/$W2/approve" "{\"comment\":\"x\",\"totpCode\":\"$(totp "$fin2_SECRET")\"}")"
[ "$(code_of "$R")" = "FORBIDDEN" ] || fail "finance_second did first review: $R"
R="$(A "$fin1_TOK" POST "/admin/v1/withdrawals/$W2/approve" "{\"comment\":\"初审通过\",\"totpCode\":\"$(totp "$fin1_SECRET")\"}")"
[ "$(jq -r .status <<<"$R")" = "awaiting_second_review" ] || fail "first review of 6000: $R"
R="$(A "$fin1_TOK" POST "/admin/v1/withdrawals/$W2/approve" "{\"comment\":\"再审\",\"totpCode\":\"$(totp "$fin1_SECRET")\"}")"
[ "$(code_of "$R")" = "FORBIDDEN" ] || fail "finance_first did second review: $R"
R="$(A "$fin2_TOK" POST "/admin/v1/withdrawals/$W2/approve" "{\"comment\":\"复审通过\",\"totpCode\":\"$(totp "$fin2_SECRET")\"}")"
[ "$(jq -r .status <<<"$R")" = "processing" ] || fail "second review: $R"
ok ">=5000 USDT: finance_first then finance_second"
R="$(A "$SA" POST "/admin/v1/withdrawals/$W3/approve" "{\"comment\":\"初审\",\"totpCode\":\"$(totp "$SA_TOTP")\"}")"
[ "$(jq -r .status <<<"$R")" = "awaiting_second_review" ] || fail "SA first review: $R"
R="$(A "$SA" POST "/admin/v1/withdrawals/$W3/approve" "{\"comment\":\"复审\",\"totpCode\":\"$(totp "$SA_TOTP")\"}")"
[ "$(code_of "$R")" = "SAME_REVIEWER" ] || fail "same person did both reviews: $R"
R="$(A "$fin2_TOK" POST "/admin/v1/withdrawals/$W3/reject" "{\"reason\":\"地址存疑\",\"totpCode\":\"$(totp "$fin2_SECRET")\"}")"
[ "$(jq -r .status <<<"$R")" = "canceled" ] || fail "reject: $R"
R="$(A "$fin2_TOK" POST "/admin/v1/withdrawals/$W3/approve" "{\"comment\":\"x\",\"totpCode\":\"$(totp "$fin2_SECRET")\"}")"
[ "$(code_of "$R")" = "NOT_PENDING" ] || fail "approve after reject: $R"
ok "second reviewer must differ; reject releases; no approve after reject"

say "7/12 ledger + balances"
B="$(A "$fin1_TOK" GET "/admin/v1/users/$UID_/balances")"
jq -e '.items[] | select(.asset=="USDT")' <<<"$B" >/dev/null || fail "balances: $B"
LG="$(A "$fin1_TOK" GET "/admin/v1/ledger?userId=$UID_")"
[ "$(jq '.items | length' <<<"$LG")" -ge 1 ] || fail "ledger: $LG"
ok "balances + ledger readable by finance"

say "8/12 trading pairs"
SYM="T${TS: -4}X"
R="$(A "$SA" POST /admin/v1/symbols "{\"base\":\"$SYM\",\"quote\":\"USDT\",\"pricePrecision\":4,\"quantityPrecision\":2,\"minQuantity\":\"0.01\",\"minNotional\":\"5\"}")"
[ "$(jq -r .status <<<"$R")" = "halted" ] || fail "create symbol: $R"
R="$(A "$SA" POST /admin/v1/symbols "{\"base\":\"$SYM\",\"quote\":\"USDT\",\"pricePrecision\":4,\"quantityPrecision\":2,\"minQuantity\":\"0.01\",\"minNotional\":\"5\"}")"
[ "$(code_of "$R")" = "SYMBOL_EXISTS" ] || fail "duplicate symbol: $R"
R="$(A "$SA" PATCH "/admin/v1/symbols/${SYM}USDT" '{"status":"trading"}')"
[ "$(code_of "$R")" = "REASON_REQUIRED" ] || fail "patch w/o reason: $R"
R="$(A "$SA" PATCH "/admin/v1/symbols/${SYM}USDT" '{"status":"trading","reason":"上线"}')"
[ "$(jq -r .status <<<"$R")" = "trading" ] || fail "patch symbol: $R"
[ "$(AH "$mkt_TOK" POST /admin/v1/symbols '{}')" = "403" ] || fail "marketing can create symbols"
ok "create (halted by default), duplicate 409, enable with reason"

say "9/12 fee change: dual approval + 24h notice + scheduled apply + rollback"
OLD="$(A "$SA" GET /admin/v1/fees | jq -r .current.spotMaker)"
R="$(A "$SA" POST /admin/v1/fee-changes '{"spotMaker":"0.5","reason":"x"}')"
[ "$(code_of "$R")" = "VALIDATION_ERROR" ] || fail "fee 0.5 accepted: $R"
R="$(A "$SA" POST /admin/v1/fee-changes '{"spotMaker":"0.0007","reason":"市场调整"}')"
FC="$(jq -r .id <<<"$R")"; [ "$(jq -r .status <<<"$R")" = "pending" ] || fail "submit: $R"
[ "$(code_of "$(A "$SA" POST "/admin/v1/fee-changes/$FC/approve" "{\"totpCode\":\"$(totp "$SA_TOTP")\"}")")" = "SELF_APPROVAL" ] || fail "submitter approved"
[ "$(AH "$fin1_TOK" POST "/admin/v1/fee-changes/$FC/approve" '{}')" = "403" ] || fail "finance_first approved fee"
R="$(A "$fin2_TOK" POST "/admin/v1/fee-changes/$FC/approve" "{\"comment\":\"财务同意\",\"totpCode\":\"$(totp "$fin2_SECRET")\"}")"
[ "$(jq -r .status <<<"$R")" = "pending" ] || fail "after finance approval: $R"
[ "$(code_of "$(A "$fin2_TOK" POST "/admin/v1/fee-changes/$FC/approve" "{\"totpCode\":\"$(totp "$fin2_SECRET")\"}")")" = "ALREADY_DECIDED" ] || fail "double vote"
R="$(A "$sa2_TOK" POST "/admin/v1/fee-changes/$FC/approve" "{\"comment\":\"同意\",\"totpCode\":\"$(totp "$sa2_SECRET")\"}")"
[ "$(jq -r .status <<<"$R")" = "scheduled" ] || fail "after dual approval: $R"
EFF="$(jq -r .effectiveAt <<<"$R")"
python3 -c "import sys,datetime as d;e=d.datetime.fromisoformat(sys.argv[1].replace('Z','+00:00'));n=d.datetime.now(d.timezone.utc);sys.exit(0 if e-n>d.timedelta(hours=23,minutes=59) else 1)" "$EFF" || fail "effectiveAt not >=24h out: $EFF"
curl -sS "$ADMIN_URL/announcements" | jq -e '.items[] | select(.category=="fee") | select(.body|contains("0.0007"))' >/dev/null || fail "no public fee announcement"
ok "dual approval -> scheduled at $EFF, public announcement published"
[ "$(A "$SA" POST /admin/v1/dev/run-scheduler '{}' | jq -r .applied)" = "0" ] || fail "applied before notice period"
[ "$(A "$SA" GET /admin/v1/fees | jq -r .current.spotMaker)" = "$OLD" ] || fail "fee changed early"
LATER="$(python3 -c 'import datetime as d;print((d.datetime.now(d.timezone.utc)+d.timedelta(hours=25)).strftime("%Y-%m-%dT%H:%M:%SZ"))')"
[ "$(A "$SA" POST /admin/v1/dev/run-scheduler "{\"now\":\"$LATER\"}" | jq -r .applied)" = "1" ] || fail "not applied after notice"
[ "$(A "$SA" GET /admin/v1/fees | jq -r .current.spotMaker)" = "0.0007" ] || fail "order fee not updated"
ok "not applied before 24h; applied after; order service serves 0.0007"
R="$(A "$SA" POST "/admin/v1/fee-changes/$FC/rollback" '{"reason":"回滚"}')"
[ "$(jq -r .status <<<"$R")" = "pending" ] && [ "$(jq -r .payload.spotMaker <<<"$R")" = "$OLD" ] && [ "$(jq -r .rollbackOf <<<"$R")" = "$FC" ] || fail "rollback: $R"
RB="$(jq -r .id <<<"$R")"
R="$(A "$fin2_TOK" POST "/admin/v1/fee-changes/$RB/reject" '{"comment":""}')"
[ "$(code_of "$R")" = "REASON_REQUIRED" ] || fail "reject w/o reason: $R"
[ "$(A "$fin2_TOK" POST "/admin/v1/fee-changes/$RB/reject" '{"comment":"暂不回滚"}' | jq -r .status)" = "rejected" ] || fail "reject rollback"
ok "rollback opens a new change (same approvals); finance can reject it"

say "10/12 coupons: budget > 10000 needs finance approval"
R="$(A "$mkt_TOK" POST /admin/v1/coupons '{"name":"新人券","type":"cash","faceValue":"10","quantity":500,"validDays":30}')"
[ "$(jq -r .status <<<"$R")" = "active" ] && [ "$(jq -r .budget <<<"$R")" = "5000" ] || fail "small coupon: $R"
R="$(A "$mkt_TOK" POST /admin/v1/coupons '{"name":"大促券","type":"cash","faceValue":"20","quantity":1000,"validDays":7}')"
CP="$(jq -r .id <<<"$R")"; [ "$(jq -r .status <<<"$R")" = "pending_finance" ] || fail "big coupon: $R"
[ "$(AH "$mkt_TOK" POST "/admin/v1/coupons/$CP/approve" '{}')" = "403" ] || fail "marketing approved coupon"
[ "$(A "$fin1_TOK" POST "/admin/v1/coupons/$CP/approve" '{"comment":"预算已确认"}' | jq -r .status)" = "active" ] || fail "finance approve coupon"
[ "$(code_of "$(A "$fin1_TOK" POST "/admin/v1/coupons/$CP/approve" '{}')")" = "NOT_PENDING" ] || fail "double coupon approve"
ok "5000 budget auto-active; 20000 waits for finance"

say "11/12 rewards: > 1000 USDT needs finance_second review"
bal() { api GET /assets/balances "$TOKEN" | jq -r '.items[] | select(.asset=="USDT") | .free'; }
B0="$(bal)"
R="$(A "$mkt_TOK" POST /admin/v1/rewards "{\"userId\":\"$UID_\",\"asset\":\"USDT\",\"amount\":\"500\",\"reason\":\"活动补发\"}")"
[ "$(jq -r .status <<<"$R")" = "executed" ] || fail "small reward: $R"
R="$(A "$mkt_TOK" POST /admin/v1/rewards "{\"userId\":\"$UID_\",\"asset\":\"USDT\",\"amount\":\"1500\",\"reason\":\"大额补发\"}")"
RW="$(jq -r .id <<<"$R")"; [ "$(jq -r .status <<<"$R")" = "pending_finance" ] || fail "big reward: $R"
[ "$(AH "$fin1_TOK" POST "/admin/v1/rewards/$RW/approve" '{}')" = "403" ] || fail "finance_first approved reward"
R="$(A "$fin2_TOK" POST "/admin/v1/rewards/$RW/approve" "{\"comment\":\"核实\",\"totpCode\":\"$(totp "$fin2_SECRET")\"}")"
[ "$(jq -r .status <<<"$R")" = "executed" ] || fail "reward approve: $R"
R="$(A "$SA" POST /admin/v1/rewards "{\"userId\":\"$UID_\",\"asset\":\"USDT\",\"amount\":\"2000\",\"reason\":\"自审测试\"}")"
SW="$(jq -r .id <<<"$R")"
[ "$(code_of "$(A "$SA" POST "/admin/v1/rewards/$SW/approve" "{\"totpCode\":\"$(totp "$SA_TOTP")\"}")")" = "SELF_APPROVAL" ] || fail "self-approved reward"
B1="$(bal)"
python3 -c "import sys;from decimal import Decimal as D;sys.exit(0 if D(sys.argv[2])-D(sys.argv[1])==D(2000) else 1)" "$B0" "$B1" || fail "balance delta $B0 -> $B1, want +2000"
ok "500 executed; 1500 after finance_second; self-approval blocked; balance +2000"

say "12/12 audit log: recorded and append-only"
AL="$(A "$SA" GET "/admin/v1/audit-logs?module=withdraw&size=50")"
[ "$(jq --arg t "withdrawal:$W2" '[.items[] | select(.target==$t and .result=="ok")] | length' <<<"$AL")" = "2" ] || fail "withdraw audit: $AL"
jq -e '.items[0] | .ip and .requestId and .level=="high" and (.roles|length>0)' <<<"$AL" >/dev/null || fail "audit fields: $(jq .items[0] <<<"$AL")"
[ "$(A "$SA" GET '/admin/v1/audit-logs?module=fee&action=apply_change' | jq -r '.items[0].admin')" = "system" ] || fail "scheduler apply not audited"
[ "$(A "$SA" GET '/admin/v1/audit-logs?module=auth&action=login&result=failed' | jq '.total')" -ge 3 ] || fail "failed logins not audited"
[ "$(A "$SA" GET '/admin/v1/audit-logs?module=user&action=reveal_pii' | jq '.total')" -ge 1 ] || fail "PII reveal not audited"
if command -v psql >/dev/null 2>&1 && [ -n "${DATABASE_URL:-}" ]; then
  if psql "$DATABASE_URL" -qtAc "DELETE FROM audit_logs WHERE id = (SELECT min(id) FROM audit_logs)" >/dev/null 2>&1; then
    fail "audit_logs DELETE allowed"
  fi
  ok "DELETE on audit_logs rejected by trigger"
fi
ok "withdraw / fee / login / PII-reveal actions audited with ip, request id, roles, level"

printf '\n\033[1;32mSMOKE-ADMIN PASSED\033[0m — auth/RBAC -> users -> KYC -> withdrawals -> ledger -> symbols -> fees -> coupons -> rewards -> audit\n'
