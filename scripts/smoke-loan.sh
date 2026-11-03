#!/usr/bin/env bash
#
# End-to-end smoke for crypto loans (earn service): market/quote, borrow
# validation and limits, hourly interest, partial and full repay, adding
# collateral, renewing a fixed term with the early-repay penalty, overdue
# interest at 3x, and liquidation by price (LTV) and by 7-day overdue.
#
#   bash scripts/smoke-loan.sh            # boots devstack
#
set -euo pipefail
cd "$(dirname "$0")/.."

if [ "${SMOKE_INNER:-}" != "1" ]; then
  echo "== booting devstack =="
  ROOT="$(pwd)"
  cd tools/devstack
  exec go run . -root "$ROOT" -exec "SMOKE_INNER=1 bash scripts/smoke-loan.sh"
fi

GATEWAY_URL="${GATEWAY_URL:-http://localhost:8080}"
ASSET_URL="${ASSET_URL:-http://localhost:8082}"
ACCOUNT_URL="${ACCOUNT_URL:-http://localhost:8081}"
FUTURES_URL="${FUTURES_URL:-${FUTURES_INTERNAL_URL:-http://localhost:8089}}"
GROWTH_URL="${GROWTH_URL:-http://localhost:8091}"
EARN_URL="${EARN_URL:-http://localhost:8092}"
ADMIN_URL="${ADMIN_URL:-http://localhost:8088}"
INTERNAL_TOKEN="${INTERNAL_TOKEN:-}"
PASSWORD="Str0ng-Passw0rd!"
TS="$(date +%s%N)"
SMOKE_NAME="${SMOKE_NAME:-LOAN}"
. scripts/lib-growth-smoke.sh

declare -A TOK UID_
reg() { read -r t s < <(register "loan-${1,,}-$TS@example.com"); TOK[$1]="$t"; UID_[$1]="$s"; }
index() { internal "$FUTURES_URL" POST /internal/dev/index "{\"symbol\":\"$1\",\"price\":\"$2\"}" >/dev/null; }
advance() { internal "$EARN_URL" POST /internal/dev/earn/advance "{\"hours\":$1}" >/dev/null; }
borrow() { ok "$(api "${TOK[$1]}" POST /loans "$2")" "borrow $2" | jq -r .id; }
loan() { ok "$(api "${TOK[$1]}" GET "/loans/$2")" "loan $2"; }
expect_err() { # expect_err <user> <body> <code>
  local r; r="$(api "${TOK[$1]}" POST /loans "$2")"
  [ "$(errcode "$r")" = "$3" ] || fail "borrow $2: want $3, got $(code "$r") $(body "$r")"
}

say "prices"
index BTCUSDT 50000
index ETHUSDT 2500

say "public market + quote"
m="$(curl -sS "$GATEWAY_URL/loans/market")"
[ "$(jq -r '.collaterals | length' <<<"$m")" = 2 ] || fail "market: $m"
eq "$(jq -r .liquidationLtv <<<"$m")" 0.95 "liquidation ltv"
q="$(curl -sS "$GATEWAY_URL/loans/quote?collateralAsset=BTC&amount=10000&term=flexible")"
eq "$(jq -r .collateral <<<"$q")" 0.25 "quote collateral"
eq "$(jq -r .hourlyInterest <<<"$q")" 0.03847 "quote hourly interest"

reg A; reg B; reg C; reg D
credit "${UID_[A]}" BTC 1; credit "${UID_[A]}" USDT 100
credit "${UID_[B]}" ETH 10; credit "${UID_[B]}" USDT 100
credit "${UID_[C]}" BTC 1
credit "${UID_[D]}" BTC 1

say "validation + limits"
expect_err A '{"collateralAsset":"BTC","amount":"5"}' AMOUNT_TOO_SMALL
expect_err A '{"collateralAsset":"DOGE","amount":"100"}' UNSUPPORTED_COLLATERAL
expect_err A '{"collateralAsset":"BTC","amount":"100","term":"90d"}' INVALID_TERM
expect_err A '{"collateralAsset":"BTC","amount":"10000","collateralAmount":"0.2"}' LTV_TOO_HIGH
expect_err A '{"collateralAsset":"BTC","amount":"1300000"}' LOAN_LIMIT_EXCEEDED
expect_err A '{"collateralAsset":"BTC","amount":"600000"}' COLLATERAL_LIMIT_EXCEEDED

say "flexible borrow + hourly interest"
LA="$(borrow A '{"collateralAsset":"BTC","amount":"10000"}')"
eq "$(spot_bal "${TOK[A]}" USDT)" 10100 "USDT after borrow"
eq "$(spot_bal "${TOK[A]}" BTC)" 0.75 "BTC after borrow"
l="$(loan A "$LA")"
[ "$(jq -r .status <<<"$l")" = active ] || fail "loan A: $l"
eq "$(jq -r .interest <<<"$l")" 0.03847 "first hour interest"
eq "$(jq -r .ltv <<<"$l")" 0.8 "initial ltv" 0.01
advance 10
eq "$(jq -r .interest <<<"$(loan A "$LA")")" 0.42317 "11 hours interest"

