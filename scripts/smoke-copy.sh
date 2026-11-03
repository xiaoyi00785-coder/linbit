#!/usr/bin/env bash
#
# End-to-end smoke for futures copy trading: trader application (0–50% share),
#   follow (ratio / fixed with per-trade cap, TP/SL), synchronized open,
#   partial and full close, daily profit-share settlement (idempotent, paid
#   to the trader's spot account), leaderboard / stats, unfollow, trader close,
#   conservation audit and gateway access.
#
#   bash scripts/smoke-copy.sh                       # boots devstack
#   GATEWAY_URL=... ASSET_URL=... ACCOUNT_URL=... FUTURES_URL=... INTERNAL_TOKEN=... \
#     SMOKE_INNER=1 bash scripts/smoke-copy.sh       # existing stack
#
set -euo pipefail
cd "$(dirname "$0")/.."

if [ "${SMOKE_INNER:-}" != "1" ]; then
  echo "== booting devstack =="
  ROOT="$(pwd)"
  cd tools/devstack
  exec go run . -root "$ROOT" -exec "SMOKE_INNER=1 bash scripts/smoke-copy.sh"
fi

GATEWAY_URL="${GATEWAY_URL:-http://localhost:8080}"
ASSET_URL="${ASSET_URL:-http://localhost:8082}"
ACCOUNT_URL="${ACCOUNT_URL:-http://localhost:8081}"
FUTURES_URL="${FUTURES_URL:-${FUTURES_INTERNAL_URL:-http://localhost:8089}}"
INTERNAL_TOKEN="${INTERNAL_TOKEN:-}"
ADMIN_URL="${ADMIN_URL:-http://localhost:8088}"
SA_USER="${ADMIN_BOOTSTRAP_USERNAME:-}"
SA_PASS="${ADMIN_BOOTSTRAP_PASSWORD:-}"
SA_TOTP="${ADMIN_BOOTSTRAP_TOTP_SECRET:-}"
PASSWORD="Str0ng-Passw0rd!"
TS="$(date +%s%N)"

say()  { printf '\n\033[1;34m== %s ==\033[0m\n' "$1"; }
fail() { printf '\033[1;31mSMOKE-COPY FAILED: %s\033[0m\n' "$1" >&2; exit 1; }
command -v jq >/dev/null 2>&1 || fail "jq is required"
command -v python3 >/dev/null 2>&1 || fail "python3 is required"

[ -n "$SA_USER" ] && [ -n "$SA_PASS" ] && [ -n "$SA_TOTP" ] || fail "ADMIN_BOOTSTRAP_* not set"
totp() {
  python3 - "$1" <<'PY'
import base64, hmac, struct, sys, time
s = sys.argv[1].strip().upper(); s += "=" * (-len(s) % 8)
k = base64.b32decode(s); c = struct.pack(">Q", int(time.time()) // 30)
h = hmac.new(k, c, "sha1").digest(); o = h[-1] & 15
print("%06d" % ((struct.unpack(">I", h[o:o+4])[0] & 0x7fffffff) % 1000000))
PY
}
A() { curl -sS -X "$2" "$ADMIN_URL$3" -H 'Content-Type: application/json' -H "Authorization: Bearer $1" ${4:+-d "$4"}; }
alogin() {
  curl -sS -X POST "$ADMIN_URL/admin/v1/auth/login" -H 'Content-Type: application/json' \
    -d "{\"username\":\"$1\",\"password\":\"$2\",\"totpCode\":\"$(totp "$3")\"}" | jq -r '.token // empty'
}

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
    -H "X-Internal-Token: $INTERNAL_TOKEN" -d "{\"userId\":\"$1\",\"asset\":\"USDT\",\"amount\":\"$2\",\"ref\":\"smoke-copy-$1\"}")"
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
[ "$(curl -sS -o /dev/null -w '%{http_code}' "$GATEWAY_URL/futures/copy/summary")" = "200" ] || fail "summary not public"
[ "$(curl -sS -o /dev/null -w '%{http_code}' "$GATEWAY_URL/futures/copy/traders")" = "200" ] || fail "leaderboard not public"
[ "$(curl -sS -o /dev/null -w '%{http_code}' "$GATEWAY_URL/futures/copy/follows")" = "401" ] || fail "follows open without token"
[ "$(curl -sS -o /dev/null -w '%{http_code}' -X POST "$FUTURES_URL/internal/dev/copy/settle")" = "401" ] || fail "settle without token"

