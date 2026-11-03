#!/usr/bin/env bash
#
# Smoke for the iOS spot/asset gaps: funding vs spot accounts (/assets/overview,
# /assets/transfer), /wallet/networks, post-only (GTX/postOnly), conditional spot
# orders, /account/fee-rates (+ per-user override) and internal transfer.
#
#   bash scripts/smoke-spot-gaps.sh            # boots the devstack itself
#   SMOKE_INNER=1 GATEWAY_URL=... bash scripts/smoke-spot-gaps.sh   # running stack
#
# When running several devstacks side by side, set DEVSTACK_*_PORT (e.g. +700:
# DEVSTACK_GATEWAY_PORT=8780 ... DEVSTACK_PG_PORT=6133).
set -euo pipefail
cd "$(dirname "$0")/.."

if [ "${SMOKE_INNER:-}" != "1" ]; then
  ROOT="$(pwd)"
  cd tools/devstack
  exec go run . -root "$ROOT" -exec "SMOKE_INNER=1 bash scripts/smoke-spot-gaps.sh"
fi

GATEWAY_URL="${GATEWAY_URL:-http://localhost:8080}"
ACCOUNT_URL="${ACCOUNT_URL:-http://localhost:8081}"
ASSET_URL="${ASSET_URL:-http://localhost:8082}"
WALLET_URL="${WALLET_URL:-http://localhost:8086}"
ORDER_URL="${ORDER_URL:-http://localhost:8084}"
FUTURES_URL="${FUTURES_INTERNAL_URL:-http://localhost:8089}"
GROWTH_URL="${GROWTH_URL:-}"
ADMIN_URL="${ADMIN_URL:-}"
INTERNAL_TOKEN="${INTERNAL_TOKEN:?INTERNAL_TOKEN required}"
PASSWORD="Str0ng-Passw0rd!"
TS="$(date +%s)"
SMOKE_NAME="SPOT-GAPS"
# shellcheck source=lib-growth-smoke.sh
. scripts/lib-growth-smoke.sh

expect_err() { # expect_err <resp> <http> <code> <label>
  [ "$(code "$1")" = "$2" ] && [ "$(errcode "$1")" = "$3" ] || fail "$4: want $2 $3, got $(code "$1") $(body "$1")"
  printf '  ok  %s -> %s\n' "$4" "$3"
}
acct_free() { # acct_free <token> <account> <asset>
  ok "$(api "$1" GET /assets/overview)" overview | jq -r --arg a "$2" --arg s "$3" \
    '([.accounts[] | select(.account==$a) | .balances[] | select(.asset==$s) | .free][0]) // "0"'
}

say "users"
read -r TA UA < <(register "sga+$TS@example.com")
read -r TB UB < <(register "sgb+$TS@example.com")
credit "$UA" USDT 1000
credit "$UA" BTC 0.01
credit "$UB" USDT 1000

say "1. overview + funding transfer"
OV="$(ok "$(api "$TA" GET /assets/overview)" overview)"
jq -e '.accounts | map(.account) == ["funding","spot","futures"]' <<<"$OV" >/dev/null || fail "overview accounts: $OV"
jq -e '.depositAccount == "funding" and .withdrawAccount == "funding"' <<<"$OV" >/dev/null || fail "overview defaults: $OV"
ok "$(api "$TA" POST /assets/transfer "{\"clientId\":\"sg-$TS-1\",\"asset\":\"USDT\",\"amount\":\"100\",\"from\":\"spot\",\"to\":\"funding\"}")" "spot->funding" >/dev/null
eq "$(acct_free "$TA" funding USDT)" 100 "funding USDT after transfer"
ok "$(api "$TA" POST /assets/transfer "{\"clientId\":\"sg-$TS-1\",\"asset\":\"USDT\",\"amount\":\"100\",\"from\":\"spot\",\"to\":\"funding\"}")" "replay" >/dev/null
eq "$(acct_free "$TA" funding USDT)" 100 "idempotent replay"
expect_err "$(api "$TA" POST /assets/transfer "{\"clientId\":\"sg-$TS-2\",\"asset\":\"BTC\",\"amount\":\"0.001\",\"from\":\"spot\",\"to\":\"futures\"}")" 400 VALIDATION_ERROR "BTC to futures"

