#!/usr/bin/env bash
#
# End-to-end smoke for the newcomer gift package + invite KYC reward +
# activity center: KYC gate, single enrollment, bind / first-deposit /
# stacked volume tasks, claim errors, vouchers landing in the coupon center,
# invite reward for both sides once the invitee passes KYC, and the
# activity list showing the points period 5 as ended.
#
#   bash scripts/smoke-gift.sh            # boots devstack
#
set -euo pipefail
cd "$(dirname "$0")/.."

if [ "${SMOKE_INNER:-}" != "1" ]; then
  echo "== booting devstack =="
  ROOT="$(pwd)"
  cd tools/devstack
  exec go run . -root "$ROOT" -exec "SMOKE_INNER=1 bash scripts/smoke-gift.sh"
fi

GATEWAY_URL="${GATEWAY_URL:-http://localhost:8080}"
ASSET_URL="${ASSET_URL:-http://localhost:8082}"
ACCOUNT_URL="${ACCOUNT_URL:-http://localhost:8081}"
FUTURES_URL="${FUTURES_URL:-${FUTURES_INTERNAL_URL:-http://localhost:8089}}"
GROWTH_URL="${GROWTH_URL:-http://localhost:8091}"
ADMIN_URL="${ADMIN_URL:-http://localhost:8088}"
INTERNAL_TOKEN="${INTERNAL_TOKEN:-}"
PASSWORD="Str0ng-Passw0rd!"
TS="$(date +%s%N)"
SMOKE_NAME="${SMOKE_NAME:-GIFT}"
. scripts/lib-growth-smoke.sh

