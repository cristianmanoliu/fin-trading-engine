#!/usr/bin/env bash
# side_filter_test.sh — Inverse-hypothesis test: is the live edge pure
# SHORT-alt beta, or symmetric? Runs the LIVE config (4H EMA 9/21, mh504) on
# the full 57-symbol universe, per-year, for side = long and both, so we can
# compare against the existing short results (per_year_cohort_backtest_full57.csv).
#
# If long-only WINS in the years short LOST (2020/21/23) and LOSES in 2022,
# that confirms the strategy is pure directional market beta — not alpha.
#
# Output: tasks/side_filter_test.csv  (side,year,net_pnl,trades,wins)
set -euo pipefail

ROOT="$(cd "$(dirname "$0")/.." && pwd)"
cd "$ROOT"
BINARY="$ROOT/bin/backtest"
DATA_DIR="$ROOT/data"
FUNDING_DIR="$ROOT/data/funding"
BASE_CFG="$ROOT/configs/default.yaml"
OUT="$ROOT/tasks/side_filter_test.csv"

echo "→ Rebuilding backtest binary..." >&2
go build -o "$BINARY" ./cmd/backtest

# Full 57-symbol universe (single source of truth).
mapfile -t SYMS < <(
  awk '/^universe:/{f=1;next} /^[a-z_]+:/{f=0} f && /^  - /{print $2}' \
    "$ROOT/configs/symbols.yaml"
)
echo "→ ${#SYMS[@]} symbols" >&2

YEARS=(2020 2021 2022 2023 2024 2025 2026)
declare -A END_MONTH=( [2020]=12 [2021]=12 [2022]=12 [2023]=12 [2024]=12 [2025]=12 [2026]=4 )

# LIVE config = EMA 9/21, mh504. Only thing that varies is --side-filter.
LIVE_FLAGS="--ema-fast-period 9 --ema-slow-period 21 --max-hold-hours 504"

WORKDIR="$(mktemp -d)"
trap 'rm -rf "$WORKDIR"' EXIT

echo "side,year,net_pnl,trades,wins" > "$OUT"

for side in long both; do
  for year in "${YEARS[@]}"; do
    last="${END_MONTH[$year]}"
    yn=0; yt=0; yw=0
    for sym in "${SYMS[@]}"; do
      merged="$WORKDIR/${sym}.csv"; : > "$merged"; found=0
      for (( m=1; m<=last; m++ )); do
        mm=$(printf '%02d' "$m")
        csv="$DATA_DIR/${sym}-1m-${year}-${mm}.csv"
        [[ -f "$csv" ]] && { cat "$csv" >> "$merged"; found=1; }
      done
      [[ $found -eq 0 ]] && { rm -f "$merged"; continue; }
      cfg="$WORKDIR/cfg.yaml"
      sed "s|csv_path:.*|csv_path: ${merged}|; s|ema_mode:.*|ema_mode: true|; s|^symbol:.*|symbol: ${sym}|" \
        "$BASE_CFG" > "$cfg"
      summary="$(
        "$BINARY" --config "$cfg" --signal-tf 4H --side-filter "$side" \
          --fee-bps 10 --stop-slippage-bps 5 --exact-fills --include-boundary \
          $LIVE_FLAGS --funding-csv-dir "$FUNDING_DIR" 2>&1 \
        | jq -r 'select(.msg | test("SUMMARY")) | "\(.total_pnl_usd // 0) \(.total_trades // 0) \(.wins // 0)"' \
        | tail -1
      )" || true
      read -r net tr wn <<< "${summary:-0 0 0}"
      net="${net:-0}"; tr="${tr:-0}"; wn="${wn:-0}"
      yn=$(awk "BEGIN{printf \"%.2f\", $yn + $net}")
      yt=$(( yt + tr )); yw=$(( yw + wn ))
      rm -f "$merged" "$cfg"
    done
    printf '%s,%s,%.2f,%d,%d\n' "$side" "$year" "$yn" "$yt" "$yw" >> "$OUT"
    echo "  ${side} ${year}: net=\$${yn} trades=${yt} wins=${yw}" >&2
  done
done
echo "→ Done. Wrote $OUT" >&2
