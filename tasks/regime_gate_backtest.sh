#!/usr/bin/env bash
# regime_gate_backtest.sh — run the strategy with a BTC-regime-driven, time-
# varying --side-filter, by slicing each symbol's 1m history at regime-episode
# boundaries and running bin/backtest per episode with the matching side.
#
# Usage: bash tasks/regime_gate_backtest.sh <X> <Y> <Z>
#   e.g.  bash tasks/regime_gate_backtest.sh 10 14 14
# Also supports MODE=baseline to force --side-filter short on the SAME episode
# boundaries (so force-close geometry matches) — used by walk_forward.py.
#
# Output: tasks/regime_gate_results/episodes_X<X>_Y<Y>_Z<Z>[_baseline].csv
#   columns: symbol,year,label,net_pnl,trades,wins
# Research-only. Full-57 universe. Live cost stack. No engine changes.
set -euo pipefail
ROOT="$(cd "$(dirname "$0")/.." && pwd)"
cd "$ROOT"

X="${1:?need X}"; Y="${2:?need Y}"; Z="${3:?need Z}"
MODE="${MODE:-gate}"   # gate | baseline
ANCHOR="$ROOT/data/anchor/BTCUSDT-1d.csv"
BINARY="$ROOT/bin/backtest"
DATA_DIR="$ROOT/data"
FUNDING_DIR="$ROOT/data/funding"
BASE_CFG="$ROOT/configs/default.yaml"
RESDIR="$ROOT/tasks/regime_gate_results"
mkdir -p "$RESDIR"

tag="X${X}_Y${Y}_Z${Z}"; [[ "$MODE" == "baseline" ]] && tag="${tag}_baseline"
OUT="$RESDIR/episodes_${tag}.csv"

echo "→ Building binary..." >&2
go build -o "$BINARY" ./cmd/backtest

mapfile -t SYMS < <(awk '/^universe:/{f=1;next} /^[a-z_]+:/{f=0} f && /^  - /{print $2}' "$ROOT/configs/symbols.yaml")
echo "→ ${#SYMS[@]} symbols | (X=$X Y=$Y Z=$Z) MODE=$MODE" >&2

# 1) timeline → episodes (start_ms,end_ms_exclusive,label,year)
EPISODES="$(mktemp)"
python3 "$ROOT/tasks/regime_label.py" --anchor "$ANCHOR" --x "$X" --y "$Y" --z "$Z" \
  | python3 -c '
import sys, csv, datetime
def to_ms(d):
    return int(datetime.datetime.strptime(d, "%Y-%m-%d").replace(tzinfo=datetime.timezone.utc).timestamp()*1000)
rows = list(csv.reader(sys.stdin)); rows = rows[1:]  # drop header
runs = []
cur_label=None; cur_start=None; prev_date=None
for d,label in rows:
    if label != cur_label:
        if cur_label is not None and cur_label != "FLAT":
            runs.append((cur_start, d, cur_label))
        cur_label=label; cur_start=d
    prev_date=d
if cur_label is not None and cur_label != "FLAT":
    end = (datetime.datetime.strptime(prev_date,"%Y-%m-%d")+datetime.timedelta(days=1)).strftime("%Y-%m-%d")
    runs.append((cur_start, end, cur_label))
w=csv.writer(sys.stdout)
for s,e,lab in runs:
    yr = s[:4]
    w.writerow([to_ms(s), to_ms(e), lab, yr])
' > "$EPISODES"

echo "  episodes: $(wc -l < "$EPISODES")" >&2

WORKDIR="$(mktemp -d)"; trap 'rm -rf "$WORKDIR" "$EPISODES"' EXIT
echo "symbol,year,label,net_pnl,trades,wins" > "$OUT"

BASE_FLAGS="--signal-tf 4H --fee-bps 10 --stop-slippage-bps 5 \
            --exact-fills --include-boundary --max-hold-hours 504"

while IFS=, read -r start_ms end_ms label year; do
  side="$label"; [[ "$MODE" == "baseline" ]] && side="short"
  for sym in "${SYMS[@]}"; do
    merged="$WORKDIR/${sym}.csv"; : > "$merged"
    found=0
    for yr in $(python3 -c "import datetime as dt;print(dt.datetime.utcfromtimestamp($start_ms/1000).year);print(dt.datetime.utcfromtimestamp($end_ms/1000).year)" | sort -u); do
      for mm in 01 02 03 04 05 06 07 08 09 10 11 12; do
        csv="$DATA_DIR/${sym}-1m-${yr}-${mm}.csv"
        [[ -f "$csv" ]] || continue
        awk -F, -v a="$start_ms" -v b="$end_ms" \
            '$1=="open_time"||$1==""{next} ($1+0)>=a && ($1+0)<b {print}' \
            "$csv" >> "$merged" && found=1
      done
    done
    [[ $found -eq 0 || ! -s "$merged" ]] && { rm -f "$merged"; continue; }

    cfg="$WORKDIR/cfg-${sym}.yaml"
    sed "s|csv_path:.*|csv_path: ${merged}|; s|ema_mode:.*|ema_mode: true|; s|^symbol:.*|symbol: ${sym}|" \
        "$BASE_CFG" > "$cfg"

    summary="$(
      "$BINARY" --config "$cfg" $BASE_FLAGS --side-filter "$side" \
        --funding-csv-dir "$FUNDING_DIR" 2>&1 \
      | jq -r 'select(.msg|test("SUMMARY")) | "\(.total_pnl_usd//0) \(.total_trades//0) \(.wins//0)"' \
      | tail -1
    )" || true
    read -r net tr wn <<< "${summary:-0 0 0}"
    net="${net:-0}"; tr="${tr:-0}"; wn="${wn:-0}"
    [[ "$tr" == "0" ]] && { rm -f "$merged" "$cfg"; continue; }
    printf '%s,%s,%s,%.2f,%d,%d\n' "$sym" "$year" "$side" "$net" "$tr" "$wn" >> "$OUT"
    rm -f "$merged" "$cfg"
  done
done < "$EPISODES"

echo "→ Wrote $OUT ($(($(wc -l < "$OUT")-1)) rows)" >&2
