#!/usr/bin/env bash
# purgatory_recheck_sweep.sh — backtest the Reddit "Purgatory Method" ENTRY signal
# (5/9 EMA cross gated by price on same side of BOTH VWAP & EMA30) on our crypto
# perps. Pre-reg: results/purgatory_recheck_2026-06-09.md.
#
# 4 cells × 3 walk-forward windows. The strategy is a 4min-equities-0DTE scalp; we
# test ONLY whether its entry signal has directional edge on perps (5m TF, our
# fixed-RR/wick-stop exit — the 2-candle scalp exit is NOT modeled). Both-sides
# AND short-only, at 5m and (for cost contrast) 4H.
set -euo pipefail
ROOT="$(cd "$(dirname "$0")/.." && pwd)"
WORKDIR="/tmp/purgatory_recheck"; mkdir -p "$WORKDIR"
CSV="${ROOT}/results/purgatory_recheck_2026-06-09.csv"
echo "cell,role,tf,side,W1_net,W2_net,W3_net,mean_net,wins,verdict,trades_total" > "$CSV"

export TARGET_RR=6.0 FEE_BPS=10 SLIP_BPS=5 MAX_HOLD_HOURS=504

# cell|role|signal_tf|side_filter|purgatory(1/0)|ema_fast|ema_slow
CELLS=(
    "0|BASELINE-4H-short|4H|short|0|9|21"
    "1|PURG-5m-both|5m|both|1|5|9"
    "2|PURG-5m-short|5m|short|1|5|9"
    "3|PURG-4H-short|4H|short|1|5|9"
)

echo "=== Purgatory entry-signal recheck — 4 cells × 3 windows × 57 syms ==="
i=0; n=${#CELLS[@]}
for cell_def in "${CELLS[@]}"; do
    i=$((i+1)); IFS='|' read -r cell_num role tf side purg ef es <<< "$cell_def"
    LOG="${WORKDIR}/cell_${cell_num}.log"
    echo "→ [${i}/${n}] cell ${cell_num} (${role}): tf=${tf} side=${side} purg=${purg} ema=${ef}/${es}"
    started=$(date +%s)
    env SIGNAL_TF="$tf" SIDE_FILTER="$side" PURGATORY_MODE="$purg" EMA_FAST="$ef" EMA_SLOW="$es" \
        RSI_MODE=0 MACD_MODE=0 PDH_PDL_MODE=0 MOMENTUM_MODE=0 \
        bash "${ROOT}/scripts/walk_forward.sh" "$LOG" >/dev/null 2>&1 \
        && echo "  ✓ $(( $(date +%s) - started ))s" \
        || echo "  ⚠ non-zero after $(( $(date +%s) - started ))s"

    extract_net() { grep -E "^${1}" "$LOG" 2>/dev/null | head -1 | awk '{print $2}' | grep -oE "[+-]?[0-9]+" | head -1 || echo 0; }
    w1=$(extract_net "W1_2023-05"); w2=$(extract_net "W2_2024-05"); w3=$(extract_net "W3_2025-05")
    : "${w1:=0}"; : "${w2:=0}"; : "${w3:=0}"
    trades=$(grep -E "^W[123]_" "$LOG" 2>/dev/null | awk '{s+=$3} END{print s+0}'); : "${trades:=0}"
    mean=$(awk -v a="$w1" -v b="$w2" -v c="$w3" 'BEGIN{printf "%.0f",(a+b+c)/3}')
    wins=$(awk -v a="$w1" -v b="$w2" -v c="$w3" 'BEGIN{w=0;if(a>0)w++;if(b>0)w++;if(c>0)w++;print w}')
    if [[ "$wins" -ge 2 && "$mean" -gt 0 ]]; then verdict=POSITIVE; else verdict=NEGATIVE; fi
    echo "  → W1=${w1} W2=${w2} W3=${w3} mean=${mean} wins=${wins}/3 trades=${trades} verdict=${verdict}"
    echo "${cell_num},${role},${tf},${side},${w1},${w2},${w3},${mean},${wins},${verdict},${trades}" >> "$CSV"
done
echo "=== done → $CSV ==="
cat "$CSV"
