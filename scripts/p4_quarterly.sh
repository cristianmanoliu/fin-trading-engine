#!/usr/bin/env bash
# p4_quarterly.sh — per-symbol per-quarter NET under P4-Combined post-fees stack.
# Output: TSV  symbol \t year-Qn \t trades \t wins \t net_usd
# Used by scripts/rolling_shortlist.py to evaluate a quarterly-rebalance
# rolling-shortlist policy against the fixed deployed-32 list.
set -euo pipefail

ROOT="$(cd "$(dirname "$0")/.." && pwd)"
SIGNAL_TF="${SIGNAL_TF:-4H}"
TARGET_RR="${TARGET_RR:-6.0}"
FEE_BPS="${FEE_BPS:-10}"
SLIP_BPS="${SLIP_BPS:-15}"   # default to slip=15bp — middle-of-the-road realistic
FUNDING_BPS="${FUNDING_BPS:-0}"
TAX_RATE="${TAX_RATE:-0}"
FUNDING_DIR="${FUNDING_DIR:-data/funding}"
EXTRA_ARGS="${EXTRA_ARGS:---side-filter short --max-hold-hours 336}"

OUT="${1:-results/p4_quarterly_slip${SLIP_BPS}_$(date +%F).tsv}"

BINARY=$(mktemp /tmp/quart-bin.XXXXXXXX)
WORKDIR=$(mktemp -d /tmp/quart-sweep.XXXXXXXX)
trap 'rm -f "$BINARY"; rm -rf "$WORKDIR"' EXIT

START_YEAR=2020
END_YEAR=2025
END_YEAR_LAST_Q=1   # 2025-Q1 last full quarter (data goes through April 2025)

SYMBOLS="RUNEUSDT SOLUSDT BNBUSDT HBARUSDT IOTAUSDT TRXUSDT XLMUSDT LINKUSDT GRTUSDT FTMUSDT SNXUSDT APEUSDT BLURUSDT AAVEUSDT MKRUSDT VETUSDT ARBUSDT ATOMUSDT PYTHUSDT BTCUSDT ENJUSDT 1INCHUSDT NEARUSDT KAVAUSDT AVAXUSDT TIAUSDT ETHUSDT DOTUSDT XRPUSDT GMXUSDT BCHUSDT ZILUSDT LDOUSDT DYDXUSDT UNIUSDT DOGEUSDT SEIUSDT 1000SHIBUSDT IMXUSDT INJUSDT ROSEUSDT GALAUSDT MANAUSDT OPUSDT ICPUSDT AXSUSDT ENSUSDT SUIUSDT WLDUSDT ADAUSDT FILUSDT SANDUSDT CHZUSDT LTCUSDT APTUSDT ETCUSDT CRVUSDT"

NCPU=$(sysctl -n hw.logicalcpu 2>/dev/null || nproc 2>/dev/null || echo 4)

echo "→ Compiling..."
(cd "$ROOT" && go build -o "$BINARY" ./cmd/backtest)
echo "→ Per-symbol per-quarter sweep  signal_tf=${SIGNAL_TF}  target_rr=${TARGET_RR}  slip=${SLIP_BPS}bp"

run_quarter() {
    local symbol=$1
    local year=$2
    local quarter=$3   # 1..4

    local m1=$(( (quarter-1)*3 + 1 ))
    local m2=$(( m1 + 1 ))
    local m3=$(( m1 + 2 ))

    local merged="${WORKDIR}/${symbol}-${year}Q${quarter}.csv"
    local found=0
    for m in $m1 $m2 $m3; do
        local mm
        mm=$(printf '%02d' "$m")
        local csv="${ROOT}/data/${symbol}-1m-${year}-${mm}.csv"
        if [[ -f "$csv" ]]; then
            cat "$csv" >> "$merged"
            found=$(( found + 1 ))
        fi
    done

    if [[ $found -eq 0 || ! -s "$merged" ]]; then
        echo -e "${symbol}\t${year}-Q${quarter}\t0\t0\t0"
        return
    fi

    local cfg
    cfg=$(mktemp "${WORKDIR}/cfg-${symbol}-${year}Q${quarter}.XXXXXXXX")
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
        | jq -r 'select(.msg | test("SUMMARY")) | "\(.total_trades) \(.wins) \(.after_tax_pnl_usd // .total_pnl_usd // 0)"' \
        2>/dev/null | tail -1)

    rm -f "$cfg" "$merged"
    if [[ -z "$result" ]]; then
        result="0 0 0"
    fi
    # Convert "trades wins net" into TSV
    local trades wins net
    read -r trades wins net <<< "$result"
    echo -e "${symbol}\t${year}-Q${quarter}\t${trades}\t${wins}\t${net%.*}"
}
export -f run_quarter
export ROOT BINARY WORKDIR FEE_BPS SLIP_BPS FUNDING_BPS TAX_RATE FUNDING_DIR EXTRA_ARGS SIGNAL_TF TARGET_RR

# Cross-product symbols × (year, quarter)
JOBS=""
for sym in $SYMBOLS; do
    for (( y=START_YEAR; y<=END_YEAR; y++ )); do
        local_last=4
        if [[ $y -eq $END_YEAR ]]; then
            local_last=$END_YEAR_LAST_Q
        fi
        for (( q=1; q<=local_last; q++ )); do
            JOBS+="$sym $y $q"$'\n'
        done
    done
done

# Header
echo -e "symbol\tperiod\ttrades\twins\tnet_usd" > "$OUT"

printf '%s' "$JOBS" \
    | xargs -P "$NCPU" -L 1 bash -c 'run_quarter "$0" "$1" "$2"' \
    | sort >> "$OUT"

echo "→ Output: $OUT"
echo "  Rows: $(( $(wc -l < "$OUT") - 1 ))"
