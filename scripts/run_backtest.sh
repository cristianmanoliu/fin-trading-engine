#!/usr/bin/env bash
set -euo pipefail

# Usage:
#   ./scripts/run_backtest.sh                                     → BTCUSDT Jan 2024
#   ./scripts/run_backtest.sh BTCUSDT 2024 01 06                  → BTCUSDT Jan–Jun 2024
#   ./scripts/run_backtest.sh BTCUSDT 2024 01 12 configs/btcusdt.yaml  → use per-symbol config

SYMBOL="${1:-BTCUSDT}"
YEAR="${2:-2024}"
START_MONTH="${3:-01}"
END_MONTH="${4:-$START_MONTH}"
BASE_CONFIG="${5:-}"

ROOT="$(cd "$(dirname "$0")/.." && pwd)"

# Resolve which config to use as the base for sed-patching csv_path.
if [[ -z "$BASE_CONFIG" ]]; then
  SYMBOL_CFG="${ROOT}/configs/$(echo "$SYMBOL" | tr '[:upper:]' '[:lower:]').yaml"
  if [[ -f "$SYMBOL_CFG" ]]; then
    BASE_CONFIG="$SYMBOL_CFG"
  else
    BASE_CONFIG="${ROOT}/configs/default.yaml"
  fi
fi

mkdir -p "${ROOT}/data"
CONFIGS_TO_CLEAN=()
trap 'rm -f "${CONFIGS_TO_CLEAN[@]}"' EXIT

# Produce a zero-padded list of months without printf-octal issues.
months=()
for (( m = 10#$START_MONTH; m <= 10#$END_MONTH; m++ )); do
  months+=( "$(printf '%02d' "$m")" )
done

# --- download all requested months ---
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
  else
    echo "→ ${FILENAME}.csv already exists, skipping"
  fi
done

# --- run backtest for each month, accumulate results ---
TOTAL_TRADES=0; TOTAL_WINS=0; TOTAL_LOSSES=0; TOTAL_PNL=0

for m in "${months[@]}"; do
  FILENAME="${SYMBOL}-1m-${YEAR}-${m}"
  CSV="${ROOT}/data/${FILENAME}.csv"

  CONFIG=$(mktemp /tmp/backtest-config.XXXXXXXX)
  CONFIGS_TO_CLEAN+=("$CONFIG")
  sed "s|csv_path:.*|csv_path: ${CSV}|" "$BASE_CONFIG" > "$CONFIG"

  echo ""
  echo "━━━ ${SYMBOL} ${YEAR}-${m} ━━━"

  cd "$ROOT" && go run ./cmd/backtest --config "$CONFIG" 2>&1 | jq -r '
    if .level == "INFO" then
      if .msg == "signal generated" then
        "  SIGNAL  \(.side | if . == 1 then "LONG " else "SHORT" end)  entry=\(.entry)  stop=\(.stop | . * 100 | round / 100)  target=\(.target | . * 100 | round / 100)  \(.reason | capture("rr=(?<r>[0-9.]+)") | "rr=\(.r)")"
      elif .msg == "position closed" then
        "  \(.outcome // "CLOSED")   \(.side | if . == 1 then "LONG " else "SHORT" end)  entry=\(.entry)  exit=\(.exit // "?")  pnl=\(.pnl_pts | . * 100 | round / 100)pts"
      elif (.msg | test("SUMMARY")) then
        "  SUMMARY  trades=\(.total_trades)  wins=\(.wins)  losses=\(.losses)  win_rate=\(.win_rate_pct | . * 10 | round / 10)%  total_pnl=\(.total_pnl_pts | . * 100 | round / 100)pts  expectancy=\(.expectancy_pts | . * 100 | round / 100)pts"
      else empty end
    else empty end
  '

  # accumulate totals (second run reads cached CSV, no re-download)
  SUMMARY=$(cd "$ROOT" && go run ./cmd/backtest --config "$CONFIG" 2>&1 | jq -r 'select(.msg | test("SUMMARY")) | "\(.total_trades) \(.wins) \(.losses) \(.total_pnl_pts)"')
  if [[ -n "$SUMMARY" ]]; then
    read -r T W L P <<< "$SUMMARY"
    TOTAL_TRADES=$(( TOTAL_TRADES + T ))
    TOTAL_WINS=$(( TOTAL_WINS + W ))
    TOTAL_LOSSES=$(( TOTAL_LOSSES + L ))
    TOTAL_PNL=$(awk "BEGIN {printf \"%.4f\", $TOTAL_PNL + $P}")
  fi
done

# --- combined summary across all months ---
if (( ${#months[@]} > 1 )); then
  LAST_MONTH="${months[${#months[@]}-1]}"
  echo ""
  echo "━━━ COMBINED ${YEAR}-${months[0]} → ${YEAR}-${LAST_MONTH} ━━━"
  if (( TOTAL_TRADES > 0 )); then
    WIN_RATE=$(awk "BEGIN {printf \"%.1f\", $TOTAL_WINS / $TOTAL_TRADES * 100}")
    EXPECTANCY=$(awk "BEGIN {printf \"%.2f\", $TOTAL_PNL / $TOTAL_TRADES}")
    echo "  trades=${TOTAL_TRADES}  wins=${TOTAL_WINS}  losses=${TOTAL_LOSSES}  win_rate=${WIN_RATE}%  total_pnl=${TOTAL_PNL}pts  expectancy=${EXPECTANCY}pts"
  else
    echo "  no trades taken"
  fi
fi
