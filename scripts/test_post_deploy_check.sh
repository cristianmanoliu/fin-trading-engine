#!/usr/bin/env bash
# test_post_deploy_check.sh — regression suite for the pure helpers
# extracted from scripts/post_deploy_check.sh as part of the PD-1
# through PD-7 audit pass.
#
# Pinned helpers:
#   - compare_code_checksums (PD-2 empty-empty equality)
#   - classify_ssh_exit (PD-1/3/4/5/7 ssh-vs-remote disambiguation)
#
# ssh_remote is impure (does I/O + sets globals) so it's documented
# integration-test-only; its CONSEQUENCES are tested through the
# call-site refactors that use classify_ssh_exit.

set -uo pipefail

SCRIPT_DIR="$(cd "$(dirname "$0")" && pwd)"
WRAPPER="${SCRIPT_DIR}/post_deploy_check.sh"

if [[ ! -f "$WRAPPER" ]]; then
    echo "wrapper missing at $WRAPPER" >&2
    exit 1
fi

# shellcheck source=post_deploy_check.sh
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
# PD-2: compare_code_checksums — empty-empty equality must NOT silently
# report MATCH. Operator believed the binary was current when neither
# side could be checked.
# ─────────────────────────────────────────────────────────────────────
echo "PD-2: compare_code_checksums"
assert_eq "matching md5"            "$(compare_code_checksums "abc123" "abc123")" "MATCH"
assert_eq "differing md5"           "$(compare_code_checksums "abc123" "def456")" "MISMATCH"
# REGRESSION: empty-empty must be UNAVAILABLE, not MATCH
assert_eq "empty-empty → UNAVAILABLE" "$(compare_code_checksums "" "")"           "UNAVAILABLE"
assert_eq "empty-set → UNAVAILABLE"   "$(compare_code_checksums "" "abc123")"     "UNAVAILABLE"
assert_eq "set-empty → UNAVAILABLE"   "$(compare_code_checksums "abc123" "")"     "UNAVAILABLE"

# ─────────────────────────────────────────────────────────────────────
# PD-1/3/4/5/7: classify_ssh_exit — separates ssh-transport failure
# from remote-command outcomes. Without this distinction, every section's
# `$(ssh ... || echo X)` collapses ssh-broken into "the thing being
# measured is broken."
# ─────────────────────────────────────────────────────────────────────
echo
echo "PD-1/3/4/5/7: classify_ssh_exit"
assert_eq "exit 0 → OK"               "$(classify_ssh_exit 0)"   "OK"
# REGRESSION: ssh-level exits must NOT collapse into REMOTE_FAILURE
assert_eq "exit 255 (transport) → SSH_FAILURE" "$(classify_ssh_exit 255)" "SSH_FAILURE"
assert_eq "exit 127 (cmd not found) → SSH_FAILURE" "$(classify_ssh_exit 127)" "SSH_FAILURE"
assert_eq "exit 126 (not executable) → SSH_FAILURE" "$(classify_ssh_exit 126)" "SSH_FAILURE"
assert_eq "exit 130 (SIGINT) → SSH_FAILURE" "$(classify_ssh_exit 130)" "SSH_FAILURE"
assert_eq "exit 137 (SIGKILL) → SSH_FAILURE" "$(classify_ssh_exit 137)" "SSH_FAILURE"
# Remote commands can exit with various non-{ssh-level} codes
assert_eq "exit 1 → REMOTE_FAILURE"   "$(classify_ssh_exit 1)"   "REMOTE_FAILURE"
assert_eq "exit 2 → REMOTE_FAILURE"   "$(classify_ssh_exit 2)"   "REMOTE_FAILURE"
assert_eq "exit 99 → REMOTE_FAILURE"  "$(classify_ssh_exit 99)"  "REMOTE_FAILURE"

# ─────────────────────────────────────────────────────────────────────
# classify_lag_ms — tier the per-engine source-to-receipt lag p99 into
# OK / TYPICAL / DEGRADED / HIGH / NO_DATA. Wrapper consumes this in §4.
# Thresholds anchored on REST polling interval (10s = 10000ms).
# ─────────────────────────────────────────────────────────────────────
echo
echo "lag-tier: classify_lag_ms"
# NO_DATA shapes — non-numeric / sentinel / negative
assert_eq "empty → NO_DATA"           "$(classify_lag_ms '')"     "NO_DATA"
assert_eq "n/a → NO_DATA"             "$(classify_lag_ms 'n/a')"  "NO_DATA"
assert_eq "null → NO_DATA"            "$(classify_lag_ms 'null')" "NO_DATA"
assert_eq "negative → NO_DATA"        "$(classify_lag_ms -1)"     "NO_DATA"
# OK tier — WebSocket-dominant
assert_eq "0 → OK"                    "$(classify_lag_ms 0)"      "OK"
assert_eq "500 → OK"                  "$(classify_lag_ms 500)"    "OK"
assert_eq "1000 → OK (boundary)"      "$(classify_lag_ms 1000)"   "OK"
# TYPICAL tier — REST-dominant or mixed
assert_eq "1001 → TYPICAL"            "$(classify_lag_ms 1001)"   "TYPICAL"
assert_eq "5000 → TYPICAL"            "$(classify_lag_ms 5000)"   "TYPICAL"
assert_eq "15000 → TYPICAL (boundary)" "$(classify_lag_ms 15000)" "TYPICAL"
# DEGRADED tier — investigate
assert_eq "15001 → DEGRADED"          "$(classify_lag_ms 15001)"  "DEGRADED"
assert_eq "20000 → DEGRADED"          "$(classify_lag_ms 20000)"  "DEGRADED"
assert_eq "30000 → DEGRADED (boundary)" "$(classify_lag_ms 30000)" "DEGRADED"
# HIGH tier — severe
assert_eq "30001 → HIGH"              "$(classify_lag_ms 30001)"  "HIGH"
assert_eq "60000 → HIGH"              "$(classify_lag_ms 60000)"  "HIGH"

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
