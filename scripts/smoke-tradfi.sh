#!/usr/bin/env bash
#
# Smoke for TradFi (tokenized stocks + metals; flags tokenizedStocks / metals):
#   flags off -> FEATURE_DISABLED on /tradfi/assets + AAPLUSDT/PAXGUSDT orders;
#   admin enables flags (2FA + reason); CF-IPCountry US / IR -> REGION_RESTRICTED;
#   admin settings (restricted list) + asset + token (ADDRESS_REQUIRED, address
#   then enable, audit); AAPLUSDT trade -> deviation monitor: index pinned +10%
#   -> widened + Telegram alert, back to par -> recovery alert; XAG/WTI perps
#   listed; crypto (BTCUSDT) orders from US still pass.
#
#   bash scripts/smoke-tradfi.sh     # boots devstack (honours DEVSTACK_*_PORT)
#
set -euo pipefail
cd "$(dirname "$0")/.."

if [ "${SMOKE_INNER:-}" != "1" ]; then
  ROOT="$(pwd)"; cd tools/devstack
  exec go run . -root "$ROOT" -exec "SMOKE_INNER=1 bash scripts/smoke-tradfi.sh"
fi

: "${GATEWAY_URL:?}" "${ACCOUNT_URL:?}" "${ASSET_URL:?}" "${ADMIN_URL:?}" "${MARKET_URL:?}" "${INTERNAL_TOKEN:?}"
SA_USER="${ADMIN_BOOTSTRAP_USERNAME:?}"; SA_PASS="${ADMIN_BOOTSTRAP_PASSWORD:?}"; SA_TOTP="${ADMIN_BOOTSTRAP_TOTP_SECRET:?}"
TG_URL="${DEVSTACK_TELEGRAM_API_URL:-http://127.0.0.1:18099}"
PASSWORD="Str0ng-Passw0rd!"
TS="$(date +%s%N)"

