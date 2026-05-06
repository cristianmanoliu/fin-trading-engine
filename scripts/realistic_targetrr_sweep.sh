#!/usr/bin/env bash
# realistic_targetrr_sweep.sh — Continuous 5y sweep across multiple target_rr
# values WITH realistic fees + stop slippage. Formal falsification of the
# hypothesis that some target_rr setting clears costs.
#
# Usage:    bash scripts/realistic_targetrr_sweep.sh [output_file]
# Override: TARGET_RRS="2.0 3.0 4.0 5.0"  FEE_BPS=8  SLIP_BPS=5
set -euo pipefail

ROOT="$(cd "$(dirname "$0")/.." && pwd)"
OUT="${1:-results/option_c_targetrr_realistic_$(date +%F).txt}"
BINARY=$(mktemp /tmp/rrr-bin.XXXXXXXX)
WORKDIR=$(mktemp -d /tmp/rrr-sweep.XXXXXXXX)
trap 'rm -f "$BINARY"; rm -rf "$WORKDIR"' EXIT

TARGET_RRS="${TARGET_RRS:-2.0 3.0 4.0 5.0}"
FEE_BPS="${FEE_BPS:-8}"
SLIP_BPS="${SLIP_BPS:-5}"

START_YEAR=2020
END_YEAR=2025
END_YEAR_MONTH=04

source "$(dirname "${BASH_SOURCE[0]}")/lib/symbols.sh"
SYMBOLS="${SYMBOLS:-$(get_symbols universe)}"

NCPU=$(sysctl -n hw.logicalcpu 2>/dev/null || nproc 2>/dev/null || echo 4)

echo "→ Compiling binary..."
(cd "$ROOT" && go build -o "$BINARY" ./cmd/backtest)

# Concatenate each symbol's CSVs ONCE, reused across target_rr values.
echo "→ Building merged CSVs (one-time)..."
for symbol in $SYMBOLS; do
    merged="${WORKDIR}/${symbol}.csv"
    for (( y=START_YEAR; y<=END_YEAR; y++ )); do
        last=$(( y == END_YEAR ? 10#$END_YEAR_MONTH : 12 ))
        for (( m=1; m<=last; m++ )); do
            mm=$(printf '%02d' "$m")
            csv="${ROOT}/data/${symbol}-1m-${y}-${mm}.csv"
            [[ -f "$csv" ]] && cat "$csv" >> "$merged"
        done
    done
done
echo "→ Sweeping target_rr=[${TARGET_RRS}] across 57 symbols (NCPU=${NCPU})  fees=${FEE_BPS}bp slip=${SLIP_BPS}bp"

run_symbol() {
    local symbol=$1
    local target_rr=$2
    local merged="${WORKDIR}/${symbol}.csv"

    if [[ ! -s "$merged" ]]; then
        echo "${symbol} 0 0 0 0 0"
        return
    fi

    local cfg
    cfg=$(mktemp "${WORKDIR}/cfg-${symbol}-${target_rr}.XXXXXXXX")
    sed "
        s|momentum_mode:.*|momentum_mode: false|;
        s|vwap_deviation_mode:.*|vwap_deviation_mode: false|;
        s|ema_mode:.*|ema_mode: true|;
        s|target_rr:.*|target_rr: ${target_rr}|;
        s|csv_path:.*|csv_path: ${merged}|
    " "${ROOT}/configs/default.yaml" > "$cfg"

    local result
    result=$("$BINARY" --config "$cfg" \
        --exact-fills --include-boundary --pessimistic-ambiguous \
        --fee-bps "$FEE_BPS" --stop-slippage-bps "$SLIP_BPS" 2>&1 \
        | jq -r 'select(.msg | test("SUMMARY")) | "\(.total_trades) \(.wins) \(.total_pnl_usd // 0) \(.gross_pnl_usd // 0) \(.total_fees_usd // 0)"' \
        2>/dev/null | tail -1)

    rm -f "$cfg"
    echo "${symbol} ${result:-0 0 0 0 0}"
}
export -f run_symbol
export ROOT BINARY WORKDIR FEE_BPS SLIP_BPS

{
echo ""
echo "════════════════════════════════════════════════════════════════════════════════"
echo "  OPTION C — EMA9×EMA21  REALISTIC target_rr sweep  (continuous 5y, \$1k stake)"
echo "  Flags: --exact-fills --include-boundary --pessimistic-ambiguous"
echo "         --fee-bps=${FEE_BPS}  --stop-slippage-bps=${SLIP_BPS}"
echo "  Period: 2020-01 → 2025-04  |  57 symbols"
echo "════════════════════════════════════════════════════════════════════════════════"
echo ""
printf "%-9s  %8s  %5s  %8s  %12s  %12s  %12s  %s\n" \
    "target_rr" "trades" "win%" "breakevn" "Gross_\$" "Fees_\$" "NET_\$" "profitable"
printf "%-9s  %8s  %5s  %8s  %12s  %12s  %12s  %s\n" \
    "─────────" "──────" "────" "────────" "────────" "──────" "─────" "──────────"

for target_rr in $TARGET_RRS; do
    RAW=$(printf '%s\n' $SYMBOLS \
        | xargs -P "$NCPU" -I{} bash -c "run_symbol '{}' '$target_rr'")

    echo "$RAW" | awk -v rr="$target_rr" '
    BEGIN { t=0; w=0; net=0; gross=0; fees=0; n=0; pos=0 }
    {
        t+=$2; w+=$3; net+=$4; gross+=$5; fees+=$6; n++
        if ($4>0) pos++
    }
    END {
        wp = (t>0) ? sprintf("%.1f%%", w/t*100) : "—"
        beven = 1.0/(1.0+rr) * 100
        printf "%-9s  %8d  %5s  %7.1f%%  %+11.0f  %11.0f  %+11.0f  %d/%d profitable\n",
            rr, t, wp, beven, gross, fees, net, pos, n
    }
    '
done

echo ""
echo "════════════════════════════════════════════════════════════════════════════════"
echo "  Verdict: any target_rr where NET >= 0 indicates a parameter region that"
echo "  clears realistic costs. If all rows are negative, the strategy family is"
echo "  not viable at any of the tested RR settings."
echo "════════════════════════════════════════════════════════════════════════════════"

} | tee "$OUT"

echo ""
echo "→ Output: $OUT"
