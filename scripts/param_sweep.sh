#!/usr/bin/env bash
# param_sweep.sh — 2D parameter sweep: min_rr × signal_type.
# Runs all (mode × symbol) combinations in parallel, capped at logical CPU count.
#
# Race-condition analysis:
#   • Per-worker output: $SYM_DIR/${mode}_${symbol}.csv — unique key per worker.
#   • Per-run temp config: mktemp — unique per invocation.
#   • Binary: read-only; no shared mutable state.
#   • Merge: sequential after all background jobs complete (wait).
#   → No race conditions.
#
# Usage:
#   ./scripts/param_sweep.sh
#   ./scripts/param_sweep.sh "BTCUSDT ETHUSDT SOLUSDT" 2020 2025 04
set -euo pipefail

SYMBOLS="${1:-BTCUSDT ETHUSDT BNBUSDT SOLUSDT XRPUSDT LINKUSDT LTCUSDT DOGEUSDT}"
START_YEAR="${2:-2020}"
END_YEAR="${3:-2025}"
END_YEAR_MONTH="${4:-04}"
RR_VALUES="1.0 1.5 2.0 2.5 3.0 3.5 4.0 5.0"

ROOT="$(cd "$(dirname "$0")/.." && pwd)"
BINARY=$(mktemp /tmp/sweep-bin.XXXXXXXX)
RESULTS=$(mktemp /tmp/sweep-results.XXXXXXXX)
SYM_DIR=$(mktemp -d /tmp/sweep-sym.XXXXXXXX)
trap 'rm -f "$BINARY" "$RESULTS"; rm -rf "$SYM_DIR"' EXIT

echo "→ Compiling..."
(cd "$ROOT" && go build -o "$BINARY" ./cmd/backtest)

NCPU=$(sysctl -n hw.logicalcpu 2>/dev/null || nproc 2>/dev/null || echo 4)

# Signal modes: (label, extra sed applied on top of csv_path + min_rr substitution)
declare -a MODE_LABELS=("both" "absorption_only" "breakout_only")
declare -a MODE_SEDS=(
    ""
    "s|breakout_body_ratio:.*|breakout_body_ratio: 99|"
    "s|wick_ratio:.*|wick_ratio: 99|"
)

