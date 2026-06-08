#!/usr/bin/env bash
# regime_switch_backtest.sh — full-stop regime-SWITCH backtest driver.
# Slices each symbol's 1m history at DWELL-AWARE SHORT-episode boundaries
# (tasks/regime_switch_episodes.py) and runs bin/backtest per (episode×symbol)
# with --side-filter short. Because each episode is an isolated backtest, a
# position cannot survive past its SHORT episode — that IS the force-close on
# regime exit. MODE=baseline runs always-short over the SAME boundaries.
#
# Usage: bash tasks/regime_switch_backtest.sh <X> <Y>
#   env: DWELL=3 (default), MODE=switch|baseline, ANCHOR, RESDIR, JOBS,
#        EXTRA_BASE_FLAGS (per-cohort EMA/max-hold)
# Research-only. No engine changes.
set -euo pipefail
ROOT="$(cd "$(dirname "$0")/.." && pwd)"; cd "$ROOT"

# ── Worker mode (verbatim from regime_gate_backtest.sh) ──────────────────────
if [[ "${1:-}" == "--worker" ]]; then
  shift
  sym="$1"; start_ms="$2"; end_ms="$3"; side="$4"; year="$5"; start_date="$6"; end_date="$7"
  merged="$(mktemp "$RG_WORKDIR/m.${sym}.XXXXXXXX")"
  found=0
  sy="${start_date:0:4}"; sm="${start_date:5:2}"
  ey="${end_date:0:4}";   em="${end_date:5:2}"
  y="$sy"; m="$sm"
  while :; do
    csv="$RG_DATA_DIR/${sym}-1m-${y}-${m}.csv"
    if [[ -f "$csv" ]]; then
      awk -F, -v a="$start_ms" -v b="$end_ms" \
          '$1=="open_time"||$1==""{next} ($1+0)>=a && ($1+0)<b {print}' \
          "$csv" >> "$merged" && found=1
    fi
    [[ "$y" == "$ey" && "$m" == "$em" ]] && break
    if [[ "$m" == "12" ]]; then m="01"; y=$(printf '%04d' $((10#$y + 1)));
    else m=$(printf '%02d' $((10#$m + 1))); fi
    [[ "$y" -gt $((10#$ey + 1)) ]] && break
  done
  if [[ $found -eq 0 || ! -s "$merged" ]]; then rm -f "$merged"; exit 0; fi
  cfg="$(mktemp "$RG_WORKDIR/cfg.${sym}.XXXXXXXX")"
  sed "s|csv_path:.*|csv_path: ${merged}|; s|ema_mode:.*|ema_mode: true|; s|^symbol:.*|symbol: ${sym}|" \
      "$RG_BASE_CFG" > "$cfg"
  summary="$(
    "$RG_BINARY" --config "$cfg" $RG_BASE_FLAGS --side-filter "$side" \
      --funding-csv-dir "$RG_FUNDING_DIR" 2>/dev/null \
    | jq -r 'select(.msg|test("SUMMARY")) | "\(.total_pnl_usd//0) \(.total_trades//0) \(.wins//0)"' \
    | tail -1
  )" || true
  read -r net tr wn <<< "${summary:-0 0 0}"
  net="${net:-0}"; tr="${tr:-0}"; wn="${wn:-0}"
  rm -f "$merged" "$cfg"
  [[ "$tr" == "0" || -z "$tr" ]] && exit 0
  printf '%s,%s,%s,%.2f,%d,%d\n' "$sym" "$year" "$side" "$net" "$tr" "$wn"
  exit 0
fi
# ── End worker mode ──────────────────────────────────────────────────────────

X="${1:?need X}"; Y="${2:?need Y}"
DWELL="${DWELL:-3}"
MODE="${MODE:-switch}"   # switch | baseline
ANCHOR="${ANCHOR:-$ROOT/data/anchor/BTCUSDT-1d.csv}"
BINARY="$ROOT/bin/backtest"
DATA_DIR="$ROOT/data"
FUNDING_DIR="$ROOT/data/funding"
BASE_CFG="$ROOT/configs/default.yaml"
RESDIR="${RESDIR:-$ROOT/tasks/regime_switch_results}"
mkdir -p "$RESDIR"

tag="X${X}_Y${Y}"; [[ "$MODE" == "baseline" ]] && tag="${tag}_baseline"
OUT="$RESDIR/episodes_${tag}.csv"

echo "→ Building binary..." >&2
go build -o "$BINARY" ./cmd/backtest

mapfile -t SYMS < <(awk '/^universe:/{f=1;next} /^[a-z_]+:/{f=0} f && /^  - /{print $2}' "$ROOT/configs/symbols.yaml")
echo "→ ${#SYMS[@]} symbols | (X=$X Y=$Y D=$DWELL) MODE=$MODE" >&2

JOBS="${JOBS:-12}"

# 1) timeline → dwell-aware SHORT episodes (start,end_excl,SHORT,year)
EPISODES="$(mktemp)"
TIMELINE="$(mktemp)"
# Z is irrelevant to SHORT labeling but regime_label.py requires it; pass --z 9999
# so LONG never fires (we collapse LONG→OFF anyway, but this keeps the timeline
# purely SHORT/FLAT and avoids wasted LONG runs).
python3 "$ROOT/tasks/regime_label.py" --anchor "$ANCHOR" --x "$X" --y "$Y" --z 9999 \
  > "$TIMELINE"
python3 "$ROOT/tasks/regime_switch_episodes.py" --timeline "$TIMELINE" --dwell "$DWELL" \
  | python3 -c '
import sys, csv, datetime
def to_ms(d):
    return int(datetime.datetime.strptime(d, "%Y-%m-%d").replace(tzinfo=datetime.timezone.utc).timestamp()*1000)
w=csv.writer(sys.stdout)
for s,e,lab,yr in csv.reader(sys.stdin):
    w.writerow([to_ms(s), to_ms(e), lab, yr, s, e])
' > "$EPISODES"

echo "  episodes: $(wc -l < "$EPISODES")" >&2

WORKDIR="$(mktemp -d)"; trap 'rm -rf "$WORKDIR" "$EPISODES" "$TIMELINE"' EXIT
echo "symbol,year,label,net_pnl,trades,wins" > "$OUT"

BASE_FLAGS="--signal-tf 4H --fee-bps 10 --stop-slippage-bps 5 \
            --exact-fills --include-boundary --max-hold-hours 504"
[[ -n "${EXTRA_BASE_FLAGS:-}" ]] && BASE_FLAGS="$BASE_FLAGS $EXTRA_BASE_FLAGS"

export RG_WORKDIR="$WORKDIR" RG_DATA_DIR="$DATA_DIR" RG_BASE_CFG="$BASE_CFG" \
       RG_BINARY="$BINARY" RG_FUNDING_DIR="$FUNDING_DIR" RG_BASE_FLAGS="$BASE_FLAGS"

# Build joblist: one line per (episode×symbol). Side is ALWAYS short.
JOBLIST="$(mktemp)"; trap 'rm -rf "$WORKDIR" "$EPISODES" "$TIMELINE" "$JOBLIST"' EXIT
while IFS=, read -r start_ms end_ms label year start_date end_date; do
  for sym in "${SYMS[@]}"; do
    printf '%s\t%s\t%s\t%s\t%s\t%s\t%s\n' \
      "$sym" "$start_ms" "$end_ms" "short" "$year" "$start_date" "$end_date"
  done
done < "$EPISODES" > "$JOBLIST"

echo "  jobs: $(wc -l < "$JOBLIST") (episode×symbol) | JOBS=$JOBS parallel" >&2

SELF="$ROOT/tasks/regime_switch_backtest.sh"
ROWS="$(mktemp)"
xargs -P "$JOBS" -L 1 bash "$SELF" --worker < "$JOBLIST" > "$ROWS" || true
cat "$ROWS" >> "$OUT"; rm -f "$ROWS"

RESULT_ROWS=$(( $(wc -l < "$OUT") - 1 ))
JOB_COUNT=$(wc -l < "$JOBLIST")
if [[ $JOB_COUNT -gt 0 && $RESULT_ROWS -eq 0 ]]; then
  echo "WARN: 0 data rows from $JOB_COUNT jobs ($OUT) — check binary/jq/flags" >&2
fi
echo "→ Wrote $OUT ($RESULT_ROWS rows)" >&2
