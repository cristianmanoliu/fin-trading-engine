#!/usr/bin/env bash
# p4_fresh_oos.sh — run P4-Combined on a custom date window for fresh-OOS validation.
#
# Default window: 2025-05 .. 2026-04 (12 months of data the original cost-survivor
# battery from 2026-05-05 never saw). Pure out-of-sample test of the deployed-16
# strategy: did the edge survive into data we never selected against?
#
# Configurable via env:
#   START_YEAR / START_MONTH / END_YEAR / END_MONTH — date window
#   SLIP_BPS — stop slippage in bp (default 15)
#   FEE_BPS — round-trip fee in bp (default 10)
#   TARGET_RR — R:R multiplier (default 6.0)
#   SIGNAL_TF — signal timeframe (default 4H)
#   SIDE_FILTER — both | long | short (default short for P4-Combined)
#   MAX_HOLD_HOURS — force-close cap (default 336 = 14 days, P4-Combined deployed)
#   FUNDING_DIR — per-symbol funding CSV dir (default data/funding)
set -euo pipefail

ROOT="$(cd "$(dirname "$0")/.." && pwd)"

START_YEAR="${START_YEAR:-2025}"
START_MONTH="${START_MONTH:-5}"
END_YEAR="${END_YEAR:-2026}"
END_MONTH="${END_MONTH:-4}"

SIGNAL_TF="${SIGNAL_TF:-4H}"
TARGET_RR="${TARGET_RR:-6.0}"
FEE_BPS="${FEE_BPS:-10}"
SLIP_BPS="${SLIP_BPS:-15}"
SIDE_FILTER="${SIDE_FILTER:-short}"
MAX_HOLD_HOURS="${MAX_HOLD_HOURS:-336}"
FUNDING_DIR="${FUNDING_DIR:-data/funding}"

WINDOW_LABEL="${START_YEAR}-$(printf '%02d' "$START_MONTH")_to_${END_YEAR}-$(printf '%02d' "$END_MONTH")"
OUT="${1:-results/fresh_oos_${WINDOW_LABEL}_slip${SLIP_BPS}_$(date +%F).txt}"

BINARY=$(mktemp /tmp/freshoos-bin.XXXXXXXX)
WORKDIR=$(mktemp -d /tmp/freshoos-sweep.XXXXXXXX)
trap 'rm -f "$BINARY"; rm -rf "$WORKDIR"' EXIT

source "$(dirname "${BASH_SOURCE[0]}")/lib/symbols.sh"
SYMBOLS="${SYMBOLS:-$(get_symbols universe)}"

NCPU=$(sysctl -n hw.logicalcpu 2>/dev/null || nproc 2>/dev/null || echo 4)

echo "→ Compiling..."
(cd "$ROOT" && go build -o "$BINARY" ./cmd/backtest)
echo "→ Fresh-OOS run: window=${WINDOW_LABEL}  signal_tf=${SIGNAL_TF}  target_rr=${TARGET_RR}  slip=${SLIP_BPS}bp  side=${SIDE_FILTER}  max_hold=${MAX_HOLD_HOURS}h"

run_symbol() {
    local symbol=$1
    local merged="${WORKDIR}/${symbol}.csv"

    # Concatenate only the months in the requested window.
    for (( y=START_YEAR; y<=END_YEAR; y++ )); do
        local m_first=1
        local m_last=12
        if [[ $y -eq $START_YEAR ]]; then m_first=$START_MONTH; fi
        if [[ $y -eq $END_YEAR ]];   then m_last=$END_MONTH; fi
        for (( m=m_first; m<=m_last; m++ )); do
            local mm
            mm=$(printf '%02d' "$m")
            local csv="${ROOT}/data/${symbol}-1m-${y}-${mm}.csv"
            [[ -f "$csv" ]] && cat "$csv" >> "$merged"
        done
    done
    if [[ ! -s "$merged" ]]; then
        echo -e "${symbol}\t0\t0\t0\t0\t0\t0\t0\t0\t0"
        return
    fi

    local cfg
    cfg=$(mktemp "${WORKDIR}/cfg-${symbol}.XXXXXXXX")
    sed "
        s|momentum_mode:.*|momentum_mode: false|;
        s|vwap_deviation_mode:.*|vwap_deviation_mode: false|;
        s|ema_mode:.*|ema_mode: true|;
        s|target_rr:.*|target_rr: ${TARGET_RR}|;
        s|csv_path:.*|csv_path: ${merged}|
    " "${ROOT}/configs/default.yaml" > "$cfg"

    local funding_arg=""
    if [[ -n "$FUNDING_DIR" ]]; then
        funding_arg="--funding-csv-dir ${FUNDING_DIR}"
    fi

    # NOTE: target_rr is configured via YAML (sed pattern above sets it in $cfg).
    # cmd/backtest does not have a --target-rr CLI flag; it's a YAML-only setting.
    # jq output is tab-separated so awk -F'\t' parses each field cleanly.
    local result
    result=$("$BINARY" --config "$cfg" \
        --exact-fills --include-boundary --pessimistic-ambiguous \
        --fee-bps "$FEE_BPS" --stop-slippage-bps "$SLIP_BPS" \
        --funding-bps-per-day 0 \
        $funding_arg \
        --signal-tf "$SIGNAL_TF" \
        --side-filter "$SIDE_FILTER" --max-hold-hours "$MAX_HOLD_HOURS" 2>&1 \
        | jq -r 'select(.msg | test("SUMMARY")) | [.total_trades, .wins, (.total_pnl_usd // 0), (.gross_pnl_usd // 0), (.total_fees_usd // 0), (.total_funding_usd // 0), (.long_net_usd // 0), (.short_net_usd // 0), (.avg_hold_hours // 0)] | @tsv' \
        2>/dev/null | tail -1)

    rm -f "$cfg" "$merged"
    if [[ -z "$result" ]]; then
        printf -v result '0\t0\t0\t0\t0\t0\t0\t0\t0'
    fi
    printf '%s\t%s\n' "$symbol" "$result"
}
export -f run_symbol
export ROOT BINARY WORKDIR FEE_BPS SLIP_BPS SIGNAL_TF TARGET_RR SIDE_FILTER MAX_HOLD_HOURS FUNDING_DIR START_YEAR START_MONTH END_YEAR END_MONTH

