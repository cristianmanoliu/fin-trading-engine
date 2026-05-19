#!/usr/bin/env bash
# layer3_cron.sh — weekly Layer 3 shadow-parity verdict + state-tracking +
# Telegram alerts on state change.
#
# Composes scripts/layer3_verdict.sh (the decision-grade gate) into a passive
# operator-facing pipeline. Once enabled via deploy/install_layer3_cron.sh, the
# operator sees Telegram only when state CHANGES (PASS becomes available, or
# parity FAILS) — INSUFFICIENT_DURATION during the initial 7d window is silent
# by design.
#
# Per real_money_executor_architecture_decision_rule_2026-05-08.md, Layer 3
# PASS is one of the prerequisites for STAGE_1 promotion. Cron-driving the
# verdict makes the gate transition mechanically observable instead of
# requiring the operator to remember to invoke layer3_verdict.sh.
#
# Usage:
#   ./scripts/layer3_cron.sh                  # default — VPS production paths
#   ./scripts/layer3_cron.sh --quiet          # cron: 1-line stdout
#   ./scripts/layer3_cron.sh --dry-run        # print Telegram payloads, don't send
#
# Cadence: WEEKLY, Sunday 10:00 UTC (Layer 3 needs ≥7d window; running
# more often is wasteful and adds Telegram noise — matches the cadence
# discipline from drift_wrapper_cron_decision_rule_2026-05-08.md).
#
# Exit codes (mirror layer3_verdict.sh upstream):
#   0  PASS                  — Layer 3 parity criterion met
#   1  THRESHOLD             — pnl drift exceeds threshold (FAIL)
#   2  SIGNAL_DIVERGENCE     — executors disagreed (FAIL)
#   3  INPUT_ERROR           — config / missing dir / no data
#   4  INSUFFICIENT_DURATION — window <7d (expected during initial period)
#   5  UNEXPECTED            — wrapper itself broke
#
# Paths (env-overridable for testing — keep parity with run_drift_check.sh style):
#   LAYER3_CRON_VERDICT     — path to layer3_verdict.sh (default: ../scripts/layer3_verdict.sh)
#   LAYER3_CRON_STUB_DIR    — Layer 3 stub dir (default: /var/log/paper-live/journal)
#   LAYER3_CRON_TESTNET_DIR — Layer 3 testnet shadow dir (default: /var/log/paper-live/journal/layer3)
#   LAYER3_CRON_HISTORY     — append-only state log (default: /var/log/paper-live/layer3_history.jsonl)
#   LAYER3_CRON_RUNS_DIR    — per-run verdict logs (default: /var/log/paper-live/layer3_runs/)
#   LAYER3_VERDICT_DIFF_OVERRIDE — passed through to layer3_verdict.sh
#                                  (default: /opt/trading-engine/bin/journal_diff if present)
set -euo pipefail

# --- args ---
QUIET=0
DRY_RUN=0
while [[ $# -gt 0 ]]; do
    case "$1" in
        --quiet)   QUIET=1; shift ;;
        --dry-run) DRY_RUN=1; shift ;;
        --help|-h)
            sed -n '2,40p' "$0" | sed 's/^# \{0,1\}//'
            exit 0
            ;;
        *) echo "ERROR: unknown flag: $1" >&2; exit 5 ;;
    esac
done

# --- jq pre-flight ---
# Same fail-fast pattern as run_drift_check.sh: a missing-jq install would
# otherwise produce opaque rc=127 deep in the script, AFTER the verdict
# binary ran. Surface a clear diagnostic up front.
if ! command -v jq >/dev/null 2>&1; then
    echo "ERROR: jq is required but not found in PATH." >&2
    echo "  Install with 'apt install jq' (Linux) or 'brew install jq' (macOS)." >&2
    exit 5
fi

# --- paths ---
SCRIPT_ROOT="$(cd "$(dirname "$0")/.." && pwd)"
VERDICT="${LAYER3_CRON_VERDICT:-${SCRIPT_ROOT}/scripts/layer3_verdict.sh}"
STUB_DIR="${LAYER3_CRON_STUB_DIR:-/var/log/paper-live/journal}"
TESTNET_DIR="${LAYER3_CRON_TESTNET_DIR:-/var/log/paper-live/journal/layer3}"
HISTORY="${LAYER3_CRON_HISTORY:-/var/log/paper-live/layer3_history.jsonl}"
RUNS_DIR="${LAYER3_CRON_RUNS_DIR:-/var/log/paper-live/layer3_runs}"

# If LAYER3_VERDICT_DIFF_OVERRIDE wasn't set by the caller (cron is the
# common case here), prefer the pre-built binary on the VPS to avoid the
# script trying to `go build` (go isn't in the non-interactive PATH).
if [[ -z "${LAYER3_VERDICT_DIFF_OVERRIDE:-}" ]] && [[ -x /opt/trading-engine/bin/journal_diff ]]; then
    export LAYER3_VERDICT_DIFF_OVERRIDE=/opt/trading-engine/bin/journal_diff
fi

mkdir -p "$RUNS_DIR"
mkdir -p "$(dirname "$HISTORY")"

# --- Telegram helper ---
# shellcheck source=lib/notify.sh
source "${SCRIPT_ROOT}/scripts/lib/notify.sh"

# --- previous state lookup ---
# Read the last appended row's verdict (if any). Used to gate INFO-tier
# transition alerts. jq -e returns 1 on null/empty — fall back to "INITIAL".
PREV_STATE="INITIAL"
if [[ -f "$HISTORY" ]] && [[ -s "$HISTORY" ]]; then
    PREV_STATE=$(tail -n 1 "$HISTORY" | jq -r '.state // "INITIAL"' 2>/dev/null || echo "INITIAL")
