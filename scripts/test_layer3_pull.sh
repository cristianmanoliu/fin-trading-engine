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

# ─────────────────────────────────────────────────────────────
# Test 1: default happy path — ssh succeeds, verdict exits 0
# ─────────────────────────────────────────────────────────────
echo "Test 1: default happy path"
sb=$(new_sandbox)
cat > "$sb/bin/ssh" <<'EOF'
#!/usr/bin/env bash
# mock ssh — echo args to log, exit 0
echo "ssh-called $*" >> "$MOCK_LOG"
echo "VERDICT_RESULT: PASS"
exit 0
EOF
chmod +x "$sb/bin/ssh"
export MOCK_LOG="$sb/mock.log"
out=$(PATH="$sb/bin:$PATH" \
      LAYER3_PULL_HOST="root@test.example" \
      bash "$HELPER" 2>&1)
rc=$?
assert_eq "1.1 default happy exit code" "$rc" "0"
assert_contains "1.2 default happy verdict line" "$out" "VERDICT_RESULT: PASS"
assert_contains "1.3 default invokes ssh with VPS host" "$(cat "$MOCK_LOG")" "root@test.example"
assert_contains "1.4 default invokes verdict on VPS" "$(cat "$MOCK_LOG")" "scripts/layer3_verdict.sh"
assert_not_contains "1.5 default does NOT emit trailing empty positional" "$(cat "$MOCK_LOG")" "layer3 ''"
cleanup_sandbox "$sb"

# ─────────────────────────────────────────────────────────────
# Test 2: verdict exits 1 → helper exits 1 (subprocess exit-code transparency)
# ─────────────────────────────────────────────────────────────
echo "Test 2: verdict THRESHOLD propagation"
sb=$(new_sandbox)
cat > "$sb/bin/ssh" <<'EOF'
#!/usr/bin/env bash
echo "VERDICT_RESULT: FAIL (THRESHOLD)"
exit 1
EOF
chmod +x "$sb/bin/ssh"
out=$(PATH="$sb/bin:$PATH" bash "$HELPER" 2>&1)
rc=$?
assert_eq "2.1 verdict exit 1 propagated" "$rc" "1"
assert_contains "2.2 verdict THRESHOLD line surfaced" "$out" "THRESHOLD"
cleanup_sandbox "$sb"

# ─────────────────────────────────────────────────────────────
# Test 3: ssh unreachable → helper exits 3 (SSH-failure dual sense)
# ─────────────────────────────────────────────────────────────
echo "Test 3: ssh unreachable"
sb=$(new_sandbox)
cat > "$sb/bin/ssh" <<'EOF'
#!/usr/bin/env bash
echo "ssh: connect to host root@test.example port 22: Connection refused" >&2
exit 255
EOF
chmod +x "$sb/bin/ssh"
out=$(PATH="$sb/bin:$PATH" bash "$HELPER" 2>&1)
rc=$?
assert_eq "3.1 ssh exit 255 maps to helper exit 3" "$rc" "3"
assert_contains "3.2 helper emits SSH-failure context" "$out" "ssh"
cleanup_sandbox "$sb"

# ─────────────────────────────────────────────────────────────
# Test 4: forwarded flags reach verdict invocation verbatim
# ─────────────────────────────────────────────────────────────
echo "Test 4: arg pass-through"
sb=$(new_sandbox)
cat > "$sb/bin/ssh" <<'EOF'
#!/usr/bin/env bash
echo "ssh-args $*" >> "$MOCK_LOG"
exit 0
EOF
chmod +x "$sb/bin/ssh"
export MOCK_LOG="$sb/mock.log"
PATH="$sb/bin:$PATH" bash "$HELPER" --skip-min-days --verbose --threshold-pct 0.7 >/dev/null 2>&1
ssh_record=$(cat "$MOCK_LOG")
assert_contains "4.1 --skip-min-days forwarded" "$ssh_record" "--skip-min-days"
assert_contains "4.2 --verbose forwarded"        "$ssh_record" "--verbose"
assert_contains "4.3 --threshold-pct forwarded"  "$ssh_record" "--threshold-pct 0.7"
assert_not_contains "4.4 --pull NOT forwarded (helper-only flag)" "$ssh_record" "--pull"
cleanup_sandbox "$sb"
unset MOCK_LOG

# ─────────────────────────────────────────────────────────────
# Test 5: --pull on empty layer3/ → exit 3, no rsync invoked
# ─────────────────────────────────────────────────────────────
echo "Test 5: --pull empty layer3 dir"
sb=$(new_sandbox)
cat > "$sb/bin/ssh" <<'EOF'
#!/usr/bin/env bash
# args after host: the remote command. Look for `ls .../layer3/*.jsonl`
# and return empty (nothing matches).
case "$*" in
    *"ls "*"/layer3/"*) exit 1 ;;  # empty match — ls returns nonzero
    *) exit 0 ;;
esac
EOF
chmod +x "$sb/bin/ssh"
cat > "$sb/bin/rsync" <<'EOF'
#!/usr/bin/env bash
echo "rsync-called $*" >> "$MOCK_LOG"
exit 0
EOF
chmod +x "$sb/bin/rsync"
export MOCK_LOG="$sb/mock.log"
: > "$MOCK_LOG"
out=$(PATH="$sb/bin:$PATH" \
      LAYER3_PULL_CACHE_DIR="$sb/cache" \
      bash "$HELPER" --pull 2>&1)
rc=$?
assert_eq "5.1 empty layer3 → exit 3" "$rc" "3"
assert_contains "5.2 message names the failure" "$out" "no Layer 3"
rsync_log=$(cat "$MOCK_LOG" 2>/dev/null || true)
assert_eq "5.3 rsync NOT invoked" "$rsync_log" ""
cleanup_sandbox "$sb"
unset MOCK_LOG

echo
echo "Total: $PASS passed, $FAIL failed"
if [[ $FAIL -gt 0 ]]; then
    echo "Failed tests:"
    printf '  - %s\n' "${FAIL_LABELS[@]}"
    exit 1
fi
exit 0