say "users"
declare -A TOK UID_
for u in T F G M N P; do
  read -r t s < <(register "copy-${u,,}-$TS@example.com")
  TOK[$u]="$t"; UID_[$u]="$s"
  credit "$s" 100000
done
for u in T:10000 F:1000 G:1000 M:20000 N:20000 P:1000; do
  ok "$(api "${TOK[${u%%:*}]}" POST /futures/transfer "{\"direction\":\"in\",\"amount\":\"${u##*:}\"}")" "transfer $u" >/dev/null
done
dev POST /internal/dev/index '{"symbol":"BTCUSDT","price":"50000"}' >/dev/null

say "trader application"
r="$(api "${TOK[T]}" POST /futures/copy/trader '{"nickname":"Alpha","profitShare":"0.6"}')"
[ "$(errcode "$r")" = "INVALID_PROFIT_SHARE" ] || fail "share 60% accepted: $r"
r="$(api "${TOK[P]}" POST /futures/copy/trader '{"nickname":"Poor","profitShare":"0.1"}')"
ok "$r" "P apply (1000 equity >= 100)" >/dev/null
r="$(ok "$(api "${TOK[T]}" POST /futures/copy/trader '{"nickname":"Alpha","bio":"BTC trend","profitShare":"0.3"}')" "T apply")"
[ "$(jq -r .status <<<"$r")" = "pending" ] || fail "application should wait for review: $r"
r="$(api "${TOK[F]}" POST /futures/copy/follows "{\"traderId\":\"${UID_[T]}\",\"amount\":\"100\"}")"
[ "$(errcode "$r")" = "TRADER_NOT_ACTIVE" ] || fail "follow pending trader: $r"

say "admin review (RBAC + 2FA + audit)"
SA="$(alogin "$SA_USER" "$SA_PASS" "$SA_TOTP")"; [ -n "$SA" ] || fail "admin login"
[ "$(curl -sS -o /dev/null -w '%{http_code}' "$ADMIN_URL/admin/v1/copy/applications")" = "401" ] || fail "applications open without session"
L="$(A "$SA" GET "/admin/v1/copy/applications?status=pending")"
[ "$(jq --arg t "${UID_[T]}" '[.items[]|select(.traderId==$t)]|length' <<<"$L")" = "1" ] || fail "T not in pending list: $L"
r="$(A "$SA" POST "/admin/v1/copy/applications/${UID_[P]}/reject" "{\"totpCode\":\"$(totp "$SA_TOTP")\"}")"
[ "$(jq -r .code <<<"$r")" = "REASON_REQUIRED" ] || fail "reject without reason: $r"
r="$(A "$SA" POST "/admin/v1/copy/applications/${UID_[P]}/reject" '{"reason":"x","totpCode":"000000"}')"
[ "$(jq -r .code <<<"$r")" = "TOTP_INVALID" ] || fail "reject without 2FA: $r"
r="$(A "$SA" POST "/admin/v1/copy/applications/${UID_[P]}/reject" "{\"reason\":\"带单经验不足\",\"totpCode\":\"$(totp "$SA_TOTP")\"}")"
[ "$(jq -r .status <<<"$r")" = "rejected" ] || fail "reject: $r"
[ "$(ok "$(api "${TOK[P]}" GET /futures/copy/trader)" "P trader" | jq -r .reviewNote)" = "带单经验不足" ] || fail "reject reason not visible to applicant"
r="$(A "$SA" POST "/admin/v1/copy/applications/${UID_[P]}/approve" "{\"totpCode\":\"$(totp "$SA_TOTP")\"}")"
[ "$(jq -r .code <<<"$r")" = "NOT_PENDING" ] || fail "approve rejected app: $r"
r="$(ok "$(api "${TOK[P]}" POST /futures/copy/trader '{"nickname":"Poor2","profitShare":"0.1"}')" "P re-apply after rejection")"
[ "$(jq -r .status <<<"$r")" = "pending" ] || fail "re-apply status: $r"
r="$(A "$SA" POST "/admin/v1/copy/applications/${UID_[T]}/approve" "{\"totpCode\":\"$(totp "$SA_TOTP")\"}")"
[ "$(jq -r .status <<<"$r")" = "active" ] || fail "approve T: $r"
AU="$(A "$SA" GET "/admin/v1/audit-logs?module=copy")"
[ "$(jq '[.items[]|select(.module=="copy")]|length' <<<"$AU")" -ge 2 ] || fail "copy review not audited: $AU"
echo "  ok  reject needs reason + 2FA, applicant sees reason, re-apply, approve, audited"
r="$(ok "$(api "${TOK[T]}" PATCH /futures/copy/trader '{"profitShare":"0.2"}')" "T patch")"
eq "$(jq -r .profitShare <<<"$r")" 0.2 "share after patch"
r="$(api "${TOK[T]}" POST /futures/copy/trader '{"nickname":"Again"}')"
[ "$(errcode "$r")" = "ALREADY_TRADER" ] || fail "double apply: $r"

