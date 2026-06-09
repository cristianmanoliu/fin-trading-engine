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
