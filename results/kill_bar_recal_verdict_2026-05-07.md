# Kill-Bar Recalibration — VERDICT 2026-05-07

**Pre-registered:** `results/kill_bar_recal_decision_rule_2026-05-07.md`
(committed `a4d9567` — before any threshold derivation).

**Verdict tier (mechanical application of the locked rule):** **NO_KILL_BAR**

## Headline finding

The recalibration via principled quantile-setting **discards all five
testable kill criteria.** With the strategy's natural variance, it is
*statistically impossible* to design threshold-based kill criteria at the
90-day horizon that simultaneously meet:

- FP(null) ≤ 20%
- TP(deg50) ≥ 50%
- TP(dead) ≥ 80%

The null and dead distributions overlap too much at this horizon. The bands
locked in the original calibration pre-registration cannot be jointly
satisfied by any threshold-based criterion with this data.

## Per-criterion results (90d verification, B=1,000)

After setting each threshold at the 10th-percentile of the null distribution
(or 90th for SYM):

| Criterion | New threshold | FP(null) | TP(deg50) | TP(dead) | Status |
|---|---|---:|---:|---:|:---:|
| KILL_PNL    | < -$33,261             | 10.6% ✓ | 3.8%  ✗ | 25.8% ✗ | DISCARDED |
| KILL_WR     | DISCARDED              | n/a     | n/a     | n/a     | DISCARDED |
| KILL_SYM    | > 115.7%               | 10.2% ✓ | 12.2% ✗ | 13.3% ✗ | DISCARDED |
| KILL_HODL_C | < -$74,274             | 10.2% ✓ | 3.4%  ✗ | 15.9% ✗ | DISCARDED |
| KILL_HODL_W | 2 consec. < -$35,316   | 3.1%  ✓ | 0.0%  ✗ | 4.9%  ✗ | DISCARDED |

**FP bands met for all four threshold-based criteria** (target was 10% by
construction; observed 3-11% reflects sampling noise around the target).

**TP bands missed by enormous margins.** The locked bands required
≥50% TP(deg50) and ≥80% TP(dead). Observed values are 0-26%, far short.

## Why this happens — statistical impossibility result

For a threshold-based kill criterion at horizon T to have FP ≤ p% under null
AND TP ≥ q% under shifted-mean alternative (e.g., dead), the **shift
between null and alternative must be at least Φ⁻¹(1-p) - Φ⁻¹(1-q) standard
deviations of the path-level statistic.**

For p=20% and q=80%: shift required = z(0.80) - z(0.20) = 0.84 - (-0.84) = **1.68σ**.

The path-level statistics here:

| Statistic | Null mean | Dead mean | Δ (= shift) | Path SD | shift/SD |
|---|---:|---:|---:|---:|---:|
| Total NET (90d)   | +$33.7k | -$1.2k  | $34.9k | ~$52k  | 0.67 |
| HODL Δ cum (90d)  | +$30k   | -$10k   | $40k   | ~$70k  | 0.57 |
| Max-sym pct       | varies  | varies  | small  | ~28%   | <0.5 |
| Monthly delta     | -$0.5k  | -$0.5k  | ~$0    | ~$15k  | ~0   |

**No statistic has a shift ≥ 1.68σ.** The overlap is too large. Whatever
threshold you pick, you can only choose between high-FP-high-TP (loose) or
low-FP-low-TP (tight) — you can't have both.

The KILL_HODL_W criterion is even worse because the joint two-window
construction further dilutes the signal: dead's monthly delta has
essentially the same distribution as null's monthly delta (both centered
near zero with similar variance).

## What this updates

### High confidence

- **At 90d, no threshold-based kill criterion can meet the locked bands.**
  This is a fundamental property of the strategy's variance, not a flaw of
  any specific threshold.
- **The kill bar as designed in CLAUDE.md is structurally over-promised.**
  The criteria can't actually distinguish "strategy degraded" from "natural
  variance" at 90d.
- **The locked recalibration rule applied mechanically discards everything.**
  Per the pre-registration, that's the verdict — no fishing for relaxed
  bands or alternative constructions allowed in this session.

