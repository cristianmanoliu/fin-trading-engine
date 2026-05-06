#!/usr/bin/env bash
# oos_persistence.sh — Out-of-sample persistence test for symbol selection.
# Split 5y into train (2020-01..2022-12) and test (2023-01..2025-04).
# Run continuous backtest on each half per symbol at target_rr=5.0 + all audit flags.
# Output per-symbol train/test PnL; ranking persistence is computed by oos_persistence.py.
set -euo pipefail

ROOT="$(cd "$(dirname "$0")/.." && pwd)"
OUT="${1:-results/oos_persistence_$(date +%F).tsv}"
BINARY=$(mktemp /tmp/oos-bin.XXXXXXXX)
WORKDIR=$(mktemp -d /tmp/oos-sweep.XXXXXXXX)
trap 'rm -f "$BINARY"; rm -rf "$WORKDIR"' EXIT

source "$(dirname "${BASH_SOURCE[0]}")/lib/symbols.sh"
SYMBOLS="${SYMBOLS:-$(get_symbols universe)}"

NCPU=$(sysctl -n hw.logicalcpu 2>/dev/null || nproc 2>/dev/null || echo 4)
SED_BASE="s|momentum_mode:.*|momentum_mode: false|; s|vwap_deviation_mode:.*|vwap_deviation_mode: false|; s|ema_mode:.*|ema_mode: true|; s|target_rr:.*|target_rr: 5.0|"

echo "→ Compiling..."
(cd "$ROOT" && go build -o "$BINARY" ./cmd/backtest)
echo "→ Running OOS persistence test (NCPU=$NCPU)..."
echo "→ train: 2020-01..2022-12  test: 2023-01..2025-04"

run_period() {
    local symbol=$1 period=$2 sy=$3 sm=$4 ey=$5 em=$6
    local merged="${WORKDIR}/${symbol}_${period}.csv"
    for (( y=sy; y<=ey; y++ )); do
        local fm=1; local lm=12
        [[ $y -eq $sy ]] && fm=$sm
        [[ $y -eq $ey ]] && lm=$em
        for (( m=fm; m<=lm; m++ )); do
            local mm; mm=$(printf '%02d' "$m")
            local csv="${ROOT}/data/${symbol}-1m-${y}-${mm}.csv"
            [[ -f "$csv" ]] && cat "$csv" >> "$merged"
        done
    done
    if [[ ! -s "$merged" ]]; then
        echo "${symbol}	${period}	0	0	0"
        return
    fi
    local cfg; cfg=$(mktemp "${WORKDIR}/cfg-${symbol}-${period}.XXXXXXXX")
    sed "${SED_BASE}; s|csv_path:.*|csv_path: ${merged}|" "${ROOT}/configs/default.yaml" > "$cfg"
    local result
    result=$("$BINARY" --config "$cfg" --exact-fills --include-boundary --pessimistic-ambiguous 2>&1 \
        | jq -r 'select(.msg | test("SUMMARY")) | "\(.total_trades)\t\(.wins)\t\(.total_pnl_usd // 0)"' 2>/dev/null | tail -1)
    rm -f "$cfg" "$merged"
    printf '%s\t%s\t%s\n' "$symbol" "$period" "${result:-0\t0\t0}"
}
export -f run_period
export ROOT BINARY WORKDIR SED_BASE

# Build (symbol period sy sm ey em) job list, run in parallel
JOBS=$(mktemp /tmp/oos-jobs.XXXXXXXX)
for sym in $SYMBOLS; do
    echo "$sym train 2020 1 2022 12" >> "$JOBS"
    echo "$sym test 2023 1 2025 4"  >> "$JOBS"
done

echo -e "symbol\tperiod\ttrades\twins\tpnl_usd" > "$OUT"
xargs -P "$NCPU" -L 1 bash -c 'run_period "$@"' _ < "$JOBS" >> "$OUT"
rm -f "$JOBS"

echo "→ Raw results: $OUT"
echo "→ Run scripts/oos_persistence.py $OUT for analysis"
