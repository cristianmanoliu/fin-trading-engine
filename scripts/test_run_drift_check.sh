#!/usr/bin/env bash
# test_run_drift_check.sh — regression suite for scripts/run_drift_check.sh.
#
# Pins the locked exit-code contract:
#   0  CLEAN              — no drift, no rule trip
#   1  INVESTIGATION      — single drift firing
#   2  INSUFFICIENT       — n_live below detector floor (pass-through)
#   3  ERROR              — detector itself failed
#   4  AUTO-KILL CANDIDATE — two firings ≥7 days apart
#   5  HISTORY_CORRUPT    — malformed line(s) in drift_check_history.jsonl
#
# T1-T7 cover the original 0-4 contract. T8-T12 are regression tests for
# the corrupt-history fail-open closed in this commit:
#
#   Original bug shape — a single malformed line in drift_check_history.jsonl
#   (kill -9 mid-write, disk-full, manual edit) caused jq + set -e + pipefail
#   to abort the wrapper inside the FIRINGS walk, exiting with jq's parse
#   exit code (~5). The case-to-Telegram map only handles 0-4 so the
#   alert never fired. weekly_audit.sh propagates the unmapped exit to
#   launchd as the day's "verdict." Decision-grade tool silently dies on
#   the only failure mode that matters.

set -uo pipefail

SCRIPT_DIR="$(cd "$(dirname "$0")" && pwd)"
WRAPPER="${SCRIPT_DIR}/run_drift_check.sh"

if [[ ! -x "$WRAPPER" ]]; then
    echo "wrapper not executable at $WRAPPER" >&2
    exit 1
fi

TMPDIR_ROOT=$(mktemp -d)
trap 'rm -rf "$TMPDIR_ROOT"' EXIT

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
        echo "    actual: ${haystack:0:300}"
        FAIL=$((FAIL + 1))
        FAIL_LABELS+=("$label")
    fi
}

# Make a fake detector script that exits with the requested code.
mkdetector() {
    local exitcode="$1"
    local script="${TMPDIR_ROOT}/fake_detector_${exitcode}_$$.py"
    cat > "$script" <<EOF
#!/usr/bin/env python3
import sys
print("fake detector run (exit=${exitcode})")
sys.exit(${exitcode})
EOF
    chmod +x "$script"
    echo "$script"
}

# Globals filled by invoke().
WRAPPER_EXIT=
WRAPPER_OUT=

invoke() {
    local detector_exit="$1" history_content="$2"
    local hist runs detector
    hist="${TMPDIR_ROOT}/history-$$-$RANDOM.jsonl"
    runs="${TMPDIR_ROOT}/runs-$$-$RANDOM"
    mkdir -p "$runs"
    printf '%s' "$history_content" > "$hist"
    detector=$(mkdetector "$detector_exit")
    # Empty Telegram env vars suppress the curl path (notify_telegram is
    # a no-op when token or chat is unset). Test asserts only on wrapper
    # output + exit code, not on network side effects.
    WRAPPER_OUT=$(TELEGRAM_BOT_TOKEN='' TELEGRAM_CHAT_ID='' \
        DRIFT_CHECK_DETECTOR="$detector" \
        DRIFT_CHECK_HISTORY="$hist" \
        DRIFT_CHECK_RUNS_DIR="$runs" \
        "$WRAPPER" --quiet 2>&1) || true
    # `|| true` collapses set -e in the test script; we still want the
    # ACTUAL exit code, so re-run capturing $? without || true.
    set +e
    TELEGRAM_BOT_TOKEN='' TELEGRAM_CHAT_ID='' \
        DRIFT_CHECK_DETECTOR="$detector" \
        DRIFT_CHECK_HISTORY="$hist" \
        DRIFT_CHECK_RUNS_DIR="$runs" \
        "$WRAPPER" --quiet > /dev/null 2>&1
    WRAPPER_EXIT=$?
    set -e
}

# Helpers to produce well-formed history entries.
mkentry() {
    local ts="$1" exit_code="$2" verdict="$3"
    printf '{"ts":"%s","exit_code":%d,"verdict":"%s","log_file":"/tmp/x.log"}\n' \
        "$ts" "$exit_code" "$verdict"
}

# --- T1: empty history + detector CLEAN ---
echo "T1: empty history + detector CLEAN → exit 0"
invoke 0 ""
assert_eq "T1.exit"     "$WRAPPER_EXIT" "0"
assert_contains "T1.text" "$WRAPPER_OUT" "CLEAN"