say "follow validation"
r="$(api "${TOK[T]}" POST /futures/copy/follows "{\"traderId\":\"${UID_[T]}\",\"amount\":\"100\"}")"
[ "$(errcode "$r")" = "CANNOT_FOLLOW_SELF" ] || fail "self follow: $r"
r="$(api "${TOK[F]}" POST /futures/copy/follows "{\"traderId\":\"${UID_[T]}\",\"amount\":\"10\"}")"
[ "$(errcode "$r")" = "INVALID_AMOUNT" ] || fail "below 50: $r"
r="$(api "${TOK[F]}" POST /futures/copy/follows "{\"traderId\":\"${UID_[T]}\",\"amount\":\"5000\"}")"
[ "$(errcode "$r")" = "INSUFFICIENT_BALANCE" ] || fail "over balance: $r"
r="$(api "${TOK[G]}" POST /futures/copy/follows "{\"traderId\":\"${UID_[T]}\",\"amount\":\"500\",\"mode\":\"fixed\"}")"
[ "$(errcode "$r")" = "INVALID_FIXED_MARGIN" ] || fail "fixed without margin: $r"

say "follow: F ratio 1 x 1000, G fixed 100 capped 50, TP 50% SL 20%"
r="$(ok "$(api "${TOK[F]}" POST /futures/copy/follows "{\"traderId\":\"${UID_[T]}\",\"amount\":\"1000\",\"mode\":\"ratio\",\"ratio\":\"1\"}")" "F follow")"
FID="$(jq -r .follow.id <<<"$r")"
eq "$(jq -r .equity <<<"$r")" 1000 "F sub equity"
eq "$(bal "${TOK[F]}")" 0 "F futures wallet after follow"
r="$(api "${TOK[F]}" POST /futures/copy/follows "{\"traderId\":\"${UID_[T]}\",\"amount\":\"50\"}")"
[ "$(errcode "$r")" = "ALREADY_FOLLOWING" ] || [ "$(errcode "$r")" = "INSUFFICIENT_BALANCE" ] || fail "double follow: $r"
r="$(ok "$(api "${TOK[G]}" POST /futures/copy/follows "{\"traderId\":\"${UID_[T]}\",\"amount\":\"500\",\"mode\":\"fixed\",\"fixedMargin\":\"100\",\"maxPerTrade\":\"50\",\"tpRatio\":\"0.5\",\"slRatio\":\"0.2\"}")" "G follow")"
GID="$(jq -r .follow.id <<<"$r")"
eq "$(bal "${TOK[G]}")" 500 "G futures wallet after follow"
r="$(api "${TOK[G]}" POST /futures/copy/follows "{\"traderId\":\"${UID_[T]}\",\"amount\":\"50\"}")"
[ "$(errcode "$r")" = "ALREADY_FOLLOWING" ] || fail "double follow: $r"

say "trader opens long 0.2 @50000 (20x cross) -> followers mirror"
order "${TOK[M]}" '{"symbol":"BTCUSDT","side":"sell","type":"limit","price":"50000","qty":"0.24"}' >/dev/null
R="$(order "${TOK[T]}" '{"symbol":"BTCUSDT","side":"buy","type":"market","qty":"0.2"}')"
[ "$(jq -r .order.status <<<"$R")" = "filled" ] || fail "T open: $R"
FV="$(ok "$(api "${TOK[F]}" GET /futures/copy/follows)" "F follows" | jq -c '.follows[0]')"
# trader margin 500 / equity 9995 x follower equity 1000 = 50.025 -> 0.02001 -> 0.0200
eq "$(jq -r '.positions[0].qty' <<<"$FV")" 0.02 "F mirrored qty"
[ "$(jq -r '.positions[0].leverage' <<<"$FV")" = "20" ] || fail "F leverage synced: $FV"
[ "$(jq -r '.positions[0].marginMode' <<<"$FV")" = "cross" ] || fail "F mode synced: $FV"
GV="$(ok "$(api "${TOK[G]}" GET /futures/copy/follows)" "G follows" | jq -c '.follows[0]')"
# fixed 100 capped to 50 margin x 20 / 50000 = 0.02
eq "$(jq -r '.positions[0].qty' <<<"$GV")" 0.02 "G mirrored qty (capped)"
eq "$(jq -r '.positions[0].tpPrice' <<<"$GV")" 51250 "G TP (50% ROE @20x)"
eq "$(jq -r '.positions[0].slPrice' <<<"$GV")" 49500 "G SL (20% ROE @20x)"
r="$(ok "$(api "${TOK[F]}" GET "/futures/copy/follows/$FID/trades")" "F trades")"
[ "$(jq -r '.trades[0].action + ":" + .trades[0].status' <<<"$r")" = "open:filled" ] || fail "F trade log: $r"
r="$(ok "$(api "${TOK[T]}" GET /futures/copy/trader/followers)" "followers")"
[ "$(jq '.followers|length' <<<"$r")" = "2" ] || fail "trader followers: $r"

