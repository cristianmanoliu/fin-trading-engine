#!/usr/bin/env bash
# gen_live_journals.sh — generate LIVE-config short-trade journals for the deployed
# symbols over the full history, for the #10 sizing-overlay + #6 OI-gate studies.
# Mirrors scripts/run_p4_variant.sh config-building (ema_mode:true, target_rr:6.0,
# side short, max-hold 504, 4H, funding CSV, fee10/slip5) but writes per-trade JSONL.
#
# Output: results/live_journals/<SYM>-*.jsonl  (one dir, all symbols)
set -euo pipefail
ROOT="$(cd "$(dirname "$0")/.." && pwd)"
OUT="${OUT:-$ROOT/results/live_journals}"
BIN="$ROOT/bin/backtest"
SYMS="${SYMS:-ROSEUSDT BCHUSDT GRTUSDT 1INCHUSDT ADAUSDT KAVAUSDT 1000SHIBUSDT ENSUSDT XLMUSDT IMXUSDT ETCUSDT RUNEUSDT AVAXUSDT APTUSDT DOTUSDT FILUSDT BTCUSDT CHZUSDT SANDUSDT TRXUSDT}"
START_YEAR=2020; END_YEAR=2026; END_MONTH=06

(cd "$ROOT" && go build -o "$BIN" ./cmd/backtest)
rm -rf "$OUT"; mkdir -p "$OUT"
WORK=$(mktemp -d); trap 'rm -rf "$WORK"' EXIT

run_one() {
  local sym=$1
  local merged="$WORK/${sym}.csv"; : > "$merged"
  for (( y=START_YEAR; y<=END_YEAR; y++ )); do
    local last=$(( y==END_YEAR ? 10#$END_MONTH : 12 ))
    for (( m=1; m<=last; m++ )); do
      local mm; mm=$(printf '%02d' "$m")
      local csv="$ROOT/data/${sym}-1m-${y}-${mm}.csv"
      [[ -f "$csv" ]] && cat "$csv" >> "$merged"
    done
  done
  [[ -s "$merged" ]] || { echo "  $sym: no data"; return; }
  local cfg="$WORK/cfg-${sym}.yaml"
  sed "s|ema_mode:.*|ema_mode: true|; s|target_rr:.*|target_rr: 6.0|; s|csv_path:.*|csv_path: ${merged}|" \
    "$ROOT/configs/default.yaml" > "$cfg"
  "$BIN" --config "$cfg" --symbol "$sym" \
    --side-filter short --max-hold-hours 504 --signal-tf 4H \
    --funding-csv-dir "$ROOT/data/funding" --fee-bps 10 --stop-slippage-bps 5 \
    --journal-dir "$OUT" >/dev/null 2>&1
  local n; n=$(cat "$OUT/${sym}"-*.jsonl 2>/dev/null | grep -c '"event":"close"' || echo 0)
  echo "  $sym: $n closed trades"
}

echo "generating live-config journals -> $OUT"
NCPU=$(sysctl -n hw.logicalcpu 2>/dev/null || echo 4)
export -f run_one; export WORK ROOT BIN OUT START_YEAR END_YEAR END_MONTH
printf '%s\n' $SYMS | xargs -P "$NCPU" -I{} bash -c 'run_one "{}"'
echo "DONE. total closed: $(cat "$OUT"/*.jsonl 2>/dev/null | grep -c '"event":"close"')"
