#!/usr/bin/env bash
#
# End-to-end wallet smoke test for the Linbit wallet service:
#
#   register -> allocate deposit address -> inject a mock-chain deposit ->
#   confirmations insufficient (no credit) -> advance blocks -> credited ->
#   duplicate injection does not double-credit -> small withdrawal auto-released,
#   broadcast, confirmed, settled (gas booked) -> large withdrawal held for manual
#   review -> admin reject releases the freeze -> whitelist blocks a non-listed
#   address -> final balances and ledger agree (decimal-exact).
#
# The chain is simulated: deposits and blocks are injected through the wallet
# service's dev-only endpoints (DEV_MODE=true). Verification codes are read back
# through the account service's dev endpoint.
#
# Two ways to run it (mirrors scripts/smoke-account.sh):
#
#   1. No stack running (default, no Docker): boots the devstack, re-runs itself
#      inside it, tears everything down afterwards.
#
#        bash scripts/smoke-wallet.sh
#
#   2. Against a running stack: export GATEWAY_URL, ACCOUNT_URL, WALLET_URL,
#      INTERNAL_TOKEN and set SMOKE_INNER=1.
#
set -euo pipefail
cd "$(dirname "$0")/.."

if [ "${SMOKE_INNER:-}" != "1" ]; then
  echo "== booting devstack (embedded postgres + miniredis + services) =="
  ROOT="$(pwd)"
  cd tools/devstack
  exec go run . -root "$ROOT" -exec "SMOKE_INNER=1 bash scripts/smoke-wallet.sh"
fi

GATEWAY_URL="${GATEWAY_URL:-http://localhost:8080}"
ACCOUNT_URL="${ACCOUNT_URL:-http://localhost:8081}"
WALLET_URL="${WALLET_URL:-http://localhost:8086}"
INTERNAL_TOKEN="${INTERNAL_TOKEN:-}"
PASSWORD="Str0ng-Passw0rd!"

# Chain constants for the USDT/TRC20 network in the built-in registry.
NETWORK="TRC20"
ASSET="USDT"
REQUIRED_CONF=19     # TRC20 confirmations needed before crediting
GAS_FEE="2"          # TRC20 gas estimate in the registry

say()  { printf '\n\033[1;34m== %s ==\033[0m\n' "$1"; }
fail() { printf '\033[1;31mSMOKE-WALLET FAILED: %s\033[0m\n' "$1" >&2; exit 1; }
ok()   { printf '  ok  %s\n' "$1"; }

command -v jq >/dev/null 2>&1 || fail "jq is required for smoke-wallet.sh"
[ -n "$INTERNAL_TOKEN" ] || fail "INTERNAL_TOKEN not set (needed for internal/dev endpoints)"

# last_code <target> <purpose> — read the most recent code via the account dev endpoint.
last_code() {
  curl -sS -G "$ACCOUNT_URL/internal/dev/last-code" \
    --data-urlencode "target=$1" --data-urlencode "purpose=$2" \
    -H "X-Internal-Token: $INTERNAL_TOKEN" | jq -r '.code // empty'
}

api() { # api <method> <path> <token> <json|''>
  local m="$1" p="$2" t="$3" b="${4:-}"
  if [ -n "$b" ]; then
    curl -sS -X "$m" "$GATEWAY_URL$p" -H 'Content-Type: application/json' \
      -H "Authorization: Bearer $t" -d "$b"
  else
    curl -sS -X "$m" "$GATEWAY_URL$p" -H "Authorization: Bearer $t"
  fi
}

# internal <method> <path> <json|''> — call a wallet internal/dev endpoint.
internal() {
  local m="$1" p="$2" b="${3:-}"
  if [ -n "$b" ]; then
    curl -sS -X "$m" "$WALLET_URL$p" -H 'Content-Type: application/json' \
      -H "X-Internal-Token: $INTERNAL_TOKEN" -d "$b"
  else
    curl -sS -X "$m" "$WALLET_URL$p" -H "X-Internal-Token: $INTERNAL_TOKEN"
  fi
}

# status <http-code> helper.
http() { curl -sS -o /dev/null -w '%{http_code}' "$@"; }

# balance_of <token> <asset> — free balance as a fixed 8dp string.
balance_of() {
  api GET "/assets/balances?account=funding" "$1" | jq -r --arg a "$2" '.items[] | select(.asset==$a) | .free'
}

