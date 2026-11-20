#!/usr/bin/env bash
# ----------------------------------------------------------------------------
# /root/deploy.sh — one-shot deploy for Linbit backend services.
#
# Usage:
#   deploy.sh <service> [service...]
#   deploy.sh --ref <sha|branch|tag> <service>...
#   deploy.sh --all                  # every compose service (be careful)
#
# What it does, in order:
#   1. df -h / must have >10% free; otherwise abort.
#   2. Fetches the git tree into a pristine snapshot at /opt/linbit-<rev>
#      using `git archive` (no .git, no submodule state).
#   3. Swaps symlink /opt/linbit → /opt/linbit-<rev> (keeps .env / secrets).
#   4. Builds the requested services with retry (same loop as /root/b.sh).
#   5. compose up -d --no-deps --no-build <service> (keeps the rest alive).
#   6. Polls http://127.0.0.1:<port>/healthz for each service that has one.
#   7. On ANY failure: restores the previous /opt/linbit symlink, restarts
#      the services from the previous image tag (keep-pre-<rev>), exits !=0.
#
# Deploy notes land in /opt/linbit/REVISION.components (what /root/b.sh already
# does); rollback image tags are tagged keep-pre-<rev> before each `build`.
# ----------------------------------------------------------------------------

set -euo pipefail

REPO_URL="${LINBIT_REPO_URL:-https://github.com/xiaoyi00785-coder/linbit.git}"
REF="main"
RELEASES_DIR="/opt"
CURRENT_LINK="/opt/linbit"
ENV_FILE="$CURRENT_LINK/.env"
SECRETS_DIR="$CURRENT_LINK/secrets"
COMPOSE_PROJECT="linbit"
HEALTHCHECK_TIMEOUT=60
DISK_MIN_FREE_PCT=10
LOG_DIR="/root/deploy-logs"
mkdir -p "$LOG_DIR"

log() { printf '[deploy %s] %s\n' "$(date -u +%H:%M:%SZ)" "$*"; }
die() { log "ERROR: $*"; exit 1; }

# Services with /healthz exposed on an internal port.
# Keep this in sync with docker-compose.yml.
declare -A HEALTHPORT=(
  [gateway]=8080
  [account]=8081
  [asset]=8082
  [order]=8083
  [market-data]=8084
  [wallet]=8085
  [wallet-signer]=8086
  [admin]=8088
  [futures]=8090
  [strategy]=8091
  [growth]=8092
  [earn]=8093
  [marketmaker]=8094
  [staking]=8095
  [paper]=8096
  [kyc]=8097
  [matching-engine]=0   # internal only, no /healthz
)

# ---- arg parsing -----------------------------------------------------------
SERVICES=()
while [ $# -gt 0 ]; do
  case "$1" in
    --ref) REF="$2"; shift 2 ;;
    --all) SERVICES=(account admin asset earn futures gateway growth kyc
                     market-data marketmaker matching-engine order paper
                     staking strategy wallet wallet-signer) ; shift ;;
    -h|--help)
      sed -n '2,25p' "$0"; exit 0 ;;
    *) SERVICES+=("$1"); shift ;;
  esac
done
[ "${#SERVICES[@]}" -gt 0 ] || die "no services specified (try --all or -h)"

# ---- preflight -------------------------------------------------------------
FREE_PCT="$(df -P / | awk 'NR==2{gsub("%","",$5); print 100-$5}')"
[ "$FREE_PCT" -ge "$DISK_MIN_FREE_PCT" ] \
  || die "disk free ${FREE_PCT}% < ${DISK_MIN_FREE_PCT}% — refusing to build"

[ -f "$ENV_FILE" ]     || die "missing $ENV_FILE"
[ -d "$SECRETS_DIR" ]  || die "missing $SECRETS_DIR"
command -v git     >/dev/null || die "git not installed"
command -v docker  >/dev/null || die "docker not installed"
docker compose version >/dev/null || die "docker compose plugin missing"

# ---- fetch tree ------------------------------------------------------------
TMP_SNAP="$(mktemp -d /tmp/linbit-snap.XXXXXX)"
trap 'rm -rf "$TMP_SNAP"' EXIT

log "fetching $REF from $REPO_URL ..."
git -C "$TMP_SNAP" init -q
git -C "$TMP_SNAP" remote add origin "$REPO_URL"
git -C "$TMP_SNAP" fetch --depth=1 origin "$REF" -q
REV="$(git -C "$TMP_SNAP" rev-parse FETCH_HEAD | cut -c1-8)"

