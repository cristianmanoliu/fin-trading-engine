#!/usr/bin/env bash
# run_p4_variant.sh — Continuous 5y × 57 symbols at signal_tf=4H, target_rr=6.0,
# wick stop, full realistic costs (10/5/3 bps + 0% tax). Accepts arbitrary extra
# args appended to the binary call so we can A/B prototype variants quickly.
#
# Usage:
#   bash scripts/run_p4_variant.sh "<label>" "<extra_args>" [output_file]
#   bash scripts/run_p4_variant.sh shorts_only "--side-filter short"
#   bash scripts/run_p4_variant.sh maxhold336 "--max-hold-hours 336"
set -euo pipefail

LABEL="${1:?usage: $0 <label> <extra_args> [outfile]}"
EXTRA_ARGS="${2:-}"
OUT="${3:-results/p4_${LABEL}_$(date +%F).txt}"

ROOT="$(cd "$(dirname "$0")/.." && pwd)"
BINARY=$(mktemp /tmp/var-bin.XXXXXXXX)
WORKDIR=$(mktemp -d /tmp/var-sweep.XXXXXXXX)
trap 'rm -f "$BINARY"; rm -rf "$WORKDIR"' EXIT

FEE_BPS="${FEE_BPS:-10}"
SLIP_BPS="${SLIP_BPS:-5}"
FUNDING_BPS="${FUNDING_BPS:-3}"
TAX_RATE="${TAX_RATE:-0}"
TARGET_RR="${TARGET_RR:-6.0}"
SIGNAL_TF="${SIGNAL_TF:-4H}"
FUNDING_DIR="${FUNDING_DIR:-}"

START_YEAR=2020
END_YEAR=2025
END_YEAR_MONTH=04

SYMBOLS="RUNEUSDT SOLUSDT BNBUSDT HBARUSDT IOTAUSDT TRXUSDT XLMUSDT LINKUSDT GRTUSDT FTMUSDT SNXUSDT APEUSDT BLURUSDT AAVEUSDT MKRUSDT VETUSDT ARBUSDT ATOMUSDT PYTHUSDT BTCUSDT ENJUSDT 1INCHUSDT NEARUSDT KAVAUSDT AVAXUSDT TIAUSDT ETHUSDT DOTUSDT XRPUSDT GMXUSDT BCHUSDT ZILUSDT LDOUSDT DYDXUSDT UNIUSDT DOGEUSDT SEIUSDT 1000SHIBUSDT IMXUSDT INJUSDT ROSEUSDT GALAUSDT MANAUSDT OPUSDT ICPUSDT AXSUSDT ENSUSDT SUIUSDT WLDUSDT ADAUSDT FILUSDT SANDUSDT CHZUSDT LTCUSDT APTUSDT ETCUSDT CRVUSDT"
NCPU=$(sysctl -n hw.logicalcpu 2>/dev/null || nproc 2>/dev/null || echo 4)

(cd "$ROOT" && go build -o "$BINARY" ./cmd/backtest)

run_one() {
    local symbol=$1
    local merged="${WORKDIR}/${symbol}.csv"
    for (( y=START_YEAR; y<=END_YEAR; y++ )); do
        local last=$(( y == END_YEAR ? 10#$END_YEAR_MONTH : 12 ))
        for (( m=1; m<=last; m++ )); do
            local mm
            mm=$(printf '%02d' "$m")
            local csv="${ROOT}/data/${symbol}-1m-${y}-${mm}.csv"
            [[ -f "$csv" ]] && cat "$csv" >> "$merged"
        done
    done
    if [[ ! -s "$merged" ]]; then
        echo "${symbol} 0 0 0 0 0 0 0"
        return
    fi
    local cfg
    cfg=$(mktemp "${WORKDIR}/cfg-${symbol}.XXXXXXXX")
    sed "
        s|momentum_mode:.*|momentum_mode: false|;
        s|vwap_deviation_mode:.*|vwap_deviation_mode: false|;
        s|ema_mode:.*|ema_mode: true|;
        s|target_rr:.*|target_rr: ${TARGET_RR}|;
        s|csv_path:.*|csv_path: ${merged}|
    " "${ROOT}/configs/default.yaml" > "$cfg"

    local funding_arg=""
    if [[ -n "$FUNDING_DIR" ]]; then
        funding_arg="--funding-csv-dir $FUNDING_DIR"
    fi
    local result
    result=$("$BINARY" --config "$cfg" \
        --exact-fills --include-boundary --pessimistic-ambiguous \
        --fee-bps "$FEE_BPS" --stop-slippage-bps "$SLIP_BPS" \
        --funding-bps-per-day "$FUNDING_BPS" --tax-rate-pct "$TAX_RATE" \
        $funding_arg \
        --signal-tf "$SIGNAL_TF" $EXTRA_ARGS 2>&1 \
        | jq -r 'select(.msg | test("SUMMARY")) | "\(.total_trades) \(.wins) \(.total_pnl_usd // 0) \(.gross_pnl_usd // 0) \(.total_fees_usd // 0) \(.total_funding_usd // 0) \(.long_net_usd // 0) \(.short_net_usd // 0)"' \
        2>/dev/null | tail -1)

    rm -f "$cfg" "$merged"
    echo "${symbol} ${result:-0 0 0 0 0 0 0 0}"
}
export -f run_one
export ROOT BINARY WORKDIR FEE_BPS SLIP_BPS FUNDING_BPS TAX_RATE TARGET_RR SIGNAL_TF EXTRA_ARGS FUNDING_DIR START_YEAR END_YEAR END_YEAR_MONTH

RAW=$(printf '%s\n' $SYMBOLS | xargs -P "$NCPU" -I{} bash -c 'run_one "{}"')

{
echo ""
echo "════════════════════════════════════════════════════════════════════════════════════════"
echo "  P4 variant: ${LABEL}"
echo "  Args:  --signal-tf ${SIGNAL_TF}  --target_rr=${TARGET_RR}  ${EXTRA_ARGS}"
echo "  Costs: fee=${FEE_BPS}bp slip=${SLIP_BPS}bp funding=${FUNDING_BPS}bp/day tax=${TAX_RATE}%"
echo "════════════════════════════════════════════════════════════════════════════════════════"
echo ""
echo "$RAW" | awk '
BEGIN { t=0; w=0; net=0; gross=0; fees=0; fund=0; lnet=0; snet=0; n=0; pos=0 }
{ t+=$2; w+=$3; net+=$4; gross+=$5; fees+=$6; fund+=$7; lnet+=$8; snet+=$9; n++; if ($4>0) pos++ }
END {
    wp = (t>0) ? sprintf("%.2f%%", w/t*100) : "—"
    printf "TOTAL  %d sym  %d trades  %s WR\n", n, t, wp
    printf "  Gross %+.0f  Fees %.0f  Funding %.0f  →  NET %+.0f  | %d/%d profitable\n", gross, fees, fund, net, pos, n
    printf "  Long_NET %+.0f   Short_NET %+.0f\n", lnet, snet
}
'

echo ""
echo "Top 15 by NET:"
echo "$RAW" | sort -k4 -rn | head -15 | awk '{ printf "  %-18s %5d trades  %4d wins  net %+9d  long %+8d  short %+8d\n", $1,$2,$3,$4,$8,$9 }'
echo ""
echo "Bottom 5 by NET:"
echo "$RAW" | sort -k4 -n | head -5 | awk '{ printf "  %-18s %5d trades  %4d wins  net %+9d  long %+8d  short %+8d\n", $1,$2,$3,$4,$8,$9 }'

} | tee "$OUT"
echo "→ Output: $OUT"