say "2. wallet networks"
NW="$(ok "$(api "$TA" GET "/wallet/networks?asset=USDT")" networks)"
jq -e '.networks | length > 0 and all(has("minDeposit") and has("confirmations") and has("memoRequired") and has("withdrawFee") and has("minWithdraw") and has("depositEnabled") and has("withdrawEnabled"))' <<<"$NW" >/dev/null || fail "networks shape: $NW"
NET="$(jq -r '.networks[0].network' <<<"$NW")"
internal "$WALLET_URL" PUT "/internal/admin/wallet/networks/USDT/$NET" '{"depositEnabled":false,"updatedBy":"smoke"}' | jq -e '.after' >/dev/null || fail "admin network put"
expect_err "$(api "$TA" GET "/wallet/deposit/address?asset=USDT&network=$NET")" 400 DEPOSIT_DISABLED "deposit disabled"
internal "$WALLET_URL" PUT "/internal/admin/wallet/networks/USDT/$NET" '{"reset":true,"updatedBy":"smoke"}' >/dev/null
ok "$(api "$TA" GET "/wallet/deposit/address?asset=USDT&network=$NET")" "deposit address re-enabled" | jq -e 'has("memoRequired")' >/dev/null || fail "address memoRequired"

say "3a. post-only"
ok "$(api "$TA" POST /spot/orders '{"symbol":"BTCUSDT","side":"sell","type":"limit","price":"70000","quantity":"0.001"}')" "maker sell" >/dev/null
expect_err "$(api "$TB" POST /spot/orders '{"symbol":"BTCUSDT","side":"buy","type":"limit","price":"70000","quantity":"0.001","timeInForce":"GTX"}')" 400 POST_ONLY_REJECTED "GTX crossing"
expect_err "$(api "$TB" POST /spot/orders '{"symbol":"BTCUSDT","side":"buy","type":"limit","price":"70000","quantity":"0.001","postOnly":true}')" 400 POST_ONLY_REJECTED "postOnly crossing"
PO="$(ok "$(api "$TB" POST /spot/orders '{"symbol":"BTCUSDT","side":"buy","type":"limit","price":"60000","quantity":"0.001","postOnly":true}')" "postOnly rest")"
POID="$(jq -r .orderId <<<"$PO")"
ok "$(api "$TB" DELETE "/spot/orders/$POID")" "cancel postOnly" >/dev/null
ok "$(api "$TB" POST /spot/orders '{"symbol":"BTCUSDT","side":"buy","type":"limit","price":"70000","quantity":"0.001","timeInForce":"IOC"}')" "taker buy (sets last=70000)" >/dev/null

say "3b. conditional orders"
BTC0="$(spot_bal "$TA" BTC)"
C="$(ok "$(api "$TA" POST /spot/orders/conditional '{"symbol":"BTCUSDT","side":"sell","type":"limit","kind":"stop","triggerPrice":"65000","price":"64000","quantity":"0.001"}')" "stop-limit create")"
CID="$(jq -r .id <<<"$C")"
[ "$(jq -r '.status+"/"+.triggerDirection' <<<"$C")" = "pending/down" ] || fail "stop create: $C"
eq "$(spot_bal "$TA" BTC)" "$(python3 -c "print($BTC0-0.001)")" "BTC frozen by stop"
body "$(api "$TA" GET /spot/orders/conditional)" | jq -e --arg id "$CID" '.items | any(.id==$id)' >/dev/null || fail "list conditional"
ok "$(api "$TA" DELETE "/spot/orders/conditional/$CID")" "cancel" | jq -e '.status=="canceled"' >/dev/null || fail "cancel status"
eq "$(spot_bal "$TA" BTC)" "$BTC0" "BTC released by cancel"
expect_err "$(api "$TA" POST /spot/orders/conditional '{"symbol":"BTCUSDT","side":"sell","type":"market","kind":"stop","triggerPrice":"75000","quantity":"0.001"}')" 400 TRIGGER_WOULD_IMMEDIATELY_FIRE "stop above last"
T="$(ok "$(api "$TB" POST /spot/orders/conditional '{"symbol":"BTCUSDT","side":"buy","type":"limit","kind":"trigger","triggerPrice":"71000","price":"71000","quantity":"0.001"}')" "trigger create")"
TID="$(jq -r .id <<<"$T")"
ok "$(api "$TA" POST /spot/orders '{"symbol":"BTCUSDT","side":"sell","type":"limit","price":"71000","quantity":"0.002"}')" "sell 0.002@71000" >/dev/null
ok "$(api "$TB" POST /spot/orders '{"symbol":"BTCUSDT","side":"buy","type":"limit","price":"71000","quantity":"0.001","timeInForce":"IOC"}')" "trade @71000" >/dev/null
for _ in $(seq 1 10); do
  ST="$(body "$(api "$TB" GET "/spot/orders/conditional?status=all")" | jq -r --arg id "$TID" '.items[] | select(.id==$id) | .status')"
  [ "$ST" = "triggered" ] && break; sleep 1
