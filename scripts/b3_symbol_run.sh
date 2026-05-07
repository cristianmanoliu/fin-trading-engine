#!/usr/bin/env bash
# b3_symbol_run.sh — produce per-symbol NET for the full universe (57) at the
# candidate strategy params, slip=5bp. Used as input to scripts/b3_features.py
# to test whether mechanism features (volatility, funding, liquidity) explain
# the deployed-16 vs rejected-41 split.
#
# Output: results/b3_symbol_net_<date>.csv
#   columns: symbol, net_usd, trades, wins, wr_pct, deployed (1/0)
#
# Cheap variant of slip_cliff.sh — single slip level, single sweep.
set -euo pipefail

ROOT="$(cd "$(dirname "$0")/.." && pwd)"
DATE_TAG="${DATE_TAG:-$(date +%F)}"
OUT="${OUT:-${ROOT}/results/b3_symbol_net_${DATE_TAG}.csv}"
SLIP_BPS="${SLIP_BPS:-5}"
FEE_BPS="${FEE_BPS:-10}"
TARGET_RR="${TARGET_RR:-6.0}"
MAX_HOLD_HOURS="${MAX_HOLD_HOURS:-504}"
SIDE_FILTER="${SIDE_FILTER:-short}"
SIGNAL_TF="${SIGNAL_TF:-4H}"

BINARY=$(mktemp /tmp/b3-bin.XXXXXXXX)
WORKDIR=$(mktemp -d /tmp/b3-sweep.XXXXXXXX)
trap 'rm -f "$BINARY"; rm -rf "$WORKDIR"' EXIT

source "${ROOT}/scripts/lib/symbols.sh"
SYMBOLS=$(get_symbols universe)
DEPLOYED=" $(get_symbols deployed) "

START_YEAR=2020
END_YEAR=2025
END_YEAR_MONTH=04

(cd "$ROOT" && go build -o "$BINARY" ./cmd/backtest)
echo "→ binary built; running 57-symbol single-slip sweep at slip=${SLIP_BPS}bp"
echo "→ output: $OUT"
echo ""

run_one() {
    local symbol=$1
    local merged="${WORKDIR}/${symbol}.csv"
    : > "$merged"
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
        echo "${symbol},,,,,"
        return
    fi
    local cfg="${WORKDIR}/cfg-${symbol}.yaml"
    sed "
        s|symbol:.*|symbol: ${symbol}|;
        s|momentum_mode:.*|momentum_mode: false|;
        s|vwap_deviation_mode:.*|vwap_deviation_mode: false|;
        s|ema_mode:.*|ema_mode: true|;
        s|target_rr:.*|target_rr: ${TARGET_RR}|;
        s|csv_path:.*|csv_path: ${merged}|
    " "${ROOT}/configs/default.yaml" > "$cfg"

    local result
    result=$("$BINARY" --config "$cfg" \
        --exact-fills --include-boundary \
        --fee-bps "$FEE_BPS" --stop-slippage-bps "$SLIP_BPS" \
        --funding-bps-per-day 0 \
        --funding-csv-dir "${ROOT}/data/funding" \
        --signal-tf "$SIGNAL_TF" --side-filter "$SIDE_FILTER" \
        --max-hold-hours "$MAX_HOLD_HOURS" \
        2>&1 | grep "BACKTEST SUMMARY")

    local trades=$(echo "$result" | grep -oE '"total_trades":[0-9]+' | head -1 | cut -d: -f2)
    local wins=$(echo "$result" | grep -oE '"wins":[0-9]+' | head -1 | cut -d: -f2)
    local pnl=$(echo "$result" | grep -oE '"total_pnl_usd":-?[0-9.]+' | head -1 | cut -d: -f2)
    local wr=$(echo "$result" | grep -oE '"win_rate_pct":[0-9.]+' | head -1 | cut -d: -f2)

    local deployed=0
    if [[ "$DEPLOYED" == *" $symbol "* ]]; then deployed=1; fi
    echo "${symbol},${pnl:-0},${trades:-0},${wins:-0},${wr:-0},${deployed}"
}

export -f run_one
export ROOT WORKDIR BINARY SLIP_BPS FEE_BPS TARGET_RR MAX_HOLD_HOURS SIDE_FILTER SIGNAL_TF DEPLOYED START_YEAR END_YEAR END_YEAR_MONTH

NCPU=$(sysctl -n hw.logicalcpu 2>/dev/null || nproc 2>/dev/null || echo 4)

echo "symbol,net_usd,trades,wins,wr_pct,deployed" > "$OUT"
echo "$SYMBOLS" | tr ' ' '\n' | xargs -n1 -P"$NCPU" -I{} bash -c 'run_one "$@"' _ {} >> "$OUT"

echo "→ done; wrote $(wc -l < "$OUT") lines (incl header) to $OUT"
echo ""
echo "→ summary:"
python3 - "$OUT" <<'EOF'
import csv, sys
from pathlib import Path
rows = list(csv.DictReader(open(sys.argv[1])))
deployed = [r for r in rows if r.get("deployed") == "1"]
rejected = [r for r in rows if r.get("deployed") == "0"]
def stats(group):
    nets = [float(r["net_usd"]) for r in group if r["net_usd"]]
    pos = sum(1 for n in nets if n > 0)
    return len(nets), pos, sum(nets)
n_d, p_d, s_d = stats(deployed)
n_r, p_r, s_r = stats(rejected)
print(f"  deployed-{n_d}: {p_d} profitable, total NET ${s_d:,.0f}")
print(f"  rejected-{n_r}: {p_r} profitable, total NET ${s_r:,.0f}")
EOF
