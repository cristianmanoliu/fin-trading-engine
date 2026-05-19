#!/usr/bin/env bash
# test_layer3_cron.sh — pins the Layer 3 cron wrapper's state-machine +
# Telegram tier semantics.
#
# Strategy: use LAYER3_CRON_VERDICT env override to substitute a tiny mock
# script that exits with a canned code and writes a canned verdict line.
# That decouples the wrapper from real journal data + real journal_diff,
# so the suite runs in milliseconds and tests every state transition.
#
# Run:
#   bash scripts/test_layer3_cron.sh
set -uo pipefail

# Skip on bash 3.2 (macOS /bin/bash) — same pattern as test_daily_digest.sh.
# layer3_cron.sh uses bash features (jq pipes, here-docs, advanced array
# semantics) that work on both 3.2 and 4+ today, but the broader test
# infrastructure assumes bash 4+ behavior. CI runs Linux bash 5.
if [[ "${BASH_VERSINFO[0]:-0}" -lt 4 ]]; then
    echo "test_layer3_cron.sh — skipped (bash ${BASH_VERSION} < 4; CI runs full coverage)"
    exit 0
fi

SCRIPT_DIR="$(cd "$(dirname "${BASH_SOURCE[0]}")" && pwd)"
CRON="${SCRIPT_DIR}/layer3_cron.sh"

if [[ ! -f "$CRON" ]]; then
    echo "layer3_cron.sh not found at $CRON" >&2
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
        echo "    actual (first 800 chars): ${haystack:0:800}"
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

# ── fixture: mock verdict script that emits canned exit + output ────────────
TMPDIR_ROOT=$(mktemp -d)
trap 'rm -rf "$TMPDIR_ROOT"' EXIT

MOCK="${TMPDIR_ROOT}/mock_verdict.sh"
cat > "$MOCK" <<'EOF'
#!/usr/bin/env bash
# Mock layer3_verdict. Echo MOCK_VERDICT_LINE if set, exit with MOCK_EXIT.
echo ">>> VERDICT: ${MOCK_LABEL:-MOCK} — exit=${MOCK_EXIT:-0}"
exit "${MOCK_EXIT:-0}"
EOF
chmod +x "$MOCK"

HISTORY="${TMPDIR_ROOT}/history.jsonl"
RUNS_DIR="${TMPDIR_ROOT}/runs"
STUB_DIR="${TMPDIR_ROOT}/stub_unused"
TESTNET_DIR="${TMPDIR_ROOT}/testnet_unused"
mkdir -p "$STUB_DIR" "$TESTNET_DIR"

run_cron() {
    # Args: MOCK_EXIT MOCK_LABEL [extra cron flags...]
    local exit_code="$1"; shift
    local label="$1"; shift
    MOCK_EXIT="$exit_code" \
    MOCK_LABEL="$label" \
    LAYER3_CRON_VERDICT="$MOCK" \
    LAYER3_CRON_HISTORY="$HISTORY" \
    LAYER3_CRON_RUNS_DIR="$RUNS_DIR" \
    LAYER3_CRON_STUB_DIR="$STUB_DIR" \
    LAYER3_CRON_TESTNET_DIR="$TESTNET_DIR" \
        bash "$CRON" --dry-run "$@" 2>&1
}

# ── T1: first run (INITIAL → INSUFFICIENT) — silent (no alert) ──────────────
echo
echo "── T1: INITIAL → INSUFFICIENT_DURATION is silent ──"
set +e
out=$(run_cron 4 "INSUFFICIENT")
rc=$?
set -e

assert_eq "T1 exit code" "$rc" "4"
assert_contains "T1 state line" "$out" "state:     INSUFFICIENT_DURATION (exit 4)"
assert_contains "T1 prev INITIAL" "$out" "prev:      INITIAL"
assert_contains "T1 alert is none" "$out" "alert:     none"
assert_not_contains "T1 no DRY_RUN payload" "$out" "DRY_RUN: would send"

# History row should exist
assert_eq "T1 history exists" "$([[ -s $HISTORY ]] && echo yes || echo no)" "yes"
row=$(tail -n 1 "$HISTORY")
assert_contains "T1 history state" "$row" '"state":"INSUFFICIENT_DURATION"'
assert_contains "T1 history exit" "$row" '"exit_code":4'
assert_contains "T1 history prev" "$row" '"prev_state":"INITIAL"'

# ── T2: second INSUFFICIENT run is still silent (no spam) ───────────────────
echo
echo "── T2: INSUFFICIENT → INSUFFICIENT is silent ──"
set +e
out=$(run_cron 4 "INSUFFICIENT")
rc=$?
set -e

assert_eq "T2 exit code" "$rc" "4"
assert_contains "T2 prev INSUFFICIENT" "$out" "prev:      INSUFFICIENT_DURATION"
assert_contains "T2 alert is none" "$out" "alert:     none"

# ── T3: INSUFFICIENT → PASS fires INFO alert (STAGE_1 unblocked moment) ─────
echo
echo "── T3: INSUFFICIENT → PASS fires INFO ──"
set +e
out=$(run_cron 0 "PASS")
rc=$?
set -e

assert_eq "T3 exit code" "$rc" "0"
assert_contains "T3 state PASS" "$out" "state:     PASS"
assert_contains "T3 prev INSUFFICIENT" "$out" "prev:      INSUFFICIENT_DURATION"
assert_contains "T3 alert INFO" "$out" "alert:     INFO"
assert_contains "T3 DRY_RUN payload" "$out" "DRY_RUN: would send INFO"
assert_contains "T3 STAGE_1 message" "$out" "STAGE_1"

