#!/usr/bin/env bash
#
# End-to-end smoke for 闪兑 convert (asset service via gateway): pairs, quotes
# by from/to amount, limits, atomic accept + idempotent replay, quote expiry,
# PRICE_UNAVAILABLE, cross pairs, stable pairs, history, admin config and the
# platform convert position in solvency.
#
#   bash scripts/smoke-convert.sh            # boots devstack
#
set -euo pipefail
cd "$(dirname "$0")/.."

if [ "${SMOKE_INNER:-}" != "1" ]; then
  echo "== booting devstack =="
  ROOT="$(pwd)"
  cd tools/devstack
  # the hedge loop is driven explicitly via /internal/convert/hedge/run
  export DEVSTACK_CONVERT_HEDGE_INTERVAL_SECONDS="${DEVSTACK_CONVERT_HEDGE_INTERVAL_SECONDS:-0}"
  exec go run . -root "$ROOT" -exec "SMOKE_INNER=1 bash scripts/smoke-convert.sh"
fi

GATEWAY_URL="${GATEWAY_URL:-http://localhost:8080}"
ACCOUNT_URL="${ACCOUNT_URL:-http://localhost:8081}"
ASSET_URL="${ASSET_URL:-http://localhost:8082}"
MARKET_URL="${MARKET_URL:-http://localhost:8085}"
ORDER_URL="${ORDER_URL:-http://localhost:8084}"
TG_URL="${DEVSTACK_TELEGRAM_API_URL:-http://127.0.0.1:18099}"
ADMIN_URL="${ADMIN_URL:-http://localhost:8088}"
INTERNAL_TOKEN="${INTERNAL_TOKEN:-}"
SA_USER="${ADMIN_BOOTSTRAP_USERNAME:-}"
SA_PASS="${ADMIN_BOOTSTRAP_PASSWORD:-}"
SA_TOTP="${ADMIN_BOOTSTRAP_TOTP_SECRET:-}"
PASSWORD="Str0ng-Passw0rd!"
TS="$(date +%s%N)"
SMOKE_NAME="CONVERT"
. scripts/lib-growth-smoke.sh
[ -n "$SA_USER" ] && [ -n "$SA_PASS" ] && [ -n "$SA_TOTP" ] || fail "ADMIN_BOOTSTRAP_* not set"
fresh() { local s; s=$(( 30 - $(date +%s) % 30 )); [ "$s" -lt 4 ] && sleep "$s"; return 0; }
idx() { internal "$MARKET_URL" POST /internal/dev/index "{\"symbol\":\"$1\",\"price\":\"$2\"}" >/dev/null; }
idx BTCUSDT 50000; idx ETHUSDT 2500; idx USDCUSDT 1

# --- fake Telegram API (alerts) ---------------------------------------------
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
        with open(log, "a") as f: f.write(json.dumps({"text": d.get("text")}, ensure_ascii=False) + "\n")
        self.send_response(200); self.send_header("Content-Type", "application/json"); self.end_headers()
        self.wfile.write(b'{"ok":true}')
    def log_message(self, *a): pass
http.server.HTTPServer(("127.0.0.1", port), H).serve_forever()
PY
TGPID=$!
trap 'kill $TGPID 2>/dev/null; rm -f "$TGLOG"' EXIT
sleep 0.5
tg_wait() { for _ in $(seq 1 40); do grep -q "$1" "$TGLOG" && return 0; sleep 0.25; done; fail "telegram alert missing: $1"; }

say "pairs (public)"
P="$(curl -sS "$GATEWAY_URL/convert/pairs")"
[ "$(jq -r '.spreadBps' <<<"$P")" = 20 ] || fail "default spread: $P"
[ "$(jq -r '.fee' <<<"$P")" = 0 ] || fail "fee"
jq -e '.pairs[] | select(.from=="USDT" and .to=="BTC" and .route=="direct")' <<<"$P" >/dev/null || fail "USDT->BTC pair"
jq -e '.pairs[] | select(.from=="BTC" and .to=="ETH" and .route=="via_usdt")' <<<"$P" >/dev/null || fail "BTC->ETH pair"
jq -e '.pairs[] | select(.from=="USDC" and .to=="USDT" and .route=="stable" and .spreadBps==0)' <<<"$P" >/dev/null || fail "stable pair"
echo "  ok"

