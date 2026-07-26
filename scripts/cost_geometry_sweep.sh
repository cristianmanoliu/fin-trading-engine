#!/usr/bin/env bash
# cost_geometry_sweep.sh — 2026-07-26 overnight arc.
#
# Tests the ONE mechanism the live cost decomposition actually identified:
#   live is gross-POSITIVE (+$3,489) but dies to costs at 67x avg leverage,
#   because notional = stake / |entry-stop| and fees are charged on notional.
#
# Every cell here attacks LEVERAGE (the stop width), not the signal. Runs the
# canonical continuous 5y-per-symbol methodology (concatenated monthly CSVs,
# NO month segmentation) with realistic costs and audit-mode fills.
#
# TRAIN/TEST SPLIT is enforced by the caller via START_YEAR/END_YEAR:
#   TRAIN 2020-01 .. 2023-12   (parameter selection)
#   TEST  2024-01 .. 2025-04   (held out; touched ONCE, after params locked)
#
# Usage:
#   VARIANT=atr2.0 EXTRA="--atr-stop-mult 2.0" START_YEAR=2020 END_YEAR=2023 \
#     bash scripts/cost_geometry_sweep.sh out.txt
set -euo pipefail

ROOT="$(cd "$(dirname "$0")/.." && pwd)"
OUT="${1:?output file required}"
VARIANT="${VARIANT:?VARIANT label required}"
EXTRA="${EXTRA:-}"

FEE_BPS="${FEE_BPS:-10}"
SLIP_BPS="${SLIP_BPS:-5}"
START_YEAR="${START_YEAR:-2020}"
END_YEAR="${END_YEAR:-2025}"
END_YEAR_MONTH="${END_YEAR_MONTH:-04}"

BINARY="${SHARED_BINARY:-}"
WORKDIR=$(mktemp -d /tmp/cg-sweep.XXXXXXXX)
OWN_BINARY=0
if [[ -z "$BINARY" ]]; then
    BINARY=$(mktemp /tmp/cg-bin.XXXXXXXX)
    OWN_BINARY=1
    (cd "$ROOT" && go build -o "$BINARY" ./cmd/backtest)
fi
cleanup() { rm -rf "$WORKDIR"; [[ $OWN_BINARY -eq 1 ]] && rm -f "$BINARY"; }
trap cleanup EXIT

source "$ROOT/scripts/lib/symbols.sh"
SYMBOLS="${SYMBOLS:-$(get_symbols universe)}"
NCPU=$(sysctl -n hw.logicalcpu 2>/dev/null || nproc 2>/dev/null || echo 4)

run_symbol() {
    local symbol=$1
    local merged="${WORKDIR}/${symbol}.csv"
    local found=0
    for (( y=START_YEAR; y<=END_YEAR; y++ )); do
        local last=12
        [[ $y -eq $END_YEAR ]] && last=$(( 10#$END_YEAR_MONTH ))
        for (( m=1; m<=last; m++ )); do
            local mm; mm=$(printf '%02d' "$m")
            local csv="${ROOT}/data/${symbol}-1m-${y}-${mm}.csv"
            [[ -f "$csv" ]] && { cat "$csv" >> "$merged"; found=1; }
        done
    done
    [[ $found -eq 0 ]] && return 0

    local cfg="${WORKDIR}/${symbol}.yaml"
    sed -e "s|^symbol:.*|symbol: ${symbol}|" \
        -e "s|csv_path:.*|csv_path: ${merged}|" \
        -e "s|ema_mode:.*|ema_mode: true|" \
        -e "s|target_rr:.*|target_rr: 6.0|" \
        -e "s|stake_usd:.*|stake_usd: 1000|" \
        -e "s|momentum_mode:.*|momentum_mode: false|" \
        -e "s|vwap_deviation_mode:.*|vwap_deviation_mode: false|" \
        "${ROOT}/configs/default.yaml" > "$cfg"

    # SUMMARY line is JSON on stderr/stdout; reduce to a fixed field set.
    local result
    # shellcheck disable=SC2086
    result=$("$BINARY" --config "$cfg" \
        --signal-tf 4H --side-filter short \
        --max-hold-hours 504 --funding-csv-dir "${ROOT}/data/funding" \
        --fee-bps "$FEE_BPS" --stop-slippage-bps "$SLIP_BPS" \
        --exact-fills --include-boundary --pessimistic-ambiguous \
        $EXTRA 2>&1 \
        | grep -F 'BACKTEST SUMMARY' \
        | jq -Rr 'fromjson? | select(.msg != null) | "\(.total_trades) \(.wins) \(.total_pnl_usd // 0) \(.gross_pnl_usd // 0) \(.total_fees_usd // 0)"' \
        2>/dev/null | tail -1)

    rm -f "$merged"
    echo "${symbol} ${result:-0 0 0 0 0}"
}
export -f run_symbol
export WORKDIR ROOT BINARY START_YEAR END_YEAR END_YEAR_MONTH FEE_BPS SLIP_BPS EXTRA

{
    echo "# VARIANT=${VARIANT} EXTRA='${EXTRA}'"
    echo "# span=${START_YEAR}-01..${END_YEAR}-${END_YEAR_MONTH} fee=${FEE_BPS} slip=${SLIP_BPS}"
    echo "# symbols=$(echo "$SYMBOLS" | wc -w | tr -d ' ')"
    printf '%s\n' $SYMBOLS | xargs -P "$NCPU" -I{} bash -c 'run_symbol "$@"' _ {}
} > "$OUT"

echo "→ ${VARIANT} done: $OUT"
