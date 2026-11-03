#!/usr/bin/env bash
#
# End-to-end smoke for USDT-margined perpetual futures (services/go/futures):
#   spot -> futures transfer, leverage / margin mode, limit + market fills with
#   0.05% fees on both sides, isolated margin adjust, TP trigger, funding,
#   isolated liquidation with ADL (fund empty), cross liquidation fee,
#   insurance-covered shortfall, tiered margin / position limits,
#   market data, conservation audit, futures -> spot transfer, gateway access.
#
#   bash scripts/smoke-futures.sh                       # boots devstack
#   GATEWAY_URL=... ASSET_URL=... ACCOUNT_URL=... FUTURES_URL=... INTERNAL_TOKEN=... \
#     SMOKE_INNER=1 bash scripts/smoke-futures.sh       # existing stack
#
set -euo pipefail
cd "$(dirname "$0")/.."

if [ "${SMOKE_INNER:-}" != "1" ]; then
  echo "== booting devstack =="
  ROOT="$(pwd)"
  cd tools/devstack
  exec go run . -root "$ROOT" -exec "SMOKE_INNER=1 bash scripts/smoke-futures.sh"
fi

GATEWAY_URL="${GATEWAY_URL:-http://localhost:8080}"
ASSET_URL="${ASSET_URL:-http://localhost:8082}"
ACCOUNT_URL="${ACCOUNT_URL:-http://localhost:8081}"
FUTURES_URL="${FUTURES_URL:-${FUTURES_INTERNAL_URL:-http://localhost:8089}}"
INTERNAL_TOKEN="${INTERNAL_TOKEN:-}"
PASSWORD="Str0ng-Passw0rd!"
TS="$(date +%s%N)"

say()  { printf '\n\033[1;34m== %s ==\033[0m\n' "$1"; }
fail() { printf '\033[1;31mSMOKE-FUTURES FAILED: %s\033[0m\n' "$1" >&2; exit 1; }
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

