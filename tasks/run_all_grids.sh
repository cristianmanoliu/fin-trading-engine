#!/usr/bin/env bash
# run_all_grids.sh — wait for breadth grid to finish, run walk-forward verdict,
# then run all 8 shadow grids sequentially + verdict after each.
# Run this in a terminal you can leave overnight.
set -euo pipefail
ROOT="$(cd "$(dirname "$0")/.." && pwd)"; cd "$ROOT"

BREADTH_LOG="/tmp/breadth_grid.log"
BREADTH_RESDIR="$ROOT/tasks/breadth_regime_gate_results"
SHADOW_RESDIR="$ROOT/tasks/shadow_regime_gate_results"

SHADOWS=(alt5-15-336 alt5-15-504 bb20 alt5-21-504 alt7-14-504 alt10-30-504 alt12-26-504 alt21-50-504)

log() { echo "[$(date +%H:%M:%S)] $*"; }

# ── 1. Wait for breadth grid ──────────────────────────────────────────────────
log "Waiting for breadth grid to finish..."
until grep -q "Breadth grid complete" "$BREADTH_LOG" 2>/dev/null; do
  sleep 60
done
log "Breadth grid done."

# ── 2. Breadth verdict ────────────────────────────────────────────────────────
log "Running breadth walk-forward verdict..."
python3 tasks/walk_forward.py --resdir "$BREADTH_RESDIR" \
  | tee "$BREADTH_RESDIR/summary.txt"

# ── 3. Shadow grids + verdicts ────────────────────────────────────────────────
for shadow in "${SHADOWS[@]}"; do
  log "=== Shadow: $shadow ==="
  bash tasks/shadow_regime_gate_grid.sh "$shadow" 2>&1 \
    | tee "/tmp/shadow_${shadow}.log"
  log "Shadow $shadow grid done. Running verdict..."
  python3 tasks/walk_forward.py --resdir "$SHADOW_RESDIR/$shadow" \
    | tee "$SHADOW_RESDIR/$shadow/summary.txt"
  log "Shadow $shadow verdict written."
done

log "ALL DONE. Results:"
log "  Breadth: $BREADTH_RESDIR/summary.txt"
for shadow in "${SHADOWS[@]}"; do
  log "  $shadow: $SHADOW_RESDIR/$shadow/summary.txt"
done
