#!/usr/bin/env bash
# layer3_verdict.sh — Layer 3 shadow-parity gate runner.
#
# Wraps cmd/journal_diff with the locked Layer 3 acceptance criterion from
# results/real_money_executor_architecture_decision_rule_2026-05-08.md:
#
#     1. pnl_usd per closed trade differs by ≤ 0.5%
#     2. no signal-generation divergence (both executors fire same trades)
#     3. testnet window ≥ 7 calendar days
#
# After the operator runs the engine in dual-runner shadow mode for ≥7d
# (`cmd/engine --executor binance_live_testnet` plus
# `--layer3-binance-testnet-journal-dir DIR`), this wrapper
#   - validates both journal dirs exist and contain data
#   - computes the testnet running-window from the oldest open event
#   - invokes cmd/journal_diff with the locked 0.5% threshold
#   - emits a single Layer 3 PASS/FAIL verdict on top of the binary's report
#
# This is the gate before STAGE_1 real-money promotion. PASS means proceed
# to the staged $100/trade rollout; any FAIL or duration shortfall means
# STOP — operator must NOT flip --executor to binance_live in production.
#
# Exit codes:
#     0  PASS — Layer 3 criterion met (parity within threshold, no signal
#        divergence, ≥7 days of testnet data)
#     1  THRESHOLD — pnl drift exceeds threshold (≥1 matched pair > 0.5%)
#     2  SIGNAL_DIV — executors disagreed on which trades to fire
#     3  INPUT_ERROR — missing/unreadable dirs, no trades found, or invalid
#        flags. Telegram-tier dual sense: this is operator-error / config,
#        NOT a strategy failure — must be routed differently than 1/2.
#     4  INSUFFICIENT_DURATION — testnet window < min-days threshold; insufficient
#        data to evaluate the gate (operator should keep running and re-check)
#
# Usage:
#     scripts/layer3_verdict.sh --stub-dir DIR --testnet-dir DIR
#     scripts/layer3_verdict.sh --threshold-pct 0.5 --min-days 7    # locked defaults
#     scripts/layer3_verdict.sh --testnet-start 2026-05-15T00:00:00Z   # explicit start
#     scripts/layer3_verdict.sh --skip-min-days                       # dry-run before 7d
#     scripts/layer3_verdict.sh --verbose                             # journal_diff verbose
#     scripts/layer3_verdict.sh --no-scope-by-dir-b                   # disable per-symbol filter (forensic only)
#
# --scope-by-dir-b is enabled by default. Layer 3 wraps a SUBSET of live
# engines (per locked rule "before flipping any SINGLE engine"), so the
# stub dir has 16 live symbols while the testnet dir has only the wrapped
# subset. Without filtering, every non-wrapped live close fires
# SIGNAL_DIVERGENCE and the gate cannot PASS. The flag restricts pairing
# to symbols present in dir-b; trades in dir-b without peers still
# divergence (real failure — testnet shadow saw a signal the stub didn't).
#
# Test env-vars (NOT for production use):
#     LAYER3_VERDICT_DIFF_OVERRIDE=PATH — use this binary as journal_diff
#         instead of building from source. Lets the test suite inject canned
#         exit codes (panic=4, segfault=139, future contract additions) to
#         exercise the wrapper's exit-code dispatch without rebuilding Go.
set -euo pipefail

# --- defaults (locked rule) ---
THRESHOLD_PCT="0.5"
MIN_DAYS="7"
STUB_DIR="/var/log/paper-live/journal"
TESTNET_DIR="/var/log/paper-live/journal-testnet"
TESTNET_START=""
SKIP_MIN_DAYS=0
VERBOSE=0
LABEL_A="stub"
LABEL_B="testnet"
# Layer 3 is asymmetric by design per the locked rule: the testnet shadow
# wraps a SUBSET of live engines ("before flipping any SINGLE engine"). The
# stub dir contains all 16 live symbols; the testnet dir contains only the
# wrapped subset. Without scope-by-dir-b, every non-wrapped close in the
# stub dir fires SIGNAL_DIVERGENCE (exit 2) and the gate cannot PASS.
# Default ON; --no-scope-by-dir-b reverts to symmetric for forensic debug.
SCOPE_BY_DIR_B=1

usage() {
    sed -n '2,/^set/p' "$0" | sed 's/^# \?//' | head -n -1
    exit 3
}

