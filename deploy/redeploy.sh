#!/usr/bin/env bash
# Sync code, rebuild, restart one or all engines, then tail the log.
# Usage:
#   ./deploy/redeploy.sh              # restart btcusdt and tail its log
#   ./deploy/redeploy.sh ethusdt      # restart a specific symbol
#   ./deploy/redeploy.sh all          # restart all 16 deployed engines (no tail)
# Deployed set last revised 2026-05-05: expanded to 16 by adding BNB/HBAR/IOTA/GRT/TRX
# (top non-deployed persistent winners from OOS test, both train and test halves positive).
# OPUSDT dropped same date — structural loser in both halves.
set -euo pipefail

ROOT="$(cd "$(dirname "$0")/.." && pwd)"
TARGET="root@178.105.24.230"
SYMBOL="${1:-btcusdt}"

"${ROOT}/deploy/sync.sh" "${TARGET}"

if [[ "$SYMBOL" == "all" ]]; then
    ssh "${TARGET}" 'systemctl restart paper-live@ethusdt paper-live@runeusdt paper-live@linkusdt paper-live@xlmusdt paper-live@ldousdt paper-live@solusdt paper-live@apeusdt paper-live@enjusdt paper-live@zilusdt paper-live@btcusdt paper-live@tiausdt paper-live@bnbusdt paper-live@hbarusdt paper-live@iotausdt paper-live@grtusdt paper-live@trxusdt'
    echo "✓ All engines restarted."
else
    SYMBOL_LC="$(echo "$SYMBOL" | tr '[:upper:]' '[:lower:]')"
    ssh "${TARGET}" "systemctl restart paper-live@${SYMBOL_LC} && sleep 2 && tail -f /var/log/paper-live/${SYMBOL_LC}.log"
fi
