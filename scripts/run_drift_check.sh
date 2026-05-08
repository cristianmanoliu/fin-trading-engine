#!/usr/bin/env bash
# run_drift_check.sh — operational wrapper around live_vs_backtest_drift.py.
#
# Implements the locked operational rule from the time-to-detection verdict
# (results/drift_detector_time_to_detection_verdict_2026-05-08.md):
#
#   Daily sequential testing inflates FP to 28%/year. The drift detector
#   should run weekly (or per-N every ~50 new trades), and a SINGLE firing
#   is investigation-grade — not auto-kill. Two firings ≥7 days apart, OR
#   a firing that coincides with a threshold-kill match, escalates to
#   strong/auto-kill.
#
# This wrapper persists per-run state so the "two firings ≥7d apart" rule
# becomes mechanically evaluable across sessions:
#
#   results/drift_check_history.jsonl   — append-only rule-eval index
#   results/drift_runs/<ts>.log         — full per-run output for audit
#
# Usage:
#   ./scripts/run_drift_check.sh                            # default (VPS)
#   ./scripts/run_drift_check.sh --quiet                    # cron: 1-line out
#   ./scripts/run_drift_check.sh --live-source local --live-dir ./logs/journal
#                                                            # extra args pass-through
#
# Exit codes (distinct so cron can react by severity):
#   0  CLEAN              — no drift, no rule trip
#   1  INVESTIGATION      — single drift firing in history
#   2  INSUFFICIENT       — n_live below detector's floor (pass-through)
#   3  ERROR              — detector itself failed
#   4  AUTO-KILL CANDIDATE — two firings ≥7 days apart
#
# Cadence reminder: WEEKLY, not daily. Daily inflates FP to ~28%/year.
set -euo pipefail

# --- args ---
QUIET=0
DETECTOR_ARGS=()
while [[ $# -gt 0 ]]; do
    case "$1" in
        --quiet) QUIET=1; shift ;;
        --help|-h)
            sed -n '2,32p' "$0" | sed 's/^# \{0,1\}//'
            exit 0
            ;;
        *) DETECTOR_ARGS+=("$1"); shift ;;
    esac
done

# --- paths ---
REPO_ROOT="$(cd "$(dirname "$0")/.." && pwd)"
DETECTOR="${REPO_ROOT}/scripts/live_vs_backtest_drift.py"
RUNS_DIR="${REPO_ROOT}/results/drift_runs"
HISTORY="${REPO_ROOT}/results/drift_check_history.jsonl"
mkdir -p "$RUNS_DIR"
[[ -f "$HISTORY" ]] || : > "$HISTORY"

if [[ ! -x "$DETECTOR" ]] && [[ ! -f "$DETECTOR" ]]; then
    echo "drift detector not found at $DETECTOR" >&2
    exit 3
fi

# --- run the detector ---
NOW_ISO=$(date -u '+%Y-%m-%dT%H:%M:%SZ')
RUN_LOG="${RUNS_DIR}/${NOW_ISO}.log"

# Capture exit code without `set -e` aborting on non-zero (1 = drift, 2 = insuff).
set +e
if [[ ${#DETECTOR_ARGS[@]} -gt 0 ]]; then
    python3 "$DETECTOR" "${DETECTOR_ARGS[@]}" > "$RUN_LOG" 2>&1
else
    python3 "$DETECTOR" > "$RUN_LOG" 2>&1
fi
DETECTOR_EXIT=$?
set -e

case "$DETECTOR_EXIT" in
    0) VERDICT="CLEAN" ;;
    1) VERDICT="DRIFT_FIRED" ;;
    2) VERDICT="INSUFFICIENT_DATA" ;;
    *) VERDICT="ERROR" ;;
esac

# --- append history record ---
jq -nc --arg ts "$NOW_ISO" \
       --argjson exit "$DETECTOR_EXIT" \
       --arg verdict "$VERDICT" \
       --arg log "$RUN_LOG" \
       '{ts: $ts, exit_code: $exit, verdict: $verdict, log_file: $log}' >> "$HISTORY"

# --- evaluate locked rule across full history ---
# Collect all DRIFT_FIRED timestamps (epoch seconds). The rule does not
# window — once two firings ≥7d apart exist, the rule trips. Operators
# can manually edit history if a firing is investigated and dismissed.
to_epoch() {
    # macOS BSD date OR GNU date — no portable single form.
    date -u -j -f "%Y-%m-%dT%H:%M:%SZ" "$1" '+%s' 2>/dev/null \
        || date -u -d "$1" '+%s'
}

FIRINGS=()
while IFS= read -r line; do
    [[ -z "$line" ]] && continue
    v=$(echo "$line" | jq -r '.verdict')
    if [[ "$v" == "DRIFT_FIRED" ]]; then
        FIRINGS+=("$(echo "$line" | jq -r '.ts')")
    fi
done < "$HISTORY"

