#!/usr/bin/env bash
# deploy_layer3.sh — enable Layer 3 shadow-parity wrap on specified live engines.
#
# Pre-reg: results/layer3_enablement_2026-05-19.md.
#
# Usage:
#   ./deploy/deploy_layer3.sh                         # default symbols: kavausdt ensusdt
#   ./deploy/deploy_layer3.sh kavausdt ensusdt grtusdt
#   ./deploy/deploy_layer3.sh stop                    # remove Layer 3 wrap from all paper-live
#
# What it does:
#   1. Sync code to VPS.
#   2. Stop any standalone testnet engines for the same symbols (replaced by Layer 3 wrap).
#   3. Install drop-in /etc/systemd/system/paper-live@<sym>.service.d/layer3.conf.
#   4. systemctl daemon-reload + restart the affected paper-live@<sym>.service.
#   5. Verify the TeeExecutor wrap is active by checking the log for the
#      "LAYER 3 SHADOW MODE ACTIVE" startup line.
#
# Reversal (per pre-reg):
#   ./deploy/deploy_layer3.sh stop
set -euo pipefail

ROOT="$(cd "$(dirname "$0")/.." && pwd)"
TARGET="root@178.105.24.230"
TESTNET_ENV="/etc/paper-live/testnet-env"
DROPIN_SRC="${ROOT}/deploy/systemd/layer3.conf"
DEFAULT_SYMBOLS=(kavausdt ensusdt)
MAX_INSTANCES=4   # rate-limit guardrail (testnet pool is generous but
                  # keep parity with deploy_testnet.sh for sanity)

if [[ ! -f "$DROPIN_SRC" ]]; then
    echo "ERROR: drop-in source not found at $DROPIN_SRC" >&2
    exit 1
fi

# ── Stop path: remove Layer 3 wrap from ALL paper-live engines ──────────────
if [[ "${1:-}" == "stop" ]]; then
    echo "→ Removing Layer 3 drop-ins from all paper-live engines..."
    ssh "${TARGET}" 'set -e
        any=0
        for d in /etc/systemd/system/paper-live@*.service.d; do
            [[ -d "$d" ]] || continue
            if [[ -f "$d/layer3.conf" ]]; then
                echo "  removing $d/layer3.conf"
                rm "$d/layer3.conf"
                # Clean up the directory if empty (cosmetic)
                rmdir "$d" 2>/dev/null || true
                any=1
            fi
        done
        if [[ $any -eq 0 ]]; then
            echo "  (no Layer 3 drop-ins found)"
            exit 0
        fi
        systemctl daemon-reload
        # Restart any paper-live engines whose drop-in we removed. Cheap
        # to restart all — journal-replay recovers state.
        echo "  → restarting paper-live engines that had Layer 3 drop-ins"
        for u in $(systemctl list-units --type=service --no-legend "paper-live@*.service" 2>/dev/null | awk "{print \$1}"); do
            systemctl restart "$u" || echo "  restart failed: $u"
        done
    '
    echo "✓ Layer 3 wrap removed."
    exit 0
fi

# ── Symbol parsing + validation ──────────────────────────────────────────────
if [[ $# -gt 0 ]]; then
    SYMBOLS=("$@")
else
    SYMBOLS=("${DEFAULT_SYMBOLS[@]}")
fi

if [[ ${#SYMBOLS[@]} -gt ${MAX_INSTANCES} ]]; then
    echo "ERROR: requested ${#SYMBOLS[@]} symbols, max is ${MAX_INSTANCES}." >&2
    exit 1
fi

# Lowercase + config existence check.
for i in "${!SYMBOLS[@]}"; do
    sym="${SYMBOLS[$i]}"
    sym_lc="$(echo "${sym}" | tr '[:upper:]' '[:lower:]')"
    SYMBOLS[i]="${sym_lc}"
    if [[ ! -f "${ROOT}/configs/${sym_lc}.yaml" ]]; then
        echo "ERROR: configs/${sym_lc}.yaml not found." >&2
        exit 1
    fi
done

echo "→ Enabling Layer 3 wrap on: ${SYMBOLS[*]}"

# ── Sync code (binaries unchanged but keep canonical) ───────────────────────
"${ROOT}/deploy/sync.sh" "${TARGET}"

# ── Verify testnet creds present (drop-in references the file) ──────────────
# shellcheck disable=SC2029 # client-side TESTNET_ENV expansion is intentional
if ! ssh "${TARGET}" "test -f ${TESTNET_ENV}"; then
    echo "ERROR: ${TESTNET_ENV} not found on VPS." >&2
    echo "       Drop-in references it. Deploy testnet creds first via:" >&2
    echo "         BINANCE_TESTNET_KEY=... BINANCE_TESTNET_SECRET=... ./deploy/deploy_testnet.sh" >&2
    exit 1
fi

# ── Stop standalone testnet engines that overlap with Layer 3 symbols ──────
echo "→ Stopping standalone testnet engines for Layer 3 symbols..."
for sym in "${SYMBOLS[@]}"; do
    # shellcheck disable=SC2029 # client-side sym expansion intentional
    ssh "${TARGET}" "
        if systemctl is-active --quiet testnet-engine@${sym}.service; then
            echo \"  stopping testnet-engine@${sym}.service\"
            systemctl stop testnet-engine@${sym}.service
            systemctl disable testnet-engine@${sym}.service 2>/dev/null || true
        fi
    "
done

# ── Create Layer 3 journal directory ────────────────────────────────────────
ssh "${TARGET}" 'mkdir -p /var/log/paper-live/journal/layer3 && \
    chown paperlive:paperlive /var/log/paper-live/journal/layer3'

# ── Install drop-in + restart each affected engine ──────────────────────────
echo "→ Copying drop-in source to VPS..."
scp -q "$DROPIN_SRC" "${TARGET}:/opt/trading-engine/deploy/systemd/layer3.conf"

for sym in "${SYMBOLS[@]}"; do
    echo "→ Installing drop-in for paper-live@${sym}.service"
    # shellcheck disable=SC2029 # client-side sym expansion intentional
    ssh "${TARGET}" "
        mkdir -p /etc/systemd/system/paper-live@${sym}.service.d
        install -m 644 /opt/trading-engine/deploy/systemd/layer3.conf \
            /etc/systemd/system/paper-live@${sym}.service.d/layer3.conf
    "
done

ssh "${TARGET}" 'systemctl daemon-reload'

echo "→ Restarting paper-live engines for Layer 3 activation..."
for sym in "${SYMBOLS[@]}"; do
    # shellcheck disable=SC2029 # client-side sym expansion intentional
    ssh "${TARGET}" "systemctl restart paper-live@${sym}.service"
done

echo ""
echo "✓ Layer 3 wrap deployed on: ${SYMBOLS[*]}"
echo ""
echo "Verify activation:"
for sym in "${SYMBOLS[@]}"; do
    echo "  ssh ${TARGET} 'grep \"LAYER 3 SHADOW MODE ACTIVE\" /var/log/paper-live/${sym}.log | tail -1'"
done
echo ""
echo "Verdict run (after ≥7 days):"
echo "  ssh ${TARGET} 'cd /opt/trading-engine && bash scripts/layer3_verdict.sh \\"
echo "    --stub-dir /var/log/paper-live/journal \\"
echo "    --testnet-dir /var/log/paper-live/journal/layer3 \\"
echo "    --threshold-pct 0.5 --min-days 7'"
echo ""
echo "Reversal:"
echo "  ./deploy/deploy_layer3.sh stop"
