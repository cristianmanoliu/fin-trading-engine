#!/usr/bin/env bash
# refresh_funding.sh — Incrementally refresh per-symbol Binance funding CSVs and
# (optionally) sync them to the VPS.
#
# Why this exists: live engines load funding CSVs once at startup. Without periodic
# refresh, trades held past the CSV's last entry get $0 funding accrual instead of
# real rates — slowly diverging from backtest assumptions over time.
#
# Recommended cadence: run weekly during forward-paper-validation windows. Engines
# pick up refreshed CSVs on next restart (sync only updates the file; does not
# restart engines).
#
# Cron example (run weekly Sunday 04:00 local time on dev machine):
#   0 4 * * 0 cd /path/to/trading-engine && bash scripts/refresh_funding.sh --sync
#
# Usage:
#   bash scripts/refresh_funding.sh           # local refresh only
#   bash scripts/refresh_funding.sh --sync    # refresh + rsync to VPS
#
set -euo pipefail
ROOT="$(cd "$(dirname "$0")/.." && pwd)"
cd "$ROOT"

SYNC=0
for arg in "$@"; do
    case "$arg" in
        --sync) SYNC=1 ;;
        *) echo "unknown arg: $arg" >&2; exit 2 ;;
    esac
done

echo "→ Running incremental funding refresh (all 57 symbols)..."
INCREMENTAL=1 bash scripts/download_funding.sh

if [[ "$SYNC" == "1" ]]; then
    TARGET="root@178.105.24.230"
    REMOTE_DIR="/opt/trading-engine"
    SSH_KEY="${HOME}/.ssh/id_ed25519"
    SSH_OPTS="-i ${SSH_KEY} -o StrictHostKeyChecking=accept-new"

    echo
    echo "→ Syncing data/funding/ to ${TARGET}..."
    rsync -az \
        -e "ssh ${SSH_OPTS}" \
        "${ROOT}/data/funding/" \
        "${TARGET}:${REMOTE_DIR}/data/funding/"
    echo "✓ Funding CSVs refreshed on VPS."
    echo
    echo "Note: live engines hold the CSV in memory from startup. To pick up the"
    echo "refreshed data, they need a restart. This script does NOT restart them"
    echo "automatically (restarts trigger a 48h backfill replay that pollutes the"
    echo "trade journal). Restart manually when convenient via:"
    echo "  ./deploy/redeploy.sh all"
fi

echo "✓ Done."
