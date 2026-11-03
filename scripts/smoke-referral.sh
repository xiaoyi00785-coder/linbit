#!/usr/bin/env bash
#
# End-to-end smoke for the regular referral (invite) program:
#   code attribution (case-insensitive, unknown code = nobody), 10% of the
#   invitee's fees incl. copy-trading sub-account fees, copy profit share
#   excluded, the 1-year window, no stacking once the inviter becomes a
#   partner, invitee/reward views and the admin referral lookup.
#
#   bash scripts/smoke-referral.sh            # boots devstack
#   SMOKE_INNER=1 ... bash scripts/smoke-referral.sh   # existing stack
#
set -euo pipefail
cd "$(dirname "$0")/.."

if [ "${SMOKE_INNER:-}" != "1" ]; then
  echo "== booting devstack =="
  ROOT="$(pwd)"
  cd tools/devstack
  exec go run . -root "$ROOT" -exec "SMOKE_INNER=1 bash scripts/smoke-referral.sh"
fi

GATEWAY_URL="${GATEWAY_URL:-http://localhost:8080}"
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
SMOKE_NAME="${SMOKE_NAME:-REFERRAL}"
. scripts/lib-growth-smoke.sh
[ -n "$SA_USER" ] && [ -n "$SA_PASS" ] && [ -n "$SA_TOTP" ] || fail "ADMIN_BOOTSTRAP_* not set"
SA="$(alogin "$SA_USER" "$SA_PASS" "$SA_TOTP")"; [ -n "$SA" ] || fail "admin login"

say "attribution"
declare -A TOK UID_ CODE
reg() { # reg <name> [invite-code]
  read -r t s < <(register "referral-${1,,}-$TS@example.com" "${2:-}")
  TOK[$1]="$t"; UID_[$1]="$s"
  CODE[$1]="$(ok "$(api "$t" GET /referral/me)" "referral/me $1" | jq -r .code)"
}
reg M; reg R
reg T "${CODE[R]}"; reg F "${CODE[R],,}"; reg X " ${CODE[R]} "; reg N "ZZZZZZZZ"
gsync
for u in T F X; do
  [ "$(ok "$(api "${TOK[$u]}" GET /referral/me)" "$u me" | jq -r .invitedBy)" = "${UID_[R]}" ] || fail "$u not attributed to R"
done
[ "$(ok "$(api "${TOK[N]}" GET /referral/me)" "N me" | jq -r '.invitedBy // ""')" = "" ] || fail "unknown code attributed"
me="$(ok "$(api "${TOK[R]}" GET /referral/me)" "R me")"
[ "$(jq -r '.invited, .rate, .validDays' <<<"$me" | tr '\n' ' ')" = "3 0.1 365 " ] || fail "R me: $me"

say "funding + copy setup (R invites trader T and follower F)"
internal "$FUTURES_URL" POST /internal/dev/index '{"symbol":"BTCUSDT","price":"50000"}' >/dev/null
for u in M:2000000 T:20000 F:2000 X:20000; do
  credit "${UID_[${u%%:*}]}" USDT "${u##*:}"
  fut_in "${TOK[${u%%:*}]}" "$(( ${u##*:} / 2 ))"
done
fut_lev "${TOK[T]}" 20; fut_lev "${TOK[X]}" 10
ok "$(api "${TOK[T]}" POST /futures/copy/trader '{"nickname":"RefAlpha","profitShare":"0.3"}')" "T apply" >/dev/null
r="$(A "$SA" POST "/admin/v1/copy/applications/${UID_[T]}/approve" "{\"totpCode\":\"$(totp "$SA_TOTP")\"}")"
[ "$(jq -r .status <<<"$r")" = "active" ] || fail "approve T: $r"
ok "$(api "${TOK[F]}" POST /futures/copy/follows "{\"traderId\":\"${UID_[T]}\",\"amount\":\"1000\",\"mode\":\"ratio\",\"ratio\":\"1\"}")" "F follow" >/dev/null

say "X registered 366 days ago -> outside the window"
r="$(internal "$GROWTH_URL" POST /internal/dev/growth/backdate "{\"uid\":\"${UID_[X]}\",\"days\":366}")"
[ "$(jq -r .updated <<<"$r")" = "1" ] || fail "backdate: $r"

say "trading: T opens/closes with profit (F mirrors), X trades"
fut_order "${TOK[M]}" '{"symbol":"BTCUSDT","side":"sell","type":"limit","price":"50000","qty":"0.3"}'
fut_order "${TOK[T]}" '{"symbol":"BTCUSDT","side":"buy","type":"market","qty":"0.2"}'
internal "$FUTURES_URL" POST /internal/dev/index '{"symbol":"BTCUSDT","price":"51000"}' >/dev/null
fut_order "${TOK[M]}" '{"symbol":"BTCUSDT","side":"buy","type":"limit","price":"51000","qty":"0.3"}'
ok "$(api "${TOK[T]}" POST /futures/positions/BTCUSDT/close '')" "T close" >/dev/null
FV="$(ok "$(api "${TOK[F]}" GET /futures/copy/follows)" "F follows" | jq -c '.follows[0]')"
[ "$(jq '.positions|length' <<<"$FV")" = "0" ] || fail "F still open: $FV"
lt0() { python3 -c 'import sys; from decimal import Decimal as D; sys.exit(0 if D(sys.argv[1])>0 else 1)' "$1" || fail "$2: $1"; }
lt0 "$(jq -r .realizedNet <<<"$FV")" "F realized"
TS0="$(spot_bal "${TOK[T]}")"
internal "$FUTURES_URL" POST /internal/dev/copy/settle "{\"period\":\"$(date -u +%F)\"}" >/dev/null
lt0 "$(python3 -c 'import sys; from decimal import Decimal as D; print(D(sys.argv[1])-D(sys.argv[2]))' "$(spot_bal "${TOK[T]}")" "$TS0")" "trader profit share"
internal "$FUTURES_URL" POST /internal/dev/index '{"symbol":"BTCUSDT","price":"50000"}' >/dev/null
fut_round "${TOK[M]}" "${TOK[X]}" 1
sleep 1

