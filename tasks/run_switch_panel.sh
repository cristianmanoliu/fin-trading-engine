#!/usr/bin/env bash
# run_switch_panel.sh — 9-cohort regime-SWITCH robustness panel.
# For each cohort: continuous always-short baseline (once) + (X,Y) switch grid +
# walk_forward_xy verdict. Caffeinate-wrap when running unattended.
#
# env: JOBS (default 8), DWELL (default 3)
set -euo pipefail
ROOT="$(cd "$(dirname "$0")/.." && pwd)"; cd "$ROOT"
export JOBS="${JOBS:-8}" DWELL="${DWELL:-3}"
export ANCHOR="$ROOT/data/anchor/BTCUSDT-1d.csv"

declare -A FLAGS
FLAGS["live"]=""
FLAGS["alt5-15-336"]="--ema-fast-period 5 --ema-slow-period 15 --max-hold-hours 336"
FLAGS["alt5-15-504"]="--ema-fast-period 5 --ema-slow-period 15 --max-hold-hours 504"
FLAGS["alt5-21-504"]="--ema-fast-period 5 --ema-slow-period 21 --max-hold-hours 504"
FLAGS["alt7-14-504"]="--ema-fast-period 7 --ema-slow-period 14 --max-hold-hours 504"
FLAGS["alt10-30-504"]="--ema-fast-period 10 --ema-slow-period 30 --max-hold-hours 504"
FLAGS["alt12-26-504"]="--ema-fast-period 12 --ema-slow-period 26 --max-hold-hours 504"
FLAGS["alt21-50-504"]="--ema-fast-period 21 --ema-slow-period 50 --max-hold-hours 504"
FLAGS["bb20"]="--bollinger-mode --bollinger-period 20 --bollinger-std-mult 2.0 --max-hold-hours 504"

XS=(5 10 15 20); YS=(7 14 30)
COHORTS=(live alt5-15-336 alt5-15-504 alt5-21-504 alt7-14-504 alt10-30-504 alt12-26-504 alt21-50-504 bb20)

log(){ echo "[$(date +%H:%M:%S)] $*"; }

# Helper: check if CSV has >1 line (header + ≥1 data row).
# A killed job leaves header-only files that -s wrongly caches as done.
rows_ok(){ [[ -f "$1" ]] && [[ "$(wc -l < "$1" 2>/dev/null)" -gt 1 ]]; }

for c in "${COHORTS[@]}"; do
  RESDIR="$ROOT/tasks/regime_switch_results/$c"; mkdir -p "$RESDIR"
  export RESDIR EXTRA_BASE_FLAGS="${FLAGS[$c]}"
  log "=== Cohort $c ==="

  # 1) continuous baseline (once) → _baseline_body.csv. The body is HEADERLESS
  # (raw sym,year,short,pnl rows) and regime_switch_baseline.sh self-removes it +
  # exits 2 on zero rows — so plain `-s` (non-empty) is the right guard here, NOT
  # rows_ok (a headerless body's first line is real data, not a header to skip).
  if [[ ! -s "$RESDIR/_baseline_body.csv" ]]; then
    bash tasks/regime_switch_baseline.sh
  fi

  # 2) switch grid + per-cell baseline materialization.
  # NOTE: a switch CSV has a header written BEFORE jobs run, so a kill mid-run
  # (mac sleep, OOM) leaves a header-only file. `-s` (size>0) would wrongly cache
  # it as done — the exact mac-sleep bug that bit the prior gate sweep. Require
  # >1 line (header + ≥1 data row) before skipping; otherwise rebuild the cell.
  for x in "${XS[@]}"; do for y in "${YS[@]}"; do
    sw="$RESDIR/episodes_X${x}_Y${y}.csv"
    bl="$RESDIR/episodes_X${x}_Y${y}_baseline.csv"
    rows_ok "$sw" || bash tasks/regime_switch_backtest.sh "$x" "$y"
    # baseline cell = shared continuous body (X,Y-independent), with header.
    if ! rows_ok "$bl"; then
      { echo "symbol,year,label,net_pnl,trades,wins"; cat "$RESDIR/_baseline_body.csv"; } > "$bl"
    fi
  done; done

  # 3) verdict. If walk_forward_xy fails (e.g. its empty-resdir guard), warn and
  # continue to the next cohort rather than aborting the whole panel under set -e.
  if python3 tasks/walk_forward_xy.py --resdir "$RESDIR" > "$RESDIR/summary.txt" 2>&1; then
    cat "$RESDIR/summary.txt"
    log "$c verdict: $(grep IN_F "$RESDIR/summary.txt" 2>/dev/null || echo '(no IN_F line)')"
  else
    cat "$RESDIR/summary.txt" >&2 || true
    log "WARN: $c verdict FAILED (walk_forward_xy non-zero) — continuing"
  fi
done
log "PANEL COMPLETE."
