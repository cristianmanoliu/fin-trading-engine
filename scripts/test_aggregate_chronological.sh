#!/usr/bin/env bash
# test_aggregate_chronological.sh — pins the chronological-min/max contract
# for the awk aggregate functions in forward_paper_status.sh and
# paper_live_compare.sh.
#
# Bug shape (2026-05-19, fixed in 7-line commit): both scripts iterate
# input files in alphabetic order (`cat "${files[@]}"`) and the awk
# block assigned first_ts = first ts seen, last_ts = last ts seen.
# That makes first_ts/last_ts depend on which file sorted first/last
# alphabetically, NOT on the actual chronological min/max across files.
# Live cohort symptom: First trade 2026-05-13 (1000SHIBUSDT's first close)
# instead of 2026-05-08 (XLMUSDT's first close). Off by 5 days, which
# propagated into the promotion countdown's days-elapsed and observed-
# rate calculation, mis-reporting earliest STAGE_1 by ~1 month.
#
# Fix: chronological tracking with `first_ts="" || $2 < first_ts` and
# `last_ts="" || $2 > last_ts`. Lex comparison is safe because the
# engine writes Go time.RFC3339 = "YYYY-MM-DDTHH:MM:SSZ" (fixed-width
# UTC, 20 chars), which sorts lex-equal-to-chronological.
#
# This test fixture is the regression pin: build 3 journals where the
# alphabetically-first file has the chronologically-LAST close, and the
# alphabetically-last file has the chronologically-FIRST close. If the
# bug returns, the assertions fail loudly.
#
# Run:
#   bash scripts/test_aggregate_chronological.sh
set -uo pipefail

SCRIPT_DIR="$(cd "$(dirname "${BASH_SOURCE[0]}")" && pwd)"
FPS="${SCRIPT_DIR}/forward_paper_status.sh"
PLC="${SCRIPT_DIR}/paper_live_compare.sh"

if [[ ! -f "$FPS" ]] || [[ ! -f "$PLC" ]]; then
    echo "missing required scripts" >&2
    exit 1
fi

PASS=0
FAIL=0
FAIL_LABELS=()

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
        echo "    actual (first 800 chars): ${haystack:0:800}"
        FAIL=$((FAIL + 1))
        FAIL_LABELS+=("$label")
    fi
}

# ── Build fixture: 3 symbols where alphabetic order ≠ chronological order
#   AAA-* (alphabetic first): first close 2026-04-15, last close 2026-04-20
#   MMM-* (alphabetic middle): first close 2026-04-10, last close 2026-04-25
#   ZZZ-* (alphabetic last): first close 2026-04-05, last close 2026-04-12
# Chronological first across cohort: 2026-04-05 (ZZZ)
# Chronological last across cohort:  2026-04-25 (MMM)
# Buggy iteration-order behaviour would report first=2026-04-15 (AAA's first)
# and last=2026-04-12 (ZZZ's last) — distinct from the correct values.
TMPDIR_ROOT=$(mktemp -d)
trap 'rm -rf "$TMPDIR_ROOT"' EXIT

JDIR="${TMPDIR_ROOT}/journal"
mkdir -p "$JDIR"
mkdir -p "${JDIR}/shadow/test_shadow"

# AAA fixture — alphabetically first, but mid-range timestamps
cat > "${JDIR}/AAA-2026-04.jsonl" <<'EOF'
{"event":"open","symbol":"AAA","ts":"2026-04-15T10:00:00Z","side":"SHORT","entry":100,"stop":102,"target":94}
{"event":"close","symbol":"AAA","ts":"2026-04-15T22:00:00Z","side":"SHORT","entry":100,"exit":94,"outcome":"TARGET","pnl_usd":600,"fee_usd":10,"slip_usd":0,"notional_usd":10000}
{"event":"open","symbol":"AAA","ts":"2026-04-20T10:00:00Z","side":"SHORT","entry":105,"stop":107,"target":99}
{"event":"close","symbol":"AAA","ts":"2026-04-20T22:00:00Z","side":"SHORT","entry":105,"exit":107,"outcome":"STOP","pnl_usd":-200,"fee_usd":10,"slip_usd":5,"notional_usd":10500}
EOF

# MMM fixture — alphabetically middle, contains the chronologically-LATEST close
cat > "${JDIR}/MMM-2026-04.jsonl" <<'EOF'
{"event":"open","symbol":"MMM","ts":"2026-04-10T10:00:00Z","side":"SHORT","entry":50,"stop":51,"target":47}
{"event":"close","symbol":"MMM","ts":"2026-04-10T22:00:00Z","side":"SHORT","entry":50,"exit":51,"outcome":"STOP","pnl_usd":-100,"fee_usd":5,"slip_usd":3,"notional_usd":5000}
{"event":"open","symbol":"MMM","ts":"2026-04-25T10:00:00Z","side":"SHORT","entry":55,"stop":56,"target":52}
{"event":"close","symbol":"MMM","ts":"2026-04-25T22:00:00Z","side":"SHORT","entry":55,"exit":52,"outcome":"TARGET","pnl_usd":300,"fee_usd":5,"slip_usd":0,"notional_usd":5500}
EOF

