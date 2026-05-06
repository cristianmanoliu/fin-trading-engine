#!/usr/bin/env bash
# Sync code, rebuild, restart one or all engines, then tail the log.
# Usage:
#   ./deploy/redeploy.sh              # restart btcusdt and tail its log
#   ./deploy/redeploy.sh ethusdt      # restart a specific symbol
#   ./deploy/redeploy.sh all          # restart all deployed engines + run health check
# Env:
#   SKIP_CHECK=1                     # skip post-deploy health check after `all`
# Deployed symbol list — source of truth in configs/symbols.yaml `deployed`.
set -euo pipefail

ROOT="$(cd "$(dirname "$0")/.." && pwd)"
TARGET="root@178.105.24.230"
SYMBOL="${1:-btcusdt}"
SKIP_CHECK="${SKIP_CHECK:-0}"

"${ROOT}/deploy/sync.sh" "${TARGET}"

if [[ "$SYMBOL" == "all" ]]; then
    source "${ROOT}/scripts/lib/symbols.sh"
    units=$(get_symbols deployed lower | sed 's/[^ ]*/paper-live@&/g')
    ssh "${TARGET}" "systemctl restart $units"
    echo "✓ All deployed engines restarted: $(get_symbols deployed lower)"

    if [[ "$SKIP_CHECK" != "1" ]]; then
        # Sleep enough for backfill + funding load + first heartbeat (~60s).
        # Without this, the health check reports stale ticks for engines that
        # just restarted but haven't completed their first heartbeat cycle.
        echo ""
        echo "→ Sleeping 60s for engines to stabilize before health check..."
        sleep 60
        "${ROOT}/scripts/post_deploy_check.sh" "${TARGET}"
    else
        echo "  (post-deploy health check skipped — run ./scripts/post_deploy_check.sh manually)"
    fi
else
    SYMBOL_LC="$(echo "$SYMBOL" | tr '[:upper:]' '[:lower:]')"
    ssh "${TARGET}" "systemctl restart paper-live@${SYMBOL_LC} && sleep 2 && tail -f /var/log/paper-live/${SYMBOL_LC}.log"
fi
