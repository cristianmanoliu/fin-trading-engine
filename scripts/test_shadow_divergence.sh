#!/usr/bin/env bash
# test_shadow_divergence.sh — regression suite for scripts/shadow_divergence.sh.
#
# Locks the PENDING classifier landed on 2026-05-20 (smoke-test of upcoming
# FILUSDT max-hold divergence event ~2026-05-25): A force-closes at 336h
# while B keeps holding past 336h MUST be classified as PENDING, not ORPHAN.
# The original implementation reported ORPHAN here and the daily_digest
# oneline said "identical (no trade >336h yet)" — exactly the bug we're
# locking against.
#
# Strategy: build a local JOURNAL_DIR with hand-rolled fixtures, run the
# helper in `local` mode (no SSH). Same idiom as test_layer3_pull.sh.
#
# Run:
#   bash scripts/test_shadow_divergence.sh
set -uo pipefail

# Skip on bash 3.2 (macOS /bin/bash) — CI runs Linux bash 5 for full coverage.
if [[ "${BASH_VERSINFO[0]:-0}" -lt 4 ]]; then
    echo "test_shadow_divergence.sh — skipped (bash ${BASH_VERSION} < 4; CI runs full coverage)"
    exit 0
fi

SCRIPT_DIR="$(cd "$(dirname "${BASH_SOURCE[0]}")" && pwd)"
HELPER="${SCRIPT_DIR}/shadow_divergence.sh"

if [[ ! -f "$HELPER" ]]; then
    echo "test_shadow_divergence.sh — skipped (helper not present)" >&2
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

# Per-test sandbox setup.
new_sandbox() {
    local sb
    sb=$(mktemp -d) || { echo "new_sandbox: mktemp -d failed" >&2; exit 1; }
    mkdir -p "$sb/journal/shadow/alt5-15-336" "$sb/journal/shadow/alt5-15-504"
    echo "$sb"
}

cleanup_sandbox() {
    local sb="${1:-}"
    [[ -n "$sb" && -d "$sb" ]] && rm -rf "$sb"
}

write_journal() {
    local file="$1"; shift
    printf '%s\n' "$@" > "$file"
}

echo "test_shadow_divergence.sh — running..."
echo

# ─────────────────────────────────────────────────────────────
# Test 1: FILUSDT-shape PENDING (A closed at max-hold, B still open)
# This is the bug the 2026-05-20 smoke-test caught.
# ─────────────────────────────────────────────────────────────
echo "Test 1: PENDING classification (A force-closed, B still holding)"
sb=$(new_sandbox)
write_journal "$sb/journal/shadow/alt5-15-336/FILUSDT-2026-05.jsonl" \
    '{"event":"open","symbol":"FILUSDT","ts":"2026-05-11T00:00:00Z","side":"SHORT","entry":1.146,"stop":1.19,"target":0.87}' \
    '{"event":"close","symbol":"FILUSDT","ts":"2026-05-25T00:00:00Z","side":"SHORT","entry":1.146,"exit":1.18,"pnl_usd":-296.68,"outcome":"max_hold"}'
write_journal "$sb/journal/shadow/alt5-15-504/FILUSDT-2026-05.jsonl" \
    '{"event":"open","symbol":"FILUSDT","ts":"2026-05-11T00:00:00Z","side":"SHORT","entry":1.146,"stop":1.19,"target":0.87}'

out=$(JOURNAL_DIR="$sb/journal" bash "$HELPER" local 2>&1)
oneline_out=$(JOURNAL_DIR="$sb/journal" ONELINE=1 bash "$HELPER" local 2>&1)

assert_contains "1.1 full report shows Pending (336h only) = 1" "$out" "Pending (336h only)                1"
assert_contains "1.2 full report shows FILUSDT in pending detail" "$out" "FILUSDT      SHORT   336h"
assert_not_contains "1.3 full report does NOT misclassify as ORPHAN" "$out" "Orphan (336h only)                 1"
assert_not_contains "1.4 full report does NOT say 'No divergence yet'" "$out" "No divergence yet"
assert_contains "1.5 oneline reports PENDING (not 'identical')" "$oneline_out" "PENDING 1 trades"
assert_not_contains "1.6 oneline does NOT say 'identical'" "$oneline_out" "identical"
cleanup_sandbox "$sb"

