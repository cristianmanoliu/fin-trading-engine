#!/usr/bin/env bash
# test_notify.sh — regression suite for scripts/lib/notify.sh.
#
# Pins the tier-selection contract: CRITICAL=3 retries, WARN=2, INFO=1,
# and crucially that unknown/typo'd severity does NOT silently downgrade
# to INFO (F6 of the script-layer audit-lens pass).

set -uo pipefail

SCRIPT_DIR="$(cd "$(dirname "$0")" && pwd)"
LIB="${SCRIPT_DIR}/lib/notify.sh"

if [[ ! -f "$LIB" ]]; then
    echo "lib missing at $LIB" >&2
    exit 1
fi

# shellcheck source=lib/notify.sh
source "$LIB"

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

assert_contains() {
    local label="$1" haystack="$2" needle="$3"
    if [[ "$haystack" == *"$needle"* ]]; then
        echo "  ✓ $label"
        PASS=$((PASS + 1))
    else
        echo "  ✗ $label: expected '$haystack' to contain '$needle'"
        FAIL=$((FAIL + 1))
        FAIL_LABELS+=("$label")
    fi
}

# ─────────────────────────────────────────────────────────────────────
# F6: _notify_select_tier — known severities map to documented retries.
# ─────────────────────────────────────────────────────────────────────
echo "F6: _notify_select_tier (known severities)"
assert_eq "CRITICAL retries" "$(_notify_select_tier CRITICAL | awk -F'|' '{print $2}')" "3"
assert_eq "WARN retries"     "$(_notify_select_tier WARN     | awk -F'|' '{print $2}')" "2"
assert_eq "INFO retries"     "$(_notify_select_tier INFO     | awk -F'|' '{print $2}')" "1"
assert_contains "CRITICAL prefix" "$(_notify_select_tier CRITICAL)" "[CRITICAL]"
assert_contains "WARN prefix"     "$(_notify_select_tier WARN)"     "[WARN]"
assert_contains "INFO prefix"     "$(_notify_select_tier INFO)"     "[INFO]"

# ─────────────────────────────────────────────────────────────────────
# REGRESSION: unknown / typo'd severities must NOT silently downgrade
# to INFO. The fix is "fail loud" — CRITICAL prefix + 3 retries +
# explicit [UNKNOWN SEVERITY: x] tag so the operator sees their typo.
# ─────────────────────────────────────────────────────────────────────
echo
echo "F6 (regression): unknown severities → CRITICAL with UNKNOWN tag"
# Typo
assert_eq "CRITCAL retries (typo)"    "$(_notify_select_tier CRITCAL | awk -F'|' '{print $2}')" "3"
assert_contains "CRITCAL has UNKNOWN tag" "$(_notify_select_tier CRITCAL)" "[UNKNOWN SEVERITY: CRITCAL]"
assert_contains "CRITCAL still CRITICAL prefix" "$(_notify_select_tier CRITCAL)" "[CRITICAL]"
# Lowercase
assert_eq "lowercase critical retries"    "$(_notify_select_tier critical | awk -F'|' '{print $2}')" "3"
assert_contains "lowercase critical UNKNOWN tag" "$(_notify_select_tier critical)" "[UNKNOWN SEVERITY: critical]"
# Empty
assert_eq "empty severity retries"        "$(_notify_select_tier '' | awk -F'|' '{print $2}')" "3"
assert_contains "empty severity UNKNOWN tag" "$(_notify_select_tier '')" "[UNKNOWN SEVERITY:"
# Random garbage
assert_eq "garbage severity retries"      "$(_notify_select_tier WANR | awk -F'|' '{print $2}')" "3"
assert_contains "garbage severity UNKNOWN tag" "$(_notify_select_tier WANR)" "[UNKNOWN SEVERITY: WANR]"

# ─────────────────────────────────────────────────────────────────────
# notify_telegram with no credentials → silent no-op (returns 0 without
# attempting network). Preserves existing documented behavior.
# ─────────────────────────────────────────────────────────────────────
echo
echo "notify_telegram with unset creds → no-op, exit 0"
set +e
TELEGRAM_BOT_TOKEN='' TELEGRAM_CHAT_ID='' notify_telegram CRITICAL "test" "body"
out_exit=$?
set -e
assert_eq "unset-creds exit"     "$out_exit"  "0"

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
