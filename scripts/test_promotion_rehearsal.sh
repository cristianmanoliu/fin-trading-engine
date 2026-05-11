#!/usr/bin/env bash
# test_promotion_rehearsal.sh — assertion-based tests for promotion_rehearsal.sh.
#
# Sources the script under guarded sourcing (BASH_SOURCE != $0 check prevents
# main flow), then drives helpers directly with known inputs. Covers:
#
#   - Per-phase tier translators (resolution_tier / promote_check_tier /
#     kill_check_tier) — each maps every documented exit code correctly,
#     including the catch-all for unknown codes
#   - aggregate_tiers precedence: BLOCKED > INPUT_ERR > WAITING > READY,
#     N/A non-influential
#   - aggregate_label translation
#   - Layer 2 readiness inspector (attestation-marker pattern)
#   - Layer 3 readiness inspector (N/A vs WAITING vs READY)
#   - CLI parse: --quiet / --phase / --local + positional VPS conflict /
#     unknown flag / invalid --phase
#
# Run:
#   bash scripts/test_promotion_rehearsal.sh
set -euo pipefail

SCRIPT_DIR="$(cd "$(dirname "${BASH_SOURCE[0]}")" && pwd)"
REHEARSAL="${SCRIPT_DIR}/promotion_rehearsal.sh"

# Source helpers (guard prevents main flow).
# shellcheck source=promotion_rehearsal.sh disable=SC1091
source "$REHEARSAL"

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
        FAIL_LINES+=("  ✗ $name: needle [$needle] not in haystack")
    fi
}

# ── resolution_tier ─────────────────────────────────────────────────────────
echo
echo "── resolution_tier (LIMBO exit → rehearsal tier) ──"
assert_eq "$(resolution_tier 0)" "WAITING"   "exit 0 CONTINUE → WAITING (still accumulating)"
assert_eq "$(resolution_tier 1)" "READY"     "exit 1 PROMOTE → READY"
assert_eq "$(resolution_tier 2)" "BLOCKED"   "exit 2 WATCH → BLOCKED (soft signals)"
assert_eq "$(resolution_tier 3)" "BLOCKED"   "exit 3 OPERATOR_REVIEW → BLOCKED"
assert_eq "$(resolution_tier 4)" "BLOCKED"   "exit 4 KILL → BLOCKED"
assert_eq "$(resolution_tier 5)" "INPUT_ERR" "exit 5 INPUT_ERROR → INPUT_ERR"
assert_eq "$(resolution_tier 99)" "INPUT_ERR" "exit 99 unknown → INPUT_ERR (catch-all)"

# ── promote_check_tier ──────────────────────────────────────────────────────
echo
echo "── promote_check_tier (stage_promotion_check exit → rehearsal tier) ──"
assert_eq "$(promote_check_tier 0)" "READY"     "exit 0 PROMOTE → READY"
assert_eq "$(promote_check_tier 1)" "BLOCKED"   "exit 1 BLOCKED → BLOCKED"
assert_eq "$(promote_check_tier 2)" "WAITING"   "exit 2 WAITING → WAITING"
assert_eq "$(promote_check_tier 3)" "INPUT_ERR" "exit 3 ERROR → INPUT_ERR"
assert_eq "$(promote_check_tier 4)" "BLOCKED"   "exit 4 PROMOTE-CANDIDATE → BLOCKED (deferred)"
assert_eq "$(promote_check_tier 7)" "INPUT_ERR" "exit 7 unknown → INPUT_ERR"

# ── kill_check_tier ─────────────────────────────────────────────────────────
echo
echo "── kill_check_tier (kill_protocol_check exit → rehearsal tier) ──"
assert_eq "$(kill_check_tier 0)" "READY"     "exit 0 CONTINUE → READY"
assert_eq "$(kill_check_tier 1)" "BLOCKED"   "exit 1 KILL → BLOCKED"
assert_eq "$(kill_check_tier 2)" "WAITING"   "exit 2 WAITING → WAITING"
assert_eq "$(kill_check_tier 3)" "INPUT_ERR" "exit 3 ERROR → INPUT_ERR"
assert_eq "$(kill_check_tier 4)" "BLOCKED"   "exit 4 OPERATOR-VERIFY → BLOCKED"

