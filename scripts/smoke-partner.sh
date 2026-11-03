#!/usr/bin/env bash
#
# End-to-end smoke for the partner (agent) program and its plumbing:
#   invite-code attribution at registration, partner application with manual
#   admin review (RBAC + 2FA + audit), futures/spot fee capture from the
#   order/futures feeds, tier promotion by directs' 30-day futures volume,
#   tier commission + 10% platform-funded override (up to 2 levels above the direct partner), regular
#   referral (non-partner inviter, no stacking), daily settlement paid into
#   the funding account (idempotent), partner dashboards and gateway access.
#
#   bash scripts/smoke-partner.sh            # boots devstack
#   SMOKE_INNER=1 ... bash scripts/smoke-partner.sh   # existing stack
#
set -euo pipefail
cd "$(dirname "$0")/.."

if [ "${SMOKE_INNER:-}" != "1" ]; then
  echo "== booting devstack =="
  ROOT="$(pwd)"
  cd tools/devstack
  exec go run . -root "$ROOT" -exec "SMOKE_INNER=1 bash scripts/smoke-partner.sh"
fi

GATEWAY_URL="${GATEWAY_URL:-http://localhost:8080}"
ASSET_URL="${ASSET_URL:-http://localhost:8082}"
ACCOUNT_URL="${ACCOUNT_URL:-http://localhost:8081}"
ORDER_URL="${ORDER_URL:-http://localhost:8084}"
FUTURES_URL="${FUTURES_URL:-${FUTURES_INTERNAL_URL:-http://localhost:8089}}"
GROWTH_URL="${GROWTH_URL:-http://localhost:8091}"
ADMIN_URL="${ADMIN_URL:-http://localhost:8088}"
INTERNAL_TOKEN="${INTERNAL_TOKEN:-}"
SA_USER="${ADMIN_BOOTSTRAP_USERNAME:-}"
SA_PASS="${ADMIN_BOOTSTRAP_PASSWORD:-}"
SA_TOTP="${ADMIN_BOOTSTRAP_TOTP_SECRET:-}"
PASSWORD="Str0ng-Passw0rd!"
TS="$(date +%s%N)"
SMOKE_NAME="${SMOKE_NAME:-PARTNER}"
. scripts/lib-growth-smoke.sh
[ -n "$SA_USER" ] && [ -n "$SA_PASS" ] && [ -n "$SA_TOTP" ] || fail "ADMIN_BOOTSTRAP_* not set"

say "gateway access"
r="$(curl -sS -w '\n%{http_code}' "$GATEWAY_URL/partner/rules")"
[ "$(code "$r")" = "200" ] || fail "rules not public"
[ "$(body "$r" | jq -r '.tiers|length')" = "4" ] || fail "tiers: $r"
[ "$(body "$r" | jq -r '.tiers[3].key + " " + .tiers[3].futuresRate + " " + .tiers[3].spotRate')" = "diamond 0.5 0.35" ] || fail "diamond tier: $r"
[ "$(body "$r" | jq -r .overrideLevels)" = "3" ] || fail "override levels"
[ "$(body "$r" | jq -r .overrideDepth)" = "2" ] || fail "override depth"
# Rebate table by market: futures 30/40/45/50%, spot 20/25/30/35% (normal/silver/gold/diamond).
TIERS="$(body "$r" | jq -r '[.tiers[] | "\(.key):\(.futuresRate):\(.spotRate)"] | join(",")')"
[ "$TIERS" = "normal:0.3:0.2,silver:0.4:0.25,gold:0.45:0.3,diamond:0.5:0.35" ] || fail "tier rates (futures/spot swapped?): $TIERS"
[ "$(body "$r" | jq -r '[.tiers[] | (.futuresRate|tonumber) > (.spotRate|tonumber)] | all')" = "true" ] || fail "futures rate must exceed spot rate: $TIERS"
[ "$(curl -sS -o /dev/null -w '%{http_code}' "$GATEWAY_URL/partner/me")" = "401" ] || fail "partner/me without token"
[ "$(curl -sS -o /dev/null -w '%{http_code}' -X POST "$GROWTH_URL/internal/dev/growth/settle")" = "401" ] || fail "settle without token"

