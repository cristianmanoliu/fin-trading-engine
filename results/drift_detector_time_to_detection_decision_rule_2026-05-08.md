# Drift Detector Time-to-Detection — PRE-REGISTERED Decision Rule

**Status:** Locked before any simulation runs. Same discipline as F1, F2,
Cat G, Cat G', Cat X, sample-split bootstrap, edge stability, kill-bar/
drift-detector calibrations.

This is the **last analysis** I'm running today before genuine stopping.
After this, the backtest investigation set is closed pending forward-
paper data accumulation.

## Question

The drift detector calibration verdict
(`results/drift_detector_calibration_verdict_2026-05-07.md`) found
TP(deg30)=TP(deg50)=TP(dead)=100% at every operating point in the
locked grid (α_family ∈ {0.05, 0.01, 0.005, 0.001}, N_LIVE ∈ {50, 100,
150, 200, 300}). Recommended deploy: α=0.001, N_LIVE=50.

But that calibration was at **fixed N**. In real forward-paper, N
accumulates over time at the historical fleet rate of ~1.18 trades/day
and the detector is run *sequentially* — daily as new trades arrive.

Two unanswered questions:

1. **Time-to-detection (TP timing)**: under degradation injected at day 0,
   what's the distribution of the day the detector first fires? Median?
   P75? P90?
2. **Sequential-testing inflation (FP timing)**: under null, daily
   re-testing of an accumulating sample inflates cumulative Type-I error
   relative to the single-shot α=0.001. How much by the 30/60/90/180/
   365-day mark?

These directly bear on the deploy decision. If detection of a dead
strategy takes 6 months, real-money exposure to broken-strategy losses
is large. If it takes 1 month, exposure is bounded.

## Method

Day-by-day forward-paper simulation, B=1,000 paths per scenario.

### Trade arrivals
- Each simulated day: draw Poisson(1.18) new trades from the historical
  pool (with replacement), apply scenario transform.
- Pool: `results/hod_journals/2026-05-07-mfe/` (deployed-16 + MFE/MAE
  fields, same source as drift_detector_calibration).

### Detector
- Identical to deployed: 5 Welch t-tests (pnl_per_trade, mfe_r, mae_r,
  loss_pnl_abs, win_pnl) + 1 WR z-test, Bonferroni-corrected at
  α_family / 6.
- α_family = 0.001 (locked from drift_detector_calibration_verdict).
- Reference: full historical pool (N=2,210), unchanged across simulation.
- N_MIN = 30 (locked from detector code).

### Sequential cadence
- After each day's new trades append, if sample size ≥ N_MIN, run detector.
- Record: first day detector fires (relative to day 0). If never fires
  within 365 days: censored.

### Scenarios (locked, identical to drift_detector_calibration)
| Scenario | Per-trade PnL transform |
|---|---|
| null  | identity (× 1.0) |
| deg30 | × 0.7 |
| deg50 | × 0.5 |
| dead  | shift mean to 0 by subtracting historical global per-trade mean |

MFE/MAE distributions unchanged across scenarios (consistent with
calibration). Degradation timing: day 0 (worst-case from operational
standpoint).

## Decision rule (LOCKED)

Two acceptance bands at the locked deploy operating point (α=0.001):

- **FP band**: cumulative FP rate at day 365 under null ≤ 20%
- **TP band**: median detection day under "dead" ≤ 60 days

Verdict tiers:

| Tier | Conditions |
|---|---|
| **DEPLOYABLE_AS_IS** | FP(null, 365d) ≤ 20% AND median(dead) ≤ 60 days |
| **NEEDS_TUNING**     | else |

If NEEDS_TUNING, the verdict identifies WHICH bar fails:
- Cumulative FP > 20% → α should be tightened further (e.g., to 0.0001)
- Median detection > 60 days → α should be loosened OR cadence changed

These are diagnostics, not solutions. Re-tuning is a separate fresh
pre-registration in a future milestone.

## What is NOT permitted

- ❌ No alternative α grid (locked at 0.001 from prior verdict)
- ❌ No alternative N_MIN (locked at 30)
- ❌ No alternative test cadence (daily locked)
- ❌ No alternative scenario definitions
- ❌ No alternative trade rate (1.18/day, historical fleet)
- ❌ No degradation injection at day > 0 (worst-case at day 0 locked)
- ❌ No reframing the bands

A NEEDS_TUNING outcome is the verdict. The diagnostic flags WHICH bar
fails but does not authorize re-running with different parameters in
this milestone.

## Pre-registered priors

| Outcome | Prior |
|---|---:|
| DEPLOYABLE_AS_IS | 55% |
| NEEDS_TUNING — too slow on dead | 25% |
| NEEDS_TUNING — too noisy on null | 20% |

Reasoning: the calibration found TP=100% at N=50 (= ~42 days at fleet
rate). Sequential testing should fire substantially before N=50 because
the t-statistic accumulates power over time. Median detection ≤ 60
days seems plausible.

The 25% on slow-detection captures: at N=30 under "dead" the t-statistic
might still be too noisy to fire reliably; could take 60-90 days to
accumulate enough power.

The 20% on noisy-null captures: 365 days of daily testing on accumulating
sample gives ~335 sequential tests; even at α/6 = 0.0001667 per-test, with
correlated tests, cumulative FP could exceed 20%.

## Computational scope

B=1,000 paths × 4 scenarios × 365 days × 6 tests = ~9M test evaluations.
Each Welch test is closed-form ~10μs in Python. Estimated runtime: 60-120
seconds. Bounded.

## Files

- `results/drift_detector_time_to_detection_decision_rule_2026-05-08.md` — this file
- `scripts/drift_detector_time_to_detection.py` — analyzer (extends
  drift_detector_calibration.py)
- `results/drift_detector_time_to_detection_2026-05-08.txt` — raw output
- `results/drift_detector_time_to_detection_verdict_2026-05-08.md` — verdict

## Why this is the last backtest analysis today

Per the lessons.md "≥5 tests, dataset exhausted" rule applied with
some grace: today we've executed 9 pre-registered analyses. After this
10th one, every concrete decision-relevant question I can pre-register
on the historical data has been answered.

What's left after this analysis: forward-paper data accumulation,
real-money small-tranche prep (gated on forward-paper), and operational
monitoring.

This pre-registration explicitly commits: **no further analyses on the
2020-2025 historical dataset within this research milestone after the
verdict for this analysis lands.** Further hypotheses require a fresh
milestone (post-forward-paper-validation, with new data).
