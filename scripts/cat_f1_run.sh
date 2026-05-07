#!/usr/bin/env bash
# cat_f1_run.sh — Cat F1 funding-cross standalone signal aggregate test.
#
# Pre-registered 2026-05-07 (results/cat_f1_funding_cross_decision_rule_2026-05-07.md).
# Threshold LOCKED at 30 bp/day. No sweep permitted.
#
# Output: results/cat_f1_aggregate_<date>.csv (per-symbol NET at slip=5,
# fee=10, threshold=30, max-hold=504h)
set -euo pipefail

ROOT="$(cd "$(dirname "$0")/.." && pwd)"
DATE_TAG="${DATE_TAG:-$(date +%F)}"
OUT="${OUT:-${ROOT}/results/cat_f1_aggregate_${DATE_TAG}.csv}"
SLIP_BPS="${SLIP_BPS:-5}"
FEE_BPS="${FEE_BPS:-10}"
TARGET_RR="${TARGET_RR:-6.0}"
MAX_HOLD_HOURS="${MAX_HOLD_HOURS:-504}"
SIGNAL_TF="${SIGNAL_TF:-4H}"
THRESHOLD="${THRESHOLD:-30}"  # LOCKED — pre-registered

BINARY=$(mktemp /tmp/f1-bin.XXXXXXXX)
WORKDIR=$(mktemp -d /tmp/f1-sweep.XXXXXXXX)
trap 'rm -f "$BINARY"; rm -rf "$WORKDIR"' EXIT

source "${ROOT}/scripts/lib/symbols.sh"
SYMBOLS=$(get_symbols "${SYMBOL_GROUP:-deployed}")

START_YEAR="${START_YEAR:-2020}"
START_MONTH="${START_MONTH:-1}"
END_YEAR="${END_YEAR:-2025}"
END_YEAR_MONTH="${END_YEAR_MONTH:-${END_MONTH:-04}}"

(cd "$ROOT" && go build -o "$BINARY" ./cmd/backtest)
echo "→ Cat F1 funding-cross | threshold=${THRESHOLD}bp/day | symbols: $(echo "$SYMBOLS" | wc -w | tr -d ' ')"
echo ""

run_one() {
    local symbol=$1
    local merged="${WORKDIR}/${symbol}.csv"
    : > "$merged"
    for (( y=START_YEAR; y<=END_YEAR; y++ )); do
        local first=1
        local last=12
        [[ $y == $START_YEAR ]] && first=$((10#$START_MONTH))
        [[ $y == $END_YEAR ]] && last=$((10#$END_YEAR_MONTH))
        for (( m=first; m<=last; m++ )); do
            local mm=$(printf '%02d' "$m")
            local csv="${ROOT}/data/${symbol}-1m-${y}-${mm}.csv"
            [[ -f "$csv" ]] && cat "$csv" >> "$merged"
        done
    done
    [[ -s "$merged" ]] || { echo "${symbol},,,,"; return; }

    local cfg="${WORKDIR}/cfg-${symbol}.yaml"
    sed "
        s|symbol:.*|symbol: ${symbol}|;
        s|momentum_mode:.*|momentum_mode: false|;
        s|vwap_deviation_mode:.*|vwap_deviation_mode: false|;
        s|ema_mode:.*|ema_mode: false|;
        s|target_rr:.*|target_rr: ${TARGET_RR}|;
        s|csv_path:.*|csv_path: ${merged}|
    " "${ROOT}/configs/default.yaml" > "$cfg"

    local result
    result=$("$BINARY" --config "$cfg" \
        --exact-fills --include-boundary \
        --fee-bps "$FEE_BPS" --stop-slippage-bps "$SLIP_BPS" \
        --funding-bps-per-day 0 \
        --funding-csv-dir "${ROOT}/data/funding" \
        --signal-tf "$SIGNAL_TF" \
        --funding-cross-mode --funding-threshold-bps "$THRESHOLD" \
        --max-hold-hours "$MAX_HOLD_HOURS" \
        2>&1 | grep "BACKTEST SUMMARY")

    local trades=$(echo "$result" | grep -oE '"total_trades":[0-9]+' | head -1 | cut -d: -f2)
    local wins=$(echo "$result" | grep -oE '"wins":[0-9]+' | head -1 | cut -d: -f2)
    local pnl=$(echo "$result" | grep -oE '"total_pnl_usd":-?[0-9.]+' | head -1 | cut -d: -f2)
    local long_net=$(echo "$result" | grep -oE '"long_net_usd":-?[0-9.]+' | head -1 | cut -d: -f2)
    local short_net=$(echo "$result" | grep -oE '"short_net_usd":-?[0-9.]+' | head -1 | cut -d: -f2)

    echo "${symbol},${trades:-0},${wins:-0},${pnl:-0},${long_net:-0},${short_net:-0}"
}

export -f run_one
export ROOT WORKDIR BINARY SLIP_BPS FEE_BPS TARGET_RR MAX_HOLD_HOURS SIGNAL_TF THRESHOLD START_YEAR START_MONTH END_YEAR END_YEAR_MONTH

NCPU=$(sysctl -n hw.logicalcpu 2>/dev/null || nproc 2>/dev/null || echo 4)
echo "symbol,trades,wins,net_usd,long_net,short_net" > "$OUT"
echo "$SYMBOLS" | tr ' ' '\n' | xargs -n1 -P"$NCPU" -I{} bash -c 'run_one "$@"' _ {} >> "$OUT"

echo "→ wrote $OUT"
echo ""

python3 - "$OUT" <<'EOF'
import csv, sys
from pathlib import Path
rows = list(csv.DictReader(open(sys.argv[1])))
n = sum(1 for r in rows if r["net_usd"])
n_prof = sum(1 for r in rows if float(r.get("net_usd","0") or 0) > 0)
total = sum(float(r.get("net_usd","0") or 0) for r in rows)
trades = sum(int(r.get("trades","0") or 0) for r in rows)
wins = sum(int(r.get("wins","0") or 0) for r in rows)
long_net = sum(float(r.get("long_net","0") or 0) for r in rows)
short_net = sum(float(r.get("short_net","0") or 0) for r in rows)
SPAN = 5.28
wr = wins/trades*100 if trades else 0
print(f"  Cat F1 aggregate:")
print(f"    symbols (with data): {n}")
print(f"    profitable:          {n_prof}/{n}")
print(f"    total trades:        {trades}")
print(f"    win rate:            {wr:.1f}%")
print(f"    total NET:           ${total:+,.0f}")
print(f"    annual NET:          ${total/SPAN:+,.0f}/yr")
print(f"    long NET:            ${long_net:+,.0f}")
print(f"    short NET:           ${short_net:+,.0f}")
print()
print(f"  Per-symbol breakdown (sorted by NET):")
rows_with_data = [r for r in rows if r["net_usd"]]
rows_with_data.sort(key=lambda r: -float(r["net_usd"]))
for r in rows_with_data:
    pnl = float(r["net_usd"])
    n_tr = int(r["trades"]) if r["trades"] else 0
    print(f"    {r['symbol']:<14}  trades={n_tr:>4}  NET=${pnl:>+11,.0f}")
EOF
