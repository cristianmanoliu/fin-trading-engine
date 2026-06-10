# Candidate #8 — Cross-sectional CARRY (dollar-neutral, rank by funding) — VERDICT: MARGINAL → NO-GO standalone (2026-06-10)

**Type:** verdict (pre-registered design applied to data → mechanical reject as standalone).
**Candidate:** #8 of the 20-candidate orthogonal search (`results/strategy_candidates_2026-06-10.md`).
**Script:** `scripts/cross_sectional_carry.py` (reuses `cross_sectional_ls.py` panel/sharpe machinery).
**Data:** 100% local — 57-symbol 1m klines + funding CSVs, 2020–2026. Zero fetch.

## The bet

Rank symbols by trailing N-day mean funding. LONG the most-negative-funding (collect funding holding long), SHORT the most-positive-funding (collect funding holding short). Dollar-neutral. PnL = price spread of the L/S book + net funding collected. Structurally distinct from momentum (`cross_sectional_ls.py`, ranks by trailing return) and from the live directional short.

## Hardened-audit lens (mirrors the momentum verdict)

Min-universe ≥20 ranked symbols (skips degenerate ragged-history days), 35 bp/side turnover cost, funding accrued from actual settled rates over the hold, by-year decomposition, no compounding fantasy. Pre-registered ACCEPT bar: best-cell ann.Sharpe > 1.0 AND positive in all readable years.

## Result

| lb | hold | rebals | mean%/reb | ann.Sharpe |
|---|---|---|---|---|
| 7 | 1 | 2077 | 0.0113 | 0.17 |
| 7 | 3 | 692 | 0.1530 | 0.76 |
| 7 | 7 | 296 | 0.0549 | 0.09 |
| 14 | 1 | 2077 | 0.0273 | 0.44 |
| 14 | 3 | 691 | 0.0954 | 0.48 |
| **14** | **7** | **296** | **0.4736** | **0.79** |
| 30 | 1 | 2077 | 0.0247 | 0.38 |
| 30 | 3 | 692 | 0.0732 | 0.35 |
| 30 | 7 | 295 | 0.0582 | 0.09 |

**Best cell (lb14/hold7): ann.Sharpe 0.79, positive in 5/7 years** — and crucially NOT a pre-2022-only mirage (2021 +1.60, 2022 +1.23, 2025 +1.62, 2026 +1.61; only 2020 negative and 2023 flat). This is more alive in the live-relevant window than momentum was (momentum: dead after 2021, Sharpe 0.67).

### Selection-bias correction — the decisive check

The 0.79 is the best of 9 grid cells. The honest factor strength is the *cell distribution*, not the max:

- All 9 cells positive (carry signal is coherent, not noise — a real effect).
- **But mean Sharpe 0.39, median 0.38; only 2/9 cells clear 0.5.** The 0.79 is the lucky tail of the grid.

So the selection-corrected strength of "the funding-carry factor" is ~0.4 Sharpe, not 0.79.

## Verdict: MARGINAL — NO-GO as a standalone deployable.

A coherent but weak (~0.4-Sharpe) factor, already hammered by 35 bp/side. The spec flagged the additional alt-short **borrow** cost (not modeled here) which would push it lower still. It does not clear the pre-registered Sharpe>1 bar, and at the selection-corrected ~0.4 it is exactly the "positive but tiny" outcome the carry family was predicted to produce. Building the two-leg delta-neutral harness for a sub-0.5-Sharpe gross factor is not justified on this result alone.

**Not fully dead, though:** unlike momentum it does NOT collapse to a pre-2022 artifact. The one path that could still justify it is as an *uncorrelated diversifier* — but the gate-3 corr-to-LIVE check is only worth running if the carry family produces something with more headroom. Per `tasks/todo.md`, the carry-harness build decision is deferred until **#13 (cross-venue funding spread)** is tested — #13 is a true-arb class (price risk cancels), cheaper to test (pure CSV math, one fetch), and is the better carry bet. Consolidate the carry-family verdict (#3/#8/#13) before any harness build.

## Gates not reached

Walk-forward (1), overfit-matrix (2), corr-to-LIVE (3) **not run** — a selection-corrected ~0.4-Sharpe gross factor doesn't merit consuming those DoF standalone. Revisit only if #13 changes the carry-family picture.

## Cross-references

- Spec: `results/strategy_candidates_2026-06-10.md` (#8), batch-2 `#13`
- Momentum sibling verdict: `results/cross_sectional_verdict_2026-06-10.md` (Sharpe 0.67, dead post-2021)
- Plan: `tasks/todo.md` Phase 0 → carry-harness decision deferred to post-#13
- Carry family: #3 (delta-neutral harvest), #8 (this), #13 (cross-venue spread) — intra-family correlation check required if >1 survives