# ─────────────────────────────────────────────────────────────
# Test 2: Full DIVERGED (both cohorts closed, different outcomes)
# ─────────────────────────────────────────────────────────────
echo "Test 2: DIVERGED classification (both closed, different close ts)"
sb=$(new_sandbox)
write_journal "$sb/journal/shadow/alt5-15-336/FILUSDT-2026-05.jsonl" \
    '{"event":"open","symbol":"FILUSDT","ts":"2026-05-11T00:00:00Z","side":"SHORT","entry":1.146,"stop":1.19,"target":0.87}' \
    '{"event":"close","symbol":"FILUSDT","ts":"2026-05-25T00:00:00Z","side":"SHORT","entry":1.146,"exit":1.18,"pnl_usd":-296.68,"outcome":"max_hold"}'
write_journal "$sb/journal/shadow/alt5-15-504/FILUSDT-2026-05.jsonl" \
    '{"event":"open","symbol":"FILUSDT","ts":"2026-05-11T00:00:00Z","side":"SHORT","entry":1.146,"stop":1.19,"target":0.87}' \
    '{"event":"close","symbol":"FILUSDT","ts":"2026-06-01T00:00:00Z","side":"SHORT","entry":1.146,"exit":0.95,"pnl_usd":1710.30,"outcome":"max_hold"}'

out=$(JOURNAL_DIR="$sb/journal" bash "$HELPER" local 2>&1)
oneline_out=$(JOURNAL_DIR="$sb/journal" ONELINE=1 bash "$HELPER" local 2>&1)

assert_contains "2.1 full report shows Diverged = 1" "$out" "Diverged (both closed)             1"
assert_contains "2.2 divergence detail row shows both outcomes" "$out" "max_hold      \$-297  max_hold     \$+1710"
assert_contains "2.3 oneline reports DIVERGED" "$oneline_out" "DIVERGED 1 trades"
assert_contains "2.4 oneline shows 504h winning" "$oneline_out" "504h wins 1/1"
cleanup_sandbox "$sb"

# ─────────────────────────────────────────────────────────────
# Test 3: MATCHED (both closed identically — current production state)
# ─────────────────────────────────────────────────────────────
echo "Test 3: MATCHED classification (no divergence)"
sb=$(new_sandbox)
write_journal "$sb/journal/shadow/alt5-15-336/IMXUSDT-2026-05.jsonl" \
    '{"event":"open","symbol":"IMXUSDT","ts":"2026-05-16T00:00:00Z","side":"SHORT","entry":0.1851,"stop":0.19,"target":0.15}' \
    '{"event":"close","symbol":"IMXUSDT","ts":"2026-05-19T12:00:00Z","side":"SHORT","entry":0.1851,"exit":0.17,"pnl_usd":815.78,"outcome":"target"}'
write_journal "$sb/journal/shadow/alt5-15-504/IMXUSDT-2026-05.jsonl" \
    '{"event":"open","symbol":"IMXUSDT","ts":"2026-05-16T00:00:00Z","side":"SHORT","entry":0.1851,"stop":0.19,"target":0.15}' \
    '{"event":"close","symbol":"IMXUSDT","ts":"2026-05-19T12:00:00Z","side":"SHORT","entry":0.1851,"exit":0.17,"pnl_usd":815.78,"outcome":"target"}'

out=$(JOURNAL_DIR="$sb/journal" bash "$HELPER" local 2>&1)
oneline_out=$(JOURNAL_DIR="$sb/journal" ONELINE=1 bash "$HELPER" local 2>&1)

assert_contains "3.1 full report shows Matched = 1" "$out" "Matched (identical)                1"
assert_contains "3.2 oneline says identical (no trade >336h yet)" "$oneline_out" "identical (no trade >336h yet)"
cleanup_sandbox "$sb"

# ─────────────────────────────────────────────────────────────
# Test 4: Mixed — 1 matched + 1 pending. Counts must be additive.
# ─────────────────────────────────────────────────────────────
echo "Test 4: Mixed matched + pending (counts additive)"
sb=$(new_sandbox)
write_journal "$sb/journal/shadow/alt5-15-336/IMXUSDT-2026-05.jsonl" \
    '{"event":"open","symbol":"IMXUSDT","ts":"2026-05-16T00:00:00Z","side":"SHORT","entry":0.1851,"stop":0.19,"target":0.15}' \
    '{"event":"close","symbol":"IMXUSDT","ts":"2026-05-19T12:00:00Z","side":"SHORT","entry":0.1851,"exit":0.17,"pnl_usd":815.78,"outcome":"target"}'
