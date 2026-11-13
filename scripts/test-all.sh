#!/usr/bin/env bash
#
# Run `go vet` and `go test` for every Go module in the repo.
#
#   bash scripts/test-all.sh                 # DB-backed tests skip (no DATABASE_URL)
#   cd tools/devstack && go run . -root "$(git -C ../.. rev-parse --show-toplevel)" \
#       -exec "bash scripts/test-all.sh"     # DB-backed tests run against embedded postgres
#
#   REQUIRE_DB=1 bash scripts/test-all.sh    # CI：没有 DATABASE_URL 时直接失败，
#                                            # 而不是让依赖数据库的测试被静默跳过
#
set -euo pipefail
cd "$(dirname "$0")/.."

if [ "${REQUIRE_DB:-}" = "1" ] && [ -z "${DATABASE_URL:-}" ]; then
  echo "REQUIRE_DB=1 but DATABASE_URL is not set: DB-backed tests would be skipped" >&2
  exit 1
fi

MODULES=(
  services/go/common
  services/go/account
  services/go/asset
  services/go/wallet
  services/go/kyc
  services/go/futures
  services/go/strategy
  services/go/growth
  services/go/earn
  services/go/marketmaker
  services/go/paper
  services/go/admin
  services/go/order
  services/go/market-data
  services/go/gateway
  tools/devstack
  tools/wsprobe
)

# Run every module even if an earlier one fails, so one run shows all failures.
FAILED=()
for m in "${MODULES[@]}"; do
  printf '\n\033[1;34m== %s ==\033[0m\n' "$m"
  if ! ( cd "$m" && go vet ./... && go test ./... ); then
    FAILED+=("$m")
  fi
done

if [ "${#FAILED[@]}" -gt 0 ]; then
  printf '\n\033[1;31mFAILED MODULES (%d):\033[0m\n' "${#FAILED[@]}"
  printf '  %s\n' "${FAILED[@]}"
  exit 1
fi
printf '\n\033[1;32mALL MODULES OK\033[0m\n'
