#!/usr/bin/env bash
# test_layer3_pull.sh — regression suite for scripts/layer3_pull.sh.
#
# Strategy: PATH-prepended shim dir provides fake `ssh`, `rsync`, `flock`.
# Helper reads LAYER3_PULL_HOST / LAYER3_PULL_VPS_BASE / LAYER3_PULL_CACHE_DIR
# env vars to redirect to controlled test locations. Mocks record their
# invocations to inspect-able files (mock_log) so tests assert call shape.
#
# Same idiom as test_layer3_cron.sh and test_daily_digest.sh.
#
# Run:
#   bash scripts/test_layer3_pull.sh
set -uo pipefail

# Skip on bash 3.2 (macOS /bin/bash) — CI runs Linux bash 5 for full coverage.
if [[ "${BASH_VERSINFO[0]:-0}" -lt 4 ]]; then
    echo "test_layer3_pull.sh — skipped (bash ${BASH_VERSION} < 4; CI runs full coverage)"
    exit 0
fi

SCRIPT_DIR="$(cd "$(dirname "${BASH_SOURCE[0]}")" && pwd)"
HELPER="${SCRIPT_DIR}/layer3_pull.sh"

if [[ ! -f "$HELPER" ]]; then
    echo "test_layer3_pull.sh — skipped (scaffolded; helper not yet present)" >&2
    exit 0
fi

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
        echo "  ✗ $label: expected to contain '$needle'"
        echo "    got: $(echo "$haystack" | head -5)"
        FAIL=$((FAIL + 1))
        FAIL_LABELS+=("$label")
    fi
}

assert_not_contains() {
    local label="$1" haystack="$2" needle="$3"
    if [[ "$haystack" != *"$needle"* ]]; then
        echo "  ✓ $label"
        PASS=$((PASS + 1))
    else
        echo "  ✗ $label: expected to NOT contain '$needle'"
        FAIL=$((FAIL + 1))
        FAIL_LABELS+=("$label")
    fi
}

# Per-test sandbox setup. Returns SANDBOX path via stdout.
new_sandbox() {
    local sb
    sb=$(mktemp -d) || { echo "new_sandbox: mktemp -d failed" >&2; exit 1; }
    mkdir -p "$sb/bin" "$sb/cache" "$sb/vps_root/journal/layer3"
    echo "$sb"
}

cleanup_sandbox() {
    local sb="${1:-}"
    [[ -n "$sb" && -d "$sb" ]] && rm -rf "$sb"
}

echo "test_layer3_pull.sh — running..."
echo

# Tests are appended in subsequent tasks.

echo
echo "Total: $PASS passed, $FAIL failed"
if [[ $FAIL -gt 0 ]]; then
    echo "Failed tests:"
    printf '  - %s\n' "${FAIL_LABELS[@]}"
    exit 1
fi
exit 0
