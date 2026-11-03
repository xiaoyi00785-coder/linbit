#!/usr/bin/env bash
#
# End-to-end smoke for the spot grid strategy service: validation, preview,
#   arithmetic + geometric grids, initial market buy, buy->sell->buy cycle with
#   realised grid profit, ledger == spot balances, strategy square, take-profit
#   stop (sells base), manual stop (keep base), external-cancel stop, ownership.
#
#   bash scripts/smoke-grid.sh                       # boots devstack
#   GATEWAY_URL=... ASSET_URL=... ACCOUNT_URL=... STRATEGY_URL=... INTERNAL_TOKEN=... \
#     SMOKE_INNER=1 bash scripts/smoke-grid.sh       # existing stack
#
set -euo pipefail
cd "$(dirname "$0")/.."

if [ "${SMOKE_INNER:-}" != "1" ]; then
  echo "== booting devstack =="
  ROOT="$(pwd)"
  cd tools/devstack
  exec go run . -root "$ROOT" -exec "SMOKE_INNER=1 bash scripts/smoke-grid.sh"
fi

GATEWAY_URL="${GATEWAY_URL:-http://localhost:8080}"
ASSET_URL="${ASSET_URL:-http://localhost:8082}"
ACCOUNT_URL="${ACCOUNT_URL:-http://localhost:8081}"
STRATEGY_URL="${STRATEGY_URL:-http://localhost:8090}"
MARKET_URL="${MARKET_URL:-http://localhost:8085}"
INTERNAL_TOKEN="${INTERNAL_TOKEN:-}"
PASSWORD="Str0ng-Passw0rd!"
TS="$(date +%s%N)"

say()  { printf '\n\033[1;34m== %s ==\033[0m\n' "$1"; }
fail() { printf '\033[1;31mSMOKE-GRID FAILED: %s\033[0m\n' "$1" >&2; exit 1; }
command -v jq >/dev/null 2>&1 || fail "jq is required"
command -v python3 >/dev/null 2>&1 || fail "python3 is required"

# eq <actual> <expected> <label> — decimal compare with 1e-6 tolerance.
eq() {
  python3 -c 'import sys; from decimal import Decimal as D; sys.exit(0 if abs(D(sys.argv[1])-D(sys.argv[2]))<=D("0.000001") else 1)' "$1" "$2" \
    || fail "$3: got $1, want $2"
}
lt() { python3 -c 'import sys; from decimal import Decimal as D; sys.exit(0 if D(sys.argv[1])<D(sys.argv[2]) else 1)' "$1" "$2" || fail "$3: $1 !< $2"; }

jwt_sub() {
  python3 - "$(printf '%s' "$1" | cut -d. -f2)" <<'PY'
import base64, json, sys
p = sys.argv[1]; p += "=" * (-len(p) % 4)
print(json.loads(base64.urlsafe_b64decode(p))["sub"])
PY
}

# register <email> -> "token sub"
register() {
  local email="$1" code resp token
  curl -sS -o /dev/null -X POST "$GATEWAY_URL/auth/code/send" -H 'Content-Type: application/json' \
    -d "{\"purpose\":\"register\",\"email\":\"$email\"}"
  code="$(curl -sS -G "$ACCOUNT_URL/internal/dev/last-code" --data-urlencode "target=$email" \
    --data-urlencode "purpose=register" -H "X-Internal-Token: $INTERNAL_TOKEN" | jq -r '.code // empty')"
  [ -n "$code" ] || fail "no register code for $email"
  resp="$(curl -sS -X POST "$GATEWAY_URL/auth/register" -H 'Content-Type: application/json' \
    -d "{\"email\":\"$email\",\"password\":\"$PASSWORD\",\"code\":\"$code\"}")"
  token="$(jq -r '.accessToken // empty' <<<"$resp")"
  [ -n "$token" ] || fail "register $email: $resp"
  printf '%s %s\n' "$token" "$(jwt_sub "$token")"
}

credit() { # credit <uid> <asset> <amount>
  local c
  c="$(curl -sS -o /dev/null -w '%{http_code}' -X POST "$ASSET_URL/internal/credit" -H 'Content-Type: application/json' \
    -H "X-Internal-Token: $INTERNAL_TOKEN" -d "{\"userId\":\"$1\",\"asset\":\"$2\",\"amount\":\"$3\",\"ref\":\"smoke-grid-$1-$2\"}")"
  [ "$c" = "200" ] || fail "credit $1 $2 returned $c"
}

