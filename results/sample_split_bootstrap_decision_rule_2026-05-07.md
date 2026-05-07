# Sample-Split Bootstrap Cross-Validation — PRE-REGISTERED Decision Rule

**Status:** Locked before any backtest runs. Committed as standalone change.

## Hypothesis being tested

The block-bootstrap CI from A1 (`results/bootstrap_ci_verdict_2026-05-07.md`)
established mean +$130k/yr with 95% CI [+$42k, +$220k] over the full 5y × 16 sym
deployed-candidate dataset (2,210 trades). That CI is the load-bearing
statistical claim used for forward-paper deploy expectations alongside the
walk-forward CI.

**The claim under test:** the +$130k/yr mean is REGIME-INDEPENDENT — i.e.,
both halves of the sample (when split at the calendar midpoint) independently
clear a meaningful positive lower-bound, not just the cumulative aggregate.

If REGIME-INDEPENDENT holds, deploy expectations stay anchored to the existing
bootstrap CI.

If it does NOT hold, we have evidence of regime-dependence that the cumulative
CI averages over — which means real-money deploy decisions should anchor to
the WEAKER half's CI as the conservative read.

## Method

Stationary block bootstrap (Politis-Romano), identical to A1, applied
independently to each half of the trade-level dataset:

- **Half A**: trades with entry timestamp in [2020-04-01, 2022-10-31]
- **Half B**: trades with entry timestamp in [2022-11-01, 2025-04-30]

Split point 2022-10-31 is the calendar midpoint of the original 5y window.

Per half:
- Mean block length L = round(√N) at that half's trade count (rule-of-thumb
  consistent with A1)
- B = 5,000 resamples (same as A1)
- Random seed = 42 + L (same as A1)
- Cost stack: fee=10bp, slip=5bp, funding=historical (deployed config)

## Decision rule (LOCKED — no post-hoc tuning)

For each half, we measure two quantities from the bootstrap distribution
of annualized NET:

- **lo95**: lower bound of the 95% CI (2.5% quantile)
- **p_pos**: bootstrap probability that annualized NET > $0

Per-half pass condition: `lo95 > $0 AND p_pos ≥ 95%`.

| Verdict tier | Condition | Interpretation |
|:---|:---|:---|
| **ROBUST_BOTH**       | both halves pass         | Cumulative CI is regime-independent. Anchor unchanged. |
| **PARTIAL_REGIME_DEP** | exactly one half passes  | Real regime-dependence. Anchor to weaker half's CI. |
| **FRAGILE**           | neither half passes      | Cumulative claim depends on aggregation. Major flag. |

## What is NOT permitted under this pre-registration

- **No sweeping the split point post-hoc.** 2022-10-31 is the calendar
  midpoint and is fixed before observing data. Trying alternative split dates
  to find a flattering result is data mining.
- **No sweeping the bootstrap block length** L beyond the rule-of-thumb √N.
- **No re-running with a different random seed** to find a flattering result.
- **No reframing the threshold** (e.g., changing $0 lower bound to $50k or
  vice versa) after observing the data.
- **No combining halves into a weighted re-aggregate** — that defeats the
  whole point of the split.

A FRAGILE outcome decisively updates expectations downward; it is not a
license to retry the test with a different split.

## Pre-registered priors

Before running:

| Outcome | Prior |
|---|---:|
| ROBUST_BOTH | 60% |
| PARTIAL_REGIME_DEP | 30% |
| FRAGILE | 10% |

Reasoning: the walk-forward 6-window CI ([-$111k, +$372k]) is much wider than
the bootstrap CI, suggesting regime variance the trade-level bootstrap misses.
A 30% prior on PARTIAL captures the walk-forward signal that variance is
quarter-correlated. The 60% on ROBUST reflects the dominant cumulative signal
(99.9% P>$0) which doesn't usually flip on a 50/50 split unless one regime is
qualitatively different.

## Files

- `results/sample_split_bootstrap_decision_rule_2026-05-07.md` — this file
- `scripts/sample_split_bootstrap.py` — to be added with results
- `results/sample_split_bootstrap_2026-05-07.txt` — raw output
- `results/sample_split_bootstrap_verdict_2026-05-07.md` — verdict applying
  this locked rule mechanically to the actual numbers