RELEASE_DIR="$RELEASES_DIR/linbit-$REV"
if [ -d "$RELEASE_DIR" ]; then
  log "snapshot $RELEASE_DIR already exists — reusing"
else
  log "materialising $RELEASE_DIR ..."
  mkdir -p "$RELEASE_DIR"
  git -C "$TMP_SNAP" archive FETCH_HEAD | tar -x -C "$RELEASE_DIR"
fi

# ---- preserve runtime files ------------------------------------------------
ln -sf "$ENV_FILE"    "$RELEASE_DIR/.env"
if [ ! -e "$RELEASE_DIR/secrets" ]; then
  ln -s "$SECRETS_DIR" "$RELEASE_DIR/secrets"
fi
if [ -f "$CURRENT_LINK/docker-compose.override.yml" ]; then
  cp -a "$CURRENT_LINK/docker-compose.override.yml" "$RELEASE_DIR/"
fi

# ---- swap symlink ----------------------------------------------------------
PREV_TARGET="$(readlink -f "$CURRENT_LINK" || true)"
log "previous: $PREV_TARGET"
log "next    : $RELEASE_DIR"
ln -sfn "$RELEASE_DIR" "$CURRENT_LINK.next"
mv -Tf "$CURRENT_LINK.next" "$CURRENT_LINK"

# ---- compose command -------------------------------------------------------
C=(docker compose -p "$COMPOSE_PROJECT"
   -f "$CURRENT_LINK/docker-compose.yml"
   -f "$CURRENT_LINK/docker-compose.override.yml"
   -f "$CURRENT_LINK/deploy/mainnet/docker-compose.mainnet.yml"
   --profile chain)

rollback() {
  log "ROLLBACK: restoring $PREV_TARGET"
  [ -n "$PREV_TARGET" ] && ln -sfn "$PREV_TARGET" "$CURRENT_LINK"
  for s in "${SERVICES[@]}"; do
    "${C[@]}" up -d --no-deps --no-build "$s" || true
  done
  exit 1
}

# Tag current running images for rollback.
for s in "${SERVICES[@]}"; do
  cid="$(docker ps --filter "label=com.docker.compose.service=$s" \
                   --filter "label=com.docker.compose.project=$COMPOSE_PROJECT" \
                   --format '{{.Image}}' | head -1 || true)"
  if [ -n "$cid" ]; then
    docker tag "$cid" "linbit/$s:keep-pre-$REV" || true
  fi
done

# ---- build with retry ------------------------------------------------------
BUILD_LOG="$LOG_DIR/build-$REV.log"
log "building ${SERVICES[*]} (log: $BUILD_LOG)"
(
  cd "$CURRENT_LINK"
  for s in "${SERVICES[@]}"; do
    ok=0
    for n in 1 2 3; do
      if "${C[@]}" build "$s" >>"$BUILD_LOG" 2>&1; then ok=1; break; fi
      log "RETRY $s attempt $n"
      sleep 20
    done
    [ "$ok" = 1 ] || { log "BUILDFAIL $s — see $BUILD_LOG"; rollback; }
  done
) || rollback

# ---- up --------------------------------------------------------------------
for s in "${SERVICES[@]}"; do
  log "up -d $s"
  "${C[@]}" up -d --no-deps --no-build "$s" || rollback
done

# ---- healthchecks ----------------------------------------------------------
for s in "${SERVICES[@]}"; do
  port="${HEALTHPORT[$s]:-0}"
  [ "$port" = 0 ] && { log "skip healthz for $s (no http port)"; continue; }
  log "polling http://127.0.0.1:$port/healthz ..."
  deadline=$(( $(date +%s) + HEALTHCHECK_TIMEOUT ))
  until curl -fsS -m 3 "http://127.0.0.1:$port/healthz" >/dev/null 2>&1; do
    if [ "$(date +%s)" -ge "$deadline" ]; then
      log "HEALTHCHECK FAILED for $s"
      rollback
    fi
    sleep 2
  done
  log "$s healthy"
done

# ---- bookkeeping -----------------------------------------------------------
{
  printf '[%s] %s services=%s ref=%s\n' \
    "$(date -u +%Y-%m-%dT%H:%M:%SZ)" "$REV" "${SERVICES[*]}" "$REF"
} >> "$CURRENT_LINK/REVISION.components"

log "DONE $REV — ${SERVICES[*]}"
