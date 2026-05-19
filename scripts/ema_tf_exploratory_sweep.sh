#!/usr/bin/env bash
# ema_tf_exploratory_sweep.sh — run the 11-cell EMA × timeframe research grid
# pre-registered at results/ema_tf_exploratory_grid_2026-05-19.md.
#
# Pure research script. No deployment outputs. Produces:
#   - results/ema_tf_exploratory_grid_2026-05-19.csv  (one row per cell)
#   - per-cell walk-forward outputs at /tmp/ema_tf_sweep/cell_NN.log
#
# Each cell invokes scripts/walk_forward.sh with one (TF, EMA_FAST, EMA_SLOW)
# triple. Other params held at LIVE production values (mh504, slip5, fee10,
# rr6, short-only) for direct baseline comparability.
#
# Usage:
#   bash scripts/ema_tf_exploratory_sweep.sh
set -euo pipefail

ROOT="$(cd "$(dirname "$0")/.." && pwd)"
WORKDIR="/tmp/ema_tf_sweep"
mkdir -p "$WORKDIR"

CSV="${ROOT}/results/ema_tf_exploratory_grid_2026-05-19.csv"
echo "cell,role,tf,ema_fast,ema_slow,W1_net,W2_net,W3_net,mean_net,wins,verdict,trades_total" > "$CSV"

# Grid (matches the LOCKED pre-reg)
# Format: "cell_num|role|tf|ema_fast|ema_slow"
CELLS=(
    "0|BASELINE|4H|9|21"
    "1|4H-interp|4H|7|14"
    "2|4H-slower|4H|10|30"
    "3|4H-MACD|4H|12|26"
    "4|4H-ratio-1to4.2|4H|5|21"
    "5|4H-ratio-1to4.25|4H|8|34"
    "6|4H-slowest|4H|21|50"
    "7|1D-alt5|1D|5|15"
    "8|1D-live|1D|9|21"
    "9|1D-slow|1D|10|30"
    "10|1D-ratio-1to4|1D|7|28"
)

# Constants — match LIVE production
export SIDE_FILTER=short
export TARGET_RR=6.0
export FEE_BPS=10
export SLIP_BPS=5            # LIVE uses 5 (not the harness default 15)
export MAX_HOLD_HOURS=504    # LIVE uses 504 (not the harness default 336)

echo "================================================================================"
echo "  EMA × TF exploratory sweep — 11 cells × 3 walk-forward windows × 57 symbols"
echo "  Pre-reg: results/ema_tf_exploratory_grid_2026-05-19.md"
echo "  Started: $(date -u '+%Y-%m-%d %H:%M:%S UTC')"
echo "  Output:  $CSV"
echo "================================================================================"

n_cells=${#CELLS[@]}
i=0
for cell_def in "${CELLS[@]}"; do
    i=$((i + 1))
    IFS='|' read -r cell_num role tf ema_fast ema_slow <<< "$cell_def"

    LOG="${WORKDIR}/cell_${cell_num}_${role}.log"
    echo ""
    echo "→ [${i}/${n_cells}] cell ${cell_num} (${role}): TF=${tf} EMA ${ema_fast}/${ema_slow}"
    started=$(date +%s)

    # Run the walk-forward harness with this cell's params.
    if SIGNAL_TF="$tf" \
       EMA_FAST="$ema_fast" EMA_SLOW="$ema_slow" \
       bash "${ROOT}/scripts/walk_forward.sh" "$LOG" >/dev/null 2>&1; then
        elapsed=$(( $(date +%s) - started ))
        echo "  ✓ completed in ${elapsed}s"
    else
        elapsed=$(( $(date +%s) - started ))
        echo "  ⚠ cell exited non-zero after ${elapsed}s — partial output may exist"
    fi

    # Extract per-window NET from the LOG file (written via `tee` inside the
    # aggregate block — see walk_forward.sh:242).
    # Aggregate table format (printf "%-26s %+10d %8d %7s%%  %s\n"):
    #   "W1_2023-05_to_2024-04           +201056     1983  18.91%  24/56 sym profitable"
    # We extract the NET (2nd column, signed integer) per window line.
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

    # Trade count: sum across windows from the same aggregate table.
    trades_total=$(grep -E "^W[123]_" "$LOG" 2>/dev/null \
        | awk '{s+=$3} END {print s+0}')
    : "${trades_total:=0}"

    # Mean + wins.
    mean_net=$(awk -v a="$w1_net" -v b="$w2_net" -v c="$w3_net" 'BEGIN{printf "%.0f", (a+b+c)/3}')
    wins=$(awk -v a="$w1_net" -v b="$w2_net" -v c="$w3_net" 'BEGIN{w=0; if(a>0)w++; if(b>0)w++; if(c>0)w++; print w}')

    # Verdict matches walk_forward.sh semantics: positive in ≥2/3 AND mean > 0.
    if [[ "$wins" -ge 2 ]] && [[ "$mean_net" -gt 0 ]]; then
        verdict="POSITIVE"
    else
        verdict="NEGATIVE"
    fi

    echo "  → W1=${w1_net}  W2=${w2_net}  W3=${w3_net}  mean=${mean_net}  wins=${wins}/3  verdict=${verdict}"

    echo "${cell_num},${role},${tf},${ema_fast},${ema_slow},${w1_net},${w2_net},${w3_net},${mean_net},${wins},${verdict},${trades_total}" >> "$CSV"
done

echo ""
echo "================================================================================"
echo "  Sweep complete. Results: $CSV"
echo "  Finished: $(date -u '+%Y-%m-%d %H:%M:%S UTC')"
echo "================================================================================"
