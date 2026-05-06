#!/usr/bin/env bash
# sweep_parallel.sh — generic parallelised parameter sweep.
#
# Each symbol runs in its own background worker.  Workers write to a dedicated
# file under SYM_DIR so there are zero shared write targets during the parallel
# phase.  The merge into RESULTS happens after all workers have exited.
#
# Race-condition analysis:
#   • Per-worker output: $SYM_DIR/${symbol}.csv — unique key, no shared writes.
#   • Per-run temp config: mktemp — unique per invocation, deleted by the worker.
#   • Binary: read-only (CSV + config); no shared mutable state.
#   • Merge: sequential, after xargs returns (all workers done).
#   → No race conditions.
#
# Required exports from the caller:
#   BINARY  ROOT  RESULTS  SYMBOLS
#   START_YEAR  END_YEAR  END_YEAR_MONTH
#   RR_VALUES   — space-separated values to substitute for target_rr
#   SED_EXTRA   — additional sed expression applied on top of csv_path + target_rr

set -euo pipefail

NCPU=$(sysctl -n hw.logicalcpu 2>/dev/null || nproc 2>/dev/null || echo 4)

SYM_DIR=$(mktemp -d /tmp/sweep-sym.XXXXXXXX)
# Ensure SYM_DIR is always cleaned up, even on unexpected exit.
trap 'rm -rf "$SYM_DIR"' EXIT

# ── Per-symbol worker ────────────────────────────────────────────────────────
# Each invocation owns exactly one output file: $SYM_DIR/${symbol}.csv.
# All writes go to that file only — no locking needed.
process_symbol() {
    local symbol=$1
    local sym_file="${SYM_DIR}/${symbol}.csv"

    for rr in $RR_VALUES; do
        for (( year = START_YEAR; year <= END_YEAR; year++ )); do
            local last=$(( year == END_YEAR ? 10#$END_YEAR_MONTH : 12 ))
            for (( m = 1; m <= last; m++ )); do
                local mm
                mm=$(printf '%02d' "$m")
                local csv="${ROOT}/data/${symbol}-1m-${year}-${mm}.csv"

                if [[ ! -f "$csv" ]]; then
                    printf '%s,%s,%s,%s,0,0,0\n' "$symbol" "$year" "$mm" "$rr" \
                        >> "$sym_file"
                    continue
                fi

                # mktemp produces a unique path — no collision across workers.
                local cfg
                cfg=$(mktemp /tmp/sweep-cfg.XXXXXXXX)

                local sed_cmd="s|csv_path:.*|csv_path: ${csv}|; s|target_rr:.*|target_rr: ${rr}|"
                if [[ -n "${SED_EXTRA:-}" ]]; then
                    sed_cmd="${sed_cmd}; ${SED_EXTRA}"
                fi
                sed "$sed_cmd" "${ROOT}/configs/default.yaml" > "$cfg"

                local summary
                summary=$("$BINARY" --config "$cfg" ${BINARY_EXTRA_ARGS:-} 2>&1 \
                    | jq -r 'select(.msg | test("SUMMARY")) | "\(.total_trades) \(.wins) \(.total_pnl_usd // 0)"' \
                    2>/dev/null || true)
                rm -f "$cfg"   # clean up immediately; not deferred so it runs even mid-loop

                if [[ -n "$summary" ]]; then
                    local T W PUSD
                    read -r T W PUSD <<< "$summary"
                    printf '%s,%s,%s,%s,%s,%s,%s\n' \
                        "$symbol" "$year" "$mm" "$rr" "$T" "$W" "$PUSD" \
                        >> "$sym_file"
                else
                    printf '%s,%s,%s,%s,0,0,0\n' "$symbol" "$year" "$mm" "$rr" \
                        >> "$sym_file"
                fi
            done
        done
    done

    echo "  ✓ ${symbol}" >&2
}

export -f process_symbol
export SYM_DIR

# ── Parallel dispatch via xargs ───────────────────────────────────────────────
# -P $NCPU  : run at most NCPU workers concurrently
# -I {}     : substitute {} with one symbol per line (single token — no quoting issues)
# bash -c … : spawn a new shell so exported functions and vars are visible
#
# One symbol per xargs item → no multi-field parsing, no word-splitting risk.
n_syms=$(echo "$SYMBOLS" | wc -w | tr -d ' ')
echo "→ Dispatching ${n_syms} symbols across ${NCPU} workers..." >&2

echo "$SYMBOLS" | tr ' ' '\n' \
    | xargs -P "$NCPU" -I{} \
        bash -c 'process_symbol "$@"' _ {}

echo "" >&2

# ── Sequential merge (all workers have exited at this point) ──────────────────
for f in "${SYM_DIR}"/*.csv; do
    [[ -f "$f" ]] && cat "$f"
done >> "$RESULTS"

# SYM_DIR is removed by the EXIT trap above.
