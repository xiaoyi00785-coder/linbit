#!/usr/bin/env bash
#
# Smoke for home operations + general transfer:
#   placements CRUD via admin (2FA + reason + audit), /home/placements filters,
#   /home/banners alias, earn APR on a placement, hot list + pins,
#   /home/newcomer (anon + authed), /app/features + admin toggle,
#   /market/fiat-rates, /account/favorites, POST /assets/transfer (idempotent,
#   SAME_ACCOUNT, earn, INSUFFICIENT_BALANCE, VALIDATION_ERROR), unified history
#   incl. legacy /futures/transfer.
#
#   bash scripts/smoke-home-transfer.sh      # boots devstack (honours DEVSTACK_*_PORT)
#   SMOKE_INNER=1 GATEWAY_URL=... ACCOUNT_URL=... ASSET_URL=... ADMIN_URL=... EARN_URL=... \
#     INTERNAL_TOKEN=... ADMIN_BOOTSTRAP_*=... bash scripts/smoke-home-transfer.sh
#
set -euo pipefail
cd "$(dirname "$0")/.."

if [ "${SMOKE_INNER:-}" != "1" ]; then
  ROOT="$(pwd)"; cd tools/devstack
  exec go run . -root "$ROOT" -exec "SMOKE_INNER=1 bash scripts/smoke-home-transfer.sh"
fi

: "${GATEWAY_URL:?}" "${ACCOUNT_URL:?}" "${ASSET_URL:?}" "${ADMIN_URL:?}" "${EARN_URL:?}" "${INTERNAL_TOKEN:?}"
SA_USER="${ADMIN_BOOTSTRAP_USERNAME:?}"; SA_PASS="${ADMIN_BOOTSTRAP_PASSWORD:?}"; SA_TOTP="${ADMIN_BOOTSTRAP_TOTP_SECRET:?}"
PASSWORD="Str0ng-Passw0rd!"
TS="$(date +%s%N)"

say()  { printf '\n\033[1;34m== %s ==\033[0m\n' "$1"; }
fail() { printf '\033[1;31mSMOKE-HOME-TRANSFER FAILED: %s\033[0m\n' "$1" >&2; exit 1; }
eq() { python3 -c 'import sys; from decimal import Decimal as D; sys.exit(0 if abs(D(sys.argv[1])-D(sys.argv[2]))<=D("0.000001") else 1)' "$1" "$2" || fail "$3: got $1, want $2"; }

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

SA="$(curl -sS -X POST "$ADMIN_URL/admin/v1/auth/login" -H 'Content-Type: application/json' \
  -d "{\"username\":\"$SA_USER\",\"password\":\"$SA_PASS\",\"totpCode\":\"$(totp "$SA_TOTP")\"}" | jq -r '.token // empty')"
[ -n "$SA" ] || fail "admin login"

say "placements: admin guards"
P='{"slot":"popup","title":"Welcome","imageUrl":"https://cdn.example.com/p.png","imageDarkUrl":"https://cdn.example.com/p-dark.png","linkUrl":"/rewards/newcomer","platform":"app","audience":"new","regions":["SG"],"appMinVersion":"1.2.0","frequency":"once","trackingId":"pop-'"$TS"'","countdownEndAt":"2030-01-01T00:00:00Z","sort":1}'
expect "$(A POST /admin/v1/home/placements "$(jq -c '. + {totpCode:"000000"}' <<<"$P")")" 400 REASON_REQUIRED "no reason"
r="$(A POST /admin/v1/home/placements "$(jq -c '. + {totpCode:"000000",reason:"x"}' <<<"$P")")"
[ "$(code "$r")" = "400" ] || [ "$(code "$r")" = "401" ] || [ "$(code "$r")" = "403" ] || fail "bad 2FA accepted: $r"
expect "$(A POST /admin/v1/home/placements "$(w smoke '{"slot":"footer","title":"x"}')")" 400 VALIDATION_ERROR "bad slot"
expect "$(A POST /admin/v1/home/placements "$(w smoke '{"slot":"carousel","title":"x","imageUrl":"javascript:alert(1)"}')")" 400 VALIDATION_ERROR "js url"

