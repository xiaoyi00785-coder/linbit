#!/usr/bin/env bash
#
# End-to-end KYC smoke test for the Linbit KYC service:
#
#   register -> status none -> upload front/selfie (type + ownership checks) ->
#   submit basic (level 1) -> duplicate submit rejected -> admin reject with a
#   reason -> resubmit -> admin approve -> account kycLevel = 1 -> advanced
#   (level 2) gating -> KYC-tiered withdrawal limit (level 0 blocked, level 1 ok).
#
# Admin review endpoints are called on the KYC service directly with the shared
# internal token (the gateway never proxies /admin/*).
#
#   bash scripts/smoke-kyc.sh            # boots the devstack (no Docker)
#   SMOKE_INNER=1 bash scripts/smoke-kyc.sh   # against a running stack
#
set -euo pipefail
cd "$(dirname "$0")/.."

if [ "${SMOKE_INNER:-}" != "1" ]; then
  echo "== booting devstack (embedded postgres + miniredis + services) =="
  ROOT="$(pwd)"
  cd tools/devstack
  exec go run . -root "$ROOT" -exec "SMOKE_INNER=1 bash scripts/smoke-kyc.sh"
fi

GATEWAY_URL="${GATEWAY_URL:-http://localhost:8080}"
ACCOUNT_URL="${ACCOUNT_URL:-http://localhost:8081}"
WALLET_URL="${WALLET_URL:-http://localhost:8086}"
KYC_URL="${KYC_URL:-http://localhost:8087}"
INTERNAL_TOKEN="${INTERNAL_TOKEN:-}"
PASSWORD="Str0ng-Passw0rd!"

say()  { printf '\n\033[1;34m== %s ==\033[0m\n' "$1"; }
fail() { printf '\033[1;31mSMOKE-KYC FAILED: %s\033[0m\n' "$1" >&2; exit 1; }
ok()   { printf '  ok  %s\n' "$1"; }

command -v jq >/dev/null 2>&1 || fail "jq is required"
[ -n "$INTERNAL_TOKEN" ] || fail "INTERNAL_TOKEN not set"

TMP="$(mktemp -d)"; trap 'rm -rf "$TMP"' EXIT
# 1x1 PNG and a plain-text "image" for type rejection.
printf '%s' 'iVBORw0KGgoAAAANSUhEUgAAAAEAAAABCAYAAAAfFcSJAAAADUlEQVR42mP8z8BQDwAEhQGAhKmMIQAAAABJRU5ErkJggg==' | base64 -d > "$TMP/a.png"
printf 'not an image' > "$TMP/bad.png"

last_code() {
  curl -sS -G "$ACCOUNT_URL/internal/dev/last-code" --data-urlencode "target=$1" \
    --data-urlencode "purpose=$2" -H "X-Internal-Token: $INTERNAL_TOKEN" | jq -r '.code // empty'
}
api() { # api <method> <path> <token> <json|''>
  if [ -n "${4:-}" ]; then
    curl -sS -X "$1" "$GATEWAY_URL$2" -H 'Content-Type: application/json' -H "Authorization: Bearer $3" -d "$4"
  else
    curl -sS -X "$1" "$GATEWAY_URL$2" -H "Authorization: Bearer $3"
  fi
}
admin() { # admin <method> <path> <json|''>
  if [ -n "${3:-}" ]; then
    curl -sS -X "$1" "$KYC_URL$2" -H 'Content-Type: application/json' -H "X-Internal-Token: $INTERNAL_TOKEN" -d "$3"
  else
    curl -sS -X "$1" "$KYC_URL$2" -H "X-Internal-Token: $INTERNAL_TOKEN"
  fi
}
winternal() {
  curl -sS -X "$1" "$WALLET_URL$2" -H 'Content-Type: application/json' -H "X-Internal-Token: $INTERNAL_TOKEN" -d "${3:-}"
}
upload() { # upload <token> <file> <side> -> body; sets CODE
  curl -sS -w '\n%{http_code}' -X POST "$GATEWAY_URL/kyc/documents" -H "Authorization: Bearer $1" \
    -F "file=@$2" -F "side=$3"
}
register() { # register <email> -> token
  curl -sS -o /dev/null -X POST "$GATEWAY_URL/auth/code/send" -H 'Content-Type: application/json' \
    -d "{\"purpose\":\"register\",\"email\":\"$1\"}"
  local c; c="$(last_code "$1" register)"
  curl -sS -X POST "$GATEWAY_URL/auth/register" -H 'Content-Type: application/json' \
    -d "{\"email\":\"$1\",\"password\":\"$PASSWORD\",\"code\":\"$c\"}" | jq -r '.accessToken // empty'
}
basic_body() { # basic_body <front> <selfie> <idNumber>
  printf '{"level":1,"firstName":"San","lastName":"Zhang","country":"SG","idType":"passport","idNumber":"%s","dateOfBirth":"1990-01-15","documents":{"front":"%s","selfie":"%s"}}' "$3" "$1" "$2"
}

