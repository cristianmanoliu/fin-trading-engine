#!/usr/bin/env bash
# audit_phase4_btc.sh — run BTC 5y at target_rr=5.0 under four flag combinations:
#   (1) --exact-fills                                  (baseline reproduction)
#   (2) --exact-fills --include-boundary               (Test α: boundary-tick fix)
#   (3) --exact-fills --pessimistic-ambiguous          (Test β: same-bar resolution)
#   (4) --exact-fills --include-boundary --pessimistic-ambiguous (fully realistic)
#
# Captures total trades / wins / total $ / ambiguous override count and stop-distance
# percentiles for each combination. Prints a side-by-side comparison.
set -euo pipefail

SYMBOL="${1:-BTCUSDT}"
START_YEAR=2020
END_YEAR=2025
END_YEAR_MONTH=04

ROOT="$(cd "$(dirname "$0")/.." && pwd)"
BINARY=$(mktemp /tmp/audit-bin.XXXXXXXX)
trap 'rm -f "$BINARY"' EXIT

echo "→ Compiling..."
(cd "$ROOT" && go build -o "$BINARY" ./cmd/backtest)

NCPU=$(sysctl -n hw.logicalcpu 2>/dev/null || nproc 2>/dev/null || echo 4)

# Sed expression: enable Option C, disable A and B, set target_rr=5.0
SED_BASE="s|momentum_mode:.*|momentum_mode: false|; s|vwap_deviation_mode:.*|vwap_deviation_mode: false|; s|ema_mode:.*|ema_mode: true|; s|target_rr:.*|target_rr: 5.0|"

run_one_month() {
    local args=$1
    local year=$2
    local mm=$3
    local csv="${ROOT}/data/${SYMBOL}-1m-${year}-${mm}.csv"
    if [[ ! -f "$csv" ]]; then
        echo "0 0 0 0"
        return
    fi
    local cfg
    cfg=$(mktemp /tmp/audit-cfg.XXXXXXXX)
    sed "${SED_BASE}; s|csv_path:.*|csv_path: ${csv}|" "${ROOT}/configs/default.yaml" > "$cfg"

    # Extract: total_trades, wins, total_pnl_usd, ambiguous_overrides
    "$BINARY" --config "$cfg" $args 2>&1 \
        | jq -r 'select(.msg | test("SUMMARY")) | "\(.total_trades) \(.wins) \(.total_pnl_usd // 0) \(.ambiguous_overrides // 0)"' \
        2>/dev/null || echo "0 0 0 0"
    rm -f "$cfg"
}
export -f run_one_month
export ROOT SYMBOL BINARY SED_BASE

run_combo() {
    local label=$1
    local args=$2
    echo ""
    echo "════════ ${label} ════════"
    echo "  Args: ${args}"

    # Build month list: (YYYY MM)*
    local months=()
    for (( y=START_YEAR; y<=END_YEAR; y++ )); do
        local last=$(( y == END_YEAR ? 10#$END_YEAR_MONTH : 12 ))
        for (( m=1; m<=last; m++ )); do
            months+=("$y $(printf '%02d' $m)")
        done
    done

    # Run in parallel via xargs
    local results
    results=$(printf '%s\n' "${months[@]}" \
        | xargs -P "$NCPU" -I{} bash -c '
            read -r y m <<< "{}"
            run_one_month "'"$args"'" "$y" "$m"
        ')

    # Aggregate
    awk '{ t+=$1; w+=$2; u+=$3; a+=$4 } END { printf "  trades=%d wins=%d total_usd=%+.0f ambiguous_overrides=%d\n", t, w, u, a }' <<< "$results"
}

run_combo "(1) BASELINE: --exact-fills"                                         "--exact-fills"
run_combo "(2) Test α:  --exact-fills --include-boundary"                       "--exact-fills --include-boundary"
run_combo "(3) Test β:  --exact-fills --pessimistic-ambiguous"                  "--exact-fills --pessimistic-ambiguous"
run_combo "(4) FULLY REALISTIC: --exact-fills --include-boundary --pessimistic" "--exact-fills --include-boundary --pessimistic-ambiguous"

echo ""
echo "════════════════════════════════════════════"
echo "Decision baseline: BTC 5y target_rr=5.0 exact-fills = +\$191k"
echo "  - If (2) within ±20% of (1): boundary bias small."
echo "  - If (3) ambiguous_overrides / wins < 0.5%: same-bar bias immaterial."
echo "  - Final answer is (4): fully realistic backtest."