RULE_TRIPPED=0
TRIP_PAIR=""
if [[ ${#FIRINGS[@]} -ge 2 ]]; then
    for ((i=0; i<${#FIRINGS[@]}-1; i++)); do
        for ((j=i+1; j<${#FIRINGS[@]}; j++)); do
            t1=$(to_epoch "${FIRINGS[i]}")
            t2=$(to_epoch "${FIRINGS[j]}")
            diff=$(( t2 > t1 ? t2 - t1 : t1 - t2 ))
            if [[ "$diff" -ge $((7 * 86400)) ]]; then
                RULE_TRIPPED=1
                TRIP_PAIR="${FIRINGS[i]} ↔ ${FIRINGS[j]}"
                break 2
            fi
        done
    done
fi

# --- decide wrapper exit code ---
if [[ "$RULE_TRIPPED" -eq 1 ]]; then
    WRAPPER_EXIT=4
elif [[ "$DETECTOR_EXIT" == "1" ]]; then
    WRAPPER_EXIT=1
elif [[ "$DETECTOR_EXIT" == "2" ]]; then
    WRAPPER_EXIT=2
elif [[ "$DETECTOR_EXIT" == "0" ]]; then
    WRAPPER_EXIT=0
else
    WRAPPER_EXIT=3
fi

# --- output ---
verdict_line() {
    case "$WRAPPER_EXIT" in
        0) echo "drift_check: CLEAN" ;;
        1) echo "drift_check: INVESTIGATION (single firing — cross-check forward-paper)" ;;
        2) echo "drift_check: INSUFFICIENT (n_live below floor)" ;;
        3) echo "drift_check: ERROR (see $RUN_LOG)" ;;
        4) echo "drift_check: AUTO-KILL CANDIDATE (firings ≥7d apart: $TRIP_PAIR)" ;;
    esac
}

# Telegram alert path. Closes the loop between the decision-grade signal and
# the operator's phone — without it, a Sunday-09:00 launchd firing of
# AUTO-KILL CANDIDATE only surfaces in launchd.out.log. Reads the same
# TELEGRAM_BOT_TOKEN / TELEGRAM_CHAT_ID env vars the engine uses; missing
# vars → silent no-op (same graceful-degrade pattern as the engine's
# notify package). Output is silenced so the bot token never appears in
# stdout / stderr (Go's net/http verbatim-URL leak is the rationale for
# the engine's redactErr helper; we avoid the analog here by sending all
# curl output to /dev/null and trusting curl's exit code only).
notify_telegram() {
    local severity="$1" body="$2"
    local token="${TELEGRAM_BOT_TOKEN:-}"
    local chat="${TELEGRAM_CHAT_ID:-}"
    [[ -z "$token" || -z "$chat" ]] && return 0
    local prefix
    case "$severity" in
        CRITICAL) prefix="🚨 [CRITICAL]" ;;
        WARN)     prefix="⚠ [WARN]" ;;
        *)        prefix="ℹ [INFO]" ;;
    esac
    curl -s --max-time 10 \
        -X POST "https://api.telegram.org/bot${token}/sendMessage" \
        --data-urlencode "chat_id=${chat}" \
        --data-urlencode "text=${prefix} drift_check on $(hostname)
${body}" >/dev/null 2>&1 || true
}

# Dispatch alerts on exit codes 1, 3, 4. Codes 0 (CLEAN) and 2 (INSUFFICIENT)
# are routine — alerting on them produces the noise the kill-bar
# mis-calibration finding warned against. Code 3 (ERROR) means the
# detector itself broke — silent failure of a decision-grade tool, must
# alert. Code 4 is the explicit auto-kill candidate.
case "$WRAPPER_EXIT" in
    1) notify_telegram WARN "$(verdict_line)
log: $RUN_LOG" ;;
    3) notify_telegram WARN "$(verdict_line)
detector itself failed — investigate before next run" ;;
    4) notify_telegram CRITICAL "$(verdict_line)
log: $RUN_LOG
This is a decision-grade kill candidate per the time-to-detection verdict. Cross-check forward_paper_status.sh + the drift run logs before acting." ;;
esac

if [[ "$QUIET" -eq 1 ]]; then
    echo "$(verdict_line) — $NOW_ISO"
    exit "$WRAPPER_EXIT"
fi

cat "$RUN_LOG"
echo
SEP=$(printf '%0.s─' {1..78})
echo "$SEP"
echo "  Operational rule history"
echo "$SEP"
echo "  Last 5 runs (most recent first):"
# Reverse-print last 5 history entries portably (no `tac` on BSD).
tail -5 "$HISTORY" | awk '{lines[NR]=$0} END {for (i=NR; i>=1; i--) print lines[i]}' \
    | jq -r '"    \(.ts)  exit=\(.exit_code)  \(.verdict)"'
echo
echo "  Drift firings in full history: ${#FIRINGS[@]}"
if [[ ${#FIRINGS[@]} -gt 0 ]]; then
    for ts in "${FIRINGS[@]}"; do
        echo "    - $ts"
    done
fi
echo
echo "  >>> $(verdict_line)"
echo "$SEP"

exit "$WRAPPER_EXIT"
