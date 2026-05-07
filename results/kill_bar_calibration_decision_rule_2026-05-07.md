# Kill-Bar Calibration — PRE-REGISTERED Decision Rule

**Status:** Locked before any Monte Carlo runs. Same discipline as F1/F2 and
the sample-split bootstrap.

## Question

Today (2026-05-07) we wired the full forward-paper kill bar in
`scripts/forward_paper_status.sh`:

| Criterion | Threshold | Source |
|---|---|---|
| Trades closed                | ≥150 (60d) | CLAUDE.md |
| WR (post 150 trades)         | ≥14%       | CLAUDE.md (vs 14.3% breakeven) |
| Net PnL                      | > $0       | CLAUDE.md |
| Single-symbol concentration  | ≤40% of |PnL| | CLAUDE.md |
| Realized fee bps             | ≤12bp      | CLAUDE.md (vs 10bp modeled) |
| Realized slip bps (losers)   | ≤25bp      | CLAUDE.md (cost-cliff edge) |
| BTC-HODL Δ cumulative        | ≥ $0       | CLAUDE.md |
| 2 consecutive 30d windows underperform HODL by >$5k | trigger | CLAUDE.md |

Each threshold was set in CLAUDE.md as "what matters." None has been
validated against the strategy's natural variance.

**The question this analysis tests:** are the thresholds calibrated, or do
they have unacceptable false-positive / false-negative rates?

## Method

Monte Carlo resampling from the 5y × 16-sym deployed-candidate trade
distribution. For each scenario × time horizon, generate B=1,000 forward-paper
paths and apply the kill bar at horizon T. Count how often each criterion
fires.

### Scenarios

| Scenario | Per-trade PnL transform | Interpretation |
|---|---|---|
| **null**         | identity (× 1.0) | Strategy performs as backtest predicted |
| **degraded -30%** | × 0.7            | Modest decay (e.g., regime shift) |
| **degraded -50%** | × 0.5            | Major decay (still profitable but halved) |
| **dead**         | mean shifted to 0, variance preserved | Strategy edge gone, only noise remains |

### Time horizons

- 60d (CLAUDE.md's stated min for deploy decision)
- 90d
- 120d
- 180d

### Sampling

Resample trade *indices* from the historical 2,210-trade distribution at
the historical rate (~1.18 trades/day fleet-wide). Use stationary block
bootstrap with L=√N=47 to preserve the lag-1 ρ=+0.32 autocorrelation
(matches A1 / sample-split methodology).

Per-trade attributes carried in resample: PnL_USD, symbol, timestamp,
fee_USD, slip_USD, notional_USD (the cost decomposition we just added).

For HODL comparison: use the actual BTC daily price series from the same
historical period as the resampled trades. (Resampling preserves the
trade-to-BTC-price mapping by index, so HODL comparison stays meaningful.)

## Decision rule (LOCKED)

For each kill criterion C:

- **FP_rate(C)** = P(C fires by 90d | scenario = null)
- **TP_rate(C, dead)** = P(C fires by 90d | scenario = dead)
- **TP_rate(C, deg50)** = P(C fires by 90d | scenario = degraded -50%)

Acceptable bands per criterion:

| Quantity | Acceptable range |
|---|---|
| FP_rate (null)     | ≤ 20% |
| TP_rate (deg50)    | ≥ 50% (degraded but profitable — partial detection ok) |
| TP_rate (dead)     | ≥ 80% |

Verdict tiers:

| Tier | Condition |
|---|---|
| **WELL_CALIBRATED**     | All criteria meet all three bands |
| **NEEDS_RECALIBRATION** | Any criterion misses any band by >5pp |
| **BORDERLINE**          | All criteria within 5pp of all bands but not perfect |

## What is NOT permitted under this pre-registration

- ❌ No re-running with B>1,000 to pick a better-looking number
- ❌ No alternative scenario definitions ("degraded -40%") to find a flattering result
- ❌ No alternative time horizons (e.g., 75d)
- ❌ No reframing the bands post-hoc — 20%/50%/80% are locked
- ❌ No applying the verdict tier "loosely" — bands are mechanical

A NEEDS_RECALIBRATION outcome is a finding, not a license to fish for
alternative thresholds. If a criterion is mis-calibrated, the FOLLOW-UP
question is "what threshold would calibrate it?" — and that's a separate
pre-registration.

## Pre-registered priors

Before running, my expectations are:

| Outcome | Prior | Rationale |
|---|---:|---|
| WELL_CALIBRATED      | 30% | The thresholds were set thoughtfully but never validated |
| NEEDS_RECALIBRATION  | 50% | Most "set without validation" thresholds end up mis-calibrated somewhere — especially under non-IID dynamics |
| BORDERLINE           | 20% | Plausible middle ground |

I expect the most likely mis-calibration is in the HODL window kill (its
math is variance-sensitive at small sample) or the WR criterion (14%
breakeven leaves only ~1pp of cushion above noise floor at 150 trades).

## Files

- `results/kill_bar_calibration_decision_rule_2026-05-07.md` — this file
- `scripts/kill_bar_calibration.py` — to be added
- `results/kill_bar_calibration_2026-05-07.txt` — raw output
- `results/kill_bar_calibration_verdict_2026-05-07.md` — verdict applying
  this locked rule mechanically
