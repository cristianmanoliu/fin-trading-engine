#!/usr/bin/env bash
# hod_journals.sh — generate per-trade JSONL journals for the deployed-16
# at the candidate strategy params (4H short EMA9/21 mh504 target_rr=6) over
# the full 5y merged 1m dataset. Output:
#   results/hod_journals/<DATE>/<SYMBOL>-<wall-month>.jsonl
#
# Used as input to scripts/hod_decompose.py for entry-hour-of-day analysis.
# This script does not produce a PnL summary — see realistic_sweep.sh / p4_*.sh
# for that. The goal here is only to capture the per-trade event stream.
#
# Usage:
#   bash scripts/hod_journals.sh
set -euo pipefail

ROOT="$(cd "$(dirname "$0")/.." && pwd)"
DATE_TAG="${DATE_TAG:-$(date +%F)}"
OUTDIR="${ROOT}/results/hod_journals/${DATE_TAG}"
WORKDIR=$(mktemp -d /tmp/hod-merge.XXXXXXXX)
BINARY=$(mktemp /tmp/hod-bin.XXXXXXXX)
trap 'rm -f "$BINARY"; rm -rf "$WORKDIR"' EXIT

mkdir -p "$OUTDIR"

source "${ROOT}/scripts/lib/symbols.sh"
SYMBOLS="${SYMBOLS:-$(get_symbols deployed)}"

(cd "$ROOT" && go build -o "$BINARY" ./cmd/backtest)
echo "→ binary built; symbols: $(echo "$SYMBOLS" | wc -w)"
echo "→ output: $OUTDIR"
echo ""

START_YEAR=2020
END_YEAR=2025
END_YEAR_MONTH=04

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
        echo "  [$symbol] NO DATA — skipped"
        return
    fi

    local cfg="${WORKDIR}/cfg-${symbol}.yaml"
    sed "
        s|symbol:.*|symbol: ${symbol}|;
        s|momentum_mode:.*|momentum_mode: false|;
        s|vwap_deviation_mode:.*|vwap_deviation_mode: false|;
        s|ema_mode:.*|ema_mode: true|;
        s|target_rr:.*|target_rr: 6.0|;
        s|csv_path:.*|csv_path: ${merged}|
    " "${ROOT}/configs/default.yaml" > "$cfg"

    local started=$(date +%s)
    local funding_arg="--funding-csv-dir ${ROOT}/data/funding"
    [[ "${NO_FUNDING:-0}" == "1" ]] && funding_arg=""
    "$BINARY" --config "$cfg" \
        --exact-fills --include-boundary \
        --fee-bps 10 --stop-slippage-bps 5 \
        --funding-bps-per-day 0 \
        $funding_arg \
        --signal-tf 4H --side-filter "${SIDE_FILTER:-short}" --max-hold-hours 504 \
        --journal-dir "$OUTDIR" \
        > /dev/null 2>&1
    local elapsed=$(( $(date +%s) - started ))
    local journal_files=$(find "$OUTDIR" -name "${symbol}-*.jsonl" -size +0c | wc -l | tr -d ' ')
    local trades=$(grep -c '"event":"open"' "$OUTDIR/${symbol}"-*.jsonl 2>/dev/null | awk -F: '{s+=$2} END{print s+0}')
    printf "  [%-13s] %ds  journal_files=%d  trades=%d\n" "$symbol" "$elapsed" "$journal_files" "$trades"
}

export -f run_one
export ROOT WORKDIR BINARY OUTDIR START_YEAR END_YEAR END_YEAR_MONTH

NCPU=$(sysctl -n hw.logicalcpu 2>/dev/null || nproc 2>/dev/null || echo 4)
echo "→ running ${NCPU}-way parallel..."
echo "$SYMBOLS" | tr ' ' '\n' | xargs -n1 -P"$NCPU" -I{} bash -c 'run_one "$@"' _ {}

echo ""
echo "✓ Journals written to $OUTDIR"
ls -la "$OUTDIR" | head -25
