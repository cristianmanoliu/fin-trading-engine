#!/usr/bin/env bash
# test_overfit_reduce_journals.sh — regression test for the reducer's
# malformed-line handling. Locks the 2026-05-30 fail-open fix: corrupt JSON
# lines must be COUNTED + WARNED, not silently dropped (silent-on-corrupt-input
# — they would otherwise understate the matrix feeding the PBO/DSR overfit
# verdict with zero signal). See scripts/overfit_reduce_journals.py.
#
# Run: bash scripts/test_overfit_reduce_journals.sh   (exit 0 = pass)
set -uo pipefail
cd "$(dirname "$0")/.."
FAIL=0
pass() { echo "  ok   $*"; }
fail() { echo "  FAIL $*"; FAIL=$((FAIL + 1)); }

echo "overfit_reduce_journals malformed-line contract"

tmp=$(mktemp -d)
trap 'rm -rf "$tmp"' EXIT
mkdir -p "$tmp/jr/labelA"
printf '%s\n' \
  '{"event":"open","ts":"2021-03-15T04:00:00Z","side":"short"}' \
  '{truncated corrupt line' \
  '{"event":"close","pnl_usd":100.0,"outcome":"TARGET"}' \
  > "$tmp/jr/labelA/SYM-2021-03.jsonl"
printf 'label,extra_flags\nlabelA,--foo\n' > "$tmp/manifest.csv"

err=$(python3 scripts/overfit_reduce_journals.py "$tmp/jr" "$tmp/manifest.csv" "$tmp/out.csv" 2>&1 >/dev/null)
rc=$?

# 1. valid trade present → reducer must NOT trip the zero-close floor; exits 0.
if [[ "$rc" -eq 0 ]]; then pass "reducer exits 0 with a valid trade present"; else fail "reducer exit=$rc (expected 0)"; fi
# 2. the corrupt line is SURFACED on stderr, not silently dropped.
if echo "$err" | grep -q "malformed JSON line"; then pass "malformed line warned on stderr"; else fail "no malformed-line warning — silent-drop regressed"; fi
# 3. the valid trade still lands in the matrix.
if grep -q "2021-03,100.00" "$tmp/out.csv" 2>/dev/null; then pass "valid trade recorded (2021-03=100.00)"; else fail "valid trade missing from matrix"; fi

echo ""
if [[ "$FAIL" -eq 0 ]]; then echo "PASS — reducer surfaces corrupt lines"; exit 0; else echo "FAIL — $FAIL"; exit 1; fi
