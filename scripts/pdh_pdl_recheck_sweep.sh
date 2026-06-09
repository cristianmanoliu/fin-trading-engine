#!/usr/bin/env bash
# pdh_pdl_recheck_sweep.sh — phantom-fix re-check of PDH/PDL break (the last
# contaminated short-only non-EMA mode). Pre-reg:
# results/pdh_pdl_phantom_recheck_2026-06-09.md. 2 cells × 3 walk-forward windows.
# Mirrors scripts/alt_signals_retest_sweep.sh.
set -euo pipefail
ROOT="$(cd "$(dirname "$0")/.." && pwd)"
WORKDIR="/tmp/pdh_pdl_recheck"; mkdir -p "$WORKDIR"
CSV="${ROOT}/results/pdh_pdl_phantom_recheck_2026-06-09.csv"
echo "cell,role,signal_mode,W1_net,W2_net,W3_net,mean_net,wins,verdict,trades_total" > "$CSV"

export SIGNAL_TF=4H SIDE_FILTER=short TARGET_RR=6.0 FEE_BPS=10 SLIP_BPS=5 MAX_HOLD_HOURS=504

CELLS=( "0|BASELINE|ema-9-21" "1|PDH-PDL|pdh-pdl-break" )

echo "=== PDH/PDL phantom re-check — 2 cells × 3 windows × 57 syms (fixed binary) ==="
i=0; n=${#CELLS[@]}
for cell_def in "${CELLS[@]}"; do
    i=$((i+1)); IFS='|' read -r cell_num role signal_label <<< "$cell_def"
    LOG="${WORKDIR}/cell_${cell_num}_${role}.log"
    echo "→ [${i}/${n}] cell ${cell_num} (${role}): ${signal_label}"
    started=$(date +%s)
    # reset mode flags; baseline=all-zero (EMA default), PDH cell sets PDH_PDL_MODE=1
    extra_env=( "EMA_FAST=0" "EMA_SLOW=0" "RSI_MODE=0" "MACD_MODE=0" "PDH_PDL_MODE=0" )
    case "$cell_num" in
        0) : ;;
        1) extra_env=( "EMA_FAST=0" "EMA_SLOW=0" "RSI_MODE=0" "MACD_MODE=0" "PDH_PDL_MODE=1" ) ;;
    esac
    if env "${extra_env[@]}" bash "${ROOT}/scripts/walk_forward.sh" "$LOG" >/dev/null 2>&1; then
        echo "  ✓ $(( $(date +%s) - started ))s"
    else
        echo "  ⚠ non-zero after $(( $(date +%s) - started ))s"
    fi
    extract_net() { grep -E "^${1}" "$LOG" 2>/dev/null | head -1 | awk '{print $2}' | grep -oE "[+-]?[0-9]+" | head -1 || echo 0; }
    w1=$(extract_net "W1_2023-05"); w2=$(extract_net "W2_2024-05"); w3=$(extract_net "W3_2025-05")
    : "${w1:=0}"; : "${w2:=0}"; : "${w3:=0}"
    trades=$(grep -E "^W[123]_" "$LOG" 2>/dev/null | awk '{s+=$3} END{print s+0}'); : "${trades:=0}"
    mean=$(awk -v a="$w1" -v b="$w2" -v c="$w3" 'BEGIN{printf "%.0f",(a+b+c)/3}')
    wins=$(awk -v a="$w1" -v b="$w2" -v c="$w3" 'BEGIN{w=0;if(a>0)w++;if(b>0)w++;if(c>0)w++;print w}')
    if [[ "$wins" -ge 2 && "$mean" -gt 0 ]]; then verdict=POSITIVE; else verdict=NEGATIVE; fi
    echo "  → W1=${w1} W2=${w2} W3=${w3} mean=${mean} wins=${wins}/3 verdict=${verdict}"
    echo "${cell_num},${role},${signal_label},${w1},${w2},${w3},${mean},${wins},${verdict},${trades}" >> "$CSV"
done
echo "=== done → $CSV ==="
cat "$CSV"
