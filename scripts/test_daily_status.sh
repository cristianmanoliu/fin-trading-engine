#!/usr/bin/env bash
# test_daily_status.sh — assertion-based tests for daily_status.sh.
#
# Covers:
#   - sourceability (BASH_SOURCE guard works; main flow doesn't fire)
#   - limbo_label / lag_label translate exit codes to human strings
#   - aggregate_exit precedence: KILL > WARN > OK
#   - aggregate_exit fail-modes (section-fail, lag-tier, LIMBO-tier)
#   - CLI: --help, --quiet, unknown flag, --local + positional VPS conflict
#
# Run:
#   bash scripts/test_daily_status.sh
set -euo pipefail

SCRIPT_DIR="$(cd "$(dirname "${BASH_SOURCE[0]}")" && pwd)"
DAILY_STATUS="${SCRIPT_DIR}/daily_status.sh"

# Source helpers (the BASH_SOURCE guard prevents main flow).
# shellcheck source=daily_status.sh disable=SC1091
source "$DAILY_STATUS"

PASS=0
FAIL=0
FAIL_LINES=()

assert_eq() {
    local got="$1" expected="$2" name="$3"
    if [[ "$got" == "$expected" ]]; then
        PASS=$((PASS + 1))
        echo "  ✓ $name"
    else
        FAIL=$((FAIL + 1))
        FAIL_LINES+=("  ✗ $name: got=[$got] expected=[$expected]")
    fi
}

assert_contains() {
    local haystack="$1" needle="$2" name="$3"
    if [[ "$haystack" == *"$needle"* ]]; then
        PASS=$((PASS + 1))
        echo "  ✓ $name"
    else
        FAIL=$((FAIL + 1))
        FAIL_LINES+=("  ✗ $name: needle [$needle] not in haystack [$haystack]")
    fi
}

# ── label helpers ───────────────────────────────────────────────────────────
echo
echo "── limbo_label ──"
assert_eq "$(limbo_label 0)" "CONTINUE"        "exit 0 → CONTINUE"
assert_eq "$(limbo_label 1)" "PROMOTE"         "exit 1 → PROMOTE"
assert_eq "$(limbo_label 2)" "WATCH"           "exit 2 → WATCH"
assert_eq "$(limbo_label 3)" "OPERATOR_REVIEW" "exit 3 → OPERATOR_REVIEW"
assert_eq "$(limbo_label 4)" "KILL"            "exit 4 → KILL"
assert_eq "$(limbo_label 5)" "INPUT_ERROR"     "exit 5 → INPUT_ERROR"
assert_eq "$(limbo_label 9)" "UNKNOWN(9)"      "exit 9 → UNKNOWN(9) (defensive)"

echo
echo "── lag_label ──"
assert_eq "$(lag_label 0)" "HEALTHY"     "exit 0 → HEALTHY"
assert_eq "$(lag_label 1)" "DEGRADED"    "exit 1 → DEGRADED"
assert_eq "$(lag_label 2)" "HIGH"        "exit 2 → HIGH"
assert_eq "$(lag_label 3)" "SSH_FAILURE" "exit 3 → SSH_FAILURE"
assert_eq "$(lag_label 4)" "INPUT_ERROR" "exit 4 → INPUT_ERROR"
assert_eq "$(lag_label -)" "SKIPPED"     "sentinel '-' → SKIPPED (not UNKNOWN)"

# ── aggregate_exit ──────────────────────────────────────────────────────────
echo
echo "── aggregate_exit happy path ──"
# All clean: LIMBO=CONTINUE, lag=HEALTHY, sections OK.
assert_eq "$(aggregate_exit 0 0 1 1)" "0" "all-clean → 0 (OK)"
# LIMBO=PROMOTE is still informational, lag healthy.
assert_eq "$(aggregate_exit 1 0 1 1)" "0" "PROMOTE+HEALTHY → 0 (OK)"
# LIMBO=WATCH is a soft signal but the locked rule maps it to NO action.
assert_eq "$(aggregate_exit 2 0 1 1)" "0" "WATCH+HEALTHY → 0 (OK per LIMBO mapping)"

