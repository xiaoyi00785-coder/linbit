#!/usr/bin/env bash
#
# Smoke for event contracts (feature flag `eventContracts`, futures service):
#   flag off -> FEATURE_DISABLED; admin enables flag; GET /event/products;
#   up/down orders at the dev index, clientId idempotency, stake/period/symbol
#   validation, daily limit, exposure limit (EXPOSURE_LIMIT), admin config /
#   pause symbol (2FA + reason + audit), forced expiry + settle (won / lost /
#   tie / no-price refund), exactly-once ledger (re-tick changes nothing),
#   WS push on event:orders (token in ?token=), admin orders + exposure.
#
#   bash scripts/smoke-event.sh     # boots devstack (honours DEVSTACK_*_PORT)
#
set -euo pipefail
cd "$(dirname "$0")/.."

if [ "${SMOKE_INNER:-}" != "1" ]; then
  ROOT="$(pwd)"; cd tools/devstack
  exec go run . -root "$ROOT" -exec "SMOKE_INNER=1 bash scripts/smoke-event.sh"
fi

: "${GATEWAY_URL:?}" "${ACCOUNT_URL:?}" "${ASSET_URL:?}" "${ADMIN_URL:?}" "${FUTURES_INTERNAL_URL:?}" "${INTERNAL_TOKEN:?}"
SA_USER="${ADMIN_BOOTSTRAP_USERNAME:?}"; SA_PASS="${ADMIN_BOOTSTRAP_PASSWORD:?}"; SA_TOTP="${ADMIN_BOOTSTRAP_TOTP_SECRET:?}"
FUTURES_URL="$FUTURES_INTERNAL_URL"
PASSWORD="Str0ng-Passw0rd!"
TS="$(date +%s%N)"

say()  { printf '\n\033[1;34m== %s ==\033[0m\n' "$1"; }
fail() { printf '\033[1;31mSMOKE-EVENT FAILED: %s\033[0m\n' "$1" >&2; exit 1; }
totp() {
  python3 - "$1" <<'PY'
import base64, hmac, struct, sys, time
s = sys.argv[1].strip().upper(); s += "=" * (-len(s) % 8)
k = base64.b32decode(s); c = struct.pack(">Q", int(time.time()) // 30)
h = hmac.new(k, c, "sha1").digest(); o = h[-1] & 15
print("%06d" % ((struct.unpack(">I", h[o:o+4])[0] & 0x7fffffff) % 1000000))
PY
}
# A <METHOD> <path> [json] -> "body\ncode" (super admin)
A() { curl -sS -w '\n%{http_code}' -X "$1" "$ADMIN_URL$2" -H 'Content-Type: application/json' -H "Authorization: Bearer $SA" ${3:+-d "$3"}; }
# api <token|-> <METHOD> <path> [json] -> "body\ncode" via gateway
api() {
  local auth=(); [ "$1" != "-" ] && auth=(-H "Authorization: Bearer $1")
  curl -sS -w '\n%{http_code}' -X "$2" "$GATEWAY_URL$3" -H 'Content-Type: application/json' "${auth[@]}" ${4:+-d "$4"}
}
body() { sed '$d' <<<"$1"; }
code() { tail -n1 <<<"$1"; }
ok() { local c; c="$(code "$1")"; case "$c" in 2*) body "$1" ;; *) fail "$2: HTTP $c $(body "$1")" ;; esac; }
expect() { # expect <resp> <http> <errcode> <label>
  [ "$(code "$1")" = "$2" ] || fail "$4: want HTTP $2, got $(code "$1") $(body "$1")"
  [ -z "$3" ] || [ "$(body "$1" | jq -r '.code // .error.code // empty')" = "$3" ] || fail "$4: want $3, got $(body "$1")"
}
w() { jq -c --arg t "$(totp "$SA_TOTP")" --arg r "$1" '. + {totpCode:$t, reason:$r}' <<<"$2"; }

