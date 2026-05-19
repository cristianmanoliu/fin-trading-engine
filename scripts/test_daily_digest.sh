#!/usr/bin/env bash
# test_daily_digest.sh — regression suite for scripts/daily_digest.sh.
#
# Covers:
#   - live cohort: 3 trades (1 TARGET, 2 STOP), WR 33.3%, PnL +4000
#   - live cohort: 1 open SHORT position (no closes for that symbol)
#   - shadow alt5-15-336: 2 trades (1 TARGET, 1 STOP), PnL +8000
#   - shadow bb20: empty directory → "no trades yet"
#   - empty JOURNAL_DIR → exit 0 + "no journal data"
#   - missing JOURNAL_DIR → exit 1 + "not found"
#
# Run:
#   bash scripts/test_daily_digest.sh
set -uo pipefail

SCRIPT_DIR="$(cd "$(dirname "${BASH_SOURCE[0]}")" && pwd)"
DIGEST="${SCRIPT_DIR}/daily_digest.sh"

if [[ ! -f "$DIGEST" ]]; then
    echo "daily_digest.sh not found at $DIGEST" >&2
    exit 1
fi

# daily_digest.sh uses `declare -A` (associative arrays, bash 4+). On macOS
# the system /bin/bash is 3.2 which crashes the script before any assertion
# can be evaluated. Skip the suite cleanly so safe-push.sh local gauntlet
# matches CI behavior on Linux bash 5+. To run locally on macOS install bash
# via homebrew: `brew install bash && /opt/homebrew/bin/bash scripts/test_daily_digest.sh`.
if [[ "${BASH_VERSINFO[0]:-0}" -lt 4 ]]; then
    echo "test_daily_digest.sh — skipped (bash ${BASH_VERSION} < 4; daily_digest.sh needs bash 4+)"
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
        echo "  ✗ $label: expected output to contain '$needle'"
        echo "    actual (first 600 chars): ${haystack:0:600}"
        FAIL=$((FAIL + 1))
        FAIL_LABELS+=("$label")
    fi
}

# ── Build fixture journals ────────────────────────────────────────────────────
TMPDIR_ROOT=$(mktemp -d)
trap 'rm -rf "$TMPDIR_ROOT"' EXIT

JDIR="${TMPDIR_ROOT}/journal"
mkdir -p "$JDIR"
mkdir -p "${JDIR}/shadow/alt5-15-336"
mkdir -p "${JDIR}/shadow/bb20"

# Snapshot dir override — daily_digest.sh defaults to /var/log/paper-live/digest_snapshots
# which is not writable in test environments (macOS local + CI runner). Without
# this override the digest script's `mkdir -p` at line 58 fails before any test
# assertion can read the output. See discovery 2026-05-19.
export SNAPSHOT_DIR="${TMPDIR_ROOT}/snapshots"

# BTCUSDT live: 3 closed trades — 1 TARGET win (+5000), 2 STOP losses (-500 each)
# Net PnL = +5000 - 500 - 500 = +4000
cat > "${JDIR}/BTCUSDT-2026-05.jsonl" <<'EOF'
{"event":"open","symbol":"BTCUSDT","ts":"2026-05-01T10:00:00Z","side":"SHORT","entry":65000,"stop":66000,"target":59000}
{"event":"close","symbol":"BTCUSDT","ts":"2026-05-02T08:00:00Z","side":"SHORT","entry":65000,"exit":59000,"outcome":"TARGET","pnl_usd":5000,"fee_usd":55,"slip_usd":0,"notional_usd":65000}
{"event":"open","symbol":"BTCUSDT","ts":"2026-05-03T10:00:00Z","side":"SHORT","entry":64000,"stop":65000,"target":58000}
{"event":"close","symbol":"BTCUSDT","ts":"2026-05-04T08:00:00Z","side":"SHORT","entry":64000,"exit":65000,"outcome":"STOP","pnl_usd":-500,"fee_usd":55,"slip_usd":5,"notional_usd":64000}
{"event":"open","symbol":"BTCUSDT","ts":"2026-05-05T10:00:00Z","side":"SHORT","entry":63000,"stop":64000,"target":57000}
{"event":"close","symbol":"BTCUSDT","ts":"2026-05-06T08:00:00Z","side":"SHORT","entry":63000,"exit":64000,"outcome":"STOP","pnl_usd":-500,"fee_usd":55,"slip_usd":5,"notional_usd":63000}
EOF

# ETHUSDT live: 1 open SHORT position (no close events)
cat > "${JDIR}/ETHUSDT-2026-05.jsonl" <<'EOF'
{"event":"open","symbol":"ETHUSDT","ts":"2026-05-10T10:00:00Z","side":"SHORT","entry":3000,"stop":3050,"target":2700}
EOF

# shadow/alt5-15-336/BTCUSDT: 2 trades — 1 TARGET (+9000), 1 STOP (-1000)
# Net PnL = +9000 - 1000 = +8000
cat > "${JDIR}/shadow/alt5-15-336/BTCUSDT-2026-05.jsonl" <<'EOF'
{"event":"open","symbol":"BTCUSDT","ts":"2026-05-01T10:00:00Z","side":"SHORT","entry":65000,"stop":66000,"target":59000}
{"event":"close","symbol":"BTCUSDT","ts":"2026-05-02T08:00:00Z","side":"SHORT","entry":65000,"exit":59000,"outcome":"TARGET","pnl_usd":9000,"fee_usd":55,"slip_usd":0,"notional_usd":65000}
{"event":"open","symbol":"BTCUSDT","ts":"2026-05-03T10:00:00Z","side":"SHORT","entry":64000,"stop":65000,"target":58000}
{"event":"close","symbol":"BTCUSDT","ts":"2026-05-04T08:00:00Z","side":"SHORT","entry":64000,"exit":65000,"outcome":"STOP","pnl_usd":-1000,"fee_usd":55,"slip_usd":5,"notional_usd":64000}
EOF

