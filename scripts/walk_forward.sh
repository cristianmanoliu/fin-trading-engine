#!/usr/bin/env bash
# walk_forward.sh — run a strategy configuration across multiple non-overlapping
# OOS windows. Validation framework: a configuration is "robust" only if it
# produces consistent results across multiple windows, not just one.
#
# Default windows: three non-overlapping 12-month chunks of the available data,
# chosen to span 2023-05 → 2026-04 (3y of holdout).
#   W1: 2023-05 → 2024-04
#   W2: 2024-05 → 2025-04
#   W3: 2025-05 → 2026-04 (the original "fresh OOS" window)
#
# For each window, runs scripts/p4_fresh_oos.sh with the given config and
# captures the per-window aggregate. Outputs a table summarising:
#   - per-window NET
#   - mean / median / sign agreement across windows
#   - validation verdict (positive in ≥2/3 windows + mean > 0 = "robust")
#
# Configurable via env (defaults match deployed P4-Combined):
#   SIGNAL_TF (default 4H), SIDE_FILTER (short), TARGET_RR (6.0), SLIP_BPS (15)
#   FEE_BPS (10), MAX_HOLD_HOURS (336)
#   SYMBOLS (default: universe from configs/symbols.yaml)
#
# Usage:
#   ./scripts/walk_forward.sh                                   # default config
#   ./scripts/walk_forward.sh results/walk_forward_my_test.txt  # custom output
#   SIGNAL_TF=1D ./scripts/walk_forward.sh                      # different TF
set -euo pipefail

ROOT="$(cd "$(dirname "$0")/.." && pwd)"

SIGNAL_TF="${SIGNAL_TF:-4H}"
SIDE_FILTER="${SIDE_FILTER:-short}"
TARGET_RR="${TARGET_RR:-6.0}"
SLIP_BPS="${SLIP_BPS:-15}"
FEE_BPS="${FEE_BPS:-10}"
MAX_HOLD_HOURS="${MAX_HOLD_HOURS:-336}"
FUNDING_FILTER_BPS="${FUNDING_FILTER_BPS:-0}"
EMA_FAST="${EMA_FAST:-0}"  # 0 → backtest defaults to 9
EMA_SLOW="${EMA_SLOW:-0}"  # 0 → backtest defaults to 21
VWAP_DEV_PCT="${VWAP_DEV_PCT:-0}"  # 0 → VWAP fade disabled; e.g. 0.02 = 2% deviation
MOMENTUM_MODE="${MOMENTUM_MODE:-0}" # 1 → enable 5m-momentum-candle strategy
PURGATORY_MODE="${PURGATORY_MODE:-0}" # 1 → enable Purgatory (5/9 cross + VWAP/EMA30 gate)
RSI_MODE="${RSI_MODE:-0}"          # 1 → enable RSI cross-50 strategy
RSI_PERIOD="${RSI_PERIOD:-0}"      # 0 → defaults to 14
PDH_PDL_MODE="${PDH_PDL_MODE:-0}"  # 1 → enable PDH/PDL break strategy
MACD_MODE="${MACD_MODE:-0}"        # 1 → enable MACD line/signal cross
MACD_FAST="${MACD_FAST:-0}"        # 0 → defaults to 12
MACD_SLOW="${MACD_SLOW:-0}"        # 0 → defaults to 26
MACD_SIGNAL="${MACD_SIGNAL:-0}"    # 0 → defaults to 9
BB_MODE="${BB_MODE:-0}"            # 1 → enable Bollinger band breakdown
BB_PERIOD="${BB_PERIOD:-0}"        # 0 → defaults to 20
BB_STD="${BB_STD:-0}"              # 0 → defaults to 2.0
TRAIL_MODE="${TRAIL_MODE:-0}"            # Cat B1: 1 → enable trailing stop
TRAIL_INTERVAL_R="${TRAIL_INTERVAL_R:-0}" # Cat B1: trail step in R; 0 → defaults to 1.0
MLTP_MODE="${MLTP_MODE:-0}"               # Cat B2: 1 → enable multi-level TP
MID_R="${MID_R:-0}"                       # Cat B2: mid-R multiple; 0 → defaults to 3.0
MID_FRAC="${MID_FRAC:-0}"                 # Cat B2: partial fraction; 0 → defaults to 0.5
CONFL_1D_MODE="${CONFL_1D_MODE:-0}"       # Cat D1: 1 → enable 1D bias confluence filter
CONFL_FAST="${CONFL_FAST:-0}"             # Cat D1: 1D EMA fast period; 0 → 9
CONFL_SLOW="${CONFL_SLOW:-0}"             # Cat D1: 1D EMA slow period; 0 → 21
VOL_FILTER_MODE="${VOL_FILTER_MODE:-0}"   # Cat E1: 1 → enable vol-regime filter
MAX_VOL="${MAX_VOL:-0}"                   # Cat E1: max annualized vol fraction; 0 → 1.20

