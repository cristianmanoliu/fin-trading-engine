# Overnight research batch — 20260609_233300

Directive: run every offline backtest-data analysis; store all results incl. no-gos.
Arbiter: overfit gate (DSR) + correlation-to-LIVE, not raw NET.

| Job | Status | Headline |
|---|---|---|
| 1. Expanded-37 gate | DONE | DSR=0.0010 PBO                         = 0.5302 | PR ≈ 1.9  indep bets |
| 2. EMA fine-grid | DONE | best: 3/15/514557 |
| 3. Multi-TF × side | DONE | best: 4H/short/295292 |
| 4. RR × max_hold | DONE | best: 8/504/371227 |
| 5. Cross-sectional L/S | DONE |  lookback  hold  rebals    mean%  ann.Sharpe      cum% BEST: lookback=14d hold=1d  ann.Sharpe=1.49  cum=10825.0%  (2267 rebalances)  |

## Interpretation

Reference: clean 34-config gate = DSR 0.658 / PBO 0.52, PR≈1.8 independent bets
(34 configs collapse to ~2 streams — see results/overfit_expansion_2026-06-09.md).
Jobs 2-4 grids are judged by whether any cell's walk-forward beats baseline AND
is low-correlation (a real diversifier). Job 5 is the one structurally-new axis.
Per the standing overfit verdict, expect grid winners to be high-correlation copies
of the live short bet (raw-NET mirages). Stored regardless — negative results are results.

## ⚠️ CORRECTION (2026-06-10) — Job 5 headline was inflated

The Job 5 "ann.Sharpe 1.49 / cum +10,825%" is OPTIMISTIC and NOT a deployable edge.
Falsification audit (results/cross_sectional_verdict_2026-06-10.md): ragged history
(only 3 symbols early → degenerate 1-vs-1 book), fantasy compounding, and 10bp/side cost
inflated it. Hardened (≥20 symbols, 35bp/side, per-period Sharpe): **Sharpe 0.67, and ALL
edge is 2020-2021 — the factor is DEAD since 2022** (every year flat-to-negative).

## Session-decisive conclusion
Both axes of the search are now tested honestly:
- Directional (Jobs 1-4): all configs >0.64 corr to LIVE, PR≈1.9, DSR 0.658→0.0010 when
  candidates added. Self-defeating.
- Cross-sectional (Job 5): decayed factor, dead since 2021.
**No live edge hides in this 5y/57-sym price data on any axis.** Only new DATA (order-book/
on-chain/cross-asset/options) or the forward-paper clock remain. Backtest search closed.