say "users + invite attribution"
declare -A TOK UID_ CODE
reg() { # reg <name> [inviter-name]
  local inv=""; [ -n "${2:-}" ] && inv="${CODE[$2]}"
  read -r t s < <(register "partner-${1,,}-$TS@example.com" "$inv")
  TOK[$1]="$t"; UID_[$1]="$s"
  CODE[$1]="$(ok "$(api "$t" GET /referral/me)" "referral/me $1" | jq -r .code)"
  [ -n "${CODE[$1]}" ] || fail "no invite code for $1"
}
reg M; reg PA; reg R
reg PB PA; reg C PB; reg D PA; reg E R
r="$(ok "$(api "${TOK[PB]}" GET /referral/me)" "PB me")"
[ "$(jq -r .invitedBy <<<"$r")" = "${UID_[PA]}" ] || fail "PB inviter: $r"
[ "$(ok "$(api "${TOK[R]}" GET /referral/me)" "R me" | jq -r .invited)" = "1" ] || fail "R invited count"
l="$(ok "$(api "${TOK[PA]}" GET /partner/links)" links)"
[[ "$(jq -r .url <<<"$l")" == *"/r/${CODE[PA]}" ]] || fail "link: $l"

say "partner application + admin review"
r="$(api "${TOK[PA]}" POST /partner/apply '{"contact":""}')"
[ "$(errcode "$r")" = "INVALID_APPLICATION" ] || fail "empty contact: $r"
r="$(ok "$(api "${TOK[PA]}" POST /partner/apply '{"contact":"tg:@pa","channel":"telegram","scale":"1000+"}')" "PA apply")"
[ "$(jq -r .status <<<"$r")" = "pending" ] || fail "PA should be pending: $r"
[ "$(errcode "$(api "${TOK[PA]}" POST /partner/apply '{"contact":"x"}')")" = "PARTNER_EXISTS" ] || fail "duplicate apply"
ok "$(api "${TOK[PB]}" POST /partner/apply '{"contact":"tg:@pb"}')" "PB apply" >/dev/null
ok "$(api "${TOK[R]}" POST /partner/apply '{"contact":"tg:@r"}')" "R apply" >/dev/null
[ "$(errcode "$(api "${TOK[PA]}" GET /partner/summary)")" = "NOT_PARTNER" ] || fail "pending partner sees summary"

SA="$(alogin "$SA_USER" "$SA_PASS" "$SA_TOTP")"; [ -n "$SA" ] || fail "admin login"
[ "$(curl -sS -o /dev/null -w '%{http_code}' "$ADMIN_URL/admin/v1/partners")" = "401" ] || fail "admin list without session"
L="$(A "$SA" GET "/admin/v1/partners?status=pending")"
[ "$(jq --arg u "${UID_[PA]}" '[.items[]|select(.uid==$u)]|length' <<<"$L")" = "1" ] || fail "PA not in pending list: $L"
r="$(A "$SA" POST "/admin/v1/partners/${UID_[R]}/reject" "{\"totpCode\":\"$(totp "$SA_TOTP")\"}")"
[ "$(jq -r .code <<<"$r")" = "REASON_REQUIRED" ] || fail "reject w/o reason: $r"
r="$(A "$SA" POST "/admin/v1/partners/${UID_[R]}/reject" '{"reason":"x","totpCode":"000000"}')"
[ "$(jq -r .code <<<"$r")" = "TOTP_INVALID" ] || fail "reject w/o 2FA: $r"
r="$(A "$SA" POST "/admin/v1/partners/${UID_[R]}/reject" "{\"reason\":\"渠道资料不足\",\"totpCode\":\"$(totp "$SA_TOTP")\"}")"
[ "$(jq -r .status <<<"$r")" = "rejected" ] || fail "reject R: $r"
[ "$(ok "$(api "${TOK[R]}" GET /partner/me)" "R me" | jq -r .partner.reviewNote)" = "渠道资料不足" ] || fail "reason not visible"
for u in PA PB; do
  r="$(A "$SA" POST "/admin/v1/partners/${UID_[$u]}/approve" "{\"totpCode\":\"$(totp "$SA_TOTP")\"}")"
  [ "$(jq -r .status <<<"$r")" = "active" ] || fail "approve $u: $r"