# api <token> <METHOD> <path> [json] — via gateway; prints body, sets CODE file.
api() {
  curl -sS -w '\n%{http_code}' -X "$2" "$GATEWAY_URL$3" -H 'Content-Type: application/json' \
    -H "Authorization: Bearer $1" ${4:+-d "$4"}
}
body() { sed '$d' <<<"$1"; }
code() { tail -n1 <<<"$1"; }
ok() { # ok <resp> <label> -> body, fails unless 2xx
  local c; c="$(code "$1")"
  case "$c" in 2*) body "$1" ;; *) fail "$2: HTTP $c $(body "$1")" ;; esac
}
errcode() { body "$1" | jq -r '.code // .error.code // empty'; }

tick() { curl -sS -o /dev/null -X POST "$STRATEGY_URL/internal/dev/grid/tick"; }
# total <token> <asset> -> free+locked
total() {
  api "$1" GET "/assets/balances?pageSize=100" | sed '$d' \
    | jq -r --arg a "$2" '[.items[] | select(.asset==$a) | ((.free|tonumber) + ((.locked // "0")|tonumber))] | add // 0'
}
spot() { ok "$(api "$1" POST /spot/orders "$2")" "spot order $2"; }
last() { curl -sS "$MARKET_URL/market/ticker?symbol=BTCUSDT" | jq -r '.lastPrice // empty'; }
wait_last() { # wait_last <price>
  for _ in $(seq 1 50); do
    python3 -c 'import sys; from decimal import Decimal as D; sys.exit(0 if sys.argv[1] and D(sys.argv[1])==D(sys.argv[2]) else 1)' "$(last)" "$1" 2>/dev/null && return 0
    sleep 0.1
  done
  fail "lastPrice never reached $1 (is $(last))"
}
bot() { ok "$(api "$1" GET "/strategy/grid/$2")" "bot $2"; }
wait_bot() { # wait_bot <token> <id> <jq-condition>
  local b
  for _ in $(seq 1 60); do
    tick; b="$(bot "$1" "$2")"
    [ "$(jq -r "$3" <<<"$b")" = "true" ] && { printf '%s' "$b"; return 0; }
    sleep 0.2
  done
  fail "bot $2 never satisfied $3: $b"
}
wait_orders() { # wait_orders <token> <id> <jq-condition on open orders>
  local o
  for _ in $(seq 1 60); do
    tick; o="$(ok "$(api "$1" GET "/strategy/grid/$2/orders?status=open")" orders)"
    [ "$(jq -r "$3" <<<"$o")" = "true" ] && { printf '%s' "$o"; return 0; }
    sleep 0.2
  done
  fail "bot $2 orders never satisfied $3: $o"
}
LAYOUT='[.items[]|"\(.side)@\(.price)"]|sort'
grid() { api "${TOK[G]}" POST /strategy/grid "$1"; }

say "gateway access"
[ "$(curl -sS -o /dev/null -w '%{http_code}' "$GATEWAY_URL/strategy/config")" = "200" ] || fail "config not public"
[ "$(curl -sS -o /dev/null -w '%{http_code}' "$GATEWAY_URL/strategy/grid/square")" = "200" ] || fail "square not public"
[ "$(curl -sS -o /dev/null -w '%{http_code}' "$GATEWAY_URL/strategy/grid")" = "401" ] || fail "grid list open without token"

say "users + a 50000 last price"
declare -A TOK UID_
for u in G M N H; do
  read -r t s < <(register "grid-${u,,}-$TS@example.com")
  TOK[$u]="$t"; UID_[$u]="$s"
  credit "$s" USDT 100000
done
credit "${UID_[M]}" BTC 10
credit "${UID_[N]}" BTC 10
spot "${TOK[M]}" '{"symbol":"BTCUSDT","side":"sell","type":"limit","price":"50000","quantity":"0.001"}' >/dev/null
spot "${TOK[N]}" '{"symbol":"BTCUSDT","side":"buy","type":"limit","price":"50000","quantity":"0.001"}' >/dev/null
wait_last 50000
G_USDT0="$(total "${TOK[G]}" USDT)"