# ── T4: PASS → PASS is silent (no spam) ─────────────────────────────────────
echo
echo "── T4: PASS → PASS is silent ──"
set +e
out=$(run_cron 0 "PASS")
rc=$?
set -e

assert_eq "T4 exit code" "$rc" "0"
assert_contains "T4 prev PASS" "$out" "prev:      PASS"
assert_contains "T4 alert is none" "$out" "alert:     none"

# ── T5: PASS → THRESHOLD fires CRITICAL (gate FAILED) ───────────────────────
echo
echo "── T5: PASS → THRESHOLD fires CRITICAL ──"
set +e
out=$(run_cron 1 "THRESHOLD")
rc=$?
set -e

assert_eq "T5 exit code" "$rc" "1"
assert_contains "T5 state THRESHOLD" "$out" "state:     THRESHOLD"
assert_contains "T5 alert CRITICAL" "$out" "alert:     CRITICAL"
assert_contains "T5 DRY_RUN CRITICAL" "$out" "DRY_RUN: would send CRITICAL"
assert_contains "T5 STAGE_1 blocker" "$out" "STAGE_1 promotion blocker"

# ── T6: THRESHOLD → THRESHOLD STILL fires CRITICAL (persistent failure) ─────
# Rationale: an ongoing failure should keep reminding the operator weekly.
# Compare with PASS→PASS which goes silent — success doesn't need repeating;
# failure does.
echo
echo "── T6: THRESHOLD → THRESHOLD STILL fires CRITICAL ──"
set +e
out=$(run_cron 1 "THRESHOLD")
rc=$?
set -e

assert_eq "T6 exit code" "$rc" "1"
assert_contains "T6 prev THRESHOLD" "$out" "prev:      THRESHOLD"
assert_contains "T6 alert CRITICAL repeated" "$out" "alert:     CRITICAL"

# ── T7: THRESHOLD → SIGNAL_DIVERGENCE also CRITICAL ─────────────────────────
echo
echo "── T7: THRESHOLD → SIGNAL_DIVERGENCE is CRITICAL ──"
set +e
out=$(run_cron 2 "SIGNAL_DIVERGENCE")
rc=$?
set -e

assert_eq "T7 exit code" "$rc" "2"
assert_contains "T7 state SIGNAL_DIVERGENCE" "$out" "state:     SIGNAL_DIVERGENCE"
assert_contains "T7 alert CRITICAL" "$out" "alert:     CRITICAL"

# ── T8: SIGNAL_DIVERGENCE → PASS fires INFO (recovery) ──────────────────────
echo
echo "── T8: SIGNAL_DIVERGENCE → PASS fires INFO ──"
set +e
out=$(run_cron 0 "PASS")
rc=$?
set -e

assert_eq "T8 exit code" "$rc" "0"
assert_contains "T8 alert INFO recovery" "$out" "alert:     INFO"
assert_contains "T8 prev SIGNAL_DIVERGENCE" "$out" "prev:      SIGNAL_DIVERGENCE"

# ── T9: INPUT_ERROR (first time) fires WARN; persistent INPUT_ERROR silent ──
echo
echo "── T9: PASS → INPUT_ERROR fires WARN ──"
set +e
out=$(run_cron 3 "INPUT_ERROR")
rc=$?
set -e

assert_eq "T9 exit code" "$rc" "3"
assert_contains "T9 alert WARN" "$out" "alert:     WARN"
assert_contains "T9 testnet empty hint" "$out" "testnet dir empty"

# ── T10: INPUT_ERROR → INPUT_ERROR is silent (don't spam config errors) ─────
echo
echo "── T10: INPUT_ERROR → INPUT_ERROR is silent ──"
set +e
out=$(run_cron 3 "INPUT_ERROR")
rc=$?
set -e

assert_eq "T10 exit code" "$rc" "3"
assert_contains "T10 alert is none" "$out" "alert:     none"

# ── T11: unexpected exit fires WARN ─────────────────────────────────────────
echo
echo "── T11: unexpected exit 99 → WARN ──"
set +e
out=$(run_cron 99 "WHAT")
rc=$?
set -e

assert_eq "T11 exit code" "$rc" "99"
assert_contains "T11 state UNEXPECTED" "$out" "state:     UNEXPECTED_99"
assert_contains "T11 alert WARN" "$out" "alert:     WARN"

# ── T12: --quiet output is one line ─────────────────────────────────────────
echo
echo "── T12: --quiet emits a one-line summary ──"
set +e
out=$(run_cron 0 "PASS" --quiet)
rc=$?
set -e

assert_eq "T12 exit code" "$rc" "0"
# Should contain a single line starting with "layer3_cron:"
# Ignore the DRY_RUN section that comes before — that's the alert printer.
quiet_line=$(echo "$out" | grep -E "^layer3_cron:" | head -1)
assert_contains "T12 quiet line" "$quiet_line" "state="
assert_contains "T12 quiet exit" "$quiet_line" "exit="

# ── T13: history file format — every row parses as JSON ─────────────────────
echo
echo "── T13: history file rows all valid JSON ──"
n_rows=$(wc -l < "$HISTORY" | tr -d ' ')
parsed_ok=$(jq -c '.' "$HISTORY" 2>/dev/null | wc -l | tr -d ' ')
assert_eq "T13 all rows parse" "$parsed_ok" "$n_rows"

# All rows should have ts, state, exit_code, prev_state, log, host
missing=$(jq -c 'select(.ts==null or .state==null or .exit_code==null or .prev_state==null or .log==null or .host==null)' "$HISTORY" | wc -l | tr -d ' ')
assert_eq "T13 all rows have required fields" "$missing" "0"

# ── Summary ──────────────────────────────────────────────────────────────────
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
