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

# ── Worker mode ──────────────────────────────────────────────────────────────
# Re-invocation of THIS script as a single-job worker (one episode × symbol).
# xargs calls `bash THIS_SCRIPT --worker <sym> <start_ms> <end_ms> <side> <year>
# <start_date> <end_date>`. Config comes via exported env (RG_* vars). Using a
# script re-invocation instead of `export -f` avoids the bash-function-export
# fragility across bash builds (BASH_FUNC import is unreliable under xargs+sh).
if [[ "${1:-}" == "--worker" ]]; then
  shift
  sym="$1"; start_ms="$2"; end_ms="$3"; side="$4"; year="$5"; start_date="$6"; end_date="$7"
  # RG_* env (WORKDIR/DATA_DIR/BASE_CFG/BINARY/FUNDING_DIR/BASE_FLAGS) MUST be
  # exported by the parent invocation; `set -u` makes a missing one fail fast
  # (helpful if you run --worker by hand for a one-off debug).
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

# Parallel fan-out across symbols within each episode. M3 Max = 16 cores; each
# bin/backtest run is independent, so we run JOBS at once via xargs -P. Override
# with JOBS=N. (Was serial → ~27 min/combo; the 8,151 backtest launches per combo
# dominate, so parallelism is the only lever that matters at the harness layer.)
JOBS="${JOBS:-12}"

# 1) timeline → episodes (start_ms,end_ms_exclusive,label,year)
# NOTE: FLAT regime days are EXCLUDED here — only SHORT/LONG runs become episodes
# (a FLAT day = strategy sits out, no trades). So the output never contains a
# "flat" label; the `label` column is the side actually traded.
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
    # start_ms,end_ms_exclusive,label,year,start_date,end_date_exclusive
    w.writerow([to_ms(s), to_ms(e), lab, yr, s, e])
' > "$EPISODES"

echo "  episodes: $(wc -l < "$EPISODES")" >&2

WORKDIR="$(mktemp -d)"; trap 'rm -rf "$WORKDIR" "$EPISODES"' EXIT
echo "symbol,year,label,net_pnl,trades,wins" > "$OUT"

BASE_FLAGS="--signal-tf 4H --fee-bps 10 --stop-slippage-bps 5 \
            --exact-fills --include-boundary --max-hold-hours 504"

# Config passed to worker re-invocations via env (RG_* namespace).
export RG_WORKDIR="$WORKDIR" RG_DATA_DIR="$DATA_DIR" RG_BASE_CFG="$BASE_CFG" \
       RG_BINARY="$BINARY" RG_FUNDING_DIR="$FUNDING_DIR" RG_BASE_FLAGS="$BASE_FLAGS"

# Build the job list: one line per (episode × symbol), tab-separated:
#   sym  start_ms  end_ms  side  year  start_date  end_date
JOBLIST="$(mktemp)"; trap 'rm -rf "$WORKDIR" "$EPISODES" "$JOBLIST"' EXIT
while IFS=, read -r start_ms end_ms label year start_date end_date; do
  # Labeler emits UPPERCASE SHORT/LONG; --side-filter requires lowercase. Gate
  # mode lowercases the regime label; baseline forces short. (This mismatch was
  # the gate-0-rows / baseline-205-rows discrepancy — caught 2026-06-07.)
  side="$(printf '%s' "$label" | tr '[:upper:]' '[:lower:]')"
  [[ "$MODE" == "baseline" ]] && side="short"
  for sym in "${SYMS[@]}"; do
    printf '%s\t%s\t%s\t%s\t%s\t%s\t%s\n' \
      "$sym" "$start_ms" "$end_ms" "$side" "$year" "$start_date" "$end_date"
  done
done < "$EPISODES" > "$JOBLIST"

echo "  jobs: $(wc -l < "$JOBLIST") (episode×symbol) | JOBS=$JOBS parallel" >&2

# Run all jobs in parallel; each worker prints at most one CSV row.
# BSD/macOS xargs has no `-a FILE` (GNU-only) — pipe the joblist via stdin.
# `-L 1` = one input line/invocation; whitespace-split passes the 7 tab fields
# as 7 args. We re-invoke THIS script in --worker mode (robust across bash
# builds vs `export -f`, which xargs+sh import unreliably).
SELF="$ROOT/tasks/regime_gate_backtest.sh"
ROWS="$(mktemp)"
xargs -P "$JOBS" -L 1 bash "$SELF" --worker < "$JOBLIST" \
  > "$ROWS" || true
cat "$ROWS" >> "$OUT"
rm -f "$ROWS"

# Guard against silent total failure. The 0-rows-from-N-jobs failure mode has
# bitten twice (uppercase side-filter; export -f not importing) — each time
# every worker exited 0 with no output, indistinguishable from a healthy run
# under `|| true`. A combo SHOULD always produce rows (every regime has trading
# symbols). 0 rows from >0 jobs = a real defect (bad flag / binary crash / jq
# missing), not an empty regime — surface it loudly.
RESULT_ROWS=$(( $(wc -l < "$OUT") - 1 ))
JOB_COUNT=$(wc -l < "$JOBLIST")
if [[ $JOB_COUNT -gt 0 && $RESULT_ROWS -eq 0 ]]; then
  echo "WARN: 0 data rows from $JOB_COUNT jobs ($OUT) — check side-filter value, binary exit code, jq availability" >&2
fi
echo "→ Wrote $OUT ($RESULT_ROWS rows)" >&2
