#!/usr/bin/env bash
# test_weekly_audit.sh — regression suite for scripts/weekly_audit.sh
# classifier helpers. Pins the Telegram-tier dual-sense fixes that prevent
# ssh-level failures from being routed to CRITICAL "journal_validate found
# errors" (F2) and python-helper crashes from silently mapping to "no
# alert" (F3-F5).
#
# Sources weekly_audit.sh under the BASH_SOURCE != $0 guard so only the
# function definitions load — main orchestration flow does NOT execute.

set -uo pipefail

SCRIPT_DIR="$(cd "$(dirname "$0")" && pwd)"
WRAPPER="${SCRIPT_DIR}/weekly_audit.sh"

if [[ ! -f "$WRAPPER" ]]; then
    echo "wrapper missing at $WRAPPER" >&2
    exit 1
fi

# shellcheck source=weekly_audit.sh
source "$WRAPPER"

PASS=0
FAIL=0
FAIL_LABELS=()

assert_eq() {
    local label="$1" actual="$2" expected="$3"
    if [[ "$actual" == "$expected" ]]; then
        echo "  ✓ $label"
        PASS=$((PASS + 1))
    else
        echo "  ✗ $label: expected '$expected', got '$actual'"
        FAIL=$((FAIL + 1))
        FAIL_LABELS+=("$label")
    fi
}

# ─────────────────────────────────────────────────────────────────────
# F2: _classify_validate_exit — distinguishes ssh-level failure from
# journal_validate corruption finding.
# ─────────────────────────────────────────────────────────────────────
echo "F2: _classify_validate_exit"
assert_eq "exit 0 → OK"             "$(_classify_validate_exit 0)"   "OK"
assert_eq "exit 1 → OK (warn)"      "$(_classify_validate_exit 1)"   "OK"
assert_eq "exit 2 → CORRUPTION"     "$(_classify_validate_exit 2)"   "CORRUPTION"
assert_eq "exit 3 → CORRUPTION"     "$(_classify_validate_exit 3)"   "CORRUPTION"
# REGRESSION: ssh connection failure must NOT route to CORRUPTION
assert_eq "exit 255 → SSH_FAILURE"  "$(_classify_validate_exit 255)" "SSH_FAILURE"
assert_eq "exit 127 → SSH_FAILURE"  "$(_classify_validate_exit 127)" "SSH_FAILURE"
assert_eq "exit 126 → SSH_FAILURE"  "$(_classify_validate_exit 126)" "SSH_FAILURE"
assert_eq "exit 130 → SSH_FAILURE"  "$(_classify_validate_exit 130)" "SSH_FAILURE"
assert_eq "exit 137 → SSH_FAILURE"  "$(_classify_validate_exit 137)" "SSH_FAILURE"
# REGRESSION: unexpected exit must NOT silently route to OK
assert_eq "exit 99 → UNEXPECTED"    "$(_classify_validate_exit 99)"  "UNEXPECTED"
assert_eq "exit 7 → UNEXPECTED"     "$(_classify_validate_exit 7)"   "UNEXPECTED"

# ─────────────────────────────────────────────────────────────────────
# F3-F5: _classify_python_exit — maps python helper exit codes to
# Telegram tiers per a per-helper alert-modes spec; UNEXPECTED catches
# everything outside the documented contract so a python crash never
# silently maps to "no alert."
# ─────────────────────────────────────────────────────────────────────
echo
echo "F3: _classify_python_exit (kill_protocol_check contract)"
KILL_MODES="1:KILL_FIRES,3:ERROR,4:OPERATOR_VERIFY"
KILL_CONTINUE="0,2"
assert_eq "kill exit 0 → CONTINUE"          "$(_classify_python_exit 0 "$KILL_MODES" "$KILL_CONTINUE")"   "CONTINUE"
assert_eq "kill exit 1 → KILL_FIRES"        "$(_classify_python_exit 1 "$KILL_MODES" "$KILL_CONTINUE")"   "KILL_FIRES"
assert_eq "kill exit 2 → CONTINUE"          "$(_classify_python_exit 2 "$KILL_MODES" "$KILL_CONTINUE")"   "CONTINUE"
assert_eq "kill exit 3 → ERROR"             "$(_classify_python_exit 3 "$KILL_MODES" "$KILL_CONTINUE")"   "ERROR"
assert_eq "kill exit 4 → OPERATOR_VERIFY"   "$(_classify_python_exit 4 "$KILL_MODES" "$KILL_CONTINUE")"   "OPERATOR_VERIFY"
# REGRESSION: python crash exit 5+ must NOT silently route to "no alert"
assert_eq "kill exit 5 → UNEXPECTED"        "$(_classify_python_exit 5 "$KILL_MODES" "$KILL_CONTINUE")"   "UNEXPECTED"
assert_eq "kill exit 137 (SIGKILL) → UNEXPECTED" "$(_classify_python_exit 137 "$KILL_MODES" "$KILL_CONTINUE")" "UNEXPECTED"

