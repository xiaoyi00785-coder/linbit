#!/usr/bin/env bash
#
# End-to-end smoke for savings products (earn service): public catalogue,
# simple (flexible) with T+1 interest, bonus tier and daily payout, redeem,
# auto-earn sweep, fixed term (newcomer cap, newcomer-only, no early redeem,
# maturity payout, total quota), on-chain staking with unlock period, summary.
#
#   bash scripts/smoke-earn.sh            # boots devstack
#
set -euo pipefail
cd "$(dirname "$0")/.."

if [ "${SMOKE_INNER:-}" != "1" ]; then
  echo "== booting devstack =="
  ROOT="$(pwd)"
  cd tools/devstack
  exec go run . -root "$ROOT" -exec "SMOKE_INNER=1 bash scripts/smoke-earn.sh"
fi

GATEWAY_URL="${GATEWAY_URL:-http://localhost:8080}"
ASSET_URL="${ASSET_URL:-http://localhost:8082}"
ACCOUNT_URL="${ACCOUNT_URL:-http://localhost:8081}"
FUTURES_URL="${FUTURES_URL:-http://localhost:8089}"
GROWTH_URL="${GROWTH_URL:-http://localhost:8091}"
EARN_URL="${EARN_URL:-http://localhost:8092}"
ADMIN_URL="${ADMIN_URL:-http://localhost:8088}"
INTERNAL_TOKEN="${INTERNAL_TOKEN:-}"
PASSWORD="Str0ng-Passw0rd!"
TS="$(date +%s%N)"
SMOKE_NAME="${SMOKE_NAME:-EARN}"
. scripts/lib-growth-smoke.sh

