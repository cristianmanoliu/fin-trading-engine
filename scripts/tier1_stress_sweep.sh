#!/usr/bin/env bash
# tier1_stress_sweep.sh — TIER 1 cell stress test at fee=15bp + slip=15bp.
# Per post_shadow_evaluation_research_decision_rule_2026-05-27.md TIER 1
# requires stress validation beyond fee=10+slip=5 (cached in
# results/{trailing_stop,mltp_exit}_sweep_2026-05-19.csv).
#
# Cells:
#   - trail-interval-2R       (trail_mode=1, trail_interval_r=2.0)
#   - trail-interval-3R       (trail_mode=1, trail_interval_r=3.0)
#   - mltp mid-4R-half        (mltp_mode=1, mid_r=4.0, mid_frac=0.5)
#   - BASELINE (no exit mod)  (control)
#
# Pre-cache before potential 2026-06-08 Gate 9 reject → TIER 1 activation.

set -euo pipefail

ROOT="$(cd "$(dirname "$0")/.." && pwd)"
WORKDIR="/tmp/tier1_stress_sweep"
mkdir -p "$WORKDIR"

CSV="${ROOT}/results/tier1_stress_sweep_$(date -u +%Y-%m-%d).csv"
echo "cell,role,exit_mode,param_r,param_frac,W1_net,W2_net,W3_net,mean_net,wins,verdict,trades_total" > "$CSV"

# STRESS cost model
export FEE_BPS=15
export SLIP_BPS=15

# LIVE config constants
export SIGNAL_TF=4H
export EMA_FAST=9
export EMA_SLOW=21
export SIDE_FILTER=short
export TARGET_RR=6.0
export MAX_HOLD_HOURS=504

# Format: "cell|role|exit_mode|param_r|param_frac"
# exit_mode: 0=baseline, 1=trail, 2=mltp
CELLS=(
    "0|BASELINE|0|0|0"
    "1|trail-interval-2R|1|2.0|0"
    "2|trail-interval-3R|1|3.0|0"
    "3|mltp-mid-4R-half|2|4.0|0.5"
)

echo "================================================================================"
echo "  TIER 1 stress sweep — fee=${FEE_BPS}bp slip=${SLIP_BPS}bp"
echo "  Cells: ${#CELLS[@]} × 3 walk-forward windows × 57 symbols"
echo "  Started: $(date -u '+%Y-%m-%d %H:%M:%S UTC')"
echo "  Output:  $CSV"
echo "================================================================================"

i=0
n_cells=${#CELLS[@]}
for cell_def in "${CELLS[@]}"; do
    i=$((i + 1))
    IFS='|' read -r cell_num role exit_mode param_r param_frac <<< "$cell_def"

    LOG="${WORKDIR}/cell_${cell_num}_${role}.log"
    echo ""
    echo "→ [${i}/${n_cells}] cell ${cell_num} (${role}): MODE=${exit_mode} R=${param_r} FRAC=${param_frac}"
    started=$(date +%s)

    case "$exit_mode" in
        0)
            TRAIL_MODE=0 MLTP_MODE=0 \
                bash "${ROOT}/scripts/walk_forward.sh" "$LOG" >/dev/null 2>&1
            ;;
        1)
            TRAIL_MODE=1 TRAIL_INTERVAL_R="$param_r" MLTP_MODE=0 \
                bash "${ROOT}/scripts/walk_forward.sh" "$LOG" >/dev/null 2>&1
            ;;
        2)
            TRAIL_MODE=0 MLTP_MODE=1 MID_R="$param_r" MID_FRAC="$param_frac" \
                bash "${ROOT}/scripts/walk_forward.sh" "$LOG" >/dev/null 2>&1
            ;;
    esac
    rc=$?
    elapsed=$(( $(date +%s) - started ))
    if [[ $rc -eq 0 ]]; then
        echo "  ✓ completed in ${elapsed}s"
    else
        echo "  ⚠ exited rc=$rc after ${elapsed}s"
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
    echo "${cell_num},${role},${exit_mode},${param_r},${param_frac},${w1_net},${w2_net},${w3_net},${mean_net},${wins},${verdict},${trades_total}" >> "$CSV"
done

echo ""
echo "================================================================================"
echo "  Stress sweep complete. Results: $CSV"
echo "  Finished: $(date -u '+%Y-%m-%d %H:%M:%S UTC')"
echo "================================================================================"