echo
echo "F4: _classify_python_exit (stage_promotion_check contract)"
PROMOTE_MODES="0:PROMOTE_READY,1:BLOCKED,3:ERROR,4:CANDIDATE"
PROMOTE_CONTINUE="2"
assert_eq "promote exit 0 → PROMOTE_READY"   "$(_classify_python_exit 0 "$PROMOTE_MODES" "$PROMOTE_CONTINUE")" "PROMOTE_READY"
assert_eq "promote exit 1 → BLOCKED"         "$(_classify_python_exit 1 "$PROMOTE_MODES" "$PROMOTE_CONTINUE")" "BLOCKED"
assert_eq "promote exit 2 → CONTINUE"        "$(_classify_python_exit 2 "$PROMOTE_MODES" "$PROMOTE_CONTINUE")" "CONTINUE"
assert_eq "promote exit 3 → ERROR"           "$(_classify_python_exit 3 "$PROMOTE_MODES" "$PROMOTE_CONTINUE")" "ERROR"
assert_eq "promote exit 4 → CANDIDATE"       "$(_classify_python_exit 4 "$PROMOTE_MODES" "$PROMOTE_CONTINUE")" "CANDIDATE"
# REGRESSION: crash exit must NOT silently route to "no alert"
assert_eq "promote exit 99 → UNEXPECTED"     "$(_classify_python_exit 99 "$PROMOTE_MODES" "$PROMOTE_CONTINUE")" "UNEXPECTED"

echo
echo "Stage 7: _classify_python_exit (lag_summary contract)"
# lag_summary.sh exit codes (commit bb53c1b):
#   0 HEALTHY / 1 DEGRADED / 2 HIGH / 3 SSH_FAILURE / 4 INPUT_ERROR
LAG_MODES="1:DEGRADED,2:HIGH,3:SSH_FAILURE,4:INPUT_ERROR"
LAG_CONTINUE="0"
assert_eq "lag exit 0 → CONTINUE"     "$(_classify_python_exit 0 "$LAG_MODES" "$LAG_CONTINUE")" "CONTINUE"
assert_eq "lag exit 1 → DEGRADED"     "$(_classify_python_exit 1 "$LAG_MODES" "$LAG_CONTINUE")" "DEGRADED"
assert_eq "lag exit 2 → HIGH"         "$(_classify_python_exit 2 "$LAG_MODES" "$LAG_CONTINUE")" "HIGH"
assert_eq "lag exit 3 → SSH_FAILURE"  "$(_classify_python_exit 3 "$LAG_MODES" "$LAG_CONTINUE")" "SSH_FAILURE"
assert_eq "lag exit 4 → INPUT_ERROR"  "$(_classify_python_exit 4 "$LAG_MODES" "$LAG_CONTINUE")" "INPUT_ERROR"
# REGRESSION: crash exit must NOT silently route to "no alert"
assert_eq "lag exit 5 → UNEXPECTED"   "$(_classify_python_exit 5 "$LAG_MODES" "$LAG_CONTINUE")" "UNEXPECTED"