# ZZZ fixture — alphabetically last, contains the chronologically-EARLIEST close
cat > "${JDIR}/ZZZ-2026-04.jsonl" <<'EOF'
{"event":"open","symbol":"ZZZ","ts":"2026-04-05T10:00:00Z","side":"SHORT","entry":200,"stop":204,"target":188}
{"event":"close","symbol":"ZZZ","ts":"2026-04-05T22:00:00Z","side":"SHORT","entry":200,"exit":188,"outcome":"TARGET","pnl_usd":1200,"fee_usd":20,"slip_usd":0,"notional_usd":20000}
{"event":"open","symbol":"ZZZ","ts":"2026-04-12T10:00:00Z","side":"SHORT","entry":210,"stop":214,"target":198}
{"event":"close","symbol":"ZZZ","ts":"2026-04-12T22:00:00Z","side":"SHORT","entry":210,"exit":214,"outcome":"STOP","pnl_usd":-400,"fee_usd":20,"slip_usd":10,"notional_usd":21000}
EOF

# Shadow fixture (same shape, different label) — verifies the fix applies to all cohorts
cp "${JDIR}/AAA-2026-04.jsonl" "${JDIR}/shadow/test_shadow/AAA-2026-04.jsonl"
cp "${JDIR}/MMM-2026-04.jsonl" "${JDIR}/shadow/test_shadow/MMM-2026-04.jsonl"
cp "${JDIR}/ZZZ-2026-04.jsonl" "${JDIR}/shadow/test_shadow/ZZZ-2026-04.jsonl"

# ── T1: forward_paper_status.sh — live cohort first/last must be chronological
echo
echo "── T1: forward_paper_status.sh live cohort ──"

set +e
out=$(JOURNAL_DIR="$JDIR" bash "$FPS" local 2>&1)
rc=$?
set -e

assert_contains "T1 exit code (=0)" "$rc" "0"
# Pin chronological correctness — the buggy version would print
# "First trade: 2026-04-15" (AAA's first) and "Last trade: 2026-04-12" (ZZZ's last)
assert_contains "T1 live First trade chronological"  "$out" "First trade:         2026-04-05T22:00:00"
assert_contains "T1 live Last trade chronological"   "$out" "Last trade:          2026-04-25T22:00:00"

# Defense-in-depth: explicit anti-assertions for the buggy values
assert_not_contains "T1 NOT first=2026-04-15 (AAA bug-shape)" "$out" "First trade:         2026-04-15"
assert_not_contains "T1 NOT last=2026-04-12 (ZZZ bug-shape)"  "$out" "Last trade:          2026-04-12"

# ── T2: forward_paper_status.sh — shadow cohort same fix
echo
echo "── T2: forward_paper_status.sh shadow cohort ──"

# The output prints first/last per cohort. Shadow has identical data so same
# chronological min/max. We rely on the shadow block appearing somewhere with
# the correct dates — counting "2026-04-05" + "2026-04-25" total occurrences
# is sufficient (must be ≥2 each = once per cohort).
n_first=$(echo "$out" | grep -c "First trade:         2026-04-05T22:00:00" || true)
n_last=$(echo "$out" | grep -c "Last trade:          2026-04-25T22:00:00" || true)

if [[ "$n_first" -ge 2 ]]; then
    echo "  ✓ T2 chronological first appears ≥2× (live + shadow)"
    PASS=$((PASS + 1))
else
    echo "  ✗ T2 chronological first count: $n_first (expected ≥2)"
    FAIL=$((FAIL + 1))
    FAIL_LABELS+=("T2 chronological first count")
fi

if [[ "$n_last" -ge 2 ]]; then
    echo "  ✓ T2 chronological last appears ≥2× (live + shadow)"
    PASS=$((PASS + 1))
else
    echo "  ✗ T2 chronological last count: $n_last (expected ≥2)"
    FAIL=$((FAIL + 1))
    FAIL_LABELS+=("T2 chronological last count")
fi

# ── T3: paper_live_compare.sh — same fix applied
echo
echo "── T3: paper_live_compare.sh ──"

set +e
out3=$(JOURNAL_DIR="$JDIR" bash "$PLC" local 2>&1)
rc3=$?
set -e

assert_contains "T3 exit code (=0)" "$rc3" "0"
# paper_live_compare prints "First trade" + "Last trade" too
assert_contains "T3 live First trade chronological"  "$out3" "2026-04-05T22:00:00"
assert_contains "T3 live Last trade chronological"   "$out3" "2026-04-25T22:00:00"
assert_not_contains "T3 NOT 2026-04-15 (AAA bug-shape)" "$out3" "First trade:         2026-04-15"

# ── Summary
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
