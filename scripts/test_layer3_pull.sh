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

# ─────────────────────────────────────────────────────────────
# Test 6: mid-write detected → retry once → still failing → exit 3
# ─────────────────────────────────────────────────────────────
echo "Test 6: mid-write guard"
sb=$(new_sandbox)
# Mock ssh: first call (ls) returns a symbol; subsequent (mid-write checks)
# emit MID indicator and exit 1. SANDBOX env carries the sandbox root so the
# mock can read/write its own count file.
cat > "$sb/bin/ssh" <<'EOF'
#!/usr/bin/env bash
COUNT_FILE="$SANDBOX/ssh_count"
count=$(cat "$COUNT_FILE" 2>/dev/null || echo 0)
count=$((count + 1))
echo "$count" > "$COUNT_FILE"
case "$*" in
    *"ls "*"/layer3/"*)
        echo "/var/log/paper-live/journal/layer3/KAVAUSDT-2026-05.jsonl"
        ;;
    *"mid-write-check"*)
        # Both retries fail — emit MID:<file> on stdout
        echo "MID:/var/log/paper-live/journal/layer3/KAVAUSDT-2026-05.jsonl"
        exit 1
        ;;
    *)
        exit 0
        ;;
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
export SANDBOX="$sb"
# LAYER3_PULL_RETRY_SLEEP overrides the 2s sleep to keep tests fast.
out=$(PATH="$sb/bin:$PATH" \
      LAYER3_PULL_CACHE_DIR="$sb/cache" \
      LAYER3_PULL_RETRY_SLEEP=0 \
      bash "$HELPER" --pull 2>&1)
rc=$?
assert_eq "6.1 persistent mid-write → exit 3" "$rc" "3"
assert_contains "6.2 message mentions mid-write" "$out" "mid-write"
rsync_log=$(cat "$MOCK_LOG" 2>/dev/null || true)
assert_eq "6.3 rsync NOT invoked when mid-write fails" "$rsync_log" ""
ssh_count=$(cat "$sb/ssh_count" 2>/dev/null || echo 0)
# Expected: 1 ls + 2 mid-write checks (initial + 1 retry) = 3
assert_eq "6.4 ssh called 3 times (ls + 2 mid-write checks)" "$ssh_count" "3"
cleanup_sandbox "$sb"
unset MOCK_LOG SANDBOX

# ─────────────────────────────────────────────────────────────
# Test 6b: ssh-255 during mid-write check → distinct error message
# (regression-pins ssh-failure-vs-data-failure dual sense)
# ─────────────────────────────────────────────────────────────
echo "Test 6b: ssh-255 distinguishability during mid-write check"
sb=$(new_sandbox)
cat > "$sb/bin/ssh" <<'EOF'
#!/usr/bin/env bash
COUNT_FILE="$SANDBOX/ssh_count"
count=$(cat "$COUNT_FILE" 2>/dev/null || echo 0)
count=$((count + 1))
echo "$count" > "$COUNT_FILE"
case "$*" in
    *"ls "*"/layer3/"*)
        echo "/var/log/paper-live/journal/layer3/KAVAUSDT-2026-05.jsonl"
        ;;
    *"mid-write-check"*)
        # Simulate ssh transport failure (e.g., connection drop)
        echo "ssh: connect to host root@test.example port 22: Connection refused" >&2
        exit 255
        ;;
    *)
        exit 0
        ;;
esac
EOF
chmod +x "$sb/bin/ssh"
export MOCK_LOG="$sb/mock.log"
export SANDBOX="$sb"
out=$(PATH="$sb/bin:$PATH" \
      LAYER3_PULL_CACHE_DIR="$sb/cache" \
      LAYER3_PULL_RETRY_SLEEP=0 \
      bash "$HELPER" --pull 2>&1)
rc=$?
assert_eq "6b.1 ssh-255 during mid-write → exit 3" "$rc" "3"
assert_contains "6b.2 ssh-failure message names ssh and 255" "$out" "ssh"
assert_contains "6b.3 ssh-failure message includes exit 255" "$out" "255"
assert_not_contains "6b.4 ssh-failure NOT confused with mid-write text" "$out" "mid-write detected"
cleanup_sandbox "$sb"
unset MOCK_LOG SANDBOX

