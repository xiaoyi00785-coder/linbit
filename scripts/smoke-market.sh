#!/usr/bin/env bash
#
# End-to-end market-data smoke test for the Linbit stack:
#
#   two users register + login -> internal credit (deposit) -> a resting limit
#   sell and a crossing limit buy match in the engine -> the engine streams trade
#   and depth events to the market-data service -> a WebSocket client (tools/wsprobe)
#   receives the live ticker / depth / kline / trades pushes -> the REST endpoints
#   (ticker, klines, depth, recent trades) return the expected values through the
#   gateway's public /market/* routes.
#
# Two ways to run it (mirrors scripts/smoke-trade.sh):
#
#   1. No stack running (default, no Docker): boots the devstack (embedded
#      postgres + miniredis + matching-engine + all services incl. market-data),
#      re-runs itself inside it, tears everything down afterwards.
#
#        bash scripts/smoke-market.sh
#
#   2. Against a running stack: export GATEWAY_URL, ASSET_URL, MARKET_URL,
#      INTERNAL_TOKEN and set SMOKE_INNER=1.
#
set -euo pipefail
cd "$(dirname "$0")/.."

if [ "${SMOKE_INNER:-}" != "1" ]; then
  echo "== booting devstack (postgres + miniredis + engine + services + market-data) =="
  ROOT="$(pwd)"
  cd tools/devstack
  exec go run . -root "$ROOT" -exec "SMOKE_INNER=1 bash scripts/smoke-market.sh"
fi

GATEWAY_URL="${GATEWAY_URL:-http://localhost:8080}"
ASSET_URL="${ASSET_URL:-http://localhost:8082}"
INTERNAL_TOKEN="${INTERNAL_TOKEN:-}"
PASSWORD="Str0ng-Passw0rd!"
SYMBOL="BTCUSDT"
# Derive the WebSocket URL from the gateway's HTTP URL (public /ws route).
WS_URL="ws://$(printf '%s' "$GATEWAY_URL" | sed -E 's#^https?://##')/ws"

say()  { printf '\n\033[1;34m== %s ==\033[0m\n' "$1"; }
fail() { printf '\033[1;31mSMOKE-MARKET FAILED: %s\033[0m\n' "$1" >&2; exit 1; }

command -v jq >/dev/null 2>&1 || fail "jq is required for smoke-market.sh"
[ -n "$INTERNAL_TOKEN" ] || fail "INTERNAL_TOKEN not set (needed for internal credit)"

# jwt_sub <access-token> — decode the JWT payload and print its "sub" claim.
jwt_sub() {
  local payload
  payload="$(printf '%s' "$1" | cut -d. -f2)"
  python3 - "$payload" <<'PY'
import base64, json, sys
p = sys.argv[1]
p += "=" * (-len(p) % 4)
print(json.loads(base64.urlsafe_b64decode(p)).get("sub", ""))
PY
}

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

credit() {
  local code
  code="$(curl -sS -o /dev/null -w '%{http_code}' -X POST "$ASSET_URL/internal/credit" \
    -H 'Content-Type: application/json' \
    -H "X-Internal-Token: $INTERNAL_TOKEN" \
    -d "{\"userId\":\"$1\",\"asset\":\"$2\",\"amount\":\"$3\",\"ref\":\"smoke-market\"}")"
  [ "$code" = "200" ] || fail "credit $1 $3 $2 returned $code"
}

place() {
  curl -sS -X POST "$GATEWAY_URL/spot/orders" \
    -H 'Content-Type: application/json' \
    -H "Authorization: Bearer $1" \
    -d "$2"
}

# expect <label> <got> <want> — exact string comparison.
expect() {
  if [ "$2" != "$3" ]; then
    fail "$1: got '$2', want '$3'"
  fi
  printf '  ok  %-28s = %s\n' "$1" "$2"
}

TS="$(date +%s)"
say "1/7 register two users"
read -r SELLER_TOKEN SELLER_ID < <(register "msell+$TS@example.com")
read -r BUYER_TOKEN BUYER_ID  < <(register "mbuy+$TS@example.com")
echo "seller=$SELLER_ID buyer=$BUYER_ID"

say "2/7 deposit: seller 0.02 BTC, buyer 1000 USDT"
credit "$SELLER_ID" BTC 0.02
credit "$BUYER_ID"  USDT 1000