TS="$(date +%s)"
EMAIL="kyc${TS}@example.com"
IDNO="E${TS}"

say "1/9 register; status is none"
TOKEN="$(register "$EMAIL")"; [ -n "$TOKEN" ] || fail "register failed"
ST="$(api GET /kyc/status "$TOKEN")"
[ "$(jq -r .status <<<"$ST")" = "none" ] && [ "$(jq -r .level <<<"$ST")" = "0" ] || fail "initial status: $ST"
ok "status none, level 0"

say "2/9 document upload: type sniffing + ownership"
R="$(upload "$TOKEN" "$TMP/bad.png" front)"; [ "$(tail -n1 <<<"$R")" = "415" ] || fail "non-image not rejected: $R"
ok "non-image rejected (415)"
R="$(upload "$TOKEN" "$TMP/a.png" sideways)"; [ "$(tail -n1 <<<"$R")" = "400" ] || fail "bad side accepted: $R"
ok "invalid side rejected (400)"
FRONT="$(upload "$TOKEN" "$TMP/a.png" front | head -n1 | jq -r '.fileId // empty')"
SELFIE="$(upload "$TOKEN" "$TMP/a.png" selfie | head -n1 | jq -r '.fileId // empty')"
[ -n "$FRONT" ] && [ -n "$SELFIE" ] || fail "upload did not return fileIds"
ok "uploaded front=$FRONT selfie=$SELFIE"
OTHER="$(register "kyco${TS}@example.com")"
R="$(api POST /kyc "$OTHER" "$(basic_body "$FRONT" "$SELFIE" "X${TS}")")"
[ "$(jq -r .code <<<"$R")" = "DOCUMENT_NOT_FOUND" ] || fail "foreign document accepted: $R"
ok "another user cannot reference these documents"
R="$(api POST /kyc "$TOKEN" "$(basic_body "$SELFIE" "$FRONT" "$IDNO")")"
[ "$(jq -r .code <<<"$R")" = "DOCUMENT_SIDE_MISMATCH" ] || fail "side mismatch accepted: $R"
ok "side mismatch rejected"

say "3/9 submit basic; advanced blocked; duplicate rejected"
adv_body() { printf '{"level":2,"firstName":"San","lastName":"Zhang","country":"SG","idType":"passport","idNumber":"%s","dateOfBirth":"1990-01-15","documents":{"front":"%s","selfie":"%s","proofOfAddress":"%s"}}' "$IDNO" "$FRONT" "$SELFIE" "$1"; }
R="$(api POST /kyc "$TOKEN" "$(adv_body "")")"
[ "$(jq -r .code <<<"$R")" = "DOCUMENT_REQUIRED" ] || fail "advanced without proof_of_address accepted: $R"
R="$(api POST /kyc "$TOKEN" "$(adv_body "$FRONT")")"
[ "$(jq -r .code <<<"$R")" != "null" ] && [ -z "$(jq -r '.id // empty' <<<"$R")" ] || fail "front doc accepted as proof_of_address: $R"
POA="$(upload "$TOKEN" "$TMP/a.png" proof_of_address | head -n1 | jq -r .fileId)"
[ -n "$POA" ] && [ "$POA" != "null" ] || fail "proof_of_address upload failed"
ok "advanced requires a proof_of_address document"
R="$(api POST /kyc "$TOKEN" "$(adv_body "$POA")")"
[ "$(jq -r .code <<<"$R")" = "KYC_LEVEL_REQUIRED" ] || fail "advanced allowed before basic: $R"
ok "advanced requires basic first"
R="$(api POST /kyc "$TOKEN" "$(printf '{"level":1,"firstName":"San","lastName":"Zhang","country":"SG","idType":"passport","idNumber":"%s","dateOfBirth":"2015-01-15","documents":{"front":"%s","selfie":"%s"}}' "$IDNO" "$FRONT" "$SELFIE")")"
[ "$(jq -r .code <<<"$R")" = "UNDERAGE" ] || fail "underage accepted: $R"
ok "underage rejected"
R="$(api POST /kyc "$TOKEN" "$(basic_body "$FRONT" "$SELFIE" "$IDNO")")"
[ "$(jq -r .status <<<"$R")" = "pending" ] || fail "submit failed: $R"
ok "basic submitted (pending)"
R="$(api POST /kyc "$TOKEN" "$(basic_body "$FRONT" "$SELFIE" "$IDNO")")"
[ "$(jq -r .code <<<"$R")" = "KYC_PENDING" ] || fail "duplicate pending accepted: $R"
ok "duplicate submit rejected (KYC_PENDING)"

