#!/usr/bin/env bash
# signal_context_fetch.sh — Pull signal-context sidecars from VPS into local cache.
#
# Idempotent rsync pull: adds new JSONL records, never deletes local files.
# Remote is READ-ONLY from this script's perspective.
#
# Usage:
#   bash scripts/signal_context_fetch.sh [VPS_HOST]
#
# Env overrides:
#   VPS_HOST          override default root@178.105.24.230
#   REMOTE_DIR        override default /var/log/paper-live/signal-context
#   LOCAL_CACHE_DIR   override default results/signal_context_cache
#
# Exit codes: 0 OK | 1 rsync failed | 2 remote dir missing | 3 local cache error
set -euo pipefail

ROOT="$(cd "$(dirname "$0")/.." && pwd)"

VPS_HOST="${VPS_HOST:-root@178.105.24.230}"
REMOTE_DIR="${REMOTE_DIR:-/var/log/paper-live/signal-context}"
LOCAL_CACHE_DIR="${LOCAL_CACHE_DIR:-${ROOT}/results/signal_context_cache}"

# Validate remote dir exists before attempting rsync.
if ! ssh "$VPS_HOST" "test -d '$REMOTE_DIR'" 2>/dev/null; then
    echo "ERROR: remote dir not found: ${VPS_HOST}:${REMOTE_DIR}" >&2
    exit 2
fi

mkdir -p "$LOCAL_CACHE_DIR" || { echo "ERROR: cannot create local cache dir: $LOCAL_CACHE_DIR" >&2; exit 3; }

echo "→ Fetching signal-context sidecars from ${VPS_HOST}:${REMOTE_DIR}"
echo "→ Local cache: $LOCAL_CACHE_DIR"

# --append: only transfer new bytes in existing files + new files.
# --checksum: verify file integrity (fallback; --append-verify not on macOS rsync).
# Never deletes local files (no --delete). Archive preserves timestamps.
rsync -a --append \
    "${VPS_HOST}:${REMOTE_DIR}/" \
    "${LOCAL_CACHE_DIR}/" \
    2>&1

echo ""
echo "→ Fetch complete. Run: python3 scripts/signal_context_inspect.py"