say "validation"
A='"symbol":"BTCUSDT","mode":"arithmetic","lowerPrice":"48000","upperPrice":"52000","gridCount":4,"investment":"1000"'
for c in \
  '{"symbol":"BTCUSDT","lowerPrice":"51000","upperPrice":"52000","gridCount":4,"investment":"1000"}:PRICE_OUT_OF_RANGE' \
  '{"symbol":"BTCUSDT","lowerPrice":"48000","upperPrice":"52000","gridCount":1,"investment":"1000"}:INVALID_GRID_COUNT' \
  '{"symbol":"BTCUSDT","lowerPrice":"48000","upperPrice":"52000","gridCount":4,"investment":"5"}:INVESTMENT_TOO_LOW' \
  '{"symbol":"BTCUSDT","lowerPrice":"48000","upperPrice":"52000","gridCount":4,"investment":"20"}:INVESTMENT_TOO_LOW' \
  '{"symbol":"BTCUSDT","mode":"fib","lowerPrice":"48000","upperPrice":"52000","gridCount":4,"investment":"1000"}:INVALID_MODE' \
  '{"symbol":"BTCUSDT","lowerPrice":"52000","upperPrice":"48000","gridCount":4,"investment":"1000"}:INVALID_RANGE' \
  '{"symbol":"BTCUSDT","lowerPrice":"49990","upperPrice":"50010","gridCount":100,"investment":"1000"}:GRID_TOO_DENSE' \
  "{$A,\"stopLossPrice\":\"50500\"}:INVALID_STOP_LOSS" \
  "{$A,\"takeProfitPrice\":\"49000\"}:INVALID_TAKE_PROFIT" \
  '{"symbol":"NOPEUSDT","lowerPrice":"1","upperPrice":"2","gridCount":4,"investment":"1000"}:SYMBOL_NOT_FOUND' \
  ; do
  r="$(grid "${c%:*}")"
  [ "$(errcode "$r")" = "${c##*:}" ] || fail "want ${c##*:} for ${c%:*}: $r"
done
r="$(grid "{\"symbol\":\"BTCUSDT\",\"lowerPrice\":\"48000\",\"upperPrice\":\"52000\",\"gridCount\":4,\"investment\":\"200000\"}")"
[ "$(errcode "$r")" = "INSUFFICIENT_BALANCE" ] || fail "over balance: $r"
[ "$(jq '.items|length' <<<"$(ok "$(api "${TOK[G]}" GET '/strategy/grid?status=all')" list)")" = "0" ] || fail "failed start left a bot"

say "preview: arithmetic 48000-52000 x4, 1000 USDT"
P="$(ok "$(api "${TOK[G]}" GET "/strategy/grid/preview?symbol=BTCUSDT&mode=arithmetic&lowerPrice=48000&upperPrice=52000&gridCount=4&investment=1000")" preview)"
[ "$(jq -c .levels <<<"$P")" = '["48000","49000","50000","51000","52000"]' ] || fail "levels: $P"
eq "$(jq -r .qtyPerGrid <<<"$P")" 0.005065 "qty per grid"   # 1000/(48000+49000+2*50000*1.004)
[ "$(jq -r '"\(.buyOrders) \(.sellOrders)"' <<<"$P")" = "2 2" ] || fail "order split: $P"
eq "$(jq -r .initialBuyQuote <<<"$P")" 508.526 "initial buy quote"
eq "$(jq -r .profitPerGridMax <<<"$P")" 0.018792 "max per-grid profit"   # 49000/48000*.999^2-1
G1="$(api "${TOK[G]}" GET "/strategy/grid/preview?symbol=BTCUSDT&mode=geometric&lowerPrice=40000&upperPrice=62500&gridCount=2&investment=1000")"
[ "$(ok "$G1" geo | jq -c .levels)" = '["40000","50000","62500"]' ] || fail "geometric levels: $G1"

say "start bot 1 (initial market buy against M's ask)"
spot "${TOK[M]}" '{"symbol":"BTCUSDT","side":"sell","type":"limit","price":"50000","quantity":"0.02"}' >/dev/null
B="$(ok "$(grid "{$A}")" "create bot1")"
B1="$(jq -r .id <<<"$B")"
[ "$(jq -r .status <<<"$B")" = "running" ] || fail "bot1 status: $B"
[ "$(jq -r .openOrders <<<"$B")" = "4" ] || fail "bot1 open orders: $B"
lt 0.0101 "$(jq -r .baseHeld <<<"$B")" "bot1 bought base"
OO="$(ok "$(api "${TOK[G]}" GET "/strategy/grid/$B1/orders?status=open")" orders)"
[ "$(jq -c '[.items[]|"\(.side)@\(.price)"]|sort' <<<"$OO")" = '["buy@48000","buy@49000","sell@51000","sell@52000"]' ] || fail "layout: $OO"
OPEN="$(ok "$(api "${TOK[G]}" GET /spot/orders/open)" open | jq '[.items[]]|length')"
[ "$OPEN" = "4" ] || fail "spot open orders: $OPEN"

