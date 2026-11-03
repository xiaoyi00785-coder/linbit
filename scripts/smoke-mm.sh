#!/usr/bin/env bash
# Smoke for the market maker: admin RBAC + 2FA + audit; virtual testnet seed
# (mm_testnet_seed, idempotent, mock chain only); multi-level two-sided quotes
# around the external index on spot + futures (spread 0.1%); index outage ->
# quotes cancelled, MM paused, Telegram alert; recovery; stop cancels all;
# solvency reconciliation + Telegram alert on shortfall. Telegram is a local
# fake server (no real token is ever used).
set -euo pipefail
cd "$(dirname "$0")/.."

if [ "${SMOKE_INNER:-}" != "1" ]; then
  echo "== booting devstack =="
  ROOT="$(pwd)"
  cd tools/devstack
  exec go run . -root "$ROOT" -exec "SMOKE_INNER=1 bash scripts/smoke-mm.sh"
fi

GATEWAY_URL="${GATEWAY_URL:-http://localhost:8080}"
ASSET_URL="${ASSET_URL:-http://localhost:8082}"
ACCOUNT_URL="${ACCOUNT_URL:-http://localhost:8081}"
ORDER_URL="${ORDER_URL:-http://localhost:8084}"
MARKET_URL="${MARKET_URL:-http://localhost:8085}"
ADMIN_URL="${ADMIN_URL:-http://localhost:8088}"
FUTURES_URL="${FUTURES_INTERNAL_URL:-http://localhost:8089}"
MM_URL="${MM_URL:-http://localhost:8093}"
TG_URL="${DEVSTACK_TELEGRAM_API_URL:-http://127.0.0.1:18099}"
INTERNAL_TOKEN="${INTERNAL_TOKEN:-}"
SA_USER="${ADMIN_BOOTSTRAP_USERNAME:-}"
SA_PASS="${ADMIN_BOOTSTRAP_PASSWORD:-}"
SA_TOTP="${ADMIN_BOOTSTRAP_TOTP_SECRET:-}"
PASSWORD="Str0ng-Passw0rd!"
TS="$(date +%s%N)"
SMOKE_NAME="MM"
. scripts/lib-growth-smoke.sh
[ -n "$SA_USER" ] && [ -n "$SA_PASS" ] && [ -n "$SA_TOTP" ] || fail "ADMIN_BOOTSTRAP_* not set"
MM="mm:main"
APASS="Adm1n-Smoke-Passw0rd!"
pass() { printf "  ok  %s\n" "$1"; }
cj() { jq -r '.code // .error.code // empty' <<<"$1"; }
# avoid a code that expires mid-request
fresh() { local s; s=$(( 30 - $(date +%s) % 30 )); [ "$s" -lt 4 ] && sleep "$s"; return 0; }
AH() { curl -sS -o /dev/null -w '%{http_code}' -X "$2" "$ADMIN_URL$3" -H 'Content-Type: application/json' -H "Authorization: Bearer $1" ${4:+-d "$4"}; }
wait_for() { # wait_for <desc> <seconds> <cmd...>
  local d="$1" n="$2"; shift 2
  for _ in $(seq 1 $((n * 4))); do "$@" && return 0; sleep 0.25; done
  [ -n "${SA:-}" ] && { echo "mm status:" >&2; A "$SA" GET /admin/v1/mm/status >&2 || true; spot_open >&2 2>/dev/null || true; }
  fail "timeout: $d"
}

# --- fake Telegram API -------------------------------------------------------
TGLOG="$(mktemp)"; TGPORT="${TG_URL##*:}"
python3 - "$TGPORT" "$TGLOG" <<'PY' &
import http.server, sys, json
port, log = int(sys.argv[1]), sys.argv[2]
class H(http.server.BaseHTTPRequestHandler):
    def do_POST(self):
        n = int(self.headers.get("Content-Length") or 0); b = self.rfile.read(n).decode()
        try:
            d = json.loads(b)
        except Exception:
            from urllib.parse import parse_qs; d = {k: v[0] for k, v in parse_qs(b).items()}
        with open(log, "a") as f: f.write(json.dumps({"path": self.path, "chat": d.get("chat_id"), "text": d.get("text")}, ensure_ascii=False) + "\n")
        self.send_response(200); self.send_header("Content-Type", "application/json"); self.end_headers()
        self.wfile.write(b'{"ok":true}')
    def log_message(self, *a): pass