# ─────────────────────────────────────────────────────────────
# Test 7: one of two rsyncs fails → exit 3, no verdict invoked
# ─────────────────────────────────────────────────────────────
echo "Test 7: partial rsync failure"
sb=$(new_sandbox)
cat > "$sb/bin/ssh" <<'EOF'
#!/usr/bin/env bash
case "$*" in
    *"ls "*"/layer3/"*)
        echo "/var/log/paper-live/journal/layer3/KAVAUSDT-2026-05.jsonl"
        ;;
    *"mid-write-check"*) exit 0 ;;
    *) exit 0 ;;
esac
EOF
chmod +x "$sb/bin/ssh"
# Mock rsync: testnet path (contains "/layer3/") succeeds; stub path fails.
cat > "$sb/bin/rsync" <<'EOF'
#!/usr/bin/env bash
echo "rsync-called $*" >> "$MOCK_LOG"
case "$*" in
    *"/layer3/"*) exit 0 ;;
    *) exit 23 ;;  # rsync partial transfer
esac
EOF
chmod +x "$sb/bin/rsync"
# Mock verdict so test 7 detects whether it's invoked.
cat > "$sb/bin/verdict_mock.sh" <<'EOF'
#!/usr/bin/env bash
echo "verdict-INVOKED $*" >> "$MOCK_LOG"
exit 0
EOF
chmod +x "$sb/bin/verdict_mock.sh"
export MOCK_LOG="$sb/mock.log"
: > "$MOCK_LOG"
out=$(PATH="$sb/bin:$PATH" \
      LAYER3_PULL_CACHE_DIR="$sb/cache" \
      LAYER3_PULL_VERDICT_BIN="$sb/bin/verdict_mock.sh" \
      LAYER3_PULL_RETRY_SLEEP=0 \
      bash "$HELPER" --pull 2>&1)
rc=$?
assert_eq "7.1 stub rsync fail → exit 3" "$rc" "3"
log=$(cat "$MOCK_LOG")
assert_contains "7.2 rsync was attempted at least once" "$log" "rsync-called"
assert_not_contains "7.3 verdict NOT invoked on partial pull" "$log" "verdict-INVOKED"
cleanup_sandbox "$sb"
unset MOCK_LOG

# ─────────────────────────────────────────────────────────────
# Test 8: stale cache wiped clean before pull
# ─────────────────────────────────────────────────────────────
echo "Test 8: stale-cache wipe"
sb=$(new_sandbox)
# Plant a stale file in the cache that should be wiped.
mkdir -p "$sb/cache/stub" "$sb/cache/testnet"
echo "stale" > "$sb/cache/stub/STALEUSDT-2026-04.jsonl"
echo "stale" > "$sb/cache/testnet/STALEUSDT-2026-04.jsonl"
cat > "$sb/bin/ssh" <<'EOF'
#!/usr/bin/env bash
case "$*" in
    *"ls "*"/layer3/"*)
        echo "/var/log/paper-live/journal/layer3/KAVAUSDT-2026-05.jsonl"
        ;;
    *"mid-write-check"*) exit 0 ;;
    *) exit 0 ;;
esac
EOF
chmod +x "$sb/bin/ssh"
cat > "$sb/bin/rsync" <<'EOF'
#!/usr/bin/env bash
exit 0
EOF
chmod +x "$sb/bin/rsync"
cat > "$sb/bin/verdict_mock.sh" <<'EOF'
#!/usr/bin/env bash
exit 0
EOF
chmod +x "$sb/bin/verdict_mock.sh"
PATH="$sb/bin:$PATH" \
    LAYER3_PULL_CACHE_DIR="$sb/cache" \
    LAYER3_PULL_VERDICT_BIN="$sb/bin/verdict_mock.sh" \
    LAYER3_PULL_RETRY_SLEEP=0 \
    bash "$HELPER" --pull >/dev/null 2>&1 || true
assert_eq "8.1 stale stub file removed" "$(ls "$sb/cache/stub" 2>/dev/null)" ""
assert_eq "8.2 stale testnet file removed" "$(ls "$sb/cache/testnet" 2>/dev/null)" ""
cleanup_sandbox "$sb"

