#!/usr/bin/env bash
set -euo pipefail

SYMBOL="${1:-BTCUSDT}"
YEAR="${2:-2024}"
MONTH="${3:-01}"

FILENAME="${SYMBOL}-1m-${YEAR}-$(printf '%02d' "$MONTH")"
URL="https://data.binance.vision/data/futures/um/monthly/klines/${SYMBOL}/1m/${FILENAME}.zip"
EXTERNAL_DEST="${TRADING_ENGINE_CSV_DIR:-$HOME/Main/data/trading-engine/csvs}"
REPO_DATA="$(dirname "$0")/../data"

mkdir -p "$EXTERNAL_DEST" "$REPO_DATA"

echo "Downloading ${FILENAME}.zip ..."
if curl -f -# -L "$URL" -o "${EXTERNAL_DEST}/${FILENAME}.zip" 2>/dev/null; then
  echo "Unzipping ..."
  unzip -o "${EXTERNAL_DEST}/${FILENAME}.zip" -d "$EXTERNAL_DEST"
  rm "${EXTERNAL_DEST}/${FILENAME}.zip"
  ln -sf "${EXTERNAL_DEST}/${FILENAME}.csv" "${REPO_DATA}/${FILENAME}.csv"
  echo "Done → ${EXTERNAL_DEST}/${FILENAME}.csv (symlinked into ${REPO_DATA})"
else
  rm -f "${EXTERNAL_DEST}/${FILENAME}.zip"
  echo "Error: no data found for ${FILENAME} on data.binance.vision"
  exit 1
fi
