# Cat G — F1 ∧ F2 Composite Filter — VERDICT 2026-05-08

**Pre-registered:** `results/cat_g_f1xf2_composite_decision_rule_2026-05-07.md`
(committed `32baad3` — before any composite backtest ran).

**Verdict tier (mechanical application of the locked rule):** **REJECT**

## Headline finding

The locked composite filter retains **zero trades** across the entire 5y
deployed-16 dataset. F1 ∧ F2 has an **empty intersection** at the
locked thresholds — every decision-tier condition trivially fails.

## Result vs locked decision rule

| Filter | n trades | % of deployed |
|---|---:|---:|
| F1 alone (funding > +30bp/day, SHORT signals) | 49 | 2.2% |
| F2 alone (≥5 deployed-16 co-cross within ±60s) | 75 | 3.4% |
| **F1 ∧ F2 (composite, design D)** | **0** | **0.00%** |

| Per-window | Filtered N | Filtered NET |
|---|---:|---:|
| W-2 (2020-05 → 2021-04) | 0 | $0 |
| W-1 (2021-05 → 2022-04) | 0 | $0 |
| W0  (2022-05 → 2023-04) | 0 | $0 |
| W1  (2023-05 → 2024-04) | 0 | $0 |
| W2  (2024-05 → 2025-04) | 0 | $0 |
| W3  (2025-05 → 2026-04) | 0 | $0 |

All decision tiers fail because filtered N = 0:
- DEPLOY-CANDIDATE: 0/6 positive windows ≠ ≥5 ✗
- SHADOW DEPLOY: 0/6 positive ≠ ≥4 ✗
- WALK-FORWARD CANDIDATE: 0/6 positive ≠ ≥3 ✗

→ **REJECT** is the only tier that fits.

## Mechanism interpretation

The empty intersection IS a finding, not a null result. It tells us
something concrete about how F1 and F2 fire:

**F1 fires on per-symbol positioning extremes** (one symbol's funding
spikes high). These tend to be idiosyncratic — driven by exchange-
specific flows, single-symbol news, or funding-arbitrage on one venue.

**F2 fires on cross-symbol regime shifts** (many symbols cross
together). These are market-wide regime moments — broad sentiment
flips, BTC-led moves dragging the universe, macro shocks.

These mechanisms operate on **different time scales and different
correlation structures.** F1 events happen one symbol at a time. F2
events happen many symbols simultaneously. Their co-occurrence within
±60s is the "extreme positioning specifically on a symbol whose
crowding aligned with a regime shift" — which the data tells us
empirically never happened cleanly in 5y at the locked thresholds.

This empirical orthogonality is an additional finding beyond what F1
and F2's individual r=0.41 and r=0.53 vs deployed (different metric)
suggested. They're not just statistically independent of deployed —
they're **temporally disjoint from each other** at strict thresholds.

## Important interpretive caveat (not a deviation)

The locked Cat G pre-reg used **deployed-16** for the F2 co-cross
denominator (K=5 out of 15 other symbols). The original F2 verdict
used **universe-57** (K=5 out of 56). With 16 symbols, K=5 means
≥31% of the universe co-crossed; with 57 symbols, K=5 means ≥9%.

The two are not the same condition. Cat G's locked rule chose
deployed-16 explicitly. F2 retained 33% of trades at universe-57 K=5;
Cat G's F2-alone retains only 3.4% of trades at deployed-16 K=5.

This was the design choice locked at pre-registration. The verdict
applies mechanically per the locked rule. **Per the locked
"What is NOT permitted" section, no re-running with universe-57 K is
allowed in this milestone** — it would be a separate hypothesis.

What this means: the REJECT verdict is specifically for "F1 ∧ F2 over
deployed-16 at locked thresholds." It does NOT preclude a future
pre-registration testing F1 ∧ F2 with the universe-57 co-cross
denominator (or different K, or different time tolerance). Each is a
fresh hypothesis.

## Pre-registered priors vs actual outcome

| Outcome | Prior | Actual |
|---|---:|:---:|
| DEPLOY-CANDIDATE         | 5%  | |
| SHADOW DEPLOY            | 20% | |
| WALK-FORWARD CANDIDATE   | 35% | |
| REJECT                   | 40% | **★** |

The 40% REJECT prior was correct. The mechanism — "low trade count
from joint filter" — was anticipated; the magnitude was even more
extreme than expected (0 trades vs the implicit assumption of a
handful per year).

## What this updates

### Locks in (high confidence)

- **F1 ∧ F2 (composite design D, deployed-16, K=5, ±60s)** is
  decisively REJECTED. This specific composite is not a viable
  filter-on-deployed at the locked thresholds.
- **F1 and F2 mechanisms are temporally disjoint** at strict
  thresholds — they don't co-occur within ±60s, even though they
  individually catch real signals.

### Does NOT update

- F1's WALK-FORWARD CANDIDATE status (its own verdict stands)
- F2's WALK-FORWARD CANDIDATE status (its own verdict stands)
- The +$130k/yr cumulative claim from A1 (Cat G doesn't touch it)
- The drift detector being the decision-grade kill mechanism

### Permits (in next milestone, with fresh pre-registration)

- F1 ∧ F2 with universe-57 co-cross denominator (matches F2's setup)
- F1 ∧ F2 with different K threshold
- F1 ∧ F2 with wider time tolerance (e.g., ±300s, ±600s)
- F1 ∨ F2 (composite design B — union)
- F1 alone gating + F2 alone gating in parallel
- Composite Cat H exploring a different operator entirely

## What's NOT permitted under the original pre-registration

- ❌ No re-running with K=3 to find a flattering composite
- ❌ No re-running with universe-57 (deployed-16 was locked)
- ❌ No widening time tolerance to ±300s
- ❌ No relaxing tier conditions ("F1 ∧ F2 with 2/6 wins"...)
- ❌ No saving REJECT by lowering F1 threshold to 20bp/day

The result stands as the mechanical output of the locked rule. The
composite at this design is falsified.

## Files

- `results/cat_g_f1xf2_composite_decision_rule_2026-05-07.md` — pre-registration
- `results/cat_g_f1xf2_composite_2026-05-08.txt` — raw output
- `scripts/cat_g_f1xf2_composite.py` — analyzer

## Status update

Cat G is logged as **REJECTED at design D, deployed-16 universe, locked
thresholds.** The candidate scoreboard has its first decisive REJECT
of a composite hypothesis.

The temporal disjointness finding is the real take-away: F1 and F2
catch genuinely distinct moments. Combining them as F1 ∧ F2 at strict
thresholds is statistically infeasible. Future composites should use
either looser thresholds or composite operators that don't require
intersection (∨, gated, sequential).
