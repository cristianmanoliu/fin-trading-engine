# Crash overlay as a hedge for a long S&P 500 book: pre-registration (2026-11-15)

**Status: LOCKED at commit. Never edit. A change needs a dated `_v2` file.**

## Question

The crash overlay is closed as a standalone book (`kraken_book_screen_2026-11-15.md`,
Result 3). The 2026-08-09 addendum (§5) left one open question: is it worth
holding as a hedge next to a long book? A hedge may cost money. It must still
beat the free alternative: **hold less stock.**

Finance fact used here: adding X to a book P raises the Sharpe ratio of the
book only if the alpha of X against P is positive (alpha = mean of X minus
beta times mean of P). So H1 tests alpha, and H2 tests the free alternative
directly.

## What was seen before this plan

- The yearly overlay P&L at real costs (Result 3). Public yearly SPY returns.
- NOT seen: monthly overlay P&L, any overlay and SPY regression, correlation,
  or drawdown path. SPY data was checked for coverage only (2014-01 to 2026-08).

## Data

- **Overlay:** the journals of the 2026-11-15 run (pre-reg config, 13 symbols,
  $500 stake, 2020-01 to 2026-07, 3,174 trades), re-priced by the logic of
  `scripts/crash_overlay_reprice.py`. Primary: **$250 stake at 8bp** book cost
  (P&L x 0.5, entry slip 8bp, stop and time-stop slip 8bp). P&L goes to the
  month (UTC) of the close.
- **Stock book:** SPY `adjusted_close` (dividends in) from
  `fin-equity-lab/data/etf/eod/SPY.parquet`, month-end to month-end returns.
  **$200k** constant-dollar exposure, reset each month. Currency: USD.
- **Window:** 2020-01 to 2026-07, all months where both exist.
- **Cash earns 0%.** This favors the overlay, because the H2 benchmark holds
  cash. A PASS must survive a re-check with T-bill rates before any next step.

## Gates

Monthly dollar series: `S = 200k x r_spy`, `X = overlay`, `C = S + X`.

- **H1 (alpha):** OLS `X = a + b x r_spy + e`. Newey-West t-stat, 3 lags.
  PASS if `a > 0` and `t(a) >= 2.0`.
- **H2 (beats holding less stock):**
  - If `sd(C) > sd(S)`, the overlay adds risk. H2 FAILS.
  - Else build `B = w x S`, with `w = sd(C) / sd(S)` (the rest in cash at 0%).
  - PASS if `sum(C) > sum(B)` and `maxDD(C) <= maxDD(B)`. maxDD is on the
    cumulative dollar path from a $200k start.
- **Verdict:** PASS only if H1 and H2 both pass. Anything else is FAIL.
- **No verdict:** fewer than 72 joint months.

## What each result means

- **PASS:** the only next step is a written plan for a shadow run in
  `fin-equity-lab`, after the T-bill re-check and the user's yes. No live step.
- **FAIL:** the crash overlay is closed as a hedge too. The project has no
  open question left.

## Report only (not gates)

beta and correlation; overlay P&L in the 10 worst SPY months; the quarterly
version; the $500 at 9.5bp version. The live gap stays unmodeled: live gross
was 27% of backtest gross (addendum §3).

## Expected outcome (stated before the run)

**FAIL.** H1: the overlay mean at $250 is about $3k a year, against a monthly
noise of thousands of dollars. Expected t(a) is well under 2, even if beta is
negative. H2: no firm guess.