# Label embeds the filter level only when active so unfiltered runs keep their
# existing filename (no churn for the deployed config baseline).
LABEL="${SIGNAL_TF}_${SIDE_FILTER}_rr${TARGET_RR}_slip${SLIP_BPS}"
if [[ "$FUNDING_FILTER_BPS" != "0" ]]; then
    LABEL="${LABEL}_filt${FUNDING_FILTER_BPS}"
fi
if [[ "$EMA_FAST" != "0" ]] || [[ "$EMA_SLOW" != "0" ]]; then
    LABEL="${LABEL}_ema${EMA_FAST}-${EMA_SLOW}"
fi
if [[ "$VWAP_DEV_PCT" != "0" ]]; then
    LABEL="${LABEL}_vwap${VWAP_DEV_PCT}"
fi
if [[ "$RSI_MODE" == "1" ]]; then
    LABEL="${LABEL}_rsi${RSI_PERIOD}"
fi
if [[ "$PDH_PDL_MODE" == "1" ]]; then
    LABEL="${LABEL}_pdhpdl"
fi
if [[ "$MACD_MODE" == "1" ]]; then
    LABEL="${LABEL}_macd${MACD_FAST}-${MACD_SLOW}-${MACD_SIGNAL}"
fi
if [[ "$BB_MODE" == "1" ]]; then
    LABEL="${LABEL}_bb${BB_PERIOD}-${BB_STD}"
fi
if [[ "$TRAIL_MODE" == "1" ]]; then
    LABEL="${LABEL}_trail${TRAIL_INTERVAL_R}"
fi
if [[ "$MLTP_MODE" == "1" ]]; then
    LABEL="${LABEL}_mltp${MID_R}-${MID_FRAC}"
fi
if [[ "$CONFL_1D_MODE" == "1" ]]; then
    LABEL="${LABEL}_confl1d${CONFL_FAST}-${CONFL_SLOW}"
fi
if [[ "$VOL_FILTER_MODE" == "1" ]]; then
    LABEL="${LABEL}_volfilt${MAX_VOL}"
fi
OUT="${1:-results/walk_forward_${LABEL}_$(date +%F).txt}"
WORKDIR=$(mktemp -d /tmp/wf-XXXXXXXX)
trap 'rm -rf "$WORKDIR"' EXIT

source "$(dirname "${BASH_SOURCE[0]}")/lib/symbols.sh"
SYMBOLS="${SYMBOLS:-$(get_symbols universe)}"
N_SYM=$(echo "$SYMBOLS" | wc -w | tr -d ' ')

# Walk-forward windows: non-overlapping 12-month chunks.
# Each entry: "label start_year start_month end_year end_month"
# Default = 3 most recent windows. Set N_WINDOWS=6 to extend back to 2020-05.
N_WINDOWS="${N_WINDOWS:-3}"
if [[ "$N_WINDOWS" == "6" ]]; then
    WINDOWS=(
        "W-2_2020-05_to_2021-04 2020 5 2021 4"
        "W-1_2021-05_to_2022-04 2021 5 2022 4"
        "W0_2022-05_to_2023-04  2022 5 2023 4"
        "W1_2023-05_to_2024-04  2023 5 2024 4"
        "W2_2024-05_to_2025-04  2024 5 2025 4"
        "W3_2025-05_to_2026-04  2025 5 2026 4"
    )