say "partial repay (interest first)"
r="$(ok "$(api "${TOK[A]}" POST "/loans/$LA/repay" '{"amount":"1000"}')" repay)"
eq "$(jq -r .repaid.interest <<<"$r")" 0.42317 "repaid interest"
eq "$(jq -r .repaid.principal <<<"$r")" 999.57683 "repaid principal"
eq "$(jq -r .principal <<<"$(loan A "$LA")")" 9000.42317 "principal left"

say "add collateral"
ok "$(api "${TOK[A]}" POST "/loans/$LA/collateral" '{"amount":"0.05"}')" "add collateral" >/dev/null
eq "$(jq -r .collateral <<<"$(loan A "$LA")")" 0.3 "collateral after add"
eq "$(spot_bal "${TOK[A]}" BTC)" 0.7 "BTC after add"
r="$(api "${TOK[A]}" POST "/loans/$LA/renew")"
[ "$(errcode "$r")" = TERM_NOT_RENEWABLE ] || fail "renew flexible: $(body "$r")"

say "repay all releases collateral"
ok "$(api "${TOK[A]}" POST "/loans/$LA/repay" '{"all":true}')" "repay all" >/dev/null
[ "$(jq -r .status <<<"$(loan A "$LA")")" = repaid ] || fail "loan A not repaid"
eq "$(spot_bal "${TOK[A]}" BTC)" 1 "BTC back"
eq "$(spot_bal "${TOK[A]}" USDT)" 99.57683 "USDT after full repay (11h interest)"
r="$(api "${TOK[A]}" POST "/loans/$LA/repay" '{"all":true}')"
[ "$(errcode "$r")" = LOAN_NOT_ACTIVE ] || fail "repay closed: $(body "$r")"
r="$(api "${TOK[A]}" GET "/loans/abc")"
[ "$(errcode "$r")" = LOAN_NOT_FOUND ] || fail "bad id: $(body "$r")"
[ "$(ok "$(api "${TOK[A]}" GET "/loans?status=closed")" closed | jq '.items | length')" = 1 ] || fail "closed list"
[ "$(ok "$(api "${TOK[A]}" GET "/loans/records")" records | jq '.items | length')" -ge 4 ] || fail "records"

say "7d term: renew + early repay penalty"
LB="$(borrow B '{"collateralAsset":"ETH","amount":"5000","term":"7d"}')"
l="$(loan B "$LB")"; due0="$(jq -r .dueAt <<<"$l")"
eq "$(jq -r .collateral <<<"$l")" 2.5 "ETH collateral"
advance 24
ok "$(api "${TOK[B]}" POST "/loans/$LB/renew")" renew >/dev/null
l="$(loan B "$LB")"
[ "$(( $(jq -r .dueAt <<<"$l") - due0 ))" = $((7*24*3600*1000)) ] || fail "renew due shift: $l"
eq "$(jq -r .interest <<<"$l")" 0 "interest paid at renew"
r="$(ok "$(api "${TOK[B]}" POST "/loans/$LB/repay" '{"all":true}')" "repay B")"
eq "$(jq -r .repaid.penalty <<<"$r")" 2.5 "early repay penalty"
eq "$(spot_bal "${TOK[B]}" ETH)" 10 "ETH back"

say "overdue 3x then liquidation after 7 days overdue"
LC="$(borrow C '{"collateralAsset":"BTC","amount":"1000","term":"7d"}')"
advance 170
l="$(loan C "$LC")"
[ "$(jq -r .overdue <<<"$l")" = true ] || fail "not overdue: $l"
# 171 charged hours, the last 3 at 3x: 177 * 0.0042808
eq "$(jq -r .interest <<<"$l")" 0.7577016 "overdue interest"
advance $((7*24))
l="$(loan C "$LC")"
[ "$(jq -r .status <<<"$l")" = liquidated ] || fail "C not liquidated: $l"
[ "$(python3 -c "from decimal import Decimal as D;print(D('$(spot_bal "${TOK[C]}" BTC)')>D('0.979'))")" = True ] || fail "C remainder"

say "price drop: warning then liquidation"
LD="$(borrow D '{"collateralAsset":"BTC","amount":"10000"}')"
index BTCUSDT 45000; advance 0
l="$(loan D "$LD")"
[ "$(jq -r .warning <<<"$l")" = true ] || fail "no warning: $l"
index BTCUSDT 42000; advance 0
l="$(loan D "$LD")"
[ "$(jq -r .status <<<"$l")" = liquidated ] || fail "D not liquidated: $l"
rec="$(ok "$(api "${TOK[D]}" GET "/loans/records?type=liquidate")" liq | jq '.items[0]')"
back="$(jq -r .detail.collateralReturned <<<"$rec")"
eq "$(spot_bal "${TOK[D]}" BTC)" "$(python3 -c "print(0.75+$back)")" "D BTC after liquidation" 0.00000001
debt="$(jq -r .amount <<<"$rec")"
python3 -c "from decimal import Decimal as D;import sys;s=D('0.25')-D('$back');sys.exit(0 if abs(s*42000-D('$debt')*D('1.03'))<1 else 1)" || fail "seize math: $rec"
index BTCUSDT 50000

printf '\n\033[1;32mSMOKE-LOAN PASSED\033[0m\n'
