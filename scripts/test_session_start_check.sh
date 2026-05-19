#!/usr/bin/env bash
# test_session_start_check.sh — pins behavior of session_start_check.sh.
#
# Strategy: mock the external commands (gh, ssh) via SESSION_CHECK_GH and
# SESSION_CHECK_SSH env overrides. Each mock script emits canned output
# corresponding to a specific state we want to verify (CI green/red,
# services active/failed, drift fresh/stale, layer3 fresh/stale).
#
# Run:
#   bash scripts/test_session_start_check.sh
set -uo pipefail

# Skip on bash 3.2 — same convention as other test_*.sh files.
if [[ "${BASH_VERSINFO[0]:-0}" -lt 4 ]]; then
    echo "test_session_start_check.sh — skipped (bash ${BASH_VERSION} < 4; CI runs full coverage)"
    exit 0
fi

SCRIPT_DIR="$(cd "$(dirname "${BASH_SOURCE[0]}")" && pwd)"
CHECK="${SCRIPT_DIR}/session_start_check.sh"

if [[ ! -f "$CHECK" ]]; then
    echo "session_start_check.sh not found at $CHECK" >&2
    exit 1
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
        echo "  ✗ $label: expected output to contain '$needle'"
        echo "    actual (first 800): ${haystack:0:800}"
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
        echo "  ✗ $label: did NOT expect output to contain '$needle'"
        FAIL=$((FAIL + 1))
        FAIL_LABELS+=("$label")
    fi
}

# ── Build mock binaries ─────────────────────────────────────────────────────
TMPDIR_ROOT=$(mktemp -d)
trap 'rm -rf "$TMPDIR_ROOT"' EXIT

# Mock gh: reads GH_FIXTURE_FILE env if set, else outputs default green CI.
MOCK_GH="${TMPDIR_ROOT}/mock_gh"
cat > "$MOCK_GH" <<'EOF'
#!/usr/bin/env bash
if [[ -n "${GH_FIXTURE_FILE:-}" ]] && [[ -f "$GH_FIXTURE_FILE" ]]; then
    cat "$GH_FIXTURE_FILE"
else
    echo '[{"conclusion":"success","headSha":"abcd123","databaseId":99999999,"displayTitle":"test"}]'
fi
EOF
chmod +x "$MOCK_GH"

# Mock ssh: reads SSH_FIXTURE_FILE env if set, else outputs default healthy.
MOCK_SSH="${TMPDIR_ROOT}/mock_ssh"
cat > "$MOCK_SSH" <<'EOF'
#!/usr/bin/env bash
# Mock ssh — ignore all args before the remote command, emit fixture content.
if [[ -n "${SSH_FIXTURE_FILE:-}" ]] && [[ -f "$SSH_FIXTURE_FILE" ]]; then
    cat "$SSH_FIXTURE_FILE"
    exit 0
fi
# Default fixture: 16 active services, 0 failed, drift fresh (now-2d), layer3 fresh (now-1h).
NOW=$(date -u +%s)
DRIFT_TS=$((NOW - 86400 * 2))
LAYER3_TS=$((NOW - 3600))
cat <<HEADER
=== services ===
HEADER
for i in $(seq 1 16); do
    echo "  paper-live@sym${i}.service"
done
cat <<TAIL
=== failed ===
=== drift ===
${DRIFT_TS}
=== layer3 ===
${LAYER3_TS}
=== ts ===
${NOW}
TAIL
EOF
chmod +x "$MOCK_SSH"

run_check() {
    # Call with: <gh_fixture_file_or_empty> <ssh_fixture_file_or_empty> [extra args]
    local gh_fix="${1:-}"; shift
    local ssh_fix="${1:-}"; shift
    GH_FIXTURE_FILE="$gh_fix" \
    SSH_FIXTURE_FILE="$ssh_fix" \
    SESSION_CHECK_GH="$MOCK_GH" \
    SESSION_CHECK_SSH="$MOCK_SSH" \
        bash "$CHECK" "$@" 2>&1
}

# ── T1: all checks green ───────────────────────────────────────────────────
echo
echo "── T1: all healthy ──"
set +e
out=$(run_check "" "")
rc=$?
set -e

assert_eq "T1 exit 0" "$rc" "0"
assert_contains "T1 ci success" "$out" "CI green on main"
assert_contains "T1 services" "$out" "16 services active on VPS, 0 failed"
assert_contains "T1 drift fresh" "$out" "drift detector last ran 2d ago"
assert_contains "T1 layer3 fresh" "$out" "Layer 3 cron last ran 0d ago"
assert_contains "T1 CLEAN" "$out" "CLEAN — safe to proceed"

# ── T2: CI failed on main → WARN ───────────────────────────────────────────
echo
echo "── T2: CI failure → WARN ──"
gh_fail="${TMPDIR_ROOT}/gh_fail.json"
echo '[{"conclusion":"failure","headSha":"deadbeef","databaseId":111,"displayTitle":"bad commit"}]' > "$gh_fail"

set +e
out=$(run_check "$gh_fail" "")
rc=$?
set -e

