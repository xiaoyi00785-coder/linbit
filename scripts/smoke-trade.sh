#!/usr/bin/env bash
#
# End-to-end spot-trade smoke test for the Linbit matching chain:
#
#   two users register + login -> internal credit (deposit) -> a limit sell and a
#   crossing limit buy match in the engine -> settlement moves funds with fees ->
#   both users' balances and the platform fee account are checked to the exact
#   decimal through the gateway / asset service.
#
# Two ways to run it (mirrors scripts/smoke.sh):
#
#   1. No stack running (default, no Docker): boots the devstack (embedded
#      postgres + miniredis + matching-engine + all services), re-runs itself
#      inside it, tears everything down afterwards.
#
#        bash scripts/smoke-trade.sh
#
#   2. Against a running stack: export GATEWAY_URL, ASSET_URL, INTERNAL_TOKEN.
#
#        GATEWAY_URL=... ASSET_URL=... INTERNAL_TOKEN=... SMOKE_INNER=1 \
#            bash scripts/smoke-trade.sh
#
set -euo pipefail
cd "$(dirname "$0")/.."

if [ "${SMOKE_INNER:-}" != "1" ]; then
  echo "== booting devstack (postgres + miniredis + engine + services) =="
  ROOT="$(pwd)"
  cd tools/devstack
  exec go run . -root "$ROOT" -exec "SMOKE_INNER=1 bash scripts/smoke-trade.sh"
fi

GATEWAY_URL="${GATEWAY_URL:-http://localhost:8080}"
ASSET_URL="${ASSET_URL:-http://localhost:8082}"
INTERNAL_TOKEN="${INTERNAL_TOKEN:-}"
PASSWORD="Str0ng-Passw0rd!"
SYMBOL="BTCUSDT"

say()  { printf '\n\033[1;34m== %s ==\033[0m\n' "$1"; }
fail() { printf '\033[1;31mSMOKE-TRADE FAILED: %s\033[0m\n' "$1" >&2; exit 1; }

command -v jq >/dev/null 2>&1 || fail "jq is required for smoke-trade.sh"

# jwt_sub <access-token> — decode the JWT payload and print its "sub" claim.
jwt_sub() {
  local payload
  payload="$(printf '%s' "$1" | cut -d. -f2)"
  # base64url -> JSON (pad to a multiple of 4), then pull .sub with jq.
  python3 - "$payload" <<'PY'
import base64, json, sys
p = sys.argv[1]
p += "=" * (-len(p) % 4)
print(json.loads(base64.urlsafe_b64decode(p)).get("sub", ""))
PY
}

# register <email> -> echoes "accessToken userId"
register() {
  local email="$1" resp token sub
  resp="$(curl -sS -X POST "$GATEWAY_URL/auth/register" \
    -H 'Content-Type: application/json' \
    -d "{\"email\":\"$email\",\"password\":\"$PASSWORD\"}")"
  token="$(printf '%s' "$resp" | jq -r '.accessToken // empty')"
  [ -n "$token" ] || fail "register $email did not return accessToken ($resp)"
  sub="$(jwt_sub "$token")"
  [ -n "$sub" ] || fail "could not extract userId from token for $email"
  printf '%s %s\n' "$token" "$sub"
}

# credit <userId> <asset> <amount> — seed a balance via asset internal API.
credit() {
  local code
  code="$(curl -sS -o /dev/null -w '%{http_code}' -X POST "$ASSET_URL/internal/credit" \
    -H 'Content-Type: application/json' \
    -H "X-Internal-Token: $INTERNAL_TOKEN" \
    -d "{\"userId\":\"$1\",\"asset\":\"$2\",\"amount\":\"$3\",\"ref\":\"smoke-deposit\"}")"
  [ "$code" = "200" ] || fail "credit $1 $3 $2 returned $code"
}

# place <token> <json> — place an order through the gateway; echoes the response.
place() {
  curl -sS -X POST "$GATEWAY_URL/spot/orders" \
    -H 'Content-Type: application/json' \
    -H "Authorization: Bearer $1" \
    -d "$2"
}

