#!/usr/bin/env bash
# overnight_arc_2026-07-27.sh — 8h research arc.
#
# DESIGN PRINCIPLE: test MECHANISMS with pre-committed hypotheses, not a
# parameter grid. Every cell below has a stated reason to work BEFORE it runs,
# and each is evaluated on a held-out period. Parameter grids are what the DSR
# budget punishes; mechanism tests with a train/test split are what survived
# last night (ATR fee cut, 57/57 symbols).
#
# Held-out split is enforced by START_YEAR/END_YEAR, same as cost_geometry_sweep.
#   TRAIN 2020-01..2023-12   TEST 2024-01..2025-04
#
# Arms (each has a mechanism, not just a number):
#   A. atr-refine   — bracket last night's atr1.5 winner (1.0/1.25/1.75/2.0).
#                     Mechanism: fee ~ 1/stop_width. Locates the cost/edge optimum.
#   B. atr x rr     — wider stop changes R geometry, so the 6:1 target may no
#                     longer be optimal. target_rr is YAML-only -> per-arm cfg.
#   C. atr + regime — vol filter ON TOP of atr1.5. Mechanism: ATR sizing already
#                     adapts to vol, so a vol filter may now be redundant OR
#                     complementary. Cheap, decisive either way.
#   D. atr + mh     — max-hold interacts with wider stops (slower time-stops).
#
# Everything writes per-symbol rows: SYMBOL trades wins net gross fees
set -euo pipefail
ROOT="$(cd "$(dirname "$0")/.." && pwd)"
cd "$ROOT"

BIN=/tmp/oa-bin
go build -o "$BIN" ./cmd/backtest

OUT_TRAIN=results/oa_train
OUT_TEST=results/oa_test
mkdir -p "$OUT_TRAIN" "$OUT_TEST"
LOG=/tmp/oa/run.log
mkdir -p /tmp/oa
: > "$LOG"

run_arm() {  # name extra span_start span_end span_endmonth outdir
    local name="$1" extra="$2" ys="$3" ye="$4" em="$5" od="$6"
    if [ -s "${od}/${name}.txt" ]; then
        echo "skip ${od}/${name}" >> "$LOG"
        return 0
    fi
    SHARED_BINARY="$BIN" VARIANT="$name" EXTRA="$extra" \
      START_YEAR="$ys" END_YEAR="$ye" END_YEAR_MONTH="$em" \
      bash scripts/cost_geometry_sweep.sh "${od}/${name}.txt" >> "$LOG" 2>&1
}

# ---- TRAIN sweep -------------------------------------------------------
declare -a ARMS=(
  "atr1.0|--atr-stop-mult 1.0"
  "atr1.25|--atr-stop-mult 1.25"
  "atr1.75|--atr-stop-mult 1.75"
  "atr2.0|--atr-stop-mult 2.0"
  "atr1.5-vol|--atr-stop-mult 1.5 --vol-filter-mode"
  "atr1.5-mh336|--atr-stop-mult 1.5 --max-hold-hours 336"
  "atr1.5-mh720|--atr-stop-mult 1.5 --max-hold-hours 720"
  "atr1.5-trail|--atr-stop-mult 1.5 --trailing-stop-mode"
  "atr1.5-atr21|--atr-stop-mult 1.5 --atr-period 21"
  "atr1.5-atr7|--atr-stop-mult 1.5 --atr-period 7"
)
echo "=== TRAIN arms: ${#ARMS[@]} ===" >> "$LOG"
for a in "${ARMS[@]}"; do
    # never let one failing arm abort the whole unattended run
    run_arm "${a%%|*}" "${a#*|}" 2020 2023 12 "$OUT_TRAIN" || \
        echo "ARM FAILED: ${a%%|*}" >> "$LOG"
done
echo "TRAIN_DONE" >> "$LOG"

# NOTE: a target_rr arm was considered and DROPPED. target_rr is YAML-only, so
# running it would require mutating tracked configs/default.yaml in place while
# unattended -- an unacceptable risk of leaving the repo dirty or, worse, of a
# crash leaving a modified live-referenced config behind. If that arm is ever
# wanted, add a --target-rr CLI flag first.

# ---- TEST: only arms that BEAT baseline on train are promoted ----------
# Selection happens in the analysis step; TEST runs are launched separately
# after a human (or the morning analysis) fixes the shortlist. This keeps the
# holdout genuinely held out.
echo "ALL_DONE" >> "$LOG"