read -r TOK UID_ < <(register "cv$TS@example.com")
credit "$UID_" USDT 1500
[ "$(code "$(curl -sS -w '\n%{http_code}' -X POST "$GATEWAY_URL/convert/quote" -d '{}')")" = 401 ] || fail "quote needs auth"

say "quote by fromAmount / toAmount"
Q="$(ok "$(api "$TOK" POST /convert/quote '{"from":"USDT","to":"BTC","fromAmount":"1000"}')" quote)"
eq "$(jq -r .toAmount <<<"$Q")" 0.01996 "toAmount" 0.00000001
eq "$(jq -r .rate <<<"$Q")" 0.00001996 "rate" 0.0000000001
[ "$(jq -r .fee <<<"$Q")" = 0 ] || fail "fee 0"
python3 -c "import sys,time;e=int(sys.argv[1])/1000-time.time();sys.exit(0 if 8<e<=11 else 1)" "$(jq -r .expiresAt <<<"$Q")" || fail "expiresAt ~10s"
Q2="$(ok "$(api "$TOK" POST /convert/quote '{"from":"BTC","to":"USDT","toAmount":"499"}')" quote-to)"
eq "$(jq -r .toAmount <<<"$Q2")" 499 "toAmount fixed"
eq "$(jq -r .fromAmount <<<"$Q2")" 0.01 "fromAmount" 0.00000002
[ "$(errcode "$(api "$TOK" POST /convert/quote '{"from":"USDT","to":"BTC","fromAmount":"5"}')")" = AMOUNT_TOO_SMALL ] || fail "min"
[ "$(errcode "$(api "$TOK" POST /convert/quote '{"from":"USDT","to":"BTC","fromAmount":"60000"}')")" = AMOUNT_TOO_LARGE ] || fail "max"
[ "$(errcode "$(api "$TOK" POST /convert/quote '{"from":"USDT","to":"BTC","fromAmount":"100","toAmount":"1"}')")" = VALIDATION_ERROR ] || fail "both amounts"
[ "$(errcode "$(api "$TOK" POST /convert/quote '{"from":"USDT","to":"DOGE","fromAmount":"100"}')")" = PAIR_NOT_SUPPORTED ] || fail "unknown pair"
R="$(api "$TOK" POST /convert/quote '{"from":"USDT","to":"TRX","fromAmount":"100"}')"
[ "$(code "$R")" = 503 ] && [ "$(errcode "$R")" = PRICE_UNAVAILABLE ] || fail "no index -> PRICE_UNAVAILABLE: $R"
echo "  ok"

say "accept (atomic, idempotent)"
QID="$(jq -r .quoteId <<<"$Q")"
A1="$(ok "$(api "$TOK" POST /convert/accept "{\"quoteId\":\"$QID\"}")" accept)"
[ "$(jq -r .replay <<<"$A1")" = false ] || fail "first accept replay"
A2="$(ok "$(api "$TOK" POST /convert/accept "{\"quoteId\":\"$QID\"}")" accept-replay)"
[ "$(jq -r .replay <<<"$A2")" = true ] || fail "replay flag"
[ "$(jq -r .order.id <<<"$A1")" = "$(jq -r .order.id <<<"$A2")" ] || fail "same order"
eq "$(spot_bal "$TOK" USDT)" 500 "USDT after"
eq "$(spot_bal "$TOK" BTC)" 0.01996 "BTC after"
Q3="$(ok "$(api "$TOK" POST /convert/quote '{"from":"USDT","to":"BTC","fromAmount":"600"}')" quote3)"
[ "$(errcode "$(api "$TOK" POST /convert/accept "{\"quoteId\":\"$(jq -r .quoteId <<<"$Q3")\"}")")" = INSUFFICIENT_BALANCE ] || fail "insufficient"
read -r TOK2 _ < <(register "cv2$TS@example.com")
[ "$(errcode "$(api "$TOK2" POST /convert/accept "{\"quoteId\":\"$(jq -r .quoteId <<<"$Q3")\"}")")" = QUOTE_NOT_FOUND ] || fail "foreign quote"
echo "  ok"

