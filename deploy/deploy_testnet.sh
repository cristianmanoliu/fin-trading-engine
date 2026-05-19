#!/usr/bin/env bash
# Deploy Layer 2 testnet engine instances (one or more symbols) to VPS.
#
# Usage:
#   # First time — provide testnet credentials. Default symbols: kavausdt ensusdt.
#   BINANCE_TESTNET_KEY=... BINANCE_TESTNET_SECRET=... ./deploy/deploy_testnet.sh
#
#   # Deploy specific symbols (subsequent — creds already on VPS):
#   ./deploy/deploy_testnet.sh kavausdt ensusdt grtusdt
#
#   # Stop and remove ALL testnet instances:
#   ./deploy/deploy_testnet.sh stop
#
# Rate-limit budget (mainnet aggTrade weight pool — see
# results/testnet_multi_symbol_extension_2026-05-19.md):
#   16 live × 120/min + N testnet × 120/min ≤ 2400/min cap
#   → at most 4 testnet instances (24 total) before exhausting headroom.
#   Default of 2 leaves a 240/min margin.
set -euo pipefail

ROOT="$(cd "$(dirname "$0")/.." && pwd)"
TARGET="root@178.105.24.230"
TESTNET_ENV="/etc/paper-live/testnet-env"
DEFAULT_SYMBOLS=(kavausdt ensusdt)
MAX_INSTANCES=4   # rate-limit guardrail

# ── Stop path: tear down all testnet instances ──────────────────────────────
if [[ "${1:-}" == "stop" ]]; then
    echo "→ Stopping all testnet engine instances..."
    ssh "${TARGET}" 'set -e
        running="$(systemctl list-units --type=service --state=active --no-legend "testnet-engine*.service" 2>/dev/null | awk "{print \$1}")"
        if [[ -z "${running}" ]]; then
            echo "  (no active testnet engines)"
        else
            for unit in ${running}; do
                echo "  stopping ${unit}"
                systemctl stop "${unit}"
                systemctl disable "${unit}" 2>/dev/null || true
            done
        fi
        # Also stop legacy non-templated unit if it still exists.
        if systemctl list-unit-files testnet-engine.service >/dev/null 2>&1; then
            systemctl stop testnet-engine.service 2>/dev/null || true
            systemctl disable testnet-engine.service 2>/dev/null || true
        fi
    '
    echo "✓ Testnet engines stopped and disabled."
    exit 0
fi

# ── Symbol parsing + validation ──────────────────────────────────────────────
if [[ $# -gt 0 ]]; then
    SYMBOLS=("$@")
else
    SYMBOLS=("${DEFAULT_SYMBOLS[@]}")
fi

if [[ ${#SYMBOLS[@]} -gt ${MAX_INSTANCES} ]]; then
    echo "ERROR: requested ${#SYMBOLS[@]} symbols, max is ${MAX_INSTANCES} (rate-limit budget)." >&2
    echo "       See results/testnet_multi_symbol_extension_2026-05-19.md for math." >&2
    exit 1
fi

# Lowercase normalization + config existence check.
for i in "${!SYMBOLS[@]}"; do
    sym="${SYMBOLS[$i]}"
    sym_lc="$(echo "${sym}" | tr '[:upper:]' '[:lower:]')"
    SYMBOLS[i]="${sym_lc}"
    if [[ ! -f "${ROOT}/configs/${sym_lc}.yaml" ]]; then
        echo "ERROR: configs/${sym_lc}.yaml not found." >&2
        exit 1
    fi
done

echo "→ Deploying testnet engines for: ${SYMBOLS[*]}"

# ── Sync code + rebuild ──────────────────────────────────────────────────────
"${ROOT}/deploy/sync.sh" "${TARGET}"

# ── Create testnet credentials file if provided ──────────────────────────────
if [[ -n "${BINANCE_TESTNET_KEY:-}" ]] && [[ -n "${BINANCE_TESTNET_SECRET:-}" ]]; then
    echo "→ Writing testnet credentials to ${TESTNET_ENV}..."
    # shellcheck disable=SC2029 # client-side TESTNET_ENV expansion is intentional
    ssh "${TARGET}" "cat > ${TESTNET_ENV} << 'ENVEOF'
BINANCE_API_KEY=${BINANCE_TESTNET_KEY}
BINANCE_API_SECRET=${BINANCE_TESTNET_SECRET}
ENVEOF
chmod 600 ${TESTNET_ENV}
chown paperlive:paperlive ${TESTNET_ENV}"
    echo "✓ Testnet credentials saved."
else
    echo "→ No BINANCE_TESTNET_KEY provided — checking if ${TESTNET_ENV} already exists on VPS..."
    # shellcheck disable=SC2029 # client-side TESTNET_ENV expansion is intentional
    if ! ssh "${TARGET}" "test -f ${TESTNET_ENV}"; then
        echo "ERROR: ${TESTNET_ENV} not found on VPS." >&2
        echo "       Provide credentials:" >&2
        echo "         BINANCE_TESTNET_KEY=... BINANCE_TESTNET_SECRET=... ./deploy/deploy_testnet.sh" >&2
        exit 1
    fi
    echo "✓ Existing credentials found."
fi

# ── Stop + remove legacy non-templated unit if present ──────────────────────
ssh "${TARGET}" 'set -e
    if systemctl list-unit-files testnet-engine.service >/dev/null 2>&1; then
        echo "  → stopping/disabling legacy testnet-engine.service"
        systemctl stop testnet-engine.service 2>/dev/null || true
        systemctl disable testnet-engine.service 2>/dev/null || true
        if [[ -f /etc/systemd/system/testnet-engine.service ]]; then
            mv /etc/systemd/system/testnet-engine.service /etc/systemd/system/testnet-engine.service.bak
        fi
        systemctl daemon-reload
    fi
'

# ── Create journal directory ─────────────────────────────────────────────────
ssh "${TARGET}" "mkdir -p /var/log/paper-live/journal/testnet && chown -R paperlive:paperlive /var/log/paper-live/journal/testnet"

# ── Install templated unit + enable per-symbol instances ────────────────────
ssh "${TARGET}" "cp /opt/trading-engine/deploy/systemd/testnet-engine@.service /etc/systemd/system/ && \
    chmod 644 /etc/systemd/system/testnet-engine@.service && \
    systemctl daemon-reload"

for sym in "${SYMBOLS[@]}"; do
    echo "→ Enabling testnet-engine@${sym}.service"
    # shellcheck disable=SC2029 # client-side sym expansion is intentional
    ssh "${TARGET}" "systemctl enable testnet-engine@${sym}.service && systemctl restart testnet-engine@${sym}.service"
done

echo ""
echo "✓ Testnet engines deployed: ${SYMBOLS[*]}"
echo ""
echo "Monitor (all):"
for sym in "${SYMBOLS[@]}"; do
    echo "  ssh ${TARGET} 'tail -f /var/log/paper-live/testnet-${sym}.log'"
done
echo ""
echo "Status:"
echo "  ssh ${TARGET} 'systemctl status testnet-engine@*.service --no-pager'"
echo ""
echo "Stop all:"
echo "  ./deploy/deploy_testnet.sh stop"
