# Sample-Split Bootstrap Cross-Validation — VERDICT 2026-05-07

**Pre-registered:** `results/sample_split_bootstrap_decision_rule_2026-05-07.md`
(committed `2a63922` — before any backtest ran).

**Verdict tier (mechanical application of the locked rule):** **ROBUST_BOTH**

## Result vs locked decision rule

Per-half pass condition: `lo95 > $0 AND P(>$0) ≥ 95%`.

| Half | Range | N | Annual point | Bootstrap 95% CI | P(>$0) | lo95>$0? | Verdict |
|:---:|:---|---:|---:|:---|---:|:---:|:---:|
| **A** | 2020-01-20 → 2022-10-31 | 918 | +$120,917/yr | [+$22,071, +$227,573]/yr | 99.0% | ✓ | **PASS** |
| **B** | 2022-11-01 → 2025-04-30 | 1,292 | +$140,775/yr | [+$9,194, +$271,536]/yr | 98.2% | ✓ | **PASS** |

Both halves independently pass → **ROBUST_BOTH**. The +$130k/yr cumulative claim
is regime-independent at this split.

Sanity check: full-sample stats from this run (2,210 trades, 5.28y,
+$687,072 total, +$130k/yr point) match A1's `bootstrap_ci_verdict_2026-05-07.md`
exactly — same data, same loader, no drift.

## Findings

### 1. Both halves independently support the deploy claim

Half A (covering 2020 sideways → 2021 bull → 2022 bear top → bottom run-in)
delivered +$121k/yr at 99.0% P(>$0). Half B (covering 2022 bottom →
2023 recovery → 2024 bull → 2025 plateau) delivered +$141k/yr at 98.2% P(>$0).

The cumulative +$130k/yr is NOT a regime-averaging artifact. Each half on its
own would have cleared the locked deploy bar.

### 2. Annual point estimates are remarkably consistent

|  | Half A | Half B | Δ |
|---|---:|---:|---:|
| Annual point | +$121k/yr | +$141k/yr | +$20k/yr |
| Bootstrap median | +$119k/yr | +$141k/yr | +$22k/yr |

A 16% improvement in Half B is plausibly noise, plausibly real (more symbols
on Binance, deeper liquidity, more EMA crosses) — but the magnitudes are far
closer than a regime-fragility scenario would produce.

### 3. Half B has wider CI despite more trades — a yellow flag worth noting

Bootstrap half-widths:
- Half A: $103k/yr (N=918, L=30)
- Half B: $131k/yr (N=1,292, L=36)

Counterintuitively, the larger sample has the wider CI. This means **per-trade
variance and/or autocorrelation is higher in Half B**. Consistent with the
walk-forward observation that quarter-level regime variance is real.

The lo95 bound for Half B is **+$9,194/yr** — barely positive. The pass
condition (`lo95 > $0`) is met, but the margin is slim. This means:

- The verdict is correctly ROBUST_BOTH per the locked rule
- But the recent regime is closer to the regime-fragile cliff than the early
  one
- A 1.8% chance (per Half B's bootstrap) of zero-or-negative annualized NET
  is non-trivial for forward-paper deploy purposes

### 4. Walk-forward CI is still the more conservative anchor

| Estimator | 95% CI | half-width |
|:---|:---|---:|
| Bootstrap (full 5y, A1)         | [+$42k, +$220k]/yr  | $89k |
| Bootstrap (Half A)              | [+$22k, +$228k]/yr  | $103k |
| Bootstrap (Half B)              | [+$9k, +$272k]/yr   | $131k |
| Walk-forward (6 quarters, CLAUDE.md) | [-$111k, +$372k]/yr | $241k |

The walk-forward CI remains 2-3× wider than any bootstrap fold. This is
**unchanged from A1's finding**: trade-level resampling cannot capture the
quarter-correlated regime variance that walk-forward measures directly.

The sample-split test does NOT challenge that finding. It confirms that the
*cumulative* claim doesn't depend on regime aggregation — but the *uncertainty
estimate* should still come from walk-forward, not from any bootstrap.

## Pre-registered priors vs actual outcome

| Outcome | Prior | Actual |
|---|---:|:---:|
| ROBUST_BOTH | 60% | **★** |
| PARTIAL_REGIME_DEP | 30% | |
| FRAGILE | 10% | |

The 60% prior captured the actual outcome. No major prior update needed.

The narrow margin on Half B's lo95 (+$9k) is a small directional update
toward "the recent regime is the borderline one" — but not enough to flip
the verdict.

## What this updates

- **Anchor for forward-paper expectations: unchanged.** Walk-forward CI
  remains the conservative read; bootstrap CI remains tighter but
  cumulative-only.
- **Regime-dependence concern from A1 + walk-forward: partially resolved.**
  The cumulative claim is robust to a calendar-midpoint split. Quarter-level
  variance is still real (per A1 + walk-forward), but it doesn't tip the
  per-half claim into negative territory.
- **Half B borderline lo95 noted in the kill-bar context.** When forward-paper
  data starts coming in and we anchor against the kill criteria, "Half-B-style
  underperformance" (lo95 ≈ +$9k/yr) is a plausible non-pathological
  outcome that should not be confused with strategy decay.

## What's NOT permitted (locked at pre-registration)

- ❌ No re-running with a different split point ("2022-09-15 looks better")
- ❌ No re-running with different bootstrap L
- ❌ No re-running with different B
- ❌ No combining halves into a re-weighted aggregate
- ❌ No reframing the threshold post-hoc

The result stands as the mechanical output of the locked rule. The locked
rule does not anticipate further data-look on this question — the result is
what it is.

## Files

- `results/sample_split_bootstrap_decision_rule_2026-05-07.md` — pre-registration
- `results/sample_split_bootstrap_2026-05-07.txt` — raw output
- `scripts/sample_split_bootstrap.py` — analyzer (imports from
  `scripts/bootstrap_ci.py` for math; mirrors A1 setup)
- `results/bootstrap_ci_verdict_2026-05-07.md` — the A1 claim being validated

## Status update

A1's claim survives the sample-split robustness test. No re-anchoring of
deploy expectations needed. The walk-forward CI remains the load-bearing
anchor for forward-paper go/no-go.
