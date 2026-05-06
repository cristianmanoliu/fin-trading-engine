#!/usr/bin/env bash
set -euo pipefail

# Runs the backtest across a range of min_rr values and prints a comparison table.
#
# Usage:
#   ./scripts/sweep.sh                          → BTCUSDT 2024, min_rr 0.0..3.0
#   ./scripts/sweep.sh BTCUSDT 2024 01 12       → custom symbol/year/months
#   ./scripts/sweep.sh BTCUSDT 2024 01 12 "0 0.5 1.0 1.5 2.0 2.5 3.0"  → custom values

SYMBOL="${1:-BTCUSDT}"
YEAR="${2:-2024}"
START_MONTH="${3:-01}"
END_MONTH="${4:-$START_MONTH}"
RR_VALUES="${5:-0 0.5 1.0 1.5 2.0 2.5 3.0}"

ROOT="$(cd "$(dirname "$0")/.." && pwd)"
CONFIGS_TO_CLEAN=()
trap 'rm -f "${CONFIGS_TO_CLEAN[@]}"' EXIT

months=()
for (( m = 10#$START_MONTH; m <= 10#$END_MONTH; m++ )); do
  months+=( "$(printf '%02d' "$m")" )
done

LAST_MONTH="${months[${#months[@]}-1]}"

# --- ensure all CSV files are downloaded first ---
mkdir -p "${ROOT}/data"
for m in "${months[@]}"; do
  FILENAME="${SYMBOL}-1m-${YEAR}-${m}"
  CSV="${ROOT}/data/${FILENAME}.csv"
  URL="https://data.binance.vision/data/futures/um/monthly/klines/${SYMBOL}/1m/${FILENAME}.zip"
  if [[ ! -f "$CSV" ]]; then
    echo "→ Downloading ${FILENAME}.zip ..."
    if curl -f -# -L "$URL" -o "${ROOT}/data/${FILENAME}.zip" 2>/dev/null && \
       unzip -t "${ROOT}/data/${FILENAME}.zip" >/dev/null 2>&1; then
      unzip -o "${ROOT}/data/${FILENAME}.zip" -d "${ROOT}/data"
      rm "${ROOT}/data/${FILENAME}.zip"
    else
      echo "→ No data for ${FILENAME}, skipping"
      rm -f "${ROOT}/data/${FILENAME}.zip"
    fi
  fi
done

echo ""
echo "Sweeping min_rr for ${SYMBOL} ${YEAR}-${months[0]} → ${YEAR}-${LAST_MONTH}"
echo ""
printf "%-8s  %7s  %5s  %9s  %9s  %8s  %8s  %7s  %12s\n" \
  "min_rr" "trades" "win%" "exp/trade" "total_pnl" "avg_win" "avg_loss" "W/L" "filtered_out"
echo "────────  ───────  ─────  ─────────  ─────────  ────────  ────────  ───────  ────────────"

BASELINE_TRADES=""

for rr in $RR_VALUES; do
  TOTAL_TRADES=0; TOTAL_WINS=0; TOTAL_PNL=0

  for m in "${months[@]}"; do
    CSV="${ROOT}/data/${SYMBOL}-1m-${YEAR}-${m}.csv"
    CONFIG=$(mktemp /tmp/sweep-config.XXXXXXXX)
    CONFIGS_TO_CLEAN+=("$CONFIG")

    sed "s|csv_path:.*|csv_path: ${CSV}|; s|min_rr:.*|min_rr: ${rr}|" \
      "${ROOT}/configs/default.yaml" > "$CONFIG"

    SUMMARY=$(cd "$ROOT" && go run ./cmd/backtest --config "$CONFIG" 2>&1 \
      | jq -r 'select(.msg | test("SUMMARY")) | "\(.total_trades) \(.wins) \(.total_pnl_pts) \(.avg_win_pts) \(.avg_loss_pts)"')

    if [[ -n "$SUMMARY" ]]; then
      read -r T W P AW AL <<< "$SUMMARY"
      TOTAL_TRADES=$(( TOTAL_TRADES + T ))
      TOTAL_WINS=$(( TOTAL_WINS + W ))
      TOTAL_PNL=$(awk "BEGIN {printf \"%.2f\", $TOTAL_PNL + $P}")
      # weighted accumulation of avg_win / avg_loss
      TOTAL_WIN_SUM=$(awk "BEGIN {printf \"%.2f\", ${TOTAL_WIN_SUM:-0} + ($AW * $W)}")
      TOTAL_LOSS_SUM=$(awk "BEGIN {printf \"%.2f\", ${TOTAL_LOSS_SUM:-0} + ($AL * ($T - $W))}")
    fi
  done

  if [[ "$rr" == "0" ]]; then
    BASELINE_TRADES=$TOTAL_TRADES
  fi

  FILTERED=$(( BASELINE_TRADES - TOTAL_TRADES ))

  if (( TOTAL_TRADES > 0 )); then
    LOSSES=$(( TOTAL_TRADES - TOTAL_WINS ))
    WIN_RATE=$(awk "BEGIN {printf \"%.1f\", $TOTAL_WINS / $TOTAL_TRADES * 100}")
    EXPECTANCY=$(awk "BEGIN {printf \"%.2f\", $TOTAL_PNL / $TOTAL_TRADES}")
    AVG_WIN=$(awk "BEGIN {printf \"%.1f\", ($TOTAL_WINS > 0) ? ${TOTAL_WIN_SUM:-0} / $TOTAL_WINS : 0}")
    AVG_LOSS=$(awk "BEGIN {printf \"%.1f\", ($LOSSES > 0) ? ${TOTAL_LOSS_SUM:-0} / $LOSSES : 0}")
    WL_RATIO=$(awk "BEGIN {printf \"%.2f\", ($AVG_LOSS > 0) ? $AVG_WIN / $AVG_LOSS : 0}")
    printf "%-8s  %7s  %5s  %9s  %9s  %8s  %8s  %7s  %12s\n" \
      "$rr" "$TOTAL_TRADES" "${WIN_RATE}%" "${EXPECTANCY}pts" "${TOTAL_PNL}pts" \
      "${AVG_WIN}pts" "${AVG_LOSS}pts" "${WL_RATIO}" "${FILTERED}"
  else
    printf "%-8s  %7s  %5s  %9s  %9s  %8s  %8s  %7s  %12s\n" \
      "$rr" "0" "—" "—" "—" "—" "—" "—" "${FILTERED}"
  fi

  # reset per-rr accumulators
  TOTAL_WIN_SUM=0; TOTAL_LOSS_SUM=0
done

echo ""
echo "Best min_rr = the row with the highest expectancy/trade AND acceptable trade count."
