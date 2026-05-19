#!/usr/bin/env bash
# alt_signals_retest_sweep.sh — sweep #7 of 2026-05-19. Tests MACD-mode +
# RSI-mode vs LIVE EMA baseline. Re-tests Cat A 2026-05-06/07 REJECTED
# verdicts at current cost model (slip=5).
#
# Pre-reg: results/alt_signals_retest_2026-05-19.md.
set -euo pipefail

ROOT="$(cd "$(dirname "$0")/.." && pwd)"
WORKDIR="/tmp/alt_signals_sweep"
mkdir -p "$WORKDIR"

CSV="${ROOT}/results/alt_signals_retest_2026-05-19.csv"
echo "cell,role,signal_mode,W1_net,W2_net,W3_net,mean_net,wins,verdict,trades_total" > "$CSV"

# Grid (matches LOCKED pre-reg)
# Format: "cell|role|signal_label"
# Signal mode is wired via per-cell env exports below since each signal
# uses different harness env vars.
CELLS=(
    "0|BASELINE|ema-9-21"
    "1|MACD-mode|macd-12-26-9"
    "2|RSI-mode|rsi-14"
)

# Held constants — match LIVE
export SIGNAL_TF=4H
export SIDE_FILTER=short
export TARGET_RR=6.0
export FEE_BPS=10
export SLIP_BPS=5
export MAX_HOLD_HOURS=504

echo "================================================================================"
echo "  Alt-signals re-test sweep #7 — 3 cells × 3 walk-forward windows × 57 symbols"
echo "  Pre-reg: results/alt_signals_retest_2026-05-19.md"
echo "  Started: $(date -u '+%Y-%m-%d %H:%M:%S UTC')"
echo "  Output:  $CSV"
echo "================================================================================"

i=0
n_cells=${#CELLS[@]}
for cell_def in "${CELLS[@]}"; do
    i=$((i + 1))
    IFS='|' read -r cell_num role signal_label <<< "$cell_def"

    LOG="${WORKDIR}/cell_${cell_num}_${role}.log"
    echo ""
    echo "→ [${i}/${n_cells}] cell ${cell_num} (${role}): signal=${signal_label}"
    started=$(date +%s)

    # Per-cell signal mode wiring. Reset all mode flags to avoid bleed-through.
    extra_env=(
        "EMA_FAST=0" "EMA_SLOW=0"
        "MACD_MODE=0" "MACD_FAST=0" "MACD_SLOW=0" "MACD_SIGNAL=0"
        "RSI_MODE=0" "RSI_PERIOD=0"
    )
    case "$cell_num" in
        0) : ;;  # all zero → harness defaults to EMA 9/21
        1) extra_env=( "EMA_FAST=0" "EMA_SLOW=0" "MACD_MODE=1" "MACD_FAST=12" "MACD_SLOW=26" "MACD_SIGNAL=9" "RSI_MODE=0" "RSI_PERIOD=0" ) ;;
        2) extra_env=( "EMA_FAST=0" "EMA_SLOW=0" "MACD_MODE=0" "MACD_FAST=0" "MACD_SLOW=0" "MACD_SIGNAL=0" "RSI_MODE=1" "RSI_PERIOD=14" ) ;;
    esac

    if env "${extra_env[@]}" bash "${ROOT}/scripts/walk_forward.sh" "$LOG" >/dev/null 2>&1; then
        elapsed=$(( $(date +%s) - started ))
        echo "  ✓ completed in ${elapsed}s"
    else
        elapsed=$(( $(date +%s) - started ))
        echo "  ⚠ cell exited non-zero after ${elapsed}s"
    fi

    extract_net() {
        local window_prefix="$1"
        grep -E "^${window_prefix}" "$LOG" 2>/dev/null \
            | head -1 \
            | awk '{print $2}' \
            | grep -oE "[+-]?[0-9]+" \
            | head -1 \
            || echo "0"
    }
    w1_net=$(extract_net "W1_2023-05")
    w2_net=$(extract_net "W2_2024-05")
    w3_net=$(extract_net "W3_2025-05")
    : "${w1_net:=0}"; : "${w2_net:=0}"; : "${w3_net:=0}"

    trades_total=$(grep -E "^W[123]_" "$LOG" 2>/dev/null \
        | awk '{s+=$3} END {print s+0}')
    : "${trades_total:=0}"

    mean_net=$(awk -v a="$w1_net" -v b="$w2_net" -v c="$w3_net" 'BEGIN{printf "%.0f", (a+b+c)/3}')
    wins=$(awk -v a="$w1_net" -v b="$w2_net" -v c="$w3_net" 'BEGIN{w=0; if(a>0)w++; if(b>0)w++; if(c>0)w++; print w}')

    if [[ "$wins" -ge 2 ]] && [[ "$mean_net" -gt 0 ]]; then
        verdict="POSITIVE"
    else
        verdict="NEGATIVE"
    fi

    echo "  → W1=${w1_net}  W2=${w2_net}  W3=${w3_net}  mean=${mean_net}  wins=${wins}/3  verdict=${verdict}"

    echo "${cell_num},${role},${signal_label},${w1_net},${w2_net},${w3_net},${mean_net},${wins},${verdict},${trades_total}" >> "$CSV"
done

echo ""
echo "================================================================================"
echo "  Sweep complete. Results: $CSV"
echo "  Finished: $(date -u '+%Y-%m-%d %H:%M:%S UTC')"
echo "================================================================================"
