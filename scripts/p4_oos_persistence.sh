#!/usr/bin/env bash
# p4_oos_persistence.sh — Out-of-sample persistence test for the P4 winner.
# Split 5y into TRAIN (2020-01 → 2022-12) and TEST (2023-01 → 2025-04).
# Run each half independently per symbol with realistic costs. Report:
#   - Train-positive AND Test-positive count (truly persistent winners)
#   - Train-positive but Test-negative count (regime-flip casualties)
#   - Spearman correlation of per-symbol PnL between train and test
#
# Configurable: SIGNAL_TF (4H), ATR_MULT (0=wick), TARGET_RR (6.0).
set -euo pipefail

ROOT="$(cd "$(dirname "$0")/.." && pwd)"
SIGNAL_TF="${SIGNAL_TF:-4H}"
ATR_MULT="${ATR_MULT:-0}"
TARGET_RR="${TARGET_RR:-6.0}"
FEE_BPS="${FEE_BPS:-10}"
SLIP_BPS="${SLIP_BPS:-5}"
FUNDING_BPS="${FUNDING_BPS:-0}"
TAX_RATE="${TAX_RATE:-0}"
FUNDING_DIR="${FUNDING_DIR:-}"
EXTRA_ARGS="${EXTRA_ARGS:-}"

LABEL="${SIGNAL_TF}_$([ "$ATR_MULT" = "0" ] && echo wick || echo "atr${ATR_MULT}")_rr${TARGET_RR}"
OUT="${1:-results/proto_oos_${LABEL}_$(date +%F).txt}"

BINARY=$(mktemp /tmp/oos-bin.XXXXXXXX)
WORKDIR=$(mktemp -d /tmp/oos-sweep.XXXXXXXX)
trap 'rm -f "$BINARY"; rm -rf "$WORKDIR"' EXIT

TRAIN_START_YEAR=2020
TRAIN_END_YEAR=2022
TRAIN_END_MONTH=12

TEST_START_YEAR=2023
TEST_END_YEAR=2025
TEST_END_MONTH=04

SYMBOLS="RUNEUSDT SOLUSDT BNBUSDT HBARUSDT IOTAUSDT TRXUSDT XLMUSDT LINKUSDT GRTUSDT FTMUSDT SNXUSDT APEUSDT BLURUSDT AAVEUSDT MKRUSDT VETUSDT ARBUSDT ATOMUSDT PYTHUSDT BTCUSDT ENJUSDT 1INCHUSDT NEARUSDT KAVAUSDT AVAXUSDT TIAUSDT ETHUSDT DOTUSDT XRPUSDT GMXUSDT BCHUSDT ZILUSDT LDOUSDT DYDXUSDT UNIUSDT DOGEUSDT SEIUSDT 1000SHIBUSDT IMXUSDT INJUSDT ROSEUSDT GALAUSDT MANAUSDT OPUSDT ICPUSDT AXSUSDT ENSUSDT SUIUSDT WLDUSDT ADAUSDT FILUSDT SANDUSDT CHZUSDT LTCUSDT APTUSDT ETCUSDT CRVUSDT"

NCPU=$(sysctl -n hw.logicalcpu 2>/dev/null || nproc 2>/dev/null || echo 4)

echo "→ Compiling..."
(cd "$ROOT" && go build -o "$BINARY" ./cmd/backtest)
echo "→ Running OOS persistence  signal_tf=${SIGNAL_TF}  atr=${ATR_MULT}  target_rr=${TARGET_RR}"

ATR_ARG=""
[[ "$ATR_MULT" != "0" ]] && ATR_ARG="--atr-stop-mult $ATR_MULT"

