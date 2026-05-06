#!/usr/bin/env bash
# option_b_sweep.sh — sweeps vwap_deviation_pct for Option B (VWAP mean reversion).
# Runs all symbols in parallel — one worker per symbol, capped at logical CPU count.
#
# Usage:
#   ./scripts/option_b_sweep.sh
#   ./scripts/option_b_sweep.sh "BTCUSDT ETHUSDT" 2020 2025 04
set -euo pipefail

SYMBOLS="${1:-BTCUSDT ETHUSDT BNBUSDT SOLUSDT XRPUSDT LINKUSDT LTCUSDT DOGEUSDT ADAUSDT AVAXUSDT ATOMUSDT TRXUSDT}"
START_YEAR="${2:-2020}"
END_YEAR="${3:-2025}"
END_YEAR_MONTH="${4:-04}"
# Option B sweeps deviation threshold, not target_rr.  Reuse RR_VALUES slot.
RR_VALUES="0.002 0.003 0.005 0.008 0.010 0.015 0.020"

ROOT="$(cd "$(dirname "$0")/.." && pwd)"
BINARY=$(mktemp /tmp/sweep-bin.XXXXXXXX)
RESULTS=$(mktemp /tmp/sweep-results.XXXXXXXX)
trap 'rm -f "$BINARY" "$RESULTS"' EXIT

echo "→ Compiling..."
(cd "$ROOT" && go build -o "$BINARY" ./cmd/backtest)

# sweep_parallel.sh substitutes target_rr with each RR_VALUES entry; here we
# substitute vwap_deviation_pct instead via the target_rr sed line override.
SED_EXTRA="s|momentum_mode:.*|momentum_mode: false|; s|vwap_deviation_mode:.*|vwap_deviation_mode: true|; s|ema_mode:.*|ema_mode: false|; s|min_rr:.*|min_rr: 1.0|"

# Override: replace target_rr substitution with vwap_deviation_pct substitution.
# We achieve this by redefining the sed expression that sweep_parallel applies.
# sweep_parallel always does: s|target_rr:.*|target_rr: ${rr}|
# For Option B we want: s|vwap_deviation_pct:.*|vwap_deviation_pct: ${rr}|
# We accomplish this by appending a corrective sed expression that fixes up
# the target_rr line back to its original value and sets vwap_deviation_pct.
# Simpler: set target_rr to a fixed 2.0 (unused in this mode) and also set dev_pct.
SED_EXTRA="${SED_EXTRA}; s|vwap_deviation_pct:.*|vwap_deviation_pct: DEVPCT_PLACEHOLDER|"

echo "symbol,year,month,dev_pct,trades,wins,total_usd" > "$RESULTS"

# Option B needs a custom worker because the sweep dimension is vwap_deviation_pct,
# not target_rr.  We inline the worker here rather than using sweep_parallel.sh.
NCPU=$(sysctl -n hw.logicalcpu 2>/dev/null || nproc 2>/dev/null || echo 4)
SYM_DIR=$(mktemp -d /tmp/sweep-sym.XXXXXXXX)
trap 'rm -f "$BINARY" "$RESULTS"; rm -rf "$SYM_DIR"' EXIT

export SYM_DIR ROOT BINARY START_YEAR END_YEAR END_YEAR_MONTH RR_VALUES