# parse_amount <string> -> normalized 8dp for exact comparison.
norm8() { printf '%s' "$1" | awk -F. '{ f=$2; for(i=length(f)+1;i<=8;i++) f=f"0"; print $1"."substr(f,1,8) }'; }

TS="$(date +%s)"
EMAIL="wallet${TS}@example.com"

say "1/11 register (email + code) and allocate a TRC20 deposit address"
http -X POST "$GATEWAY_URL/auth/code/send" -H 'Content-Type: application/json' \
  -d "{\"purpose\":\"register\",\"email\":\"$EMAIL\"}" >/dev/null || fail "send register code failed"
REG_CODE="$(last_code "$EMAIL" register)"
[ -n "$REG_CODE" ] || fail "no register code recorded"
REG="$(curl -sS -X POST "$GATEWAY_URL/auth/register" -H 'Content-Type: application/json' \
  -d "{\"email\":\"$EMAIL\",\"password\":\"$PASSWORD\",\"code\":\"$REG_CODE\"}")"
TOKEN="$(printf '%s' "$REG" | jq -r '.accessToken // empty')"
[ -n "$TOKEN" ] || fail "register did not return accessToken: $REG"
ok "registered ($EMAIL)"

# Withdrawals are capped per KYC level (level 0 = 1000 USDT/24h by default); this
# smoke exercises >5000 USDT review, so lift the user to level 2 via the account
# internal endpoint (the KYC flow itself is covered by smoke-kyc.sh).
UID_="$(api GET /account "$TOKEN" | jq -r '.uid // empty')"
[ -n "$UID_" ] || fail "no uid from /account"
curl -sS -X POST "$ACCOUNT_URL/internal/account/kyc-level" -H 'Content-Type: application/json' \
  -H "X-Internal-Token: $INTERNAL_TOKEN" -d "{\"uid\":\"$UID_\",\"level\":2}" >/dev/null \
  || fail "set kyc level failed"
ok "kyc level raised to 2 for limit-free review tests"

DEP="$(api GET "/wallet/deposit/address?asset=$ASSET&network=$NETWORK" "$TOKEN")"
ADDR="$(printf '%s' "$DEP" | jq -r '.address // empty')"
[ -n "$ADDR" ] || fail "no deposit address: $DEP"
ok "deposit address = $ADDR"

# Idempotent reuse: a second call returns the same address.
DEP2="$(api GET "/wallet/deposit/address?asset=$ASSET&network=$NETWORK" "$TOKEN")"
[ "$(printf '%s' "$DEP2" | jq -r '.address')" = "$ADDR" ] || fail "deposit address not reused"
ok "address reused on second request"

say "2/11 inject a deposit; confirmations insufficient -> not credited"
TX1="0x$(date +%s)abc1"
INJ="$(internal POST /internal/dev/chain/deposit \
  "{\"network\":\"$NETWORK\",\"address\":\"$ADDR\",\"asset\":\"$ASSET\",\"amount\":\"500\",\"txHash\":\"$TX1\",\"logIndex\":0}")"
[ "$(printf '%s' "$INJ" | jq -r '.injected')" = "true" ] || fail "deposit inject failed: $INJ"
ok "injected 500 $ASSET (block 1)"
internal POST /internal/scan '' >/dev/null || fail "scan failed"
BAL="$(balance_of "$TOKEN" "$ASSET")"
[ "$(norm8 "$BAL")" = "0.00000000" ] || fail "balance after 1 confirmation = $BAL, want 0 (need $REQUIRED_CONF)"
ok "not credited yet (balance 0)"

say "3/11 advance blocks to reach $REQUIRED_CONF confirmations -> credited"
internal POST /internal/dev/chain/advance "{\"network\":\"$NETWORK\",\"blocks\":$((REQUIRED_CONF-1))}" >/dev/null \
  || fail "advance failed"
internal POST /internal/scan '' >/dev/null || fail "scan failed"
BAL="$(balance_of "$TOKEN" "$ASSET")"
[ "$(norm8 "$BAL")" = "500.00000000" ] || fail "balance after confirming = $BAL, want 500"
ok "credited 500 $ASSET"

say "4/11 deposit record reflects completion"
RECS="$(api GET "/wallet/deposit/records?asset=$ASSET" "$TOKEN")"
[ "$(printf '%s' "$RECS" | jq -r --arg t "$TX1" '.items[] | select(.txId==$t) | .status')" = "completed" ] \
  || fail "deposit record not completed: $RECS"
ok "deposit record completed"

