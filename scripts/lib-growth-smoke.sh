# Shared helpers for the growth smokes (sourced). Expects GATEWAY_URL,
# ACCOUNT_URL, ASSET_URL, FUTURES_URL, GROWTH_URL, ADMIN_URL, INTERNAL_TOKEN,
# PASSWORD, TS and SMOKE_NAME.

say()  { printf '\n\033[1;34m== %s ==\033[0m\n' "$1"; }
fail() { printf '\033[1;31mSMOKE-%s FAILED: %s\033[0m\n' "$SMOKE_NAME" "$1" >&2; exit 1; }
command -v jq >/dev/null 2>&1 || fail "jq is required"
command -v python3 >/dev/null 2>&1 || fail "python3 is required"

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
eq() {
  python3 -c 'import sys; from decimal import Decimal as D; sys.exit(0 if abs(D(sys.argv[1])-D(sys.argv[2]))<=D(sys.argv[3]) else 1)' "$1" "$2" "${4:-0.000001}" \
    || fail "$3: got $1, want $2"
}
jwt_sub() {
  python3 - "$(printf '%s' "$1" | cut -d. -f2)" <<'PY'
import base64, json, sys
p = sys.argv[1]; p += "=" * (-len(p) % 4)
print(json.loads(base64.urlsafe_b64decode(p))["sub"])
PY
}
# register <email> [inviteCode] -> "token sub"
register() {
  local email="$1" invite="${2:-}" code resp token
  curl -sS -o /dev/null -X POST "$GATEWAY_URL/auth/code/send" -H 'Content-Type: application/json' \
    -d "{\"purpose\":\"register\",\"email\":\"$email\"}"
  code="$(curl -sS -G "$ACCOUNT_URL/internal/dev/last-code" --data-urlencode "target=$email" \
    --data-urlencode "purpose=register" -H "X-Internal-Token: $INTERNAL_TOKEN" | jq -r '.code // empty')"
  [ -n "$code" ] || fail "no register code for $email"
  resp="$(curl -sS -X POST "$GATEWAY_URL/auth/register" -H 'Content-Type: application/json' \
    -d "{\"email\":\"$email\",\"password\":\"$PASSWORD\",\"code\":\"$code\",\"inviteCode\":\"$invite\"}")"
  token="$(jq -r '.accessToken // empty' <<<"$resp")"
  [ -n "$token" ] || fail "register $email: $resp"
  printf '%s %s\n' "$token" "$(jwt_sub "$token")"
}
credit() { # credit <uid> <asset> <amount>
  local c
  c="$(curl -sS -o /dev/null -w '%{http_code}' -X POST "$ASSET_URL/internal/credit" -H 'Content-Type: application/json' \
    -H "X-Internal-Token: $INTERNAL_TOKEN" -d "{\"userId\":\"$1\",\"asset\":\"$2\",\"amount\":\"$3\",\"ref\":\"smoke-$TS-$1-$2\"}")"
  [ "$c" = "200" ] || fail "credit $1 $2 returned $c"
}
api() {
  curl -sS -w '\n%{http_code}' -X "$2" "$GATEWAY_URL$3" -H 'Content-Type: application/json' \
    -H "Authorization: Bearer $1" ${4:+-d "$4"}
}
body() { sed '$d' <<<"$1"; }
code() { tail -n1 <<<"$1"; }
ok() {
  local c; c="$(code "$1")"
  case "$c" in 2*) body "$1" ;; *) fail "$2: HTTP $c $(body "$1")" ;; esac
}
errcode() { body "$1" | jq -r '.code // .error.code // empty'; }
internal() { # internal <base> <METHOD> <path> [json]
  curl -sS -X "$2" "$1$3" -H 'Content-Type: application/json' -H "X-Internal-Token: $INTERNAL_TOKEN" ${4:+-d "$4"}
}
gsync() { internal "$GROWTH_URL" POST /internal/dev/growth/sync >/dev/null; }
gsettle() { internal "$GROWTH_URL" POST /internal/dev/growth/settle; }
spot_bal() { ok "$(api "$1" GET "/assets/balances?pageSize=100")" balances | jq -r --arg a "${2:-USDT}" '([.items[] | select(.asset==$a) | .free][0]) // "0"'; }
fut_order() { ok "$(api "$1" POST /futures/orders "$2")" "futures order $2" >/dev/null; }
fut_lev() { ok "$(api "$1" POST /futures/leverage "{\"symbol\":\"BTCUSDT\",\"leverage\":$2,\"marginMode\":\"cross\"}")" "leverage" >/dev/null; }
fut_in() { ok "$(api "$1" POST /futures/transfer "{\"direction\":\"in\",\"amount\":\"$2\"}")" "futures transfer" >/dev/null; }
# fut_round <maker-token> <taker-token> <qty>: taker opens long and closes it
# against resting maker orders at 50000 (taker pays taker fees both ways).
fut_round() {
  fut_order "$1" "{\"symbol\":\"BTCUSDT\",\"side\":\"sell\",\"type\":\"limit\",\"price\":\"50000\",\"qty\":\"$3\"}"
  fut_order "$2" "{\"symbol\":\"BTCUSDT\",\"side\":\"buy\",\"type\":\"market\",\"qty\":\"$3\"}"
  fut_order "$1" "{\"symbol\":\"BTCUSDT\",\"side\":\"buy\",\"type\":\"limit\",\"price\":\"50000\",\"qty\":\"$3\"}"
  fut_order "$2" "{\"symbol\":\"BTCUSDT\",\"side\":\"sell\",\"type\":\"market\",\"qty\":\"$3\",\"reduceOnly\":true}"
}