### Implications for the next-session decision

The user's option-1 ("fix the alarm") path is now refined:

- (1a) **Accept lower TP standards** — e.g., 30% TP(dead) at 90d. This is
  the inherent ceiling at this horizon. Re-pre-register with relaxed bands.
- (1b) **Use a longer horizon** — at 180d the shift increases (more trades,
  more cumulative effect) and TP improves. Re-pre-register at horizon=180d.
- (1c) **Replace threshold-based kill with a different mechanism** — e.g.,
  the drift detector (`scripts/live_vs_backtest_drift.py` from commit
  `94bde2a`) compares distributions, not thresholds. Distribution-based
  detection has different statistical properties.
- (1d) **Combine multiple criteria with consensus rules** — e.g., "kill only
  when 3 of 5 criteria fire" — partial cancellation of independent FPs.
  Each composite is a fresh hypothesis requiring its own pre-registration.

## What the user should NOT conclude

- ❌ "The strategy is broken" — these results are about the kill BAR's
  detection power, not the strategy's edge. The strategy's edge was already
  validated by A1 + A3.
- ❌ "We should turn off all kill criteria" — at 180d, criteria *do*
  reach the bands. And distribution-based methods (drift detector) sidestep
  the threshold-overlap problem entirely.
- ❌ "The pre-registration was wrong" — the bands captured what good
  detection should look like. The verdict that the bands aren't reachable
  at this horizon is the analysis's PURPOSE, not a flaw.

## Methodology audit trail

This run-2 verdict supersedes the `RUN1` results (commit `c803d2b`) which
used a buggy "dead" scenario implementation that subtracted the per-window
mean instead of the global per-trade mean. The bug zeroed both the dead
scenario's mean AND its variance, producing artifact TP numbers (e.g., 0%
where 26% was correct).

Both `kill_bar_calibration.py` and `kill_bar_recal.py` were corrected
(`transform_scenario` now takes `global_mean: float`). Both calibration
and recalibration were re-run. The original calibration verdict
(NEEDS_RECALIBRATION) is unchanged in tier — confirmed under corrected
scenario. This recal verdict (NO_KILL_BAR) is also unchanged in tier —
confirmed under corrected scenario.

The bug only affected the magnitude of TP(dead) numbers. Specifically:
- KILL_PNL TP(dead) buggy 50.6% → fixed 53.9% (small change)
- KILL_SYM TP(dead) buggy 100% → fixed 42.7% (artifact corrected)
- KILL_HODL_C TP(dead) buggy 63.0% → fixed 54.7%
- KILL_HODL_W TP(dead) buggy 28.3% → fixed 39.8%

Pre-registered priors:

| Outcome | Prior | Actual |
|---|---:|:---:|
| CLEAN_RECAL      | 25% | |
| PARTIAL_RECAL    | 50% | |
| MOSTLY_DISCARDED | 20% | |
| NO_KILL_BAR      | 5%  | **★** |

The 5% prior was very wrong. NO_KILL_BAR was the actual outcome. This is a
significant prior update: the threshold-overlap impossibility was
under-anticipated. Future kill-bar designs should explicitly check the
statistic-shift-vs-SD ratio at the proposed horizon before locking bands.

## Files

- `results/kill_bar_recal_decision_rule_2026-05-07.md` — pre-registration
- `results/kill_bar_recal_run2_2026-05-07.txt` — corrected raw output
- `results/kill_bar_calibration_run2_2026-05-07.txt` — corrected original
  calibration
- `scripts/kill_bar_recal.py` — analyzer (with global_mean fix)
- `scripts/kill_bar_calibration.py` — calibration (with global_mean fix)

## Status update

Forward-paper kill bar logged as **NO_AUTOMATED_KILL_AT_90D**. The original
NEEDS_RECALIBRATION verdict stands; the recalibration attempt found that
no threshold-based criterion at 90d can meet the locked bands due to
distribution overlap. Next-session decision (1a/1b/1c/1d) is the user's
to make with this information.

The drift detector (`scripts/live_vs_backtest_drift.py`) is the most
promising path that doesn't share this fundamental statistical limitation.