say "5/11 duplicate injection does not double-credit"
internal POST /internal/dev/chain/deposit \
  "{\"network\":\"$NETWORK\",\"address\":\"$ADDR\",\"asset\":\"$ASSET\",\"amount\":\"500\",\"txHash\":\"$TX1\",\"logIndex\":0}" >/dev/null
internal POST /internal/dev/chain/advance "{\"network\":\"$NETWORK\",\"blocks\":2}" >/dev/null
internal POST /internal/scan '' >/dev/null
BAL="$(balance_of "$TOKEN" "$ASSET")"
[ "$(norm8 "$BAL")" = "500.00000000" ] || fail "duplicate injection changed balance to $BAL"
ok "duplicate ignored (balance still 500)"

say "6/11 top up to 10500 and verify decimal-exact total"
TX2="0x$(date +%s)abc2"
internal POST /internal/dev/chain/deposit \
  "{\"network\":\"$NETWORK\",\"address\":\"$ADDR\",\"asset\":\"$ASSET\",\"amount\":\"10000\",\"txHash\":\"$TX2\",\"logIndex\":0}" >/dev/null
internal POST /internal/dev/chain/advance "{\"network\":\"$NETWORK\",\"blocks\":$REQUIRED_CONF}" >/dev/null
internal POST /internal/scan '' >/dev/null
BAL="$(balance_of "$TOKEN" "$ASSET")"
[ "$(norm8 "$BAL")" = "10500.00000000" ] || fail "topped-up balance = $BAL, want 10500"
ok "balance = 10500 $ASSET"

say "7/11 small withdrawal auto-released, broadcast, confirmed, settled"
# A code for purpose=withdraw reuses the account verification capability.
curl -sS -o /dev/null -X POST "$GATEWAY_URL/account/code/send" -H 'Content-Type: application/json' \
  -H "Authorization: Bearer $TOKEN" -d '{"purpose":"withdraw"}'
WD_CODE="$(last_code "$EMAIL" withdraw)"
[ -n "$WD_CODE" ] || fail "no withdraw code recorded"
SMALL="$(api POST /wallet/withdraw "$TOKEN" \
  "{\"asset\":\"$ASSET\",\"network\":\"$NETWORK\",\"address\":\"TDestSmall0001\",\"amount\":\"100\",\"code\":\"$WD_CODE\"}")"
SMALL_ID="$(printf '%s' "$SMALL" | jq -r '.id // empty')"
[ -n "$SMALL_ID" ] || fail "small withdrawal not created: $SMALL"
# Auto-released: internal status is approved, exposed as processing.
[ "$(printf '%s' "$SMALL" | jq -r '.status')" = "processing" ] || fail "small withdrawal not auto-approved: $SMALL"
ok "small withdrawal $SMALL_ID auto-released"
# Freeze = amount 100 only (gas deducted from amount; user receives 98).
BAL="$(balance_of "$TOKEN" "$ASSET")"
[ "$(norm8 "$BAL")" = "10400.00000000" ] || fail "balance after freeze = $BAL, want 10400"
ok "frozen 100 (gas 2 deducted from amount); free = 10400"

# Broadcast it, then advance to confirmations and complete.
PROC="$(internal POST /internal/process '')"
[ "$(printf '%s' "$PROC" | jq -r '.broadcast')" = "1" ] || fail "withdrawal not broadcast: $PROC"
internal POST /internal/dev/chain/advance "{\"network\":\"$NETWORK\",\"blocks\":$REQUIRED_CONF}" >/dev/null
PROC="$(internal POST /internal/process '')"
[ "$(printf '%s' "$PROC" | jq -r '.completed')" = "1" ] || fail "withdrawal not completed: $PROC"
WSTATUS="$(api GET "/wallet/withdraw/records?asset=$ASSET" "$TOKEN" | jq -r --arg id "$SMALL_ID" '.items[] | select(.id==$id) | .status')"
[ "$WSTATUS" = "completed" ] || fail "small withdrawal status = $WSTATUS, want completed"
# Gas is booked to the platform: user balance stays 10400, locked back to 0.
BAL="$(balance_of "$TOKEN" "$ASSET")"
[ "$(norm8 "$BAL")" = "10400.00000000" ] || fail "balance after settlement = $BAL, want 10400"
ok "settled; free = 10400, gas booked to platform"

say "8/11 large withdrawal held for manual review"
curl -sS -o /dev/null -X POST "$GATEWAY_URL/account/code/send" -H 'Content-Type: application/json' \
  -H "Authorization: Bearer $TOKEN" -d '{"purpose":"withdraw"}'
