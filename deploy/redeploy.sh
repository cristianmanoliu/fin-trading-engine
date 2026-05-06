#!/usr/bin/env bash
# Sync code, rebuild, restart one or all engines, then tail the log.
# Usage:
#   ./deploy/redeploy.sh              # restart btcusdt and tail its log
#   ./deploy/redeploy.sh ethusdt      # restart a specific symbol
#   ./deploy/redeploy.sh all          # restart all deployed engines (no tail)
# Deployed symbol list — source of truth in configs/symbols.yaml `deployed`.
set -euo pipefail

ROOT="$(cd "$(dirname "$0")/.." && pwd)"
TARGET="root@178.105.24.230"
SYMBOL="${1:-btcusdt}"

"${ROOT}/deploy/sync.sh" "${TARGET}"

if [[ "$SYMBOL" == "all" ]]; then
    source "${ROOT}/scripts/lib/symbols.sh"
    units=$(get_symbols deployed lower | sed 's/[^ ]*/paper-live@&/g')
    ssh "${TARGET}" "systemctl restart $units"
    echo "✓ All deployed engines restarted: $(get_symbols deployed lower)"
else
    SYMBOL_LC="$(echo "$SYMBOL" | tr '[:upper:]' '[:lower:]')"
    ssh "${TARGET}" "systemctl restart paper-live@${SYMBOL_LC} && sleep 2 && tail -f /var/log/paper-live/${SYMBOL_LC}.log"
fi