assert_eq "T2 exit 0 (non-strict)" "$rc" "0"
assert_contains "T2 ci failure warn" "$out" "CI failure on main"
assert_contains "T2 commit message" "$out" "bad commit"
assert_contains "T2 review msg" "$out" "review before substantive work"

# ── T3: CI failure + --strict → exit 1 ─────────────────────────────────────
echo
echo "── T3: CI failure + --strict → exit 1 ──"
set +e
out=$(run_check "$gh_fail" "" --strict)
rc=$?
set -e

assert_eq "T3 exit 1 (strict)" "$rc" "1"

# ── T4: failed services → WARN ─────────────────────────────────────────────
echo
echo "── T4: failed services on VPS → WARN ──"
ssh_failed="${TMPDIR_ROOT}/ssh_failed.txt"
NOW=$(date -u +%s)
cat > "$ssh_failed" <<EOF
=== services ===
  paper-live@sym1.service
  paper-live@sym2.service
=== failed ===
  paper-live@brokenA.service
  paper-live@brokenB.service
  paper-live@brokenC.service
=== drift ===
$((NOW - 86400 * 2))
=== layer3 ===
$((NOW - 3600))
=== ts ===
${NOW}
EOF

set +e
out=$(run_check "" "$ssh_failed")
rc=$?
set -e

assert_eq "T4 exit 0" "$rc" "0"
assert_contains "T4 failed services warn" "$out" "3 failed service(s)"

# ── T5: stale drift detector → WARN ────────────────────────────────────────
echo
echo "── T5: drift detector stale (10d) → WARN ──"
ssh_drift_stale="${TMPDIR_ROOT}/ssh_drift_stale.txt"
NOW=$(date -u +%s)
cat > "$ssh_drift_stale" <<EOF
=== services ===
$(for i in $(seq 1 16); do echo "  paper-live@sym${i}.service"; done)
=== failed ===
=== drift ===
$((NOW - 86400 * 10))
=== layer3 ===
$((NOW - 3600))
=== ts ===
${NOW}
EOF

set +e
out=$(run_check "" "$ssh_drift_stale")
rc=$?
set -e

assert_eq "T5 exit 0" "$rc" "0"
assert_contains "T5 drift stale warn" "$out" "drift detector last ran 10d ago"
assert_contains "T5 cron broken hint" "$out" "weekly cron may be broken"

# ── T6: missing drift history → WARN ───────────────────────────────────────
echo
echo "── T6: missing drift history → WARN ──"
ssh_drift_missing="${TMPDIR_ROOT}/ssh_drift_missing.txt"
NOW=$(date -u +%s)
cat > "$ssh_drift_missing" <<EOF
=== services ===
$(for i in $(seq 1 16); do echo "  paper-live@sym${i}.service"; done)
=== failed ===
=== drift ===
missing
=== layer3 ===
$((NOW - 3600))
=== ts ===
${NOW}
EOF

set +e
out=$(run_check "" "$ssh_drift_missing")
rc=$?
set -e

assert_contains "T6 drift missing warn" "$out" "drift_check_history.jsonl missing"

# ── T7: Layer 3 cron not yet installed → INFO not WARN ─────────────────────
echo
echo "── T7: Layer 3 cron missing → INFO not WARN ──"
ssh_layer3_missing="${TMPDIR_ROOT}/ssh_layer3_missing.txt"
NOW=$(date -u +%s)
cat > "$ssh_layer3_missing" <<EOF
=== services ===
$(for i in $(seq 1 16); do echo "  paper-live@sym${i}.service"; done)
=== failed ===
=== drift ===
$((NOW - 86400 * 2))
=== layer3 ===
missing
=== ts ===
${NOW}
EOF

set +e
out=$(run_check "" "$ssh_layer3_missing")
rc=$?
set -e

assert_contains "T7 layer3 info" "$out" "Layer 3 cron history not yet written"
assert_not_contains "T7 not a warn" "$out" "⚠  Layer 3"

# ── T8: --quiet output is one line ─────────────────────────────────────────
echo
echo "── T8: --quiet output ──"
set +e
out=$(run_check "" "" --quiet)
rc=$?
set -e

assert_eq "T8 exit 0" "$rc" "0"
quiet=$(echo "$out" | head -1)
assert_contains "T8 quiet format" "$quiet" "session_start_check: CLEAN"

# ── T9: CI still running (conclusion empty) → WARN ─────────────────────────
echo
echo "── T9: CI in-flight → WARN ──"
gh_running="${TMPDIR_ROOT}/gh_running.json"
echo '[{"conclusion":null,"headSha":"abc123","databaseId":777,"displayTitle":"x"}]' > "$gh_running"

set +e
out=$(run_check "$gh_running" "")
rc=$?
set -e

assert_eq "T9 exit 0" "$rc" "0"
assert_contains "T9 in-flight warn" "$out" "CI conclusion empty"
assert_contains "T9 watch hint" "$out" "gh run watch 777"

# ── Summary ─────────────────────────────────────────────────────────────────
echo
TOTAL=$((PASS + FAIL))
echo "─── results: $PASS / $TOTAL passed ───"
if [[ $FAIL -gt 0 ]]; then
    echo "FAILED:"
    for L in "${FAIL_LABELS[@]}"; do
        echo "  - $L"
    done
    exit 1
fi
echo "all tests passed"