http.server.HTTPServer(("127.0.0.1", port), H).serve_forever()
PY
TGPID=$!
trap 'kill $TGPID 2>/dev/null; rm -f "$TGLOG"' EXIT
sleep 0.5
tg_has() { grep -q "$1" "$TGLOG"; }

idx() { internal "$MARKET_URL" POST /internal/dev/index "$1" >/dev/null; }
spot_open() { curl -sS "$ORDER_URL/spot/orders/open?symbol=BTCUSDT&pageSize=100" -H "X-User-Id: $MM"; }
fut_open() { curl -sS "$FUTURES_URL/futures/orders?symbol=BTCUSDT&status=open" -H "X-User-Id: $MM"; }
n_spot() { spot_open | jq '.items | length'; }
n_fut() { fut_open | jq '.orders | length'; }

say "index pinned + pair discovery"
idx '{"symbol":"BTCUSDT","price":"60000"}'
I="$(curl -sS "$MARKET_URL/market/index?symbol=BTCUSDT")"
[ "$(jq -r .indexPrice <<<"$I")" = 60000 ] || fail "index: $I"
has_pairs() { local P; P="$(internal "$MM_URL" GET /internal/mm/pairs)"
  [ "$(jq '[.items[] | select(.symbol=="BTCUSDT")] | length' <<<"$P")" = 2 ]; }
wait_for "pairs discovered" 20 has_pairs
P="$(internal "$MM_URL" GET /internal/mm/pairs)"
D="$(jq -c '.items[] | select(.market=="spot" and .symbol=="BTCUSDT")' <<<"$P")"
[ "$(jq -r .spreadBps <<<"$D")" = 10 ] && [ "$(jq -r .levels <<<"$D")" = 5 ] && [ "$(jq -r .refreshMs <<<"$D")" = 3000 ] || fail "defaults: $D"
# keep the smoke focused: disable every other pair (internal, setup only)
for k in $(jq -r '.items[] | select(.symbol!="BTCUSDT") | "\(.market)/\(.symbol)"' <<<"$P"); do
  internal "$MM_URL" PUT "/internal/mm/pairs/$k" '{"enabled":false,"updatedBy":"smoke"}' >/dev/null
done
pass "index 60000; spot+futures BTCUSDT registered with defaults (spread 10bps, 5 levels, 3000ms)"

say "admin RBAC"
SA="$(alogin "$SA_USER" "$SA_PASS" "$SA_TOTP")"; [ -n "$SA" ] || fail "super admin login"
mk() {
  local R sec; fresh; R="$(A "$SA" POST /admin/v1/admins "{\"username\":\"$1${TS: -9}\",\"password\":\"$APASS\",\"roles\":[\"$2\"],\"totpCode\":\"$(totp "$SA_TOTP")\"}")"
  sec="$(jq -r '.totpSecret // empty' <<<"$R")"; [ -n "$sec" ] || fail "create $1: $R"
  printf -v "$1_SECRET" '%s' "$sec"; printf -v "$1_TOK" '%s' "$(alogin "$1${TS: -9}" "$APASS" "$sec")"
}
mk sup support; mk risk risk
[ "$(AH "$sup_TOK" GET /admin/v1/mm/status)" = 403 ] || fail "support can view mm"
[ "$(AH "$risk_TOK" GET /admin/v1/mm/status)" = 200 ] || fail "risk cannot view mm"
[ "$(AH "$risk_TOK" POST /admin/v1/mm/start '{}')" = 403 ] || fail "risk can start mm"
[ "$(AH "$risk_TOK" POST /admin/v1/mm/testnet-seed '{"ref":"x"}')" = 403 ] || fail "risk can seed"
S="$(A "$SA" GET /admin/v1/mm/status)"
[ "$(jq -r .testnetSeedAllowed <<<"$S")" = true ] && [ "$(jq -r .running <<<"$S")" = false ] && [ "$(jq -r .stopLoss <<<"$S")" = false ] || fail "status: $S"
pass "support 403; risk view-only; status: stopped, seed allowed (mock chain), no stop-loss"

