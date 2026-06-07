#!/usr/bin/env bash
# breadth_regime_gate_grid.sh — same 36-combo grid as regime_gate_grid.sh but
# uses the alt-breadth anchor (BREADTH-1d.csv) instead of the BTC-price anchor.
# Outputs land in tasks/breadth_regime_gate_results/ to keep them separate from
# the BTC-anchor results consumed by walk_forward.py.
#
# Run breadth_anchor_build.sh first to produce data/anchor/BREADTH-1d.csv.
# Research-only. No engine changes. No live/shadow/VPS interaction.
set -euo pipefail
ROOT="$(cd "$(dirname "$0")/.." && pwd)"; cd "$ROOT"

ANCHOR="$ROOT/data/anchor/BREADTH-1d.csv"
if [[ ! -f "$ANCHOR" ]]; then
  echo "ERROR: $ANCHOR missing — run tasks/breadth_anchor_build.sh first" >&2
  exit 1
fi

RESDIR="$ROOT/tasks/breadth_regime_gate_results"; mkdir -p "$RESDIR"
DRIVER="$ROOT/tasks/regime_gate_backtest.sh"

export JOBS="${JOBS:-12}"
export ANCHOR RESDIR

XS=(5 10 15 20); YS=(7 14 30); ZS=(7 14 30)
total=$(( ${#XS[@]} * ${#YS[@]} * ${#ZS[@]} )); i=0
START="$(date +%s)"
for x in "${XS[@]}"; do for y in "${YS[@]}"; do for z in "${ZS[@]}"; do
  i=$((i+1))
  g="$RESDIR/episodes_X${x}_Y${y}_Z${z}.csv"
  b="$RESDIR/episodes_X${x}_Y${y}_Z${z}_baseline.csv"
  echo "=== [$i/$total] X=$x Y=$y Z=$z  ($(date +%H:%M:%S)) ===" >&2
  if [[ -s "$g" ]]; then echo "  gate: cached, skip" >&2
  else bash "$DRIVER" "$x" "$y" "$z"; fi
  if [[ -s "$b" ]]; then echo "  baseline: cached, skip" >&2
  else MODE=baseline bash "$DRIVER" "$x" "$y" "$z"; fi
done; done; done
ELAPSED=$(( $(date +%s) - START ))
echo "→ Breadth grid complete: $total combos × 2 modes in $RESDIR (${ELAPSED}s)" >&2
