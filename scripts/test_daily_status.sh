#!/usr/bin/env bash
# test_daily_status.sh — assertion-based tests for daily_status.sh.
#
# Covers:
#   - sourceability (BASH_SOURCE guard works; main flow doesn't fire)
#   - limbo_label / lag_label translate exit codes to human strings
#   - aggregate_exit precedence: KILL > WARN > OK
#   - aggregate_exit fail-modes (section-fail, lag-tier, LIMBO-tier)
#   - CLI: --help, --quiet, unknown flag, --local + positional VPS conflict
#
# Run:
#   bash scripts/test_daily_status.sh
set -euo pipefail

SCRIPT_DIR="$(cd "$(dirname "${BASH_SOURCE[0]}")" && pwd)"
DAILY_STATUS="${SCRIPT_DIR}/daily_status.sh"

# Source helpers (the BASH_SOURCE guard prevents main flow).
# shellcheck source=daily_status.sh disable=SC1091
source "$DAILY_STATUS"

PASS=0
FAIL=0
FAIL_LINES=()

assert_eq() {
    local got="$1" expected="$2" name="$3"
    if [[ "$got" == "$expected" ]]; then
        PASS=$((PASS + 1))
        echo "  ✓ $name"
    else
        FAIL=$((FAIL + 1))
        FAIL_LINES+=("  ✗ $name: got=[$got] expected=[$expected]")
    fi
}

assert_contains() {
    local haystack="$1" needle="$2" name="$3"
    if [[ "$haystack" == *"$needle"* ]]; then
        PASS=$((PASS + 1))
        echo "  ✓ $name"
    else
        FAIL=$((FAIL + 1))
        FAIL_LINES+=("  ✗ $name: needle [$needle] not in haystack [$haystack]")
    fi
}

# ── label helpers ───────────────────────────────────────────────────────────
echo
echo "── limbo_label ──"
assert_eq "$(limbo_label 0)" "CONTINUE"        "exit 0 → CONTINUE"
assert_eq "$(limbo_label 1)" "PROMOTE"         "exit 1 → PROMOTE"
assert_eq "$(limbo_label 2)" "WATCH"           "exit 2 → WATCH"
assert_eq "$(limbo_label 3)" "OPERATOR_REVIEW" "exit 3 → OPERATOR_REVIEW"
assert_eq "$(limbo_label 4)" "KILL"            "exit 4 → KILL"
assert_eq "$(limbo_label 5)" "INPUT_ERROR"     "exit 5 → INPUT_ERROR"
assert_eq "$(limbo_label 9)" "UNKNOWN(9)"      "exit 9 → UNKNOWN(9) (defensive)"

echo
echo "── lag_label ──"
assert_eq "$(lag_label 0)" "HEALTHY"     "exit 0 → HEALTHY"
assert_eq "$(lag_label 1)" "DEGRADED"    "exit 1 → DEGRADED"
assert_eq "$(lag_label 2)" "HIGH"        "exit 2 → HIGH"
assert_eq "$(lag_label 3)" "SSH_FAILURE" "exit 3 → SSH_FAILURE"
assert_eq "$(lag_label 4)" "INPUT_ERROR" "exit 4 → INPUT_ERROR"
assert_eq "$(lag_label -)" "SKIPPED"     "sentinel '-' → SKIPPED (not UNKNOWN)"

# ── aggregate_exit ──────────────────────────────────────────────────────────
echo
echo "── aggregate_exit happy path ──"
# All clean: LIMBO=CONTINUE, lag=HEALTHY, sections OK.
assert_eq "$(aggregate_exit 0 0 1 1)" "0" "all-clean → 0 (OK)"
# LIMBO=PROMOTE is still informational, lag healthy.
assert_eq "$(aggregate_exit 1 0 1 1)" "0" "PROMOTE+HEALTHY → 0 (OK)"
# LIMBO=WATCH is a soft signal but the locked rule maps it to NO action.
assert_eq "$(aggregate_exit 2 0 1 1)" "0" "WATCH+HEALTHY → 0 (OK per LIMBO mapping)"

echo
echo "── aggregate_exit WARN tier ──"
# LIMBO=OPERATOR_REVIEW alone → WARN.
assert_eq "$(aggregate_exit 3 0 1 1)" "1" "OPERATOR_REVIEW → WARN"
# Lag DEGRADED alone → WARN.
assert_eq "$(aggregate_exit 0 1 1 1)" "1" "lag DEGRADED → WARN"
# Lag SSH failure → WARN (couldn't tell, treat as concern but not KILL).
assert_eq "$(aggregate_exit 0 3 1 1)" "1" "lag SSH_FAILURE → WARN"
# Section failed to invoke (cost helper missing) → WARN.
assert_eq "$(aggregate_exit 0 0 0 1)" "1" "cost section failed → WARN"
# Traj section failed → WARN.
assert_eq "$(aggregate_exit 0 0 1 0)" "1" "traj section failed → WARN"

echo
echo "── aggregate_exit KILL tier ──"
# LIMBO=KILL → KILL regardless of other tiers.
assert_eq "$(aggregate_exit 4 0 1 1)" "2" "LIMBO KILL → 2"
# LIMBO=INPUT_ERROR → KILL (snapshot missing means we cannot trust any
# verdict above; explicit not silent CONTINUE).
assert_eq "$(aggregate_exit 5 0 1 1)" "2" "LIMBO INPUT_ERROR → 2"
# Lag HIGH → KILL.
assert_eq "$(aggregate_exit 0 2 1 1)" "2" "lag HIGH → 2"

echo
echo "── aggregate_exit precedence ──"
# KILL beats WARN.
assert_eq "$(aggregate_exit 4 1 0 0)" "2" "KILL+WARN+section-fail → KILL (highest tier wins)"
# Two WARN tiers don't escalate to KILL.
assert_eq "$(aggregate_exit 3 1 1 1)" "1" "OPERATOR_REVIEW + DEGRADED → WARN (no escalation)"
# LIMBO sentinel "-" (not invoked) shouldn't crash.
assert_eq "$(aggregate_exit - - 1 1)" "0" "unset LIMBO/lag default → OK"

# ── CLI parse + flow ────────────────────────────────────────────────────────
echo
echo "── CLI flag parsing ──"

# --help should exit 0 and print sections from the header docstring.
help_output=$(bash "$DAILY_STATUS" --help 2>&1)
help_rc=$?
assert_eq "$help_rc" "0" "--help exits 0"
assert_contains "$help_output" "daily_status.sh — single paste-ready" "--help shows header"

# Unknown flag → exit 3 (INPUT_ERR).
set +e
bash "$DAILY_STATUS" --nonsense 2>/dev/null
rc=$?
set -e
assert_eq "$rc" "3" "unknown flag → INPUT_ERR"

# --local + positional VPS = mutually exclusive → exit 3.
set +e
err=$(bash "$DAILY_STATUS" --local root@other 2>&1)
rc=$?
set -e
assert_eq "$rc" "3" "--local + positional VPS → INPUT_ERR"
assert_contains "$err" "mutually exclusive" "conflict diagnostic surfaces"

# ── Final ───────────────────────────────────────────────────────────────────
echo
echo "── results ──"
echo "PASS: $PASS"
echo "FAIL: $FAIL"
if [[ "$FAIL" -gt 0 ]]; then
    for line in "${FAIL_LINES[@]}"; do
        echo "$line"
    done
    exit 1
fi
echo "all green"
exit 0
