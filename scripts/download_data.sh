#!/usr/bin/env bash
set -euo pipefail

SYMBOL="${1:-BTCUSDT}"
YEAR="${2:-2024}"
MONTH="${3:-01}"

FILENAME="${SYMBOL}-1m-${YEAR}-$(printf '%02d' "$MONTH")"
URL="https://data.binance.vision/data/futures/um/monthly/klines/${SYMBOL}/1m/${FILENAME}.zip"
DEST="$(dirname "$0")/../data"

mkdir -p "$DEST"

echo "Downloading ${FILENAME}.zip ..."
if curl -f -# -L "$URL" -o "${DEST}/${FILENAME}.zip" 2>/dev/null; then
  echo "Unzipping ..."
  unzip -o "${DEST}/${FILENAME}.zip" -d "$DEST"
  rm "${DEST}/${FILENAME}.zip"
  echo "Done → ${DEST}/${FILENAME}.csv"
else
  rm -f "${DEST}/${FILENAME}.zip"
  echo "Error: no data found for ${FILENAME} on data.binance.vision"
  exit 1
fi