say "cycle: buy@49000 fills -> sell@50000 -> fills -> buy@49000 again"
spot "${TOK[N]}" '{"symbol":"BTCUSDT","side":"sell","type":"limit","price":"49000","quantity":"0.005065"}' >/dev/null
# openOrders is already 4 before the fill is processed; wait for the
# replacement sell@50000 itself rather than racing the fill handler.
OO="$(wait_orders "${TOK[G]}" "$B1" '([.items[]|select(.side=="sell" and .price=="50000")]|length==1) and ((.items|length)==4)')"
B="$(wait_bot "${TOK[G]}" "$B1" '.openOrders==4 and (.levels|length)==5')"
# N lifts M's remaining 50000 ask first (time priority), then the bot's sell.
spot "${TOK[N]}" '{"symbol":"BTCUSDT","side":"buy","type":"limit","price":"50000","quantity":"0.02"}' >/dev/null
B="$(wait_bot "${TOK[G]}" "$B1" '.cycles==1')"
GP="$(jq -r .gridProfit <<<"$B")"
python3 -c 'import sys; from decimal import Decimal as D; sys.exit(0 if D("4.5")<D(sys.argv[1])<D("5.1") else 1)' "$GP" || fail "grid profit $GP"
OO="$(wait_orders "${TOK[G]}" "$B1" "($LAYOUT) == [\"buy@48000\",\"buy@49000\",\"sell@51000\",\"sell@52000\"]")"
B="$(ok "$(api "${TOK[G]}" GET "/strategy/grid/$B1")" "bot1 after cycle")"
jq -e '.totalProfit and .profitRate and .unrealizedPnl and .runtimeSeconds>=0 and .maxDrawdown' <<<"$B" >/dev/null || fail "stats: $B"

say "bot ledger == spot balances (G holds nothing else)"
eq "$(total "${TOK[G]}" BTC)" "$(jq -r .baseHeld <<<"$B")" "BTC held"
eq "$(python3 -c "print($(total "${TOK[G]}" USDT)-$G_USDT0)")" "$(python3 -c "print($(jq -r .quoteBalance <<<"$B")-1000)")" "USDT delta"

say "strategy square + ownership"
SQ="$(curl -sS "$GATEWAY_URL/strategy/grid/square?sort=runtime")"
jq -e --arg id "$B1" '[.items[]|select(.id==$id)]|length==1' <<<"$SQ" >/dev/null || fail "square: $SQ"
jq -e '.items[0]|has("profitRate") and has("runtimeSeconds") and has("maxDrawdown7d") and (has("userId")|not)' <<<"$SQ" >/dev/null || fail "square fields: $SQ"
[ "$(code "$(api "${TOK[H]}" GET "/strategy/grid/$B1")")" = "404" ] || fail "other user sees bot"
[ "$(code "$(api "${TOK[H]}" POST "/strategy/grid/$B1/stop" '{}')")" = "404" ] || fail "other user stops bot"

say "one-click copy from the square"
[ "$(errcode "$(api "${TOK[G]}" POST "/strategy/grid/square/$B1/copy" '{"investment":"300"}')")" = "SELF_COPY" ] || fail "self copy allowed"
[ "$(code "$(api "${TOK[H]}" POST "/strategy/grid/square/999999999/copy" '{"investment":"300"}')")" = "404" ] || fail "copy unknown bot"
C="$(ok "$(api "${TOK[H]}" POST "/strategy/grid/square/$B1/copy" '{"investment":"300","sellBaseOnStop":true}')" "copy bot1")"
CID="$(jq -r .id <<<"$C")"
[ "$(jq -r '"\(.copiedFrom) \(.symbol) \(.gridCount) \(.investment)"' <<<"$C")" = "$B1 BTCUSDT $(jq -r .gridCount <<<"$(ok "$(api "${TOK[G]}" GET "/strategy/grid/$B1")" b1)") 300" ] || fail "copy params: $C"
[ "$(curl -sS "$GATEWAY_URL/strategy/grid/square?sort=copies" | jq -r --arg id "$B1" '.items[]|select(.id==$id)|.copyCount')" = "1" ] || fail "copyCount not 1"
ok "$(api "${TOK[H]}" POST "/strategy/grid/$CID/stop" '{"sellBase":true}')" "stop copy" >/dev/null
echo "  ok  copy: self-copy blocked, params cloned, copyCount=1, copy bot stopped"