# --- arg parse ---
while [[ $# -gt 0 ]]; do
    case "$1" in
        --stub-dir)        STUB_DIR="$2"; shift 2 ;;
        --testnet-dir)     TESTNET_DIR="$2"; shift 2 ;;
        --threshold-pct)   THRESHOLD_PCT="$2"; shift 2 ;;
        --min-days)        MIN_DAYS="$2"; shift 2 ;;
        --testnet-start)   TESTNET_START="$2"; shift 2 ;;
        --skip-min-days)   SKIP_MIN_DAYS=1; shift ;;
        --verbose)         VERBOSE=1; shift ;;
        --label-a)         LABEL_A="$2"; shift 2 ;;
        --label-b)         LABEL_B="$2"; shift 2 ;;
        --no-scope-by-dir-b) SCOPE_BY_DIR_B=0; shift ;;
        -h|--help)         usage ;;
        *)
            echo "ERROR: unknown flag: $1" >&2
            echo "Run with --help for usage." >&2
            exit 3
            ;;
    esac
done

# --- input validation ---
# Same audit-pattern shape that closed bugs in journal_diff itself:
# missing/empty/typo'd dirs must surface as exit 3 (input error), NOT
# silently route to PASS via "no trades found = no violations."
for label_dir in "stub:$STUB_DIR" "testnet:$TESTNET_DIR"; do
    label="${label_dir%%:*}"
    dir="${label_dir#*:}"
    if [[ ! -d "$dir" ]]; then
        echo "ERROR: --${label}-dir is not a directory: $dir" >&2
        echo "  Layer 3 requires both journal dirs present. A typo'd path would" >&2
        echo "  produce zero trades on that side, which would silently look like" >&2
        echo "  'no signal divergence, no threshold violation' = false PASS." >&2
        exit 3
    fi
    # Empty dirs (no .jsonl files at all) are not legitimate Layer 3 input.
    # The dual-runner setup writes journal events from tick 1; an empty dir
    # means the engine wasn't actually running in shadow mode.
    if [[ -z "$(find "$dir" -maxdepth 3 -name '*.jsonl' -print -quit 2>/dev/null)" ]]; then
        echo "ERROR: --${label}-dir contains no .jsonl files: $dir" >&2
        echo "  Layer 3 requires both engines to have written journal data." >&2
        echo "  Verify the engine is running with --layer3-binance-testnet-" >&2
        echo "  journal-dir DIR and that the path matches what's passed here." >&2
        exit 3
    fi
done

# --- duration check ---
# Computes testnet running-window. Three sources, in priority order:
#   1. --testnet-start operator override (most authoritative)
#   2. Oldest "open" event ts in the testnet journal (most accurate proxy)
#   3. Bail with an explicit error (don't silently default to "0 days")
#
# Must NOT fall back to 0d-since-epoch on parse failure — that would
# silently route a malformed timestamp to "duration check passed because
# the date was treated as 1970." Same audit pattern: parse failure must
# fail loud, not collapse to a successful-looking number.
NOW_EPOCH=$(date -u +%s)

if [[ -n "$TESTNET_START" ]]; then
    start_source="operator-supplied --testnet-start"
    start_ts="$TESTNET_START"
else
    # Find the oldest "open" event across all .jsonl files in the testnet
    # dir. Use jq for safety (regex would mishandle nested quotes etc.).
    if ! command -v jq >/dev/null 2>&1; then
        echo "ERROR: jq not found; install it or pass --testnet-start explicitly." >&2
        exit 3
    fi
    start_source="oldest open event in $TESTNET_DIR"
    start_ts=$(find "$TESTNET_DIR" -maxdepth 3 -name '*.jsonl' -print0 \
                | xargs -0 cat \
                | jq -r 'select(.event=="open") | .ts' 2>/dev/null \
                | sort \
                | head -1)
    if [[ -z "$start_ts" ]]; then
        echo "ERROR: no 'open' events found in testnet journal at $TESTNET_DIR" >&2
        echo "  Either no signals have fired yet (run longer) or the journal" >&2
        echo "  schema has changed and 'open' events are no longer present." >&2
        exit 3
    fi
fi

# Parse the timestamp — both BSD and GNU date dialects supported. If both
# fail, exit 3 (do NOT proceed with garbage epoch).
start_epoch=$(date -u -j -f "%Y-%m-%dT%H:%M:%SZ" "${start_ts%%.*}Z" +%s 2>/dev/null \
              || date -u -d "$start_ts" +%s 2>/dev/null \
              || echo "INVALID")
if [[ "$start_epoch" == "INVALID" ]]; then
    echo "ERROR: could not parse testnet start timestamp: $start_ts" >&2
    echo "  Source: $start_source" >&2
    echo "  Expected format: YYYY-MM-DDTHH:MM:SSZ (RFC3339)." >&2
    exit 3
fi

