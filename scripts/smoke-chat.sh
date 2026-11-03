#!/usr/bin/env bash
#
# Smoke for the trading chat room (feature flag `chat`):
#   flag off -> FEATURE_DISABLED; admin enables flag + config (2FA + reason + audit);
#   GET /chat/rooms (general + spot.* + futures.*), anonymous history read,
#   POST needs login + KYC (CHAT_KYC_REQUIRED), link/contact filters, max length,
#   rate limit (CHAT_RATE_LIMITED), clientId idempotency, pagination, WS chat:{room}
#   push (message + delete), report, admin delete/mute/ban/reports + audit.
#
#   bash scripts/smoke-chat.sh      # boots devstack (honours DEVSTACK_*_PORT)
#
set -euo pipefail
cd "$(dirname "$0")/.."

if [ "${SMOKE_INNER:-}" != "1" ]; then
  ROOT="$(pwd)"; cd tools/devstack
  exec go run . -root "$ROOT" -exec "SMOKE_INNER=1 bash scripts/smoke-chat.sh"
fi

: "${GATEWAY_URL:?}" "${ACCOUNT_URL:?}" "${ASSET_URL:?}" "${ADMIN_URL:?}" "${INTERNAL_TOKEN:?}"
SA_USER="${ADMIN_BOOTSTRAP_USERNAME:?}"; SA_PASS="${ADMIN_BOOTSTRAP_PASSWORD:?}"; SA_TOTP="${ADMIN_BOOTSTRAP_TOTP_SECRET:?}"
PASSWORD="Str0ng-Passw0rd!"
TS="$(date +%s%N)"

say()  { printf '\n\033[1;34m== %s ==\033[0m\n' "$1"; }
fail() { printf '\033[1;31mSMOKE-CHAT FAILED: %s\033[0m\n' "$1" >&2; exit 1; }

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

