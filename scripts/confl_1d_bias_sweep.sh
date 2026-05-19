#!/usr/bin/env bash
# confl_1d_bias_sweep.sh — sweep #2 of 2026-05-19. 1D bias confluence as
# entry filter on top of LIVE 4H 9/21 mh504 short. Pre-reg:
# results/confl_1d_bias_sweep_2026-05-19.md.
#
# Pure research script. No deployment outputs.
#
# Output:
#   - results/confl_1d_bias_sweep_2026-05-19.csv
#   - /tmp/confl_1d_sweep/cell_NN.log per cell
set -euo pipefail

ROOT="$(cd "$(dirname "$0")/.." && pwd)"
WORKDIR="/tmp/confl_1d_sweep"
mkdir -p "$WORKDIR"

CSV="${ROOT}/results/confl_1d_bias_sweep_2026-05-19.csv"
echo "cell,role,confl,confl_fast,confl_slow,W1_net,W2_net,W3_net,mean_net,wins,verdict,trades_total" > "$CSV"

# Grid (matches LOCKED pre-reg)
# Format: "cell|role|confl_mode|fast|slow"
CELLS=(
    "0|BASELINE|0|0|0"
    "1|match-live|1|9|21"
    "2|faster-bias|1|5|15"
    "3|slower-bias|1|21|50"
    "4|fib-bias|1|13|34"
)

# Held constants — match LIVE
export SIGNAL_TF=4H
export EMA_FAST=9
export EMA_SLOW=21
export SIDE_FILTER=short
export TARGET_RR=6.0
export FEE_BPS=10
export SLIP_BPS=5
export MAX_HOLD_HOURS=504

echo "================================================================================"
echo "  1D bias confluence sweep #2 — 5 cells × 3 walk-forward windows × 57 symbols"
echo "  Pre-reg: results/confl_1d_bias_sweep_2026-05-19.md"
echo "  Started: $(date -u '+%Y-%m-%d %H:%M:%S UTC')"
echo "  Output:  $CSV"
echo "================================================================================"

i=0
n_cells=${#CELLS[@]}
for cell_def in "${CELLS[@]}"; do
    i=$((i + 1))
    IFS='|' read -r cell_num role confl fast slow <<< "$cell_def"

    LOG="${WORKDIR}/cell_${cell_num}_${role}.log"
    echo ""
    if [[ "$confl" == "1" ]]; then
        echo "→ [${i}/${n_cells}] cell ${cell_num} (${role}): CONFL_1D=ON  1D EMA ${fast}/${slow}"
    else
        echo "→ [${i}/${n_cells}] cell ${cell_num} (${role}): CONFL_1D=OFF (baseline)"
    fi
    started=$(date +%s)

    if CONFL_1D_MODE="$confl" CONFL_FAST="$fast" CONFL_SLOW="$slow" \
       bash "${ROOT}/scripts/walk_forward.sh" "$LOG" >/dev/null 2>&1; then
        elapsed=$(( $(date +%s) - started ))
        echo "  ✓ completed in ${elapsed}s"
    else
        elapsed=$(( $(date +%s) - started ))
        echo "  ⚠ cell exited non-zero after ${elapsed}s"
    fi

    # Parse aggregate table — same logic as ema_tf sweep driver.
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

    echo "${cell_num},${role},${confl},${fast},${slow},${w1_net},${w2_net},${w3_net},${mean_net},${wins},${verdict},${trades_total}" >> "$CSV"
done

echo ""
echo "================================================================================"
echo "  Sweep complete. Results: $CSV"
echo "  Finished: $(date -u '+%Y-%m-%d %H:%M:%S UTC')"
echo "================================================================================"
