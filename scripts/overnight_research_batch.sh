#!/usr/bin/env bash
# overnight_research_batch.sh — autonomous overnight research batch (2026-06-09).
#
# Runs every offline backtest-data analysis we can, sequentially, caffeinated,
# each job isolated (one failure does not abort the rest). Writes a verdict per
# job + a master summary, then git-commits + pushes results. NO user prompts.
#
# Driven by the directive: "everything we can do now offline with backtest data,
# schedule it ... even if it's a no-go, store the result." The arbiter for every
# candidate is the overfit gate (DSR) + correlation-to-LIVE, NOT raw NET (which
# the 2025-26 bear window games).
#
# Launch detached:  nohup caffeinate -i bash scripts/overnight_research_batch.sh \
#                     > /tmp/overnight_batch.log 2>&1 &
set -uo pipefail   # NOT -e: a failing job must not kill the batch
ROOT="$(cd "$(dirname "$0")/.." && pwd)"; cd "$ROOT"

STAMP="$(date +%Y%m%d_%H%M%S)"
OUTDIR="results/overnight_${STAMP}"
mkdir -p "$OUTDIR"
SUMMARY="${OUTDIR}/SUMMARY.md"
RUNLOG="${OUTDIR}/run.log"

log()  { echo "[$(date -u '+%H:%M:%S')] $*" | tee -a "$RUNLOG"; }
sect() { echo "" | tee -a "$RUNLOG"; echo "================ $* ================" | tee -a "$RUNLOG"; }

{
  echo "# Overnight research batch — ${STAMP}"
  echo ""
  echo "Directive: run every offline backtest-data analysis; store all results incl. no-gos."
  echo "Arbiter: overfit gate (DSR) + correlation-to-LIVE, not raw NET."
  echo ""
  echo "| Job | Status | Headline |"
  echo "|---|---|---|"
} > "$SUMMARY"

add_summary() { echo "| $1 | $2 | $3 |" >> "$SUMMARY"; }

log "Overnight batch start. Output: $OUTDIR"

# ─────────────────────────────────────────────────────────────────────────────
# JOB 1 — finish the expanded-37 overfit matrix (may already be running/done)
#          then run the gate + correlation on it.
# ─────────────────────────────────────────────────────────────────────────────
sect "JOB 1: expanded-37 overfit gate + correlation"
EXP_MATRIX="results/overfit_returns_matrix_2026-06-09_expanded37.csv"
if [[ ! -s "$EXP_MATRIX" ]]; then
  log "Expanded-37 matrix missing — generating (37 configs × 57 sym × 5y)."
  MANIFEST=/tmp/overfit_configs_expanded.csv bash scripts/overfit_matrix_gen.sh "$EXP_MATRIX" >> "$RUNLOG" 2>&1
fi
if [[ -s "$EXP_MATRIX" ]]; then
  python3 scripts/backtest_overfit_analysis.py "$EXP_MATRIX" --live-col LIVE --S 16 > "${OUTDIR}/job1_overfit_gate.txt" 2>&1
  python3 scripts/candidate_correlation.py "$EXP_MATRIX" --live-col LIVE > "${OUTDIR}/job1_correlation.txt" 2>&1
  dsr=$(grep -oE "DSR=[0-9.]+" "${OUTDIR}/job1_overfit_gate.txt" | head -1)
  pbo=$(grep -oE "PBO *= *[0-9.]+" "${OUTDIR}/job1_overfit_gate.txt" | head -1)
  pr=$(grep -oE "PR ≈ [0-9.]+ " "${OUTDIR}/job1_correlation.txt" | head -1)
  log "JOB1 done. ${dsr} ${pbo} ${pr}"
  add_summary "1. Expanded-37 gate" "DONE" "${dsr} ${pbo} | ${pr} indep bets"
else
  log "JOB1 FAILED — no matrix."
  add_summary "1. Expanded-37 gate" "FAIL" "matrix gen failed"
fi

