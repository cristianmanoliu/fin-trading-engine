#!/usr/bin/env bash
# Syncs the local codebase to the VPS and rebuilds all binaries.
# Usage: ./deploy/sync.sh [user@host]
set -euo pipefail

ROOT="$(cd "$(dirname "$0")/.." && pwd)"
TARGET="${1:-root@178.105.24.230}"
REMOTE_DIR="/opt/trading-engine"

SSH_KEY="${HOME}/.ssh/id_ed25519"
SSH_OPTS="-i ${SSH_KEY} -o StrictHostKeyChecking=accept-new"

echo "→ Syncing to ${TARGET}:${REMOTE_DIR}..."
rsync -az --delete \
    -e "ssh ${SSH_OPTS}" \
    --exclude='.git/' \
    --exclude='bin/' \
    --exclude='logs/' \
    --exclude='data/' \
    "${ROOT}/" \
    "${TARGET}:${REMOTE_DIR}/"

# Funding CSVs for P4-Combined / P4-Shorts-Only — small (~330k rows total) and load-bearing.
# Excluded from the main sync (which excludes data/) so we ship them explicitly here.
if [[ -d "${ROOT}/data/funding" ]]; then
    echo "→ Syncing data/funding/ (Binance funding rate history)..."
    ssh ${SSH_OPTS} "${TARGET}" "mkdir -p ${REMOTE_DIR}/data/funding"
    rsync -az --delete \
        -e "ssh ${SSH_OPTS}" \
        "${ROOT}/data/funding/" \
        "${TARGET}:${REMOTE_DIR}/data/funding/"
fi

echo "→ Building binaries on ${TARGET}..."
# shellcheck disable=SC2029
ssh ${SSH_OPTS} "${TARGET}" bash <<EOF
set -euo pipefail
export PATH="\$PATH:/usr/local/go/bin"
cd "${REMOTE_DIR}"
mkdir -p bin
go build -o bin/engine         ./cmd/engine
go build -o bin/backtest       ./cmd/backtest
go build -o bin/journal_report ./cmd/journal_report
# Operator tools — kill_switch (Path C real-money close-all per
# auto_kill_execution_decision_rule_2026-05-08.md) and journal_diff
# (Layer 3 shadow-mode parity gate).
go build -o bin/kill_switch    ./cmd/kill_switch
go build -o bin/journal_diff   ./cmd/journal_diff
echo "✓ Build complete: \$(ls -lh bin/)"
EOF

echo "✓ Sync and build done. Run 'ssh ${TARGET}' to continue setup."