done
[ "$ST" = "triggered" ] || fail "trigger did not fire (status=$ST)"
echo "  ok  trigger fired"

say "4. fee rates"
FR="$(ok "$(api "$TA" GET "/account/fee-rates?symbol=BTCUSDT")" fee-rates)"
jq -e '.vipLevel==0 and (.spot.maker|length>0) and .overridden==false' <<<"$FR" >/dev/null || fail "fee-rates: $FR"
internal "$ORDER_URL" PUT "/internal/admin/fee-overrides/$UA" '{"maker":"0","taker":"0.0005","note":"smoke","updatedBy":"smoke"}' >/dev/null
FR="$(ok "$(api "$TA" GET "/account/fee-rates?symbol=BTCUSDT")" fee-rates)"
jq -e '.overridden and .spot.maker=="0" and .spot.taker=="0.0005"' <<<"$FR" >/dev/null || fail "override: $FR"
internal "$ORDER_URL" DELETE "/internal/admin/fee-overrides/$UA" >/dev/null

say "5. internal transfer"
SECRET="$(ok "$(api "$TA" POST /2fa/totp/bind '{}')" bind | jq -r .secret)"
ok "$(api "$TA" POST /2fa/totp/verify "{\"code\":\"$(totp "$SECRET")\"}")" "totp verify" >/dev/null
IT() { api "$TA" POST /wallet/internal-transfer "{\"clientId\":\"$1\",\"asset\":\"USDT\",\"amount\":\"$2\",$3,\"totpCode\":\"$(totp "$SECRET")\"}"; }
B0="$(acct_free "$TB" funding USDT)"
expect_err "$(api "$TA" POST /wallet/internal-transfer "{\"clientId\":\"it-$TS-0\",\"asset\":\"USDT\",\"amount\":\"5\",\"toUid\":\"$UB\",\"totpCode\":\"000000\"}")" 401 TWOFA_REQUIRED "bad totp"
expect_err "$(IT "it-$TS-x" 5 "\"toUid\":\"$UA\"")" 400 SELF_TRANSFER "self"
expect_err "$(IT "it-$TS-y" 5 "\"toEmail\":\"nobody+$TS@example.com\"")" 404 RECIPIENT_NOT_FOUND "unknown recipient"
R="$(ok "$(IT "it-$TS-1" 5 "\"toUid\":\"$UB\"")" "internal transfer")"
jq -e '.status=="completed" and .fee=="0"' <<<"$R" >/dev/null || fail "itx: $R"
ok "$(IT "it-$TS-1" 5 "\"toUid\":\"$UB\"")" "replay" >/dev/null
expect_err "$(IT "it-$TS-1" 6 "\"toUid\":\"$UB\"")" 409 IDEMPOTENCY_CONFLICT "replay mismatch"
eq "$(acct_free "$TA" funding USDT)" 95 "sender funding"
eq "$(acct_free "$TB" funding USDT)" "$(python3 -c "print($B0+5)")" "recipient funding"
internal "$WALLET_URL" PUT /internal/admin/internal-transfer/config '{"dailyLimitUsdt":"10","updatedBy":"smoke"}' >/dev/null
expect_err "$(IT "it-$TS-2" 6 "\"toUid\":\"$UB\"")" 400 INTERNAL_TRANSFER_LIMIT_EXCEEDED "daily limit"
internal "$WALLET_URL" PUT /internal/admin/internal-transfer/config '{"enabled":false,"dailyLimitUsdt":"50000","updatedBy":"smoke"}' >/dev/null
expect_err "$(IT "it-$TS-3" 1 "\"toUid\":\"$UB\"")" 403 INTERNAL_TRANSFER_DISABLED "disabled"
internal "$WALLET_URL" PUT /internal/admin/internal-transfer/config '{"enabled":true,"updatedBy":"smoke"}' >/dev/null

printf '\n\033[1;32mSMOKE-SPOT-GAPS PASSED\033[0m\n'