declare -A TOK UID_
reg() { read -r t s < <(register "earn-${1,,}-$TS@example.com"); TOK[$1]="$t"; UID_[$1]="$s"; }
advance() { internal "$EARN_URL" POST /internal/dev/earn/advance "{\"hours\":$1}" >/dev/null; }
sub() { ok "$(api "${TOK[$1]}" POST "/earn/products/$2/subscribe" "{\"amount\":\"$3\"}")" "subscribe $2 $3"; }
red() { ok "$(api "${TOK[$1]}" POST "/earn/products/$2/redeem" "$3")" "redeem $2 $3"; }
err_is() { [ "$(errcode "$1")" = "$2" ] || fail "want $2, got $(code "$1") $(body "$1")"; }
pos() { ok "$(api "${TOK[$1]}" GET /earn/positions)" positions | jq -r --arg p "$2" '([.items[] | select(.productId==$p) | .amount][0]) // "0"'; }

internal "$FUTURES_URL" POST /internal/dev/index '{"symbol":"ETHUSDT","price":"2500"}' >/dev/null

say "public catalogue"
[ "$(curl -sS "$GATEWAY_URL/earn/products" | jq '.items | length')" -ge 20 ] || fail "products"
[ "$(curl -sS "$GATEWAY_URL/earn/products?kind=fixed" | jq '[.items[] | select(.kind!="fixed")] | length')" = 0 ] || fail "kind filter"
p="$(curl -sS "$GATEWAY_URL/earn/products/simple-USDT")"
eq "$(jq -r .aprMax <<<"$p")" 0.012 "simple-USDT aprMax"
[ "$(jq -r .redeemRule <<<"$(curl -sS "$GATEWAY_URL/earn/products/chain-SOL")")" = "2 天解锁" ] || fail "redeem rule"

reg E; reg F; reg G
credit "${UID_[E]}" USDT 20000; credit "${UID_[E]}" ETH 10; credit "${UID_[E]}" SOL 100
credit "${UID_[F]}" USDT 1000
credit "${UID_[G]}" USDT 500

say "simple: T+1 interest with bonus tier"
err_is "$(api "${TOK[E]}" POST /earn/products/simple-USDT/subscribe '{"amount":"0.5"}')" AMOUNT_TOO_SMALL
err_is "$(api "${TOK[E]}" POST /earn/products/nope/subscribe '{"amount":"5"}')" PRODUCT_NOT_FOUND
sub E simple-USDT 15000 >/dev/null
eq "$(spot_bal "${TOK[E]}" USDT)" 5000 "spot after subscribe"
eq "$(pos E simple-USDT)" 15000 "simple position"
advance 24
eq "$(spot_bal "${TOK[E]}" USDT)" 5000 "no interest on subscription day"
advance 24
# (10000 x 1.2% + 5000 x 1.0%) / 365
eq "$(spot_bal "${TOK[E]}" USDT)" 5000.46575342 "first day interest"
red E simple-USDT '{"amount":"5000"}' >/dev/null
eq "$(pos E simple-USDT)" 10000 "after partial redeem"
err_is "$(api "${TOK[E]}" POST /earn/products/simple-USDT/redeem '{"amount":"20000"}')" INSUFFICIENT_POSITION
red E simple-USDT '{"all":true}' >/dev/null
eq "$(spot_bal "${TOK[E]}" USDT)" 20000.46575342 "after redeem all"
[ "$(ok "$(api "${TOK[E]}" GET '/earn/records?type=interest')" rec | jq '.items | length')" = 1 ] || fail "interest record"

say "fixed: newcomer cap, no early redeem, maturity"
err_is "$(api "${TOK[E]}" POST /earn/products/fixed-USDT-7/subscribe '{"amount":"1500"}')" USER_LIMIT_EXCEEDED
o="$(sub E fixed-USDT-7 1000)"
eq "$(jq -r .expectedInterest <<<"$o")" 1.68767123 "7d expected interest"
err_is "$(api "${TOK[E]}" POST /earn/products/fixed-USDT-7/redeem '{"all":true}')" REDEEM_NOT_ALLOWED
sub F fixed-USDT-30 100 >/dev/null
err_is "$(api "${TOK[F]}" POST /earn/products/fixed-USDT-7/subscribe '{"amount":"100"}')" NEWCOMER_ONLY
advance 167
[ "$(ok "$(api "${TOK[E]}" GET '/earn/orders?status=holding')" o | jq '.items | length')" = 1 ] || fail "matured early"
advance 2
[ "$(ok "$(api "${TOK[E]}" GET '/earn/orders?status=matured')" o | jq '.items | length')" = 1 ] || fail "not matured"
eq "$(spot_bal "${TOK[E]}" USDT)" 20002.15342465 "after maturity"

say "total quota via ops upsert"
r="$(internal "$EARN_URL" POST /internal/earn/products "{\"id\":\"fixed-USDT-q$TS\",\"kind\":\"fixed\",\"asset\":\"USDT\",\"title\":\"quota test\",\"apr\":\"0.05\",\"termDays\":3,\"minAmount\":\"1\",\"totalQuota\":\"100\",\"sort\":99}")"
[ "$(jq -r .id <<<"$r")" = "fixed-USDT-q$TS" ] || fail "upsert: $r"
err_is "$(api "${TOK[F]}" POST "/earn/products/fixed-USDT-q$TS/subscribe" '{"amount":"150"}')" QUOTA_EXCEEDED
sub F "fixed-USDT-q$TS" 100 >/dev/null
[ "$(curl -sS "$GATEWAY_URL/earn/products/fixed-USDT-q$TS" | jq -r .remaining)" = 0 ] || fail "remaining"

say "on-chain staking with unlock"
sub E chain-SOL 50 >/dev/null
sub E chain-ETH 1 >/dev/null
advance 48
eq "$(spot_bal "${TOK[E]}" SOL)" 50.00876712 "SOL staking interest"
r="$(red E chain-SOL '{"all":true}')"
[ "$(jq -r .status <<<"$r")" = unlocking ] || fail "unlock: $r"
eq "$(spot_bal "${TOK[E]}" SOL)" 50.00876712 "SOL locked during unlock"
[ "$(ok "$(api "${TOK[E]}" GET /earn/positions)" p | jq '.unlocking | length')" = 1 ] || fail "unlocking list"
advance 49
eq "$(spot_bal "${TOK[E]}" SOL)" 100.00876712 "SOL released"
r="$(red E chain-ETH '{"all":true}')"
[ "$(jq -r .status <<<"$r")" = redeemed ] || fail "eth redeem: $r"

say "auto-earn sweep"
err_is "$(api "${TOK[G]}" POST /earn/products/chain-ETH/auto '{"enabled":true}')" AUTO_NOT_SUPPORTED
ok "$(api "${TOK[G]}" POST /earn/products/simple-USDT/auto '{"enabled":true}')" auto >/dev/null
advance 0
eq "$(spot_bal "${TOK[G]}" USDT)" 0 "auto swept"
eq "$(pos G simple-USDT)" 500 "auto position"

say "summary"
sub E simple-USDT 1000 >/dev/null
s="$(ok "$(api "${TOK[E]}" GET /earn/summary)" summary)"
eq "$(jq -r .totalValueUsdt <<<"$s")" 1000 "summary value"
python3 -c "import sys;from decimal import Decimal as D;sys.exit(0 if D('$(jq -r .totalInterestUsdt <<<"$s")')>D('2') else 1)" || fail "summary interest: $s"

printf '\n\033[1;32mSMOKE-EARN PASSED\033[0m\n'
