# Pre-registered decision rule for Cat D2a: out-of-sample confirmation of D1
**Written: 2026-05-07, BEFORE running D2a backtest.**

## Purpose

D1 (1D EMA 9/21 confluence) verdicted SUPPORTIVE-UNDERPOWERED under the D2
Sharpe-gated rule. The D2 rule explicitly required an out-of-sample variant
test before any deployment escalation. D2a IS that test.

## What we're testing

**D2a:** Same mechanism as D1 (1D bias confluence filter on the 4H short EMA
9/21 entry signal), but with **DIFFERENT 1D EMA periods**. This makes D2a
genuinely out-of-sample for the Sharpe rule:

- The Sharpe rule's threshold (+50% for SUPPORTIVE) was set knowing D1's
  observed +28%. That's data-snooping for D1.
- D2a uses NEW parameters (1D EMA 5/13). Its Sharpe is unobserved when this
  rule is being applied. Truly out-of-sample.

**Parameter choice:** 1D EMA 5/13 (faster periods than D1's 9/21).
- Hypothesis: faster bias filter reacts to regime transitions sooner; may
  capture more genuine regime-shift opportunities than the slower 9/21.
- Pre-registered as the ONE variant tested. NOT iterating on parameters
  after seeing results (commitment #1 below).

## Decision rule

**Identical to D2 rule** (`results/cat_d2_sharpe_gated_rule_2026-05-07.md`).
Re-stated here for self-containment:

| Outcome                                                          | Verdict                       | Action                                              |
|------------------------------------------------------------------|-------------------------------|-----------------------------------------------------|
| Sharpe ≥ baseline × 2.00 AND sum > 0                             | **STRONG**                    | Deploy as shadow + 60 days; consider live replace at 60 days if forward-paper concurs |
| Sharpe ≥ baseline × 1.50 AND sum > 0                             | **SUPPORTIVE**                | Deploy as shadow + 90 days observation              |
| Sharpe ≥ baseline × 1.25 AND sum > 0                             | **SUPPORTIVE-UNDERPOWERED**   | Insufficient evidence to deploy D1 (one underpowered + one underpowered = two underpowered, not one strong) |
| Sharpe > baseline AND sum > 0                                    | **NEUTRAL**                   | D1 was likely noise; do NOT deploy D1              |
| Sharpe ≤ baseline OR sum ≤ 0                                     | **REJECTED**                  | D1 was likely noise; do NOT deploy D1              |

**Note:** the D2 rule's SUPPORTIVE-UNDERPOWERED action was "shadow + 180 days
+ required out-of-sample test." That out-of-sample test IS D2a — so D2a's
verdict gates the D1 deployment decision more than D1's own verdict did.

## D1 deployment decision matrix

After D2a runs, the combined D1+D2a evidence determines deploy:

| D1 verdict | D2a verdict | Combined deploy decision for D1 |
|------------|-------------|---------------------------------|
| SUPPORTIVE-UNDERPOWERED | STRONG or SUPPORTIVE | **Deploy D1 (1D EMA 9/21) as shadow** — out-of-sample variant confirmed mechanism |
| SUPPORTIVE-UNDERPOWERED | SUPPORTIVE-UNDERPOWERED | **Do NOT deploy D1** — both signals are within noise; need bigger sample to distinguish |
| SUPPORTIVE-UNDERPOWERED | NEUTRAL or REJECTED | **Do NOT deploy D1** — out-of-sample variant failed; D1 was likely noise |

Note: BB deploy decision is INDEPENDENT of D2a (BB was already validated under
two independent rules — Cat A literal and D2). BB shadow deploy proceeds
regardless of D2a outcome.

## Pre-registered hypothesis

PRIOR EXPECTATION: most likely **NEUTRAL or REJECTED**. Reasoning:
- D1's +28% Sharpe was statistically within noise (~1 SE). Random parameter
  variations of D1's mechanism are likely to scatter Sharpe values around
  baseline ±0.4 by chance.
- A 5/13 confluence is structurally faster — could either capture real
  regime change benefit OR introduce more noise from over-reactive bias flips.
- Most likely outcome: D2a Sharpe within ±10% of baseline. NEUTRAL.

If D2a verdicts SUPPORTIVE or stronger: this is unexpected positive evidence
for the D1 mechanism. Combined with D1's own SUPPORTIVE-UNDERPOWERED, the
combined signal is more credible. Deploy D1 as shadow.

If D2a verdicts NEUTRAL or REJECTED: D1's signal was likely noise at this
sample size. Don't deploy D1. The Sharpe-rule framework is preserved but
this specific candidate doesn't pass out-of-sample.

## Commitments NOT to do regardless of results

1. NOT test additional period combinations (3/8, 7/15, 13/34, etc.) after seeing D2a results. ONE pre-registered variant.
2. NOT lower the threshold "because D2a was close." If D2a misses by 5%, it misses.
3. NOT switch back to sum-gate to "rescue" D2a if Sharpe rejects.
4. NOT redefine the deployment decision matrix above after seeing combined results.
5. If both D1 and D2a are SUPPORTIVE-UNDERPOWERED: the combined signal is two weak signals, NOT one strong signal. Do not deploy.
6. The required "real-money escalation" (whenever D1 or D2a's eventual shadow result allows it) follows the existing forward-paper criteria in `CLAUDE.md` — not overridden by D2 or D2a verdicts.

## Multiple-comparison context

This is the **10th candidate tested against the same baseline**. Family-wise
false-positive rate continues to grow. Treat any positive verdict
~3.5× more skeptically than at first pass.

That said: D2a is specifically designed to be a SECOND independent observation
of the SAME mechanism. If both D1 (already observed) and D2a (about to be observed)
agree, the combined evidence is stronger than the multiple-comparison concern
suggests for two independent candidates.
