# Edge Stability Analysis — PRE-REGISTERED Decision Rule

**Status:** Locked before any analysis runs. Same discipline as F1/F2,
sample-split bootstrap, kill-bar calibration.

## Question

The A1 bootstrap CI and A3 sample-split bootstrap both validate the
*cumulative* +$130k/yr claim over the 5y × 16 sym dataset. The sample-split
test compared Half A (+$121k/yr) vs Half B (+$141k/yr) and found slight
improvement, not decay.

But quarter-level granularity could reveal a trend that 50/50 splits average
out. **Specifically:** is the strategy's edge stable across the 21 calendar
quarters of the 5y dataset, or is it decaying / improving over time?

This matters because the deployed strategy in forward-paper is exposed to
the *current* market structure. If the edge has been declining quarter-over-
quarter, anchoring expectations to the cumulative 5y rate is biased upward —
forward-paper should anchor to the *recent* regime instead.

## Method

Bucket the 5y × 16 sym dataset into 21 calendar quarters (2020-Q1 →
2025-Q1, where 2020-Q1 starts at the first trade and 2025-Q1 ends at
the last trade). Compute strategy net PnL per quarter.

Run ordinary least squares regression: `NET_q = a + b · t` where `t` is the
quarter index 1..21.

Significance level α = 0.05 (locked). Two-sided test on slope `b`.

## Decision rule (LOCKED)

| Tier | Condition | Interpretation |
|---|---|---|
| **STABLE_EDGE**     | p(slope) > 0.05 | No detectable trend. Cumulative anchor stands. |
| **IMPROVING_EDGE**  | p(slope) ≤ 0.05 AND slope > 0 | Edge is growing. Forward-paper expectations may be conservative. |
| **DECAYING_EDGE**   | p(slope) ≤ 0.05 AND slope < 0 | Edge is shrinking. Forward-paper expectations should be revised down. |

## What is NOT permitted

- ❌ No alternative window definitions (no monthly buckets, no 6-month
  buckets — calendar quarters only)
- ❌ No alternative regression forms (no quadratic, no log-linear)
- ❌ No alternative significance levels (α=0.05 locked)
- ❌ No truncating the dataset (full 5y, not "recent 3y to test recent decay")
- ❌ No swapping in different cost stacks for comparison

A DECAYING_EDGE outcome is a finding that updates expectations. It is NOT a
license to re-test alternative regression forms looking for the most
flattering result.

## Pre-registered priors

| Outcome | Prior |
|---|---:|
| STABLE_EDGE      | 65% |
| IMPROVING_EDGE   | 15% |
| DECAYING_EDGE    | 20% |

Reasoning: A3 sample-split bootstrap showed Half B (recent) is *slightly
better* than Half A (early), arguing against decay at coarse granularity.
Cumulative variance is huge so a decay trend would have to be substantial
to clear α=0.05 with n=21. 65% prior on STABLE reflects this. The 20%
prior on DECAYING reflects the structural concern that crypto markets have
become more efficient — even if the 5y average looks fine, recent quarters
might be marginal.

## Decision implications (declared up front)

If STABLE_EDGE: no anchor revision needed. Forward-paper expectations stay
on the bootstrap CI.

If IMPROVING_EDGE: the cumulative anchor is conservative. Forward-paper
performance may exceed it. Acknowledge but DO NOT revise upward — that
would be optimistic re-anchoring and methodologically unsound.

If DECAYING_EDGE: the cumulative anchor is biased upward. Forward-paper
expectations should be revised to the recent-quarter average. Specifically:
extrapolate the regression line to the next forward-paper quarter and use
that as the central estimate. Walk-forward CI half-width remains the
uncertainty.

## What this analysis does NOT do

- It does not test for *regime change* (a sudden shift in level rather
  than a slow trend) — that would require a structural break test.
- It does not test for *autocorrelation in quarterly returns* — that's A1's
  trade-level autocorr applied at a different scale; could be a follow-up.
- It does not propose a *new strategy* — only quantifies whether the
  current one is stable.

## Files

- `results/edge_stability_decision_rule_2026-05-07.md` — this file
- `scripts/edge_stability.py` — to be added
- `results/edge_stability_2026-05-07.txt` — raw output
- `results/edge_stability_verdict_2026-05-07.md` — verdict