credit() {
  local c
  c="$(curl -sS -o /dev/null -w '%{http_code}' -X POST "$ASSET_URL/internal/credit" -H 'Content-Type: application/json' \
    -H "X-Internal-Token: $INTERNAL_TOKEN" -d "{\"userId\":\"$1\",\"asset\":\"USDT\",\"amount\":\"$2\",\"ref\":\"smoke-fut-$1\"}")"
  [ "$c" = "200" ] || fail "credit $1 returned $c"
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
dev() { # dev <METHOD> <path> [json]
  curl -sS -X "$1" "$FUTURES_URL$2" -H 'Content-Type: application/json' -H "X-Internal-Token: $INTERNAL_TOKEN" ${3:+-d "$3"}
}
spot_usdt() { api "$1" GET /assets/balances | sed '$d' | jq -r '.items[] | select(.asset=="USDT") | .free'; }
acct() { ok "$(api "$1" GET /futures/account)" account; }
bal() { acct "$1" | jq -r .walletBalance; }
order() { ok "$(api "$1" POST /futures/orders "$2")" "order $2"; }
lev() { ok "$(api "$1" POST /futures/leverage "{\"symbol\":\"BTCUSDT\",\"leverage\":$2,\"marginMode\":\"$3\"}")" "leverage $2 $3" >/dev/null; }
pos() { ok "$(api "$1" GET /futures/positions)" positions | jq -c "[.positions[] | select(.symbol==\"BTCUSDT\")][0] // empty"; }

say "gateway access"
[ "$(curl -sS -o /dev/null -w '%{http_code}' "$GATEWAY_URL/futures/market/contracts")" = "200" ] || fail "public market data not open"
[ "$(curl -sS -o /dev/null -w '%{http_code}' "$GATEWAY_URL/futures/account")" = "401" ] || fail "/futures/account open without token"
c="$(curl -sS -o /dev/null -w '%{http_code}' -X POST "$GATEWAY_URL/internal/dev/tick")"
[ "$c" = "404" ] || [ "$c" = "403" ] || [ "$c" = "401" ] || fail "gateway exposes /internal ($c)"
[ "$(curl -sS -o /dev/null -w '%{http_code}' -X POST "$FUTURES_URL/internal/dev/tick")" = "401" ] || fail "dev endpoint without token"

say "contracts"
C="$(curl -sS "$GATEWAY_URL/futures/market/contracts")"
[ "$(jq '.contracts|length' <<<"$C")" = "12" ] || fail "want 12 contracts"
[ "$(jq -r '.contracts[]|select(.symbol=="BTCUSDT").maxLeverage' <<<"$C")" = "200" ] || fail "BTC max lev"
[ "$(jq -r '.contracts[]|select(.symbol=="AVAXUSDT").maxLeverage' <<<"$C")" = "75" ] || fail "AVAX max lev"
eq "$(jq -r '.contracts[]|select(.symbol=="BTCUSDT").takerFeeRate' <<<"$C")" 0.0005 "taker fee"
[ "$(jq -r '.contracts[]|select(.symbol=="BTCUSDT").tags|index("layer1")!=null' <<<"$C")" = "true" ] || fail "BTC contract tags must include layer1"
[ "$(jq -r '[.contracts[]|select((.tags|type)!="array")]|length' <<<"$C")" = "0" ] || fail "every contract needs a tags array"
FT0="$(curl -sS "$GATEWAY_URL/futures/market/ticker?symbol=BTCUSDT")"
[ "$(jq -r .priceSource <<<"$FT0")" = "none" ] || fail "no index/trades/book -> priceSource none: $FT0"
[ "$(jq -r .source <<<"$(curl -sS "$GATEWAY_URL/futures/market/klines?symbol=BTCUSDT&interval=1m")")" = "none" ] || fail "klines source none"
eq "$(jq -r '.contracts[]|select(.symbol=="BTCUSDT").makerFeeRate' <<<"$C")" 0.0005 "maker fee"
[ "$(jq '.contracts[]|select(.symbol=="BTCUSDT").riskTiers|length' <<<"$C")" = "6" ] || fail "BTC risk tiers"
eq "$(jq -r '.contracts[]|select(.symbol=="BTCUSDT").riskTiers[1].maintenanceAmount' <<<"$C")" 50 "tier2 maint amount"
[ "$(jq -r '.defaults.leverage' <<<"$C")" = "20" ] && [ "$(jq -r '.defaults.marginMode' <<<"$C")" = "cross" ] || fail "defaults $C"

say "users + transfer in"
declare -A TOK UID_
for u in A B C D E F G H I K; do
  read -r t s < <(register "fut-${u,,}-$TS@example.com")
  TOK[$u]="$t"; UID_[$u]="$s"
  credit "$s" 100000
done
for u in A B C D E F G I K; do
  r="$(ok "$(api "${TOK[$u]}" POST /futures/transfer '{"direction":"in","amount":"20000"}')" "transfer $u")"
  [ "$(jq -r .status <<<"$r")" = "done" ] || fail "transfer $u: $r"
done
ok "$(api "${TOK[H]}" POST /futures/transfer '{"direction":"in","amount":"600"}')" "transfer H" >/dev/null
eq "$(spot_usdt "${TOK[A]}")" 80000 "A spot after transfer"
eq "$(bal "${TOK[A]}")" 20000 "A futures balance"
r="$(api "${TOK[A]}" POST /futures/transfer '{"direction":"in","amount":"999999"}')"
[ "$(errcode "$r")" = "INSUFFICIENT_BALANCE" ] || fail "over-transfer: $r"
eq "$(bal "${TOK[A]}")" 20000 "A balance after failed transfer"

say "leverage / mode"
L="$(ok "$(api "${TOK[A]}" GET '/futures/leverage?symbol=BTCUSDT')" lev)"
[ "$(jq -r .leverage <<<"$L")" = "20" ] && [ "$(jq -r .marginMode <<<"$L")" = "cross" ] || fail "default leverage $L"
r="$(api "${TOK[A]}" POST /futures/leverage '{"symbol":"BTCUSDT","leverage":201,"marginMode":"cross"}')"
[ "$(code "$r")" = "400" ] || fail "leverage 201 accepted"

say "no index -> fail closed"
r="$(api "${TOK[A]}" POST /futures/orders '{"symbol":"ETHUSDT","side":"buy","type":"limit","price":"3000","qty":"0.01"}')"
[ "$(errcode "$r")" = "MARK_PRICE_UNAVAILABLE" ] || fail "order without mark: $r"

say "index 50000; A limit short 1 (maker), B market long 1 (taker)"
P="$(dev POST /internal/dev/index '{"symbol":"BTCUSDT","price":"50000"}')"
eq "$(jq -r .markPrice <<<"$P")" 50000 "mark"
order "${TOK[A]}" '{"symbol":"BTCUSDT","side":"sell","type":"limit","price":"50000","qty":"1"}' >/dev/null
R="$(order "${TOK[B]}" '{"symbol":"BTCUSDT","side":"buy","type":"market","qty":"1"}')"
[ "$(jq -r .order.status <<<"$R")" = "filled" ] || fail "B market: $R"
eq "$(ok "$(api "${TOK[A]}" GET /futures/trades)" t | jq -r '.trades[0].fee')" 25 "A maker fee 0.05%"
eq "$(ok "$(api "${TOK[B]}" GET /futures/trades)" t | jq -r '.trades[0].fee')" 25 "B taker fee 0.05%"
eq "$(bal "${TOK[A]}")" 19975 "A balance after fee"
PA="$(pos "${TOK[A]}")"
[ "$(jq -r .side <<<"$PA")" = "short" ] || fail "A pos $PA"
eq "$(jq -r .margin <<<"$PA")" 2500 "A cross IM"
r="$(api "${TOK[A]}" POST /futures/leverage '{"symbol":"BTCUSDT","leverage":10,"marginMode":"isolated"}')"
[ "$(errcode "$r")" = "HAS_POSITION" ] || fail "mode switch with position: $r"

say "tiered limits: 200x caps notional at 50000"
lev "${TOK[I]}" 200 cross
r="$(api "${TOK[I]}" POST /futures/orders '{"symbol":"BTCUSDT","side":"buy","type":"limit","price":"40000","qty":"2"}')"
[ "$(errcode "$r")" = "POSITION_LIMIT" ] || fail "200x 80000 notional accepted: $r"
lev "${TOK[I]}" 20 cross
ok "$(api "${TOK[I]}" POST /futures/orders '{"symbol":"BTCUSDT","side":"buy","type":"limit","price":"40000","qty":"2"}')" "20x 2 BTC" | jq -r .order.orderId > /tmp/fut-i-oid.$$
ok "$(api "${TOK[I]}" DELETE "/futures/orders/$(cat /tmp/fut-i-oid.$$)")" cancel >/dev/null; rm -f /tmp/fut-i-oid.$$
r="$(api "${TOK[A]}" POST /futures/leverage '{"symbol":"BTCUSDT","leverage":200,"marginMode":"cross"}')"
[ "$(code "$r")" = "200" ] || fail "A 200x with 50000 notional: $r"
lev "${TOK[A]}" 20 cross

say "resting order freezes and cancel releases"
R="$(order "${TOK[A]}" '{"symbol":"BTCUSDT","side":"sell","type":"limit","price":"60000","qty":"0.1","reduceOnly":false}')"
lt 0 "$(acct "${TOK[A]}" | jq -r .orderMargin)" "order margin frozen"
OID="$(jq -r .order.orderId <<<"$R")"
ok "$(api "${TOK[A]}" DELETE "/futures/orders/$OID")" cancel >/dev/null
eq "$(acct "${TOK[A]}" | jq -r .orderMargin)" 0 "order margin released"

say "isolated: C long 0.5 @49000 10x, D market short"
lev "${TOK[C]}" 10 isolated
order "${TOK[C]}" '{"symbol":"BTCUSDT","side":"buy","type":"limit","price":"49000","qty":"0.5"}' >/dev/null
order "${TOK[D]}" '{"symbol":"BTCUSDT","side":"sell","type":"market","qty":"0.5"}' >/dev/null
PC="$(pos "${TOK[C]}")"
[ "$(jq -r .marginMode <<<"$PC")" = "isolated" ] || fail "C mode $PC"
eq "$(jq -r .margin <<<"$PC")" 2450 "C iso margin"
eq "$(bal "${TOK[C]}")" 17537.75 "C balance (20000-2450-12.25 fee)"
ok "$(api "${TOK[C]}" POST /futures/positions/BTCUSDT/margin '{"amount":"200"}')" add >/dev/null
eq "$(pos "${TOK[C]}" | jq -r .margin)" 2650 "C margin +200"
ok "$(api "${TOK[C]}" POST /futures/positions/BTCUSDT/margin '{"amount":"-100"}')" remove >/dev/null
eq "$(pos "${TOK[C]}" | jq -r .margin)" 2550 "C margin -100"
r="$(api "${TOK[C]}" POST /futures/positions/BTCUSDT/margin '{"amount":"-200"}')"
[ "$(errcode "$r")" = "MARGIN_TOO_LOW" ] || fail "removal below IM (2500 @ mark) allowed: $r"
eq "$(bal "${TOK[C]}")" 17437.75 "C balance after adjust"

say "TP: B tp 52000, E bid 51900; index -> 52000"
ok "$(api "${TOK[B]}" POST /futures/positions/BTCUSDT/tpsl '{"tpPrice":"52000","slPrice":"45000"}')" tpsl >/dev/null
[ "$(pos "${TOK[B]}" | jq -r .tpPrice)" = "52000" ] || fail "tp not stored"
order "${TOK[E]}" '{"symbol":"BTCUSDT","side":"buy","type":"limit","price":"51900","qty":"1"}' >/dev/null
dev POST /internal/dev/index '{"symbol":"BTCUSDT","price":"52000"}' >/dev/null
dev POST /internal/dev/tick '' >/dev/null
[ -z "$(pos "${TOK[B]}")" ] || fail "B still has position after TP: $(pos "${TOK[B]}")"
ok "$(api "${TOK[B]}" GET /futures/orders)" o | jq -e '.orders[] | select(.source=="tp" and .status=="filled")' >/dev/null || fail "no filled tp order"
# B: 20000 - 25 (open) + 1900 pnl - 25.95 (close fee 51900*0.0005)
eq "$(bal "${TOK[B]}")" 21849.05 "B balance after TP"

say "funding 0.0001 @ mark 52000"
F="$(dev POST /internal/dev/funding '{"symbol":"BTCUSDT","rate":"0.0001"}')"
eq "$(jq -r .markPrice <<<"$F")" 52000 "funding mark"
eq "$(ok "$(api "${TOK[E]}" GET '/futures/bills?kind=funding')" b | jq -r '.bills[0].amount')" -5.2 "E long pays"
eq "$(ok "$(api "${TOK[A]}" GET '/futures/bills?kind=funding')" b | jq -r '.bills[0].amount')" 5.2 "A short receives"
[ "$(curl -sS "$GATEWAY_URL/futures/market/funding-history?symbol=BTCUSDT" | jq '.items // .history // . | length')" != "0" ] || fail "funding history empty"

say "ADL: F long 1 @52000 50x iso, gap to 45000 with an empty insurance fund"
lev "${TOK[F]}" 50 isolated
order "${TOK[G]}" '{"symbol":"BTCUSDT","side":"sell","type":"limit","price":"52000","qty":"1"}' >/dev/null
order "${TOK[F]}" '{"symbol":"BTCUSDT","side":"buy","type":"market","qty":"1"}' >/dev/null
eq "$(pos "${TOK[F]}" | jq -r .margin)" 1040 "F iso margin"
FB="$(bal "${TOK[F]}")"; eq "$FB" 18934 "F balance (20000-1040-26)"
eq "$(pos "${TOK[G]}" | jq -r .adlRank)" 5 "G (best short score) first in ADL queue"
eq "$(curl -sS "$GATEWAY_URL/futures/market/insurance-fund" | jq -r .balance)" 0 "insurance empty"
dev POST /internal/dev/index '{"symbol":"BTCUSDT","price":"45000"}' >/dev/null
[ -z "$(pos "${TOK[F]}")" ] || fail "F not liquidated"
eq "$(bal "${TOK[F]}")" 18934 "F loss capped at isolated margin"
[ -z "$(pos "${TOK[G]}")" ] || fail "G not deleveraged: $(pos "${TOK[G]}")"
# G closed at F's bankruptcy price 52000-1040 = 50960: 20000 - 26 + 1040
eq "$(bal "${TOK[G]}")" 21014 "G ADL pnl at bankruptcy price"
ok "$(api "${TOK[G]}" GET /futures/orders)" o | jq -e '.orders[] | select(.source=="adl" and .status=="filled")' >/dev/null || fail "no adl order for G"
INS="$(curl -sS "$GATEWAY_URL/futures/market/insurance-fund")"
eq "$(jq -r '.recentLiquidations[0].shortfall' <<<"$INS")" 0 "no shortfall left after ADL"
eq "$(jq -r .balance <<<"$INS")" 0 "insurance untouched"
eq "$(jq -r '.recentAdl[0].price' <<<"$INS")" 50960 "ADL price"
[ -n "$(pos "${TOK[A]}")" ] || fail "A should not be deleveraged"

say "cross liquidation fee: H short 1 @45000 100x with 600 USDT"
lev "${TOK[H]}" 100 cross
order "${TOK[I]}" '{"symbol":"BTCUSDT","side":"buy","type":"limit","price":"45000","qty":"1"}' >/dev/null
order "${TOK[H]}" '{"symbol":"BTCUSDT","side":"sell","type":"market","qty":"1"}' >/dev/null
eq "$(bal "${TOK[H]}")" 577.5 "H balance after fee"
dev POST /internal/dev/index '{"symbol":"BTCUSDT","price":"45500"}' >/dev/null
[ -z "$(pos "${TOK[H]}")" ] || fail "H not liquidated: $(pos "${TOK[H]}")"
eq "$(bal "${TOK[H]}")" 0 "H cross equity taken"
INS="$(curl -sS "$GATEWAY_URL/futures/market/insurance-fund")"
eq "$(jq -r '.recentLiquidations[0].fee' <<<"$INS")" 77.5 "liquidation fee = remaining equity"
eq "$(jq -r '.recentLiquidations[0].shortfall' <<<"$INS")" 0 "no shortfall"
eq "$(jq -r .balance <<<"$INS")" 77.5 "insurance = H fee"

say "insurance covers a small shortfall: K long 0.1 @45500 100x iso, gap to 44500"
lev "${TOK[K]}" 100 isolated
order "${TOK[I]}" '{"symbol":"BTCUSDT","side":"sell","type":"limit","price":"45500","qty":"0.1"}' >/dev/null
order "${TOK[K]}" '{"symbol":"BTCUSDT","side":"buy","type":"market","qty":"0.1"}' >/dev/null
eq "$(pos "${TOK[K]}" | jq -r .margin)" 45.5 "K iso margin"
KB="$(bal "${TOK[K]}")"
dev POST /internal/dev/index '{"symbol":"BTCUSDT","price":"44500"}' >/dev/null
[ -z "$(pos "${TOK[K]}")" ] || fail "K not liquidated"
eq "$(bal "${TOK[K]}")" "$KB" "K loss capped"
INS="$(curl -sS "$GATEWAY_URL/futures/market/insurance-fund")"
lt 0 "$(jq -r '.recentLiquidations[0].shortfall' <<<"$INS")" "K shortfall"
lt 0 "$(jq -r .balance <<<"$INS")" "insurance covered K (no ADL, balance still positive)"
[ "$(jq '.recentAdl|length' <<<"$INS")" = "1" ] || fail "unexpected ADL: $INS"

say "market data"
T="$(curl -sS "$GATEWAY_URL/futures/market/ticker?symbol=BTCUSDT")"
jq -e '(.. | objects | select(has("markPrice"))) | .markPrice' <<<"$T" >/dev/null || fail "ticker: $T"
D="$(curl -sS "$GATEWAY_URL/futures/market/depth?symbol=BTCUSDT")"
jq -e 'has("bids") and has("asks")' <<<"$D" >/dev/null || fail "depth: $D"
[ "$(curl -sS "$GATEWAY_URL/futures/market/trades?symbol=BTCUSDT" | jq '[.. | arrays][0] | length')" -ge 5 ] || fail "trades"
[ "$(curl -sS "$GATEWAY_URL/futures/market/klines?symbol=BTCUSDT&interval=1m" | jq '[.. | arrays][0] | length')" -ge 1 ] || fail "klines"
dev POST /internal/dev/tick '' >/dev/null
[ "$(curl -sS "$GATEWAY_URL/futures/market/mark-history?symbol=BTCUSDT" | jq '[.. | arrays][0] | length')" -ge 1 ] || fail "mark history"
[ "$(curl -sS "$GATEWAY_URL/futures/market/klines?symbol=BTCUSDT&interval=1m" | jq -r .source)" = "trade" ] || fail "BTC klines source trade"
# no-trade fallback: ETH has an index but no trades/book -> lastPrice=index, flat 24h, index candles
dev POST /internal/dev/index '{"symbol":"ETHUSDT","price":"3000"}' >/dev/null
dev POST /internal/dev/tick '' >/dev/null
ET="$(curl -sS "$GATEWAY_URL/futures/market/ticker?symbol=ETHUSDT")"
[ "$(jq -r .priceSource <<<"$ET")" = "index" ] || fail "ETH priceSource: $ET"
eq "$(jq -r .lastPrice <<<"$ET")" 3000 "ETH lastPrice falls back to index"
eq "$(jq -r .highPrice24h <<<"$ET")" 3000 "ETH 24h high flat"
eq "$(jq -r .priceChangePercent24h <<<"$ET")" 0 "ETH 24h change"
EK="$(curl -sS "$GATEWAY_URL/futures/market/klines?symbol=ETHUSDT&interval=1m")"
[ "$(jq -r .source <<<"$EK")" = "index" ] && [ "$(jq '.klines|length' <<<"$EK")" -ge 1 ] || fail "ETH index klines: $EK"
eq "$(jq -r '.klines[-1].close' <<<"$EK")" 3000 "ETH index candle close"

say "transfer out"
r="$(api "${TOK[A]}" POST /futures/transfer '{"direction":"out","amount":"9999999"}')"
[ "$(code "$r")" = "400" ] || fail "over-withdraw accepted: $r"
ok "$(api "${TOK[A]}" POST /futures/transfer '{"direction":"out","amount":"1000"}')" out >/dev/null
eq "$(spot_usdt "${TOK[A]}")" 81000 "A spot after transfer out"

say "conservation audit"
AU="$(dev GET /internal/dev/audit)"
eq "$(jq -r .diff <<<"$AU")" 0 "Σequity vs net transfers ($AU)"
lt 0 "$(jq -r .feeBalance <<<"$AU")" "fee account accrued"

printf '\n\033[1;32mSMOKE-FUTURES PASSED\033[0m\n'
