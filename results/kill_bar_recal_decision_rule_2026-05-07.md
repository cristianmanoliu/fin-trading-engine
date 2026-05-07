# Kill-Bar Recalibration — PRE-REGISTERED Decision Rule

**Status:** Locked before any recalibration runs. Same discipline as the
original calibration pre-registration, F1, F2, sample-split bootstrap.

## Context

The original kill-bar calibration verdict
(`results/kill_bar_calibration_verdict_2026-05-07.md`) found
NEEDS_RECALIBRATION: 4-of-5 testable criteria fail their FP/TP bands at the
90-day horizon. This pre-registration locks the recalibration procedure
BEFORE any new thresholds are computed or tested.

## Approach: principled-quantile thresholds

For each criterion C with a continuous statistic s(C) (e.g., total PnL,
max-sym-pct, HODL delta), set the kill threshold at the quantile of the
**null distribution** of s(C) at 90d that gives a target FP rate.

This is mechanical: the threshold comes from the null distribution we
already measured, NOT from looking at what threshold "would have worked"
on the data. There is no degree of freedom for tuning.

## Locked decision parameters

| Parameter | Value | Rationale |
|---|---|---|
| Target FP rate per criterion (null) | **10%** | Half the original 20% acceptance band — comfortable margin |
| Calibration horizon | **90 days** | Same as original calibration pre-registration |
| Null distribution source | The 1,000 paths from the original Monte Carlo | Same data the original verdict measured |
| Verification horizons | 60d, 90d, 120d, 180d | Same as original |
| Verification scenarios | null, deg30, deg50, dead | Same as original |

## The threshold-setting rule (mechanical)

For each criterion, the recalibrated threshold is:

| Criterion | Statistic | Quantile rule | Direction |
|---|---|---|---|
| KILL_PNL    | total NET at horizon | 10th percentile of null distribution | trigger if statistic < threshold |
| KILL_WR     | WR % when n_trades ≥ 150 | 10th percentile of null distribution | trigger if statistic < threshold |
| KILL_SYM    | max-sym pct of \|total\| | 90th percentile of null distribution | trigger if statistic > threshold |
| KILL_HODL_C | HODL delta cumulative | 10th percentile of null distribution | trigger if statistic < threshold |
| KILL_HODL_W | per-window delta minimum | 10th percentile of *single-window* null delta distribution | trigger if 2 consecutive < threshold |

For KILL_HODL_W: the 2-consecutive-windows construction means single-window
FP=10% gives joint FP ≈ 1% under independence; with the lag-1 ρ=+0.32
autocorrelation we measured in A1, joint FP is closer to 2-3%. This is
expected; we accept it because the verification step measures the actual
joint FP and applies the locked acceptance bands.

## Acceptance bands (locked — same as original)

A recalibrated criterion is ACCEPTED if all three bands hold at the
verification step:

| Quantity | Band |
|---|---|
| FP_rate at 90d (null) | ≤ 20% |
| TP_rate at 90d (deg50) | ≥ 50% |
| TP_rate at 90d (dead)  | ≥ 80% |

Any criterion failing any band is **discarded** from the kill bar entirely.
Better to have a smaller kill bar with reliable criteria than a noisy one.

## Verdict tiers

| Tier | Condition |
|---|---|
| **CLEAN_RECAL**      | All 5 criteria pass → full kill bar recalibrated |
| **PARTIAL_RECAL**    | 3-4 criteria pass; rest discarded → reduced kill bar |
| **MOSTLY_DISCARDED** | 1-2 criteria pass → minimal kill bar |
| **NO_KILL_BAR**      | 0 criteria pass → no automated kill, manual only |

## What is NOT permitted

- ❌ No alternative quantile targets ("try 5%, try 15%, see what works")
- ❌ No alternative principled rules ("use mean ± 2σ instead of quantile")
- ❌ No re-running with different B
- ❌ No reframing the acceptance bands
- ❌ No "saving" criteria that fail bands by relaxing one band
- ❌ No combining discarded criteria into a composite ("ANY 2 of these 3 fire")

A criterion that fails after recalibration is finalized as DISCARDED, full
stop. Composing previously-failed criteria is a separate hypothesis
requiring its own pre-registration.

## Pre-registered priors

| Outcome | Prior |
|---|---:|
| CLEAN_RECAL      | 25% |
| PARTIAL_RECAL    | 50% |
| MOSTLY_DISCARDED | 20% |
| NO_KILL_BAR      | 5%  |

Reasoning: KILL_PNL and KILL_HODL_C are likely to recalibrate cleanly via
quantile setting (continuous statistics with smooth distributions).
KILL_SYM and KILL_WR are at higher risk — KILL_SYM because the fleet
naturally has high concentration, KILL_WR because the signal-to-noise at
n=150 is poor. KILL_HODL_W is borderline because the dual-window
construction interacts non-trivially with autocorrelation.

50% PARTIAL prior reflects expectation that 1-2 criteria probably get
discarded (likely WR, possibly SYM).

## Files

- `results/kill_bar_recal_decision_rule_2026-05-07.md` — this file
- `scripts/kill_bar_recal.py` — to be added (extends kill_bar_calibration.py)
- `results/kill_bar_recal_2026-05-07.txt` — raw output
- `results/kill_bar_recal_verdict_2026-05-07.md` — verdict applying this rule

## What this WILL produce, and what it WILL NOT

WILL: a list of recalibrated thresholds (or DISCARDED markers) per
criterion, with verified FP/TP rates under the locked bands.

WILL NOT: an automatic update to `forward_paper_status.sh` or CLAUDE.md.
The recalibration verdict is the input for those changes; the user
explicitly approves before any production code is modified.