# --- T2: empty history + detector DRIFT_FIRED ---
echo "T2: empty history + detector DRIFT_FIRED → exit 1"
invoke 1 ""
assert_eq "T2.exit"     "$WRAPPER_EXIT" "1"
assert_contains "T2.text" "$WRAPPER_OUT" "INVESTIGATION"

# --- T3: empty history + detector INSUFFICIENT_DATA ---
echo "T3: empty history + detector INSUFFICIENT → exit 2"
invoke 2 ""
assert_eq "T3.exit"     "$WRAPPER_EXIT" "2"
assert_contains "T3.text" "$WRAPPER_OUT" "INSUFFICIENT"

# --- T4: empty history + detector unknown error ---
echo "T4: empty history + detector ERROR → exit 3"
invoke 3 ""
assert_eq "T4.exit"     "$WRAPPER_EXIT" "3"
assert_contains "T4.text" "$WRAPPER_OUT" "ERROR"

# --- T5: history with single DRIFT_FIRED — not enough for rule ---
echo "T5: 1 DRIFT_FIRED + detector CLEAN → exit 0 (rule needs 2)"
invoke 0 "$(mkentry "2026-04-01T00:00:00Z" 1 DRIFT_FIRED)"
assert_eq "T5.exit"     "$WRAPPER_EXIT" "0"
assert_contains "T5.text" "$WRAPPER_OUT" "CLEAN"

# --- T6: two DRIFT_FIRED ≥7d apart → AUTO-KILL CANDIDATE ---
# Note: $() strips trailing newlines, so re-add explicit \n between
# concatenated entries — without it both JSON objects collapse onto one
# line and the line-by-line walk only sees a single (multi-object) entry.
echo "T6: 2 DRIFT_FIRED ≥7d apart → exit 4"
hist=$(mkentry "2026-04-01T00:00:00Z" 1 DRIFT_FIRED)$'\n'
hist+=$(mkentry "2026-04-15T00:00:00Z" 1 DRIFT_FIRED)$'\n'
invoke 0 "$hist"
assert_eq "T6.exit"     "$WRAPPER_EXIT" "4"
assert_contains "T6.text" "$WRAPPER_OUT" "AUTO-KILL CANDIDATE"

# --- T7: two DRIFT_FIRED <7d apart → NOT enough for rule ---
echo "T7: 2 DRIFT_FIRED <7d apart → exit 0 (rule untripped)"
hist=$(mkentry "2026-04-01T00:00:00Z" 1 DRIFT_FIRED)$'\n'
hist+=$(mkentry "2026-04-05T00:00:00Z" 1 DRIFT_FIRED)$'\n'
invoke 0 "$hist"
assert_eq "T7.exit"     "$WRAPPER_EXIT" "0"

# --- T8 (REGRESSION): malformed JSON line should NOT crash the wrapper ---
echo "T8: malformed JSON line → exit 5 HISTORY_CORRUPT (not jq abort)"
hist=$(mkentry "2026-04-01T00:00:00Z" 0 CLEAN)
hist+="this is not json at all"$'\n'
hist+=$(mkentry "2026-04-02T00:00:00Z" 0 CLEAN)
invoke 0 "$hist"
assert_eq "T8.exit"     "$WRAPPER_EXIT" "5"
assert_contains "T8.text" "$WRAPPER_OUT" "HISTORY_CORRUPT"

# --- T9 (REGRESSION): DRIFT_FIRED with missing .ts field ---
echo "T9: DRIFT_FIRED entry missing .ts → exit 5 HISTORY_CORRUPT"
hist='{"exit_code":1,"verdict":"DRIFT_FIRED","log_file":"/tmp/x.log"}'$'\n'
invoke 0 "$hist"
assert_eq "T9.exit"     "$WRAPPER_EXIT" "5"
assert_contains "T9.text" "$WRAPPER_OUT" "HISTORY_CORRUPT"

# --- T10 (REGRESSION): DRIFT_FIRED with malformed .ts value ---
echo "T10: DRIFT_FIRED with garbage .ts → exit 5 HISTORY_CORRUPT"
hist='{"ts":"not-a-timestamp","exit_code":1,"verdict":"DRIFT_FIRED","log_file":"/tmp/x.log"}'$'\n'
invoke 0 "$hist"
assert_eq "T10.exit"    "$WRAPPER_EXIT" "5"
assert_contains "T10.text" "$WRAPPER_OUT" "HISTORY_CORRUPT"