echo
echo "── aggregate_exit WARN tier ──"
# LIMBO=OPERATOR_REVIEW alone → WARN.
assert_eq "$(aggregate_exit 3 0 1 1)" "1" "OPERATOR_REVIEW → WARN"
# Lag DEGRADED alone → WARN.
assert_eq "$(aggregate_exit 0 1 1 1)" "1" "lag DEGRADED → WARN"
# Lag SSH failure → WARN (couldn't tell, treat as concern but not KILL).
assert_eq "$(aggregate_exit 0 3 1 1)" "1" "lag SSH_FAILURE → WARN"
# Section failed to invoke (cost helper missing) → WARN.
assert_eq "$(aggregate_exit 0 0 0 1)" "1" "cost section failed → WARN"
# Traj section failed → WARN.
assert_eq "$(aggregate_exit 0 0 1 0)" "1" "traj section failed → WARN"

echo
echo "── aggregate_exit KILL tier ──"
# LIMBO=KILL → KILL regardless of other tiers.
assert_eq "$(aggregate_exit 4 0 1 1)" "2" "LIMBO KILL → 2"
# LIMBO=INPUT_ERROR → KILL (snapshot missing means we cannot trust any
# verdict above; explicit not silent CONTINUE).
assert_eq "$(aggregate_exit 5 0 1 1)" "2" "LIMBO INPUT_ERROR → 2"
# Lag HIGH → KILL.
assert_eq "$(aggregate_exit 0 2 1 1)" "2" "lag HIGH → 2"

echo
echo "── aggregate_exit precedence ──"
# KILL beats WARN.
assert_eq "$(aggregate_exit 4 1 0 0)" "2" "KILL+WARN+section-fail → KILL (highest tier wins)"
# Two WARN tiers don't escalate to KILL.
assert_eq "$(aggregate_exit 3 1 1 1)" "1" "OPERATOR_REVIEW + DEGRADED → WARN (no escalation)"
# LIMBO sentinel "-" (not invoked) shouldn't crash.
assert_eq "$(aggregate_exit - - 1 1)" "0" "unset LIMBO/lag default → OK"

# ── extract_stage_1_compact (added 2026-05-12 with timeline integration) ───
echo
echo "── extract_stage_1_compact ──"

# Empty input → empty output (no false-positive 'STAGE_1=' appendage).
assert_eq "$(extract_stage_1_compact "")" "" "empty input → empty output"

# Basic quiet line WITHOUT baseline scenario (rate=historical case).
basic_line="forward-paper: day 7 / 0 trades / rate=1.18/d (historical_fleet) / binding=trade-count / STAGE_1 earliest 2026-09-09 (120d)"
assert_eq "$(extract_stage_1_compact "$basic_line")" "120d" \
    "basic line → 120d"

# Quiet line WITH baseline + spread suffix (observed-rate case). The
# extractor must capture the PRIMARY scenario's days, not the baseline's,
# even though both appear in the same line.
dual_line="forward-paper: day 30 / 5 trades / rate=0.17/d (observed) / binding=trade-count / STAGE_1 earliest 2028-10-21 (870d) / baseline 1.18/d → 2026-09-09 (+773d spread)"
assert_eq "$(extract_stage_1_compact "$dual_line")" "870d" \
    "dual-projection line → primary 870d (not baseline 97d, not spread 773d)"

# Malformed line missing STAGE_1 marker → empty output (defensive).
malformed="some other tool output without the expected token"
assert_eq "$(extract_stage_1_compact "$malformed")" "" \
    "malformed line → empty (defensive)"

# Line with STAGE_1 marker but unexpected paren content (operator wrote
# a custom rate? regression where timeline emits a different format?)
# → empty output, NOT a garbled annotation.
bad_format="forward-paper: ... STAGE_1 earliest 2026-09-09 (TBD)"
assert_eq "$(extract_stage_1_compact "$bad_format")" "" \
    "non-numeric days → empty (won't append STAGE_1=TBD to footer)"

# ── CLI parse + flow ────────────────────────────────────────────────────────
echo
echo "── CLI flag parsing ──"