# ── Per-worker function ───────────────────────────────────────────────────────
# Arguments: mode  symbol  extra_sed
# Writes exclusively to $SYM_DIR/${mode}_${symbol}.csv — no other worker touches it.
process_mode_symbol() {
    local mode=$1 symbol=$2 extra_sed=$3
    local out="${SYM_DIR}/${mode}_${symbol}.csv"

    for rr in $RR_VALUES; do
        for (( year = START_YEAR; year <= END_YEAR; year++ )); do
            local last=$(( year == END_YEAR ? 10#$END_YEAR_MONTH : 12 ))
            for (( m = 1; m <= last; m++ )); do
                local mm
                mm=$(printf '%02d' "$m")
                local csv="${ROOT}/data/${symbol}-1m-${year}-${mm}.csv"

                if [[ ! -f "$csv" ]]; then
                    printf '%s,%s,%s,%s,%s,0,0,0\n' \
                        "$symbol" "$year" "$mm" "$mode" "$rr" >> "$out"
                    continue
                fi

                local cfg
                cfg=$(mktemp /tmp/sweep-cfg.XXXXXXXX)

                local sed_cmd="s|csv_path:.*|csv_path: ${csv}|; s|min_rr:.*|min_rr: ${rr}|"
                [[ -n "$extra_sed" ]] && sed_cmd="${sed_cmd}; ${extra_sed}"
                sed "$sed_cmd" "${ROOT}/configs/default.yaml" > "$cfg"

                local summary
                summary=$("$BINARY" --config "$cfg" 2>&1 \
                    | jq -r 'select(.msg | test("SUMMARY")) | "\(.total_trades) \(.wins) \(.total_pnl_usd // 0)"' \
                    2>/dev/null || true)
                rm -f "$cfg"

                if [[ -n "$summary" ]]; then
                    local T W PUSD
                    read -r T W PUSD <<< "$summary"
                    printf '%s,%s,%s,%s,%s,%s,%s,%s\n' \
                        "$symbol" "$year" "$mm" "$mode" "$rr" "$T" "$W" "$PUSD" \
                        >> "$out"
                else
                    printf '%s,%s,%s,%s,%s,0,0,0\n' \
                        "$symbol" "$year" "$mm" "$mode" "$rr" >> "$out"
                fi
            done
        done
    done
    echo "  ✓ ${mode}/${symbol}" >&2
}

export ROOT BINARY START_YEAR END_YEAR END_YEAR_MONTH RR_VALUES SYM_DIR
export -f process_mode_symbol

# ── Parallel dispatch using background jobs + portable semaphore ──────────────
# We don't use xargs here because each job needs two string arguments (mode + sed
# expression) which can contain spaces — xargs word-splitting would corrupt them.
# Instead we track PIDs and throttle manually.  bash 3.2-compatible (macOS).

pids=()

throttle() {
    # If we've hit the concurrency cap, wait for the oldest job to finish.
    while [[ ${#pids[@]} -ge $NCPU ]]; do
        wait "${pids[0]}" 2>/dev/null || true
        pids=("${pids[@]:1}")
    done
}

n_jobs=$(( ${#MODE_LABELS[@]} * $(echo "$SYMBOLS" | wc -w | tr -d ' ') ))
echo ""
echo "→ Dispatching ${n_jobs} workers (max ${NCPU} parallel)..." >&2

for mode_idx in "${!MODE_LABELS[@]}"; do
    mode="${MODE_LABELS[$mode_idx]}"
    extra_sed="${MODE_SEDS[$mode_idx]}"
    for symbol in $SYMBOLS; do
        throttle
        process_mode_symbol "$mode" "$symbol" "$extra_sed" &
        pids+=($!)
    done
done

# Wait for all remaining background jobs.
for pid in "${pids[@]}"; do
    wait "$pid" 2>/dev/null || true
done
echo "" >&2

# ── Sequential merge ──────────────────────────────────────────────────────────
echo "symbol,year,month,mode,min_rr,trades,wins,total_usd" > "$RESULTS"
for f in "${SYM_DIR}"/*.csv; do
    [[ -f "$f" ]] && cat "$f"
done >> "$RESULTS"

# ── Aggregate and display ─────────────────────────────────────────────────────
awk -F, \
    -v symbols="$SYMBOLS" \
    -v rrs="$RR_VALUES" \
    -v start_year="$START_YEAR" \
    -v end_year="$END_YEAR" \
'
NR > 1 {
    sym=$1; yr=$2+0; mode=$4; rr=$5
    t=$6+0; w=$7+0; usd=$8+0
    mr_t[mode,rr]+=t; mr_w[mode,rr]+=w; mr_usd[mode,rr]+=usd
    mrsy_usd[mode,rr,sym,yr]+=usd
}

function kfmt(v) { return sprintf("%+.0fk", v/1000) }

END {
    n_syms  = split(symbols, sym_arr, " ")
    n_rrs   = split(rrs,     rr_arr,  " ")
    n_years = 0
    for (y = start_year+0; y <= end_year+0; y++) yr_arr[++n_years] = y

    sep = ""; for(i=0;i<90;i++) sep=sep"═"
    modes[1]="both"; modes[2]="absorption_only"; modes[3]="breakout_only"

    for (mi=1;mi<=3;mi++) {
        mode = modes[mi]
        printf "\n%s\n  MODE: %-20s  ($1k stake, %d symbols, %d-%d)\n%s\n\n", \
            sep, mode, n_syms, start_year, end_year, sep

        printf "%-7s  %8s  %5s  %10s  %9s  %s\n", \
            "min_rr", "trades", "win%", "total_$", "avg_$/sym", "syms_profitable"
        printf "%-7s  %8s  %5s  %10s  %9s\n", \
            "───────","────────","─────","──────────","─────────"

        for (ri=1;ri<=n_rrs;ri++) {
            rr=rr_arr[ri]
            t=mr_t[mode,rr]+0; w=mr_w[mode,rr]+0; u=mr_usd[mode,rr]+0
            if (t==0) { printf "%-7s  (no trades)\n", rr; continue }

            pos_syms=0; sym_detail=""
            for (si=1;si<=n_syms;si++) {
                sym=sym_arr[si]
                sym_usd=0
                for (yi=1;yi<=n_years;yi++) sym_usd+=mrsy_usd[mode,rr,sym,yr_arr[yi]]+0
                if (sym_usd > 0) pos_syms++
                sym_detail=sym_detail sprintf(" %s:%s", sym, kfmt(sym_usd))
            }
            flag=""; if (u > 0) flag=" ✓"
            printf "%-7s  %8d  %4.1f%%  %+10.0f  %+9.0f%s  | %d/%d |%s\n", \
                rr, t, w/t*100, u, u/n_syms, flag, pos_syms, n_syms, sym_detail
        }
    }
    printf "\n%s\n", sep
}
' "$RESULTS"
