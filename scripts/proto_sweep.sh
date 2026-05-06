#!/usr/bin/env bash
# proto_sweep.sh — Run multiple prototype configurations through the realistic
# continuous 5y sweep, all with --fee-bps=8 --stop-slippage-bps=5.
#
# Tests the cost-geometry escape hypothesis: wider stops (longer TF and/or
# ATR-scaled) reduce implicit leverage and per-trade fees enough to clear costs.
#
# Configurations:
#   B0  baseline:   5m  + wick stop      (= the falsified config)
#   P1  5m + ATR(14)×2.0
#   P2  30m + wick stop
#   P3  30m + ATR(14)×2.0
#   P4  4H + wick stop          (very sparse signal — sanity only)
#   P5  4H + ATR(14)×2.0        (very sparse signal — sanity only)
#
# Each is run with target_rr=5.0, --exact-fills --include-boundary
# --pessimistic-ambiguous --fee-bps=8 --stop-slippage-bps=5.
set -euo pipefail

ROOT="$(cd "$(dirname "$0")/.." && pwd)"
OUT="${1:-results/proto_sweep_$(date +%F).txt}"
BINARY=$(mktemp /tmp/proto-bin.XXXXXXXX)
WORKDIR=$(mktemp -d /tmp/proto-sweep.XXXXXXXX)
trap 'rm -f "$BINARY"; rm -rf "$WORKDIR"' EXIT

FEE_BPS="${FEE_BPS:-10}"
SLIP_BPS="${SLIP_BPS:-5}"
FUNDING_BPS="${FUNDING_BPS:-0}"
TAX_RATE="${TAX_RATE:-0}"
TARGET_RR="${TARGET_RR:-5.0}"

START_YEAR=2020
END_YEAR=2025
END_YEAR_MONTH=04

SYMBOLS="RUNEUSDT SOLUSDT BNBUSDT HBARUSDT IOTAUSDT TRXUSDT XLMUSDT LINKUSDT GRTUSDT FTMUSDT SNXUSDT APEUSDT BLURUSDT AAVEUSDT MKRUSDT VETUSDT ARBUSDT ATOMUSDT PYTHUSDT BTCUSDT ENJUSDT 1INCHUSDT NEARUSDT KAVAUSDT AVAXUSDT TIAUSDT ETHUSDT DOTUSDT XRPUSDT GMXUSDT BCHUSDT ZILUSDT LDOUSDT DYDXUSDT UNIUSDT DOGEUSDT SEIUSDT 1000SHIBUSDT IMXUSDT INJUSDT ROSEUSDT GALAUSDT MANAUSDT OPUSDT ICPUSDT AXSUSDT ENSUSDT SUIUSDT WLDUSDT ADAUSDT FILUSDT SANDUSDT CHZUSDT LTCUSDT APTUSDT ETCUSDT CRVUSDT"

NCPU=$(sysctl -n hw.logicalcpu 2>/dev/null || nproc 2>/dev/null || echo 4)

echo "→ Compiling..."
(cd "$ROOT" && go build -o "$BINARY" ./cmd/backtest)

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

# Run one (symbol, prototype) backtest, return: trades wins net gross fees
# Args: symbol prototype_args
run_one() {
    local symbol=$1
    shift
    local proto_args="$*"
    local merged="${WORKDIR}/${symbol}.csv"

    if [[ ! -s "$merged" ]]; then
        echo "${symbol} 0 0 0 0 0"
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

    local result
    result=$("$BINARY" --config "$cfg" \
        --exact-fills --include-boundary --pessimistic-ambiguous \
        --fee-bps "$FEE_BPS" --stop-slippage-bps "$SLIP_BPS" \
        --funding-bps-per-day "$FUNDING_BPS" --tax-rate-pct "$TAX_RATE" \
        $proto_args 2>&1 \
        | jq -r 'select(.msg | test("SUMMARY")) | "\(.total_trades) \(.wins) \(.total_pnl_usd // 0) \(.gross_pnl_usd // 0) \(.total_fees_usd // 0) \(.total_funding_usd // 0) \(.after_tax_pnl_usd // .total_pnl_usd // 0)"' \
        2>/dev/null | tail -1)

    rm -f "$cfg"
    echo "${symbol} ${result:-0 0 0 0 0 0 0}"
}
export -f run_one
export ROOT BINARY WORKDIR FEE_BPS SLIP_BPS FUNDING_BPS TAX_RATE TARGET_RR

run_proto() {
    local label="$1"
    local args="$2"
    echo "  → ${label}  (${args})" >&2

    local raw
    raw=$(printf '%s\n' $SYMBOLS \
        | xargs -P "$NCPU" -I{} bash -c "run_one '{}' $args")

    echo "$raw" | awk -v label="$label" -v args="$args" '
    BEGIN { t=0; w=0; net=0; gross=0; fees=0; funding=0; aftertax=0; n=0; pos=0 }
    {
        t+=$2; w+=$3; net+=$4; gross+=$5; fees+=$6; funding+=$7; aftertax+=$8; n++
        if ($8>0) pos++
    }
    END {
        wp = (t>0) ? sprintf("%.1f%%", w/t*100) : "—"
        printf "%-32s %8d %5s %+11.0f %11.0f %11.0f %+12.0f %+12.0f %3d/%d\n",
            label, t, wp, gross, fees, funding, net, aftertax, pos, n
    }
    '
}

{
echo ""
echo "════════════════════════════════════════════════════════════════════════════════════════════"
echo "  PROTOTYPE SWEEP — cost-geometry escape  (continuous 5y, 57 symbols, target_rr=${TARGET_RR})"
echo "  Costs: --fee-bps=${FEE_BPS}  --stop-slippage-bps=${SLIP_BPS}  --funding-bps-per-day=${FUNDING_BPS}  --tax-rate-pct=${TAX_RATE}"
echo "  Audit flags: --exact-fills --include-boundary --pessimistic-ambiguous"
echo "════════════════════════════════════════════════════════════════════════════════════════════"
echo ""
printf "%-32s %8s %5s %11s %11s %11s %12s %12s %s\n" \
    "Configuration" "trades" "win%" "Gross_\$" "Fees_\$" "Funding_\$" "NET(pretax)" "AfterTax_\$" "profit/total"
printf "%-32s %8s %5s %11s %11s %11s %12s %12s %s\n" \
    "─────────────" "──────" "────" "────────" "──────" "─────────" "──────────" "──────────" "──────────"

run_proto "B0  5m  + wick (baseline)"  "--signal-tf 5m"
run_proto "P1  5m  + ATR(14)x2.0"      "--signal-tf 5m  --atr-stop-mult 2.0"
run_proto "P2  30m + wick"             "--signal-tf 30m"
run_proto "P3  30m + ATR(14)x2.0"      "--signal-tf 30m --atr-stop-mult 2.0"
run_proto "P4  4H  + wick"             "--signal-tf 4H"
run_proto "P5  4H  + ATR(14)x2.0"      "--signal-tf 4H  --atr-stop-mult 2.0"

echo ""
echo "════════════════════════════════════════════════════════════════════════════════════════════"
echo "  Verdict guide:"
echo "  • NET >= 0 with profit/total >= 28/57: real candidate, deep-dive next."
echo "  • NET near 0 with mixed profit/total: marginal — sweep target_rr or refine."
echo "  • NET deeply negative everywhere: signal family doesn't beat costs at any geometry."
echo "  • 'nontriv' counts symbols with > 20 trades: low values mean signal is too sparse to trust."
echo "════════════════════════════════════════════════════════════════════════════════════════════"

} | tee "$OUT"

echo ""
echo "→ Output: $OUT"