# ws_collect <room> <seconds> <outfile>: subscribe to chat:<room> on gateway /ws, dump data frames.
ws_collect() {
  python3 - "$GATEWAY_URL" "$1" "$2" "$3" <<'PY' &
import base64, json, os, socket, struct, sys, time, urllib.parse
u = urllib.parse.urlparse(sys.argv[1]); room, secs, out = sys.argv[2], float(sys.argv[3]), sys.argv[4]
s = socket.create_connection((u.hostname, u.port or 80)); key = base64.b64encode(os.urandom(16)).decode()
s.sendall(("GET /ws HTTP/1.1\r\nHost: %s\r\nUpgrade: websocket\r\nConnection: Upgrade\r\nSec-WebSocket-Key: %s\r\nSec-WebSocket-Version: 13\r\n\r\n" % (u.netloc, key)).encode())
buf = b""
while b"\r\n\r\n" not in buf: buf += s.recv(4096)
assert b" 101 " in buf.split(b"\r\n")[0], buf
buf = buf.split(b"\r\n\r\n", 1)[1]
def send(o):
    p = json.dumps(o).encode(); m = os.urandom(4)
    h = bytes([0x81]) + (bytes([0x80 | len(p)]) if len(p) < 126 else bytes([0x80 | 126]) + struct.pack(">H", len(p)))
    s.sendall(h + m + bytes(b ^ m[i % 4] for i, b in enumerate(p)))
send({"id": "1", "op": "subscribe", "args": ["chat:" + room]})
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
setkyc() { curl -sS -o /dev/null -X POST "$ACCOUNT_URL/internal/account/kyc-level" -H "X-Internal-Token: $INTERNAL_TOKEN" -H 'Content-Type: application/json' -d "{\"uid\":\"$1\",\"level\":$2}"; }

say "flag off -> FEATURE_DISABLED"
ok "$(A PUT /admin/v1/app/features "$(w 'chat off for smoke' '{"features":{"chat":false}}')")" "flag off" >/dev/null
sleep 11  # chat caches the flag for 10s
expect "$(api - GET /chat/rooms)" 403 FEATURE_DISABLED "rooms while off"

say "admin: enable flag + config (guards, validation, audit)"
ok "$(A PUT /admin/v1/app/features "$(w 'launch chat' '{"features":{"chat":true}}')")" "flag on" >/dev/null
expect "$(A PUT /admin/v1/chat/config '{"totpCode":"000000","minIntervalMs":3000}')" 400 REASON_REQUIRED "no reason"
expect "$(A PUT /admin/v1/chat/config "$(w smoke '{"maxLength":0}')")" 400 VALIDATION_ERROR "bad maxLength"
expect "$(A PUT /admin/v1/chat/config "$(w smoke '{"nope":1}')")" 400 VALIDATION_ERROR "unknown key"
CFG="$(ok "$(A PUT /admin/v1/chat/config "$(w 'chat defaults' '{"minIntervalMs":3000,"maxLength":200,"minKycLevel":1,"blockLinks":true,"blockContacts":true,"bannedWords":["拉盘群"],"reportsToHide":0}')")" config)"
[ "$(jq -r .config.maxLength <<<"$CFG")" = "200" ] || fail "config not saved: $CFG"
sleep 11
ok "$(A GET /admin/v1/chat/config)" "get config" | jq -e '.config.minIntervalMs==3000 and .defaults.maxLength==200' >/dev/null || fail "config get"

say "rooms + anonymous read"
R="$(ok "$(api - GET /chat/rooms)" rooms)"
jq -e '[.rooms[].id] | index("general") != null and index("spot.BTCUSDT") != null' <<<"$R" >/dev/null || fail "rooms: $R"
jq -e '[.rooms[]|select(.type=="futures")]|length>0' <<<"$R" >/dev/null || fail "no futures rooms: $R"
jq -e '.rules.maxLength==200 and .rules.minIntervalMs==3000 and .rules.minKycLevel==1' <<<"$R" >/dev/null || fail "rules: $R"
ok "$(api - GET '/chat/rooms/spot.BTCUSDT/messages?limit=5')" history | jq -e '.items|type=="array"' >/dev/null || fail history
expect "$(api - GET /chat/rooms/spot.NOPE/messages)" 404 NOT_FOUND "unknown room"

say "posting rules"
expect "$(api - POST /chat/rooms/general/messages '{"clientId":"x","text":"hi"}')" 401 AUTH_UNAUTHORIZED "anon post"
T1="$(register "chat1$TS@example.com")"; [ -n "$T1" ] || fail "register 1"; U1="$(sub "$T1")"
T2="$(register "chat2$TS@example.com")"; [ -n "$T2" ] || fail "register 2"; U2="$(sub "$T2")"
expect "$(api "$T1" POST /chat/rooms/general/messages '{"clientId":"k0","text":"hello"}')" 403 CHAT_KYC_REQUIRED "no kyc"
setkyc "$U1" 1; setkyc "$U2" 1; sleep 3.2  # unverified KYC cache TTL
expect "$(api "$T1" POST /chat/rooms/general/messages '{"clientId":"l1","text":"看 https://evil.example/x"}')" 400 CHAT_LINK_BLOCKED "link"
expect "$(api "$T1" POST /chat/rooms/general/messages '{"clientId":"l2","text":"ｗｗｗ．ｅｖｉｌ．ｃｏｍ"}')" 400 CHAT_LINK_BLOCKED "fullwidth link"
expect "$(api "$T1" POST /chat/rooms/general/messages '{"clientId":"c1","text":"加微信 wxid_pump888"}')" 400 CHAT_CONTACT_BLOCKED "wechat"
expect "$(api "$T1" POST /chat/rooms/general/messages '{"clientId":"c2","text":"电话 13812345678"}')" 400 CHAT_CONTACT_BLOCKED "phone"
expect "$(api "$T1" POST /chat/rooms/general/messages '{"clientId":"c3","text":"tg @pumpking"}')" 400 CHAT_CONTACT_BLOCKED "telegram"
expect "$(api "$T1" POST /chat/rooms/general/messages '{"clientId":"c4","text":"进拉盘群"}')" 400 CHAT_WORD_BLOCKED "banned word"
LONG="$(python3 -c 'print("涨"*201)')"
expect "$(api "$T1" POST /chat/rooms/general/messages "{\"clientId\":\"big\",\"text\":\"$LONG\"}")" 400 CHAT_TOO_LONG "too long"
expect "$(api "$T1" POST /chat/rooms/general/messages '{"text":"x"}')" 400 VALIDATION_ERROR "no clientId"

say "post + WS push + idempotency + rate limit"
WSO="$(mktemp)"; ws_collect spot.BTCUSDT 6 "$WSO"; WSPID=$!; sleep 1
M1="$(ok "$(api "$T1" POST /chat/rooms/spot.BTCUSDT/messages '{"clientId":"m1","text":"BTC 65000 支撑很强"}')" post1)"
ID1="$(jq -r .id <<<"$M1")"; jq -e '.room=="spot.BTCUSDT" and (.sender|startswith("用户***")) and .clientId=="m1"' <<<"$M1" >/dev/null || fail "post shape: $M1"
R2="$(api "$T1" POST /chat/rooms/spot.BTCUSDT/messages '{"clientId":"m1","text":"BTC 65000 支撑很强"}')"
[ "$(code "$R2")" = "200" ] && [ "$(body "$R2" | jq -r .id)" = "$ID1" ] || fail "idempotent replay: $R2"
R3="$(api "$T1" POST /chat/rooms/spot.BTCUSDT/messages '{"clientId":"m2","text":"again"}')"
expect "$R3" 429 CHAT_RATE_LIMITED "rate limit"
body "$R3" | jq -e '.retryAfterMs>0' >/dev/null || fail "retryAfterMs"
M2="$(ok "$(api "$T2" POST /chat/rooms/spot.BTCUSDT/messages '{"clientId":"n1","text":"我看空 ETH/BTC"}')" post2)"; ID2="$(jq -r .id <<<"$M2")"
sleep 3.2
ok "$(api "$T1" POST /chat/rooms/spot.BTCUSDT/messages '{"clientId":"m3","text":"第三条"}')" post3 >/dev/null

say "history pagination"
H="$(ok "$(api - GET '/chat/rooms/spot.BTCUSDT/messages?limit=2')" hist)"
jq -e '(.items|length)==2 and .hasMore==true and .items[0].id > .items[1].id and (.items[0].clientId==null)' <<<"$H" >/dev/null || fail "page1: $H"
LAST="$(jq -r '.items[1].id' <<<"$H")"
ok "$(api - GET "/chat/rooms/spot.BTCUSDT/messages?limit=2&before=$LAST")" hist2 | jq -e --argjson l "$LAST" 'all(.items[]; .id < $l)' >/dev/null || fail page2

say "report + admin moderation"
expect "$(api "$T1" POST "/chat/messages/$ID1/report" '{"reason":"self"}')" 400 VALIDATION_ERROR "self report"
ok "$(api "$T1" POST "/chat/messages/$ID2/report" '{"reason":"spam"}')" report >/dev/null
RP="$(ok "$(A GET '/admin/v1/chat/reports?status=open')" reports)"
RID="$(jq -r --argjson m "$ID2" '[.items[]|select(.messageId==$m)][0].id' <<<"$RP")"; [ "$RID" != "null" ] || fail "report missing: $RP"
jq -e --arg u "$U2" '.items[0].message.uid==$u' <<<"$RP" >/dev/null || fail "report lacks full uid"
expect "$(A DELETE "/admin/v1/chat/messages/$ID2" '{"totpCode":"000000"}')" 400 REASON_REQUIRED "delete no reason"
ok "$(A DELETE "/admin/v1/chat/messages/$ID2" "$(w 'spam' '{}')")" delete >/dev/null
ok "$(api - GET '/chat/rooms/spot.BTCUSDT/messages?limit=50')" hist3 | jq -e --argjson m "$ID2" 'all(.items[]; .id != $m)' >/dev/null || fail "deleted still visible"
ok "$(A GET "/admin/v1/chat/messages?uid=$U2")" adminmsgs | jq -e '.items[0].status=="deleted" and .items[0].deletedReason=="spam"' >/dev/null || fail "admin msgs"
ok "$(A GET '/admin/v1/chat/reports?status=resolved')" resolved | jq -e --argjson r "$RID" '[.items[]|select(.id==$r)]|length==1' >/dev/null || fail "report not resolved by delete"
wait "$WSPID" || true
grep -q '"type":"message"' "$WSO" && grep -q "\"id\":$ID1" "$WSO" || fail "ws: no message frame: $(cat "$WSO")"
grep -q '"type":"delete"' "$WSO" || fail "ws: no delete frame: $(cat "$WSO")"
grep -q '"event":"subscribed"' "$WSO" || fail "ws: not subscribed"

say "mute / ban"
expect "$(A POST /admin/v1/chat/mutes "$(w 'flood' "{\"uid\":\"$U2\",\"durationMinutes\":0}")")" 400 VALIDATION_ERROR "mute 0"
ok "$(A POST /admin/v1/chat/mutes "$(w 'flood' "{\"uid\":\"$U2\",\"durationMinutes\":10}")")" mute | jq -e '.after.mutedUntil!=null' >/dev/null || fail mute
expect "$(api "$T2" POST /chat/rooms/general/messages '{"clientId":"n2","text":"hi"}')" 403 CHAT_MUTED "muted"
ok "$(A DELETE "/admin/v1/chat/mutes/$U2" "$(w 'appeal ok' '{}')")" unmute >/dev/null
ok "$(api "$T2" POST /chat/rooms/general/messages '{"clientId":"n3","text":"谢谢解禁"}')" "post after unmute" >/dev/null
ok "$(A POST /admin/v1/chat/bans "$(w 'scam' "{\"uid\":\"$U2\"}")")" ban >/dev/null
expect "$(api "$T2" POST /chat/rooms/general/messages '{"clientId":"n4","text":"hi"}')" 403 CHAT_BANNED "banned"
ok "$(A GET /admin/v1/chat/sanctions)" sanctions | jq -e --arg u "$U2" '[.items[]|select(.uid==$u and .banned)]|length==1' >/dev/null || fail sanctions
ok "$(A DELETE "/admin/v1/chat/bans/$U2" "$(w 'unban' '{}')")" unban >/dev/null

say "audit"
AL="$(ok "$(A GET '/admin/v1/audit-logs?module=chat&size=50')" audit)"
for a in chat_config chat_delete_message chat_mute chat_unmute chat_ban chat_unban; do
  jq -e --arg a "$a" '[.items[]|select(.action==$a and .level=="high" and .result=="ok")]|length>0' <<<"$AL" >/dev/null || fail "audit $a missing"
done
jq -e '[.items[]|select(.action=="chat_delete_message")][0].reason=="spam"' <<<"$AL" >/dev/null || fail "audit reason"

say "feature flag in /app/features"
ok "$(api - GET /app/features)" features | jq -e '.features.chat==true' >/dev/null || fail "features.chat"

printf '\n\033[1;32mSMOKE-CHAT PASSED\033[0m\n'
