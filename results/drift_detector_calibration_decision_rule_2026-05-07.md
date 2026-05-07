# Drift-Detector Calibration — PRE-REGISTERED Decision Rule

**Status:** Locked before any calibration runs. Same discipline as
F1/F2, sample-split bootstrap, kill-bar calibration, edge stability.

## Question

The kill-bar recalibration verdict
(`results/kill_bar_recal_verdict_2026-05-07.md`, NO_KILL_BAR) found that
threshold-based kill criteria at 90d cannot meet the locked acceptance
bands due to distribution overlap (shift/SD < 1, need ≥1.68).

The recommended path forward (1c) was: switch from threshold-based to
**distribution-based** detection — specifically the existing drift
detector (`scripts/live_vs_backtest_drift.py`, commit `94bde2a`). It uses
Welch t-tests on 5 continuous per-trade metrics plus a WR z-test,
Bonferroni-corrected across 6 tests, against the full backtest empirical
distribution.

**The question this analysis tests:** does the drift detector reach the
same locked acceptance bands as the original kill bar? If yes, it's the
viable path forward. If no, we need (1a) lower TP standards or (1b)
longer horizons.

## Method

Monte Carlo resampling from the historical 5y × 16-sym deployed-candidate
trade pool (with MFE/MAE: `results/hod_journals/2026-05-07-mfe/`). Each
path simulates the detector at a specific `(α_family, N_LIVE)` operating
point under each scenario.

### Operating-point grid (locked, no expansion permitted)

- `α_family ∈ {0.05, 0.01, 0.005, 0.001}` (family-wise α — Bonferroni-
  corrected per-test α = α_family / 6)
- `N_LIVE ∈ {50, 100, 150, 200, 300}` (trade count of the "live" sample)

Total: 20 operating points per scenario.

### Scenarios (locked)

Same as kill-bar calibration:

| Scenario | Per-trade PnL transform | MFE/MAE transform |
|---|---|---|
| **null**   | identity (×1.0) | unchanged |
| **deg30**  | × 0.7           | unchanged (intra-trade dynamics same) |
| **deg50**  | × 0.5           | unchanged |
| **dead**   | shift mean to 0 by subtracting global per-trade mean | unchanged |

The "MFE/MAE unchanged" choice reflects the realistic interpretation:
under a degraded strategy, intra-trade price excursions look similar to
the original; only the exit-classified outcomes shift.

This means the detector's `mfe_r` and `mae_r` tests are designed to NOT
fire under these scenarios — they're specifically catching cost-stack
divergence (slippage, broker microstructure) rather than edge degradation.

For edge degradation, the load-bearing tests are `pnl_per_trade`,
`loss_pnl_abs`, `win_pnl`, and `WR`.

### Bootstrap parameters

- B = 1,000 paths per (α, N_LIVE, scenario)
- Random seed reproducibility across runs

## Selection rule (LOCKED)

For each `α_family` and each `N_LIVE`:
- Compute FP rate at scenario=null
- Compute TP rate at scenario=deg50, scenario=dead

Across the 20 operating points, select the `(α*, N*)` that maximizes
**TP(dead)** subject to **FP(null) ≤ 20%**.

Tie-breaking: if multiple points reach the same TP(dead), prefer the one
with smaller N_LIVE (faster detection).

## Verdict tiers

| Tier | Condition |
|---|---|
| **DETECTOR_VIABLE**   | At selected (α*, N*): FP(null) ≤ 20% AND TP(deg50) ≥ 50% AND TP(dead) ≥ 80% |
| **DETECTOR_PARTIAL**  | Selected point exists but TP(dead) < 80% (the best achievable falls short) |
| **DETECTOR_TOO_NOISY** | No (α, N_LIVE) in the grid satisfies FP(null) ≤ 20% |

A DETECTOR_VIABLE outcome means we have a working operational kill mechanism.

A DETECTOR_PARTIAL outcome means the detector is better than threshold
kill (which was DISCARDED) but doesn't reach the original acceptance
bands. We accept it with caveats.

A DETECTOR_TOO_NOISY outcome means even Bonferroni-corrected the detector
fires too often under null — we'd need stricter tests or different
metrics.

## What is NOT permitted

- ❌ No alternative `α_family` grid (e.g., adding 0.1 or 0.0001)
- ❌ No alternative `N_LIVE` grid (e.g., 25, 75, 500)
- ❌ No alternative scenario definitions (e.g., scaling MFE/MAE under deg)
- ❌ No alternative selection rule (e.g., maximize TP(deg50) instead of TP(dead))
- ❌ No replacing Bonferroni with FDR or Holm correction
- ❌ No removing metrics from the drift detector
- ❌ No re-running with different B

A DETECTOR_PARTIAL outcome that misses TP(dead) ≥ 80% is the verdict; it
is NOT a license to add more tests, longer windows, or alternative
multiple-comparison corrections in this session. Such moves are separate
hypotheses requiring their own pre-registration.

## Pre-registered priors

| Outcome | Prior |
|---|---:|
| DETECTOR_VIABLE      | 35% |
| DETECTOR_PARTIAL     | 50% |
| DETECTOR_TOO_NOISY   | 15% |

Reasoning: a properly Bonferroni-corrected family-wise test should have
FP ≈ α regardless of N (so DETECTOR_TOO_NOISY is unlikely if Bonferroni
math is right). At small N_LIVE, individual tests have low power (per
napkin math earlier: at N=100, t-statistic for $311 mean shift is ~0.6,
not detectable). DETECTOR_PARTIAL likely if the grid maxes out at
N_LIVE=300 since 300 trades may still have insufficient power for the
Bonferroni-corrected joint test.

The 35% on VIABLE captures the chance that one or two metrics combine
their power well enough at higher N_LIVE to clear 80%.

## Files

- `results/drift_detector_calibration_decision_rule_2026-05-07.md` — this file
- `scripts/drift_detector_calibration.py` — to be added
- `results/drift_detector_calibration_2026-05-07.txt` — raw output
- `results/drift_detector_calibration_verdict_2026-05-07.md` — verdict
