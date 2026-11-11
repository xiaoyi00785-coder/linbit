#!/usr/bin/env bash
# 现货链路端到端 smoke：注册 → 登录 → 查余额（都走 gateway）。
# 无栈（默认，不需要 Docker）：拉起 devstack（内嵌 postgres + miniredis + 3 个服务），
# 在里面重跑本脚本，结束后拆掉。
#   bash scripts/smoke.sh
# 已有栈：
#   GATEWAY_URL=http://localhost:8080 SMOKE_INNER=1 bash scripts/smoke.sh
set -euo pipefail

cd "$(dirname "$0")/.."

if [ "${SMOKE_INNER:-}" != "1" ]; then
  echo "== booting devstack (embedded postgres + miniredis + services) =="
  ROOT="$(pwd)"
  # devstack 是独立 Go module：进该目录运行，并用 -root 指回仓库根。
  # 它拉起栈，在其中重跑本脚本（SMOKE_INNER=1，cwd 为仓库根），然后拆掉。
  cd tools/devstack
  exec go run . -root "$ROOT" -exec "SMOKE_INNER=1 bash scripts/smoke.sh"
fi

GATEWAY_URL="${GATEWAY_URL:-http://localhost:8080}"
ACCOUNT_URL="${ACCOUNT_URL:-http://localhost:8081}"
INTERNAL_TOKEN="${INTERNAL_TOKEN:-}"
EMAIL="smoke+$(date +%s)@example.com"
PASSWORD="Str0ng-Passw0rd!"

say()  { printf '\n\033[1;34m== %s ==\033[0m\n' "$1"; }
fail() { printf '\033[1;31mSMOKE FAILED: %s\033[0m\n' "$1" >&2; exit 1; }

command -v jq >/dev/null 2>&1 && HAVE_JQ=1 || HAVE_JQ=0

# json_get <json> <key> — extract a top-level string value from a JSON object.
json_get() {
  if [ "$HAVE_JQ" = "1" ]; then
    printf '%s' "$1" | jq -r ".$2 // empty"
  else
    printf '%s' "$1" | sed -n "s/.*\"$2\"[[:space:]]*:[[:space:]]*\"\([^\"]*\)\".*/\1/p"
  fi
}

# 注册需要邮箱或短信验证码。经 account 的 DEV_MODE 内部接口读回，代替真实服务商。
[ -n "$INTERNAL_TOKEN" ] || fail "INTERNAL_TOKEN not set (needed to read dev verification code)"

say "1/3 register $EMAIL (request code -> register)"
CODE_HTTP=$(curl -sS -o /dev/null -w '%{http_code}' -X POST "$GATEWAY_URL/auth/code/send" \
  -H 'Content-Type: application/json' \
  -d "{\"purpose\":\"register\",\"email\":\"$EMAIL\"}")
[ "$CODE_HTTP" = "200" ] || fail "send register code returned $CODE_HTTP"
CODE_JSON=$(curl -sS -G "$ACCOUNT_URL/internal/dev/last-code" \
  --data-urlencode "target=$EMAIL" --data-urlencode "purpose=register" \
  -H "X-Internal-Token: $INTERNAL_TOKEN")
REG_CODE=$(json_get "$CODE_JSON" code)
[ -n "$REG_CODE" ] || fail "could not read register code ($CODE_JSON)"
REG=$(curl -sS -X POST "$GATEWAY_URL/auth/register" \
  -H 'Content-Type: application/json' \
  -d "{\"email\":\"$EMAIL\",\"password\":\"$PASSWORD\",\"code\":\"$REG_CODE\"}")
echo "$REG"
ACCESS=$(json_get "$REG" accessToken)
[ -n "$ACCESS" ] || fail "register did not return accessToken"

say "2/3 login"
LOGIN=$(curl -sS -X POST "$GATEWAY_URL/auth/login" \
  -H 'Content-Type: application/json' \
  -d "{\"email\":\"$EMAIL\",\"password\":\"$PASSWORD\"}")
echo "$LOGIN"
ACCESS=$(json_get "$LOGIN" accessToken)
[ -n "$ACCESS" ] || fail "login did not return accessToken"

say "2.5 reject balances without token (gateway auth)"
CODE=$(curl -sS -o /dev/null -w '%{http_code}' "$GATEWAY_URL/assets/balances")
[ "$CODE" = "401" ] || fail "unauthenticated balances returned $CODE, want 401"
echo "unauthenticated request correctly rejected ($CODE)"

say "3/3 query balances with token"
BAL=$(curl -sS "$GATEWAY_URL/assets/balances" -H "Authorization: Bearer $ACCESS")
echo "$BAL"
case "$BAL" in
  *'"items"'*'"USDT"'*) : ;;
  *) fail "balances did not contain seeded assets" ;;
esac

printf '\n\033[1;32mSMOKE PASSED\033[0m — register -> login -> balances OK via gateway\n'
