#!/usr/bin/env bash
# p4_quarterly_extend.sh — extend p4_quarterly TSV to include 2025-Q2..2026-Q1.
# These 4 quarters were not in the original sweep (which stopped at 2025-Q1).
# Output: appends new rows to a NEW TSV that merges old + new.
set -euo pipefail

ROOT="$(cd "$(dirname "$0")/.." && pwd)"
SIGNAL_TF="${SIGNAL_TF:-4H}"
TARGET_RR="${TARGET_RR:-6.0}"
FEE_BPS="${FEE_BPS:-10}"
SLIP_BPS="${SLIP_BPS:-15}"
FUNDING_BPS="${FUNDING_BPS:-0}"
TAX_RATE="${TAX_RATE:-0}"
FUNDING_DIR="${FUNDING_DIR:-data/funding}"
EXTRA_ARGS="${EXTRA_ARGS:---side-filter short --max-hold-hours 336}"

EXISTING="${EXISTING:-results/p4_quarterly_slip15_2026-05-06.tsv}"
OUT="${1:-results/p4_quarterly_slip${SLIP_BPS}_full_2026-05-06.tsv}"

BINARY=$(mktemp /tmp/quart-bin.XXXXXXXX)
WORKDIR=$(mktemp -d /tmp/quart-extend.XXXXXXXX)
trap 'rm -f "$BINARY"; rm -rf "$WORKDIR"' EXIT

source "$(dirname "${BASH_SOURCE[0]}")/lib/symbols.sh"
SYMBOLS="${SYMBOLS:-$(get_symbols universe)}"

NCPU=$(sysctl -n hw.logicalcpu 2>/dev/null || nproc 2>/dev/null || echo 4)

echo "→ Compiling..."
(cd "$ROOT" && go build -o "$BINARY" ./cmd/backtest)

run_quarter() {
    local symbol=$1
    local year=$2
    local quarter=$3

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
    local trades wins net
    read -r trades wins net <<< "$result"
    echo -e "${symbol}\t${year}-Q${quarter}\t${trades}\t${wins}\t${net%.*}"
}
export -f run_quarter
export ROOT BINARY WORKDIR FEE_BPS SLIP_BPS FUNDING_BPS TAX_RATE FUNDING_DIR EXTRA_ARGS SIGNAL_TF TARGET_RR

# Just the new quarters: 2025-Q2..Q4 + 2026-Q1
JOBS=""
for sym in $SYMBOLS; do
    for q in 2 3 4; do
        JOBS+="$sym 2025 $q"$'\n'
    done
    JOBS+="$sym 2026 1"$'\n'
done

NEW_TSV="${WORKDIR}/new_quarters.tsv"
printf '%s' "$JOBS" | xargs -P "$NCPU" -L 1 bash -c 'run_quarter "$0" "$1" "$2"' | sort > "$NEW_TSV"

echo "→ Merging $(wc -l < "$EXISTING") existing rows + $(wc -l < "$NEW_TSV") new rows"

# Combine existing (skip header) + new, then re-sort
{
    head -1 "$EXISTING"
    {
        tail -n +2 "$EXISTING"
        cat "$NEW_TSV"
    } | sort
} > "$OUT"

echo "→ Output: $OUT"
echo "  Total rows: $(( $(wc -l < "$OUT") - 1 ))"
