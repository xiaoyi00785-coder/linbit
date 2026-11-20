#!/usr/bin/env bash
#
# Smoke for the IM service (services/go/im):
#   register two users -> POST /im/token (returns Linbit JWT as usersig) ->
#   send 1v1 text -> history -> group create -> group text -> recall -> red
#   packet create + 2 claims -> conversation list unread counter.
#
#   bash scripts/smoke-im.sh      # boots devstack (honours DEVSTACK_*_PORT)
#
set -euo pipefail
cd "$(dirname "$0")/.."

if [ "${SMOKE_INNER:-}" != "1" ]; then
  ROOT="$(pwd)"; cd tools/devstack
  exec go run . -root "$ROOT" -exec "SMOKE_INNER=1 bash scripts/smoke-im.sh"
fi

: "${GATEWAY_URL:?}" "${INTERNAL_TOKEN:?}"
PASSWORD="Str0ng-Passw0rd!"
TS="$(date +%s%N)"

say()  { printf '\n\033[1;34m== %s ==\033[0m\n' "$1"; }
fail() { printf '\033[1;31mSMOKE-IM FAILED: %s\033[0m\n' "$1" >&2; exit 1; }

command -v jq >/dev/null 2>&1 || fail "jq required"

# api <token|-> <METHOD> <path> [json] -> body (status via separate check)
api_body() {
  local tok="$1" method="$2" path="$3" body="${4:-}"
  local hdr=()
  [ "$tok" != "-" ] && hdr+=(-H "Authorization: Bearer $tok")
  [ -n "$body" ] && hdr+=(-H "Content-Type: application/json" -d "$body")
  curl -sS -X "$method" "$GATEWAY_URL$path" "${hdr[@]}"
}
api_code() {
  local tok="$1" method="$2" path="$3" body="${4:-}"
  local hdr=()
  [ "$tok" != "-" ] && hdr+=(-H "Authorization: Bearer $tok")
  [ -n "$body" ] && hdr+=(-H "Content-Type: application/json" -d "$body")
  curl -sS -o /dev/null -w '%{http_code}' -X "$method" "$GATEWAY_URL$path" "${hdr[@]}"
}

# register-and-login wraps the account service's happy path to keep the smoke terse.
register_login() {
  local email="$1" code body last tokens
  code="$(api_code - POST /auth/code/send "{\"email\":\"$email\",\"purpose\":\"register\"}")"
  [ "$code" = "200" ] || fail "code send $email returned $code"
  last="$(curl -sS -G "$ACCOUNT_URL/internal/dev/last-code" \
      --data-urlencode "target=$email" --data-urlencode "purpose=register" \
      -H "X-Internal-Token: $INTERNAL_TOKEN" | jq -r '.code // empty')"
  [ -n "$last" ] || fail "no register code for $email"
  body="$(api_body - POST /auth/register "{\"email\":\"$email\",\"password\":\"$PASSWORD\",\"code\":\"$last\"}")"
  local tok uid
  tok="$(echo "$body" | jq -r '.accessToken // empty')"
  [ -n "$tok" ] || fail "register for $email: $body"
  # Decode the JWT "sub" claim to recover the uid — /auth/register returns the
  # tokens but not the uid directly.
  uid="$(echo "$tok" | awk -F. '{print $2}' | base64 -d 2>/dev/null | jq -r '.sub')" || true
  [ -n "$uid" ] && [ "$uid" != "null" ] || uid="$(echo "$tok" | awk -F. '{print $2}' | tr '_-' '/+' | awk '{while(length($0)%4)$0=$0"="; print}' | base64 -d 2>/dev/null | jq -r '.sub')"
  [ -n "$uid" ] && [ "$uid" != "null" ] || fail "decode uid from token: $tok"
  echo "$tok $uid"
}

say "register alice + bob"
read ALICE_TOK ALICE_UID < <(register_login "alice-$TS@test.local")
read BOB_TOK   BOB_UID   < <(register_login "bob-$TS@test.local")

say "POST /im/token -> usersig"
BODY="$(api_body "$ALICE_TOK" POST /im/token "{}")"
echo "$BODY" | jq -e '.enabled and .usersig and .userId' >/dev/null || fail "im/token: $BODY"

say "alice -> bob text message"
BODY="$(api_body "$ALICE_TOK" POST "/im/conversations/$BOB_UID/messages" '{"kind":"text","payload":{"text":"hi bob"}}')"
MID="$(echo "$BODY" | jq -r '.messageId // empty')"
[ -n "$MID" ] || fail "send c2c: $BODY"

say "bob reads history"
BODY="$(api_body "$BOB_TOK" GET "/im/conversations/$ALICE_UID/messages" '')"
COUNT="$(echo "$BODY" | jq -r '.MsgList | length')"
[ "$COUNT" -ge 1 ] || fail "want at least 1 message, got $COUNT body=$BODY"

say "alice creates group with bob"
BODY="$(api_body "$ALICE_TOK" POST /im/groups "{\"name\":\"squad-$TS\",\"members\":[\"$BOB_UID\"]}")"
GID="$(echo "$BODY" | jq -r '.GroupId // empty')"
[ -n "$GID" ] || fail "create group: $BODY"

say "bob sends into group, then recalls"
BODY="$(api_body "$BOB_TOK" POST "/im/groups/$GID/messages" '{"kind":"text","payload":{"text":"hello group"}}')"
GMID="$(echo "$BODY" | jq -r '.messageId // empty')"
[ -n "$GMID" ] || fail "group send: $BODY"
CODE="$(api_code "$BOB_TOK" POST "/im/messages/$GMID/recall" '')"
[ "$CODE" = "200" ] || fail "recall: $CODE"

say "alice creates 10 USDT / 2 share red packet (equal)"
# Seed alice's funding account with 100 USDT so the ledger move succeeds.
# /internal/deposit honours the explicit `account` field; /internal/credit
# always goes to spot regardless of what the body says.
curl -sS -X POST "$ASSET_URL/internal/deposit" -H "X-Internal-Token: $INTERNAL_TOKEN" -H 'Content-Type: application/json' \
  -d "{\"userId\":\"$ALICE_UID\",\"asset\":\"USDT\",\"account\":\"funding\",\"amount\":\"100\",\"ref\":\"seed-im-$TS\"}" >/dev/null
BODY="$(api_body "$ALICE_TOK" POST /im/redpackets "{\"groupId\":$GID,\"totalAmount\":\"10.00\",\"shares\":2,\"mode\":\"equal\",\"blessing\":\"bless\"}")"
RP="$(echo "$BODY" | jq -r '.id // empty')"
[ -n "$RP" ] || fail "redpacket: $BODY"

say "both claim"
for tok in "$ALICE_TOK" "$BOB_TOK"; do
  CODE="$(api_code "$tok" POST "/im/redpackets/$RP/claim" '')"
  [ "$CODE" = "200" ] || fail "claim: $CODE"
done

say "double-claim rejected"
CODE="$(api_code "$BOB_TOK" POST "/im/redpackets/$RP/claim" '')"
[ "$CODE" = "400" ] || fail "double claim should 400, got $CODE"

say "conversation list reflects traffic"
BODY="$(api_body "$BOB_TOK" GET /im/conversations '')"
echo "$BODY" | jq -e '.items | length >= 1' >/dev/null || fail "convs: $BODY"

printf '\n\033[1;32mSMOKE-IM OK\033[0m\n'