# Pre-build train and test merged CSVs, one per symbol per split.
echo "→ Building merged CSVs (train + test)..."
for symbol in $SYMBOLS; do
    train_csv="${WORKDIR}/${symbol}_train.csv"
    test_csv="${WORKDIR}/${symbol}_test.csv"
    for (( y=TRAIN_START_YEAR; y<=TRAIN_END_YEAR; y++ )); do
        last=$(( y == TRAIN_END_YEAR ? 10#$TRAIN_END_MONTH : 12 ))
        for (( m=1; m<=last; m++ )); do
            mm=$(printf '%02d' "$m")
            csv="${ROOT}/data/${symbol}-1m-${y}-${mm}.csv"
            [[ -f "$csv" ]] && cat "$csv" >> "$train_csv"
        done
    done
    for (( y=TEST_START_YEAR; y<=TEST_END_YEAR; y++ )); do
        last=$(( y == TEST_END_YEAR ? 10#$TEST_END_MONTH : 12 ))
        for (( m=1; m<=last; m++ )); do
            mm=$(printf '%02d' "$m")
            csv="${ROOT}/data/${symbol}-1m-${y}-${mm}.csv"
            [[ -f "$csv" ]] && cat "$csv" >> "$test_csv"
        done
    done
done

run_split() {
    local symbol=$1
    local split=$2  # train | test
    local merged="${WORKDIR}/${symbol}_${split}.csv"
    if [[ ! -s "$merged" ]]; then
        echo "${symbol} ${split} 0 0 0"
        return
    fi
    local cfg
    cfg=$(mktemp "${WORKDIR}/cfg-${symbol}-${split}.XXXXXXXX")
    sed "
        s|momentum_mode:.*|momentum_mode: false|;
        s|vwap_deviation_mode:.*|vwap_deviation_mode: false|;
        s|ema_mode:.*|ema_mode: true|;
        s|target_rr:.*|target_rr: ${TARGET_RR}|;
        s|csv_path:.*|csv_path: ${merged}|
    " "${ROOT}/configs/default.yaml" > "$cfg"

    local funding_arg=""
    if [[ -n "$FUNDING_DIR" ]]; then
        funding_arg="--funding-csv-dir $FUNDING_DIR"
    fi
    local result
    result=$("$BINARY" --config "$cfg" \
        --exact-fills --include-boundary --pessimistic-ambiguous \
        --fee-bps "$FEE_BPS" --stop-slippage-bps "$SLIP_BPS" \
        --funding-bps-per-day "$FUNDING_BPS" --tax-rate-pct "$TAX_RATE" \
        $funding_arg \
        --signal-tf "$SIGNAL_TF" $ATR_ARG $EXTRA_ARGS 2>&1 \
        | jq -r 'select(.msg | test("SUMMARY")) | "\(.total_trades) \(.wins) \(.after_tax_pnl_usd // .total_pnl_usd // 0)"' \
        2>/dev/null | tail -1)

    rm -f "$cfg"
    echo "${symbol} ${split} ${result:-0 0 0}"
}
export -f run_split
export ROOT BINARY WORKDIR FEE_BPS SLIP_BPS FUNDING_BPS TAX_RATE FUNDING_DIR EXTRA_ARGS SIGNAL_TF ATR_ARG TARGET_RR

# Cross-product symbols × splits, run in parallel
JOBS=""
for sym in $SYMBOLS; do
    JOBS+="$sym train"$'\n'
    JOBS+="$sym test"$'\n'
done
RAW=$(printf '%s' "$JOBS" \
    | xargs -P "$NCPU" -L 1 bash -c 'run_split "$0" "$1"')

{
echo ""
echo "════════════════════════════════════════════════════════════════════════════════════════"
echo "  P4 OOS PERSISTENCE  signal_tf=${SIGNAL_TF}  atr=${ATR_MULT}  target_rr=${TARGET_RR}"
echo "  Train: ${TRAIN_START_YEAR}-01 → ${TRAIN_END_YEAR}-${TRAIN_END_MONTH}"
echo "  Test:  ${TEST_START_YEAR}-01 → ${TEST_END_YEAR}-${TEST_END_MONTH}"
echo "  Costs: --fee-bps=${FEE_BPS}  --stop-slippage-bps=${SLIP_BPS}  --funding-bps-per-day=${FUNDING_BPS}  --tax-rate-pct=${TAX_RATE}"
echo "════════════════════════════════════════════════════════════════════════════════════════"
echo ""
printf "%-18s %10s %10s %s\n" "Symbol" "Train_NET" "Test_NET" "Verdict"
printf "%-18s %10s %10s %s\n" "──────" "─────────" "────────" "───────"

# Pivot raw output: one line per symbol with train/test
echo "$RAW" | awk '
{
    sym=$1; phase=$2; net=$5
    if (phase=="train") train_net[sym]=net
    else if (phase=="test") test_net[sym]=net
}
END {
    for (sym in train_net) {
        tr=train_net[sym]+0
        te=test_net[sym]+0
        verdict="—"
        if (tr>0 && te>0) verdict="✓ PERSISTENT_WIN"
        else if (tr>0 && te<=0) verdict="✗ TRAIN-WIN_TEST-LOSS  (regime flip)"
        else if (tr<=0 && te>0) verdict="↗ TRAIN-LOSS_TEST-WIN  (recovery)"
        else verdict="✗ persistent_loss"
        printf "%s\t%d\t%d\t%s\n", sym, tr, te, verdict
    }
}' \
    | sort -t$'\t' -k2 -rn \
    | awk -F'\t' '{
        ts=($2>=0)?"+":"";
        es=($3>=0)?"+":"";
        printf "%-18s %s%9d %s%9d  %s\n", $1, ts, $2, es, $3, $4
    }'

echo ""

# Aggregate stats and Spearman correlation
echo "$RAW" | awk '
{
    sym=$1; phase=$2; net=$5
    if (phase=="train") train_net[sym]=net+0
    else if (phase=="test") test_net[sym]=net+0
}
END {
    n=0; persist=0; flip=0; recover=0; pl=0
    train_total=0; test_total=0
    train_pos=0; test_pos=0
    for (sym in train_net) {
        n++
        tr=train_net[sym]; te=test_net[sym]
        train_total+=tr; test_total+=te
        if (tr>0) train_pos++
        if (te>0) test_pos++
        if (tr>0 && te>0) persist++
        else if (tr>0 && te<=0) flip++
        else if (tr<=0 && te>0) recover++
        else pl++
    }
    printf "────────────────────────────────────────────────────────────────────\n"
    printf "Train aggregate: %+d$  (%d/%d profitable)\n", train_total, train_pos, n
    printf "Test  aggregate: %+d$  (%d/%d profitable)\n", test_total, test_pos, n
    printf "\n"
    printf "Persistence breakdown (train, test) signs:\n"
    printf "  ✓ Both positive:        %d/%d  (truly persistent — strategy generalizes)\n", persist, n
    printf "  ✗ Train+, Test-:        %d/%d  (regime-flip casualties)\n", flip, n
    printf "  ↗ Train-, Test+:        %d/%d  (late bloomers — possibly listed mid-period)\n", recover, n
    printf "  ✗ Both negative:        %d/%d  (consistent losers — drop)\n", pl, n
}'

echo ""

# Spearman rank correlation: rank symbols by train PnL, by test PnL, compute correlation
echo "$RAW" | awk '
{
    sym=$1; phase=$2; net=$5
    if (phase=="train") train_net[sym]=net+0
    else if (phase=="test") test_net[sym]=net+0
}
END {
    # Create arrays of (sym, train, test)
    n=0
    for (sym in train_net) { n++; arr_sym[n]=sym; arr_tr[n]=train_net[sym]; arr_te[n]=test_net[sym] }

    # Bubble sort to assign train ranks
    for (i=1;i<=n;i++) tr_idx[i]=i
    for (i=1;i<n;i++) for (j=i+1;j<=n;j++) if (arr_tr[tr_idx[i]] < arr_tr[tr_idx[j]]) { tmp=tr_idx[i]; tr_idx[i]=tr_idx[j]; tr_idx[j]=tmp }
    for (r=1;r<=n;r++) tr_rank[tr_idx[r]]=r

    for (i=1;i<=n;i++) te_idx[i]=i
    for (i=1;i<n;i++) for (j=i+1;j<=n;j++) if (arr_te[te_idx[i]] < arr_te[te_idx[j]]) { tmp=te_idx[i]; te_idx[i]=te_idx[j]; te_idx[j]=tmp }
    for (r=1;r<=n;r++) te_rank[te_idx[r]]=r

    # Spearman: 1 - 6 Σd² / (n(n²-1))
    sum_d2=0
    for (i=1;i<=n;i++) { d=tr_rank[i]-te_rank[i]; sum_d2 += d*d }
    rho = 1 - 6.0*sum_d2/(n*(n*n-1))
    printf "Spearman correlation (train PnL rank vs test PnL rank): ρ = %+.3f\n", rho
    printf "  ρ ≈ +1: per-symbol selection from train would persist into test (ideal)\n"
    printf "  ρ ≈  0: per-symbol selection is noise — strategy IS the edge, not the symbol pick\n"
    printf "  ρ < 0:  per-symbol selection inverts — top train picks become test losers (worst case)\n"
}'

} | tee "$OUT"

echo ""
echo "→ Output: $OUT"