say "price 51000; trader closes half then all"
dev POST /internal/dev/index '{"symbol":"BTCUSDT","price":"51000"}' >/dev/null
order "${TOK[N]}" '{"symbol":"BTCUSDT","side":"buy","type":"limit","price":"51000","qty":"0.24"}' >/dev/null
order "${TOK[T]}" '{"symbol":"BTCUSDT","side":"sell","type":"market","qty":"0.1","reduceOnly":true}' >/dev/null
FV="$(ok "$(api "${TOK[F]}" GET /futures/copy/follows)" "F follows" | jq -c '.follows[0]')"
eq "$(jq -r '.positions[0].qty' <<<"$FV")" 0.01 "F after half close"
ok "$(api "${TOK[T]}" POST /futures/positions/BTCUSDT/close '')" "T close" >/dev/null
FV="$(ok "$(api "${TOK[F]}" GET /futures/copy/follows)" "F follows" | jq -c '.follows[0]')"
[ "$(jq '.positions|length' <<<"$FV")" = "0" ] || fail "F still open: $FV"
# pnl 0.02 x 1000 = 20, fees 0.5 + 0.51
eq "$(jq -r .realizedNet <<<"$FV")" 18.99 "F realized net"
GV="$(ok "$(api "${TOK[G]}" GET /futures/copy/follows)" "G follows" | jq -c '.follows[0]')"
eq "$(jq -r .realizedNet <<<"$GV")" 18.99 "G realized net"

say "daily settlement: 20% of 18.99 per follower -> trader spot"
T0="$(spot_usdt "${TOK[T]}")"
dev POST /internal/dev/copy/settle '{"period":"2026-11-02"}' >/dev/null
dev POST /internal/dev/copy/settle '{"period":"2026-11-02"}' >/dev/null
dev POST /internal/dev/copy/settle '{"period":"2026-11-03"}' >/dev/null   # nothing new above HWM
T1="$(spot_usdt "${TOK[T]}")"
eq "$(python3 -c "print($T1-$T0)")" 7.596 "trader spot share"
FV="$(ok "$(api "${TOK[F]}" GET /futures/copy/follows)" "F follows" | jq -c '.follows[0]')"
eq "$(jq -r .follow.sharePaid <<<"$FV")" 3.798 "F share paid"
eq "$(jq -r .equity <<<"$FV")" 1015.192 "F equity after share"
eq "$(jq -r .pnl <<<"$FV")" 15.192 "F pnl"
r="$(ok "$(api "${TOK[F]}" GET "/futures/copy/follows/$FID/trades")" "F trades")"
[ "$(jq '.settlements|length' <<<"$r")" = "2" ] || fail "settlement rows: $r"
r="$(ok "$(api "${TOK[T]}" GET /futures/transfers)" "T transfers")"
[ "$(jq '[.transfers[]|select(.kind=="copy_profit_share" and .status=="done")]|length' <<<"$r")" = "2" ] || fail "share transfers: $r"

say "a loss is never charged and must be recovered first"
dev POST /internal/dev/index '{"symbol":"BTCUSDT","price":"51000"}' >/dev/null
order "${TOK[M]}" '{"symbol":"BTCUSDT","side":"sell","type":"limit","price":"51000","qty":"1"}' >/dev/null
order "${TOK[T]}" '{"symbol":"BTCUSDT","side":"buy","type":"market","qty":"0.1"}' >/dev/null
dev POST /internal/dev/index '{"symbol":"BTCUSDT","price":"50900"}' >/dev/null
order "${TOK[N]}" '{"symbol":"BTCUSDT","side":"buy","type":"limit","price":"50900","qty":"1"}' >/dev/null
ok "$(api "${TOK[T]}" POST /futures/positions/BTCUSDT/close '')" "T close 2" >/dev/null
T2="$(spot_usdt "${TOK[T]}")"
dev POST /internal/dev/copy/settle '{"period":"2026-11-04"}' >/dev/null
eq "$(spot_usdt "${TOK[T]}")" "$T2" "no share on a loss"

