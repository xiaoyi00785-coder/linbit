#!/usr/bin/env bash
#
# End-to-end smoke for the points carnival + coupon center + futures bonus:
#   ended period 5 rejects enroll/redeem, admin schedules a period (2FA +
#   audit, overlap rejected), points only after enrollment (volume tiers,
#   deposit cap, valid invitee), redeem rules (once per tier, balance, tier,
#   enrollment), voucher activation (trial needs own futures funds), and the
#   bonus paying futures fees (deduction first, then trial).
#
#   bash scripts/smoke-points.sh            # boots devstack
#   SMOKE_INNER=1 ... bash scripts/smoke-points.sh   # existing stack
#
set -euo pipefail
cd "$(dirname "$0")/.."

if [ "${SMOKE_INNER:-}" != "1" ]; then
  echo "== booting devstack =="
  ROOT="$(pwd)"
  cd tools/devstack
  exec go run . -root "$ROOT" -exec "SMOKE_INNER=1 bash scripts/smoke-points.sh"
fi

GATEWAY_URL="${GATEWAY_URL:-http://localhost:8080}"
ASSET_URL="${ASSET_URL:-http://localhost:8082}"
ACCOUNT_URL="${ACCOUNT_URL:-http://localhost:8081}"
FUTURES_URL="${FUTURES_URL:-${FUTURES_INTERNAL_URL:-http://localhost:8089}}"
GROWTH_URL="${GROWTH_URL:-http://localhost:8091}"
ADMIN_URL="${ADMIN_URL:-http://localhost:8088}"
INTERNAL_TOKEN="${INTERNAL_TOKEN:-}"
SA_USER="${ADMIN_BOOTSTRAP_USERNAME:-}"
SA_PASS="${ADMIN_BOOTSTRAP_PASSWORD:-}"
SA_TOTP="${ADMIN_BOOTSTRAP_TOTP_SECRET:-}"
PASSWORD="Str0ng-Passw0rd!"
TS="$(date +%s%N)"
SMOKE_NAME="${SMOKE_NAME:-POINTS}"
. scripts/lib-growth-smoke.sh
[ -n "$SA_USER" ] && [ -n "$SA_PASS" ] && [ -n "$SA_TOTP" ] || fail "ADMIN_BOOTSTRAP_* not set"
SA="$(alogin "$SA_USER" "$SA_PASS" "$SA_TOTP")"; [ -n "$SA" ] || fail "admin login"

