#!/usr/bin/env bash
# p4_targetrr_sweep.sh — sweeps target_rr for the P4 winner (4H + wick stop)
# under realistic costs. Identifies the optimal RR setting.
#
# Configurable: TARGET_RRS (default "2.0 3.0 4.0 5.0 6.0 7.0"),
#               SIGNAL_TF (4H), ATR_MULT (0 = wick), FEE_BPS (8), SLIP_BPS (5).
set -euo pipefail

ROOT="$(cd "$(dirname "$0")/.." && pwd)"
SIGNAL_TF="${SIGNAL_TF:-4H}"
ATR_MULT="${ATR_MULT:-0}"
TARGET_RRS="${TARGET_RRS:-2.0 3.0 4.0 5.0 6.0 7.0}"
FEE_BPS="${FEE_BPS:-10}"
SLIP_BPS="${SLIP_BPS:-5}"
FUNDING_BPS="${FUNDING_BPS:-0}"
TAX_RATE="${TAX_RATE:-0}"

LABEL="${SIGNAL_TF}_$([ "$ATR_MULT" = "0" ] && echo wick || echo "atr${ATR_MULT}")"
OUT="${1:-results/proto_targetrr_${LABEL}_$(date +%F).txt}"

BINARY=$(mktemp /tmp/p4rr-bin.XXXXXXXX)
WORKDIR=$(mktemp -d /tmp/p4rr-sweep.XXXXXXXX)
trap 'rm -f "$BINARY"; rm -rf "$WORKDIR"' EXIT

START_YEAR=2020
END_YEAR=2025
END_YEAR_MONTH=04

source "$(dirname "${BASH_SOURCE[0]}")/lib/symbols.sh"
SYMBOLS="${SYMBOLS:-$(get_symbols universe)}"

NCPU=$(sysctl -n hw.logicalcpu 2>/dev/null || nproc 2>/dev/null || echo 4)

echo "→ Compiling..."
(cd "$ROOT" && go build -o "$BINARY" ./cmd/backtest)

echo "→ Building merged CSVs (one-time, reused across target_rr values)..."
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
echo "→ Sweeping target_rr=[${TARGET_RRS}]  signal_tf=${SIGNAL_TF}  atr_mult=${ATR_MULT}"

ATR_ARG=""
[[ "$ATR_MULT" != "0" ]] && ATR_ARG="--atr-stop-mult $ATR_MULT"

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
        --fee-bps "$FEE_BPS" --stop-slippage-bps "$SLIP_BPS" \
        --funding-bps-per-day "$FUNDING_BPS" --tax-rate-pct "$TAX_RATE" \
        --signal-tf "$SIGNAL_TF" $ATR_ARG 2>&1 \
        | jq -r 'select(.msg | test("SUMMARY")) | "\(.total_trades) \(.wins) \(.total_pnl_usd // 0) \(.gross_pnl_usd // 0) \(.total_fees_usd // 0) \(.total_funding_usd // 0) \(.after_tax_pnl_usd // .total_pnl_usd // 0)"' \
        2>/dev/null | tail -1)

    rm -f "$cfg"
    echo "${symbol} ${result:-0 0 0 0 0 0 0}"
}
export -f run_symbol
export ROOT BINARY WORKDIR FEE_BPS SLIP_BPS FUNDING_BPS TAX_RATE SIGNAL_TF ATR_ARG

{
echo ""
echo "════════════════════════════════════════════════════════════════════════════════"
echo "  P4-style target_rr sweep   signal_tf=${SIGNAL_TF}  atr_mult=${ATR_MULT}"
echo "  57 symbols, continuous 5y; costs: ${FEE_BPS}bp fee + ${SLIP_BPS}bp slip + ${FUNDING_BPS}bp/day funding + ${TAX_RATE}% tax"
echo "════════════════════════════════════════════════════════════════════════════════"
echo ""
printf "%-9s  %7s  %5s  %7s  %11s  %11s  %11s  %12s  %12s  %s\n" \
    "target_rr" "trades" "win%" "breakvn" "Gross_\$" "Fees_\$" "Funding_\$" "NET(pretax)" "AfterTax_\$" "profitable"
printf "%-9s  %7s  %5s  %7s  %11s  %11s  %11s  %12s  %12s  %s\n" \
    "─────────" "──────" "────" "───────" "────────" "──────" "─────────" "──────────" "──────────" "──────────"

for target_rr in $TARGET_RRS; do
    RAW=$(printf '%s\n' $SYMBOLS \
        | xargs -P "$NCPU" -I{} bash -c "run_symbol '{}' '$target_rr'")
    echo "$RAW" | awk -v rr="$target_rr" '
    BEGIN { t=0; w=0; net=0; gross=0; fees=0; funding=0; aftertax=0; n=0; pos=0 }
    { t+=$2; w+=$3; net+=$4; gross+=$5; fees+=$6; funding+=$7; aftertax+=$8; n++; if ($8>0) pos++ }
    END {
        wp = (t>0) ? sprintf("%.1f%%", w/t*100) : "—"
        beven = 1.0/(1.0+rr) * 100
        printf "%-9s  %7d  %5s  %6.1f%%  %+10.0f  %10.0f  %10.0f  %+11.0f  %+11.0f  %d/%d\n",
            rr, t, wp, beven, gross, fees, funding, net, aftertax, pos, n
    }
    '
done

echo ""
echo "════════════════════════════════════════════════════════════════════════════════"
echo "  The optimal target_rr is the one with highest NET. Watch trade count:"
echo "  if it drops <5,000 the result is statistically thin and may not generalize."
echo "════════════════════════════════════════════════════════════════════════════════"

} | tee "$OUT"

echo ""
echo "→ Output: $OUT"