done
r="$(A "$SA" POST "/admin/v1/partners/${UID_[PA]}/approve" "{\"totpCode\":\"$(totp "$SA_TOTP")\"}")"
[ "$(jq -r .code <<<"$r")" = "NOT_PENDING" ] || fail "double approve: $r"
AU="$(A "$SA" GET "/admin/v1/audit-logs?module=partner")"
[ "$(jq '[.items[]?] | length' <<<"$AU")" -ge 3 ] || fail "audit entries: $AU"
[ "$(errcode "$(api "${TOK[C]}" GET /partner/summary)")" = "NOT_PARTNER" ] || fail "non-partner summary"

say "trading (futures + spot)"
internal "$FUTURES_URL" POST /internal/dev/index '{"symbol":"BTCUSDT","price":"50000"}' >/dev/null
for u in M:3000000 D:1000000 C:200000 E:200000; do
  credit "${UID_[${u%%:*}]}" USDT "${u##*:}"
  fut_in "${TOK[${u%%:*}]}" "$(( ${u##*:} / 2 ))"
  fut_lev "${TOK[${u%%:*}]}" 10
done
fut_round "${TOK[M]}" "${TOK[D]}" 60   # D: 6,000,000 notional -> PA silver
fut_round "${TOK[M]}" "${TOK[C]}" 1
fut_round "${TOK[M]}" "${TOK[E]}" 1
# spot: E sells (maker, fee in USDT), C buys (taker, fee in BTC -> valued at price)
credit "${UID_[E]}" BTC 1
r="$(curl -sS -X POST "$GATEWAY_URL/spot/orders" -H 'Content-Type: application/json' -H "Authorization: Bearer ${TOK[E]}" \
  -d '{"symbol":"BTCUSDT","side":"sell","type":"limit","price":"60000","quantity":"0.5","timeInForce":"GTC"}')"
[ "$(jq -r .status <<<"$r")" = "new" ] || fail "spot sell: $r"
r="$(curl -sS -X POST "$GATEWAY_URL/spot/orders" -H 'Content-Type: application/json' -H "Authorization: Bearer ${TOK[C]}" \
  -d '{"symbol":"BTCUSDT","side":"buy","type":"limit","price":"60000","quantity":"0.5","timeInForce":"GTC"}')"
[ "$(jq -r .status <<<"$r")" = "filled" ] || fail "spot buy: $r"
sleep 1

say "expected commissions (from the source feeds)"
FF="$(internal "$FUTURES_URL" GET "/internal/trades/feed?after=0&limit=1000")"
SF="$(internal "$ORDER_URL" GET "/internal/trades/feed?after=0&limit=1000")"
EXP="$(python3 - "${UID_[C]}" "${UID_[D]}" "${UID_[E]}" "$FF" "$SF" <<'PY'
import json, sys
from decimal import Decimal as D
c, d, e = sys.argv[1:4]
ff, sf = json.loads(sys.argv[4])["items"], json.loads(sys.argv[5])["items"]
def fut(u, k="fee"): return sum((D(i[k]) for i in ff if i["userId"] == u), D(0))
def spot(u):
    t = D(0)
    for i in sf:
        if i["userId"] != u: continue
        f = D(i["fee"])
        t += f * D(i["price"]) if i["feeAsset"] == i["base"] else f
    return t
vol = fut(d, "notional")
rateD = D("0.40") if vol >= D(5000000) else D("0.30")
pb = fut(c) * D("0.30") + spot(c) * D("0.20")
pa = fut(d) * rateD + pb * D("0.10")
r = (fut(e) + spot(e)) * D("0.10")
q = lambda x: x.quantize(D("0.00000001"), rounding="ROUND_DOWN")
print(q(pa), q(pb), q(r), vol, fut(c) > 0 and spot(c) > 0 and spot(e) > 0)
PY
)"
read -r EXP_PA EXP_PB EXP_R VOL_D ALLFEES <<<"$EXP"
echo "PA=$EXP_PA PB=$EXP_PB R=$EXP_R volD=$VOL_D"
[ "$ALLFEES" = "True" ] || fail "missing fee events"
eq "$VOL_D" 6000000 "D notional" 1