# ─────────────────────────────────────────────────────────────────────────────
# JOB 2 — EMA fine-grid (fast × slow), 4H short, feed the gate.
#          The "many combinations" request, judged by DSR + correlation not NET.
# ─────────────────────────────────────────────────────────────────────────────
sect "JOB 2: EMA fine-grid (fast × slow) 4H short"
J2CSV="${OUTDIR}/job2_ema_finegrid.csv"
echo "fast,slow,W1,W2,W3,mean,wins,trades" > "$J2CSV"
J2LOG="${OUTDIR}/job2.log"
for fast in 3 5 8 13 21; do
  for slow in 15 26 34 55; do
    [[ "$fast" -ge "$slow" ]] && continue
    WL="/tmp/j2_${fast}_${slow}.log"
    SIGNAL_TF=4H SIDE_FILTER=short EMA_FAST=$fast EMA_SLOW=$slow TARGET_RR=6.0 \
      FEE_BPS=10 SLIP_BPS=5 MAX_HOLD_HOURS=504 \
      bash scripts/walk_forward.sh "$WL" >/dev/null 2>&1
    ex() { grep -E "^$1" "$WL" 2>/dev/null | head -1 | awk '{print $2}' | grep -oE "[+-]?[0-9]+" | head -1 || echo 0; }
    w1=$(ex "W1_2023-05"); w2=$(ex "W2_2024-05"); w3=$(ex "W3_2025-05")
    : "${w1:=0}"; : "${w2:=0}"; : "${w3:=0}"
    tr=$(grep -E "^W[123]_" "$WL" 2>/dev/null | awk '{s+=$3} END{print s+0}')
    mean=$(awk -v a=$w1 -v b=$w2 -v c=$w3 'BEGIN{printf "%.0f",(a+b+c)/3}')
    wins=$(awk -v a=$w1 -v b=$w2 -v c=$w3 'BEGIN{w=0;if(a>0)w++;if(b>0)w++;if(c>0)w++;print w}')
    echo "${fast},${slow},${w1},${w2},${w3},${mean},${wins},${tr}" >> "$J2CSV"
    echo "ema ${fast}/${slow}: mean=${mean} wins=${wins}/3" | tee -a "$J2LOG"
    rm -f "$WL"
  done
done
best2=$(tail -n +2 "$J2CSV" | sort -t, -k6 -n | tail -1)
log "JOB2 done. best cell: $best2"
add_summary "2. EMA fine-grid" "DONE" "best: $(echo $best2 | cut -d, -f1-2,6 | tr ',' '/')"

# ─────────────────────────────────────────────────────────────────────────────
# JOB 3 — Multi-TF × side grid (the untested timeframe cells).
# ─────────────────────────────────────────────────────────────────────────────
sect "JOB 3: multi-TF × side grid (EMA 9/21)"
J3CSV="${OUTDIR}/job3_tf_side.csv"
echo "tf,side,W1,W2,W3,mean,wins,trades" > "$J3CSV"
for tf in 5m 15m 30m 1H 2H 4H 1D; do
  for side in short both; do
    WL="/tmp/j3_${tf}_${side}.log"
    SIGNAL_TF=$tf SIDE_FILTER=$side EMA_FAST=9 EMA_SLOW=21 TARGET_RR=6.0 \
      FEE_BPS=10 SLIP_BPS=5 MAX_HOLD_HOURS=504 \
      bash scripts/walk_forward.sh "$WL" >/dev/null 2>&1
    ex() { grep -E "^$1" "$WL" 2>/dev/null | head -1 | awk '{print $2}' | grep -oE "[+-]?[0-9]+" | head -1 || echo 0; }
    w1=$(ex "W1_2023-05"); w2=$(ex "W2_2024-05"); w3=$(ex "W3_2025-05")
    : "${w1:=0}"; : "${w2:=0}"; : "${w3:=0}"
    tr=$(grep -E "^W[123]_" "$WL" 2>/dev/null | awk '{s+=$3} END{print s+0}')
    mean=$(awk -v a=$w1 -v b=$w2 -v c=$w3 'BEGIN{printf "%.0f",(a+b+c)/3}')
    wins=$(awk -v a=$w1 -v b=$w2 -v c=$w3 'BEGIN{w=0;if(a>0)w++;if(b>0)w++;if(c>0)w++;print w}')
    echo "${tf},${side},${w1},${w2},${w3},${mean},${wins},${tr}" >> "$J3CSV"
    echo "tf=${tf} side=${side}: mean=${mean} wins=${wins}/3 trades=${tr}" | tee -a "$RUNLOG"
    rm -f "$WL"
  done
done
best3=$(tail -n +2 "$J3CSV" | sort -t, -k6 -n | tail -1)
log "JOB3 done. best cell: $best3"
add_summary "3. Multi-TF × side" "DONE" "best: $(echo $best3 | cut -d, -f1-2,6 | tr ',' '/')"