process_symbol() {
    local symbol=$1
    local sym_file="${SYM_DIR}/${symbol}.csv"

    for dev in $RR_VALUES; do
        for (( year = START_YEAR; year <= END_YEAR; year++ )); do
            local last=$(( year == END_YEAR ? 10#$END_YEAR_MONTH : 12 ))
            for (( m = 1; m <= last; m++ )); do
                local mm
                mm=$(printf '%02d' "$m")
                local csv="${ROOT}/data/${symbol}-1m-${year}-${mm}.csv"

                if [[ ! -f "$csv" ]]; then
                    echo "${symbol},${year},${mm},${dev},0,0,0" >> "$sym_file"
                    continue
                fi

                local cfg
                cfg=$(mktemp /tmp/sweep-cfg.XXXXXXXX)
                sed "s|csv_path:.*|csv_path: ${csv}|; \
                     s|momentum_mode:.*|momentum_mode: false|; \
                     s|vwap_deviation_mode:.*|vwap_deviation_mode: true|; \
                     s|ema_mode:.*|ema_mode: false|; \
                     s|vwap_deviation_pct:.*|vwap_deviation_pct: ${dev}|; \
                     s|min_rr:.*|min_rr: 1.0|" \
                    "${ROOT}/configs/default.yaml" > "$cfg"

                local summary
                summary=$("$BINARY" --config "$cfg" 2>&1 \
                    | jq -r 'select(.msg | test("SUMMARY")) | "\(.total_trades) \(.wins) \(.total_pnl_usd // 0)"' \
                    2>/dev/null || true)
                rm -f "$cfg"

                if [[ -n "$summary" ]]; then
                    local T W PUSD
                    read -r T W PUSD <<< "$summary"
                    echo "${symbol},${year},${mm},${dev},${T},${W},${PUSD}" >> "$sym_file"
                else
                    echo "${symbol},${year},${mm},${dev},0,0,0" >> "$sym_file"
                fi
            done
        done
    done
    echo "  ✓ ${symbol}" >&2
}

export -f process_symbol

n_syms=$(echo "$SYMBOLS" | wc -w | tr -d ' ')
echo "→ Dispatching ${n_syms} symbols across ${NCPU} workers..." >&2
echo "$SYMBOLS" | tr ' ' '\n' | xargs -P "$NCPU" -I{} bash -c 'process_symbol "$@"' _ {}
echo "" >&2

for f in "$SYM_DIR"/*.csv; do
    [[ -f "$f" ]] && cat "$f"
done >> "$RESULTS"
rm -rf "$SYM_DIR"

awk -F, \
    -v symbols="$SYMBOLS" \
    -v devs="$RR_VALUES" \
    -v start_year="$START_YEAR" \
    -v end_year="$END_YEAR" \
'
NR > 1 {
    sym=$1; yr=$2+0; dev=$4
    t=$5+0; w=$6+0; usd=$7+0
    dev_t[dev]+=t; dev_w[dev]+=w; dev_usd[dev]+=usd
    devsy_usd[dev,sym,yr]+=usd
}

function kfmt(v) { return sprintf("%+.0fk", v/1000) }

END {
    n_syms = split(symbols, sym_arr, " ")
    n_devs = split(devs,    dev_arr, " ")
    n_years = 0
    for (y = start_year+0; y <= end_year+0; y++) yr_arr[++n_years] = y

    sep = ""; for(i=0;i<100;i++) sep=sep"═"
    printf "\n%s\n  OPTION B — VWAP DEVIATION MODE  vwap_deviation_pct sweep  ($1k stake, %d symbols, %d–%d)\n%s\n\n", \
        sep, n_syms, start_year, end_year, sep

    printf "%-8s  %8s  %5s  %10s  %9s  %s\n", \
        "dev_pct", "trades", "win%", "total_$", "avg_$/sym", "syms_profitable"
    printf "%-8s  %8s  %5s  %10s  %9s\n", \
        "────────","────────","─────","──────────","─────────"

    for (di=1;di<=n_devs;di++) {
        dev=dev_arr[di]
        t=dev_t[dev]+0; w=dev_w[dev]+0; u=dev_usd[dev]+0
        if (t==0) { printf "%-8s  (no trades)\n", dev; continue }
        pos_syms=0
        sym_detail=""
        for (si=1;si<=n_syms;si++) {
            sym=sym_arr[si]
            sym_usd=0
            for (yi=1;yi<=n_years;yi++) sym_usd+=devsy_usd[dev,sym,yr_arr[yi]]+0
            if (sym_usd > 0) pos_syms++
            sym_detail=sym_detail sprintf(" %s:%s", sym, kfmt(sym_usd))
        }
        flag=""; if (u > 0) flag=" ✓"
        printf "%-8s  %8d  %4.1f%%  %+10.0f  %+9.0f%s  | %d/%d syms |%s\n", \
            dev, t, w/t*100, u, u/n_syms, flag, pos_syms, n_syms, sym_detail
    }
    printf "\n%s\n", sep
}
' "$RESULTS"