say "unsettled estimate + settlement"
gsync
est="$(ok "$(api "${TOK[PB]}" GET /partner/summary)" "PB summary" | jq -r .unsettledEstimate)"
eq "$est" "$EXP_PB" "PB unsettled estimate"
declare -A BEFORE
for u in PA PB R; do BEFORE[$u]="$(spot_bal "${TOK[$u]}")"; done
S="$(gsettle)"
[ "$(jq -r '.settlementId > 0' <<<"$S")" = "true" ] || fail "settle: $S"
[ "$(jq -r .paid <<<"$S")" -ge 3 ] || fail "payouts: $S"
gain() { python3 -c 'import sys; from decimal import Decimal as D; print(D(sys.argv[1])-D(sys.argv[2]))' "$(spot_bal "${TOK[$1]}")" "${BEFORE[$1]}"; }
eq "$(gain PA)" "$EXP_PA" "PA payout (silver 40% + override)"
eq "$(gain PB)" "$EXP_PB" "PB payout (normal 30%/20%)"
eq "$(gain R)" "$EXP_R" "R referral payout (10%)"

say "idempotent settlement"
S2="$(gsettle)"
[ "$(jq -r .paid <<<"$S2")" = "0" ] || fail "second settle paid again: $S2"
eq "$(gain PA)" "$EXP_PA" "PA after re-settle"

say "dashboards"
s="$(ok "$(api "${TOK[PA]}" GET /partner/summary)" "PA summary")"
[ "$(jq -r .tier <<<"$s")" = "silver" ] || fail "PA tier: $s"
[ "$(jq -r '"\(.futuresRate) \(.spotRate)"' <<<"$s")" = "0.4 0.25" ] || fail "silver rates (futures 40% / spot 25%): $s"
[ "$(jq -r '.directs, .directPartners' <<<"$s" | tr '\n' ' ')" = "2 1 " ] || fail "PA directs: $s"
eq "$(jq -r .total <<<"$s")" "$EXP_PA" "PA total" 0.00000002
[ "$(jq -r .nextTier.tier <<<"$s")" = "gold" ] || fail "next tier: $s"
c="$(ok "$(api "${TOK[PA]}" GET "/partner/commissions?kind=override")" "PA override list")"
[ "$(jq -r '.items[0].sourceUid' <<<"$c")" = "${UID_[PB]}" ] || fail "override source: $c"
[ "$(errcode "$(api "${TOK[PA]}" GET "/partner/commissions?kind=bogus")")" = "INVALID_KIND" ] || fail "bad kind"
d="$(ok "$(api "${TOK[PA]}" GET /partner/commissions/daily)" daily)"
eq "$(jq -r '[.items[].total|tonumber]|add' <<<"$d")" "$EXP_PA" "daily sum" 0.00000005
t="$(ok "$(api "${TOK[PA]}" GET "/partner/team?type=partner")" team)"
[ "$(jq -r '.total, .items[0].uid' <<<"$t" | tr '\n' ' ')" = "1 ${UID_[PB]} " ] || fail "team partners: $t"
t="$(ok "$(api "${TOK[PA]}" GET /partner/team)" team)"
[ "$(jq -r .total <<<"$t")" = "2" ] || fail "team all: $t"
rf="$(ok "$(api "${TOK[R]}" GET /referral/rewards)" "R rewards")"
[ "$(jq -r '.items[0].kind' <<<"$rf")" = "referral" ] || fail "R rewards: $rf"
ri="$(ok "$(api "${TOK[R]}" GET /referral/invitees)" "R invitees")"
[ "$(jq -r '.items[0].active' <<<"$ri")" = "true" ] || fail "invitee window: $ri"
# a partner's own commission never comes from referral (no stacking)
[ "$(ok "$(api "${TOK[PA]}" GET "/partner/commissions?kind=referral")" x | jq -r .total)" = "0" ] || fail "partner got referral"

printf '\n\033[1;32mSMOKE-PARTNER PASSED\033[0m\n'
