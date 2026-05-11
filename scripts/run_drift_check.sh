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
#   5  HISTORY_CORRUPT    — malformed line(s) in drift_check_history.jsonl;
#                           two-firings rule cannot be evaluated until repaired
#
# Paths (env-overridable for testing):
#   DRIFT_CHECK_DETECTOR   — path to live_vs_backtest_drift.py
#   DRIFT_CHECK_HISTORY    — path to drift_check_history.jsonl
#   DRIFT_CHECK_RUNS_DIR   — directory for per-run logs
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

# --- paths (env-overridable for testing) ---
REPO_ROOT="$(cd "$(dirname "$0")/.." && pwd)"
DETECTOR="${DRIFT_CHECK_DETECTOR:-${REPO_ROOT}/scripts/live_vs_backtest_drift.py}"
RUNS_DIR="${DRIFT_CHECK_RUNS_DIR:-${REPO_ROOT}/results/drift_runs}"
HISTORY="${DRIFT_CHECK_HISTORY:-${REPO_ROOT}/results/drift_check_history.jsonl}"
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

# Per-line lenient parse. Each malformed line is counted into MALFORMED
# rather than aborting the script under set -e + pipefail. Without this
# guard, a single bad append (kill -9 mid-write, disk-full, manual edit)
# crashes the wrapper with jq's parse code (~5), bypasses the case→
# Telegram dispatch below, and weekly_audit propagates the unmapped exit
# to launchd as the day's verdict — a decision-grade tool silently dying
# on the only failure mode that matters. Pinned by T8-T11 of
# scripts/test_run_drift_check.sh.
FIRINGS=()
MALFORMED=0
MALFORMED_DETAILS=()
LINE_NUM=0
while IFS= read -r line; do
    LINE_NUM=$((LINE_NUM + 1))
    [[ -z "$line" ]] && continue
    # jq -e returns non-zero on parse failure; the if-test absorbs it so
    # set -e does NOT abort.
    if ! verdict=$(echo "$line" | jq -er '.verdict // ""' 2>/dev/null); then
        MALFORMED=$((MALFORMED + 1))
        MALFORMED_DETAILS+=("L${LINE_NUM}: malformed JSON")
        continue
    fi
    [[ "$verdict" != "DRIFT_FIRED" ]] && continue
    if ! ts=$(echo "$line" | jq -er '.ts // ""' 2>/dev/null) || [[ -z "$ts" ]]; then
        MALFORMED=$((MALFORMED + 1))
        MALFORMED_DETAILS+=("L${LINE_NUM}: DRIFT_FIRED with missing .ts")
        continue
    fi
    # Pre-validate the timestamp parses — to_epoch failure under set -e
    # would otherwise abort the same way as the original bug.
    if ! to_epoch "$ts" >/dev/null 2>&1; then
        MALFORMED=$((MALFORMED + 1))
        MALFORMED_DETAILS+=("L${LINE_NUM}: malformed .ts=${ts}")
        continue
    fi
    FIRINGS+=("$ts")
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

# Defensive: if the history has any corrupt lines, suppress AUTO-KILL.
# The two-firings rule cannot be trusted against partial data; the
# operator must repair the file before treating any rule trip as
# decision-grade. Pinned by T11.
if [[ "$MALFORMED" -gt 0 ]] && [[ "$RULE_TRIPPED" -eq 1 ]]; then
    RULE_TRIPPED=0
    TRIP_PAIR="(suppressed — history corrupt)"
fi

# --- decide wrapper exit code ---
# Precedence: AUTO-KILL > INVESTIGATION > HISTORY_CORRUPT > ERROR > INSUFF > CLEAN.
# History corruption sits below the current run's drift firing because a
# fresh DRIFT_FIRED is still a real signal worth investigating, just
# without the rule context. The CLEAN-but-corrupt case routes through
# exit 5 instead of silently exiting 0.
if [[ "$RULE_TRIPPED" -eq 1 ]]; then
    WRAPPER_EXIT=4
elif [[ "$DETECTOR_EXIT" == "1" ]]; then
    WRAPPER_EXIT=1