say "3/7 build & start WebSocket probe (via gateway public /ws)"
PROBE_BIN="$(mktemp -u)"
( cd tools/wsprobe && go build -o "$PROBE_BIN" . ) || fail "build wsprobe"
PROBE_OUT="$(mktemp)"
CHANNELS="ticker:$SYMBOL,depth:$SYMBOL,kline:$SYMBOL:1m,trades:$SYMBOL"
"$PROBE_BIN" -url "$WS_URL" -channels "$CHANNELS" -timeout 25 >"$PROBE_OUT" 2>&1 &
PROBE_PID=$!
echo "probe pid=$PROBE_PID url=$WS_URL channels=$CHANNELS"
# Give the probe time to connect and subscribe (so it receives the live pushes).
sleep 2

say "4/7 seller rests limit sell 0.02 @ 70000 (maker; creates ask depth)"
SELL_RESP="$(place "$SELLER_TOKEN" \
  "{\"symbol\":\"$SYMBOL\",\"side\":\"sell\",\"type\":\"limit\",\"price\":\"70000\",\"quantity\":\"0.02\",\"timeInForce\":\"GTC\"}")"
echo "$SELL_RESP"
[ "$(printf '%s' "$SELL_RESP" | jq -r '.status')" = "new" ] || fail "sell order not resting (new)"

say "5/7 buyer crosses limit buy 0.01 @ 70000 (taker; 0.01 trade, 0.01 ask remains)"
BUY_RESP="$(place "$BUYER_TOKEN" \
  "{\"symbol\":\"$SYMBOL\",\"side\":\"buy\",\"type\":\"limit\",\"price\":\"70000\",\"quantity\":\"0.01\",\"timeInForce\":\"GTC\"}")"
echo "$BUY_RESP"
[ "$(printf '%s' "$BUY_RESP" | jq -r '.status')" = "filled" ] || fail "buy order not filled"

say "6/7 WebSocket client received all live pushes"
if wait "$PROBE_PID"; then
  cat "$PROBE_OUT"
  echo "  ok  websocket received ticker/depth/kline/trades"
else
  cat "$PROBE_OUT"
  fail "WebSocket probe did not receive all required channels"
fi

say "7/7 REST queries via gateway (public /market/*)"
# ticker
TICK="$(curl -sS "$GATEWAY_URL/market/ticker?symbol=$SYMBOL")"
echo "ticker: $TICK"
expect "ticker lastPrice"          "$(printf '%s' "$TICK" | jq -r '.[0].lastPrice')"          "70000"
expect "ticker volume"             "$(printf '%s' "$TICK" | jq -r '.[0].volume')"             "0.01"
expect "ticker quoteVolume"        "$(printf '%s' "$TICK" | jq -r '.[0].quoteVolume')"        "700"
expect "ticker high"               "$(printf '%s' "$TICK" | jq -r '.[0].high')"               "70000"
expect "ticker low"                "$(printf '%s' "$TICK" | jq -r '.[0].low')"                "70000"
expect "ticker priceChangePercent" "$(printf '%s' "$TICK" | jq -r '.[0].priceChangePercent')" "0"

# klines (1m)
KL="$(curl -sS "$GATEWAY_URL/market/klines?symbol=$SYMBOL&interval=1m&limit=10")"
echo "klines: $KL"
expect "kline count>=1" "$(printf '%s' "$KL" | jq 'length>=1')" "true"
expect "kline close"    "$(printf '%s' "$KL" | jq -r '.[-1].close')"  "70000"
expect "kline volume"   "$(printf '%s' "$KL" | jq -r '.[-1].volume')" "0.01"
expect "kline open"     "$(printf '%s' "$KL" | jq -r '.[-1].open')"   "70000"

# depth (0.01 remains on the ask after the partial fill)
DEPTH="$(curl -sS "$GATEWAY_URL/market/depth?symbol=$SYMBOL&limit=20")"
echo "depth: $DEPTH"
expect "depth best ask price" "$(printf '%s' "$DEPTH" | jq -r '.asks[0][0]')" "70000"
expect "depth best ask qty"   "$(printf '%s' "$DEPTH" | jq -r '.asks[0][1]')" "0.01"
expect "depth bids empty"     "$(printf '%s' "$DEPTH" | jq -r '.bids | length')" "0"

# recent trades
TR="$(curl -sS "$GATEWAY_URL/market/trades?symbol=$SYMBOL&limit=10")"
echo "trades: $TR"
expect "trade price"    "$(printf '%s' "$TR" | jq -r '.[0].price')"    "70000"
expect "trade quantity" "$(printf '%s' "$TR" | jq -r '.[0].quantity')" "0.01"
expect "trade side"     "$(printf '%s' "$TR" | jq -r '.[0].side')"     "buy"

rm -f "$PROBE_BIN" "$PROBE_OUT"
printf '\n\033[1;32mSMOKE-MARKET PASSED\033[0m — match -> engine events -> WS pushes + REST ticker/kline/depth/trades\n'
