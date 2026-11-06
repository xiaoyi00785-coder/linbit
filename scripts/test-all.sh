#!/usr/bin/env bash
#
# Run `go vet` and `go test` for every Go module in the repo.
#
#   bash scripts/test-all.sh                 # DB-backed tests skip (no DATABASE_URL)
#   cd tools/devstack && go run . -root "$(git -C ../.. rev-parse --show-toplevel)" \
#       -exec "bash scripts/test-all.sh"     # DB-backed tests run against embedded postgres
#
set -euo pipefail
cd "$(dirname "$0")/.."

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
)

for m in "${MODULES[@]}"; do
  printf '\n\033[1;34m== %s ==\033[0m\n' "$m"
  ( cd "$m" && go vet ./... && go test ./... )
done

printf '\n\033[1;32mALL MODULES OK\033[0m\n'