declare -A TOK UID_
reg() { read -r t s < <(register "points-${1,,}-$TS@example.com" "${2:-}"); TOK[$1]="$t"; UID_[$1]="$s"; }
deposit() { # deposit <uid> <amount> <n>
  [ "$(internal "$ASSET_URL" POST /internal/deposit "{\"userId\":\"$1\",\"asset\":\"USDT\",\"amount\":\"$2\",\"ref\":\"smoke-dep-$TS-$3\"}" | jq -r .status)" = ok ] || fail "deposit"
}
fwallet() { ok "$(api "$1" GET /futures/account)" "futures account" | jq -r .walletBalance; }
pts() { ok "$(api "${TOK[$1]}" GET "/points/me")" "points me $1"; }

say "period 5 is over"
c5="$(curl -sS "$GATEWAY_URL/points/campaign?campaign=carnival-5")"
[ "$(jq -r '.status, (.rules.redeem|length)' <<<"$c5" | tr '\n' ' ')" = "ended 8 " ] || fail "period 5: $c5"
[ "$(jq -r '.dailyMaxVolumePoints' <<<"$c5")" = "168" ] || fail "daily max: $c5"
[ "$(curl -sS "$GATEWAY_URL/points/campaign" | jq -r .key)" = "carnival-5" ] || fail "current campaign should be the latest ended one"
reg U; reg Z; reg M
r="$(api "${TOK[U]}" POST /points/enroll '{"campaign":"carnival-5"}')"; [ "$(errcode "$r")" = CAMPAIGN_ENDED ] || fail "enroll ended: $r"
r="$(api "${TOK[U]}" POST /points/redeem '{"campaign":"carnival-5","points":10}')"; [ "$(errcode "$r")" = CAMPAIGN_ENDED ] || fail "redeem ended: $r"

say "admin schedules a period (2FA, overlap rejected)"
now_ms=$(( $(date +%s) * 1000 ))
[ "$(curl -sS -o /dev/null -w '%{http_code}' "$ADMIN_URL/admin/v1/points/campaigns")" = 401 ] || fail "admin campaigns without session"
mk() { A "$SA" POST /admin/v1/points/campaigns "{\"key\":\"$1\",\"title\":\"积分嘉年华 · 冒烟\",\"startsAt\":$2,\"endsAt\":$3,\"totpCode\":\"$4\"}"; }
r="$(mk "smoke-$TS" $((now_ms-60000)) $((now_ms+86400000)) 000000)"; [ "$(jq -r '.code // .error.code' <<<"$r")" != null ] || fail "bad 2FA accepted: $r"
r="$(mk "smoke-$TS" $((now_ms-60000)) $((now_ms+86400000)) "$(totp "$SA_TOTP")")"
[ "$(jq -r .status <<<"$r")" = active ] || fail "create: $r"
sleep 31 # next TOTP window
r="$(mk "smoke2-$TS" $((now_ms+3600000)) $((now_ms+7200000)) "$(totp "$SA_TOTP")")"
[ "$(jq -r '.code // .error.code' <<<"$r")" = CAMPAIGN_OVERLAP ] || fail "overlap: $r"
[ "$(A "$SA" GET /admin/v1/points/campaigns | jq -r '.items | length')" -ge 2 ] || fail "admin list"
[ "$(curl -sS "$GATEWAY_URL/points/campaign" | jq -r .key)" = "smoke-$TS" ] || fail "current campaign"

say "points only after enrollment"
internal "$FUTURES_URL" POST /internal/dev/index '{"symbol":"BTCUSDT","price":"50000"}' >/dev/null
credit "${UID_[M]}" USDT 2000000; credit "${UID_[U]}" USDT 60000
fut_in "${TOK[M]}" 2000000; fut_in "${TOK[U]}" 60000
fut_lev "${TOK[M]}" 20; fut_lev "${TOK[U]}" 20
fut_round "${TOK[M]}" "${TOK[U]}" 0.1   # 10k volume before enrolling: no points
deposit "${UID_[U]}" 500 pre
gsync
[ "$(pts U | jq -r '.enrolled, .balance' | tr '\n' ' ')" = "false 0 " ] || fail "unenrolled points"
ok "$(api "${TOK[U]}" POST /points/enroll '{}')" enroll >/dev/null
ok "$(api "${TOK[U]}" POST /points/enroll '{}')" "enroll twice" >/dev/null
CODE_U="$(ok "$(api "${TOK[U]}" GET /referral/me)" "referral me" | jq -r .code)"
reg V "$CODE_U"
sleep 1
fut_round "${TOK[M]}" "${TOK[U]}" 10    # 1,000,000 volume -> 1+1+3+3+5+8+12 = 33
deposit "${UID_[U]}" 900 a; deposit "${UID_[U]}" 600 b   # 1500 -> 15, capped 10
credit "${UID_[V]}" USDT 10000; fut_in "${TOK[V]}" 10000; fut_lev "${TOK[V]}" 20
fut_round "${TOK[M]}" "${TOK[V]}" 2     # 200k notional -> 100 fees: valid invitee -> 8
gsync
me="$(pts U)"
[ "$(jq -r '.enrolled, .balance, .today.volume.points, .today.deposit.points, .today.invite.points' <<<"$me" | tr '\n' ' ')" = "true 51 33 10 8 " ] || fail "points: $me"
eq "$(jq -r .today.volume.progress <<<"$me")" 1000000 "volume progress"
eq "$(jq -r .today.deposit.progress <<<"$me")" 1500 "deposit progress"
[ "$(pts M | jq -r .balance)" = 0 ] || fail "maker not enrolled but has points"

say "redeem rules"
red() { api "${TOK[$1]}" POST /points/redeem "{\"points\":$2}"; }
r="$(ok "$(red U 10)" "redeem 10")"; [ "$(jq -r '.kind, .amount, .balance' <<<"$r" | tr '\n' ' ')" = "deduction 3 41 " ] || fail "redeem 10: $r"
[ "$(errcode "$(red U 10)")" = ALREADY_REDEEMED ] || fail "tier twice"
[ "$(errcode "$(red U 15)")" = INVALID_TIER ] || fail "invalid tier"
[ "$(errcode "$(red U 50)")" = INSUFFICIENT_POINTS ] || fail "insufficient"
[ "$(errcode "$(red Z 10)")" = NOT_ENROLLED ] || fail "not enrolled"
r="$(ok "$(red U 35)" "redeem 35")"; [ "$(jq -r '.kind, .amount, .balance' <<<"$r" | tr '\n' ' ')" = "trial 10 6 " ] || fail "redeem 35: $r"
[ "$(pts U | jq -c .redeemedTiers)" = "[10,35]" ] || fail "redeemed tiers"
[ "$(ok "$(api "${TOK[U]}" GET /points/history)" history | jq '[.items[] | select(.kind=="redeem")] | length')" = 2 ] || fail "history"
[ "$(ok "$(api "${TOK[U]}" GET /points/redemptions)" redemptions | jq '.items | length')" = 2 ] || fail "redemptions"

say "coupon center + activation"
cs="$(ok "$(api "${TOK[U]}" GET /coupons)" coupons)"
[ "$(jq -r '[.items[] | select(.status=="issued")] | length' <<<"$cs")" = 2 ] || fail "coupons: $cs"
[ "$(ok "$(api "${TOK[U]}" GET /coupons/summary)" summary | jq -r .available)" = 2 ] || fail "summary"
TRIAL="$(jq -r '.items[] | select(.kind=="trial") | .id' <<<"$cs")"
DED="$(jq -r '.items[] | select(.kind=="deduction") | .id' <<<"$cs")"
avail="$(ok "$(api "${TOK[U]}" GET /futures/account)" acct | jq -r .availableBalance)"
out="$(python3 -c 'import sys,decimal as d; print(d.Decimal(sys.argv[1]).quantize(d.Decimal(1), rounding=d.ROUND_FLOOR))' "$avail")"
ok "$(api "${TOK[U]}" POST /futures/transfer "{\"direction\":\"out\",\"amount\":\"$out\"}")" "transfer out" >/dev/null
r="$(api "${TOK[U]}" POST "/coupons/$TRIAL/activate")"; [ "$(errcode "$r")" = INSUFFICIENT_OWN_FUNDS ] || fail "trial w/o own funds: $r"
fut_in "${TOK[U]}" 100
ok "$(api "${TOK[U]}" POST "/coupons/$TRIAL/activate")" "activate trial" >/dev/null
ok "$(api "${TOK[U]}" POST "/coupons/$DED/activate")" "activate deduction" >/dev/null
r="$(api "${TOK[U]}" POST "/coupons/$DED/activate")"; [ "$(errcode "$r")" = COUPON_NOT_ACTIVATABLE ] || fail "activate twice: $r"
r="$(api "${TOK[Z]}" POST "/coupons/$DED/activate")"; [ "$(errcode "$r")" = COUPON_NOT_FOUND ] || fail "other user's coupon: $r"
b="$(ok "$(api "${TOK[U]}" GET /futures/bonus)" bonus)"
eq "$(jq -r .available.trial <<<"$b")" 10 "trial available"; eq "$(jq -r .available.deduction <<<"$b")" 3 "deduction available"

say "bonus pays futures fees (deduction first)"
fut_in "${TOK[U]}" 2000
w0="$(fwallet "${TOK[U]}")"
fut_round "${TOK[M]}" "${TOK[U]}" 0.1   # fees 2 x 2.5 = 5 -> 3 deduction + 2 trial
w1="$(fwallet "${TOK[U]}")"
eq "$w1" "$w0" "wallet unchanged when the bonus covers all fees" 0.0001
b="$(ok "$(api "${TOK[U]}" GET /futures/bonus)" bonus)"
eq "$(jq -r .available.trial <<<"$b")" 8 "trial left"; eq "$(jq -r .available.deduction <<<"$b")" 0 "deduction used"
cs="$(ok "$(api "${TOK[U]}" GET /coupons)" coupons)"
eq "$(jq -r ".items[] | select(.id==$TRIAL) | .bonus.remaining" <<<"$cs")" 8 "coupon bonus remaining"
fut_round "${TOK[M]}" "${TOK[U]}" 0.4   # fees 20 > 8 trial -> 12 paid from own funds
w2="$(fwallet "${TOK[U]}")"
eq "$w2" "$(python3 -c "import sys; from decimal import Decimal as D; print(D('$w1')-12)")" "own funds pay the remainder" 0.0001
gsync
[ "$(A "$SA" GET "/admin/v1/users/${UID_[U]}/coupons" | jq '.items | length')" = 2 ] || fail "admin coupons"

printf '\n\033[1;32mSMOKE-POINTS PASSED\033[0m\n'