say "cross + stable pairs"
X="$(ok "$(api "$TOK" POST /convert/quote '{"from":"BTC","to":"ETH","fromAmount":"0.01"}')" cross)"
eq "$(jq -r .toAmount <<<"$X")" 0.1996 "BTC->ETH" 0.00000001
ok "$(api "$TOK" POST /convert/accept "{\"quoteId\":\"$(jq -r .quoteId <<<"$X")\"}")" cross-accept >/dev/null
credit "$UID_" USDC 100
S="$(ok "$(api "$TOK" POST /convert/quote '{"from":"USDC","to":"USDT","fromAmount":"100"}')" stable)"
eq "$(jq -r .toAmount <<<"$S")" 100 "USDC->USDT 1:1"
ok "$(api "$TOK" POST /convert/accept "{\"quoteId\":\"$(jq -r .quoteId <<<"$S")\"}")" stable-accept >/dev/null
eq "$(spot_bal "$TOK" USDT)" 600 "USDT after stable"
echo "  ok"

say "expiry"
E="$(ok "$(api "$TOK" POST /convert/quote '{"from":"USDT","to":"BTC","fromAmount":"100"}')" quote-exp)"
sleep 11
R="$(api "$TOK" POST /convert/accept "{\"quoteId\":\"$(jq -r .quoteId <<<"$E")\"}")"
[ "$(code "$R")" = 409 ] && [ "$(errcode "$R")" = QUOTE_EXPIRED ] || fail "expired: $R"
eq "$(spot_bal "$TOK" USDT)" 600 "no move on expiry"
echo "  ok"

say "history"
H="$(ok "$(api "$TOK" GET '/convert/orders?page=1&pageSize=2')" orders)"
[ "$(jq -r .total <<<"$H")" = 3 ] && [ "$(jq '.items|length' <<<"$H")" = 2 ] || fail "paging: $H"
H="$(ok "$(api "$TOK" GET '/convert/orders?asset=USDC')" orders-asset)"
[ "$(jq -r .total <<<"$H")" = 1 ] || fail "asset filter: $H"
echo "  ok"

say "admin config / orders / position / solvency"
SA="$(alogin "$SA_USER" "$SA_PASS" "$SA_TOTP")"; [ -n "$SA" ] || fail "admin login"
[ "$(jq -r .spreadBps <<<"$(A "$SA" GET /admin/v1/convert/config)")" = 20 ] || fail "admin config"
[ "$(jq -r '.code // .error.code' <<<"$(A "$SA" PUT /admin/v1/convert/config '{"spreadBps":50,"totpCode":"000000"}')")" = TOTP_INVALID ] || fail "2FA required"
fresh; C="$(A "$SA" PUT /admin/v1/convert/config "{\"spreadBps\":50,\"minUsdt\":\"20\",\"assets\":[{\"asset\":\"ETH\",\"enabled\":false}],\"reason\":\"smoke\",\"totpCode\":\"$(totp "$SA_TOTP")\"}")"
[ "$(jq -r .spreadBps <<<"$C")" = 50 ] || fail "config update: $C"
Q="$(ok "$(api "$TOK" POST /convert/quote '{"from":"USDT","to":"BTC","fromAmount":"100"}')" quote-50)"
eq "$(jq -r .toAmount <<<"$Q")" 0.00199 "spread 50bps" 0.00000001
[ "$(errcode "$(api "$TOK" POST /convert/quote '{"from":"USDT","to":"BTC","fromAmount":"15"}')")" = AMOUNT_TOO_SMALL ] || fail "new min"
[ "$(errcode "$(api "$TOK" POST /convert/quote '{"from":"USDT","to":"ETH","fromAmount":"100"}')")" = PAIR_NOT_SUPPORTED ] || fail "disabled asset"
fresh; A "$SA" PUT /admin/v1/convert/config "{\"spreadBps\":20,\"minUsdt\":\"10\",\"assets\":[{\"asset\":\"ETH\",\"enabled\":true}],\"reason\":\"smoke restore\",\"totpCode\":\"$(totp "$SA_TOTP")\"}" >/dev/null
O="$(A "$SA" GET "/admin/v1/convert/orders?userId=$UID_")"
[ "$(jq -r .total <<<"$O")" = 3 ] || fail "admin orders: $O"
PZ="$(A "$SA" GET /admin/v1/convert/position)"
jq -e '.items[] | select(.asset=="BTC")' <<<"$PZ" >/dev/null || fail "position: $PZ"
SV="$(A "$SA" POST /admin/v1/solvency/run '{}')"
CP="$(jq -r '.lines[] | select(.asset=="BTC") | .convertPosition' <<<"$SV")"
python3 -c "import sys;from decimal import Decimal as D;sys.exit(0 if D(sys.argv[1])<0 else 1)" "$CP" || fail "solvency BTC convertPosition: $CP"
echo "  ok"