fi

# --- run the verdict ---
RUN_TS=$(date -u '+%Y-%m-%dT%H-%M-%SZ')
RUN_LOG="${RUNS_DIR}/layer3_verdict_${RUN_TS}.log"

set +e
"$VERDICT" \
    --stub-dir "$STUB_DIR" \
    --testnet-dir "$TESTNET_DIR" \
    > "$RUN_LOG" 2>&1
VERDICT_EXIT=$?
set -e

# Map upstream exit → our state label
case "$VERDICT_EXIT" in
    0) STATE="PASS" ;;
    1) STATE="THRESHOLD" ;;
    2) STATE="SIGNAL_DIVERGENCE" ;;
    3) STATE="INPUT_ERROR" ;;
    4) STATE="INSUFFICIENT_DURATION" ;;
    *) STATE="UNEXPECTED_${VERDICT_EXIT}" ;;
esac

# --- append history row ---
HOST=$(hostname)
NOW_ISO=$(date -u '+%Y-%m-%dT%H:%M:%SZ')
HISTORY_ROW=$(jq -cn \
    --arg ts "$NOW_ISO" \
    --arg state "$STATE" \
    --argjson exit_code "$VERDICT_EXIT" \
    --arg prev "$PREV_STATE" \
    --arg log "$RUN_LOG" \
    --arg host "$HOST" \
    '{ts: $ts, state: $state, exit_code: $exit_code, prev_state: $prev, log: $log, host: $host}')
echo "$HISTORY_ROW" >> "$HISTORY"

# --- Telegram dispatch ---
# Tier rules:
#   - PASS (initial transition): INFO   — STAGE_1 unblocked
#   - PASS → PASS:                silent — no news
#   - INSUFFICIENT_DURATION:      silent — expected pre-7d
#   - THRESHOLD or SIGNAL_DIV:    CRITICAL — gate FAILED, every week
#   - INPUT_ERROR:                WARN — config or empty data
#   - UNEXPECTED:                 WARN — wrapper or upstream broke
ALERT_TIER=""
ALERT_TITLE=""
ALERT_BODY=""

verdict_summary() {
    grep -E '^>>> VERDICT' "$RUN_LOG" 2>/dev/null | tail -1 || \
        echo "exit=${VERDICT_EXIT} state=${STATE} (no verdict line found)"
}

case "$STATE" in
    PASS)
        if [[ "$PREV_STATE" != "PASS" ]]; then
            ALERT_TIER=INFO
            ALERT_TITLE="layer3_cron on ${HOST}: PASS (was ${PREV_STATE})"
            ALERT_BODY="Layer 3 shadow-parity gate PASSed.
$(verdict_summary)
log: $RUN_LOG
Next: per real_money_protocol_decision_rule_2026-05-08.md, operator may now consider promoting to STAGE_1 (\$100/trade) once forward-paper + Layer 2 gates also GREEN."
        fi
        ;;
    THRESHOLD|SIGNAL_DIVERGENCE)
        ALERT_TIER=CRITICAL
        ALERT_TITLE="layer3_cron on ${HOST}: ${STATE}"
        ALERT_BODY="Layer 3 shadow-parity FAILED.
$(verdict_summary)
log: $RUN_LOG
This is a STAGE_1 promotion blocker. Operator MUST investigate executor parity before any real-money flip. See locked rule: results/real_money_executor_architecture_decision_rule_2026-05-08.md"
        ;;
    INPUT_ERROR)
        # Distinguish "expected empty during pre-fill window" from real config error.
        # The verdict's own log line indicates which — surface it as-is.
        if [[ "$PREV_STATE" != "INPUT_ERROR" ]]; then
            ALERT_TIER=WARN
            ALERT_TITLE="layer3_cron on ${HOST}: INPUT_ERROR (was ${PREV_STATE})"
            ALERT_BODY="Layer 3 verdict could not run.
$(verdict_summary)
log: $RUN_LOG
Likely cause: testnet dir empty (no fills yet — expected pre-first-event) or path mismatch."
        fi
        ;;
    INSUFFICIENT_DURATION)
        : # silent — expected during the initial 7d window
        ;;
    *)
        ALERT_TIER=WARN
        ALERT_TITLE="layer3_cron on ${HOST}: ${STATE}"
        ALERT_BODY="Layer 3 verdict produced unexpected exit ${VERDICT_EXIT}.
$(verdict_summary)
log: $RUN_LOG
Wrapper itself may be broken — review before next run."
        ;;
esac

if [[ -n "$ALERT_TIER" ]]; then
    if [[ "$DRY_RUN" == "1" ]]; then
        echo "DRY_RUN: would send ${ALERT_TIER}:"
        echo "  title: ${ALERT_TITLE}"
        echo "  body:"
        echo "${ALERT_BODY}" | sed 's/^/    /'
    else
        notify_telegram "$ALERT_TIER" "$ALERT_TITLE" "$ALERT_BODY"
    fi
fi

# --- stdout summary ---
if [[ "$QUIET" == "1" ]]; then
    echo "layer3_cron: state=${STATE} exit=${VERDICT_EXIT} prev=${PREV_STATE} alert=${ALERT_TIER:-none}"
else
    echo "================================================================================"
    echo "  layer3_cron on ${HOST}"
    echo "  ts:        ${NOW_ISO}"
    echo "  state:     ${STATE} (exit ${VERDICT_EXIT})"
    echo "  prev:      ${PREV_STATE}"
    echo "  alert:     ${ALERT_TIER:-none (no state change worth alerting)}"
    echo "  log:       ${RUN_LOG}"
    echo "  history:   ${HISTORY}"
    echo "================================================================================"
    verdict_summary | sed 's/^/  /'
fi

exit "$VERDICT_EXIT"
