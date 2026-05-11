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
