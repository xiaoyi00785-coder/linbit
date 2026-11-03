#!/usr/bin/env bash
#
# End-to-end smoke test for the Linbit spot chain:
#   register -> login -> query balances (all through the gateway).
#
# Two ways to run it:
#
#   1. No stack running (default, no Docker needed): this script boots the local
#      devstack (embedded postgres + miniredis + the 3 services), re-runs itself
#      inside it, and tears everything down afterwards.
#
#        bash scripts/smoke.sh
#
#   2. Against an already-running stack: export GATEWAY_URL and run it.
#
#        GATEWAY_URL=http://localhost:8080 SMOKE_INNER=1 bash scripts/smoke.sh
#
set -euo pipefail

cd "$(dirname "$0")/.."

if [ "${SMOKE_INNER:-}" != "1" ]; then
  echo "== booting devstack (embedded postgres + miniredis + services) =="
  ROOT="$(pwd)"
  # devstack is its own Go module; run it from inside that module and point it
  # back at the repo root. It boots the stack, re-runs this script inside it
  # (SMOKE_INNER=1, cwd = repo root), then tears everything down.
  cd tools/devstack
  exec go run . -root "$ROOT" -exec "SMOKE_INNER=1 bash scripts/smoke.sh"
fi

GATEWAY_URL="${GATEWAY_URL:-http://localhost:8080}"
EMAIL="smoke+$(date +%s)@example.com"
PASSWORD="Str0ng-Passw0rd!"

say()  { printf '\n\033[1;34m== %s ==\033[0m\n' "$1"; }
fail() { printf '\033[1;31mSMOKE FAILED: %s\033[0m\n' "$1" >&2; exit 1; }

command -v jq >/dev/null 2>&1 && HAVE_JQ=1 || HAVE_JQ=0

# json_get <json> <key> — extract a top-level string value from a JSON object.
json_get() {
  if [ "$HAVE_JQ" = "1" ]; then
    printf '%s' "$1" | jq -r ".$2 // empty"
  else
    printf '%s' "$1" | sed -n "s/.*\"$2\"[[:space:]]*:[[:space:]]*\"\([^\"]*\)\".*/\1/p"
  fi
}

say "1/3 register $EMAIL"
REG=$(curl -sS -X POST "$GATEWAY_URL/auth/register" \
  -H 'Content-Type: application/json' \
  -d "{\"email\":\"$EMAIL\",\"password\":\"$PASSWORD\"}")
echo "$REG"
ACCESS=$(json_get "$REG" accessToken)
[ -n "$ACCESS" ] || fail "register did not return accessToken"

say "2/3 login"
LOGIN=$(curl -sS -X POST "$GATEWAY_URL/auth/login" \
  -H 'Content-Type: application/json' \
  -d "{\"email\":\"$EMAIL\",\"password\":\"$PASSWORD\"}")
echo "$LOGIN"
ACCESS=$(json_get "$LOGIN" accessToken)
[ -n "$ACCESS" ] || fail "login did not return accessToken"

say "2.5 reject balances without token (gateway auth)"
CODE=$(curl -sS -o /dev/null -w '%{http_code}' "$GATEWAY_URL/assets/balances")
[ "$CODE" = "401" ] || fail "unauthenticated balances returned $CODE, want 401"
echo "unauthenticated request correctly rejected ($CODE)"

say "3/3 query balances with token"
BAL=$(curl -sS "$GATEWAY_URL/assets/balances" -H "Authorization: Bearer $ACCESS")
echo "$BAL"
case "$BAL" in
  *'"items"'*'"USDT"'*) : ;;
  *) fail "balances did not contain seeded assets" ;;
esac

printf '\n\033[1;32mSMOKE PASSED\033[0m — register -> login -> balances OK via gateway\n'