else
    WINDOWS=(
        "W1_2023-05_to_2024-04 2023 5 2024 4"
        "W2_2024-05_to_2025-04 2024 5 2025 4"
        "W3_2025-05_to_2026-04 2025 5 2026 4"
    )
fi

echo "→ Walk-forward sweep"
echo "  Config: signal_tf=${SIGNAL_TF}  side=${SIDE_FILTER}  target_rr=${TARGET_RR}  slip=${SLIP_BPS}bp  fee=${FEE_BPS}bp  max_hold=${MAX_HOLD_HOURS}h"
echo "  Universe: ${N_SYM} symbols"
echo "  Windows: ${#WINDOWS[@]}"
echo ""

# Run each window, capture per-window aggregate
declare -a window_labels
declare -a window_nets
declare -a window_trades
declare -a window_wrs
declare -a window_pos_sym

for win in "${WINDOWS[@]}"; do
    read -r label sy sm ey em <<< "$win"
    echo "→ Window: $label"
    win_out="${WORKDIR}/${label}.txt"
    SYMBOLS="$SYMBOLS" \
    SIGNAL_TF="$SIGNAL_TF" SIDE_FILTER="$SIDE_FILTER" TARGET_RR="$TARGET_RR" \
    SLIP_BPS="$SLIP_BPS" FEE_BPS="$FEE_BPS" MAX_HOLD_HOURS="$MAX_HOLD_HOURS" \
    FUNDING_FILTER_BPS="$FUNDING_FILTER_BPS" \
    EMA_FAST="$EMA_FAST" EMA_SLOW="$EMA_SLOW" \
    VWAP_DEV_PCT="$VWAP_DEV_PCT" MOMENTUM_MODE="$MOMENTUM_MODE" PURGATORY_MODE="$PURGATORY_MODE" \
    RSI_MODE="$RSI_MODE" RSI_PERIOD="$RSI_PERIOD" PDH_PDL_MODE="$PDH_PDL_MODE" \
    MACD_MODE="$MACD_MODE" MACD_FAST="$MACD_FAST" MACD_SLOW="$MACD_SLOW" MACD_SIGNAL="$MACD_SIGNAL" \
    BB_MODE="$BB_MODE" BB_PERIOD="$BB_PERIOD" BB_STD="$BB_STD" \
    TRAIL_MODE="$TRAIL_MODE" TRAIL_INTERVAL_R="$TRAIL_INTERVAL_R" \
    MLTP_MODE="$MLTP_MODE" MID_R="$MID_R" MID_FRAC="$MID_FRAC" \
    CONFL_1D_MODE="$CONFL_1D_MODE" CONFL_FAST="$CONFL_FAST" CONFL_SLOW="$CONFL_SLOW" \
    VOL_FILTER_MODE="$VOL_FILTER_MODE" MAX_VOL="$MAX_VOL" \
    START_YEAR="$sy" START_MONTH="$sm" END_YEAR="$ey" END_MONTH="$em" \
    "${ROOT}/scripts/p4_fresh_oos.sh" "$win_out" > /dev/null 2>&1

    # Extract aggregate from the TOTAL line
    # Format: "TOTAL  56 sym (with trades)  1983 trades  18.91% WR"
    #         "  Gross +201056  Fees 124114  Funding 0  →  NET -81668  | 24/56 sym profitable"
    # NB: bypass set -e on grep failures with `|| true`; preserve empty default.
    total_line=$(grep "^TOTAL" "$win_out" | head -1 || true)
    net_line=$(grep -E "NET [+-]" "$win_out" | head -1 || true)

    trades=$(echo "$total_line" | grep -oE "[0-9]+ trades" | head -1 | grep -oE "[0-9]+" || echo 0)
    wr=$(echo "$total_line" | grep -oE "[0-9.]+% WR" | head -1 | grep -oE "[0-9.]+" || echo 0)
    net=$(echo "$net_line" | grep -oE "NET [+-][0-9]+" | head -1 | grep -oE "[+-][0-9]+" || echo 0)
    pos=$(echo "$net_line" | grep -oE "[0-9]+/[0-9]+ sym profitable" | head -1 || echo "?/? sym profitable")
    : "${trades:=0}"; : "${wr:=0}"; : "${net:=0}"

    window_labels+=("$label")
    window_nets+=("${net:-0}")
    window_trades+=("${trades:-0}")
    window_wrs+=("${wr:-0}")
    window_pos_sym+=("${pos:-?/? sym profitable}")

    printf "  → trades=%s  WR=%s%%  NET=%s  profitable=%s\n" "$trades" "$wr" "$net" "$pos"