# free_balance <token> <asset> — user's free balance for <asset> via the gateway.
free_balance() {
  curl -sS "$GATEWAY_URL/assets/balances?pageSize=100" -H "Authorization: Bearer $1" \
    | jq -r --arg a "$2" '.items[] | select(.asset==$a) | .free'
}

# fee_balance <userId> <asset> — platform fee account balance via asset internal.
fee_balance() {
  curl -sS "$ASSET_URL/internal/balances?userId=$1" -H "X-Internal-Token: $INTERNAL_TOKEN" \
    | jq -r --arg a "$2" '.items[] | select(.asset==$a) | .free'
}

# expect <label> <got> <want> — exact (string) comparison of fixed-8 amounts.
expect() {
  if [ "$2" != "$3" ]; then
    fail "$1: got '$2', want '$3'"
  fi
  printf '  ok  %-28s = %s\n' "$1" "$2"
}

[ -n "$INTERNAL_TOKEN" ] || fail "INTERNAL_TOKEN not set (needed for internal credit)"

TS="$(date +%s)"
say "1/6 register two users"
read -r SELLER_TOKEN SELLER_ID < <(register "sell+$TS@example.com")
read -r BUYER_TOKEN BUYER_ID  < <(register "buy+$TS@example.com")
echo "seller=$SELLER_ID buyer=$BUYER_ID"

say "2/6 deposit (internal credit): seller 0.01 BTC, buyer 1000 USDT"
credit "$SELLER_ID" BTC 0.01
credit "$BUYER_ID"  USDT 1000

say "3/6 seller places limit sell 0.01 @ 70000 (rests as maker)"
SELL_RESP="$(place "$SELLER_TOKEN" \
  "{\"symbol\":\"$SYMBOL\",\"side\":\"sell\",\"type\":\"limit\",\"price\":\"70000\",\"quantity\":\"0.01\",\"timeInForce\":\"GTC\"}")"
echo "$SELL_RESP"
[ "$(printf '%s' "$SELL_RESP" | jq -r '.status')" = "new" ] || fail "sell order not resting (new)"

say "4/6 buyer places crossing limit buy 0.01 @ 70000 (fills as taker)"
BUY_RESP="$(place "$BUYER_TOKEN" \
  "{\"symbol\":\"$SYMBOL\",\"side\":\"buy\",\"type\":\"limit\",\"price\":\"70000\",\"quantity\":\"0.01\",\"timeInForce\":\"GTC\"}")"
echo "$BUY_RESP"
[ "$(printf '%s' "$BUY_RESP" | jq -r '.status')" = "filled" ] || fail "buy order not filled"

say "5/6 verify balances (exact decimal)"
# Trade: 0.01 BTC @ 70000 = 700 USDT. Both fees 0.1%.
#   buyer (taker) fee: 0.01 * 0.001 = 0.00001 BTC
#   seller (maker) fee: 700 * 0.001 = 0.7 USDT
# buyer : USDT 1000-700 = 300 ; BTC 0.01-0.00001 = 0.00999
# seller: USDT 700-0.7 = 699.3 ; BTC 0
# fees  : USDT 0.7 ; BTC 0.00001
expect "buyer USDT free"   "$(free_balance "$BUYER_TOKEN"  USDT)" "300.00000000"
expect "buyer BTC free"    "$(free_balance "$BUYER_TOKEN"  BTC)"  "0.00999000"
expect "seller USDT free"  "$(free_balance "$SELLER_TOKEN" USDT)" "699.30000000"
expect "seller BTC free"   "$(free_balance "$SELLER_TOKEN" BTC)"  "0.00000000"
expect "fee account USDT"  "$(fee_balance  platform USDT)"        "0.70000000"
expect "fee account BTC"   "$(fee_balance  platform BTC)"         "0.00001000"

say "6/6 value conservation"
# BTC: buyer + seller + fees == 0.01 ; USDT: == 1000 (seller deposited BTC only,
# buyer deposited USDT only, so totals are checked per the single trade).
echo "  BTC: 0.00999 + 0 + 0.00001 = 0.01   OK"
echo "  USDT: 300 + 699.3 + 0.7   = 1000    OK"

printf '\n\033[1;32mSMOKE-TRADE PASSED\033[0m — register -> deposit -> match -> settle with exact fees\n'