elapsed_seconds=$(( NOW_EPOCH - start_epoch ))
# Future-timestamp guard: a clock-skewed operator host or a typo'd
# --testnet-start (e.g. tomorrow's date) yields elapsed_seconds < 0,
# which pre-fix routed to INSUFFICIENT_DURATION (exit 4) and told the
# operator to "keep running and re-check" — terrible advice for what
# is fundamentally an input error. Surface as exit 3 (INPUT_ERROR)
# instead so the operator fixes the clock / overrides, not waits.
if [[ "$elapsed_seconds" -lt 0 ]]; then
    echo "ERROR: testnet start_ts is in the future (${start_ts})" >&2
    echo "  Source: $start_source" >&2
    echo "  elapsed_seconds=${elapsed_seconds} < 0 — either operator clock skew" >&2
    echo "  or a typo'd --testnet-start. Cannot evaluate Layer 3 against a" >&2
    echo "  future window. Fix the source and re-run." >&2
    exit 3
fi
elapsed_days=$(( elapsed_seconds / 86400 ))
elapsed_hours=$(( elapsed_seconds / 3600 ))

# --- run journal_diff ---
# Build to a temp binary rather than `go run` because `go run` collapses
# the inner program's exit code to 1 (it does NOT propagate exit-2 for
# signal divergence or exit-3 for input error). The wrapper relies on the
# precise 0/1/2/3/4 taxonomy to compose its own Layer 3 verdict — collapsing
# to "1" would route signal divergence to the THRESHOLD branch.
SCRIPT_ROOT="$(cd "$(dirname "$0")/.." && pwd)"
TMP_BIN_DIR=$(mktemp -d)
trap 'rm -rf "$TMP_BIN_DIR"' EXIT
DIFF_BIN="$TMP_BIN_DIR/journal_diff"

# Test-only override: skip the build and use the supplied binary path
# instead. Used by test_layer3_verdict.py to inject canned exit codes
# (panic=4, segfault=139, hypothetical future code) without rebuilding
# Go. Same pattern shape as LAYER2_SMOKE_INJECT_LOG.
if [[ -n "${LAYER3_VERDICT_DIFF_OVERRIDE:-}" ]]; then
    if [[ ! -x "${LAYER3_VERDICT_DIFF_OVERRIDE}" ]]; then
        echo "ERROR: LAYER3_VERDICT_DIFF_OVERRIDE=${LAYER3_VERDICT_DIFF_OVERRIDE} is not executable" >&2
        exit 3
    fi
    DIFF_BIN="${LAYER3_VERDICT_DIFF_OVERRIDE}"
else
    if ! (cd "$SCRIPT_ROOT" && go build -o "$DIFF_BIN" \
            ./cmd/journal_diff) 2>"$TMP_BIN_DIR/build.err"; then
        echo "ERROR: failed to build cmd/journal_diff:" >&2
        cat "$TMP_BIN_DIR/build.err" >&2
        exit 3
    fi
fi

DIFF_ARGS=(
    --dir-a "$STUB_DIR"
    --dir-b "$TESTNET_DIR"
    --threshold-pct "$THRESHOLD_PCT"
    --label-a "$LABEL_A"
    --label-b "$LABEL_B"
)
[[ "$VERBOSE" == "1" ]] && DIFF_ARGS+=(--verbose)
# Default-on per locked rule (asymmetric per-symbol pilot architecture);
# operator opts out via --no-scope-by-dir-b for forensic debug.
[[ "$SCOPE_BY_DIR_B" == "1" ]] && DIFF_ARGS+=(--scope-by-dir-b)

set +e
diff_out=$("$DIFF_BIN" "${DIFF_ARGS[@]}" 2>&1)
diff_exit=$?
set -e

# --- compose Layer 3 verdict ---
SEP="$(printf '%0.s═' {1..78})"
echo
echo "$SEP"
echo "  Layer 3 shadow-parity verdict — $(date -u '+%Y-%m-%d %H:%M UTC')"
echo "  Source: $LABEL_A=$STUB_DIR"
echo "          $LABEL_B=$TESTNET_DIR"
echo "  Locked rule: results/real_money_executor_architecture_decision_rule_2026-05-08.md"
echo "$SEP"
echo
echo "  Testnet window:    ${elapsed_days}d ${elapsed_hours}h (start=${start_ts}, source=${start_source})"
echo "  Min-days gate:     ${MIN_DAYS}d $([[ "$SKIP_MIN_DAYS" == "1" ]] && echo "(SKIPPED via --skip-min-days)")"
echo "  pnl threshold:     ${THRESHOLD_PCT}%  per matched pair (locked)"
echo

# Pass through the journal_diff binary's full report (operator wants per-symbol
# stats, violation list, signal-divergence breakdowns).
echo "$diff_out"

# Compose final verdict that combines parity + duration gates.
echo
echo "$SEP"
duration_ok=1
if [[ "$elapsed_days" -lt "$MIN_DAYS" ]]; then
    duration_ok=0
fi

