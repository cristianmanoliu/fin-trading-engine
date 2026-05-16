#!/usr/bin/env bash
# Deploy Layer 2 testnet engine (BTCUSDT) to VPS.
#
# Usage:
#   # First time — provide testnet credentials:
#   BINANCE_TESTNET_KEY=... BINANCE_TESTNET_SECRET=... ./deploy/deploy_testnet.sh
#
#   # Subsequent deploys (creds already on VPS):
#   ./deploy/deploy_testnet.sh
#
#   # Stop and remove:
#   ./deploy/deploy_testnet.sh stop
set -euo pipefail

ROOT="$(cd "$(dirname "$0")/.." && pwd)"
TARGET="root@178.105.24.230"
TESTNET_ENV="/etc/paper-live/testnet-env"

if [[ "${1:-}" == "stop" ]]; then
    echo "→ Stopping testnet engine..."
    ssh "${TARGET}" "systemctl stop testnet-engine && systemctl disable testnet-engine"
    echo "✓ Testnet engine stopped and disabled."
    exit 0
fi

# ── Sync code + rebuild ──────────────────────────────────────────────────────
"${ROOT}/deploy/sync.sh" "${TARGET}"

# ── Create testnet credentials file if provided ──────────────────────────────
if [[ -n "${BINANCE_TESTNET_KEY:-}" ]] && [[ -n "${BINANCE_TESTNET_SECRET:-}" ]]; then
    echo "→ Writing testnet credentials to ${TESTNET_ENV}..."
    ssh "${TARGET}" "cat > ${TESTNET_ENV} << 'ENVEOF'
BINANCE_API_KEY=${BINANCE_TESTNET_KEY}
BINANCE_API_SECRET=${BINANCE_TESTNET_SECRET}
ENVEOF
chmod 600 ${TESTNET_ENV}
chown paperlive:paperlive ${TESTNET_ENV}"
    echo "✓ Testnet credentials saved."
else
    echo "→ No BINANCE_TESTNET_KEY provided — checking if ${TESTNET_ENV} already exists on VPS..."
    if ! ssh "${TARGET}" "test -f ${TESTNET_ENV}"; then
        echo "ERROR: ${TESTNET_ENV} not found on VPS." >&2
        echo "       Provide credentials:" >&2
        echo "         BINANCE_TESTNET_KEY=... BINANCE_TESTNET_SECRET=... ./deploy/deploy_testnet.sh" >&2
        exit 1
    fi
    echo "✓ Existing credentials found."
fi

# ── Create journal directory ─────────────────────────────────────────────────
ssh "${TARGET}" "mkdir -p /var/log/paper-live/journal/testnet && chown -R paperlive:paperlive /var/log/paper-live/journal/testnet"

# ── Install + start systemd service ──────────────────────────────────────────
ssh "${TARGET}" "cp /opt/trading-engine/deploy/systemd/testnet-engine.service /etc/systemd/system/ && \
    chmod 644 /etc/systemd/system/testnet-engine.service && \
    systemctl daemon-reload && \
    systemctl enable testnet-engine && \
    systemctl restart testnet-engine"

echo ""
echo "✓ Testnet engine deployed and started."
echo ""
echo "Monitor:"
echo "  ssh ${TARGET} 'tail -f /var/log/paper-live/testnet-btcusdt.log'"
echo ""
echo "Status:"
echo "  ssh ${TARGET} 'systemctl status testnet-engine'"
echo ""
echo "Stop after 24h:"
echo "  ./deploy/deploy_testnet.sh stop"