write_journal "$sb/journal/shadow/alt5-15-504/IMXUSDT-2026-05.jsonl" \
    '{"event":"open","symbol":"IMXUSDT","ts":"2026-05-16T00:00:00Z","side":"SHORT","entry":0.1851,"stop":0.19,"target":0.15}' \
    '{"event":"close","symbol":"IMXUSDT","ts":"2026-05-19T12:00:00Z","side":"SHORT","entry":0.1851,"exit":0.17,"pnl_usd":815.78,"outcome":"target"}'
write_journal "$sb/journal/shadow/alt5-15-336/FILUSDT-2026-05.jsonl" \
    '{"event":"open","symbol":"FILUSDT","ts":"2026-05-11T00:00:00Z","side":"SHORT","entry":1.146,"stop":1.19,"target":0.87}' \
    '{"event":"close","symbol":"FILUSDT","ts":"2026-05-25T00:00:00Z","side":"SHORT","entry":1.146,"exit":1.18,"pnl_usd":-296.68,"outcome":"max_hold"}'
write_journal "$sb/journal/shadow/alt5-15-504/FILUSDT-2026-05.jsonl" \
    '{"event":"open","symbol":"FILUSDT","ts":"2026-05-11T00:00:00Z","side":"SHORT","entry":1.146,"stop":1.19,"target":0.87}'

out=$(JOURNAL_DIR="$sb/journal" bash "$HELPER" local 2>&1)

assert_contains "4.1 Matched = 1" "$out" "Matched (identical)                1"
assert_contains "4.2 Pending (336h only) = 1" "$out" "Pending (336h only)                1"
# Closed trades count for A = matched + pending_a + diverged + orphan_a = 1 + 1 + 0 + 0 = 2
assert_contains "4.3 Closed-trades count for A includes pending (= 2)" "$out" "Closed trades                      2          1"
cleanup_sandbox "$sb"

# ─────────────────────────────────────────────────────────────
# Test 5: True ORPHAN — A closed but B never opened
# (journal-replay bug signal; must NOT downgrade to PENDING)
# ─────────────────────────────────────────────────────────────
echo "Test 5: True ORPHAN preserved (B has no matching open)"
sb=$(new_sandbox)
write_journal "$sb/journal/shadow/alt5-15-336/SOLUSDT-2026-05.jsonl" \
    '{"event":"open","symbol":"SOLUSDT","ts":"2026-05-12T00:00:00Z","side":"SHORT","entry":150.0,"stop":160.0,"target":100.0}' \
    '{"event":"close","symbol":"SOLUSDT","ts":"2026-05-13T00:00:00Z","side":"SHORT","entry":150.0,"exit":160.0,"pnl_usd":-500,"outcome":"stop"}'
# No SOLUSDT in alt5-15-504 at all — simulate the orphan condition.
# But need some content in alt5-15-504 dir to avoid "no journal data" early-exit.
write_journal "$sb/journal/shadow/alt5-15-504/IMXUSDT-2026-05.jsonl" \
    '{"event":"open","symbol":"IMXUSDT","ts":"2026-05-16T00:00:00Z","side":"SHORT","entry":0.1851,"stop":0.19,"target":0.15}'

out=$(JOURNAL_DIR="$sb/journal" bash "$HELPER" local 2>&1)

assert_contains "5.1 ORPHAN_A correctly reported (not PENDING)" "$out" "Orphan (336h only)                 1"
assert_not_contains "5.2 not misclassified as PENDING" "$out" "Pending (336h only)                1"
cleanup_sandbox "$sb"

# ─────────────────────────────────────────────────────────────
# Summary
# ─────────────────────────────────────────────────────────────
echo
TOTAL=$((PASS + FAIL))
echo "test_shadow_divergence.sh — $PASS/$TOTAL passed"
if [[ $FAIL -gt 0 ]]; then
    echo "FAILED tests:"
    for label in "${FAIL_LABELS[@]}"; do
        echo "  - $label"
    done
    exit 1
fi
exit 0
