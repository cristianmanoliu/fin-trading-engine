#!/usr/bin/env bash
# gen_batch3_journals.sh — generate per-trade journals for batch-3 candidate #21
# (RSI/MACD deep validation). Mirrors gen_live_journals.sh exactly except the
# signal mode: cell "macd" adds --macd-mode, cell "rsi" adds --rsi-mode.
# Baseline cell = existing results/live_journals (gen_live_journals.sh output).
#
# Pre-reg: results/strategy_candidates_batch3_2026-06-10.md (#21).
# Output: results/batch3_journals/<cell>/<SYM>-*.jsonl
set -euo pipefail
ROOT="$(cd "$(dirname "$0")/.." && pwd)"
BIN="$ROOT/bin/backtest"
SYMS="${SYMS:-ROSEUSDT BCHUSDT GRTUSDT 1INCHUSDT ADAUSDT KAVAUSDT 1000SHIBUSDT ENSUSDT XLMUSDT IMXUSDT ETCUSDT RUNEUSDT AVAXUSDT APTUSDT DOTUSDT FILUSDT BTCUSDT CHZUSDT SANDUSDT TRXUSDT}"
START_YEAR=2020; END_YEAR=2026; END_MONTH=06

(cd "$ROOT" && go build -o "$BIN" ./cmd/backtest)
WORK=$(mktemp -d); trap 'rm -rf "$WORK"' EXIT

run_one() {
  local cell=$1 sym=$2
  local out="$ROOT/results/batch3_journals/$cell"
  local merged="$WORK/${cell}-${sym}.csv"; : > "$merged"
  for (( y=START_YEAR; y<=END_YEAR; y++ )); do
    local last=$(( y==END_YEAR ? 10#$END_MONTH : 12 ))
    for (( m=1; m<=last; m++ )); do
      local mm; mm=$(printf '%02d' "$m")
      local csv="$ROOT/data/${sym}-1m-${y}-${mm}.csv"
      [[ -f "$csv" ]] && cat "$csv" >> "$merged"
    done
  done
  [[ -s "$merged" ]] || { echo "  $cell/$sym: no data"; return; }
  local cfg="$WORK/cfg-${cell}-${sym}.yaml"
  sed "s|ema_mode:.*|ema_mode: true|; s|target_rr:.*|target_rr: 6.0|; s|csv_path:.*|csv_path: ${merged}|" \
    "$ROOT/configs/default.yaml" > "$cfg"
  local mode_flags=()
  case "$cell" in
    macd) mode_flags=( --macd-mode --macd-fast 12 --macd-slow 26 --macd-signal 9 ) ;;
    rsi)  mode_flags=( --rsi-mode --rsi-period 14 ) ;;
    *) echo "unknown cell $cell" >&2; return 1 ;;
  esac
  "$BIN" --config "$cfg" --symbol "$sym" \
    --side-filter short --max-hold-hours 504 --signal-tf 4H \
    --funding-csv-dir "$ROOT/data/funding" --fee-bps 10 --stop-slippage-bps 5 \
    "${mode_flags[@]}" \
    --journal-dir "$out" >/dev/null 2>&1
  rm -f "$merged"
  local n; n=$(cat "$out/${sym}"-*.jsonl 2>/dev/null | grep -c '"event":"close"' || echo 0)
  echo "  $cell/$sym: $n closed trades"
}

for cell in macd rsi; do
  rm -rf "$ROOT/results/batch3_journals/$cell"
  mkdir -p "$ROOT/results/batch3_journals/$cell"
done

echo "generating batch-3 #21 journals (macd + rsi cells) -> results/batch3_journals/"
NCPU=$(sysctl -n hw.logicalcpu 2>/dev/null || echo 4)
export -f run_one; export WORK ROOT BIN START_YEAR END_YEAR END_MONTH
{ for s in $SYMS; do echo "macd $s"; echo "rsi $s"; done; } \
  | xargs -P "$NCPU" -L1 bash -c 'run_one $0 $1'
echo "DONE macd: $(cat "$ROOT/results/batch3_journals/macd"/*.jsonl 2>/dev/null | grep -c '"event":"close"')"
echo "DONE rsi:  $(cat "$ROOT/results/batch3_journals/rsi"/*.jsonl 2>/dev/null | grep -c '"event":"close"')"
