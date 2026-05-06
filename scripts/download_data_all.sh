#!/usr/bin/env bash
# download_data_all.sh — Batch-recover the full market-data corpus from Binance's
# public archive at data.binance.vision.
#
# This is the recovery script for the 12 GB of 1m kline CSVs deliberately
# excluded from git (see docs/DATA_RECOVERY.md and .gitignore).
#
# Coverage: all 57 symbols × 2020-01 through 2025-04 (whatever Binance has
# available — symbols listed mid-period only have data from their listing month).
# Output: data/{SYMBOL}-1m-{YYYY}-{MM}.csv (extracted from the .zip archive).
#
# Usage:
#   ./scripts/download_data_all.sh              # all 57 symbols, 2020-01..2025-04
#   ./scripts/download_data_all.sh "BTCUSDT ETHUSDT" 2024 01 12  # subset
#   PARALLEL=20 ./scripts/download_data_all.sh  # crank concurrency
#
# Args (all optional):
#   $1 — space-separated symbols  (default: full 57-symbol universe)
#   $2 — start year                (default: 2020)
#   $3 — start month               (default: 1)
#   $4 — end year                  (default: 2025)
#   $5 — end month                 (default: 4)
#
# Env:
#   PARALLEL — concurrent downloads (default: 8). Higher = faster, but Binance
#              may rate-limit at 20+. 8 is safe.
#
# Skips files that already exist on disk (idempotent — re-run after a partial
# download to fill in misses).
#
# Expected runtime: ~5-15 min on a 50+ Mbps connection at PARALLEL=8.
# Final disk usage: ~12 GB.
set -euo pipefail

ROOT="$(cd "$(dirname "$0")/.." && pwd)"
DEST="${ROOT}/data"
PARALLEL="${PARALLEL:-8}"

source "$(dirname "${BASH_SOURCE[0]}")/lib/symbols.sh"
DEFAULT_SYMBOLS="$(get_symbols universe)"

SYMBOLS="${1:-$DEFAULT_SYMBOLS}"
START_YEAR="${2:-2020}"
START_MONTH="${3:-1}"
END_YEAR="${4:-2025}"
END_MONTH="${5:-4}"

mkdir -p "$DEST"

echo "→ Downloading market data"
echo "  symbols:    $(echo $SYMBOLS | wc -w | tr -d ' ') symbol(s)"
echo "  date range: ${START_YEAR}-$(printf '%02d' $START_MONTH) → ${END_YEAR}-$(printf '%02d' $END_MONTH)"
echo "  parallel:   $PARALLEL concurrent"
echo "  destination:$DEST"
echo ""

# Build the work list: one (symbol, year, month) tuple per line.
JOBS=$(mktemp /tmp/dl-jobs.XXXXXXXX)
trap 'rm -f "$JOBS"' EXIT

for sym in $SYMBOLS; do
    for (( y=START_YEAR; y<=END_YEAR; y++ )); do
        local_first=1
        local_last=12
        if [[ $y -eq $START_YEAR ]]; then local_first=$START_MONTH; fi
        if [[ $y -eq $END_YEAR ]]; then local_last=$END_MONTH; fi
        for (( m=local_first; m<=local_last; m++ )); do
            mm=$(printf '%02d' "$m")
            csv="${DEST}/${sym}-1m-${y}-${mm}.csv"
            # Skip if already on disk (idempotent).
            [[ -f "$csv" ]] && continue
            echo "$sym $y $mm" >> "$JOBS"
        done
    done
done

TOTAL=$(wc -l < "$JOBS" | tr -d ' ')
echo "→ $TOTAL file(s) to fetch (skipping any already on disk)"
echo ""

if [[ "$TOTAL" -eq 0 ]]; then
    echo "✓ Nothing to download. All requested files already exist."
    exit 0
fi

# fetch_one: download + unzip a single (symbol, year, month). Used by xargs.
# Silent on success; warns on miss (some symbols don't exist before their listing month).
fetch_one() {
    local sym=$1 year=$2 mm=$3
    local fname="${sym}-1m-${year}-${mm}"
    local url="https://data.binance.vision/data/futures/um/monthly/klines/${sym}/1m/${fname}.zip"
    local zip="${DEST}/${fname}.zip"

    if curl -fsS -L "$url" -o "$zip" 2>/dev/null; then
        unzip -qq -o "$zip" -d "$DEST"
        rm -f "$zip"
        echo "  ✓ ${fname}"
    else
        rm -f "$zip"
        # Mid-period listings have no data for early months — expected, not an error.
        echo "  ✗ ${fname}  (no archive — symbol probably not listed yet)"
    fi
}
export -f fetch_one
export DEST

# xargs runs PARALLEL workers; preserves output ordering loosely via line-buffering.
< "$JOBS" xargs -P "$PARALLEL" -L 1 bash -c 'fetch_one "$0" "$1" "$2"'

echo ""
echo "→ Done. Final disk usage:"
du -sh "$DEST" | sed 's/^/  /'
echo ""
echo "→ File counts per top-5 symbol (sanity check):"
for sym in BTCUSDT ETHUSDT SOLUSDT BNBUSDT XRPUSDT; do
    count=$(ls "${DEST}/${sym}-1m-"*.csv 2>/dev/null | wc -l | tr -d ' ')
    echo "  $sym: $count month(s)"
done
