#!/usr/bin/env bash
# p4_per_symbol.sh — per-symbol breakdown for the P4 winner (4H + wick stop)
# under realistic costs. Sorted by NET descending. Tells you whether the
# +$678k aggregate edge is concentrated in a few symbols (selection risk)
# or distributed across the universe (signal robustness).
#
# Configurable via env: SIGNAL_TF (default 4H), ATR_MULT (default 0 = wick),
# TARGET_RR (default 5.0), FEE_BPS (8), SLIP_BPS (5).
set -euo pipefail

ROOT="$(cd "$(dirname "$0")/.." && pwd)"
SIGNAL_TF="${SIGNAL_TF:-4H}"
ATR_MULT="${ATR_MULT:-0}"
TARGET_RR="${TARGET_RR:-5.0}"
FEE_BPS="${FEE_BPS:-10}"
SLIP_BPS="${SLIP_BPS:-5}"
FUNDING_BPS="${FUNDING_BPS:-0}"
TAX_RATE="${TAX_RATE:-0}"

LABEL="${SIGNAL_TF}_$([ "$ATR_MULT" = "0" ] && echo wick || echo "atr${ATR_MULT}")_rr${TARGET_RR}"
OUT="${1:-results/proto_persym_${LABEL}_$(date +%F).txt}"

BINARY=$(mktemp /tmp/persym-bin.XXXXXXXX)
WORKDIR=$(mktemp -d /tmp/persym-sweep.XXXXXXXX)
trap 'rm -f "$BINARY"; rm -rf "$WORKDIR"' EXIT

START_YEAR=2020
END_YEAR=2025
END_YEAR_MONTH=04

source "$(dirname "${BASH_SOURCE[0]}")/lib/symbols.sh"
SYMBOLS="${SYMBOLS:-$(get_symbols universe)}"

NCPU=$(sysctl -n hw.logicalcpu 2>/dev/null || nproc 2>/dev/null || echo 4)

echo "→ Compiling..."
(cd "$ROOT" && go build -o "$BINARY" ./cmd/backtest)
echo "→ Per-symbol run: signal_tf=${SIGNAL_TF} atr_mult=${ATR_MULT} target_rr=${TARGET_RR}  costs=${FEE_BPS}/${SLIP_BPS}bp"

ATR_ARG=""
[[ "$ATR_MULT" != "0" ]] && ATR_ARG="--atr-stop-mult $ATR_MULT"

run_symbol() {
    local symbol=$1
    local merged="${WORKDIR}/${symbol}.csv"
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
        echo "${symbol} 0 0 0 0 0"
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

    local result
    result=$("$BINARY" --config "$cfg" \
        --exact-fills --include-boundary --pessimistic-ambiguous \
        --fee-bps "$FEE_BPS" --stop-slippage-bps "$SLIP_BPS" \
        --funding-bps-per-day "$FUNDING_BPS" --tax-rate-pct "$TAX_RATE" \
        --signal-tf "$SIGNAL_TF" $ATR_ARG 2>&1 \
        | jq -r 'select(.msg | test("SUMMARY")) | "\(.total_trades) \(.wins) \(.total_pnl_usd // 0) \(.gross_pnl_usd // 0) \(.total_fees_usd // 0) \(.total_funding_usd // 0) \(.long_net_usd // 0) \(.short_net_usd // 0) \(.avg_hold_hours // 0)"' \
        2>/dev/null | tail -1)

    rm -f "$cfg" "$merged"
    echo "${symbol} ${result:-0 0 0 0 0 0 0 0 0}"
}
export -f run_symbol
export ROOT BINARY WORKDIR FEE_BPS SLIP_BPS FUNDING_BPS TAX_RATE TARGET_RR SIGNAL_TF ATR_ARG START_YEAR END_YEAR END_YEAR_MONTH

RAW=$(printf '%s\n' $SYMBOLS \
    | xargs -P "$NCPU" -I{} bash -c 'run_symbol "{}"')

{
echo ""
echo "════════════════════════════════════════════════════════════════════════════════════════"
echo "  PROTOTYPE per-symbol  signal_tf=${SIGNAL_TF}  atr=${ATR_MULT}  target_rr=${TARGET_RR}"
echo "  Costs: --fee-bps=${FEE_BPS}  --stop-slippage-bps=${SLIP_BPS}  --funding-bps-per-day=${FUNDING_BPS}  --tax-rate-pct=${TAX_RATE}"
echo "════════════════════════════════════════════════════════════════════════════════════════"
echo ""
printf "%-18s %7s %5s %5s %10s %10s %10s %10s %10s %6s\n" "Symbol" "Trades" "Wins" "WR%" "Gross_\$" "Fees_\$" "Fund_\$" "Long_NET" "Short_NET" "HoldHr"
printf "%-18s %7s %5s %5s %10s %10s %10s %10s %10s %6s\n" "──────" "──────" "────" "───" "────────" "──────" "──────" "────────" "─────────" "──────"

echo "$RAW" \
    | awk '{
        sym=$1; t=$2; w=$3; net=$4; gross=$5; fees=$6; fund=$7; lnet=$8; snet=$9; hh=$10
        wp = (t>0) ? sprintf("%.1f", w/t*100) : "—"
        printf "%s\t%d\t%d\t%s\t%d\t%d\t%d\t%d\t%d\t%.1f\t%d\n", sym, t, w, wp, gross, fees, fund, lnet, snet, hh, net
      }' \
    | sort -t$'\t' -k11 -rn \
    | awk -F'\t' '{
        printf "%-18s %7d %5d %5s%% %+9d %9d %9d %+9d %+9d %5.1fh   net %+d\n", $1,$2,$3,$4,$5,$6,$7,$8,$9,$10,$11
      }'

echo ""

# Aggregate stats
echo "$RAW" | awk '
BEGIN { t=0; w=0; net=0; gross=0; fees=0; fund=0; lnet=0; snet=0; n=0; pos=0; long_pos=0; short_pos=0 }
{
    t+=$2; w+=$3; net+=$4; gross+=$5; fees+=$6; fund+=$7; lnet+=$8; snet+=$9; n++
    if ($4>0) pos++
    if ($8>0) long_pos++
    if ($9>0) short_pos++
}
END {
    wp = (t>0) ? sprintf("%.2f%%", w/t*100) : "—"
    printf "────────────────────────────────────────────────────────────────────────────────────\n"
    printf "TOTAL  %d sym  %d trades  %s WR\n", n, t, wp
    printf "  Gross %+.0f  Fees %.0f  Funding %.0f  →  NET %+.0f  | %d/%d sym profitable\n", gross, fees, fund, net, pos, n
    printf "  Long_NET total: %+.0f  (%d/%d sym positive on long side)\n", lnet, long_pos, n
    printf "  Short_NET total: %+.0f  (%d/%d sym positive on short side)\n", snet, short_pos, n
}
'

echo ""
echo "Deployment shortlist (positive NET only, sorted desc):"
echo "$RAW" | awk '$4 > 0 { print $0 }' | sort -k4 -rn | awk '{ printf "  %-18s  net %+d  long %+d  short %+d\n", $1, $4, $8, $9 }'

} | tee "$OUT"

echo ""
echo "→ Output: $OUT"
