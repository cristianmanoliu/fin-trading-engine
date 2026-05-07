# Cat G' — F1 ∧ F2 Composite, Universe-57 Co-Cross Denominator — PRE-REGISTERED Decision Rule

**Status:** Locked before any analysis runs. Same discipline as F1, F2, Cat G,
sample-split bootstrap, edge stability, kill-bar/drift-detector calibrations.

This is the **direct follow-up** explicitly permitted by the Cat G verdict
(`results/cat_g_f1xf2_composite_verdict_2026-05-08.md`):

> Permits in next milestone, with fresh pre-registration:
> - F1 ∧ F2 with universe-57 co-cross denominator (matches F2's setup)

The Cat G verdict found EMPTY intersection at deployed-16 K=5 (≥31% of 15
other symbols). The question this pre-registration tests: **does the
composite have non-empty intersection at universe-57 K=5 (≥9% of 56 other
symbols)?** And if so, does it produce decision-tier-passing economics?

## What changes vs Cat G (the ONLY change)

| Parameter | Cat G (rejected) | Cat G' |
|---|---|---|
| Universe for trade pool | deployed-16 | deployed-16 (unchanged) |
| F1 threshold | > 30 bp/day | > 30 bp/day (unchanged) |
| **F2 co-cross denominator** | **deployed-16 (15 others)** | **universe-57 (56 others)** |
| F2 K threshold | ≥ 5 | ≥ 5 (unchanged — matches F2's original) |
| F2 time tolerance | ±60s | ±60s (unchanged) |
| Cost stack | fee=10/slip=5/funding=historical | unchanged |
| Walk-forward windows | 6 (same as F1/F2/G) | unchanged |

**Exactly one parameter is varied** — the denominator. Everything else is
locked from Cat G.

## Why this is permitted (and not data-mining)

- The Cat G verdict explicitly noted "deployed-16 K=5 = 31%" vs "universe-57
  K=5 = 9%" as a structural difference, not a tuning knob.
- F2's original verdict used universe-57. Aligning Cat G' with F2's
  denominator is methodologically consistent, not parameter-fishing.
- This is a SINGLE follow-up, not a sweep. The Cat G' result will be
  treated as decisive: PASS at some tier OR REJECT. No further
  denominator variants permitted in this milestone.

## Method

Identical to Cat G except F2 condition uses universe-57 trade timestamps
to compute the co-cross count.

For each deployed-16 trade at timestamp `t`:
- F1 condition: 8h funding × 3 × 10000 > +30 bp/day at `t`
- F2 condition: count number of OTHER universe-57 trades with same side
  within ±60s of `t`. Threshold: ≥ 5.
- Composite: keep only trades where BOTH F1 and F2 conditions hold.

Walk-forward 6-window evaluation at the same windows as Cat G.

## Decision rule (LOCKED — same as Cat G)

| Tier | Conditions (ALL must hold) |
|---|---|
| **DEPLOY-CANDIDATE**       | ≥5/6 windows positive AND mean annual NET ≥ $50k/yr AND filtered NET ≥ unfiltered NET |
| **SHADOW DEPLOY**          | ≥4/6 windows positive AND mean annual NET ≥ $30k/yr AND filtered NET/trade ≥ 1.5× unfiltered |
| **WALK-FORWARD CANDIDATE** | ≥3/6 windows positive AND mean > 0 AND filtered NET/trade > unfiltered |
| **REJECT**                 | else |

## What is NOT permitted

- ❌ No sweeping the F1 threshold (locked at 30 bp/day)
- ❌ No sweeping the F2 K threshold (locked at 5)
- ❌ No sweeping the F2 time tolerance (locked at ±60s)
- ❌ No alternative universe denominator (universe-57 is locked; this is
     the ONE allowed change vs Cat G)
- ❌ No alternative composite operators (∧ is locked)
- ❌ No alternative cost stack (10/5/historical only)
- ❌ No relaxing tier conditions

A REJECT outcome here means the F1∧F2 mechanism class is decisively dead
on this dataset across BOTH denominators. The next move must pivot axes
entirely (HG4 cross-sectional, external data, architecture changes) —
NOT sweep more F1×F2 parameters.

## Pre-registered priors (updated based on Cat G evidence)

| Outcome | Prior | Δ vs Cat G |
|---|---:|:---:|
| DEPLOY-CANDIDATE         | 5%  | unchanged |
| SHADOW DEPLOY            | 15% | -5pp (Cat G's 0% intersection updates downward) |
| WALK-FORWARD CANDIDATE   | 30% | -5pp |
| **REJECT**               | **50%** | **+10pp** |

Reasoning: Cat G found F1 and F2 are temporally disjoint at deployed-16
denominator with ±60s tolerance. The universe-57 denominator triples the
F2 fire rate (33% retained vs 3.4% retained in Cat G's deployed-16 setup),
so the joint probability rises from ≈0.07% (Cat G) to maybe 0.7% — non-zero
but still small. Roughly 15-20 joint events expected over 5y across all 16
deployed trade streams. Resolution risk dominates: even if mechanism is
real, statistical power at n≈15-20 is poor, so REJECT prior is high.

The 50% REJECT prior represents the dominant risk (low n + temporal
disjointness). Combined with the 30% WALK-FORWARD CANDIDATE, the prior
mass is heavily on "modest signal at best."

## What this analysis WILL produce, and what it WILL NOT

WILL: a mechanical tier verdict applying the locked rule to whatever
trade count survives the universe-57 composite filter.

WILL NOT: change the Cat G verdict (Cat G is REJECT at deployed-16
denominator, separately and durably). Cat G' is a fresh hypothesis on
related but distinct conditions.

## Files

- `results/cat_g_prime_universe57_decision_rule_2026-05-08.md` — this file
- `scripts/cat_g_prime_universe57.py` — analyzer (extends cat_g_f1xf2_composite.py
  with universe-57 co-cross denominator)
- `results/cat_g_prime_universe57_2026-05-08.txt` — raw output (when run)
- `results/cat_g_prime_universe57_verdict_2026-05-08.md` — verdict (when run)

## Cross-references

- `results/cat_g_f1xf2_composite_decision_rule_2026-05-07.md` — Cat G pre-reg
- `results/cat_g_f1xf2_composite_verdict_2026-05-08.md` — Cat G verdict (REJECT)
- `results/cat_f1_funding_cross_verdict_2026-05-07.md` — F1 verdict (WFC)
- `results/cat_f2_cocross_confluence_verdict_2026-05-07.md` — F2 verdict (WFC)
- `results/hod_journals/2026-05-07-univ57/` — universe-57 journal data
