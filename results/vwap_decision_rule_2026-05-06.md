# Pre-registered decision rule for VWAP-fade strategy validation
**Written: 2026-05-06 EOS, BEFORE running any 6-window VWAP tests.**
**Purpose: prevent retroactive goalpost-moving + apply consistent discipline.**

## What we're testing

VWAP deviation fade as a STANDALONE strategy candidate. Mean-reversion thesis:
when 4H close stretches ≥ X% from session VWAP, fade back to VWAP.

Configurations tested:

| Config | VWAP threshold | Side | TF | Max-hold |
|---|---|---|---|---|
| V05 | 0.5% | both | 4H | 504h |
| V10 | 1.0% | both | 4H | 504h |
| V15 | 1.5% | both | 4H | 504h |
| V20 | 2.0% | both | 4H | 504h |
| V30 | 3.0% | both | 4H | 504h |

All on universe-57, slip=15bp, fee=10bp, funding=CSV (post-fix).
Both-sides because mean-reversion is naturally bidirectional (long on under-VWAP, short on over-VWAP).

## Windows (6 non-overlapping 12-month each)

Same as the 6-window validation suite earlier today (W-2 through W3).

## Decision rules — applied LITERALLY after results land

### Stage 1 — Standalone viability

For each of V05/V10/V15/V20/V30, check standalone:

| Outcome | Verdict | Action |
|---|---|---|
| Positive in **≥5/6 windows** AND mean NET > 0 | **STRONG** | Real strategy candidate; check Stage 2 |
| Positive in **4/6 windows** AND mean > 0 | **WEAK** | Possible candidate; check Stage 2 |
| Positive in **3/6** OR mean ≤ 0 | **Inconclusive** | Reject this threshold |
| Positive in **≤2/6** | **REJECTED** | Reject this threshold |

If ALL 5 thresholds are REJECTED/Inconclusive → VWAP-fade as a standalone strategy is REFUTED. Document and stop.

### Stage 2 — Diversification check (only for STRONG/WEAK candidates)

Compute correlation between the candidate's per-window NET and the deployed
EMA-short strategy's per-window NET (using the existing 6-window result).

| Correlation | Verdict | Action |
|---|---|---|
| r < +0.30 | **DIVERSIFIER** | Deploy as shadow strategy alongside live |
| 0.30 ≤ r < 0.60 | **PARTIAL** | Borderline; document, don't deploy |
| r ≥ +0.60 | **REDUNDANT** | Same risk profile as deployed; don't deploy |

A negatively-correlated candidate (r < 0) would be the strongest possible
diversifier. Deploy as shadow regardless of standalone Stage 1 strength.

## What I commit NOT to do regardless of results

1. Will NOT test additional VWAP thresholds after seeing results
2. Will NOT redefine "win" to mean something other than per-window NET > 0
3. Will NOT switch to log-returns or Sharpe to break ties retroactively
4. Will NOT test on different timeframes (1H, 1D) AFTER seeing 4H results
5. Will deploy shadow only on the rule above — no "well it ALMOST cleared"

## Pre-registered hypothesis

VWAP fade has an attractive structure (high WR, tight target) but I expect
it to STRUGGLE on 4H — the session VWAP drifts considerably over 4 hours,
making the "mean reversion to VWAP" target less meaningful than at intraday
TFs. Prior: most thresholds REJECTED, possibly one (V20 or V30) WEAK.

If a candidate clears Stage 1 STRONG AND Stage 2 DIVERSIFIER → that's a
significant finding (real diversifier in crypto perp shorts). Update strongly.
