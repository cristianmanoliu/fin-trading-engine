#!/usr/bin/env bash
# regime_gate_grid.sh — run the episode-slicing driver across the full (X,Y,Z)
# grid, in both gate and baseline modes. Produces all per-episode CSVs that
# walk_forward.py consumes. Idempotent: skips a combo whose output already
# exists (so a crashed/interrupted run resumes cleanly).
#
# Grid (spec §6): X∈{5,10,15,20} Y∈{7,14,30} Z∈{7,14,30} = 36 combos.
# Each combo runs gate + baseline = 72 driver invocations. At ~90s/invocation
# (parallel, JOBS=12) that's ~1.5-2h wall-clock. Override parallelism with JOBS.
#
# Research-only. No engine changes. No live/shadow/VPS interaction.
set -euo pipefail
ROOT="$(cd "$(dirname "$0")/.." && pwd)"; cd "$ROOT"
RESDIR="${RESDIR:-$ROOT/tasks/regime_gate_results}"; mkdir -p "$RESDIR"
DRIVER="$ROOT/tasks/regime_gate_backtest.sh"

export JOBS="${JOBS:-12}"

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
echo "→ Grid complete: $total combos × 2 modes in $RESDIR (${ELAPSED}s)" >&2
