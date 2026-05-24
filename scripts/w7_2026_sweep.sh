#!/usr/bin/env bash
# w7_2026_sweep.sh — W7 walk-forward window: Jan–Apr 2026, live candidate config.
# Pre-reg: results/w7_2026_backtest_decision_rule_2026-05-24.md
#
# Runs deployed-16 AND universe-57 in sequence.
# Flags match production cost model: fee=10bp, slip=5bp, exact-fills.
# Config matches live: 4H signal, EMA9/21, short-only, target_rr=6.0, mh=504h.
set -euo pipefail

ROOT="$(cd "$(dirname "$0")/.." && pwd)"
OUT_DIR="${ROOT}/results"
DATE=$(date +%F)
OUT_DEPLOYED="${OUT_DIR}/w7_2026_deployed16_${DATE}.txt"
OUT_UNIVERSE="${OUT_DIR}/w7_2026_universe57_${DATE}.txt"

BINARY=$(mktemp /tmp/w7-bin.XXXXXXXX)
WORKDIR=$(mktemp -d /tmp/w7-sweep.XXXXXXXX)
trap 'rm -f "$BINARY"; rm -rf "$WORKDIR"' EXIT

FEE_BPS=10
SLIP_BPS=5
START_YEAR=2026
START_MONTH=01
END_YEAR=2026
END_MONTH=04

source "${ROOT}/scripts/lib/symbols.sh"
DEPLOYED=$(get_symbols deployed)
UNIVERSE=$(get_symbols universe)

NCPU=$(sysctl -n hw.logicalcpu 2>/dev/null || nproc 2>/dev/null || echo 4)

# Live candidate config overrides (stake_usd also patched here; no --stake-usd CLI flag)
SED_BASE="s|momentum_mode:.*|momentum_mode: false|; \
s|vwap_deviation_mode:.*|vwap_deviation_mode: false|; \
s|ema_mode:.*|ema_mode: true|; \
s|target_rr:.*|target_rr: 6.0|; \
s|max_hold_hours:.*|max_hold_hours: 504|; \
s|stake_usd:.*|stake_usd: 1000|; \
s|stake_usdt:.*|stake_usdt: 1000|"

echo "→ Compiling binary..."
(cd "$ROOT" && go build -o "$BINARY" ./cmd/backtest)
echo "→ Binary: $BINARY"

