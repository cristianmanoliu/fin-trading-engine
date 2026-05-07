#!/usr/bin/env bash
# slip_cliff.sh — fine-grained slip sensitivity sweep at the deployed candidate
# parameters (4H short EMA9/21 mh504 target_rr=6 fee=10), to identify the exact
# slip level at which the strategy transitions from profitable to broken. This
# sharpens the kill-switch threshold ("realized stop-side slip > 25 bp" per
# CLAUDE.md ## Forward-paper go/no-go criteria) by quantifying what's just
# beyond and just below 25 bp.
#
# Sweep is parameter-sensitivity, not strategy-tuning — slip is an UNCONTROLLED
# execution variable (you don't choose it, the market gives it to you), so a
# sensitivity sweep is robustness mapping rather than candidate generation. It
# does not consume the same statistical degrees-of-freedom as new entry signals.
#
# Output: results/slip_cliff_<date>.csv, columns:
#   slip_bps, n_symbols, n_profitable, total_net_usd, total_trades, total_wins,
#   wr_pct, annual_net_usd
#
# Usage:
#   bash scripts/slip_cliff.sh
#   SYMBOL_GROUP=universe bash scripts/slip_cliff.sh
#   SLIP_BPS_LIST="5 10 15" bash scripts/slip_cliff.sh   # custom slip list
set -euo pipefail

ROOT="$(cd "$(dirname "$0")/.." && pwd)"
DATE_TAG="${DATE_TAG:-$(date +%F)}"
SYMBOL_GROUP="${SYMBOL_GROUP:-deployed}"
SLIP_BPS_LIST="${SLIP_BPS_LIST:-5 10 15 20 22 25 27 30 35}"
FEE_BPS="${FEE_BPS:-10}"
TARGET_RR="${TARGET_RR:-6.0}"
MAX_HOLD_HOURS="${MAX_HOLD_HOURS:-504}"
SIDE_FILTER="${SIDE_FILTER:-short}"
SIGNAL_TF="${SIGNAL_TF:-4H}"

OUT="${OUT:-${ROOT}/results/slip_cliff_${SYMBOL_GROUP}_${DATE_TAG}.csv}"
BINARY=$(mktemp /tmp/slip-bin.XXXXXXXX)
WORKDIR=$(mktemp -d /tmp/slip-sweep.XXXXXXXX)
trap 'rm -f "$BINARY"; rm -rf "$WORKDIR"' EXIT

source "${ROOT}/scripts/lib/symbols.sh"
SYMBOLS=$(get_symbols "$SYMBOL_GROUP")

START_YEAR=2020
END_YEAR=2025
END_YEAR_MONTH=04

(cd "$ROOT" && go build -o "$BINARY" ./cmd/backtest)
echo "→ binary built; symbols: $(echo "$SYMBOLS" | wc -w)  group: $SYMBOL_GROUP"
echo "→ slip values: $SLIP_BPS_LIST"
echo "→ output: $OUT"
echo ""

# Merge CSVs once per symbol — reused across all slip values.
echo "→ building merged CSVs (one-time)..."
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
ready_count=0
for symbol in $SYMBOLS; do
    [[ -s "${WORKDIR}/${symbol}.csv" ]] && ready_count=$((ready_count+1))
done
echo "  → $ready_count of $(echo "$SYMBOLS" | wc -w) symbols have data"
echo ""

# Generate per-symbol config once (slip is a CLI flag, not in YAML).
for symbol in $SYMBOLS; do
    cfg="${WORKDIR}/cfg-${symbol}.yaml"
    sed "
        s|symbol:.*|symbol: ${symbol}|;
        s|momentum_mode:.*|momentum_mode: false|;
        s|vwap_deviation_mode:.*|vwap_deviation_mode: false|;
        s|ema_mode:.*|ema_mode: true|;
        s|target_rr:.*|target_rr: ${TARGET_RR}|;
        s|csv_path:.*|csv_path: ${WORKDIR}/${symbol}.csv|
    " "${ROOT}/configs/default.yaml" > "$cfg"
done

