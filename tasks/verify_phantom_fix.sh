#!/usr/bin/env bash
# verify_phantom_fix.sh — PROVE the side-filter fix (d1d0fae) eliminates phantom
# LONG fills in RSI/MACD modes under --side-filter short.
#
# Method: build the FIXED binary (HEAD) and the PRE-FIX binary (d1d0fae^), run
# RSI-mode + MACD-mode on a handful of symbols with --journal-dir, count LONG vs
# SHORT closes in each. Claim under test:
#   - PRE-FIX  : RSI/MACD journals contain LONG closes (phantom)   → bug present
#   - POST-FIX : RSI/MACD journals contain ZERO LONG closes        → bug fixed
set -euo pipefail
ROOT="$(cd "$(dirname "$0")/.." && pwd)"
cd "$ROOT"

WORK=$(mktemp -d /tmp/verify_phantom.XXXX)
trap 'rm -rf "$WORK"' EXIT

SYMS="BTCUSDT ETHUSDT SOLUSDT BNBUSDT XRPUSDT"   # liquid subset, fast
FLAGS_BASE="--signal-tf 4H --side-filter short --max-hold-hours 504 \
  --funding-csv-dir $ROOT/data/funding --fee-bps 10 --stop-slippage-bps 5 \
  --exact-fills --include-boundary --pessimistic-ambiguous"

# Build both binaries WITHOUT disturbing working tree: build HEAD first
# (current), then a detached worktree build of d1d0fae^.
echo "→ Building POST-FIX binary (current HEAD)..."
POSTFIX="$WORK/bt_postfix"
go build -o "$POSTFIX" ./cmd/backtest

echo "→ Building PRE-FIX binary (d1d0fae^) in a temp worktree..."
PREFIX="$WORK/bt_prefix"
WT="$WORK/wt_prefix"
git worktree add -q --detach "$WT" "d1d0fae^"
( cd "$WT" && go build -o "$PREFIX" ./cmd/backtest )
git worktree remove --force "$WT"

run_mode() { # $1=binary  $2=mode-flags  $3=label  $4=jdir
  local bin="$1" modeflags="$2" label="$3" jdir="$4"
  mkdir -p "$jdir"
  for sym in $SYMS; do
    # merge that symbol's 5y 1m CSVs
    local merged="$WORK/${sym}.csv"
    if [[ ! -f "$merged" ]]; then
      for f in data/${sym}-1m-20{20,21,22,23,24,25}-*.csv; do
        [[ -f "$f" ]] && cat "$f" >> "$merged"
      done
    fi
    [[ -s "$merged" ]] || { echo "  (no data $sym)"; continue; }
    local cfg="$WORK/cfg-${label}-${sym}.yaml"
    sed "s|momentum_mode:.*|momentum_mode: false|; s|vwap_deviation_mode:.*|vwap_deviation_mode: false|; s|ema_mode:.*|ema_mode: true|; s|target_rr:.*|target_rr: 6.0|; s|csv_path:.*|csv_path: ${merged}|" \
      configs/default.yaml > "$cfg"
    # shellcheck disable=SC2086
    "$bin" --config "$cfg" --symbol "$sym" $FLAGS_BASE $modeflags --journal-dir "$jdir" >/dev/null 2>&1 || true
  done
}

count_sides() { # $1=jdir  → prints "OPEN_LONG OPEN_SHORT CLOSE_LONG CLOSE_SHORT"
  local jdir="$1"
  python3 - "$jdir" <<'PY'
import sys, json, glob, os
jdir=sys.argv[1]
ol=os_=cl=cs=0
for fp in glob.glob(os.path.join(jdir,"*.jsonl")):
    for line in open(fp):
        line=line.strip()
        if not line: continue
        try: e=json.loads(line)
        except: continue
        ev=e.get("event"); sd=(e.get("side") or "").upper()
        if ev=="open":
            if sd=="LONG": ol+=1
            elif sd=="SHORT": os_+=1
        elif ev=="close":
            if sd=="LONG": cl+=1
            elif sd=="SHORT": cs+=1
print(ol, os_, cl, cs)
PY
}

echo ""
echo "================ RSI-mode ================"
RSI_FLAGS="--rsi-mode --rsi-period 14"
run_mode "$PREFIX"  "$RSI_FLAGS" "rsi_prefix"  "$WORK/j_rsi_pre"
run_mode "$POSTFIX" "$RSI_FLAGS" "rsi_postfix" "$WORK/j_rsi_post"
read pre_ol pre_os pre_cl pre_cs <<<"$(count_sides "$WORK/j_rsi_pre")"
read pos_ol pos_os pos_cl pos_cs <<<"$(count_sides "$WORK/j_rsi_post")"
echo "PRE-FIX  RSI : open LONG=$pre_ol SHORT=$pre_os | close LONG=$pre_cl SHORT=$pre_cs"
echo "POST-FIX RSI : open LONG=$pos_ol SHORT=$pos_os | close LONG=$pos_cl SHORT=$pos_cs"

echo ""
echo "================ MACD-mode ================"
MACD_FLAGS="--macd-mode --macd-fast 12 --macd-slow 26 --macd-signal 9"
run_mode "$PREFIX"  "$MACD_FLAGS" "macd_prefix"  "$WORK/j_macd_pre"
run_mode "$POSTFIX" "$MACD_FLAGS" "macd_postfix" "$WORK/j_macd_post"
read mpre_ol mpre_os mpre_cl mpre_cs <<<"$(count_sides "$WORK/j_macd_pre")"
read mpos_ol mpos_os mpos_cl mpos_cs <<<"$(count_sides "$WORK/j_macd_post")"
echo "PRE-FIX  MACD: open LONG=$mpre_ol SHORT=$mpre_os | close LONG=$mpre_cl SHORT=$mpre_cs"
echo "POST-FIX MACD: open LONG=$mpos_ol SHORT=$mpos_os | close LONG=$mpos_cl SHORT=$mpos_cs"

echo ""
echo "================ VERDICT ================"
fail=0
if [[ "$pre_ol" -gt 0 ]]; then echo "✓ PRE-FIX RSI had $pre_ol phantom LONG opens (bug confirmed present)"; else echo "✗ UNEXPECTED: PRE-FIX RSI had 0 LONG opens — bug not reproduced"; fail=1; fi
if [[ "$pos_ol" -eq 0 ]]; then echo "✓ POST-FIX RSI has 0 LONG opens (fix confirmed)"; else echo "✗ FAIL: POST-FIX RSI still has $pos_ol LONG opens — fix INCOMPLETE"; fail=1; fi
if [[ "$mpre_ol" -gt 0 ]]; then echo "✓ PRE-FIX MACD had $mpre_ol phantom LONG opens (bug confirmed present)"; else echo "✗ UNEXPECTED: PRE-FIX MACD had 0 LONG opens"; fail=1; fi
if [[ "$mpos_ol" -eq 0 ]]; then echo "✓ POST-FIX MACD has 0 LONG opens (fix confirmed)"; else echo "✗ FAIL: POST-FIX MACD still has $mpos_ol LONG opens"; fail=1; fi
echo ""
[[ $fail -eq 0 ]] && echo "RESULT: PASS — fix verified, phantom longs eliminated" || echo "RESULT: FAIL — see above"
exit $fail