# ── aggregate_tiers precedence ──────────────────────────────────────────────
echo
echo "── aggregate_tiers precedence ──"
assert_eq "$(aggregate_tiers READY READY READY READY READY)" "0" "all READY → 0"
assert_eq "$(aggregate_tiers READY READY READY N/A N/A)"     "0" "READY + N/A → 0 (N/A non-influential)"
assert_eq "$(aggregate_tiers WAITING READY READY READY READY)" "2" "single WAITING → 2"
assert_eq "$(aggregate_tiers READY WAITING WAITING N/A N/A)" "2" "multiple WAITING + N/A → 2"
assert_eq "$(aggregate_tiers READY READY BLOCKED READY READY)" "1" "single BLOCKED → 1"
assert_eq "$(aggregate_tiers BLOCKED WAITING N/A READY READY)" "1" "BLOCKED beats WAITING (precedence)"
assert_eq "$(aggregate_tiers INPUT_ERR WAITING READY READY READY)" "3" "single INPUT_ERR → 3"
assert_eq "$(aggregate_tiers BLOCKED INPUT_ERR WAITING N/A N/A)" "1" "BLOCKED beats INPUT_ERR"
assert_eq "$(aggregate_tiers READY READY N/A N/A N/A)" "0" "majority N/A but some READY → 0"
assert_eq "$(aggregate_tiers N/A N/A N/A N/A N/A)" "0" "all N/A → 0 (vacuously READY)"
assert_eq "$(aggregate_tiers WEIRD_TIER READY READY READY READY)" "3" "unknown tier → INPUT_ERR"

# ── aggregate_label ─────────────────────────────────────────────────────────
echo
echo "── aggregate_label ──"
assert_eq "$(aggregate_label 0)" "READY"     "0 → READY"
assert_eq "$(aggregate_label 1)" "BLOCKED"   "1 → BLOCKED"
assert_eq "$(aggregate_label 2)" "WAITING"   "2 → WAITING"
assert_eq "$(aggregate_label 3)" "INPUT_ERR" "3 → INPUT_ERR"
assert_eq "$(aggregate_label 9)" "UNKNOWN(9)" "9 → UNKNOWN(9) (defensive)"

# ── layer2_tier ─────────────────────────────────────────────────────────────
echo
echo "── layer2_tier (attestation-marker inspection) ──"

TMPDIR=$(mktemp -d)
trap 'rm -rf "$TMPDIR"' EXIT

# Case: no smoke dir at all → WAITING.
assert_eq "$(layer2_tier "$TMPDIR/nonexistent" 7)" "WAITING" "no smoke dir → WAITING"

# Case: dir exists, no marker → WAITING (smoke logs alone don't attest PASS).
mkdir -p "$TMPDIR/smoke1"
touch "$TMPDIR/smoke1/BTCUSDT_2026-05-11.log"  # a smoke log but no marker
assert_eq "$(layer2_tier "$TMPDIR/smoke1" 7)" "WAITING" "smoke logs but no marker → WAITING (not READY)"

# Case: fresh marker → READY.
mkdir -p "$TMPDIR/smoke2"
touch "$TMPDIR/smoke2/.last_pass"  # default mtime = now
assert_eq "$(layer2_tier "$TMPDIR/smoke2" 7)" "READY" "fresh marker → READY"

# Case: stale marker (older than window) → WAITING.
mkdir -p "$TMPDIR/smoke3"
touch "$TMPDIR/smoke3/.last_pass"
# Set mtime to 10 days ago. Use POSIX touch -t with date arithmetic
# that works on both BSD (macOS) + GNU.
old_date=$(date -u -v-10d '+%Y%m%d%H%M' 2>/dev/null || date -u -d '10 days ago' '+%Y%m%d%H%M')
touch -t "$old_date" "$TMPDIR/smoke3/.last_pass"
assert_eq "$(layer2_tier "$TMPDIR/smoke3" 7)" "WAITING" "10d-old marker with 7d window → WAITING (stale)"
assert_eq "$(layer2_tier "$TMPDIR/smoke3" 14)" "READY" "10d-old marker with 14d window → READY (still fresh)"

# ── layer3_tier ─────────────────────────────────────────────────────────────
echo
echo "── layer3_tier (testnet dir + attestation) ──"

# Case: no testnet dir → N/A (Layer 2 hasn't been done; not your turn).
assert_eq "$(layer3_tier "$TMPDIR/no-testnet")" "N/A" "no testnet dir → N/A (Layer 2 first)"

# Case: testnet dir exists but is empty → N/A.
mkdir -p "$TMPDIR/testnet_empty"
assert_eq "$(layer3_tier "$TMPDIR/testnet_empty")" "N/A" "empty testnet dir → N/A"

# Case: testnet dir has jsonl but no marker → WAITING.
mkdir -p "$TMPDIR/testnet_data"
touch "$TMPDIR/testnet_data/BTCUSDT-2026-05.jsonl"
assert_eq "$(layer3_tier "$TMPDIR/testnet_data")" "WAITING" "testnet jsonl present, no marker → WAITING"

# Case: fresh attestation marker → READY.
mkdir -p "$TMPDIR/testnet_attest"
touch "$TMPDIR/testnet_attest/.last_layer3_pass"
assert_eq "$(layer3_tier "$TMPDIR/testnet_attest")" "READY" "fresh layer3 marker → READY"

