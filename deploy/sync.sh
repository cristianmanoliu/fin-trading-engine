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
#
# SD-1: previously, missing local data/funding/ silently skipped the sync.
# Engine on the VPS would then start with default per-day funding rates (the
# `--funding-csv-dir` fallback added by the cmd/engine 2nd-pass audit) and
# silently diverge from backtest assumptions. Operator on a fresh dev box
# would deploy without funding CSVs and not be told. Now: explicit warn so
# the gap surfaces at deploy time rather than weeks later when realized
# funding diverges from modeled.
if [[ -d "${ROOT}/data/funding" ]]; then
    echo "→ Syncing data/funding/ (Binance funding rate history)..."
    ssh ${SSH_OPTS} "${TARGET}" "mkdir -p ${REMOTE_DIR}/data/funding"
    # -u (--update): skip files where VPS mtime >= local mtime.
    # Protects CSVs written by the VPS-side funding_refresh_cron.sh (Sundays
    # 03:00 UTC) from being clobbered by stale local copies during redeploy.
    # --delete intentionally omitted: VPS-cron may create symbol files not yet
    # present locally; removing them would undo a successful refresh.
    rsync -azu \
        -e "ssh ${SSH_OPTS}" \
        "${ROOT}/data/funding/" \
        "${TARGET}:${REMOTE_DIR}/data/funding/"
else
    echo "⚠ ${ROOT}/data/funding/ does not exist locally — skipping funding CSV sync." >&2
    echo "   Engine will fall back to default per-day funding rates and diverge from" >&2
    echo "   backtest assumptions. To fix:" >&2
    echo "     ./scripts/refresh_funding.sh           # download fresh history" >&2
    echo "     ./deploy/sync.sh ${TARGET}             # re-run this sync" >&2
fi

echo "→ Installing systemd units on ${TARGET}..."
# Copy all unit files from source to the installed location and daemon-reload
# so that changes to deploy/systemd/ take effect on the next restart.
# Previously this only happened in install.sh (one-time bootstrap); omitting
# it here meant redeploy.sh would restart engines against stale unit configs
# with no error — a silent-fail identical to the fail-open class we've closed.
# Pattern mirrors deploy_testnet.sh:120-122. Drop-ins (layer3.conf) are
# managed separately by deploy_layer3.sh and are intentionally excluded.
ssh ${SSH_OPTS} "${TARGET}" "
    cp ${REMOTE_DIR}/deploy/systemd/paper-live@.service    /etc/systemd/system/ && \
    cp ${REMOTE_DIR}/deploy/systemd/paper-live.target      /etc/systemd/system/ && \
    cp ${REMOTE_DIR}/deploy/systemd/paper-live-watchdog.service /etc/systemd/system/ && \
    cp ${REMOTE_DIR}/deploy/systemd/paper-live-watchdog.timer   /etc/systemd/system/ && \
    cp ${REMOTE_DIR}/deploy/systemd/paper-live-digest.service   /etc/systemd/system/ && \
    cp ${REMOTE_DIR}/deploy/systemd/paper-live-digest.timer     /etc/systemd/system/ && \
    cp ${REMOTE_DIR}/deploy/systemd/testnet-engine@.service /etc/systemd/system/ && \
    chmod 644 /etc/systemd/system/paper-live* /etc/systemd/system/testnet-engine* && \
    systemctl daemon-reload"

echo "→ Building binaries on ${TARGET}..."
# shellcheck disable=SC2029,SC2087
# SC2029: ${SSH_OPTS} expands client-side, intentional (let SSH parse flags).
# SC2087: heredoc EOF unquoted on purpose — ${REMOTE_DIR} expands client-side
# (used for the `cd` path) while \$PATH stays escaped for server-side eval.
ssh ${SSH_OPTS} "${TARGET}" bash <<EOF
set -euo pipefail
export PATH="\$PATH:/usr/local/go/bin"
cd "${REMOTE_DIR}"
mkdir -p bin
go build -o bin/engine         ./cmd/engine
go build -o bin/backtest       ./cmd/backtest
go build -o bin/journal_report ./cmd/journal_report
# Operator tools — kill_switch (Path C real-money close-all per
# auto_kill_execution_decision_rule_2026-05-08.md), journal_diff
# (Layer 3 shadow-mode parity gate), journal_validate (self-consistency
# checker for paper-live JSONL journals).
go build -o bin/kill_switch     ./cmd/kill_switch
go build -o bin/journal_diff    ./cmd/journal_diff
go build -o bin/journal_validate ./cmd/journal_validate
echo "✓ Build complete: \$(ls -lh bin/)"
EOF

echo "✓ Sync and build done. Run 'ssh ${TARGET}' to continue setup."