say "leaderboard and stats"
L="$(curl -sS "$GATEWAY_URL/futures/copy/traders?sort=roi&days=30")"
TR="$(jq -c --arg id "${UID_[T]}" '.traders[]|select(.traderId==$id)' <<<"$L")"
[ -n "$TR" ] || fail "trader not on leaderboard: $L"
[ "$(jq -r .followers <<<"$TR")" = "2" ] || fail "followers: $TR"
[ "$(jq -r .closedTrades <<<"$TR")" = "3" ] || fail "closed trades: $TR"
eq "$(jq -r .winRate <<<"$TR")" 0.6667 "win rate"
eq "$(jq -r .profitShareEarned <<<"$TR")" 7.596 "share earned"
[ "$(jq -r .nickname <<<"$TR")" = "Alpha" ] || fail "nickname"
for k in composite followers winrate; do
  [ "$(curl -sS "$GATEWAY_URL/futures/copy/traders?sort=$k" | jq '.traders|length')" -ge 1 ] || fail "sort $k"
done
D="$(curl -sS "$GATEWAY_URL/futures/copy/traders/${UID_[T]}?days=7")"
[ "$(jq '.trader.curve|length' <<<"$D")" -ge 1 ] || fail "curve: $D"
S="$(curl -sS "$GATEWAY_URL/futures/copy/summary")"
[ "$(jq -r .traders <<<"$S")" -ge 1 ] && [ "$(jq -r .totalFollowers <<<"$S")" -ge 2 ] || fail "summary: $S"
[ "$(jq --arg p "${UID_[P]}" '[.traders[]|select(.traderId==$p)]|length' <<<"$L")" = "0" ] || fail "pending applicant on leaderboard"
eq "$(jq -r .maxProfitShare <<<"$S")" 0.5 "max share"

say "update params, unfollow returns balance"
r="$(ok "$(api "${TOK[G]}" PATCH "/futures/copy/follows/$GID" '{"maxPerTrade":"80","slRatio":"0.3"}')" "G patch")"
eq "$(jq -r .follow.maxPerTrade <<<"$r")" 80 "G cap"
FB="$(ok "$(api "${TOK[F]}" GET /futures/copy/follows)" "F" | jq -r '.follows[0].equity')"
r="$(ok "$(api "${TOK[F]}" DELETE "/futures/copy/follows/$FID")" "F unfollow")"
[ "$(jq -r .follow.status <<<"$r")" = "stopped" ] || fail "unfollow: $r"
eq "$(bal "${TOK[F]}")" "$FB" "F balance returned"
r="$(api "${TOK[F]}" DELETE "/futures/copy/follows/$FID")"
[ "$(errcode "$r")" = "FOLLOW_NOT_ACTIVE" ] || fail "double unfollow: $r"

say "trader with open copy positions closes -> followers stopped"
order "${TOK[T]}" '{"symbol":"BTCUSDT","side":"buy","type":"market","qty":"0.1"}' >/dev/null
GV="$(ok "$(api "${TOK[G]}" GET /futures/copy/follows)" "G" | jq -c '.follows[0]')"
[ "$(jq '.positions|length' <<<"$GV")" = "1" ] || fail "G mirrored second open: $GV"
ok "$(api "${TOK[T]}" POST /futures/copy/trader/close '')" "trader close" >/dev/null
r="$(ok "$(api "${TOK[G]}" GET '/futures/copy/follows?status=all')" "G all")"
[ "$(jq -r '.follows[0].follow.status' <<<"$r")" = "stopped" ] || fail "G not stopped: $r"
GB="$(bal "${TOK[G]}")"
lt 990 "$GB" "G balance returned ($GB)"
r="$(api "${TOK[G]}" POST /futures/copy/follows "{\"traderId\":\"${UID_[T]}\",\"amount\":\"50\"}")"
[ "$(errcode "$r")" = "TRADER_NOT_ACTIVE" ] || fail "follow closed trader: $r"

say "conservation audit"
A="$(dev GET /internal/dev/audit)"
eq "$(jq -r .diff <<<"$A")" 0 "audit diff ($A)"

printf '\n\033[1;32mSMOKE-COPY PASSED\033[0m\n'
