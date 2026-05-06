#!/usr/bin/env bash
# continuous_sweep.sh — Run each symbol as a single continuous 5-year backtest
# (no monthly restart, no force-close at month boundaries).
# Eliminates the monthly-segmentation force-close bias measured in audit Phase 4.
#
# Usage: bash scripts/continuous_sweep.sh [output_file]
# Output: tab-separated symbol / trades / wins / total_usd, then sorted summary
set -euo pipefail

ROOT="$(cd "$(dirname "$0")/.." && pwd)"
OUT="${1:-results/option_c_57sym_continuous_$(date +%F).txt}"
BINARY=$(mktemp /tmp/cont-bin.XXXXXXXX)
WORKDIR=$(mktemp -d /tmp/cont-sweep.XXXXXXXX)
trap 'rm -f "$BINARY"; rm -rf "$WORKDIR"' EXIT

START_YEAR=2020
END_YEAR=2025
END_YEAR_MONTH=04

source "$(dirname "${BASH_SOURCE[0]}")/lib/symbols.sh"
SYMBOLS="${SYMBOLS:-$(get_symbols universe)}"

NCPU=$(sysctl -n hw.logicalcpu 2>/dev/null || nproc 2>/dev/null || echo 4)

SED_BASE="s|momentum_mode:.*|momentum_mode: false|; s|vwap_deviation_mode:.*|vwap_deviation_mode: false|; s|ema_mode:.*|ema_mode: true|; s|target_rr:.*|target_rr: 5.0|"

echo "→ Compiling binary..."
(cd "$ROOT" && go build -o "$BINARY" ./cmd/backtest)
echo "→ Running continuous 5y backtest for 57 symbols (NCPU=$NCPU)..."
echo "→ Output: $OUT"

run_symbol() {
    local symbol=$1
    local merged="${WORKDIR}/${symbol}.csv"

    # Concatenate all monthly CSVs in chronological order
    local found=0
    for (( y=START_YEAR; y<=END_YEAR; y++ )); do
        local last=$(( y == END_YEAR ? 10#$END_YEAR_MONTH : 12 ))
        for (( m=1; m<=last; m++ )); do
            local mm
            mm=$(printf '%02d' "$m")
            local csv="${ROOT}/data/${symbol}-1m-${y}-${mm}.csv"
            if [[ -f "$csv" ]]; then
                cat "$csv" >> "$merged"
                found=1
            fi
        done
    done

    if [[ $found -eq 0 ]]; then
        echo "${symbol} 0 0 0"
        return
    fi

    # Build per-run config pointing at the merged CSV
    local cfg
    cfg=$(mktemp "${WORKDIR}/cfg-${symbol}.XXXXXXXX")
    sed "${SED_BASE}; s|csv_path:.*|csv_path: ${merged}|" \
        "${ROOT}/configs/default.yaml" > "$cfg"

    # Run the continuous backtest
    local result
    result=$("$BINARY" --config "$cfg" \
        --exact-fills --include-boundary --pessimistic-ambiguous 2>&1 \
        | jq -r 'select(.msg | test("SUMMARY")) | "\(.total_trades) \(.wins) \(.total_pnl_usd // 0)"' \
        2>/dev/null | tail -1)

    rm -f "$cfg" "$merged"
    echo "${symbol} ${result:-0 0 0}"
}
export -f run_symbol
export ROOT BINARY WORKDIR SED_BASE START_YEAR END_YEAR END_YEAR_MONTH

# Run all symbols in parallel and collect raw results
RAW=$(printf '%s\n' $SYMBOLS \
    | xargs -P "$NCPU" -I{} bash -c 'run_symbol "{}"')

# Sort by total_usd descending and format report
{
echo ""
echo "════════════════════════════════════════════════════════════════════"
echo "  OPTION C — EMA9×EMA21  target_rr=5.0  CONTINUOUS 5y backtest"
echo "  (no monthly restart — eliminates force-close segmentation bias)"
echo "  Flags: --exact-fills --include-boundary --pessimistic-ambiguous"
echo "  Period: 2020-01 → 2025-04  |  \$1k stake per trade"
echo "════════════════════════════════════════════════════════════════════"
echo ""
printf "%-18s %8s %6s %6s %12s\n" "Symbol" "Trades" "Wins" "WinPct" "Total_\$1k"
printf "%-18s %8s %6s %6s %12s\n" "──────" "──────" "────" "──────" "─────────"

# Parse, compute win%, sort descending by total_usd
echo "$RAW" \
    | awk '{
        sym=$1; t=$2; w=$3; u=$4
        if (t>0) wp = sprintf("%.1f%%", w/t*100); else wp="–"
        printf "%s\t%d\t%d\t%s\t%d\n", sym, t, w, wp, int(u/1000)
      }' \
    | sort -t$'\t' -k5 -rn \
    | awk -F'\t' '{
        sign = ($5 >= 0) ? "+" : ""
        printf "%-18s %8d %6d %6s %s%8dk\n", $1, $2, $3, $4, sign, $5
      }'

echo ""

# Aggregate totals
echo "$RAW" | awk '
BEGIN { t=0; w=0; u=0; n=0; pos=0 }
{
    t+=$2; w+=$3; u+=$4; n++
    if ($4>0) pos++
}
END {
    printf "────────────────────────────────────────────────────────────────────\n"
    printf "TOTAL  %d symbols | %d trades | %d wins (%.1f%%) | %+.0f$ total\n",
        n, t, w, (t>0?w/t*100:0), u
    printf "       %d/%d profitable | avg %+.0f$/symbol\n", pos, n, u/n
}
'

} | tee "$OUT"

echo ""
echo "→ Full results saved to: $OUT"