echo
echo "F5: _classify_python_exit (forward_paper_resolution contract)"
RESOLUTION_MODES="1:PROMOTE,2:WATCH,3:OPERATOR_REVIEW,4:KILL,5:INPUT_ERROR"
RESOLUTION_CONTINUE="0"
assert_eq "resolution exit 0 → CONTINUE"        "$(_classify_python_exit 0 "$RESOLUTION_MODES" "$RESOLUTION_CONTINUE")" "CONTINUE"
assert_eq "resolution exit 1 → PROMOTE"         "$(_classify_python_exit 1 "$RESOLUTION_MODES" "$RESOLUTION_CONTINUE")" "PROMOTE"
assert_eq "resolution exit 2 → WATCH"           "$(_classify_python_exit 2 "$RESOLUTION_MODES" "$RESOLUTION_CONTINUE")" "WATCH"
assert_eq "resolution exit 3 → OPERATOR_REVIEW" "$(_classify_python_exit 3 "$RESOLUTION_MODES" "$RESOLUTION_CONTINUE")" "OPERATOR_REVIEW"
assert_eq "resolution exit 4 → KILL"            "$(_classify_python_exit 4 "$RESOLUTION_MODES" "$RESOLUTION_CONTINUE")" "KILL"
assert_eq "resolution exit 5 → INPUT_ERROR"     "$(_classify_python_exit 5 "$RESOLUTION_MODES" "$RESOLUTION_CONTINUE")" "INPUT_ERROR"
# REGRESSION: exit 6+ (e.g. python crash) → UNEXPECTED, not silent
assert_eq "resolution exit 6 → UNEXPECTED"      "$(_classify_python_exit 6 "$RESOLUTION_MODES" "$RESOLUTION_CONTINUE")" "UNEXPECTED"

echo
echo "Stage 8: _classify_python_exit (promotion_rehearsal contract)"
# Stage 8 added 2026-05-11 PM wires promotion_rehearsal.sh into the
# weekly cron so Layer 2 + Layer 3 attestation markers get monitored
# alongside the other 6 decision-grade evaluators. The rehearsal exit
# contract: 0 READY / 1 BLOCKED / 2 WAITING / 3 INPUT_ERR.
REHEARSAL_MODES="0:READY,1:BLOCKED,3:INPUT_ERR"
REHEARSAL_CONTINUE="2"
assert_eq "rehearsal exit 0 → READY"      "$(_classify_python_exit 0 "$REHEARSAL_MODES" "$REHEARSAL_CONTINUE")" "READY"
assert_eq "rehearsal exit 1 → BLOCKED"    "$(_classify_python_exit 1 "$REHEARSAL_MODES" "$REHEARSAL_CONTINUE")" "BLOCKED"
assert_eq "rehearsal exit 2 → CONTINUE"   "$(_classify_python_exit 2 "$REHEARSAL_MODES" "$REHEARSAL_CONTINUE")" "CONTINUE"
assert_eq "rehearsal exit 3 → INPUT_ERR"  "$(_classify_python_exit 3 "$REHEARSAL_MODES" "$REHEARSAL_CONTINUE")" "INPUT_ERR"
# REGRESSION: crash / OOM / future contract additions → UNEXPECTED, never silent.
# Same shape as the F3-F5 fail-open pattern the classifier was designed to close.
assert_eq "rehearsal exit 4 → UNEXPECTED" "$(_classify_python_exit 4 "$REHEARSAL_MODES" "$REHEARSAL_CONTINUE")" "UNEXPECTED"
assert_eq "rehearsal exit 137 (SIGKILL) → UNEXPECTED" "$(_classify_python_exit 137 "$REHEARSAL_MODES" "$REHEARSAL_CONTINUE")" "UNEXPECTED"

# --- summary ---
echo
TOTAL=$((PASS + FAIL))
echo "─── results: $PASS / $TOTAL passed ───"
if [[ $FAIL -gt 0 ]]; then
    echo "FAILED:"
    for label in "${FAIL_LABELS[@]}"; do
        echo "  - $label"
    done
    exit 1
fi
echo "all tests passed"
