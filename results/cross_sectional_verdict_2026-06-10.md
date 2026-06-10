# Cross-sectional long-short — hardened verdict (2026-06-10)

**Status:** Falsification audit of the overnight batch's one positive lead. The raw Job 5
result (ann.Sharpe 1.49, cum +10,825%) was the only non-mirage candidate all session — so it
got the hardest scrutiny. **Outcome: a decayed factor, dead since 2021. Not deployable.**

## The raw result (overnight batch Job 5)

`scripts/cross_sectional_ls.py`, dollar-neutral: each day rank 57 perps by trailing return,
long top 20% / short bottom 20%, equal dollars. Grid of lookback × hold:

| lookback | hold | rebals | mean%/reb | ann.Sharpe | cum% |
|---:|---:|---:|---:|---:|---:|
| 14 | 1 | 2267 | 0.262 | **1.49** | 10,825 |
| 14 | 3 | 755 | 0.749 | 1.41 | 7,794 |
| 7 | 3 | 758 | 0.691 | 1.22 | 3,704 |
| 30 | 1 | 2251 | 0.217 | 1.23 | 3,658 |

Consistently positive across all 9 cells (Sharpe 0.63–1.49) — which ruled out a single
cherry-picked cell and made it worth a real audit. Temporal logic verified clean (rank on
`[i-lb, i]`, trade `[i, i+hold]` — no look-ahead).

## Three confounds, all real, that inflate the raw number

1. **Ragged history / degenerate early universe.** At the earliest day only **3 symbols
   exist**; <16 until ~day 200. Top/bottom-20% of 3-6 symbols = a 1-vs-1 bet, not a
   cross-sectional book. The huge early compounding rides this near-degenerate period.
2. **Fantasy compounding.** cum +10,825% assumes full reinvestment of a dollar-neutral book
   with no capital constraint, no borrow cost on the short leg, no slippage on illiquid alts.
   Meaningless as a return; only the per-rebalance mean + Sharpe are interpretable.
3. **Optimistic cost.** 10bp/side on a DAILY-rebalanced 57-alt momentum book is far too low —
   the alt short leg realistically carries borrow + slippage well above that.

## Hardened re-run — the edge decays to death after 2021

Min-universe gate (≥20 symbols, skips 234 degenerate days) + realistic 35bp/side + per-period
Sharpe (no compounding fantasy), best cell (lb14/hold1):

- **ann.Sharpe 1.49 → 0.67** (halved by cost + degenerate-day removal).
- **Per-year mean return / rebalance:**

| Year | mean%/reb | n |
|---|---:|---:|
| 2020 | +0.099 | 118 |
| **2021** | **+0.773** | 365 |
| 2022 | −0.015 | 366 |
| 2023 | −0.116 | 365 |
| 2024 | +0.030 | 365 |
| 2025 | −0.032 | 365 |
| 2026 | −0.036 | 119 |

**ALL the edge is 2020-2021.** Every year since 2022 is flat-to-negative. Cross-sectional
momentum was a real, large factor in the early wild-west crypto era and has been **dead for
4 years** — the entire live-relevant window. (And 35bp is still optimistic for daily alt
rebalancing; the true post-2021 number is likely worse.)

## Verdict

**Not deployable. A decayed factor.** Same shape as everything else this session — profitable
in one regime/era, dead otherwise — just a *different* era (the 2020-21 alt bull) than the
directional class's 2022-crash dependence. It is uncorrelated with the live short bet (that
part of the thesis held), but uncorrelated-and-dead is not edge.

## What this closes — the decisive session conclusion

We have now tested both axes of the search space honestly:
- **Directional** (33 configs + today's 3 new): all >0.64 correlated with LIVE, PR≈1.9
  (≈2 independent bets), adding candidates collapses DSR 0.658→0.0010. Self-defeating.
- **Cross-sectional** (the one structurally-different axis): a decayed factor, dead since 2021.

**There is no live edge hiding in this 5y/57-symbol data on any axis we can construct from
price.** The only paths left are genuinely new DATA (order-book microstructure, on-chain,
cross-asset, options skew) — a from-scratch acquisition+infra build, not an extension of this
engine — or the forward-paper clock on the deployed config. Backtest search is closed, now
proven on both axes. Forward-paper is the arbiter.

## Artifacts
- `scripts/cross_sectional_ls.py` — raw screen (overnight Job 5)
- `results/overnight_20260609_233300/job5_cross_sectional.txt` — raw output
- Hardened re-run inline above (min-universe + realistic cost + by-year decomposition)