elif [[ "$MALFORMED" -gt 0 ]]; then
    WRAPPER_EXIT=5
elif [[ "$DETECTOR_EXIT" == "2" ]]; then
    WRAPPER_EXIT=2
elif [[ "$DETECTOR_EXIT" == "0" ]]; then
    WRAPPER_EXIT=0
else
    WRAPPER_EXIT=3
fi

# --- output ---
verdict_line() {
    local main
    case "$WRAPPER_EXIT" in
        0) main="drift_check: CLEAN" ;;
        1) main="drift_check: INVESTIGATION (single firing — cross-check forward-paper)" ;;
        2) main="drift_check: INSUFFICIENT (n_live below floor)" ;;
        3) main="drift_check: ERROR (see $RUN_LOG)" ;;
        4) main="drift_check: AUTO-KILL CANDIDATE (firings ≥7d apart: $TRIP_PAIR)" ;;
        5) main="drift_check: HISTORY_CORRUPT (${MALFORMED} malformed line(s) in $HISTORY)" ;;
    esac
    # Surface corruption alongside the main verdict so an INVESTIGATION
    # or AUTO-KILL pair isn't treated as fully decision-grade when the
    # underlying history is questionable. (Exit 5 already names it.)
    if [[ "$MALFORMED" -gt 0 ]] && [[ "$WRAPPER_EXIT" != "5" ]]; then
        main+=" [+ HISTORY_CORRUPT: ${MALFORMED} malformed lines]"
    fi
    echo "$main"
}

# Telegram alert path uses the shared scripts/lib/notify.sh helper which
# implements tier-aware retry (CRITICAL 3 / WARN 2 / INFO 1) and graceful
# no-op when env vars unset, mirroring the Go engine's pkg/notify package.
# Closes the loop between the decision-grade signal and the operator's
# phone — without it, a Sunday-09:00 launchd firing of AUTO-KILL CANDIDATE
# only surfaces in launchd.out.log.
# shellcheck source=lib/notify.sh
source "${REPO_ROOT}/scripts/lib/notify.sh"

# Dispatch alerts on exit codes 1, 3, 4. Codes 0 (CLEAN) and 2 (INSUFFICIENT)
# are routine — alerting on them produces the noise the kill-bar
# mis-calibration finding warned against. Code 3 (ERROR) means the
# detector itself broke — silent failure of a decision-grade tool, must
# alert. Code 4 is the explicit auto-kill candidate.
case "$WRAPPER_EXIT" in
    1) notify_telegram WARN "drift_check on $(hostname)" "$(verdict_line)
log: $RUN_LOG" ;;
    3) notify_telegram WARN "drift_check on $(hostname)" "$(verdict_line)
detector itself failed — investigate before next run" ;;
    4) notify_telegram CRITICAL "drift_check on $(hostname)" "$(verdict_line)
log: $RUN_LOG
This is a decision-grade kill candidate per the time-to-detection verdict. Cross-check forward_paper_status.sh + the drift run logs before acting." ;;
    5) notify_telegram WARN "drift_check on $(hostname)" "$(verdict_line)
log: $RUN_LOG
Two-firings rule cannot be evaluated until $HISTORY is repaired.
Sample malformed entries:
$(printf '  %s\n' "${MALFORMED_DETAILS[@]:0:5}")
Detector's most-recent verdict was: $VERDICT (detector exit $DETECTOR_EXIT)" ;;
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
# Per-line lenient parse — a malformed entry prints as "(malformed)" rather
# than aborting under set -e + pipefail. Same shape as the FIRINGS walk.
tail -5 "$HISTORY" | awk '{lines[NR]=$0} END {for (i=NR; i>=1; i--) print lines[i]}' \
    | while IFS= read -r _entry; do
        if _parsed=$(echo "$_entry" | jq -r '"    \(.ts)  exit=\(.exit_code)  \(.verdict)"' 2>/dev/null); then
            echo "$_parsed"
        else
            echo "    (malformed: ${_entry:0:80}...)"
        fi
    done
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