run_symbol() {
    local symbol=$1
    local merged="${WORKDIR}/${symbol}.csv"

    local found=0
    for (( y=START_YEAR; y<=END_YEAR; y++ )); do
        local m_start=1
        local m_end=12
        [[ $y -eq START_YEAR ]] && m_start=$((10#$START_MONTH))
        [[ $y -eq END_YEAR   ]] && m_end=$((10#$END_MONTH))
        for (( m=m_start; m<=m_end; m++ )); do
            local mm
            mm=$(printf '%02d' "$m")
            local csv="${ROOT}/data/${symbol}-1m-${y}-${mm}.csv"
            if [[ -f "$csv" ]]; then
                cat "$csv" >> "$merged"
                found=1
            fi
        done
    done

    if [[ $found -eq 0 ]]; then
        echo "${symbol} 0 0 0 0 0"
        return
    fi

    local cfg
    cfg=$(mktemp "${WORKDIR}/cfg-${symbol}.XXXXXXXX")
    sed "${SED_BASE}; s|csv_path:.*|csv_path: ${merged}|" \
        "${ROOT}/configs/default.yaml" > "$cfg"

    local funding_flag=()
    [[ -f "${ROOT}/data/funding/${symbol}.csv" ]] && \
        funding_flag=(--funding-csv-dir "${ROOT}/data/funding")

    local result
    result=$("$BINARY" \
        --config "$cfg" \
        --signal-tf 4H \
        --side-filter short \
        --fee-bps "$FEE_BPS" \
        --stop-slippage-bps "$SLIP_BPS" \
        --exact-fills --include-boundary --pessimistic-ambiguous \
        "${funding_flag[@]}" 2>&1 \
        | jq -r 'select(.msg | test("SUMMARY")) | "\(.total_trades) \(.wins) \(.total_pnl_usd // 0) \(.gross_pnl_usd // 0) \(.total_fees_usd // 0)"' \
        2>/dev/null | tail -1)

    rm -f "$cfg" "$merged"
    echo "${symbol} ${result:-0 0 0 0 0}"
}
export -f run_symbol
export ROOT BINARY WORKDIR SED_BASE START_YEAR START_MONTH END_YEAR END_MONTH FEE_BPS SLIP_BPS

render_table() {
    local label="$1"
    local raw="$2"
    local out="$3"

    {
    echo ""
    echo "════════════════════════════════════════════════════════════════════════════════════════"
    echo "  W7 — EMA9×EMA21  4H  short-only  target_rr=6.0  mh=504h  LIVE CANDIDATE CONFIG"
    echo "  Universe: ${label}"
    echo "  Flags: --exact-fills --include-boundary --pessimistic-ambiguous"
    echo "         --fee-bps=${FEE_BPS}  --stop-slippage-bps=${SLIP_BPS}"
    echo "  Period: 2026-01 → 2026-04  |  \$1k stake per trade"
    echo "  Pre-reg: results/w7_2026_backtest_decision_rule_2026-05-24.md"
    echo "════════════════════════════════════════════════════════════════════════════════════════"
    echo ""
    printf "%-18s %8s %6s %6s %12s %12s %12s\n" "Symbol" "Trades" "Wins" "WinPct" "Gross_\$" "Fees_\$" "NET_\$"
    printf "%-18s %8s %6s %6s %12s %12s %12s\n" "──────" "──────" "────" "──────" "────────" "──────" "──────"

    echo "$raw" \
        | awk '{
            sym=$1; t=$2; w=$3; net=$4; gross=$5; fees=$6
            if (t>0) wp = sprintf("%.1f%%", w/t*100); else wp="–"
            printf "%s\t%d\t%d\t%s\t%.0f\t%.0f\t%.0f\n", sym, t, w, wp, gross, fees, net
          }' \
        | sort -t$'\t' -k7 -rn \
        | awk -F'\t' '{
            gsign = ($5 >= 0) ? "+" : ""
            nsign = ($7 >= 0) ? "+" : ""
            printf "%-18s %8d %6d %6s %s%10.0f %10.0f %s%10.0f\n", $1, $2, $3, $4, gsign, $5, $6, nsign, $7
          }'

    echo ""

    echo "$raw" | awk '
    BEGIN { t=0; w=0; net=0; gross=0; fees=0; n=0; pos=0; neg=0 }
    {
        t+=$2; w+=$3; net+=$4; gross+=$5; fees+=$6; n++
        if ($2>0) {
            if ($4>0) pos++; else neg++
        }
    }
    END {
        printf "────────────────────────────────────────────────────────────────────────────────────────\n"
        printf "TOTAL  %d symbols | %d trades | %d wins (%.1f%%)\n", n, t, w, (t>0?w/t*100:0)
        printf "       Gross:    %+.0f$\n", gross
        printf "       Fees:     %.0f$\n", fees
        printf "       NET:      %+.0f$\n", net
        printf "       %d/%d profitable symbols\n", pos, pos+neg
    }
    '
    } | tee "$out"
}

echo ""
echo "═══ PASS 1: deployed-16 ═══"
RAW_DEPLOYED=$(printf '%s\n' $DEPLOYED \
    | xargs -P "$NCPU" -I{} bash -c 'run_symbol "{}"')
render_table "deployed-16" "$RAW_DEPLOYED" "$OUT_DEPLOYED"

echo ""
echo "═══ PASS 2: universe-57 ═══"
RAW_UNIVERSE=$(printf '%s\n' $UNIVERSE \
    | xargs -P "$NCPU" -I{} bash -c 'run_symbol "{}"')
render_table "universe-57" "$RAW_UNIVERSE" "$OUT_UNIVERSE"

echo ""
echo "→ Deployed-16 results: $OUT_DEPLOYED"
echo "→ Universe-57 results: $OUT_UNIVERSE"
