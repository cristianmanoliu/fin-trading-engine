#!/usr/bin/env bash
# verify_rsi_concentration.sh — Is the post-fix RSI "+79% vs baseline" a robust
# cross-symbol/cross-window result, or driven by one symbol / one window?
#
# Runs RSI-mode (fixed binary, HEAD) per-symbol over the full 57-symbol universe,
# 5y, with journaling; computes per-symbol total NET and the single-symbol share.
# Kill-criteria red flag: any single symbol > 40% of cumulative PnL.
set -euo pipefail
ROOT="$(cd "$(dirname "$0")/.." && pwd)"
cd "$ROOT"
source "$ROOT/scripts/lib/symbols.sh"
SYMS="$(get_symbols universe)"

WORK=$(mktemp -d /tmp/verify_rsi_conc.XXXX)
trap 'rm -rf "$WORK"' EXIT

BIN="$WORK/bt"; go build -o "$BIN" ./cmd/backtest
JDIR="$WORK/journals"; mkdir -p "$JDIR"
FLAGS="--signal-tf 4H --side-filter short --max-hold-hours 504 \
  --rsi-mode --rsi-period 14 \
  --funding-csv-dir $ROOT/data/funding --fee-bps 10 --stop-slippage-bps 5 \
  --exact-fills --include-boundary --pessimistic-ambiguous"

for sym in $SYMS; do
  merged="$WORK/${sym}.csv"
  for f in data/${sym}-1m-20{20,21,22,23,24,25}-*.csv; do [[ -f "$f" ]] && cat "$f" >> "$merged"; done
  [[ -s "$merged" ]] || continue
  cfg="$WORK/cfg-${sym}.yaml"
  sed "s|momentum_mode:.*|momentum_mode: false|; s|vwap_deviation_mode:.*|vwap_deviation_mode: false|; s|ema_mode:.*|ema_mode: true|; s|target_rr:.*|target_rr: 6.0|; s|csv_path:.*|csv_path: ${merged}|" \
    configs/default.yaml > "$cfg"
  # shellcheck disable=SC2086
  "$BIN" --config "$cfg" --symbol "$sym" $FLAGS --journal-dir "$JDIR" >/dev/null 2>&1 || true
done

python3 - "$JDIR" <<'PY'
import sys, json, glob, os, collections
jdir=sys.argv[1]
pnl=collections.defaultdict(float); trades=collections.defaultdict(int)
pnl_by_year=collections.defaultdict(float)
for fp in glob.glob(os.path.join(jdir,"*.jsonl")):
    for line in open(fp):
        line=line.strip()
        if not line: continue
        try: e=json.loads(line)
        except: continue
        if e.get("event")!="close": continue
        sym=e.get("symbol","?"); usd=float(e.get("pnl_usd",0) or 0)
        pnl[sym]+=usd; trades[sym]+=1
        ts=e.get("ts","")
        yr=ts[:4] if len(ts)>=4 else "?"
        pnl_by_year[yr]+=usd
total=sum(pnl.values())
print(f"TOTAL NET (57-sym, 5y, post-fix RSI): ${total:,.0f}   trades={sum(trades.values())}")
print("\nPer-symbol NET (top 12 by |contribution|):")
ranked=sorted(pnl.items(), key=lambda kv:-abs(kv[1]))
pos_total=sum(v for v in pnl.values() if v>0)
for sym,v in ranked[:12]:
    share = (v/total*100) if total else 0
    pshare= (v/pos_total*100) if (pos_total and v>0) else 0
    print(f"  {sym:12s} ${v:>12,.0f}  trades={trades[sym]:>4}  share_of_total={share:>6.1f}%  share_of_gross_pos={pshare:>5.1f}%")
# max single-symbol share
top_sym,top_v=ranked[0]
print(f"\nMax single-symbol share of TOTAL: {top_sym} = {top_v/total*100:.1f}%  (kill-flag if >40%)")
print(f"Max single-symbol share of GROSS-POSITIVE: {max((v/pos_total*100) for v in pnl.values() if v>0):.1f}%")
n_pos=sum(1 for v in pnl.values() if v>0); n_neg=sum(1 for v in pnl.values() if v<0)
print(f"Symbols positive: {n_pos}  negative: {n_neg}  (breadth)")
print("\nPer-YEAR NET (concentration in time):")
for yr in sorted(pnl_by_year):
    print(f"  {yr}: ${pnl_by_year[yr]:>12,.0f}  ({pnl_by_year[yr]/total*100:>5.1f}% of total)")
PY