# shadow/bb20: empty directory (no files — already created above)

# ── T1: main scenario — live + shadows ───────────────────────────────────────
echo
echo "── T1: main scenario (live + shadows) ──"

set +e
out=$(DRY_RUN=1 JOURNAL_DIR="$JDIR" bash "$DIGEST" 2>&1)
rc=$?
set -e

assert_eq "T1 exit code" "$rc" "0"
assert_contains "T1 LIVE header" "$out" "LIVE"
assert_contains "T1 3 trades"    "$out" "3 trades"
assert_contains "T1 WR 33.3%"    "$out" "33.3%"
assert_contains "T1 PnL +4000"   "$out" "+4000"
assert_contains "T1 open SHORT"  "$out" "SHORT"
assert_contains "T1 ETHUSDT"     "$out" "ETHUSDT"

# Shadow alt5-15-336 → ALT5 15 336 (uppercase + dash→space)
assert_contains "T1 ALT5 15 336 header" "$out" "ALT5 15 336"
assert_contains "T1 shadow 2 trades"    "$out" "2 trades"
assert_contains "T1 shadow +8000"       "$out" "+8000"

# BB20 shadow — empty directory → no trades yet
assert_contains "T1 BB20 header"     "$out" "BB20"
assert_contains "T1 BB20 no trades"  "$out" "no trades yet"

# Open position line: ETHUSDT is the open symbol
assert_contains "T1 Open line"   "$out" "Open:"

# Verdict line present
assert_contains "T1 verdict"     "$out" "Verdict: WAITING"

# ── T2: empty journal directory ──────────────────────────────────────────────
echo
echo "── T2: empty JOURNAL_DIR (no *.jsonl files) ──"

EMPTY_JDIR="${TMPDIR_ROOT}/empty_journal"
mkdir -p "$EMPTY_JDIR"

set +e
out2=$(DRY_RUN=1 JOURNAL_DIR="$EMPTY_JDIR" bash "$DIGEST" 2>&1)
rc2=$?
set -e

assert_eq "T2 exit code" "$rc2" "0"
assert_contains "T2 no journal data" "$out2" "no journal data"

# ── T3: missing JOURNAL_DIR ──────────────────────────────────────────────────
echo
echo "── T3: missing JOURNAL_DIR → exit 1 ──"

set +e
out3=$(DRY_RUN=1 JOURNAL_DIR="/nonexistent/path/journal" bash "$DIGEST" 2>&1)
rc3=$?
set -e

assert_eq "T3 exit code" "$rc3" "1"
assert_contains "T3 not found message" "$out3" "not found"

# ── T4: live-only open position (no closes at all) ───────────────────────────
echo
echo "── T4: live cohort with only open events (no closes) ──"

OPENONLY_JDIR="${TMPDIR_ROOT}/openonly_journal"
mkdir -p "$OPENONLY_JDIR"
cat > "${OPENONLY_JDIR}/SOLUSDT-2026-05.jsonl" <<'EOF'
{"event":"open","symbol":"SOLUSDT","ts":"2026-05-10T10:00:00Z","side":"LONG","entry":150,"stop":145,"target":180}
EOF

set +e
out4=$(DRY_RUN=1 JOURNAL_DIR="$OPENONLY_JDIR" bash "$DIGEST" 2>&1)
rc4=$?
set -e

assert_eq "T4 exit code" "$rc4" "0"
assert_contains "T4 LIVE header" "$out4" "LIVE"
assert_contains "T4 open LONG"   "$out4" "LONG"
# No trades closed → shows 0 trades closed or open-only line
assert_contains "T4 0 trades closed" "$out4" "0 trades"

# ── T5: PARTIAL outcomes are excluded from trade count ───────────────────────
echo
echo "── T5: PARTIAL outcome excluded from trade count ──"

PARTIAL_JDIR="${TMPDIR_ROOT}/partial_journal"
mkdir -p "$PARTIAL_JDIR"
cat > "${PARTIAL_JDIR}/BNBUSDT-2026-05.jsonl" <<'EOF'
{"event":"open","symbol":"BNBUSDT","ts":"2026-05-01T10:00:00Z","side":"SHORT","entry":600,"stop":620,"target":480}
{"event":"close","symbol":"BNBUSDT","ts":"2026-05-02T08:00:00Z","side":"SHORT","outcome":"PARTIAL","pnl_usd":500}
{"event":"close","symbol":"BNBUSDT","ts":"2026-05-03T08:00:00Z","side":"SHORT","outcome":"TARGET","pnl_usd":1500}
EOF

set +e
out5=$(DRY_RUN=1 JOURNAL_DIR="$PARTIAL_JDIR" bash "$DIGEST" 2>&1)
rc5=$?
set -e

assert_eq "T5 exit code" "$rc5" "0"
# Only 1 terminal close (TARGET) — PARTIAL is excluded
assert_contains "T5 1 trade (PARTIAL excluded)" "$out5" "1 trades"

# ── Summary ───────────────────────────────────────────────────────────────────
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