run_one() {
    local symbol=$1
    local slip_bps=$2
    local cfg="${WORKDIR}/cfg-${symbol}.yaml"
    local merged="${WORKDIR}/${symbol}.csv"
    [[ -s "$merged" ]] || { echo "${symbol},${slip_bps},,,,,"; return; }

    local result
    result=$("$BINARY" --config "$cfg" \
        --exact-fills --include-boundary \
        --fee-bps "$FEE_BPS" --stop-slippage-bps "$slip_bps" \
        --funding-bps-per-day 0 \
        --funding-csv-dir "${ROOT}/data/funding" \
        --signal-tf "$SIGNAL_TF" --side-filter "$SIDE_FILTER" \
        --max-hold-hours "$MAX_HOLD_HOURS" \
        2>&1 | grep "BACKTEST SUMMARY")

    # Pull stats from the SUMMARY line (single JSONL record).
    local trades=$(echo "$result" | grep -oE '"total_trades":[0-9]+' | head -1 | cut -d: -f2)
    local wins=$(echo "$result" | grep -oE '"wins":[0-9]+' | head -1 | cut -d: -f2)
    local pnl=$(echo "$result" | grep -oE '"total_pnl_usd":-?[0-9.]+' | head -1 | cut -d: -f2)
    echo "${symbol},${slip_bps},${trades:-0},${wins:-0},${pnl:-0}"
}

export -f run_one
export ROOT WORKDIR BINARY FEE_BPS TARGET_RR MAX_HOLD_HOURS SIDE_FILTER SIGNAL_TF

NCPU=$(sysctl -n hw.logicalcpu 2>/dev/null || nproc 2>/dev/null || echo 4)
echo "→ running per-symbol × per-slip in ${NCPU}-way parallel..."

# Per-symbol per-slip raw output goes to a temp CSV first.
RAW_CSV="${WORKDIR}/raw.csv"
echo "symbol,slip_bps,trades,wins,pnl_usd" > "$RAW_CSV"

# Build the full job list (symbol × slip) and pipe to xargs.
{
    for slip in $SLIP_BPS_LIST; do
        for symbol in $SYMBOLS; do
            echo "${symbol} ${slip}"
        done
    done
} | xargs -n2 -P"$NCPU" bash -c 'run_one "$@"' _ >> "$RAW_CSV"

# Aggregate per-slip — count, sum, profitable count.
python3 - "$RAW_CSV" "$OUT" <<'EOF'
import csv, sys
from collections import defaultdict

raw_path, out_path = sys.argv[1], sys.argv[2]

per_slip = defaultdict(lambda: {"n":0, "n_prof":0, "trades":0, "wins":0, "pnl":0.0, "symbols":[]})

with open(raw_path) as f:
    reader = csv.DictReader(f)
    for row in reader:
        if not row["pnl_usd"]:
            continue
        slip = int(row["slip_bps"])
        pnl = float(row["pnl_usd"])
        bucket = per_slip[slip]
        bucket["n"] += 1
        if pnl > 0: bucket["n_prof"] += 1
        bucket["trades"] += int(row["trades"])
        bucket["wins"]   += int(row["wins"])
        bucket["pnl"]   += pnl
        bucket["symbols"].append((row["symbol"], pnl))

# Use 5.28y span as in bootstrap script (matches actual data).
SPAN_YEARS = 5.28

with open(out_path, "w", newline="") as f:
    w = csv.writer(f)
    w.writerow(["slip_bps","n_symbols","n_profitable","total_net_usd","total_trades","total_wins","wr_pct","annual_net_usd"])
    for slip in sorted(per_slip):
        b = per_slip[slip]
        wr = (b["wins"] / b["trades"] * 100) if b["trades"] else 0
        annual = b["pnl"] / SPAN_YEARS
        w.writerow([slip, b["n"], b["n_prof"], f"{b['pnl']:.2f}", b["trades"], b["wins"], f"{wr:.2f}", f"{annual:.2f}"])
        # Print to stdout too.
        print(f"slip={slip:>3}bp  n={b['n']:>2}  prof={b['n_prof']:>2}  trades={b['trades']:>4}  WR={wr:>5.2f}%  NET=${b['pnl']:>+13,.0f}  annual=${annual:>+11,.0f}/yr")

print()
print(f"✓ saved {out_path}")
EOF