say "virtual testnet seed"
[ "$(cj "$(A "$SA" POST /admin/v1/mm/testnet-seed '{"ref":"s1","totpCode":"000000"}')")" = TOTP_INVALID ] || fail "seed without 2FA"
fresh; R="$(A "$SA" POST /admin/v1/mm/testnet-seed "{\"ref\":\"s1-$TS\",\"reason\":\"smoke\",\"totpCode\":\"$(totp "$SA_TOTP")\"}")"
[ "$(jq -r .status <<<"$R")" = ok ] || fail "seed: $R"
fresh; R="$(A "$SA" POST /admin/v1/mm/testnet-seed "{\"ref\":\"s1-$TS\",\"reason\":\"smoke\",\"totpCode\":\"$(totp "$SA_TOTP")\"}")"
[ "$(jq -r .status <<<"$R")" = already_applied ] || fail "seed replay: $R"
B="$(internal "$ASSET_URL" GET "/internal/balances?userId=$MM")"
eq "$(jq -r '.items[] | select(.asset=="USDT") | .total' <<<"$B")" 10000000 "MM USDT after seed"
R="$(internal "$ASSET_URL" POST /internal/mm/testnet-seed '{"userId":"u-not-mm","asset":"USDT","amount":"1","ref":"bad"}')"
[ "$(cj "$R")" = NOT_MM_ACCOUNT ] || fail "seed to non-MM account: $R"
R="$(internal "$ASSET_URL" POST /internal/mm/testnet-seed "{\"userId\":\"$MM\",\"asset\":\"USDT\",\"amount\":\"20000000\",\"ref\":\"big-$TS\"}")"
[ -n "$(cj "$R")" ] || fail "seed above MM_SEED_MAX accepted: $R"
L="$(A "$SA" GET "/admin/v1/audit-logs?module=mm&action=testnet_seed")"
[ "$(jq '[.items[] | select(.result=="ok")] | length' <<<"$L")" -ge 2 ] || fail "seed audit: $L"
pass "2FA required; 10,000,000 USDT credited once (idempotent ref); non-MM / over-cap refused; audited"
# give the MM some base so spot quotes both sides (unbacked on purpose -> solvency shortfall below)
credit "$MM" BTC 1

say "configure + start"
fresh; R="$(A "$SA" POST /admin/v1/mm/futures-transfer "{\"direction\":\"in\",\"amount\":\"1000000\",\"totpCode\":\"$(totp "$SA_TOTP")\"}")"
[ -z "$(cj "$R")" ] || fail "futures transfer: $R"
[ "$(cj "$(A "$SA" PUT /admin/v1/mm/pairs/spot/BTCUSDT '{"levels":3,"totpCode":"000000"}')")" = TOTP_INVALID ] || fail "update without 2FA"
[ "$(cj "$(A "$SA" PUT /admin/v1/mm/pairs/spot/BTCUSDT "{\"levels\":99,\"totpCode\":\"$(totp "$SA_TOTP")\"}")")" = INVALID_PARAM ] || fail "levels 99 accepted"
fresh; R="$(A "$SA" PUT /admin/v1/mm/pairs/spot/BTCUSDT "{\"levels\":3,\"maxSkewBps\":0,\"maxPosition\":\"500000\",\"refreshMs\":1000,\"reason\":\"smoke\",\"totpCode\":\"$(totp "$SA_TOTP")\"}")"
[ "$(jq -r .levels <<<"$R")" = 3 ] && [ "$(jq -r .updatedBy <<<"$R")" = "$SA_USER" ] || fail "update pair: $R"
fresh; R="$(A "$SA" PUT /admin/v1/mm/pairs/futures/BTCUSDT "{\"refreshMs\":1000,\"reason\":\"smoke\",\"totpCode\":\"$(totp "$SA_TOTP")\"}")"
[ "$(jq -r .refreshMs <<<"$R")" = 1000 ] || fail "update futures pair: $R"
fresh; R="$(A "$SA" POST /admin/v1/mm/start "{\"reason\":\"smoke\",\"totpCode\":\"$(totp "$SA_TOTP")\"}")"
[ "$(jq -r .running <<<"$R")" = true ] || fail "start: $R"
spot_ok() { [ "$(n_spot)" = 6 ]; }
fut_ok() { [ "$(n_fut)" = 10 ]; }
wait_for "spot ladder (3x2)" 20 spot_ok
wait_for "futures ladder (5x2)" 20 fut_ok
O="$(spot_open)"
BB="$(jq -r '[.items[] | select(.side=="buy") | .price | tonumber * 1] | max' <<<"$O")"
BA="$(jq -r '[.items[] | select(.side=="sell") | .price | tonumber * 1] | min' <<<"$O")"
eq "$BB" 59970 "spot best bid"; eq "$BA" 60030 "spot best ask"
eq "$(python3 -c "print(($BA-$BB)/60000)")" 0.001 "spot spread"
F="$(fut_open)"
eq "$(jq -r '[.orders[] | select(.side=="buy") | .price | tonumber * 1] | max' <<<"$F")" 59970 "futures best bid"
eq "$(jq -r '[.orders[] | select(.side=="sell") | .price | tonumber * 1] | min' <<<"$F")" 60030 "futures best ask"
eq "$(jq -r '[.orders[] | select(.side=="buy") | .price | tonumber * 1] | min' <<<"$F")" 59850 "futures 5th bid (5+4*5 bps)"
pass "spot 3 levels/side, futures 5 levels/side; best 59970/60030 = 0.1% around index"