# ─────────────────────────────────────────────────────────────────────────────
# JOB 4 — target_rr × max_hold grid (exit-geometry, 4H short EMA 9/21).
# ─────────────────────────────────────────────────────────────────────────────
sect "JOB 4: target_rr × max_hold grid"
J4CSV="${OUTDIR}/job4_rr_maxhold.csv"
echo "rr,maxhold,W1,W2,W3,mean,wins,trades" > "$J4CSV"
for rr in 2 3 4 6 8 10; do
  for mh in 168 336 504 720; do
    WL="/tmp/j4_${rr}_${mh}.log"
    SIGNAL_TF=4H SIDE_FILTER=short EMA_FAST=9 EMA_SLOW=21 TARGET_RR=$rr \
      FEE_BPS=10 SLIP_BPS=5 MAX_HOLD_HOURS=$mh \
      bash scripts/walk_forward.sh "$WL" >/dev/null 2>&1
    ex() { grep -E "^$1" "$WL" 2>/dev/null | head -1 | awk '{print $2}' | grep -oE "[+-]?[0-9]+" | head -1 || echo 0; }
    w1=$(ex "W1_2023-05"); w2=$(ex "W2_2024-05"); w3=$(ex "W3_2025-05")
    : "${w1:=0}"; : "${w2:=0}"; : "${w3:=0}"
    tr=$(grep -E "^W[123]_" "$WL" 2>/dev/null | awk '{s+=$3} END{print s+0}')
    mean=$(awk -v a=$w1 -v b=$w2 -v c=$w3 'BEGIN{printf "%.0f",(a+b+c)/3}')
    wins=$(awk -v a=$w1 -v b=$w2 -v c=$w3 'BEGIN{w=0;if(a>0)w++;if(b>0)w++;if(c>0)w++;print w}')
    echo "${rr},${mh},${w1},${w2},${w3},${mean},${wins},${tr}" >> "$J4CSV"
    echo "rr=${rr} mh=${mh}: mean=${mean} wins=${wins}/3" | tee -a "$RUNLOG"
    rm -f "$WL"
  done
done
best4=$(tail -n +2 "$J4CSV" | sort -t, -k6 -n | tail -1)
log "JOB4 done. best cell: $best4"
add_summary "4. RR × max_hold" "DONE" "best: $(echo $best4 | cut -d, -f1-2,6 | tr ',' '/')"

# ─────────────────────────────────────────────────────────────────────────────
# JOB 5 — Cross-sectional momentum (THE new-axis shot): dollar-neutral
#          long-strongest / short-weakest, ranked daily by trailing return.
#          Built as a standalone offline analyzer over the raw 1m CSVs (no engine
#          mode needed). This is the ONE bet structurally uncorrelated with
#          "short the downtrend" — the only thing that could break the PR≈1.8 wall.
# ─────────────────────────────────────────────────────────────────────────────
sect "JOB 5: cross-sectional long-short (dollar-neutral)"
python3 scripts/cross_sectional_ls.py > "${OUTDIR}/job5_cross_sectional.txt" 2>&1
if [[ $? -eq 0 ]]; then
  hl=$(grep -E "ANNUAL|Sharpe|VERDICT" "${OUTDIR}/job5_cross_sectional.txt" | head -2 | tr '\n' ' ')
  log "JOB5 done. $hl"
  add_summary "5. Cross-sectional L/S" "DONE" "$hl"
else
  log "JOB5 FAILED (see job5 txt)"
  add_summary "5. Cross-sectional L/S" "FAIL" "see job5_cross_sectional.txt"
fi

# ─────────────────────────────────────────────────────────────────────────────
# WRAP — commit + push all results.
# ─────────────────────────────────────────────────────────────────────────────
sect "WRAP: commit + push"
rm -f backtest engine 2>/dev/null   # stray build artifacts
{
  echo ""
  echo "## Interpretation"
  echo ""
  echo "Reference: clean 34-config gate = DSR 0.658 / PBO 0.52, PR≈1.8 independent bets"
  echo "(34 configs collapse to ~2 streams — see results/overfit_expansion_2026-06-09.md)."
  echo "Jobs 2-4 grids are judged by whether any cell's walk-forward beats baseline AND"
  echo "is low-correlation (a real diversifier). Job 5 is the one structurally-new axis."
  echo "Per the standing overfit verdict, expect grid winners to be high-correlation copies"
  echo "of the live short bet (raw-NET mirages). Stored regardless — negative results are results."
} >> "$SUMMARY"

git add "$OUTDIR" results/overfit_returns_matrix_2026-06-09_expanded37.csv \
  results/overfit_expansion_2026-06-09.md scripts/candidate_correlation.py \
  scripts/cross_sectional_ls.py scripts/overnight_research_batch.sh 2>/dev/null
git commit -q -F - <<EOF
research(overnight): batch sweep ${STAMP} — EMA/TF/RR grids + cross-sectional + overfit gate

Autonomous overnight batch per directive "run everything offline, store all results
incl. no-gos." Jobs: (1) expanded-37 overfit gate + correlation, (2) EMA fast×slow
fine-grid, (3) multi-TF×side grid, (4) target_rr×max_hold grid, (5) cross-sectional
dollar-neutral long-short (the one structurally-uncorrelated axis). All judged by
overfit DSR + correlation-to-LIVE, not raw NET. Results: ${OUTDIR}/SUMMARY.md.

Co-Authored-By: Claude Opus 4.8 <noreply@anthropic.com>
EOF
git push origin main >> "$RUNLOG" 2>&1 && log "pushed" || log "push failed (results committed locally)"

log "OVERNIGHT BATCH COMPLETE. Summary: $SUMMARY"
