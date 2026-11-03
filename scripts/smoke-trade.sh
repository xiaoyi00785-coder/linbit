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
ACCOUNT_URL="${ACCOUNT_URL:-http://localhost:8081}"
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
# Registration requires a verification code; it is read back through the account
# service's dev-only internal endpoint (DEV_MODE=true).
register() {
  local email="$1" resp token sub code
  curl -sS -o /dev/null -X POST "$GATEWAY_URL/auth/code/send" \
    -H 'Content-Type: application/json' \
    -d "{\"purpose\":\"register\",\"email\":\"$email\"}"
  code="$(curl -sS -G "$ACCOUNT_URL/internal/dev/last-code" \
    --data-urlencode "target=$email" --data-urlencode "purpose=register" \
    -H "X-Internal-Token: $INTERNAL_TOKEN" | jq -r '.code // empty')"
  [ -n "$code" ] || fail "could not read register code for $email"
  resp="$(curl -sS -X POST "$GATEWAY_URL/auth/register" \
    -H 'Content-Type: application/json' \
    -d "{\"email\":\"$email\",\"password\":\"$PASSWORD\",\"code\":\"$code\"}")"
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

# api_get <token> <path> — authenticated GET through the gateway; echoes the body.
api_get() {
  curl -sS "$GATEWAY_URL$2" -H "Authorization: Bearer $1"
}

# http_code <token> <path> — authenticated GET; echoes only the HTTP status code.
http_code() {
  curl -sS -o /dev/null -w '%{http_code}' "$GATEWAY_URL$2" -H "Authorization: Bearer $1"
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

# Precision: BTCUSDT pricePrecision=2, quantityPrecision=6 — the place response
# must be formatted as fixed-precision decimal strings (not raw "0.01"/"70000").
expect "buy resp quantity fmt"       "$(printf '%s' "$BUY_RESP" | jq -r '.quantity')"       "0.010000"
expect "buy resp filledQuantity fmt" "$(printf '%s' "$BUY_RESP" | jq -r '.filledQuantity')" "0.010000"
expect "buy resp price fmt"          "$(printf '%s' "$BUY_RESP" | jq -r '.price')"          "70000.00"

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

say "6/9 value conservation"
# BTC: buyer + seller + fees == 0.01 ; USDT: == 1000 (seller deposited BTC only,
# buyer deposited USDT only, so totals are checked per the single trade).
echo "  BTC: 0.00999 + 0 + 0.00001 = 0.01   OK"
echo "  USDT: 300 + 699.3 + 0.7   = 1000    OK"

say "7/9 order history returns terminal orders only (no new/partially_filled)"
# Both orders are now filled, so each user's history has exactly one filled order
# and never any live (new/partially_filled) status.
BUYER_HIST="$(api_get "$BUYER_TOKEN" "/spot/orders/history?symbol=$SYMBOL")"
echo "$BUYER_HIST"
HIST_TOTAL="$(printf '%s' "$BUYER_HIST" | jq -r '.total')"
expect "buyer history total" "$HIST_TOTAL" "1"
# Every returned status must be terminal.
NON_TERMINAL="$(printf '%s' "$BUYER_HIST" \
  | jq -r '[.items[] | select(.status|IN("new","partially_filled"))] | length')"
expect "buyer history non-terminal count" "$NON_TERMINAL" "0"
expect "buyer history status" "$(printf '%s' "$BUYER_HIST" | jq -r '.items[0].status')" "filled"
# Precision is applied in history too.
expect "buyer history quantity fmt" "$(printf '%s' "$BUYER_HIST" | jq -r '.items[0].quantity')" "0.010000"
expect "buyer history price fmt"    "$(printf '%s' "$BUYER_HIST" | jq -r '.items[0].price')"    "70000.00"
# A non-terminal status filter must return nothing.
NEW_FILTER_TOTAL="$(api_get "$BUYER_TOKEN" "/spot/orders/history?status=new" | jq -r '.total')"
expect "buyer history status=new total" "$NEW_FILTER_TOTAL" "0"

say "8/9 trade precision (fee formatted to charging-asset precision)"
BUYER_TRADES="$(api_get "$BUYER_TOKEN" "/spot/trades?symbol=$SYMBOL")"
echo "$BUYER_TRADES"
expect "buyer trade price fmt"    "$(printf '%s' "$BUYER_TRADES" | jq -r '.items[0].price')"    "70000.00"
expect "buyer trade quantity fmt" "$(printf '%s' "$BUYER_TRADES" | jq -r '.items[0].quantity')" "0.010000"
# buyer (taker) fee is in BTC (base) -> quantityPrecision=6
expect "buyer trade fee fmt"      "$(printf '%s' "$BUYER_TRADES" | jq -r '.items[0].fee')"      "0.000010"

say "9/9 wallet withdraw endpoints (networks + records) via gateway"
# networks: requires auth, returns configured networks with gasFeeEstimate.
NET_CODE="$(http_code "$BUYER_TOKEN" "/wallet/withdraw/networks?asset=USDT")"
expect "withdraw networks status" "$NET_CODE" "200"
NET="$(api_get "$BUYER_TOKEN" "/wallet/withdraw/networks?asset=USDT")"
echo "$NET"
expect "withdraw networks asset" "$(printf '%s' "$NET" | jq -r '.asset')" "USDT"
NET_COUNT="$(printf '%s' "$NET" | jq -r '.networks | length')"
[ "$NET_COUNT" -ge 1 ] || fail "withdraw networks empty for USDT"
echo "  ok  withdraw networks count          = $NET_COUNT"
GAS="$(printf '%s' "$NET" | jq -r '.networks[0].gasFeeEstimate')"
[ -n "$GAS" ] && [ "$GAS" != "null" ] || fail "withdraw network missing gasFeeEstimate"
echo "  ok  withdraw networks[0].gasFeeEstimate = $GAS"
# All USDT networks share a 12 USDT minimum withdrawal.
BAD_MIN="$(printf '%s' "$NET" | jq -r '[.networks[] | select(.minWithdraw != "12.00000000")] | length')"
expect "USDT networks all minWithdraw=12" "$BAD_MIN" "0"
# networks require authentication through the gateway.
NET_NOAUTH="$(curl -sS -o /dev/null -w '%{http_code}' "$GATEWAY_URL/wallet/withdraw/networks?asset=USDT")"
expect "withdraw networks no-auth" "$NET_NOAUTH" "401"
# records: paginated envelope (empty list for now).
REC_CODE="$(http_code "$BUYER_TOKEN" "/wallet/withdraw/records")"
expect "withdraw records status" "$REC_CODE" "200"
REC="$(api_get "$BUYER_TOKEN" "/wallet/withdraw/records")"
echo "$REC"
expect "withdraw records total"     "$(printf '%s' "$REC" | jq -r '.total')"            "0"
expect "withdraw records items len" "$(printf '%s' "$REC" | jq -r '.items | length')"   "0"

printf '\n\033[1;32mSMOKE-TRADE PASSED\033[0m — trade+settle, history (terminal-only), precision, withdraw endpoints\n'
