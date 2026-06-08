#!/usr/bin/env bash
# shadow_regime_gate_grid.sh — run the regime-gate grid for a named shadow
# cohort. Uses the breadth anchor by default (override with ANCHOR env var).
# Outputs land in tasks/shadow_regime_gate_results/<shadow_name>/ so each
# cohort has its own result directory, consumable by walk_forward.py.
#
# Usage:
#   bash tasks/shadow_regime_gate_grid.sh <shadow_name>
#
# Shadow names (from deploy/systemd/paper-live@.service):
#   alt5-15-336   alt5-15-504   bb20   alt5-21-504   alt7-14-504
#   alt10-30-504  alt12-26-504  alt21-50-504
#
# Environment overrides:
#   ANCHOR=<path>   anchor CSV (default: data/anchor/BREADTH-1d.csv)
#   JOBS=N          parallelism (default: 12)
#
# Research-only. No engine changes. No live/shadow/VPS interaction.
set -euo pipefail
ROOT="$(cd "$(dirname "$0")/.." && pwd)"; cd "$ROOT"

SHADOW="${1:?Usage: $0 <shadow_name>}"
ANCHOR="${ANCHOR:-$ROOT/data/anchor/BTCUSDT-1d.csv}"

if [[ ! -f "$ANCHOR" ]]; then
  echo "ERROR: $ANCHOR missing — run tasks/breadth_anchor_build.sh first" >&2
  exit 1
fi

# Shadow parameter table.
# Format: EMA shadows → extra flags for --ema-fast-period / --ema-slow-period / --max-hold-hours
# BB shadow → --bollinger-mode --bollinger-period 20 --bollinger-std-mult 2.0
declare -A SHADOW_FLAGS
SHADOW_FLAGS["alt5-15-336"]="--ema-fast-period 5 --ema-slow-period 15 --max-hold-hours 336"
SHADOW_FLAGS["alt5-15-504"]="--ema-fast-period 5 --ema-slow-period 15 --max-hold-hours 504"
SHADOW_FLAGS["bb20"]="--bollinger-mode --bollinger-period 20 --bollinger-std-mult 2.0 --max-hold-hours 504"
SHADOW_FLAGS["alt5-21-504"]="--ema-fast-period 5 --ema-slow-period 21 --max-hold-hours 504"
SHADOW_FLAGS["alt7-14-504"]="--ema-fast-period 7 --ema-slow-period 14 --max-hold-hours 504"
SHADOW_FLAGS["alt10-30-504"]="--ema-fast-period 10 --ema-slow-period 30 --max-hold-hours 504"
SHADOW_FLAGS["alt12-26-504"]="--ema-fast-period 12 --ema-slow-period 26 --max-hold-hours 504"
SHADOW_FLAGS["alt21-50-504"]="--ema-fast-period 21 --ema-slow-period 50 --max-hold-hours 504"

if [[ -z "${SHADOW_FLAGS[$SHADOW]+x}" ]]; then
  echo "ERROR: unknown shadow '$SHADOW'" >&2
  echo "Known: ${!SHADOW_FLAGS[*]}" >&2
  exit 1
fi

EXTRA_FLAGS="${SHADOW_FLAGS[$SHADOW]}"
RESDIR="$ROOT/tasks/shadow_regime_gate_results/$SHADOW"; mkdir -p "$RESDIR"
DRIVER="$ROOT/tasks/regime_gate_backtest.sh"

export JOBS="${JOBS:-12}"
export SHORT_ONLY="${SHORT_ONLY:-1}"
export ANCHOR RESDIR

# Inject shadow-specific extra flags into the backtest invocations.
# regime_gate_backtest.sh uses BASE_FLAGS; we append to it.
# BASE_FLAGS is set inside regime_gate_backtest.sh main body, so we override
# via EXTRA_BASE_FLAGS which the driver will append if set.
export EXTRA_BASE_FLAGS="$EXTRA_FLAGS"

XS=(5 10 15 20); YS=(7 14 30); ZS=(7 14 30)
total=$(( ${#XS[@]} * ${#YS[@]} * ${#ZS[@]} )); i=0
echo "→ Shadow: $SHADOW | extra flags: $EXTRA_FLAGS" >&2
echo "→ Anchor: $ANCHOR" >&2
echo "→ Output: $RESDIR" >&2
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
echo "→ Shadow '$SHADOW' grid complete: $total combos × 2 modes in $RESDIR (${ELAPSED}s)" >&2