declare -A TOK UID_
reg() { read -r t s < <(register "gift-${1,,}-$TS@example.com" "${2:-}"); TOK[$1]="$t"; UID_[$1]="$s"; }
kyc() { internal "$ACCOUNT_URL" POST /internal/account/kyc-level "{\"uid\":\"${UID_[$1]}\",\"level\":${2:-1}}" >/dev/null; }
deposit() {
  [ "$(internal "$ASSET_URL" POST /internal/deposit "{\"userId\":\"$1\",\"asset\":\"USDT\",\"amount\":\"$2\",\"ref\":\"gift-dep-$TS-$3\"}" | jq -r .status)" = ok ] || fail "deposit"
}
nc() { ok "$(api "${TOK[$1]}" GET /rewards/newcomer)" "newcomer $1"; }
st() { nc "$1" | jq -r --arg k "$2" '.tasks[] | select(.key==$k) | .status'; }
claim() { api "${TOK[$1]}" POST "/rewards/newcomer/tasks/$2/claim"; }

say "activity center (public)"
acts="$(curl -sS "$GATEWAY_URL/activities")"
[ "$(jq -r '[.items[].key] | .[0:2] | join(",")' <<<"$acts")" = "newcomer,invite" ] || fail "activities: $acts"
[ "$(jq -r '.items[] | select(.key=="carnival-5") | .status' <<<"$acts")" = ended ] || fail "carnival-5 not ended: $acts"
[ "$(jq -r '.items[0].desc' <<<"$acts")" = "完成任务最高领取 220 USDT 奖励" ] || fail "newcomer desc: $acts"

say "KYC gate + single enrollment"
reg G; reg K
v="$(nc G)"
[ "$(jq -r '.enrolled, .kycOk, (.tasks|length), .days' <<<"$v" | tr '\n' ' ')" = "false false 8 7 " ] || fail "initial: $v"
[ "$(jq -r '[.tasks[].status]|unique|join(",")' <<<"$v")" = locked ] || fail "locked: $v"
r="$(api "${TOK[G]}" POST /rewards/newcomer/enroll)"; [ "$(errcode "$r")" = KYC_REQUIRED ] || fail "enroll w/o kyc: $r"
r="$(claim G bind)"; [ "$(errcode "$r")" = NOT_ENROLLED ] || fail "claim before enroll: $r"
kyc G 1
v="$(ok "$(api "${TOK[G]}" POST /rewards/newcomer/enroll)" enroll)"
[ "$(jq -r '.enrolled, .kycOk' <<<"$v" | tr '\n' ' ')" = "true true " ] || fail "enrolled: $v"
dl=$(( $(jq -r '.deadline - .enrolledAt' <<<"$v") / 3600000 )); [ "$dl" = 168 ] || fail "window $dl h"
r="$(api "${TOK[G]}" POST /rewards/newcomer/enroll)"; [ "$(errcode "$r")" = ALREADY_ENROLLED ] || fail "re-enroll: $r"

say "bind email + phone"
[ "$(st G bind)" = in_progress ] || fail "bind should be in progress"
r="$(claim G bind)"; [ "$(errcode "$r")" = TASK_NOT_COMPLETED ] || fail "bind early: $r"
internal "$ACCOUNT_URL" POST /internal/dev/account/bind-phone "{\"uid\":\"${UID_[G]}\",\"phone\":\"+1555${TS: -7}\"}" >/dev/null
[ "$(st G bind)" = claimable ] || fail "bind should be claimable"
c="$(ok "$(claim G bind)" "claim bind")"
[ "$(jq -r '.kind, .amount' <<<"$c" | tr '\n' ' ')" = "deduction 5 " ] || fail "bind reward: $c"
r="$(claim G bind)"; [ "$(errcode "$r")" = ALREADY_CLAIMED ] || fail "double claim: $r"
r="$(claim G nope)"; [ "$(errcode "$r")" = TASK_NOT_FOUND ] || fail "unknown task: $r"

say "first deposit (>= 50) then transfer to futures"
deposit "${UID_[G]}" 40 1; fut_in "${TOK[G]}" 10; gsync
[ "$(st G first_deposit)" = in_progress ] || fail "deposit 40 should not count"
deposit "${UID_[G]}" 5000 2; gsync
[ "$(st G first_deposit)" = in_progress ] || fail "needs a futures transfer after the deposit"
fut_in "${TOK[G]}" 4000; gsync
[ "$(st G first_deposit)" = claimable ] || fail "first deposit should be claimable"
[ "$(jq -r '.kind, .amount' <<<"$(ok "$(claim G first_deposit)" "claim dep")" | tr '\n' ' ')" = "trial 5 " ] || fail "dep reward"

say "volume levels stack"
internal "$FUTURES_URL" POST /internal/dev/index '{"symbol":"BTCUSDT","price":"50000"}' >/dev/null
credit "${UID_[K]}" USDT 20000; fut_in "${TOK[K]}" 20000
fut_lev "${TOK[G]}" 20; fut_lev "${TOK[K]}" 20
fut_round "${TOK[K]}" "${TOK[G]}" 0.1; gsync   # G: 10,000 notional
v="$(nc G)"
[ "$(jq -r '[.tasks[] | select(.type=="volume") | .status] | join(",")' <<<"$v")" = "claimable,in_progress,in_progress,in_progress,in_progress,in_progress" ] || fail "vol after 10k: $v"
r="$(claim G volume_2)"; [ "$(errcode "$r")" = TASK_NOT_COMPLETED ] || fail "vol2 early: $r"
fut_round "${TOK[K]}" "${TOK[G]}" 0.4; gsync   # G: 50,000
v="$(nc G)"
[ "$(jq -r '[.tasks[] | select(.type=="volume") | .status] | join(",")' <<<"$v")" = "claimable,claimable,in_progress,in_progress,in_progress,in_progress" ] || fail "vol after 50k: $v"
ok "$(claim G volume_1)" v1 >/dev/null; ok "$(claim G volume_2)" v2 >/dev/null
[ "$(st G volume_1),$(st G volume_2)" = "claimed,claimed" ] || fail "claimed"
cs="$(ok "$(api "${TOK[G]}" GET /coupons)" coupons)"
[ "$(jq -r '[.items[] | .kind + ":" + (.amount|tostring)] | sort | join(",")' <<<"$cs")" = "deduction:5,trial:10,trial:5,trial:5" ] || fail "coupons: $cs"
[ "$(jq -r '[.items[].status]|unique|join(",")' <<<"$cs")" = issued ] || fail "coupon status: $cs"
r="$(claim K volume_1)"; [ "$(errcode "$r")" = NOT_ENROLLED ] || fail "K not enrolled: $r"

say "invite reward: both sides once the invitee passes KYC"
reg I
CODE="$(ok "$(api "${TOK[I]}" GET /referral/me)" "ref me" | jq -r .code)"
reg J "$CODE"; gsync
cnt() { ok "$(api "${TOK[$1]}" GET /coupons)" c | jq -r '[.items[] | select(.title|test("邀请奖励"))] | length'; }
[ "$(cnt I),$(cnt J)" = "0,0" ] || fail "reward before kyc"
kyc J 1; gsync; gsync
[ "$(cnt I),$(cnt J)" = "1,1" ] || fail "invite reward: $(cnt I),$(cnt J)"
[ "$(ok "$(api "${TOK[J]}" GET /coupons)" c | jq -r '.items[0].kind, .items[0].amount' | tr '\n' ' ')" = "trial 20 " ] || fail "invite coupon"
gsync; [ "$(cnt I),$(cnt J)" = "1,1" ] || fail "invite reward not idempotent"

printf '\n\033[1;32mSMOKE-GIFT PASSED\033[0m\n'
