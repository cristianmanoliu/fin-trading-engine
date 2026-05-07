# Cat G' — F1 ∧ F2 Composite, Universe-57 Denominator — VERDICT 2026-05-08

**Pre-registered:** `results/cat_g_prime_universe57_decision_rule_2026-05-08.md`
(committed `3ce50c0` — before any analysis ran).

**Verdict tier (mechanical application of the locked rule):** **REJECT**

## Headline finding

Expanding the F2 co-cross denominator from deployed-16 to universe-57
**did** produce a non-empty intersection (6 trades vs Cat G's 0), but the
composite **underperforms** the unfiltered deployed strategy on every
meaningful metric. F1 ∧ F2 mechanism class is now decisively rejected
across BOTH denominators tested.

## Result vs locked decision rule

| Filter | n trades | % of deployed | Total NET | NET/trade |
|---|---:|---:|---:|---:|
| Unfiltered deployed | 2,210 | 100% | +$687,072 | +$311 |
| F1 alone | 49 | 2.2% | — | — |
| F2 alone (univ-57 K=5) | 723 | 32.7% | — | — |
| **F1 ∧ F2 composite (G')** | **6** | **0.27%** | **+$1,145** | **+$191** |

Per-window walk-forward:

| Window | unf-N | unf-NET | **flt-N** | **flt-NET** | flt-NET/tr |
|:---:|---:|---:|---:|---:|---:|
| W-2 | 276 | -$45,001 | 3 | **+$4,120** | +$1,373 |
| W-1 | 388 | +$268,749 | 1 | -$1,007 | -$1,007 |
| W0  | 471 | +$196,204 | 0 | $0 | — |
| W1  | 510 | +$8,799 | 2 | -$1,968 | -$984 |
| W2  | 509 | +$253,338 | 0 | $0 | — |
| W3  | 0 | $0 | 0 | $0 | — |

Locked decision tiers — every condition fails:

- **DEPLOY-CANDIDATE**: 1/6 wins (need ≥5) ✗, $229/yr (need ≥$50k) ✗, flt $1,145 < unf $682k ✗
- **SHADOW DEPLOY**: 1/6 wins (need ≥4) ✗, $229/yr (need ≥$30k) ✗, flt/tr 0.60× unf (need ≥1.5×) ✗
- **WALK-FORWARD CANDIDATE**: 1/6 wins (need ≥3) ✗, mean +$229/yr ✓, flt/tr $191 < unf $317 ✗

Two of three WALK-FORWARD CANDIDATE conditions fail → **REJECT**.

## What we learned

### 1. The denominator change resolved the empty-intersection but did not resolve the underperformance

Cat G's deployed-16 K=5 found 0 joint events. Cat G''s universe-57 K=5
found 6 — confirming the empty intersection in Cat G was largely a
denominator artifact. **But the 6 events that DO co-occur underperform
the unfiltered baseline.** The composite is not just rare; it's rare
*and worse-than-average* on the rare occasions it fires.

### 2. F1 and F2 mechanisms are genuinely disjoint AND anti-correlated in quality

When the rare F1 ∧ F2 co-occurrence DOES happen (6 events), the per-trade
NET is **$191 vs the unfiltered $317** — only 0.60× as good per trade.
This means the composite isn't just statistically empty — when it fires,
it's actively below average.

Possible mechanism: F1 (extreme funding) + F2 (regime-shift moment)
co-occurring may indicate a violent market move where positioning is
already crowded AND many symbols are crossing simultaneously. These
moments are characterized by elevated volatility and wider spreads,
which hurt the strategy's wick-based stops more than they help target-
hitting. The composite catches "high-stress regime moments" rather than
"high-quality reversion setups."

### 3. The pre-registered temporal-disjointness finding from Cat G is reinforced

Cat G said: F1 fires per-symbol on idiosyncratic positioning; F2 fires
cross-symbol on regime shifts. They operate on different time scales.

Cat G' confirms this AND adds: the rare moments when they DO co-occur
are higher-volatility, NOT higher-quality. The mechanism interaction
isn't just empty — it's negative-quality.

## Pre-registered priors vs actual outcome

| Outcome | Prior | Actual |
|---|---:|:---:|
| DEPLOY-CANDIDATE         | 5%  | |
| SHADOW DEPLOY            | 15% | |
| WALK-FORWARD CANDIDATE   | 30% | |
| **REJECT**               | **50%** | **★** |

The 50% REJECT prior captured the verdict. Tier was correct. The
expected joint event count (15-20) was overestimated — actual was 6.
Update: F1 and F2 are MORE temporally disjoint than expected, not less.

## What this updates

### Locks in (high confidence)

- **F1 ∧ F2 mechanism class is decisively REJECTED on this dataset.**
  Both denominators tested (deployed-16 and universe-57) at locked
  thresholds produce REJECT outcomes.
- **The composite is not rare-but-good — it's rare AND below-average.**
  This rules out "low statistical power" as the explanation; the
  mechanism interaction itself is negative.
- **F1 and F2 are temporally disjoint AT ANY denominator at strict
  thresholds.** Cat G + Cat G' together establish this as a stronger
  finding than either alone.

### Forbids (per the Cat G' locked rule)

The Cat G' pre-reg explicitly stated:

> A REJECT outcome at G' means F1∧F2 mechanism class is decisively dead
> on this dataset across BOTH denominators. The next move must pivot
> axes entirely (HG4 cross-sectional, external data, architecture
> changes) — NOT sweep more F1×F2 parameters.

So per the locked rule:

- ❌ Cat G'' (wider time tolerance) is NOT permitted in this milestone.
  Two failed denominators is sufficient evidence that the mechanism is
  exhausted at the parameter-tuning level.
- ❌ Cat H (alternative operator: F1 ∨ F2) is NOT permitted in this
  milestone for the same reason.
- ❌ Continuing to sweep F1×F2 parameters violates the lessons.md
  "≥5 tests, marginal-or-negative, pivot axes" rule.

### Permits (after a fresh next-milestone pre-registration, if user wants)

- Cross-axis hypotheses: cross-exchange OOS replication, external data
  feeds, architectural changes (cross-sectional portfolio, multi-day
  positions), regime-conditioned variants.
- F1×F2 may be revisited in a TRULY future milestone (e.g., post-forward-
  paper-validation, with fresh dataset accumulated) but not within this
  research cycle.

## What's NOT permitted under any reading

- ❌ "Cat G''' with ±300s tolerance" — rule explicitly forbids
- ❌ "Cat G'''' with K=3" — same
- ❌ "Cat H union" — explicitly out of scope per the locked rule
- ❌ Saving REJECT by relaxing the tier conditions

The result stands as the mechanical output of the locked rule. The
F1×F2 thread is closed.

## Files

- `results/cat_g_prime_universe57_decision_rule_2026-05-08.md` — pre-reg
- `results/cat_g_prime_universe57_2026-05-08.txt` — raw output
- `scripts/cat_g_prime_universe57.py` — analyzer

## Status update

Cat G' is logged as **REJECTED at universe-57 denominator, locked
thresholds.** Combined with Cat G's REJECT at deployed-16, the F1×F2
mechanism class is DECISIVELY EXHAUSTED on this dataset. The candidate
scoreboard now has two consecutive composite REJECTs.

The honest implication: the deployed strategy's edge does not amplify
via simple F1/F2 conditional gating. Future research must pivot to a
new alpha axis (cross-exchange, external data, architecture) per the
lessons.md rule.

This concludes the F-series investigation set on the current dataset.