say()  { printf '\n\033[1;34m== %s ==\033[0m\n' "$1"; }
fail() { printf '\033[1;31mSMOKE-TRADFI FAILED: %s\033[0m\n' "$1" >&2; exit 1; }
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
# apic <token|-> <country> <METHOD> <path> [json] -> via gateway with CF-IPCountry
apic() {
  local auth=(); [ "$1" != "-" ] && auth=(-H "Authorization: Bearer $1")
  curl -sS -w '\n%{http_code}' -X "$3" "$GATEWAY_URL$4" -H 'Content-Type: application/json' -H "CF-IPCountry: $2" "${auth[@]}" ${5:+-d "$5"}
}
mi() { curl -sS -X "$1" "$MARKET_URL$2" -H 'Content-Type: application/json' -H "X-Internal-Token: $INTERNAL_TOKEN" ${3:+-d "$3"}; }
index() { mi POST /internal/dev/index "{\"symbol\":\"$1\",\"price\":\"$2\"}" >/dev/null; }
credit() {
  [ "$(curl -sS -o /dev/null -w '%{http_code}' -X POST "$ASSET_URL/internal/credit" -H 'Content-Type: application/json' \
    -H "X-Internal-Token: $INTERNAL_TOKEN" -d "{\"userId\":\"$1\",\"asset\":\"$2\",\"amount\":\"$3\",\"ref\":\"smoke-tf-$1-$2-$TS\"}")" = "200" ] || fail "credit $1 $2"
}
fresh() { local s; s=$(( 30 - $(date +%s) % 30 )); [ "$s" -lt 4 ] && sleep "$s"; return 0; }
wait_for() { local d="$1" n="$2"; shift 2; for _ in $(seq 1 $((n * 4))); do "$@" && return 0; sleep 0.25; done; fail "timeout: $d"; }

# --- fake Telegram API -------------------------------------------------------
TGLOG="$(mktemp)"; TGPORT="${TG_URL##*:}"
python3 - "$TGPORT" "$TGLOG" <<'PY' &
import http.server, sys, json
port, log = int(sys.argv[1]), sys.argv[2]
class H(http.server.BaseHTTPRequestHandler):
    def do_POST(self):
        n = int(self.headers.get("Content-Length") or 0); b = self.rfile.read(n).decode()
        try: d = json.loads(b)
        except Exception:
            from urllib.parse import parse_qs; d = {k: v[0] for k, v in parse_qs(b).items()}
        with open(log, "a") as f: f.write(json.dumps({"path": self.path, "text": d.get("text")}, ensure_ascii=False) + "\n")
        self.send_response(200); self.send_header("Content-Type", "application/json"); self.end_headers(); self.wfile.write(b'{"ok":true}')
    def log_message(self, *a): pass
http.server.HTTPServer(("127.0.0.1", port), H).serve_forever()
PY
TGPID=$!
trap 'kill $TGPID 2>/dev/null; rm -f "$TGLOG"' EXIT
sleep 0.5
tg_has() { grep -q "$1" "$TGLOG"; }

SA="$(curl -sS -X POST "$ADMIN_URL/admin/v1/auth/login" -H 'Content-Type: application/json' \
  -d "{\"username\":\"$SA_USER\",\"password\":\"$SA_PASS\",\"totpCode\":\"$(totp "$SA_TOTP")\"}" | jq -r '.token // empty')"
[ -n "$SA" ] || fail "admin login"

say "users"
T1="$(register "tf-a-$TS@example.com")"; U1="$(sub "$T1")"; credit "$U1" USDT 20000
T2="$(register "tf-b-$TS@example.com")"; U2="$(sub "$T2")"; credit "$U2" AAPL 5; credit "$U2" PAXG 1

say "catalogue + seeds"
G="$(mi GET /internal/tradfi/gate)"
jq -e '.features.tokenizedStocks==false and .features.metals==false and (.restrictedCountries|index("US")) and (.restrictedCountries|index("IR"))
  and .spotSymbols.AAPLUSDT=="tokenizedStocks" and .spotSymbols.XAUTUSDT=="metals" and .futuresSymbols.XAGUSDT=="metals" and .futuresSymbols.WTIUSDT=="metals"' <<<"$G" >/dev/null || fail "gate: $G"
I="$(mi GET /internal/tradfi/assets)"
jq -e '(.items|length)==13 and ([.items[]|select(.asset=="AAPL")|.tokens[]|.issuer]|sort)==["ondo","xstocks"]
  and ([.items[].tokens[]?|select(.depositEnabled or .withdrawEnabled)]|length)==0' <<<"$I" >/dev/null || fail "assets: $I"
S="$(ok "$(api - GET /market/symbols)" symbols)"
for s in AAPLUSDT TSLAUSDT NVDAUSDT MSFTUSDT GOOGLUSDT AMZNUSDT METAUSDT COINUSDT SPYUSDT PAXGUSDT XAUTUSDT; do
  jq -e --arg s "$s" '[.. | objects | select(.symbol? == $s)] | length > 0' <<<"$S" >/dev/null || fail "spot symbol $s missing"
done
C="$(ok "$(api - GET /market/categories)" categories)"
jq -e '[.. | objects | select(.id? == "stocks")] | length == 1' <<<"$C" >/dev/null || fail "stocks category: $C"
jq -e '[.. | objects | select(.id? == "metals")] | length == 1' <<<"$C" >/dev/null || fail "metals category: $C"
F="$(ok "$(api - GET /futures/market/contracts)" contracts)"
for s in XAGUSDT WTIUSDT; do jq -e --arg s "$s" '[.. | objects | select(.symbol? == $s)] | length > 0' <<<"$F" >/dev/null || fail "perp $s missing: $F"; done

say "flags off -> FEATURE_DISABLED"
expect "$(apic - SG GET /tradfi/assets)" 403 FEATURE_DISABLED "catalogue off"
expect "$(apic "$T1" SG POST /spot/orders '{"symbol":"AAPLUSDT","side":"buy","type":"limit","price":"200","quantity":"0.1"}')" 403 FEATURE_DISABLED "AAPL off"
expect "$(apic "$T1" SG POST /spot/orders '{"symbol":"PAXGUSDT","side":"buy","type":"limit","price":"3000","quantity":"0.01"}')" 403 FEATURE_DISABLED "PAXG off"
expect "$(apic "$T1" SG POST /futures/orders '{"symbol":"XAGUSDT","side":"buy","type":"market","quantity":"1","leverage":5}')" 403 FEATURE_DISABLED "XAG off"

say "admin enables tokenizedStocks + metals"
fresh; ok "$(A PUT /admin/v1/app/features "$(w 'launch tradfi' '{"features":{"tokenizedStocks":true,"metals":true}}')")" "flags on" >/dev/null
sleep 31   # gateway gate cache (30s)

say "region restriction"
expect "$(apic - US GET /tradfi/assets)" 403 REGION_RESTRICTED "catalogue US"
expect "$(apic "$T1" US POST /spot/orders '{"symbol":"AAPLUSDT","side":"buy","type":"limit","price":"200","quantity":"0.1"}')" 403 REGION_RESTRICTED "AAPL US"
expect "$(apic "$T1" ir POST /spot/orders '{"symbol":"PAXGUSDT","side":"buy","type":"limit","price":"3000","quantity":"0.01"}')" 403 REGION_RESTRICTED "PAXG IR"
expect "$(apic "$T1" US GET '/wallet/deposit/address?asset=AAPL&network=SOL')" 403 REGION_RESTRICTED "deposit addr US"
R="$(apic "$T1" US POST /spot/orders '{"symbol":"BTCUSDT","side":"buy","type":"limit","price":"1","quantity":"0.001"}')"
[ "$(code "$R")" != 403 ] || fail "BTC from US must not be region gated: $(body "$R")"
L="$(ok "$(apic - SG GET /tradfi/assets)" catalogue)"
jq -e '(.items|length)==13 and ([.items[].tokens[]?]|length)==0' <<<"$L" >/dev/null || fail "catalogue (no addresses -> no tokens): $L"

say "admin settings / asset / token (2FA + reason + audit)"
expect "$(A PUT /admin/v1/tradfi/settings '{"deviationBps":300}')" 400 REASON_REQUIRED "settings no reason"
fresh; expect "$(A PUT /admin/v1/tradfi/settings "$(w 'bad list' '{"restrictedCountries":["USA"]}')")" 400 VALIDATION_ERROR "bad country"
fresh; ok "$(A PUT /admin/v1/tradfi/settings "$(w 'add SG for test' '{"restrictedCountries":["US","PR","GU","VI","AS","MP","UM","CU","IR","KP","SY","SG"]}')")" "settings" >/dev/null
fresh; expect "$(A PUT /admin/v1/tradfi/tokens "$(w 'enable w/o address' '{"asset":"AAPL","issuer":"xstocks","network":"SOL","depositEnabled":true}')")" 400 ADDRESS_REQUIRED "address required"
fresh; T="$(ok "$(A PUT /admin/v1/tradfi/tokens "$(w 'verified mint' '{"asset":"AAPL","issuer":"xstocks","network":"SOL","contractAddress":"XsbEhLAtcf6HdfpFZ5xEMdqW8nfAvcsP5bdudRLJzJp","depositEnabled":true,"withdrawEnabled":true}')")" "token")"
jq -e '.after.depositEnabled==true and .after.contractAddress!=""' <<<"$T" >/dev/null || fail "token after: $T"
fresh; ok "$(A PUT /admin/v1/tradfi/assets/AAPL "$(w 'tighter band' '{"deviationBps":500}')")" "asset" >/dev/null
AU="$(ok "$(A GET '/admin/v1/audit-logs?module=tradfi')" audit)"
[ "$(jq '[.. | objects | select(.module? == "tradfi")] | length' <<<"$AU")" -ge 3 ] || fail "audit: $AU"
fresh; ok "$(A PUT /admin/v1/tradfi/settings "$(w 'restore list' '{"restrictedCountries":["US","PR","GU","VI","AS","MP","UM","CU","IR","KP","SY"]}')")" "settings restore" >/dev/null
fresh; ok "$(A PUT /admin/v1/tradfi/assets/AAPL "$(w 'default band' '{"clearDeviationBps":true}')")" "asset restore" >/dev/null
sleep 31
L="$(ok "$(apic - SG GET /tradfi/assets?kind=stock)" catalogue)"
jq -e '(.items|length)==9 and ([.items[]|select(.asset=="AAPL")|.tokens[]|.issuer])==["xstocks"]' <<<"$L" >/dev/null || fail "catalogue AAPL token: $L"

say "trade AAPLUSDT + deviation monitor"
index AAPLUSDT 200
ok "$(apic "$T2" SG POST /spot/orders '{"symbol":"AAPLUSDT","side":"sell","type":"limit","price":"200","quantity":"0.5"}')" "sell" >/dev/null
ok "$(apic "$T1" SG POST /spot/orders '{"symbol":"AAPLUSDT","side":"buy","type":"limit","price":"200","quantity":"0.5"}')" "buy" >/dev/null
dev_is() { mi GET '/internal/tradfi/deviation?check=1' | jq -e --arg w "$1" '[.. | objects | select(.symbol? == "AAPLUSDT")][0] | (.widened|tostring) == $w' >/dev/null; }
wait_for "AAPL at par" 15 dev_is false
index AAPLUSDT 220     # spot 200 vs ref 220 -> ~909 bps > 200 bps default
wait_for "AAPL widened" 15 dev_is true
X="$(ok "$(api - GET /market/index?symbol=AAPLUSDT)" index)"
jq -e '.reference=="tradfi" and .widened==true and .widenFactor>1 and .deviationBps>200' <<<"$X" >/dev/null || fail "index widen: $X"
wait_for "telegram deviation alert" 10 tg_has "AAPLUSDT"
index AAPLUSDT 200
wait_for "AAPL recovered" 15 dev_is false
[ "$(grep -c AAPLUSDT "$TGLOG")" -ge 2 ] || fail "telegram recovery alert: $(cat "$TGLOG")"

printf '\n\033[1;32mSMOKE-TRADFI PASSED\033[0m\n'
