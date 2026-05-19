#!/usr/bin/env bash
# side_filter_validation_sweep.sh — sweep #4 of 2026-05-19. Side-filter
# validation: shorts (LIVE) vs longs-only vs both-sides. LIVE config.
# Pre-reg: results/side_filter_validation_2026-05-19.md.
#
# Pure research script. No deployment outputs.
set -euo pipefail

ROOT="$(cd "$(dirname "$0")/.." && pwd)"
WORKDIR="/tmp/side_filter_sweep"
mkdir -p "$WORKDIR"

CSV="${ROOT}/results/side_filter_validation_2026-05-19.csv"
echo "cell,role,side_filter,W1_net,W2_net,W3_net,mean_net,wins,verdict,trades_total" > "$CSV"

# Grid (matches LOCKED pre-reg)
CELLS=(
    "0|BASELINE|short"
    "1|longs-mirror|long"
    "2|both-sides|both"
)

# Held constants — match LIVE production (everything EXCEPT side filter)
export SIGNAL_TF=4H
export EMA_FAST=9
export EMA_SLOW=21
export TARGET_RR=6.0
export FEE_BPS=10
export SLIP_BPS=5
export MAX_HOLD_HOURS=504

echo "================================================================================"
echo "  Side-filter validation sweep #4 — 3 cells × 3 walk-forward windows × 57 symbols"
echo "  Pre-reg: results/side_filter_validation_2026-05-19.md"
echo "  Started: $(date -u '+%Y-%m-%d %H:%M:%S UTC')"
echo "  Output:  $CSV"
echo "================================================================================"

i=0
n_cells=${#CELLS[@]}
for cell_def in "${CELLS[@]}"; do
    i=$((i + 1))
    IFS='|' read -r cell_num role side <<< "$cell_def"

    LOG="${WORKDIR}/cell_${cell_num}_${role}.log"
    echo ""
    echo "→ [${i}/${n_cells}] cell ${cell_num} (${role}): SIDE_FILTER=${side}"
    started=$(date +%s)

    if SIDE_FILTER="$side" \
       bash "${ROOT}/scripts/walk_forward.sh" "$LOG" >/dev/null 2>&1; then
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

    echo "${cell_num},${role},${side},${w1_net},${w2_net},${w3_net},${mean_net},${wins},${verdict},${trades_total}" >> "$CSV"
done

echo ""
echo "================================================================================"
echo "  Sweep complete. Results: $CSV"
echo "  Finished: $(date -u '+%Y-%m-%d %H:%M:%S UTC')"
echo "================================================================================"