# Case: stale marker → WAITING.
mkdir -p "$TMPDIR/testnet_stale"
touch "$TMPDIR/testnet_stale/.last_layer3_pass"
touch -t "$old_date" "$TMPDIR/testnet_stale/.last_layer3_pass"
assert_eq "$(layer3_tier "$TMPDIR/testnet_stale")" "WAITING" "10d-old layer3 marker → WAITING (stale)"

# ── stat fallback (regression for the BSD-vs-GNU stat-order bug) ───────────
# Pre-fix, layer2_tier/layer3_tier used `stat -f %m` (BSD) FIRST then GNU
# `stat -c %Y` as fallback. On Linux CI, `stat -f` is interpreted as
# "display filesystem status" — GNU stat accepts the invocation and prints
# the format string "%m" LITERALLY instead of erroring, so the fallback
# never runs. Non-numeric mtime → bash arithmetic error under set -e →
# function aborts mid-execution → empty stdout → tests see got=[].
#
# Fix landed: (1) reverse stat order so GNU is primary, BSD is fallback;
# (2) add defensive `[[ "$mtime" =~ ^[0-9]+$ ]] || mtime=0` so even if
# both stat invocations produce garbage, the function returns a valid
# tier (WAITING, the safe stale default).
#
# This case simulates the garbage-mtime path by overriding `stat` with a
# function that always prints a non-numeric string. layer2_tier should
# still return a valid tier (WAITING), NOT empty stdout.
echo
echo "── stat fallback regression (pre-fix would return empty under set -e) ──"
mkdir -p "$TMPDIR/smoke_stat_garbage"
touch "$TMPDIR/smoke_stat_garbage/.last_pass"
# Override `stat` for this assertion's subshell. The function under test
# runs inside command substitution → subshell → inherits this override.
# (Then the override goes out of scope, leaving the rest of the test
# suite unaffected.)
result=$(stat() { echo "%m"; return 0; }; export -f stat; layer2_tier "$TMPDIR/smoke_stat_garbage" 7)
assert_eq "$result" "WAITING" \
    "non-numeric mtime → WAITING (defensive fallback, never empty)"

# ── CLI parsing ─────────────────────────────────────────────────────────────
echo
echo "── CLI flag parsing ──"

# --help exits 0.
help_output=$(bash "$REHEARSAL" --help 2>&1)
help_rc=$?
assert_eq "$help_rc" "0" "--help exits 0"
# Header docstring is what --help prints (lines 2-60 with leading '# ' stripped).
# Assert on text actually in that range, not in the verbose-output banner.
assert_contains "$help_output" "Composes the audited" "--help shows docstring intro"

# Unknown flag → exit 3.
set +e
bash "$REHEARSAL" --nonsense 2>/dev/null
rc=$?
set -e
assert_eq "$rc" "3" "unknown flag → INPUT_ERR"

# --local + positional VPS → exit 3.
set +e
err=$(bash "$REHEARSAL" --local root@other 2>&1)
rc=$?
set -e
assert_eq "$rc" "3" "--local + positional VPS → INPUT_ERR"
assert_contains "$err" "mutually exclusive" "conflict diagnostic surfaces"

# Invalid --phase (out of range) → exit 3.
set +e
err=$(bash "$REHEARSAL" --phase 9 2>&1)
rc=$?
set -e
assert_eq "$rc" "3" "--phase 9 → INPUT_ERR"
assert_contains "$err" "must be 1-5" "phase range diagnostic surfaces"

# Invalid --phase (non-numeric) → exit 3.
set +e
err=$(bash "$REHEARSAL" --phase abc 2>&1)
rc=$?
set -e
assert_eq "$rc" "3" "--phase abc → INPUT_ERR"

# ── T9 LIVE_ARGS propagation to Phase 1 (resolution.py) ────────────────────
echo
echo "── T9: LIVE_ARGS propagated to forward_paper_resolution invocation ──"
# T9 ssh-target-consistency: pre-fix, run_phase_1 invoked resolution.py
# without --live-source/--vps args, so even with --local the resolution's
# subprocess calls to kill_protocol + stage_promotion still hit production.
# Source-scan asserts LIVE_ARGS now flows into the Phase 1 invocation.
REHEARSAL_SOURCE="$REHEARSAL"
HAS_LIVE_ARGS=$(grep -c 'python3.*RESOLUTION_PY.*LIVE_ARGS' "$REHEARSAL_SOURCE")
if [[ "$HAS_LIVE_ARGS" -ge 1 ]]; then
    echo "  ✓ run_phase_1 passes LIVE_ARGS to resolution.py"
    PASS=$((PASS + 1))
else
    echo "  ✗ run_phase_1 does NOT pass LIVE_ARGS to resolution.py"
    FAIL=$((FAIL + 1))
    FAIL_LINES+=("  ✗ T9.live_args_propagation: run_phase_1 missing LIVE_ARGS pass-through")
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