# --- T11 (REGRESSION): two valid DRIFT_FIRED ≥7d apart PLUS one malformed
# line — rule trip MUST be suppressed; corruption takes precedence over
# auto-kill so the operator doesn't act on a rule evaluated against a
# half-broken file. ---
echo "T11: valid auto-kill pair + malformed line → exit 5 (suppress AUTO-KILL)"
hist=$(mkentry "2026-04-01T00:00:00Z" 1 DRIFT_FIRED)
hist+="garbage line"$'\n'
hist+=$(mkentry "2026-04-15T00:00:00Z" 1 DRIFT_FIRED)
invoke 0 "$hist"
assert_eq "T11.exit"    "$WRAPPER_EXIT" "5"
assert_contains "T11.text" "$WRAPPER_OUT" "HISTORY_CORRUPT"

# --- T12: detector DRIFT firing this run + clean history → exit 1
# (drift takes precedence over the empty-rule case; corruption check
# only suppresses auto-kill, not new firings) ---
echo "T12: clean history + detector DRIFT_FIRED → exit 1 INVESTIGATION"
hist=$(mkentry "2026-04-01T00:00:00Z" 0 CLEAN)
invoke 1 "$hist"
assert_eq "T12.exit"    "$WRAPPER_EXIT" "1"
assert_contains "T12.text" "$WRAPPER_OUT" "INVESTIGATION"

# --- T13 (PIN): operator-supplied HISTORY parent dir doesn't exist —
# pre-fix the redirect at line 62 (`: > "$HISTORY"`) failed under set -e
# without ever reaching the case → Telegram dispatch. The wrapper now
# pre-creates the parent dir, so this path PASSES rather than aborting. ---
echo "T13: HISTORY parent dir missing → wrapper creates it, exits 0"
nested_hist="${TMPDIR_ROOT}/deep/path/that/does/not/exist/$$/history.jsonl"
nested_runs="${TMPDIR_ROOT}/deep-runs-$$"
detector_zero=$(mkdetector 0)
set +e
TELEGRAM_BOT_TOKEN='' TELEGRAM_CHAT_ID='' \
    DRIFT_CHECK_DETECTOR="$detector_zero" \
    DRIFT_CHECK_HISTORY="$nested_hist" \
    DRIFT_CHECK_RUNS_DIR="$nested_runs" \
    "$WRAPPER" --quiet > /dev/null 2>&1
T13_RC=$?
set -e
assert_eq "T13.exit"     "$T13_RC" "0"
# Verify the parent dir was actually created (= the mkdir worked).
if [[ -d "$(dirname "$nested_hist")" ]]; then
    PASS=$((PASS + 1))
    echo "  ✓ T13.parent_dir_created"
else
    FAIL=$((FAIL + 1))
    FAIL_LABELS+=("T13.parent_dir_created")
    echo "  ✗ T13.parent_dir_created: parent dir not created"
fi

# --- T14 (PIN): detector exit 99 → wrapper exit 3 (ERROR).
# Locks the `else WRAPPER_EXIT=3` catch-all so a future refactor that
# moves the if-elif chain can't accidentally drop it. T4 tests exit 3
# specifically; T14 tests an exit code outside the documented 0-3
# Python contract. Same lens as today's layer3_verdict F1 — unknown
# subprocess exit codes must never fall through to a success branch.
echo "T14: detector exit 99 (unknown) → wrapper exit 3 (ERROR)"
invoke 99 ""
assert_eq "T14.exit"     "$WRAPPER_EXIT" "3"
assert_contains "T14.text" "$WRAPPER_OUT" "ERROR"

# --- T15 (PIN): detector exit 139 (SIGSEGV shape) → wrapper exit 3.
# Defends against the Python interpreter crashing on a corrupt jsonl
# / OOM-killed by the operator's laptop / etc. Must not silently route
# to CLEAN or INVESTIGATION via type-coercion accident.
echo "T15: detector exit 139 (SIGSEGV shape) → wrapper exit 3 (ERROR)"
invoke 139 ""
assert_eq "T15.exit"     "$WRAPPER_EXIT" "3"
assert_contains "T15.text" "$WRAPPER_OUT" "ERROR"

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