say "4/9 admin list/detail; reject requires a reason"
[ "$(curl -sS -o /dev/null -w '%{http_code}' "$KYC_URL/admin/kyc/applications")" = "401" ] || fail "admin open without token"
[ "$(curl -sS -o /dev/null -w '%{http_code}' "$GATEWAY_URL/admin/kyc/applications")" != "200" ] || fail "gateway proxies /admin"
ok "admin endpoints require internal token and are not exposed via gateway"
LIST="$(admin GET '/admin/kyc/applications?status=pending')"
APP="$(jq -r --arg u "$IDNO" '.items[] | select(.idNumber==$u) | .id' <<<"$LIST" | head -n1)"
[ -n "$APP" ] || fail "pending application not listed: $LIST"
DET="$(admin GET "/admin/kyc/applications/$APP")"
DOCURL="$(jq -r '.documents[0].url // empty' <<<"$DET")"; [ -n "$DOCURL" ] || fail "no documents in detail: $DET"
cmp -s <(curl -sS "$KYC_URL$DOCURL" -H "X-Internal-Token: $INTERNAL_TOKEN") "$TMP/a.png" || fail "document read-back mismatch"
ok "detail + controlled document read ($APP)"
R="$(admin POST "/admin/kyc/applications/$APP/reject" '{"reason":""}')"
[ "$(jq -r .code <<<"$R")" = "VALIDATION_ERROR" ] || fail "reject without reason accepted: $R"
R="$(admin POST "/admin/kyc/applications/$APP/reject" '{"reason":"证件照片模糊"}')"
[ "$(jq -r .status <<<"$R")" = "rejected" ] || fail "reject failed: $R"
ST="$(api GET /kyc/status "$TOKEN")"
[ "$(jq -r .status <<<"$ST")" = "rejected" ] && [ "$(jq -r .reason <<<"$ST")" = "证件照片模糊" ] || fail "status after reject: $ST"
ok "rejected with reason visible to user"

say "5/9 KYC gate: level 0 cannot withdraw"
DEP="$(api GET "/wallet/deposit/address?asset=USDT&network=TRC20" "$TOKEN" | jq -r '.address // empty')"
[ -n "$DEP" ] || fail "no deposit address"
winternal POST /internal/dev/chain/deposit "{\"network\":\"TRC20\",\"address\":\"$DEP\",\"asset\":\"USDT\",\"amount\":\"3000\",\"txHash\":\"0xkyc$TS\",\"logIndex\":0}" >/dev/null
winternal POST /internal/dev/chain/advance '{"network":"TRC20","blocks":25}' >/dev/null
winternal POST /internal/scan '' >/dev/null
withdraw() {
  curl -sS -o /dev/null -X POST "$GATEWAY_URL/account/code/send" -H 'Content-Type: application/json' \
    -H "Authorization: Bearer $TOKEN" -d '{"purpose":"withdraw"}'
  local c; c="$(last_code "$EMAIL" withdraw)"
  api POST /wallet/withdraw "$TOKEN" "{\"asset\":\"USDT\",\"network\":\"TRC20\",\"address\":\"TKycDest0001\",\"amount\":\"$1\",\"code\":\"$c\"}"
}
R="$(withdraw 1500)"
[ "$(jq -r .code <<<"$R")" = "KYC_REQUIRED" ] || fail "level-0 withdrawal not blocked: $R"
R="$(withdraw 20)"
[ "$(jq -r .code <<<"$R")" = "KYC_REQUIRED" ] || fail "level-0 small withdrawal not blocked: $R"
ok "level 0 withdrawals rejected (KYC_REQUIRED)"

