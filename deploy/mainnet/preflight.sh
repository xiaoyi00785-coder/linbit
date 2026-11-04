#!/usr/bin/env bash
# 主网启动前检查（只读）。用法: ENV_FILE=.env deploy/mainnet/preflight.sh
# DB 检查需要 DATABASE_URL（从 ENV_FILE 读取）与本机 psql；若无 psql，设 PSQL="docker compose exec -T postgres psql -U linbit -d linbit"。
# FAIL 时退出码 1；WARN 只提示。
set -uo pipefail
ENV_FILE=${ENV_FILE:-.env}
fails=0
fail() { echo "FAIL $*"; fails=$((fails+1)); }
warn() { echo "WARN $*"; }
ok()   { echo "OK   $*"; }
envget() { grep -E "^$1=" "$ENV_FILE" 2>/dev/null | tail -1 | cut -d= -f2- | sed -e 's/[[:space:]]*#.*$//' -e 's/^"//' -e 's/"$//'; }

[ -f "$ENV_FILE" ] || { echo "FAIL ENV_FILE $ENV_FILE not found"; exit 1; }

# 1) TRUSTED_PROXY_CIDRS vs geo-block. The gateway only trusts CF-IPCountry when
#    its TCP peer is in TRUSTED_PROXY_CIDRS. Behind host nginx -> docker-proxy the
#    peer is the docker bridge gateway (172.18.0.1), never a Cloudflare IP.
geo=$(envget GEO_BLOCK_ENABLED); geo=${geo:-true}   # mainnet overlay forces true
tpc=$(envget TRUSTED_PROXY_CIDRS)
cf_re='^(173\.245\.48\.|103\.21\.244\.|103\.22\.200\.|103\.31\.4\.|141\.101\.64\.|108\.162\.192\.|190\.93\.240\.|188\.114\.96\.|197\.234\.240\.|198\.41\.128\.|162\.158\.|104\.16\.|104\.24\.|172\.64\.|131\.0\.72\.)'
if [ -z "$tpc" ]; then
  fail "TRUSTED_PROXY_CIDRS empty (gateway refuses to start on mainnet)"
elif [ "$geo" = "true" ]; then
  has_bridge=0; has_cf=0
  for c in ${tpc//,/ }; do
    [[ $c =~ ^172\.(1[6-9]|2[0-9]|3[01])\. ]] && has_bridge=1
    [[ $c =~ $cf_re || $c =~ ^2(400:cb00|606:4700|803:f800|405:b500|405:8100|a06:98c0|c0f:f248): ]] && has_cf=1
  done
  if [ $has_bridge = 1 ]; then ok "TRUSTED_PROXY_CIDRS includes docker bridge peer ($tpc)"
  elif [ $has_cf = 1 ]; then warn "TRUSTED_PROXY_CIDRS has only Cloudflare ranges: correct only if CF connects straight to the gateway; behind nginx/docker-proxy geo-block sees no country"
  else fail "GEO_BLOCK_ENABLED=true but TRUSTED_PROXY_CIDRS ($tpc) has neither the docker bridge gateway nor Cloudflare ranges: CF-IPCountry ignored, geo-block inert"; fi
fi

# 2) Signer passphrase: host path for the docker secret, mode 0400.
pf=$(envget WALLET_SIGNER_PASSPHRASE_FILE); pf=${pf:-./secrets/signer_passphrase}
if [[ $pf == /run/secrets/* ]]; then
  fail "WALLET_SIGNER_PASSPHRASE_FILE=$pf is the in-container path; use the host path ./secrets/signer_passphrase"
elif [ ! -s "$pf" ]; then
  fail "signer passphrase file $pf missing or empty"
else
  m=$(stat -c %a "$pf"); [ "$m" = 400 ] && ok "signer passphrase $pf mode 0400" || fail "signer passphrase $pf mode $m (want 0400)"
fi

# 3) tradfi_tokens: on-chain deposit/withdraw only for verified mainnet contracts.
DBURL=${DATABASE_URL:-$(envget DATABASE_URL)}
PSQL=${PSQL:-psql "$DBURL"}
q() { $PSQL -XAtq -F'|' -c "$1"; }
if ! q 'SELECT 1' >/dev/null 2>&1; then
  warn "database unreachable (set PSQL=...); tradfi_tokens checks skipped"
else
  if ! q "SELECT 1 FROM information_schema.columns WHERE table_name='tradfi_tokens' AND column_name='contract_verified'" | grep -q 1; then
    fail "tradfi_tokens.contract_verified missing: market-data migration 00009 not applied (rebuild/restart market-data first)"
  else
    bad=$(q "SELECT asset||'/'||issuer||'/'||network FROM tradfi_tokens WHERE (deposit_enabled OR withdraw_enabled) AND NOT contract_verified ORDER BY 1")
    [ -n "$bad" ] && fail "tradfi_tokens enabled but unverified (wallet ignores them on mainnet; disable per docs/deploy/tradfi-tokens-runbook.md): $(echo $bad)" || ok "no enabled unverified tradfi_tokens"
    tn=$(q "SELECT asset||'/'||issuer||'/'||network||'('||updated_by||')' FROM tradfi_tokens WHERE NOT contract_verified AND (contract_address<>'' OR updated_by ILIKE '%test%') ORDER BY 1")
    [ -n "$tn" ] && warn "tradfi_tokens with testnet-flagged or unverified contract addresses (inert on mainnet): $(echo $tn)"
  fi
fi

echo "preflight: $fails failure(s)"
[ $fails = 0 ]
