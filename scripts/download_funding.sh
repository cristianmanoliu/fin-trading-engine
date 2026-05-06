#!/usr/bin/env bash
# download_funding.sh — Downloads Binance USDT-M perpetual funding-rate history
# per symbol from the public REST API and saves as CSV files in data/funding/.
#
# Endpoint: GET /fapi/v1/fundingRate?symbol=X&startTime=Y&endTime=Z&limit=1000
# Response: array of {symbol, fundingTime, fundingRate, markPrice}
# Funding events occur every 8h. 5 years = ~5,475 events per symbol.
#
# Output: data/funding/{SYMBOL}.csv with columns: funding_time_ms, funding_rate
# The fundingRate is a signed decimal (e.g. 0.0001 = +0.01% per 8h period).
#
# Usage:
#   bash scripts/download_funding.sh           # all default symbols
#   SYMBOLS="BTCUSDT ETHUSDT" bash scripts/download_funding.sh
set -euo pipefail

ROOT="$(cd "$(dirname "$0")/.." && pwd)"
OUTDIR="${ROOT}/data/funding"
mkdir -p "$OUTDIR"

API_BASE="${API_BASE:-https://fapi.binance.com}"

# Default: same 57 symbols used in proto_sweep.sh
source "$(dirname "${BASH_SOURCE[0]}")/lib/symbols.sh"
SYMBOLS="${SYMBOLS:-$(get_symbols universe)}"

# Pull from 2020-01-01 onwards. Use seconds × 1000 for macOS-compat (BSD date has no %N).
START_MS=1577836800000
END_MS=$(($(date +%s) * 1000))

download_symbol() {
    local symbol=$1
    local outfile="${OUTDIR}/${symbol}.csv"
    local tmp_json
    tmp_json=$(mktemp /tmp/fund-${symbol}.XXXXXXXX.json)

    local cur_start=$START_MS
    local total_rows=0
    local appending=0

    # Incremental refresh: when INCREMENTAL=1 AND file exists, resume from last entry.
    if [[ "${INCREMENTAL:-0}" == "1" && -s "$outfile" ]]; then
        local last_ms
        last_ms=$(awk -F',' 'NR>1 && $1+0>0 {print $1}' "$outfile" | sort -n | tail -1 | tr -d '"')
        if [[ -n "$last_ms" && "$last_ms" -gt 0 ]]; then
            cur_start=$((last_ms + 1))
            appending=1
            local existing
            existing=$(($(wc -l < "$outfile") - 1))
            echo "  [${symbol}] incremental from $(date -u -r $((last_ms/1000)) '+%Y-%m-%dT%H:%M:%SZ' 2>/dev/null || echo $last_ms) (${existing} existing rows)" >&2
        fi
    fi

    # Full refresh: file missing OR INCREMENTAL=0 → recreate header
    if [[ "$appending" == "0" ]]; then
        if [[ -s "$outfile" && "${INCREMENTAL:-0}" != "1" ]]; then
            local lines
            lines=$(wc -l < "$outfile" | tr -d ' ')
            echo "  [${symbol}] cached (${lines} rows) — set INCREMENTAL=1 to refresh" >&2
            return 0
        fi
        echo "funding_time_ms,funding_rate" > "$outfile"
    fi
    while true; do
        # Binance limit: 1000 events per call. 1 event per 8h = 333 days per call.
        local resp
        resp=$(curl -fsS -m 30 \
            "${API_BASE}/fapi/v1/fundingRate?symbol=${symbol}&startTime=${cur_start}&endTime=${END_MS}&limit=1000" \
            -o "$tmp_json" -w "%{http_code}" 2>&1) || {
            local http_code=$?
            echo "  [${symbol}] HTTP error: ${resp}" >&2
            rm -f "$tmp_json"
            return 1
        }

        # Validate JSON array
        local count
        count=$(jq 'length' "$tmp_json" 2>/dev/null || echo "0")
        if [[ "$count" -eq 0 ]]; then
            break
        fi

        # Extract rows
        jq -r '.[] | [.fundingTime, .fundingRate] | @csv' "$tmp_json" >> "$outfile"
        total_rows=$((total_rows + count))

        # Advance: next page starts after the last fundingTime
        local last_time
        last_time=$(jq -r '.[-1].fundingTime' "$tmp_json")
        if [[ "$count" -lt 1000 ]]; then
            break
        fi
        cur_start=$((last_time + 1))

        # Be polite to the API
        sleep 0.25
    done

    rm -f "$tmp_json"
    echo "  [${symbol}] downloaded ${total_rows} rows → ${outfile}" >&2
}
export -f download_symbol
export OUTDIR API_BASE START_MS END_MS

# Limit concurrency to 4 to respect rate limits (~500 req/min budget)
printf '%s\n' $SYMBOLS | xargs -P 4 -I{} bash -c 'download_symbol "{}"'

echo ""
echo "→ Funding data ready in: ${OUTDIR}"
echo "→ Per-symbol counts:"
for s in $SYMBOLS; do
    f="${OUTDIR}/${s}.csv"
    if [[ -f "$f" ]]; then
        n=$(($(wc -l < "$f") - 1))
        printf "    %-18s %6d events\n" "$s" "$n"
    fi
done