say "6/9 resubmit and approve; account kycLevel = 1"
R="$(api POST /kyc "$TOKEN" "$(basic_body "$FRONT" "$SELFIE" "$IDNO")")"
[ "$(jq -r .status <<<"$R")" = "pending" ] || fail "resubmit failed: $R"
APP="$(admin GET '/admin/kyc/applications?status=pending' | jq -r --arg u "$IDNO" '.items[] | select(.idNumber==$u) | .id' | head -n1)"
R="$(admin POST "/admin/kyc/applications/$APP/approve" '')"
[ "$(jq -r .status <<<"$R")" = "approved" ] || fail "approve failed: $R"
R="$(admin POST "/admin/kyc/applications/$APP/approve" '')"
[ "$(jq -r .code <<<"$R")" = "NOT_PENDING" ] || fail "double approve accepted: $R"
[ "$(api GET /kyc/status "$TOKEN" | jq -r .status)" = "approved" ] || fail "status not approved"
[ "$(api GET /account "$TOKEN" | jq -r .kycLevel)" = "1" ] || fail "account kycLevel not 1"
[ "$(admin GET /admin/kyc/level-sync | jq '.items | length')" = "0" ] || fail "level sync still pending"
ok "approved; account kycLevel = 1; sync outbox drained"

say "7/9 same ID cannot be approved for another user"
F2="$(upload "$OTHER" "$TMP/a.png" front | head -n1 | jq -r .fileId)"
S2="$(upload "$OTHER" "$TMP/a.png" selfie | head -n1 | jq -r .fileId)"
R="$(api POST /kyc "$OTHER" "$(basic_body "$F2" "$S2" "$IDNO")")"
[ "$(jq -r .code <<<"$R")" = "ID_NUMBER_TAKEN" ] || fail "reused ID accepted: $R"
ok "ID_NUMBER_TAKEN"

say "8/9 level 1 lifts the limit"
R="$(withdraw 1500)"
[ -n "$(jq -r '.id // empty' <<<"$R")" ] || fail "level-1 withdrawal not created: $R"
ok "1500 USDT withdrawal accepted at level 1 ($(jq -r .status <<<"$R"))"

say "9/9 advanced (level 2) submit + approve"
adv_body() { printf '{"level":2,"firstName":"San","lastName":"Zhang","country":"SG","idType":"passport","idNumber":"%s","dateOfBirth":"1990-01-15","documents":{"front":"%s","selfie":"%s","proofOfAddress":"%s"}}' "$IDNO" "$FRONT" "$SELFIE" "$1"; }
R="$(api POST /kyc "$TOKEN" "$(adv_body "")")"
[ "$(jq -r .code <<<"$R")" = "DOCUMENT_REQUIRED" ] || fail "advanced without proof_of_address accepted: $R"
R="$(api POST /kyc "$TOKEN" "$(adv_body "$FRONT")")"
[ "$(jq -r .code <<<"$R")" != "null" ] && [ -z "$(jq -r '.id // empty' <<<"$R")" ] || fail "front doc accepted as proof_of_address: $R"
POA="$(upload "$TOKEN" "$TMP/a.png" proof_of_address | head -n1 | jq -r .fileId)"
[ -n "$POA" ] && [ "$POA" != "null" ] || fail "proof_of_address upload failed"
ok "advanced requires a proof_of_address document"
R="$(api POST /kyc "$TOKEN" "$(adv_body "$POA")")"
[ "$(jq -r .status <<<"$R")" = "pending" ] && [ "$(jq -r .level <<<"$R")" = "2" ] || fail "advanced submit: $R"
APP="$(admin GET '/admin/kyc/applications?status=pending' | jq -r --arg u "$IDNO" '.items[] | select(.idNumber==$u) | .id' | head -n1)"
[ "$(admin POST "/admin/kyc/applications/$APP/approve" '' | jq -r .status)" = "approved" ] || fail "advanced approve failed"
[ "$(api GET /account "$TOKEN" | jq -r .kycLevel)" = "2" ] || fail "account kycLevel not 2"
[ "$(admin GET "/admin/kyc/applications/$APP" | jq -r '[.documents[] | select(.side=="proof_of_address")] | length')" = "1" ] || fail "proof_of_address missing in admin detail"
ok "advanced approved; kycLevel = 2"

printf '\n\033[1;32mSMOKE-KYC PASSED\033[0m — upload -> submit -> reject/resubmit -> approve -> level sync -> limits\n'