say "placements: create"
POP="$(ok "$(A POST /admin/v1/home/placements "$(w 'launch popup' "$P")")" "create popup" | jq -r .placement.id)"
EP="earn-home-$TS"
ok "$(curl -sS -w '\n%{http_code}' -X POST "$EARN_URL/internal/earn/products" -H 'Content-Type: application/json' -H "X-Internal-Token: $INTERNAL_TOKEN" \
  -d "{\"id\":\"$EP\",\"kind\":\"fixed\",\"asset\":\"USDT\",\"title\":\"home promo\",\"apr\":\"0.123\",\"termDays\":7,\"minAmount\":\"1\",\"sort\":99}")" "earn product" >/dev/null
PROMO="$(ok "$(A POST /admin/v1/home/placements "$(w promo "{\"slot\":\"promo_bar\",\"title\":\"Earn\",\"linkUrl\":\"/earn\",\"earnProductId\":\"$EP\",\"content\":{\"text\":\"APR up to {apr}\"}}")")" promo | jq -r .placement.id)"
ok "$(A POST /admin/v1/home/placements "$(w hint '{"slot":"search_hint","title":"BTC","content":{"keyword":"BTCUSDT"}}')")" hint >/dev/null
BAN="$(ok "$(A POST /admin/v1/banners "$(w banner '{"title":"Banner '"$TS"'","imageUrl":"https://cdn.example.com/b.png","linkUrl":"https://example.com/x","platform":"all"}')")" banner | jq -r .placement.id)"
[ "$(ok "$(A GET /admin/v1/banners)" list | jq --argjson i "$BAN" '[.items[]|select(.id==$i and .slot=="carousel")]|length')" = "1" ] || fail "banner alias not carousel"

say "placements: public filters"
pub() { ok "$(api - GET "/home/placements?$1")" "placements $1"; }
sleep 0 # writes invalidate the cache
R="$(pub 'slots=popup&platform=app&appVersion=1.3.0&region=SG')"
[ "$(jq --argjson i "$POP" '[.slots.popup[]|select(.id==$i)]|length' <<<"$R")" = "1" ] || fail "popup not visible: $R"
jq -e --argjson i "$POP" '.slots.popup[]|select(.id==$i)|.imageDarkUrl!="" and .frequency=="once" and .countdownEndAt!=null and (.trackingId|startswith("pop-")) and (has("updatedBy")|not)' <<<"$R" >/dev/null || fail "popup fields: $R"
for q in 'platform=app&appVersion=1.1.9&region=SG' 'platform=app&region=SG' 'platform=app&appVersion=1.3.0&region=HK' 'platform=web&region=SG' 'platform=app&appVersion=1.3.0&region=SG&audience=existing'; do
  [ "$(pub "slots=popup&$q" | jq --argjson i "$POP" '[.slots.popup[]|select(.id==$i)]|length')" = "0" ] || fail "popup leaked for $q"
done
R="$(pub 'slots=promo_bar,search_hint,carousel')"
[ "$(jq '.slots|keys|sort' -c <<<"$R")" = '["carousel","promo_bar","search_hint"]' ] || fail "slot keys: $R"
eq "$(jq -r --argjson i "$PROMO" '.slots.promo_bar[]|select(.id==$i).earnApr' <<<"$R")" 0.123 "earn APR from earn service"
expect "$(api - GET '/home/placements?slots=footer')" 400 VALIDATION_ERROR "unknown slot"
[ "$(ok "$(api - GET '/home/banners?platform=web')" banners | jq --argjson i "$BAN" '[.items[]|select(.id==$i)]|length')" = "1" ] || fail "banner alias public"

say "placements: update/delete + audit"
ok "$(A PUT "/admin/v1/home/placements/$BAN" "$(w 'disable' '{"slot":"carousel","title":"off","imageUrl":"https://cdn.example.com/b.png","enabled":false}')")" disable >/dev/null
[ "$(ok "$(api - GET /home/banners)" banners | jq --argjson i "$BAN" '[.items[]|select(.id==$i)]|length')" = "0" ] || fail "disabled banner public"
ok "$(A DELETE "/admin/v1/banners/$BAN" "$(w 'cleanup' '{}')")" delete >/dev/null
expect "$(A DELETE "/admin/v1/home/placements/$BAN" "$(w 'again' '{}')")" 404 NOT_FOUND "delete twice"
AL="$(ok "$(A GET '/admin/v1/audit-logs?module=content&size=50')" audit)"
for a in create_placement create_banner update_placement delete_banner; do
  jq -e --arg a "$a" '[.items[]|select(.action==$a and .level=="high" and .result=="ok")]|length>0' <<<"$AL" >/dev/null || fail "audit $a missing"
done

say "hot list + pins"
ok "$(A PUT /admin/v1/home/hot-pins/gainers "$(w pin '{"symbols":["ETHUSDT"]}')")" pin >/dev/null
H="$(ok "$(api - GET '/home/hot?type=gainers')" hot)"
[ "$(jq -r '.items[0].symbol + ":" + (.items[0].pinned|tostring)' <<<"$H")" = "ETHUSDT:true" ] || fail "pin first: $H"
for t in hot losers new volume; do ok "$(api - GET "/home/hot?type=$t")" "hot $t" >/dev/null; done
expect "$(api - GET '/home/hot?type=bogus')" 400 VALIDATION_ERROR "hot type"
expect "$(A PUT /admin/v1/home/hot-pins/gainers "$(w pin '{"symbols":["NOPEUSDT"]}')")" 400 VALIDATION_ERROR "unlisted pin"
ok "$(A PUT /admin/v1/home/hot-pins/gainers "$(w unpin '{"symbols":[]}')")" unpin >/dev/null

say "app features"
F="$(ok "$(api - GET /app/features)" features)"
jq -e '.features.convert==true and .features.tokenizedStocks==false and .features.chat==false and .features.subAccounts==false' <<<"$F" >/dev/null || fail "feature defaults: $F"
expect "$(A PUT /admin/v1/app/features '{"features":{"chat":true}}')" 400 REASON_REQUIRED "features reason"
expect "$(A PUT /admin/v1/app/features "$(w x '{"features":{"bogus":true}}')")" 400 VALIDATION_ERROR "unknown feature"
r="$(ok "$(A PUT /admin/v1/app/features "$(w 'chat beta' '{"features":{"chat":true}}')")" "features put")"
jq -e '.before.chat==false and .after.chat==true' <<<"$r" >/dev/null || fail "features before/after: $r"
[ "$(ok "$(api - GET /app/features)" features | jq .features.chat)" = "true" ] || fail "feature toggle not visible"
ok "$(A PUT /admin/v1/app/features "$(w 'revert' '{"features":{"chat":false}}')")" revert >/dev/null

say "fiat rates"
R="$(ok "$(api - GET '/market/fiat-rates?base=USDT')" fiat)"
jq -e '.base=="USDT" and .rates.USD==1 and (.rates.CNY>0) and (.rates.EUR>0) and (.rates.JPY>0) and (.source|length>0)' <<<"$R" >/dev/null || fail "fiat: $R"
echo "fiat source=$(jq -r .source <<<"$R")"
expect "$(api - GET '/market/fiat-rates?base=BTC')" 400 VALIDATION_ERROR "fiat base"

say "newcomer"
N="$(ok "$(api - GET /home/newcomer)" "newcomer anon")"
jq -e '.loggedIn==false and .cta=="login" and (.tasks|length>0) and all(.tasks[]; .status=="locked")' <<<"$N" >/dev/null || fail "anon newcomer: $N"
TOK="$(register "home$TS@example.com")"; [ -n "$TOK" ] || fail register
UID_="$(sub "$TOK")"
N="$(ok "$(api "$TOK" GET /home/newcomer)" "newcomer authed")"
jq -e '.loggedIn==true and (.cta=="kyc" or .cta=="enroll" or .cta=="open")' <<<"$N" >/dev/null || fail "authed newcomer: $N"
[ "$(code "$(api garbage GET /home/newcomer)")" = "200" ] || fail "bad token must be anonymous"

say "favorites"
expect "$(api - GET /account/favorites)" 401 "" "favorites anon"
r="$(ok "$(api "$TOK" PUT /account/favorites '{"symbols":["ethusdt","BTCUSDT","ETHUSDT"]}')" fav-put)"
[ "$(jq -c .symbols <<<"$r")" = '["ETHUSDT","BTCUSDT"]' ] || fail "fav replace: $r"
ok "$(api "$TOK" PUT /account/favorites/SOLUSDT)" fav-add >/dev/null
r="$(ok "$(api "$TOK" PUT /account/favorites/SOLUSDT)" fav-add-again)"
[ "$(jq -c .symbols <<<"$r")" = '["ETHUSDT","BTCUSDT","SOLUSDT"]' ] || fail "fav add idempotent: $r"
ok "$(api "$TOK" PUT '/account/favorites/BTCUSDT?market=futures')" fav-fut >/dev/null
r="$(ok "$(api "$TOK" DELETE /account/favorites/BTCUSDT)" fav-del)"
[ "$(jq -c .symbols <<<"$r")" = '["ETHUSDT","SOLUSDT"]' ] || fail "fav delete: $r"
[ "$(ok "$(api "$TOK" GET /account/favorites)" fav-all | jq '.items|length')" = "3" ] || fail "fav all markets"
expect "$(api "$TOK" PUT '/account/favorites/BTC-USDT')" 400 VALIDATION_ERROR "fav symbol"
expect "$(api "$TOK" PUT '/account/favorites/BTCUSDT?market=earn')" 400 VALIDATION_ERROR "fav market"

say "general transfer"
c="$(curl -sS -o /dev/null -w '%{http_code}' -X POST "$ASSET_URL/internal/credit" -H 'Content-Type: application/json' -H "X-Internal-Token: $INTERNAL_TOKEN" \
  -d "{\"userId\":\"$UID_\",\"asset\":\"USDT\",\"amount\":\"1000\",\"ref\":\"smoke-home-$UID_\"}")"
[ "$c" = "200" ] || fail "credit $c"
expect "$(api - POST /assets/transfer '{}')" 401 "" "anon transfer"
T='{"asset":"USDT","amount":"100","from":"spot","to":"futures","clientId":"c1-'"$TS"'"}'
r1="$(ok "$(api "$TOK" POST /assets/transfer "$T")" transfer)"
jq -e '.status=="done" and .from=="spot" and .to=="futures" and .asset=="USDT"' <<<"$r1" >/dev/null || fail "transfer: $r1"
r2="$(ok "$(api "$TOK" POST /assets/transfer "$T")" replay)"
[ "$(jq -r .id <<<"$r1")" = "$(jq -r .id <<<"$r2")" ] || fail "replay created new transfer"
expect "$(api "$TOK" POST /assets/transfer "$(jq -c '.amount="5"' <<<"$T")")" 409 IDEMPOTENCY_CONFLICT "clientId reuse"
expect "$(api "$TOK" POST /assets/transfer '{"asset":"USDT","amount":"1","from":"spot","to":"spot","clientId":"s'"$TS"'"}')" 400 SAME_ACCOUNT same
expect "$(api "$TOK" POST /assets/transfer '{"asset":"USDT","amount":"1","from":"spot","to":"earn","clientId":"e'"$TS"'"}')" 400 ACCOUNT_NOT_SUPPORTED earn
expect "$(api "$TOK" POST /assets/transfer '{"asset":"BTC","amount":"1","from":"spot","to":"futures","clientId":"b'"$TS"'"}')" 400 VALIDATION_ERROR asset
expect "$(api "$TOK" POST /assets/transfer '{"asset":"USDT","amount":"1","from":"spot","to":"futures"}')" 400 VALIDATION_ERROR clientId
expect "$(api "$TOK" POST /assets/transfer '{"asset":"USDT","amount":"5000","from":"spot","to":"futures","clientId":"i'"$TS"'"}')" 400 INSUFFICIENT_BALANCE "spot insufficient"
expect "$(api "$TOK" POST /assets/transfer '{"asset":"USDT","amount":"5000","from":"futures","to":"spot","clientId":"j'"$TS"'"}')" 400 INSUFFICIENT_BALANCE "futures insufficient"
ok "$(api "$TOK" POST /assets/transfer '{"asset":"USDT","amount":"30","from":"futures","to":"spot","clientId":"c2-'"$TS"'"}')" out >/dev/null
ok "$(api "$TOK" POST /futures/transfer '{"direction":"in","amount":"10"}')" legacy >/dev/null
eq "$(ok "$(api "$TOK" GET /assets/balances)" bal | jq -r '.items[]|select(.asset=="USDT").free')" 920 "spot after transfers"
eq "$(ok "$(api "$TOK" GET /futures/account)" facct | jq -r .walletBalance)" 80 "futures after transfers"
Hs="$(ok "$(api "$TOK" GET '/assets/transfers?page=1&pageSize=10&asset=USDT')" history)"
jq -e '.total==4 and (.items|length)==4 and ([.items[]|select(.status=="failed")]|length)==1 and ([.items[]|select(.clientId==null)]|length)==1 and .items[0].createdAt!=null' <<<"$Hs" >/dev/null || fail "history: $Hs"
[ "$(ok "$(api "$TOK" GET '/assets/transfers?page=1&pageSize=1')" page | jq '.items|length')" = "1" ] || fail "pageSize"

say "OK"
printf '\033[1;32mSMOKE-HOME-TRANSFER PASSED\033[0m\n'