done

# Aggregate analysis
{
echo ""
echo "════════════════════════════════════════════════════════════════════════════════════════"
echo "  WALK-FORWARD VALIDATION  ${LABEL}"
echo "  Config: signal_tf=${SIGNAL_TF}  side=${SIDE_FILTER}  target_rr=${TARGET_RR}  slip=${SLIP_BPS}bp"
echo "  Costs: --fee-bps=${FEE_BPS}  --stop-slippage-bps=${SLIP_BPS}  funding=CSV  max_hold=${MAX_HOLD_HOURS}h"
echo "  Universe: ${N_SYM} symbols"
echo "════════════════════════════════════════════════════════════════════════════════════════"
echo ""

printf "%-26s %10s %8s %8s  %s\n" "Window" "NET" "Trades" "WR%" "Profitable"
printf "%-26s %10s %8s %8s  %s\n" "------" "---" "------" "---" "----------"

n_pos=0
sum_net=0
for i in "${!window_labels[@]}"; do
    label="${window_labels[$i]}"
    net="${window_nets[$i]}"
    trades="${window_trades[$i]}"
    wr="${window_wrs[$i]}"
    pos="${window_pos_sym[$i]}"
    printf "%-26s %+10d %8d %7s%%  %s\n" "$label" "$net" "$trades" "$wr" "$pos"
    if [[ "$net" -gt 0 ]]; then
        n_pos=$((n_pos + 1))
    fi
    sum_net=$((sum_net + net))
done

n_total=${#window_labels[@]}
mean_net=$((sum_net / n_total))

echo ""
echo "  Aggregate"
echo "  ────────────────────────────────────────────────────────────────────────────"
printf "    Sum NET across %d windows: %+d\n" "$n_total" "$sum_net"
printf "    Mean NET per window:       %+d\n" "$mean_net"
printf "    Positive windows:          %d/%d\n" "$n_pos" "$n_total"
echo ""

echo "  Verdict"
echo "  ────────────────────────────────────────────────────────────────────────────"
if [[ "$n_pos" -eq "$n_total" ]] && [[ "$mean_net" -gt 0 ]]; then
    echo "    STRONG — positive in ALL windows AND mean > 0"
elif [[ "$n_pos" -gt $((n_total / 2)) ]] && [[ "$mean_net" -gt 0 ]]; then
    echo "    SUPPORTIVE — positive in majority of windows AND mean > 0"
    echo "    (one losing window is consistent with normal regime variance, NOT a free pass)"
elif [[ "$n_pos" -eq 0 ]]; then
    echo "    REJECTED — no window positive"
elif [[ "$mean_net" -lt 0 ]]; then
    echo "    REJECTED — mean NET < 0 across windows"
else
    echo "    REJECTED — minority of windows positive"
fi
echo ""
echo "  Caveat: each window is ONE regime sample. With n=${n_total} windows, a"
echo "  STRONG/SUPPORTIVE verdict is HYPOTHESIS-STRENGTH evidence — not proof of"
echo "  forward edge. Multiple-comparison adjustment applies when many configs are"
echo "  tested. The framework is designed to REJECT, not to validate."

} | tee "$OUT"

echo ""
echo "→ Output: $OUT"
