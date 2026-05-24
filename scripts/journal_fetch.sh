#!/usr/bin/env bash
# journal_fetch.sh — Pull journal JSONL files from VPS into local cache.
#
# Idempotent rsync pull: adds new JSONL records, never deletes local files.
# Remote is READ-ONLY from this script's perspective.
#
# Fetches both live journals (top-level) and shadow journals (shadow/<label>/)
# preserving the subdirectory structure for use by signal_journal_reconcile.py.
#
# Usage:
#   bash scripts/journal_fetch.sh [VPS_HOST]
#
# Env overrides:
#   VPS_HOST          override default root@178.105.24.230
#   REMOTE_DIR        override default /var/log/paper-live/journal
#   LOCAL_CACHE_DIR   override default results/journal_cache
#
# Exit codes: 0 OK | 1 rsync failed | 2 remote dir missing | 3 local cache error
set -euo pipefail

ROOT="$(cd "$(dirname "$0")/.." && pwd)"

VPS_HOST="${VPS_HOST:-root@178.105.24.230}"
REMOTE_DIR="${REMOTE_DIR:-/var/log/paper-live/journal}"
LOCAL_CACHE_DIR="${LOCAL_CACHE_DIR:-${ROOT}/results/journal_cache}"

# Validate remote dir exists before attempting rsync.
if ! ssh "$VPS_HOST" "test -d '$REMOTE_DIR'" 2>/dev/null; then
    echo "ERROR: remote dir not found: ${VPS_HOST}:${REMOTE_DIR}" >&2
    exit 2
fi

mkdir -p "$LOCAL_CACHE_DIR" || { echo "ERROR: cannot create local cache dir: $LOCAL_CACHE_DIR" >&2; exit 3; }

echo "→ Fetching journal files from ${VPS_HOST}:${REMOTE_DIR}"
echo "→ Local cache: $LOCAL_CACHE_DIR"

# --append: only transfer new bytes in existing files + new files.
# Archive preserves timestamps and subdirs (live/*.jsonl + shadow/<label>/*.jsonl).
# Never deletes local files (no --delete). Read-only on remote.
rsync -a --append \
    "${VPS_HOST}:${REMOTE_DIR}/" \
    "${LOCAL_CACHE_DIR}/" \
    2>&1

echo ""
echo "→ Fetch complete. Run: python3 scripts/signal_journal_reconcile.py"
