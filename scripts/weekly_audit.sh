#!/usr/bin/env bash
# weekly_audit.sh — operational wrapper combining the two artifacts the
# Sunday-09:00 launchd job produces:
#
#   1. run_drift_check.sh — decision-grade kill signal (exit 0/1/2/3/4)
#   2. forward_paper_status.sh — operational snapshot, dated under
#      results/forward_paper_snapshots/<YYYY-MM-DD>.txt for longitudinal
#      diffing
#
# Drift check exits with the decision-grade severity; this wrapper
# preserves that as its OWN exit code so launchd's last-exit-code column
# (visible via `launchctl list`) reflects the kill signal, not snapshot
# success.
#
# Snapshot failure is non-fatal — a transient SSH or jq error at snapshot
# time should not mask a real drift verdict. The drift check ran first
# anyway, so the decision-grade signal is already persisted to
# results/drift_check_history.jsonl regardless of what happens here.
#
# Usage:
#   ./scripts/weekly_audit.sh              # default — invoked by launchd
#   ./scripts/weekly_audit.sh --no-snapshot  # only run drift check
#
# Composable with deploy/drift-check.launchd.plist; the plist's
# ProgramArguments points at this wrapper instead of run_drift_check.sh
# directly.
set -uo pipefail

WITH_SNAPSHOT=1
while [[ $# -gt 0 ]]; do
    case "$1" in
        --no-snapshot) WITH_SNAPSHOT=0; shift ;;
        --help|-h) sed -n '2,28p' "$0" | sed 's/^# \{0,1\}//'; exit 0 ;;
        *) echo "unknown arg: $1" >&2; exit 1 ;;
    esac
done

REPO_ROOT="$(cd "$(dirname "$0")/.." && pwd)"
cd "$REPO_ROOT"

# --- 1. Drift check (decision-grade signal) ---
"${REPO_ROOT}/scripts/run_drift_check.sh" --quiet
DRIFT_EXIT=$?

# --- 2. Forward-paper snapshot (operational telemetry) ---
if [[ "$WITH_SNAPSHOT" -eq 1 ]]; then
    SNAPSHOT_DIR="${REPO_ROOT}/results/forward_paper_snapshots"
    mkdir -p "$SNAPSHOT_DIR"
    SNAPSHOT_FILE="${SNAPSHOT_DIR}/$(date -u +%Y-%m-%d).txt"
    if "${REPO_ROOT}/scripts/forward_paper_status.sh" > "$SNAPSHOT_FILE" 2>&1; then
        echo "snapshot: $SNAPSHOT_FILE"
    else
        echo "snapshot FAILED (non-fatal — drift signal preserved): see $SNAPSHOT_FILE" >&2
    fi
fi

# --- exit with the drift wrapper's code so launchd surfaces the right thing ---
exit "$DRIFT_EXIT"