say "expected: 10% of T + F (incl. copy) fees, nothing for X or the profit share"
FF="$(internal "$FUTURES_URL" GET "/internal/trades/feed?after=0&limit=1000")"
NEXT="$(jq -r .next <<<"$FF")"
read -r EXP_R COPYF XF <<<"$(python3 - "${UID_[T]}" "${UID_[F]}" "${UID_[X]}" "$FF" <<'PY'
import json, sys
from decimal import Decimal as D
t, f, x = sys.argv[1:4]
ff = json.loads(sys.argv[4])["items"]
fee = lambda u, c=None: sum((D(i["fee"]) - D(i["bonusFee"]) for i in ff if i["userId"] == u and (c is None or i["copy"] == c)), D(0))
print((fee(t) + fee(f)) * D("0.10"), fee(f, True), fee(x))
PY
)"
echo "R=$EXP_R copyFeesF=$COPYF feesX=$XF"
lt0 "$COPYF" "copy sub-account fees"
lt0 "$XF" "X fees"
R0="$(spot_bal "${TOK[R]}")"
S="$(gsettle)"; [ "$(jq -r '.settlementId > 0' <<<"$S")" = "true" ] || fail "settle: $S"
GAIN="$(python3 -c 'import sys; from decimal import Decimal as D; print(D(sys.argv[1])-D(sys.argv[2]))' "$(spot_bal "${TOK[R]}")" "$R0")"
eq "$GAIN" "$EXP_R" "R referral payout" 0.00000002

say "views"
ri="$(ok "$(api "${TOK[R]}" GET "/referral/invitees?pageSize=50")" "R invitees")"
[ "$(jq '.items|length' <<<"$ri")" = "3" ] || fail "invitees: $ri"
[ "$(jq '[.items[]|select(.active==false)]|length' <<<"$ri")" = "1" ] || fail "expired invitee: $ri"
[ "$(jq -r --arg u "${UID_[T]}" '[.items[].uid|select(.==$u)]|length' <<<"$ri")" = "0" ] || fail "uid not masked: $ri"
rw="$(ok "$(api "${TOK[R]}" GET /referral/rewards)" "R rewards")"
[ "$(jq -r '[.items[].kind]|unique|join(",")' <<<"$rw")" = "referral" ] || fail "rewards: $rw"
[ "$(jq -r --arg x "${UID_[X]}" '[.items[]|select(.sourceUid==$x)]|length' <<<"$rw")" = "0" ] || fail "X earned R a reward: $rw"
eq "$(ok "$(api "${TOK[R]}" GET /referral/me)" me | jq -r .totalRewards)" "$EXP_R" "totalRewards" 0.00000002
[ "$(curl -sS -o /dev/null -w '%{http_code}' "$ADMIN_URL/admin/v1/referrals/${UID_[T]}")" = "401" ] || fail "admin lookup w/o session"
L="$(A "$SA" GET "/admin/v1/referrals/${UID_[R]}")"
[ "$(jq -r .invited <<<"$L")" = "3" ] || fail "admin lookup: $L"
eq "$(jq -r .commissionTotals.referral <<<"$L")" "$EXP_R" "admin referral total" 0.00000002
[ "$(jq -r .inviter <<<"$(A "$SA" GET "/admin/v1/referrals/${UID_[T]}")")" = "${UID_[R]}" ] || fail "admin inviter"

say "R becomes a partner -> partner rates replace referral (no stacking)"
ok "$(api "${TOK[R]}" POST /partner/apply '{"contact":"tg:@r"}')" "R apply" >/dev/null
r="$(A "$SA" POST "/admin/v1/partners/${UID_[R]}/approve" "{\"totpCode\":\"$(totp "$SA_TOTP")\"}")"
[ "$(jq -r .status <<<"$r")" = "active" ] || fail "approve R: $r"
fut_lev "${TOK[T]}" 10
fut_round "${TOK[M]}" "${TOK[T]}" 1
sleep 1
FF2="$(internal "$FUTURES_URL" GET "/internal/trades/feed?after=$NEXT&limit=1000")"
EXP_P="$(python3 - "${UID_[T]}" "$FF2" <<'PY'
import json, sys
from decimal import Decimal as D
print(sum((D(i["fee"]) - D(i["bonusFee"]) for i in json.loads(sys.argv[2])["items"] if i["userId"] == sys.argv[1]), D(0)) * D("0.30"))
PY
)"
R1="$(spot_bal "${TOK[R]}")"
gsettle >/dev/null
GAIN="$(python3 -c 'import sys; from decimal import Decimal as D; print(D(sys.argv[1])-D(sys.argv[2]))' "$(spot_bal "${TOK[R]}")" "$R1")"
eq "$GAIN" "$EXP_P" "R partner payout (30%, no referral on top)" 0.00000002
rw2="$(ok "$(api "${TOK[R]}" GET /referral/rewards)" "R rewards")"
[ "$(jq '.items|length' <<<"$rw2")" = "$(jq '.items|length' <<<"$rw")" ] || fail "referral rows grew after partner approval: $rw2"

printf '\n\033[1;32mSMOKE-REFERRAL PASSED\033[0m\n'