# ─────────────────────────────────────────────────────────────
# Test 10: concurrent invocation blocked by flock
# ─────────────────────────────────────────────────────────────
echo "Test 10: flock concurrency guard"
sb=$(new_sandbox)
mkdir -p "$sb/cache"
# Acquire the lock from outside, then run the helper — it must fail to lock.
exec 7>"$sb/cache/.lock"
flock -n 7
cat > "$sb/bin/ssh" <<'EOF'
#!/usr/bin/env bash
echo "ssh-INVOKED" >> "$MOCK_LOG"
exit 0
EOF
chmod +x "$sb/bin/ssh"
export MOCK_LOG="$sb/mock.log"
: > "$MOCK_LOG"
out=$(PATH="$sb/bin:$PATH" \
      LAYER3_PULL_CACHE_DIR="$sb/cache" \
      bash "$HELPER" --pull 2>&1)
rc=$?
exec 7>&-  # release the lock so cleanup works
assert_eq "10.1 concurrent --pull blocked → exit 3" "$rc" "3"
assert_contains "10.2 message names the lock collision" "$out" "in progress"
ssh_log=$(cat "$MOCK_LOG" 2>/dev/null || true)
assert_eq "10.3 helper exited before any ssh call" "$ssh_log" ""
cleanup_sandbox "$sb"
unset MOCK_LOG

# ─────────────────────────────────────────────────────────────
# Test 9: multi-month files → both rsync'd, symbol set deduped
# ─────────────────────────────────────────────────────────────
echo "Test 9: month-boundary handling"
sb=$(new_sandbox)
cat > "$sb/bin/ssh" <<'EOF'
#!/usr/bin/env bash
case "$*" in
    *"ls "*"/layer3/"*)
        echo "/var/log/paper-live/journal/layer3/KAVAUSDT-2026-05.jsonl"
        echo "/var/log/paper-live/journal/layer3/KAVAUSDT-2026-06.jsonl"
        echo "/var/log/paper-live/journal/layer3/ENSUSDT-2026-06.jsonl"
        ;;
    *"mid-write-check"*) exit 0 ;;
    *) exit 0 ;;
esac
EOF
chmod +x "$sb/bin/ssh"
cat > "$sb/bin/rsync" <<'EOF'
#!/usr/bin/env bash
echo "rsync-pattern $2" >> "$MOCK_LOG"
exit 0
EOF
chmod +x "$sb/bin/rsync"
cat > "$sb/bin/verdict_mock.sh" <<'EOF'
#!/usr/bin/env bash
echo "verdict-INVOKED $*" >> "$MOCK_LOG"
exit 0
EOF
chmod +x "$sb/bin/verdict_mock.sh"
export MOCK_LOG="$sb/mock.log"
: > "$MOCK_LOG"
PATH="$sb/bin:$PATH" \
    LAYER3_PULL_CACHE_DIR="$sb/cache" \
    LAYER3_PULL_VERDICT_BIN="$sb/bin/verdict_mock.sh" \
    LAYER3_PULL_RETRY_SLEEP=0 \
    bash "$HELPER" --pull >/dev/null 2>&1 || true
log=$(cat "$MOCK_LOG")
# rsync should be called with KAVAUSDT-*.jsonl (one pattern, covering both months)
# and ENSUSDT-*.jsonl, plus the testnet bulk glob. Total stub rsyncs = 2 unique symbols.
kava_count=$(grep -c "KAVAUSDT-" <<< "$log")
ens_count=$(grep -c "ENSUSDT-"  <<< "$log")
assert_contains "9.1 KAVAUSDT pulled" "$log" "KAVAUSDT-"
assert_contains "9.2 ENSUSDT pulled" "$log" "ENSUSDT-"
assert_eq "9.3 KAVAUSDT pulled once (symbol deduped)" "$kava_count" "1"
assert_eq "9.4 ENSUSDT pulled once" "$ens_count" "1"
assert_contains "9.5 verdict invoked with cache dirs" "$log" "verdict-INVOKED"
assert_contains "9.6 verdict received --stub-dir" "$log" "--stub-dir"
assert_contains "9.7 verdict received --testnet-dir" "$log" "--testnet-dir"
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