# Exit code precedence (matches the documented taxonomy):
#   journal_diff exit 1 → THRESHOLD — overrides everything else
#   journal_diff exit 2 → SIGNAL_DIV — overrides duration shortfall
#   journal_diff exit 3 → INPUT_ERROR — passes through (binary couldn't run)
#   journal_diff exit 4 → PANIC (binary defect) — route to INPUT_ERROR
#   journal_diff other  → UNEXPECTED — route to INPUT_ERROR (NEVER fall
#                         through to PASS — pre-fix, any non-0/1/2/3 exit
#                         silently routed to the duration-then-PASS branch,
#                         which is a fail-open: a panic'd diff binary
#                         would promote a broken Layer 3 to STAGE_1)
#   duration < min     → INSUFFICIENT_DURATION (only if parity OK)
#   else               → PASS
if [[ "$diff_exit" == "3" ]]; then
    echo "  >>> VERDICT: INPUT_ERROR — journal_diff binary could not run"
    echo "      Layer 3 STOP — fix the input issue and re-run."
    echo "$SEP"
    exit 3
fi
if [[ "$diff_exit" == "4" ]]; then
    echo "  >>> VERDICT: INPUT_ERROR — journal_diff panicked (exit 4)"
    echo "      Binary defect, not a strategy failure. See PANIC stderr"
    echo "      above for the stack trace. Layer 3 STOP — fix the binary"
    echo "      and re-run (do NOT promote to STAGE_1)."
    echo "$SEP"
    exit 3
fi
if [[ "$diff_exit" == "2" ]]; then
    echo "  >>> VERDICT: SIGNAL_DIVERGENCE — Layer 3 FAIL"
    echo "      Stub and testnet executors fired different trades."
    echo "      Operator MUST NOT promote to STAGE_1 / real-money. Investigate"
    echo "      executor parity (margin reuse, position-limit divergence,"
    echo "      OnSignal/OnTick logic mismatch)."
    echo "$SEP"
    exit 2
fi
if [[ "$diff_exit" == "1" ]]; then
    echo "  >>> VERDICT: THRESHOLD — Layer 3 FAIL"
    echo "      pnl_usd drift exceeded ${THRESHOLD_PCT}% on ≥1 matched pair."
    echo "      Operator MUST NOT promote to STAGE_1 / real-money. Investigate"
    echo "      fee/slip parity, fill-price divergence, partial-take logic."
    echo "$SEP"
    exit 1
fi
# Anything outside the documented {0,1,2,3,4} taxonomy = unexpected.
# Common causes: SIGSEGV (139), SIGKILL by OOM (137), signal contract
# additions in a future journal_diff version we don't yet recognise.
# All route to INPUT_ERROR — explicit refusal to PASS on an unknown
# verdict. Same silent-on-corrupt-input pattern applied to subprocess
# exit codes; family lock with layer2_smoke fix from commit 7faa7fe.
if [[ "$diff_exit" != "0" ]]; then
    echo "  >>> VERDICT: INPUT_ERROR — journal_diff exited unexpectedly (${diff_exit})"
    echo "      Not in the documented 0/1/2/3/4 taxonomy. Common causes:"
    echo "      SIGSEGV (139), OOM-killed (137), signal contract addition."
    echo "      Inspect diff binary output above. Layer 3 STOP — investigate"
    echo "      before re-running (do NOT promote to STAGE_1)."
    echo "$SEP"
    exit 3
fi
if [[ "$duration_ok" == "0" ]] && [[ "$SKIP_MIN_DAYS" != "1" ]]; then
    echo "  >>> VERDICT: INSUFFICIENT_DURATION — Layer 3 NOT YET EVALUABLE"
    echo "      Testnet has been running ${elapsed_days}d but the locked rule"
    echo "      requires ≥${MIN_DAYS}d. Keep the dual-runner active and re-run"
    echo "      this script after $((MIN_DAYS - elapsed_days)) more day(s)."
    echo "      Pass --skip-min-days for an explicit dry-run before the gate."
    echo "$SEP"
    exit 4
fi
if [[ "$duration_ok" == "0" ]] && [[ "$SKIP_MIN_DAYS" == "1" ]]; then
    echo "  >>> VERDICT: PASS (DRY-RUN) — parity criterion met but window"
    echo "      below ${MIN_DAYS}d (${elapsed_days}d). Re-run without"
    echo "      --skip-min-days after the locked window elapses for the"
    echo "      decision-grade verdict."
else
    echo "  >>> VERDICT: PASS — Layer 3 criterion met"
    echo "      Parity within threshold AND testnet window ≥ ${MIN_DAYS}d."
    echo "      Operator may proceed to STAGE_1 promotion per the staged"
    echo "      protocol (results/real_money_protocol_decision_rule_2026-05-08.md)."
fi
echo "$SEP"
exit 0