RAW=$(printf '%s\n' $SYMBOLS \
    | xargs -P "$NCPU" -I{} bash -c 'run_symbol "{}"')

{
echo ""
echo "════════════════════════════════════════════════════════════════════════════════════════"
echo "  FRESH OOS  window=${WINDOW_LABEL}  signal_tf=${SIGNAL_TF}  target_rr=${TARGET_RR}"
echo "  Costs: --fee-bps=${FEE_BPS}  --stop-slippage-bps=${SLIP_BPS}  funding=CSV  side=${SIDE_FILTER}  max_hold=${MAX_HOLD_HOURS}h"
echo "════════════════════════════════════════════════════════════════════════════════════════"
echo ""
printf "%-15s %7s %5s %6s %10s %10s %10s %10s %6s\n" \
    "Symbol" "Trades" "Wins" "WR%" "Gross_\$" "Fees_\$" "Long_NET" "Short_NET" "HoldHr"
printf "%-15s %7s %5s %6s %10s %10s %10s %10s %6s\n" \
    "──────" "──────" "────" "───" "────────" "──────" "────────" "─────────" "──────"

echo "$RAW" \
    | awk -F'\t' '{
        sym=$1; trades=$2; wins=$3; net=$4; gross=$5; fees=$6; fund=$7; lnet=$8; snet=$9; hh=$10
        wp = (trades>0) ? sprintf("%.1f", wins/trades*100) : "—"
        printf "%s\t%d\t%d\t%s\t%d\t%d\t%d\t%d\t%.1f\t%d\n", sym, trades, wins, wp, gross, fees, lnet, snet, hh, net
      }' \
    | sort -t$'\t' -k10 -rn \
    | awk -F'\t' '{
        printf "%-15s %7d %5d %5s%% %+9d %9d %+9d %+9d %5.1fh   net %+d\n",
          $1,$2,$3,$4,$5,$6,$7,$8,$9,$10
      }'

echo ""

# Aggregate stats
echo "$RAW" | awk -F'\t' '
BEGIN { t=0; w=0; net=0; gross=0; fees=0; fund=0; lnet=0; snet=0; n=0; pos=0; long_pos=0; short_pos=0 }
{
    if ($2 > 0) {
        t+=$2; w+=$3; net+=$4; gross+=$5; fees+=$6; fund+=$7; lnet+=$8; snet+=$9; n++
        if ($4>0) pos++
        if ($8>0) long_pos++
        if ($9>0) short_pos++
    }
}
END {
    wp = (t>0) ? sprintf("%.2f%%", w/t*100) : "—"
    printf "────────────────────────────────────────────────────────────────────────────────────\n"
    printf "TOTAL  %d sym (with trades)  %d trades  %s WR\n", n, t, wp
    printf "  Gross %+.0f  Fees %.0f  Funding %.0f  →  NET %+.0f  | %d/%d sym profitable\n", gross, fees, fund, net, pos, n
    printf "  Long_NET total: %+.0f  (%d/%d sym positive on long side)\n", lnet, long_pos, n
    printf "  Short_NET total: %+.0f  (%d/%d sym positive on short side)\n", snet, short_pos, n
}
'

} | tee "$OUT"

echo ""
echo "→ Output: $OUT"
