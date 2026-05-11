#!/usr/bin/env bash
# test_lag_summary.sh — regression suite for the pure helpers in
# scripts/lag_summary.sh (classify_lag_ms + fleet_verdict).

set -uo pipefail

SCRIPT_DIR="$(cd "$(dirname "$0")" && pwd)"
WRAPPER="${SCRIPT_DIR}/lag_summary.sh"

if [[ ! -f "$WRAPPER" ]]; then
    echo "wrapper missing at $WRAPPER" >&2
    exit 1
fi

# shellcheck source=lag_summary.sh
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
# classify_lag_ms — same contract as post_deploy_check.sh's helper.
# Duplicated test coverage here because the lag_summary script's
# verdict depends on the classifier's tier boundaries.
# ─────────────────────────────────────────────────────────────────────
echo "classify_lag_ms (mirror of post_deploy_check tier coverage)"
assert_eq "empty → NO_DATA"           "$(classify_lag_ms '')"     "NO_DATA"
assert_eq "n/a → NO_DATA"             "$(classify_lag_ms 'n/a')"  "NO_DATA"
assert_eq "1000 → OK"                 "$(classify_lag_ms 1000)"   "OK"
assert_eq "1001 → TYPICAL"            "$(classify_lag_ms 1001)"   "TYPICAL"
assert_eq "15001 → DEGRADED"          "$(classify_lag_ms 15001)"  "DEGRADED"
assert_eq "30001 → HIGH"              "$(classify_lag_ms 30001)"  "HIGH"

# ─────────────────────────────────────────────────────────────────────
# fleet_verdict — given (high_count, degraded_count) → "VERDICT|exit".
# Pure precedence: HIGH > DEGRADED > HEALTHY. NO_DATA does NOT
# downgrade (it's non-information, not bad-information).
# ─────────────────────────────────────────────────────────────────────
echo
echo "fleet_verdict (precedence: HIGH > DEGRADED > HEALTHY)"
assert_eq "0,0 → HEALTHY|0"      "$(fleet_verdict 0 0)"  "HEALTHY|0"
assert_eq "0,1 → DEGRADED|1"     "$(fleet_verdict 0 1)"  "DEGRADED|1"
assert_eq "1,0 → HIGH|2"         "$(fleet_verdict 1 0)"  "HIGH|2"
assert_eq "1,1 → HIGH|2 (precedence)" "$(fleet_verdict 1 1)"  "HIGH|2"
assert_eq "0,5 → DEGRADED|1"     "$(fleet_verdict 0 5)"  "DEGRADED|1"
assert_eq "3,2 → HIGH|2"         "$(fleet_verdict 3 2)"  "HIGH|2"

# REGRESSION: counts as strings (bash arithmetic should coerce, but
# verify the helper doesn't trip on whitespace/leading-zero shapes).
echo
echo "fleet_verdict (resilience to numeric input shapes)"
assert_eq "00 0 → HEALTHY|0"     "$(fleet_verdict 00 0)" "HEALTHY|0"
assert_eq "0 00 → HEALTHY|0"     "$(fleet_verdict 0 00)" "HEALTHY|0"

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