register() {
  local email="$1" c resp
  curl -sS -o /dev/null -X POST "$GATEWAY_URL/auth/code/send" -H 'Content-Type: application/json' -d "{\"purpose\":\"register\",\"email\":\"$email\"}"
  c="$(curl -sS -G "$ACCOUNT_URL/internal/dev/last-code" --data-urlencode "target=$email" --data-urlencode "purpose=register" -H "X-Internal-Token: $INTERNAL_TOKEN" | jq -r '.code // empty')"
  [ -n "$c" ] || fail "no register code"
  resp="$(curl -sS -X POST "$GATEWAY_URL/auth/register" -H 'Content-Type: application/json' -d "{\"email\":\"$email\",\"password\":\"$PASSWORD\",\"code\":\"$c\"}")"
  jq -r '.accessToken // empty' <<<"$resp"
}
sub() { python3 -c 'import base64,json,sys; p=sys.argv[1].split(".")[1]; p+="="*(-len(p)%4); print(json.loads(base64.urlsafe_b64decode(p))["sub"])' "$1"; }
eq() { python3 -c 'import sys; from decimal import Decimal as D; sys.exit(0 if D(sys.argv[1])==D(sys.argv[2]) else 1)' "$1" "$2" || fail "$3: want $2, got $1"; }
dev() { curl -sS -X "$1" "$FUTURES_URL$2" -H 'Content-Type: application/json' -H "X-Internal-Token: $INTERNAL_TOKEN" ${3:+-d "$3"}; }
credit() {
  [ "$(curl -sS -o /dev/null -w '%{http_code}' -X POST "$ASSET_URL/internal/credit" -H 'Content-Type: application/json' \
    -H "X-Internal-Token: $INTERNAL_TOKEN" -d "{\"userId\":\"$1\",\"asset\":\"USDT\",\"amount\":\"$2\",\"ref\":\"smoke-ev-$1-$TS\"}")" = "200" ] || fail "credit $1"
}
usdt() { ok "$(api "$1" GET /assets/balances)" balances | jq -r '[.items[] | select(.asset=="USDT") | .free][0] // "0"'; }
place() { api "$1" POST /event/orders "$2"; }
tick() { dev POST /internal/dev/event/tick '' >/dev/null; }
expire() { ok "$(printf '%s\n200' "$(dev POST /internal/dev/event/expire "{\"id\":\"$1\",\"agoSeconds\":${2:-1}}")")" "expire $1" >/dev/null; }
index() { dev POST /internal/dev/index "{\"symbol\":\"$1\",\"price\":\"$2\"}" >/dev/null; }
order() { ok "$(api "$1" GET "/event/orders/$2")" "order $2"; }
settle_wait() { # settle_wait <tok> <id> -> order json once settled
  local o i; for i in $(seq 1 20); do
    tick; o="$(order "$1" "$2")"; [ "$(jq -r .status <<<"$o")" = settled ] && { echo "$o"; return; }; sleep 0.5
  done; fail "order $2 never settled: $o"
}

# ws_collect <token> <seconds> <outfile>: /ws?token=, subscribe event:orders, dump frames.
ws_collect() {
  python3 - "$GATEWAY_URL" "$1" "$2" "$3" <<'PY' &
import base64, json, os, socket, struct, sys, time, urllib.parse
u = urllib.parse.urlparse(sys.argv[1]); tok, secs, out = sys.argv[2], float(sys.argv[3]), sys.argv[4]
s = socket.create_connection((u.hostname, u.port or 80)); key = base64.b64encode(os.urandom(16)).decode()
s.sendall(("GET /ws?token=%s HTTP/1.1\r\nHost: %s\r\nUpgrade: websocket\r\nConnection: Upgrade\r\nSec-WebSocket-Key: %s\r\nSec-WebSocket-Version: 13\r\n\r\n" % (tok, u.netloc, key)).encode())
buf = b""
while b"\r\n\r\n" not in buf: buf += s.recv(4096)
assert b" 101 " in buf.split(b"\r\n")[0], buf
buf = buf.split(b"\r\n\r\n", 1)[1]
p = json.dumps({"id": "1", "op": "subscribe", "args": ["event:orders"]}).encode(); m = os.urandom(4)
s.sendall(bytes([0x81, 0x80 | len(p)]) + m + bytes(b ^ m[i % 4] for i, b in enumerate(p)))
s.settimeout(0.3); end = time.time() + secs; frames = []
while time.time() < end:
    try:
        d = s.recv(65536)
        if not d: break
        buf += d
    except socket.timeout:
        pass
    while len(buf) >= 2:
        n = buf[1] & 0x7F; off = 2
        if n == 126:
            if len(buf) < 4: break
            n = struct.unpack(">H", buf[2:4])[0]; off = 4
        elif n == 127:
            if len(buf) < 10: break
            n = struct.unpack(">Q", buf[2:10])[0]; off = 10
        if len(buf) < off + n: break
        frames.append(buf[off:off + n].decode("utf-8", "replace")); buf = buf[off + n:]
open(out, "w").write("\n".join(frames))
PY
}

SA="$(curl -sS -X POST "$ADMIN_URL/admin/v1/auth/login" -H 'Content-Type: application/json' \
  -d "{\"username\":\"$SA_USER\",\"password\":\"$SA_PASS\",\"totpCode\":\"$(totp "$SA_TOTP")\"}" | jq -r '.token // empty')"
[ -n "$SA" ] || fail "admin login"

say "users"
T1="$(register "ev-a-$TS@example.com")"; U1="$(sub "$T1")"; credit "$U1" 20000
T2="$(register "ev-b-$TS@example.com")"; U2="$(sub "$T2")"; credit "$U2" 20000
T3="$(register "ev-c-$TS@example.com")"; U3="$(sub "$T3")"   # no funds
index BTCUSDT 50000; index ETHUSDT 3000

say "flag off -> FEATURE_DISABLED"
P="$(ok "$(api - GET /event/products)" products)"
jq -e '.enabled==false and (.products|length)==5 and .tieRule=="refund"' <<<"$P" >/dev/null || fail "products off: $P"
expect "$(place "$T1" '{"symbol":"BTCUSDT","period":"5m","direction":"up","stake":"10","clientId":"x0"}')" 403 FEATURE_DISABLED "flag off"
expect "$(api - POST /event/orders '{}')" 401 "" "anonymous order"

say "admin enables eventContracts"
ok "$(A PUT /admin/v1/app/features "$(w 'launch event contracts' '{"features":{"eventContracts":true}}')")" "flag on" >/dev/null
for i in $(seq 1 40); do P="$(body "$(api - GET /event/products)")"; [ "$(jq -r .enabled <<<"$P")" = true ] && break; sleep 0.5; done
jq -e '.enabled==true and ([.periods[].period]==["5m","15m","30m","1h","4h","1d"]) and .payoutRatio=="0.8" and .minStake=="5" and .maxStake=="1000" and .dailyLimit=="10000" and .stakeAsset=="USDT"' <<<"$P" >/dev/null || fail "products on: $P"
jq -e '[.products[].symbol]==["BTCUSDT","ETHUSDT","SOLUSDT","BNBUSDT","TRXUSDT"]' <<<"$P" >/dev/null || fail "symbols: $P"
eq "$(jq -r '.products[]|select(.symbol=="BTCUSDT").indexPrice' <<<"$P")" 50000 "BTC index"

say "validation"
expect "$(place "$T1" '{"symbol":"BTCUSDT","period":"5m","direction":"up","stake":"4.99","clientId":"v1"}')" 400 STAKE_OUT_OF_RANGE "min stake"
expect "$(place "$T1" '{"symbol":"BTCUSDT","period":"5m","direction":"up","stake":"1000.01","clientId":"v2"}')" 400 STAKE_OUT_OF_RANGE "max stake"
expect "$(place "$T1" '{"symbol":"BTCUSDT","period":"2m","direction":"up","stake":"10","clientId":"v3"}')" 400 PERIOD_NOT_SUPPORTED "period"
expect "$(place "$T1" '{"symbol":"DOGEUSDT","period":"5m","direction":"up","stake":"10","clientId":"v4"}')" 400 SYMBOL_NOT_SUPPORTED "symbol"
expect "$(place "$T1" '{"symbol":"BTCUSDT","period":"5m","direction":"sideways","stake":"10","clientId":"v5"}')" 400 "" "direction"
expect "$(place "$T1" '{"symbol":"SOLUSDT","period":"5m","direction":"up","stake":"10","clientId":"v6"}')" 503 INDEX_UNAVAILABLE "no index"
expect "$(place "$T3" '{"symbol":"BTCUSDT","period":"5m","direction":"up","stake":"10","clientId":"v7"}')" 400 INSUFFICIENT_BALANCE "no funds"
R="$(ok "$(api "$T3" GET '/event/orders?status=rejected')" rejected)"
jq -e '(.orders|length)==1 and .orders[0].status=="rejected"' <<<"$R" >/dev/null || fail "rejected list: $R"

say "place up/down + idempotency"
WSO="$(mktemp)"; ws_collect "$T1" 14 "$WSO"; WSPID=$!; sleep 1
O1="$(ok "$(place "$T1" '{"symbol":"BTCUSDT","period":"5m","direction":"up","stake":"100","clientId":"o1"}')" o1)"
ID1="$(jq -r .id <<<"$O1")"
jq -e '.status=="open" and .direction=="up" and .period=="5m" and .expiryTime-.openTime==300000' <<<"$O1" >/dev/null || fail "o1 shape: $O1"
eq "$(jq -r .openPrice <<<"$O1")" 50000 "o1 open price"; eq "$(jq -r .expectedPayout <<<"$O1")" 180 "o1 expected payout"
R="$(place "$T1" '{"symbol":"BTCUSDT","period":"5m","direction":"up","stake":"100","clientId":"o1"}')"
[ "$(code "$R")" = 200 ] && [ "$(body "$R" | jq -r .id)" = "$ID1" ] || fail "replay: $R"
eq "$(usdt "$T1")" 19900 "A after one stake (replay not charged)"
ID2="$(ok "$(place "$T1" '{"symbol":"BTCUSDT","period":"5m","direction":"down","stake":"50","clientId":"o2"}')" o2 | jq -r .id)"
ID3="$(ok "$(place "$T1" '{"symbol":"ETHUSDT","period":"15m","direction":"up","stake":"20","clientId":"o3"}')" o3 | jq -r .id)"
ID4="$(ok "$(place "$T2" '{"symbol":"BTCUSDT","period":"1h","direction":"down","stake":"30","clientId":"o4"}')" o4 | jq -r .id)"
eq "$(usdt "$T1")" 19830 "A after three stakes"
expect "$(api "$T2" GET "/event/orders/$ID1")" 404 "" "other user's order"
L="$(ok "$(api "$T1" GET '/event/orders?status=open')" open)"
[ "$(jq '.orders|length' <<<"$L")" = 3 ] || fail "open list: $L"; eq "$(jq -r .dailyUsed <<<"$L")" 170 "dailyUsed"

say "settle: up won, down lost, ETH tie; re-tick exactly-once"
index BTCUSDT 50100; sleep 1.3   # let the 1s sampler record the new index
expire "$ID1" 0; expire "$ID2" 0; expire "$ID3" 0
S1="$(settle_wait "$T1" "$ID1")"; S2="$(settle_wait "$T1" "$ID2")"; S3="$(settle_wait "$T1" "$ID3")"
jq -e '.result=="won"' <<<"$S1" >/dev/null || fail "o1: $S1"; eq "$(jq -r .payout <<<"$S1")" 180 "o1 payout"; eq "$(jq -r .settlePrice <<<"$S1")" 50100 "o1 settle"
jq -e '.result=="lost"' <<<"$S2" >/dev/null || fail "o2: $S2"; eq "$(jq -r .payout <<<"$S2")" 0 "o2 payout"
jq -e '.result=="tie"' <<<"$S3" >/dev/null || fail "o3: $S3"; eq "$(jq -r .payout <<<"$S3")" 20 "o3 refund"
eq "$(usdt "$T1")" 20030 "A after settle (19830+180+20)"
tick; tick; eq "$(usdt "$T1")" 20030 "A unchanged after re-ticks"

say "no settle price inside grace -> refund"
O5="$(ok "$(place "$T1" '{"symbol":"BTCUSDT","period":"5m","direction":"up","stake":"40","clientId":"o5"}')" o5)"; ID5="$(jq -r .id <<<"$O5")"
expire "$ID5" 600   # expiry 10 min ago: no samples there, grace (120s) passed
S5="$(settle_wait "$T1" "$ID5")"
jq -e '.result=="refunded"' <<<"$S5" >/dev/null || fail "o5: $S5"; eq "$(usdt "$T1")" 20030 "A after refund"

say "WS push event:orders"
wait "$WSPID" || true
grep -q "\"id\":\"$ID1\"" "$WSO" && grep -q '"channel":"event:orders"' "$WSO" || fail "no ws settlement push: $(cat "$WSO")"
grep -q "\"id\":\"$ID4\"" "$WSO" && fail "ws leaked another user's order"

say "admin: config, exposure limit, daily limit, pause"
expect "$(A PUT /admin/v1/event/config '{"exposureLimit":"100"}')" 400 "" "config without reason/totp"
C="$(ok "$(A PUT /admin/v1/event/config "$(w 'tight exposure for smoke' '{"exposureLimit":"100","dailyLimit":"1000"}')")" "config put")"
eq "$(jq -r .after.exposureLimit <<<"$C")" 100 "exposure after"; eq "$(jq -r .before.exposureLimit <<<"$C")" 50000 "exposure before"
ok "$(place "$T2" '{"symbol":"ETHUSDT","period":"30m","direction":"up","stake":"90","clientId":"t1"}')" t1 >/dev/null
expect "$(place "$T2" '{"symbol":"ETHUSDT","period":"30m","direction":"up","stake":"20","clientId":"t2"}')" 400 EXPOSURE_LIMIT "exposure"
ok "$(place "$T1" '{"symbol":"ETHUSDT","period":"30m","direction":"down","stake":"20","clientId":"t3"}')" "hedging side allowed" >/dev/null
ok "$(place "$T2" '{"symbol":"ETHUSDT","period":"4h","direction":"up","stake":"90","clientId":"t4"}')" "other period separate book" >/dev/null
E="$(ok "$(A GET /admin/v1/event/exposure)" exposure)"
eq "$(jq -r '.books[]|select(.symbol=="ETHUSDT" and .period=="30m").netExposure' <<<"$E")" 70 "net exposure"
expect "$(place "$T2" '{"symbol":"BTCUSDT","period":"5m","direction":"down","stake":"800","clientId":"d1"}')" 400 DAILY_LIMIT_EXCEEDED "daily limit"
ok "$(A PUT /admin/v1/event/symbols/BTCUSDT "$(w 'pause btc for smoke' '{"paused":true}')")" pause >/dev/null
expect "$(place "$T1" '{"symbol":"BTCUSDT","period":"5m","direction":"up","stake":"10","clientId":"p1"}')" 400 SYMBOL_PAUSED "paused"
jq -e '.products[]|select(.symbol=="BTCUSDT").paused==true' <<<"$(body "$(api - GET /event/products)")" >/dev/null || fail "products paused"
ok "$(A PUT /admin/v1/event/symbols/BTCUSDT "$(w 'resume btc' '{"paused":false}')")" resume >/dev/null
ok "$(place "$T1" '{"symbol":"BTCUSDT","period":"5m","direction":"up","stake":"10","clientId":"p2"}')" "after resume" >/dev/null
AO="$(ok "$(A GET "/admin/v1/event/orders?uid=$U1&status=settled")" "admin orders")"
[ "$(jq '.orders|length' <<<"$AO")" = 4 ] || fail "admin settled list: $AO"
AU="$(ok "$(A GET '/admin/v1/audit-logs?module=event&size=50')" audit)"
jq -e '[.items[].action] | (index("event_config")!=null and index("event_symbol_pause")!=null and index("event_symbol_resume")!=null)' <<<"$AU" >/dev/null || fail "audit: $AU"

say "SMOKE-EVENT OK"
