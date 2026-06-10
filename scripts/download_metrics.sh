#!/usr/bin/env bash
# download_metrics.sh — fetch Binance UM futures METRICS (OI, L/S ratios, taker ratio).
#
# Unlocks candidates #1 (OI divergence), #2 (L/S extreme fade), #4-v2 (taker ratio),
# #6 (OI-confirmed breakout), #9 (positioning-stress composite). Per
# `results/strategy_candidates_2026-06-10.md` Phase 1 + `tasks/todo.md`.
#
# Source: data.binance.vision/data/futures/um/daily/metrics/<SYM>/ (daily zips,
# 5-min granularity rows, history from 2020-09). Columns:
#   create_time,symbol,sum_open_interest,sum_open_interest_value,
#   count_toptrader_long_short_ratio,sum_toptrader_long_short_ratio,
#   count_long_short_ratio,sum_taker_long_short_vol_ratio
#
# Output: data/metrics/<SYM>.csv (concatenated, header kept once). Idempotent:
# tracks the last-fetched date per symbol so re-runs only pull new days.
#
# Usage: download_metrics.sh [SYM1 SYM2 ...]   (default: deployed-16)
set -uo pipefail

BASE="https://data.binance.vision/data/futures/um/daily/metrics"
LISTBASE="https://s3-ap-northeast-1.amazonaws.com/data.binance.vision?delimiter=/&prefix=data/futures/um/daily/metrics"
OUT="data/metrics"
TMP="$(mktemp -d)"; trap 'rm -rf "$TMP"' EXIT
mkdir -p "$OUT" "$TMP/ex"

if [ "$#" -gt 0 ]; then
  SYMS=("$@")
else
  SYMS=(ROSEUSDT BCHUSDT GRTUSDT 1INCHUSDT ADAUSDT KAVAUSDT 1000SHIBUSDT ENSUSDT \
        XLMUSDT IMXUSDT ETCUSDT RUNEUSDT AVAXUSDT APTUSDT DOTUSDT FILUSDT BTCUSDT \
        CHZUSDT SANDUSDT TRXUSDT)
fi

list_dates() {  # $1=sym -> all available YYYY-MM-DD (one per zip)
  curl -s -m 60 "${LISTBASE}/${1}/" \
    | grep -o '<Key>[^<]*</Key>' | sed 's/<[^>]*>//g' | grep -v CHECKSUM \
    | grep -oE '[0-9]{4}-[0-9]{2}-[0-9]{2}' | sort -u
}

for sym in "${SYMS[@]}"; do
  dest="$OUT/${sym}.csv"
  have_last=""
  [ -s "$dest" ] && have_last=$(tail -1 "$dest" | cut -d, -f1 | cut -d' ' -f1)
  echo "[$sym] last-have=${have_last:-none}; listing dates..."
  dates=$(list_dates "$sym")
  [ -z "$dates" ] && { echo "  no metrics archive"; continue; }
  n=0; got=0
  while read -r d; do
    [ -z "$d" ] && continue
    n=$((n+1))
    # skip dates we already have
    if [ -n "$have_last" ] && [[ "$d" < "$have_last" || "$d" == "$have_last" ]]; then continue; fi
    z="$TMP/m.zip"
    if curl -s -m 30 -f "${BASE}/${sym}/${sym}-metrics-${d}.zip" -o "$z" 2>/dev/null; then
      unzip -o -q "$z" -d "$TMP/ex" 2>/dev/null || continue
      f=$(ls "$TMP/ex"/*.csv 2>/dev/null | head -1); [ -z "$f" ] && continue
      if [ ! -s "$dest" ]; then
        cat "$f" >> "$dest"            # first write: keep header
      else
        tail -n +2 "$f" >> "$dest"     # subsequent: strip header
      fi
      rm -f "$TMP/ex"/*.csv
      got=$((got+1))
    fi
  done <<< "$dates"
  echo "  [$sym] available=$n fetched=$got -> $dest ($(wc -l <"$dest" 2>/dev/null) rows)"
done
echo "DONE. metrics -> $OUT/"