# --help should exit 0 and print sections from the header docstring.
help_output=$(bash "$DAILY_STATUS" --help 2>&1)
help_rc=$?
assert_eq "$help_rc" "0" "--help exits 0"
assert_contains "$help_output" "daily_status.sh — single paste-ready" "--help shows header"

# Unknown flag → exit 3 (INPUT_ERR).
set +e
bash "$DAILY_STATUS" --nonsense 2>/dev/null
rc=$?
set -e
assert_eq "$rc" "3" "unknown flag → INPUT_ERR"

# --local + positional VPS = mutually exclusive → exit 3.
set +e
err=$(bash "$DAILY_STATUS" --local root@other 2>&1)
rc=$?
set -e
assert_eq "$rc" "3" "--local + positional VPS → INPUT_ERR"
assert_contains "$err" "mutually exclusive" "conflict diagnostic surfaces"

# ── End-to-end QUIET integration (timeline ↔ daily_status, 2026-05-12) ────
echo
echo "── QUIET footer integrates STAGE_1=Nd ──"

# Build a synthetic local journal so the timeline section has data to
# project from. Then invoke daily_status --local --quiet and assert the
# aggregate footer contains STAGE_1=Nd. Run in a tempdir so we don't
# touch repo state.
INT_DIR=$(mktemp -d)
mkdir -p "$INT_DIR/logs/journal"
python3 -c "
import json
import datetime as dt
start = dt.datetime(2026, 5, 5, 20, 6)
with open('$INT_DIR/logs/journal/BTCUSDT-2026-05.jsonl', 'w') as f:
    for i in range(3):
        ts_open = (start + dt.timedelta(days=i, hours=2)).strftime('%Y-%m-%dT%H:%M:%SZ')
        ts_close = (start + dt.timedelta(days=i, hours=3)).strftime('%Y-%m-%dT%H:%M:%SZ')
        f.write(json.dumps({'event':'open','symbol':'BTCUSDT','ts':ts_open,
                            'side':'LONG','entry':100,'stop':99,'target':106}) + '\n')
        f.write(json.dumps({'event':'close','symbol':'BTCUSDT','ts':ts_close,
                            'side':'LONG','entry':100,'exit':106,
                            'outcome':'TARGET','pnl_usd':100}) + '\n')
"
set +e
quiet_out=$(cd "$INT_DIR" && bash "$DAILY_STATUS" --local --quiet 2>&1)
quiet_rc=$?
set -e
rm -rf "$INT_DIR"

# Don't assert exit code precisely — depends on LIMBO state which depends
# on synthetic-vs-real-snapshot history; assertion is that STAGE_1=Nd
# appears in the footer.
assert_contains "$quiet_out" "STAGE_1=" \
    "QUIET footer includes STAGE_1=Nd when timeline projects successfully"
assert_contains "$quiet_out" "daily_status:" \
    "QUIET footer still has daily_status: prefix"

# Negative case: no journal data → timeline exits 3 (input error) →
# extract_stage_1_compact returns empty → no STAGE_1= appended. Run
# from a tempdir with NO logs/ to exercise this path.
EMPTY_DIR=$(mktemp -d)
set +e
empty_out=$(cd "$EMPTY_DIR" && bash "$DAILY_STATUS" --local --quiet 2>&1)
set -e
rmdir "$EMPTY_DIR"
if [[ "$empty_out" == *"STAGE_1="* ]]; then
    FAIL=$((FAIL + 1))
    FAIL_LINES+=("  ✗ empty-journal case: footer should NOT contain STAGE_1= when timeline can't project, but got: $empty_out")
else
    PASS=$((PASS + 1))
    echo "  ✓ no-journal-data → footer omits STAGE_1= (defensive)"
fi

# ── Final ───────────────────────────────────────────────────────────────────
echo
echo "── results ──"
echo "PASS: $PASS"
echo "FAIL: $FAIL"
if [[ "$FAIL" -gt 0 ]]; then
    for line in "${FAIL_LINES[@]}"; do
        echo "$line"
    done
    exit 1
fi
echo "all green"
exit 0
