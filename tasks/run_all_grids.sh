#!/usr/bin/env bash
# run_all_grids.sh — run all 8 shadow grids (BTC anchor, short-gate-only)
# sequentially + walk-forward verdict after each.
# Run this in a terminal you can leave overnight.
set -euo pipefail
ROOT="$(cd "$(dirname "$0")/.." && pwd)"; cd "$ROOT"

SHADOW_RESDIR="$ROOT/tasks/shadow_regime_gate_results"
SHADOWS=(alt5-15-336 alt5-15-504 bb20 alt5-21-504 alt7-14-504 alt10-30-504 alt12-26-504 alt21-50-504)

export JOBS="${JOBS:-4}"
export ANCHOR="$ROOT/data/anchor/BTCUSDT-1d.csv"
export SHORT_ONLY=1

log() { echo "[$(date +%H:%M:%S)] $*"; }

log "Starting shadow grids — BTC anchor, short-gate-only, JOBS=$JOBS"
log "Anchor: $ANCHOR"

for shadow in "${SHADOWS[@]}"; do
  log "=== Shadow: $shadow ==="
  bash tasks/shadow_regime_gate_grid.sh "$shadow" 2>&1 \
    | tee "/tmp/shadow_${shadow}.log"
  log "Shadow $shadow grid done. Running verdict..."
  python3 tasks/walk_forward.py --resdir "$SHADOW_RESDIR/$shadow" \
    | tee "$SHADOW_RESDIR/$shadow/summary.txt"
  log "Shadow $shadow verdict written."
done

log "ALL DONE."
for shadow in "${SHADOWS[@]}"; do
  verdict=$(grep "VERDICT" "$SHADOW_RESDIR/$shadow/summary.txt" 2>/dev/null || echo "missing")
  log "  $shadow: $verdict"
done
