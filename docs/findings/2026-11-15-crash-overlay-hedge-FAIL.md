# Crash overlay as a hedge for a long SPY book: FAIL (2026-11-15)

Rule: `results/crash_overlay_hedge_prereg_2026-11-15.md`, locked at 126f524
before the run. Script: `scripts/crash_overlay_hedge_test.py`. Expected outcome
in the pre-reg: FAIL. Result: **FAIL on both gates.**

## Primary ($250 stake, 8bp book cost, $200k SPY book, 79 months 2020-01 to 2026-07)

| | value | gate | result |
|---|---:|---|:--:|
| overlay mean | $3,006 a year | | |
| alpha vs SPY | $10,287 a year, NW t = 1.22 | t >= 2.0 | **FAIL** |
| beta | -$463 per 1% SPY move, t = -3.07 | report only | |
| correlation | -0.33 | report only | |
| sd of book alone / with overlay | $9,839 / $9,991 a month | with <= alone | **FAIL** (adds risk) |

Report only:
- 10 worst SPY months: book -$145,923, overlay +$21,793. It paid in 6 of 10.
- Quarterly (n = 27): alpha t = 1.15, beta t = -2.23.
- $500 at 9.5bp: alpha t = 1.03. sd with overlay $14,090 vs $9,839. FAIL.

## Reading

- **The hedge effect is real in the backtest.** Beta is negative with t = -3.1,
  and the overlay paid in most bad SPY months.
- **Its own noise is too big.** The overlay sd at $250 ($6.9k a month) is larger
  than what the negative correlation removes. So it adds risk to the book.
  Holding less SPY lowers risk at no cost, and the overlay does not.
- **The alpha cannot be told from zero.** Most of the $10k a year alpha is the
  negative beta times the SPY gain of 2020 to 2026. t = 1.22 does not pass.
  The alpha t-stat does not depend on the stake, so a smaller stake does not
  fix H1. (A stake under about $230 would lower the risk a little. That is a
  fact of the formula, not a re-test, and it does not change the verdict.)
- The live gap of the addendum stays: live gross was 27% of backtest gross.

## Disposition

The crash overlay is closed as a hedge too. **No open question is left in this
project.**