say "USDC depeg guard (pause / resume)"
idx USDCUSDT 0.99
[ "$(errcode "$(api "$TOK" POST /convert/quote '{"from":"USDC","to":"USDT","fromAmount":"20"}')")" = PAIR_PAUSED_DEPEG ] || fail "depeg pause USDC->USDT"
[ "$(errcode "$(api "$TOK" POST /convert/quote '{"from":"USDT","to":"USDC","fromAmount":"20"}')")" = PAIR_PAUSED_DEPEG ] || fail "depeg pause USDT->USDC"
P="$(curl -sS "$GATEWAY_URL/convert/pairs")"
jq -e '.pairs[] | select(.from=="USDC" and .to=="USDT" and .paused==true)' <<<"$P" >/dev/null || fail "pairs paused flag: $P"
tg_wait "USDC 脱锚"
idx USDCUSDT 1.001
ok "$(api "$TOK" POST /convert/quote '{"from":"USDC","to":"USDT","fromAmount":"20"}')" depeg-resume >/dev/null
tg_wait "闪兑恢复"
EV="$(A "$SA" GET '/admin/v1/convert/events?pageSize=50')"
jq -e '[.items[].kind] | index("pair_paused") and index("pair_resumed")' <<<"$EV" >/dev/null || fail "depeg audit events: $EV"
idx USDCUSDT 0.98   # outside the band: USDC<->crypto still trades, at the real index
Q="$(ok "$(api "$TOK" POST /convert/quote '{"from":"USDC","to":"BTC","fromAmount":"100"}')" usdc-real-index)"
eq "$(jq -r .indexRate <<<"$Q")" 0.0000196 "USDC priced at its index" 0.0000000001
idx USDCUSDT 1
echo "  ok"