say "bot 2: geometric, take-profit 50600 -> stop + sell base"
spot "${TOK[M]}" '{"symbol":"BTCUSDT","side":"sell","type":"limit","price":"50000","quantity":"0.02"}' >/dev/null
B="$(ok "$(grid '{"symbol":"BTCUSDT","mode":"geometric","lowerPrice":"45000","upperPrice":"55000","gridCount":5,"investment":"500","takeProfitPrice":"50600"}')" "create bot2")"
B2="$(jq -r .id <<<"$B")"
[ "$(jq -r .mode <<<"$B")" = "geometric" ] || fail "bot2: $B"
spot "${TOK[N]}" '{"symbol":"BTCUSDT","side":"buy","type":"limit","price":"49500","quantity":"0.05"}' >/dev/null   # bids for the close
spot "${TOK[M]}" '{"symbol":"BTCUSDT","side":"sell","type":"limit","price":"50600","quantity":"0.05"}' >/dev/null
spot "${TOK[N]}" '{"symbol":"BTCUSDT","side":"buy","type":"limit","price":"50600","quantity":"0.02"}' >/dev/null
wait_last 50600
B="$(wait_bot "${TOK[G]}" "$B2" '.status=="stopped"')"
[ "$(jq -r .stopReason <<<"$B")" = "take_profit" ] || fail "bot2 reason: $B"
[ "$(jq -r .openOrders <<<"$B")" = "0" ] || fail "bot2 open: $B"
lt "$(jq -r .baseHeld <<<"$B")" 0.00001 "bot2 sold its base"
jq -e '.finalValue // .value' <<<"$B" >/dev/null

say "bot 1: manual stop keeping base"
B="$(ok "$(api "${TOK[G]}" POST "/strategy/grid/$B1/stop" '{"sellBase":false}')" "stop bot1")"
[ "$(jq -r '"\(.status) \(.stopReason) \(.openOrders)"' <<<"$B")" = "stopped manual 0" ] || fail "bot1 stop: $B"
lt 0.005 "$(jq -r .baseHeld <<<"$B")" "bot1 kept base"
[ "$(errcode "$(api "${TOK[G]}" POST "/strategy/grid/$B1/stop" '{}')")" = "BOT_NOT_RUNNING" ] || fail "double stop"
OPEN="$(ok "$(api "${TOK[G]}" GET /spot/orders/open)" open | jq '[.items[]]|length')"
[ "$OPEN" = "0" ] || fail "spot open orders after stops: $OPEN"

say "bot 3: an order cancelled outside the bot stops it"
spot "${TOK[M]}" '{"symbol":"BTCUSDT","side":"sell","type":"limit","price":"50600","quantity":"0.01"}' >/dev/null
B="$(ok "$(grid '{"symbol":"BTCUSDT","lowerPrice":"40000","upperPrice":"52000","gridCount":3,"investment":"300","sellBaseOnStop":false}')" "create bot3")"
B3="$(jq -r .id <<<"$B")"
OID="$(ok "$(api "${TOK[G]}" GET "/strategy/grid/$B3/orders?status=open")" o3 | jq -r '.items[0].orderId')"
ok "$(api "${TOK[G]}" DELETE "/spot/orders/$OID")" "manual cancel" >/dev/null
B="$(wait_bot "${TOK[G]}" "$B3" '.status=="stopped"')"
[ "$(jq -r .stopReason <<<"$B")" = "order_canceled" ] || fail "bot3 reason: $B"
SQ="$(curl -sS "$GATEWAY_URL/strategy/grid/square")"
[ "$(jq '.total' <<<"$SQ")" = "0" ] || fail "square still lists stopped bots: $SQ"
L="$(ok "$(api "${TOK[G]}" GET '/strategy/grid?status=stopped')" stopped)"
[ "$(jq '.items|length' <<<"$L")" = "3" ] || fail "stopped list: $L"

printf '\n\033[1;32mSMOKE-GRID PASSED\033[0m\n'
