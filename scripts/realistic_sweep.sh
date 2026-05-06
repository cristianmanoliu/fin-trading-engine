#!/usr/bin/env bash
# realistic_sweep.sh — Continuous 5-year sweep WITH realistic execution costs.
# Same methodology as continuous_sweep.sh but applies fees + stop slippage.
#
# Defaults: --fee-bps=8 (Binance Futures taker × 2 round-trip)
#           --stop-slippage-bps=5 (stop-market fills past trigger)
# Override via env: FEE_BPS=10 SLIP_BPS=8 bash scripts/realistic_sweep.sh
#
# Usage: bash scripts/realistic_sweep.sh [output_file]
set -euo pipefail

ROOT="$(cd "$(dirname "$0")/.." && pwd)"
OUT="${1:-results/option_c_57sym_realistic_$(date +%F).txt}"
BINARY=$(mktemp /tmp/real-bin.XXXXXXXX)
WORKDIR=$(mktemp -d /tmp/real-sweep.XXXXXXXX)
trap 'rm -f "$BINARY"; rm -rf "$WORKDIR"' EXIT

FEE_BPS="${FEE_BPS:-8}"
SLIP_BPS="${SLIP_BPS:-5}"

START_YEAR=2020
END_YEAR=2025
END_YEAR_MONTH=04

source "$(dirname "${BASH_SOURCE[0]}")/lib/symbols.sh"
SYMBOLS="${SYMBOLS:-$(get_symbols universe)}"

NCPU=$(sysctl -n hw.logicalcpu 2>/dev/null || nproc 2>/dev/null || echo 4)

SED_BASE="s|momentum_mode:.*|momentum_mode: false|; s|vwap_deviation_mode:.*|vwap_deviation_mode: false|; s|ema_mode:.*|ema_mode: true|; s|target_rr:.*|target_rr: 5.0|"

echo "→ Compiling binary..."
(cd "$ROOT" && go build -o "$BINARY" ./cmd/backtest)
echo "→ Running REALISTIC continuous 5y backtest for 57 symbols (NCPU=$NCPU)..."
echo "→ Costs: fee_bps=${FEE_BPS} slip_bps=${SLIP_BPS}"
echo "→ Output: $OUT"

run_symbol() {
    local symbol=$1
    local merged="${WORKDIR}/${symbol}.csv"

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
        echo "${symbol} 0 0 0 0 0"
        return
    fi

    local cfg
    cfg=$(mktemp "${WORKDIR}/cfg-${symbol}.XXXXXXXX")
    sed "${SED_BASE}; s|csv_path:.*|csv_path: ${merged}|" \
        "${ROOT}/configs/default.yaml" > "$cfg"

    # Extract: total_trades, wins, NET total_pnl_usd, gross_pnl_usd, total_fees_usd
    local result
    result=$("$BINARY" --config "$cfg" \
        --exact-fills --include-boundary --pessimistic-ambiguous \
        --fee-bps "$FEE_BPS" --stop-slippage-bps "$SLIP_BPS" 2>&1 \
        | jq -r 'select(.msg | test("SUMMARY")) | "\(.total_trades) \(.wins) \(.total_pnl_usd // 0) \(.gross_pnl_usd // 0) \(.total_fees_usd // 0)"' \
        2>/dev/null | tail -1)

    rm -f "$cfg" "$merged"
    echo "${symbol} ${result:-0 0 0 0 0}"
}
export -f run_symbol
export ROOT BINARY WORKDIR SED_BASE START_YEAR END_YEAR END_YEAR_MONTH FEE_BPS SLIP_BPS

RAW=$(printf '%s\n' $SYMBOLS \
    | xargs -P "$NCPU" -I{} bash -c 'run_symbol "{}"')

{
echo ""
echo "════════════════════════════════════════════════════════════════════════════════════════"
echo "  OPTION C — EMA9×EMA21  target_rr=5.0  REALISTIC CONTINUOUS 5y"
echo "  Flags: --exact-fills --include-boundary --pessimistic-ambiguous"
echo "         --fee-bps=${FEE_BPS}  --stop-slippage-bps=${SLIP_BPS}"
echo "  Period: 2020-01 → 2025-04  |  \$1k stake per trade"
echo "════════════════════════════════════════════════════════════════════════════════════════"
echo ""
printf "%-18s %8s %6s %6s %12s %12s %12s\n" "Symbol" "Trades" "Wins" "WinPct" "Gross_\$1k" "Fees_\$1k" "NET_\$1k"
printf "%-18s %8s %6s %6s %12s %12s %12s\n" "──────" "──────" "────" "──────" "─────────" "────────" "───────"

echo "$RAW" \
    | awk '{
        sym=$1; t=$2; w=$3; net=$4; gross=$5; fees=$6
        if (t>0) wp = sprintf("%.1f%%", w/t*100); else wp="–"
        printf "%s\t%d\t%d\t%s\t%d\t%d\t%d\n", sym, t, w, wp, int(gross/1000), int(fees/1000), int(net/1000)
      }' \
    | sort -t$'\t' -k7 -rn \
    | awk -F'\t' '{
        gsign = ($5 >= 0) ? "+" : ""
        nsign = ($7 >= 0) ? "+" : ""
        printf "%-18s %8d %6d %6s %s%8dk %8dk %s%8dk\n", $1, $2, $3, $4, gsign, $5, $6, nsign, $7
      }'

echo ""

echo "$RAW" | awk '
BEGIN { t=0; w=0; net=0; gross=0; fees=0; n=0; pos=0 }
{
    t+=$2; w+=$3; net+=$4; gross+=$5; fees+=$6; n++
    if ($4>0) pos++
}
END {
    printf "────────────────────────────────────────────────────────────────────────────────────────\n"
    printf "TOTAL  %d symbols | %d trades | %d wins (%.1f%%)\n", n, t, w, (t>0?w/t*100:0)
    printf "       Gross:    %+.0f$\n", gross
    printf "       Fees:     %.0f$\n", fees
    printf "       NET:      %+.0f$\n", net
    printf "       %d/%d profitable | avg %+.0f$/symbol (net)\n", pos, n, net/n
}
'

} | tee "$OUT"

echo ""
echo "→ Full results saved to: $OUT"