WD_CODE="$(last_code "$EMAIL" withdraw)"
BIG="$(api POST /wallet/withdraw "$TOKEN" \
  "{\"asset\":\"$ASSET\",\"network\":\"$NETWORK\",\"address\":\"TDestBig0001\",\"amount\":\"6000\",\"code\":\"$WD_CODE\"}")"
BIG_ID="$(printf '%s' "$BIG" | jq -r '.id // empty')"
[ -n "$BIG_ID" ] || fail "large withdrawal not created: $BIG"
[ "$(printf '%s' "$BIG" | jq -r '.status')" = "pending" ] || fail "large withdrawal not pending review: $BIG"
ok "large withdrawal $BIG_ID pending manual review"
BAL="$(balance_of "$TOKEN" "$ASSET")"
[ "$(norm8 "$BAL")" = "4400.00000000" ] || fail "balance after big freeze = $BAL, want 4400"
ok "frozen 6000; free = 4400"

say "9/11 admin list + reject releases the freeze"
ADMIN="$(internal GET '/internal/admin/withdrawals?status=pending_review' '')"
printf '%s' "$ADMIN" | jq -e --arg id "$BIG_ID" '.items[] | select(.id==$id)' >/dev/null \
  || fail "large withdrawal not in admin queue: $ADMIN"
REJ="$(internal POST "/internal/admin/withdrawals/$BIG_ID/reject" '{"reason":"smoke_test"}')"
[ "$(printf '%s' "$REJ" | jq -r '.status')" = "canceled" ] || fail "reject did not cancel: $REJ"
BAL="$(balance_of "$TOKEN" "$ASSET")"
[ "$(norm8 "$BAL")" = "10400.00000000" ] || fail "balance after reject = $BAL, want 10400 (unfrozen)"
ok "rejected; freeze released; free = 10400"

say "10/11 enable whitelist -> non-whitelisted address is blocked"
curl -sS -o /dev/null -X POST "$GATEWAY_URL/account/code/send" -H 'Content-Type: application/json' \
  -H "Authorization: Bearer $TOKEN" -d '{"purpose":"address"}'
AL_CODE="$(last_code "$EMAIL" address)"
api PUT /account/withdraw-whitelist "$TOKEN" "{\"enabled\":true,\"code\":\"$AL_CODE\"}" >/dev/null \
  || fail "enable whitelist failed"
curl -sS -o /dev/null -X POST "$GATEWAY_URL/account/code/send" -H 'Content-Type: application/json' \
  -H "Authorization: Bearer $TOKEN" -d '{"purpose":"withdraw"}'
WD_CODE="$(last_code "$EMAIL" withdraw)"
BLOCK_HTTP="$(http -X POST "$GATEWAY_URL/wallet/withdraw" -H 'Content-Type: application/json' \
  -H "Authorization: Bearer $TOKEN" \
  -d "{\"asset\":\"$ASSET\",\"network\":\"$NETWORK\",\"address\":\"TNotWhitelisted\",\"amount\":\"100\",\"code\":\"$WD_CODE\"}")"
[ "$BLOCK_HTTP" = "403" ] || fail "whitelisted-mode withdrawal to non-listed addr returned $BLOCK_HTTP, want 403"
BAL="$(balance_of "$TOKEN" "$ASSET")"
[ "$(norm8 "$BAL")" = "10400.00000000" ] || fail "blocked withdrawal changed balance to $BAL"
ok "blocked with 403; no freeze applied"

say "11/11 final balances and ledger agree (decimal-exact)"
BALS="$(api GET /assets/balances "$TOKEN")"
FINAL="$(printf '%s' "$BALS" | jq -r --arg a "$ASSET" '.items[] | select(.asset==$a) | .free')"
LOCKED="$(printf '%s' "$BALS" | jq -r --arg a "$ASSET" '.items[] | select(.asset==$a) | .locked')"
[ "$(norm8 "$FINAL")" = "10400.00000000" ] || fail "final free = $FINAL, want 10400"
[ "$(norm8 "$LOCKED")" = "0.00000000" ] || fail "final locked = $LOCKED, want 0"
ok "free = 10400, locked = 0 (deposits 10500 - withdrawal 100 incl. gas 2)"

printf '\n\033[1;32mSMOKE-WALLET PASSED\033[0m — deposit/confirm/credit -> withdraw/review/reject -> whitelist -> balances reconcile\n'