say "spot hedge: switch off / thin depth partial / fill / alert"
hrun() { internal "$ASSET_URL" POST /internal/convert/hedge/run '{}'; }
NET0="$(jq -r '.items[] | select(.asset=="BTC") | .netPosition' <<<"$(A "$SA" GET /admin/v1/convert/position)")"
python3 -c "import sys;from decimal import Decimal as D;sys.exit(0 if D(sys.argv[1])<D('-0.001') else 1)" "$NET0" || fail "need BTC short to hedge: $NET0"
# devstack is WALLET_CHAIN=mock -> default OFF; nothing must be placed
[ "$(jq '.orders|length' <<<"$(hrun)")" = 0 ] || fail "hedge ran while switched off"
[ "$(jq -r '.code // .error.code' <<<"$(A "$SA" PUT /admin/v1/convert/config "{\"hedge\":{\"enabled\":true},\"totpCode\":\"$(fresh; totp "$SA_TOTP")\"}")")" = VALIDATION_ERROR ] || fail "hedge switch needs reason"
fresh; C="$(A "$SA" PUT /admin/v1/convert/config "{\"hedge\":{\"enabled\":true,\"slippageBps\":50,\"unhedgedAlertUsdt\":\"10\",\"unhedgedAlertMinutes\":1},\"reason\":\"smoke hedge on\",\"totpCode\":\"$(totp "$SA_TOTP")\"}")"
[ "$(jq -r .hedge.enabled <<<"$C")" = true ] || fail "hedge enable: $C"
read -r MTOK MUID < <(register "cvm$TS@example.com")
credit "$MUID" BTC 2
mk() { ok "$(api "$MTOK" POST /spot/orders "{\"symbol\":\"BTCUSDT\",\"side\":\"sell\",\"type\":\"limit\",\"price\":\"$1\",\"quantity\":\"$2\",\"timeInForce\":\"GTC\"}")" "maker $1" >/dev/null; }
mk 50400 0.5     # above the 50 bps cap (50250): must never be taken
mk 50100 0.003   # thin depth inside the cap
R="$(hrun)"
O="$(jq -c '.orders[] | select(.asset=="BTC")' <<<"$R")"
[ "$(jq -r .status <<<"$O")" = partial ] || fail "thin depth partial: $R"
eq "$(jq -r .filledQty <<<"$O")" 0.003 "partial filled qty"
eq "$(jq -r .limitPrice <<<"$O")" 50250 "slippage cap price"
# remainder stays unhedged > 1 min above 10 USDT -> Telegram + audit event
sleep 62; hrun >/dev/null
tg_wait "未对冲敞口"
jq -e '[.items[].kind] | index("unhedged_alert")' <<<"$(A "$SA" GET '/admin/v1/convert/events?kind=unhedged_alert')" >/dev/null || fail "unhedged event"
mk 50150 1       # depth arrives
R="$(hrun)"
[ "$(jq -r '.orders[] | select(.asset=="BTC") | .status' <<<"$R")" = filled ] || fail "hedge fill: $R"
HG="$(A "$SA" GET '/admin/v1/convert/hedges?asset=BTC')"
[ "$(jq -r .total <<<"$HG")" -ge 2 ] || fail "admin hedges: $HG"
jq -e '.items | all(.limitPrice|tonumber <= 50250)' <<<"$HG" >/dev/null || fail "cap: $HG"
PZ="$(A "$SA" GET /admin/v1/convert/position)"
NET="$(jq -r '.items[] | select(.asset=="BTC") | .netPosition' <<<"$PZ")"
python3 -c "import sys;from decimal import Decimal as D;sys.exit(0 if abs(D(sys.argv[1]))<D('0.0002') else 1)" "$NET" || fail "BTC net after hedge: $PZ"
jq -e '.items[] | select(.asset=="BTC") | .residualUsdt | tonumber | fabs < 10' <<<"$PZ" >/dev/null || fail "residualUsdt: $PZ"
SV="$(A "$SA" POST /admin/v1/solvency/run '{}')"
HB="$(jq -r '.lines[] | select(.asset=="BTC") | .hedgeBalance' <<<"$SV")"
python3 -c "import sys;from decimal import Decimal as D;sys.exit(0 if D(sys.argv[1])>0 else 1)" "$HB" || fail "solvency hedgeBalance: $SV"
# switch off again: new exposure is not hedged
credit "$UID_" USDT 500
fresh; A "$SA" PUT /admin/v1/convert/config "{\"hedge\":{\"enabled\":false},\"reason\":\"smoke hedge off\",\"totpCode\":\"$(totp "$SA_TOTP")\"}" >/dev/null
ok "$(api "$TOK" POST /convert/accept "$(jq -c '{quoteId}' <<<"$(ok "$(api "$TOK" POST /convert/quote '{"from":"USDT","to":"BTC","fromAmount":"100"}')" q-off)")")" accept-off >/dev/null
[ "$(jq '.orders|length' <<<"$(hrun)")" = 0 ] || fail "hedge ran after switch off"
echo "  ok"

printf '\n\033[1;32mSMOKE-CONVERT PASSED\033[0m\n'