say "follows the index"
idx '{"symbol":"BTCUSDT","price":"61000"}'
moved() { [ "$(spot_open | jq '[.items[] | select(.side=="buy") | .price | tonumber * 1] | max')" = 60969.5 ]; }
wait_for "requote to 61000" 15 moved
pass "best bid requoted to 60969.5 after index -> 61000"

say "feed outage -> pause + Telegram"
idx '{"outage":true}'
paused() { [ "$(n_spot)" = 0 ] && [ "$(n_fut)" = 0 ]; }
wait_for "quotes cancelled on outage" 15 paused
wait_for "telegram pause alert" 10 tg_has "做市暂停"
S="$(internal "$MM_URL" GET /internal/mm/status)"
[ "$(jq -r '.pairs[] | select(.market=="spot" and .symbol=="BTCUSDT") | .status' <<<"$S")" = paused ] || fail "status during outage: $S"
grep -q '"chat": "devstack-chat"' "$TGLOG" || fail "telegram chat id: $(cat "$TGLOG")"
grep -q '/botdevstack-telegram-token/sendMessage' "$TGLOG" || fail "telegram path"
idx '{"outage":false}'
wait_for "quotes back" 15 spot_ok
wait_for "telegram recovery alert" 10 tg_has "做市恢复"
pass "outage: all quotes cancelled, pairs paused, Telegram alert; recovery requotes + alert"

say "stop"
fresh; R="$(A "$SA" POST /admin/v1/mm/stop "{\"reason\":\"smoke\",\"totpCode\":\"$(totp "$SA_TOTP")\"}")"
[ "$(jq -r .running <<<"$R")" = false ] || fail "stop: $R"
[ "$(n_spot)" = 0 ] && [ "$(n_fut)" = 0 ] || fail "orders left after stop"
sleep 1.5; [ "$(n_spot)" = 0 ] || fail "quoting after stop"
L="$(A "$SA" GET "/admin/v1/audit-logs?module=mm")"
for a in start stop update_pair futures_transfer; do
  [ "$(jq --arg a "$a" '[.items[] | select(.action==$a)] | length' <<<"$L")" -ge 1 ] || fail "audit $a missing"
done
pass "stop cancels everything; start/stop/update_pair/futures_transfer audited"

say "solvency"
[ "$(AH "$sup_TOK" POST /admin/v1/solvency/run '{}')" = 403 ] || fail "support can run solvency"
R="$(A "$SA" POST /admin/v1/solvency/run '{}')"
[ "$(jq -r .ok <<<"$R")" = false ] || fail "unbacked BTC should fail: $R"
[ "$(jq -r '.lines[] | select(.asset=="BTC") | .ok' <<<"$R")" = false ] || fail "BTC line: $R"
U="$(jq -c '.lines[] | select(.asset=="USDT")' <<<"$R")"
[ "$(jq -r .ok <<<"$U")" = true ] && eq "$(jq -r .virtualSeed <<<"$U")" 10000000 "virtual seed" || fail "USDT line: $U"
eq "$(jq -r .marketMaker <<<"$U")" 10000000 "MM USDT (9M spot + 1M futures margin)" 1
wait_for "telegram solvency alert" 10 tg_has "偿付对账失败"
G="$(A "$SA" GET /admin/v1/solvency/latest)"
[ "$(jq -r .report.ok <<<"$G")" = false ] && [[ "$(jq -r .trigger <<<"$G")" == manual:* ]] || fail "latest: $G"
pass "holdings vs user+MM liabilities per asset; virtual seed excluded; shortfall -> Telegram; report stored"

printf '\n\033[1;32mSMOKE-MM PASSED\033[0m\n'
