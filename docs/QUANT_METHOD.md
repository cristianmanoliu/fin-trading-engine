# The method — what to carry to the next quant project

This project ran 3.5 months, ~85 strategy trials, an 88-day live forward-paper
run, and **zero dollars of trading capital**. It produced no deployable
strategy. The method below is the part that transfers, and it is worth more
than the strategy would have been.

Written to be read by someone starting a *different* project. Nothing here is
crypto- or perp-specific. The one file you copy is
[`scripts/quant_honesty.py`](../scripts/quant_honesty.py) (stdlib + numpy, no
project imports, `--selftest`).

---

## The order of operations — this is the whole thing

v1 ran: build engine → validate strategy → discover fees kill it → discover the
venue was region-blocked. Every step was expensive and the two that decided the
outcome came last.

**Invert it. Cheapest disqualifier first.**

| # | Step | Cost | Kills |
|---|---|---|---|
| 1 | **Venue access.** Open the account, fund it, place one $10 trade by hand. | an afternoon | "the venue does not exist for you" |
| 2 | **Screen.** `screen(gross_r, fee_bps, slip_bps, stop_pct)` — require gross ≥ 3× cost. | 30 seconds | most ideas, before any code |
| 3 | **Break-even fee curve.** Vary only the fee; find where net crosses zero. | an afternoon | anything needing fees below your venue's |
| 4 | **Honest backtest.** `honesty()` — median, drop-top-5%, by-year. | a day | tail-mirages |
| 5 | **Overfit gate.** PBO / DSR across the whole search. | a day | the search itself |
| 6 | Only now: write code. | months | — |

Steps 1–5 cost under a week combined. Step 6 cost 3.5 months. **v1 did 6 first.**

The single sharpest version of this: *forward paper is for validating execution,
not for discovering arithmetic.* 88 days of live running produced a result that
step 3 would have predicted in an afternoon.

---

## The two checks that actually killed things

### 1. Cost is deterministic; gross is a random variable

```
cost_R = (fee_rt_bps + slip_bps) × L × 1e-4,   L = 1 / stop_pct
```

Risk-based sizing forces leverage `L`. Fees are charged on **notional, twice** —
not on your stake. So a $1,000 risk against a 1.5% stop pays fees on ~$67,000 of
exposure, round-trip.

Measured here: cost **0.0952 R** known to four decimals; gross **+0.0296 R** with
a 95% CI of **[−0.41, +0.47]** — an interval *15× wider than the cost it must
beat*. The certainty-equivalent of (uncertain gross − certain cost) was negative
regardless of the point estimate.

**Require gross ≥ 3× cost.** Not 1×. Backtest gross historically arrives at
~1/10 of its modeled value; cost arrives at 1.0×. Applied retroactively this one
line rejects every candidate this project ever ran.

### 2. Aggregate P&L hides tail dependence

Mean, total net, Sharpe, and by-year positivity **all survive** a book whose
entire edge is 5% of trades. Median and drop-top-5% do not.

Four candidates here passed on mean and t-statistic, then died on this check:
settlement drift (#11), listing drift (#12), MVRV (#20), and the final regime
gate (C4 — mean +20.19 bp, median +2.61 bp, **drop-top-5% −23.83 bp**).

`honesty()` flags this automatically as `tail_carried`.

**Corollary:** by-year positivity is *not* sufficient. C4 was positive in 4/6
years — with 2026 alone carrying +263 bp against negatives elsewhere.

---

## Pre-registration — the discipline, minimally

Not bureaucracy. It is the only reason a negative result here is trustworthy
rather than a rationalization.

1. **Write the hypothesis, thresholds, and accept/reject criteria in a file.
   Commit it. Then run the study.** Separate commits, in that order — so the
   thresholds provably could not move to meet the result.
2. **Declare the trial budget.** One hypothesis, one run. "It's promising, let's
   try one more cell" is how N reaches 85 and DSR reaches 0.001.
3. **Pre-register the expected failure mode.** If you can name in advance how it
   will probably fail and it fails that way, you learned something. C4's pre-reg
   predicted *"passes on t-stats, fails on drop-top-5%"* — which is exactly what
   happened.
4. **No partial credit.** Any single criterion failing is a reject. Otherwise
   every result becomes "promising, needs follow-up."
5. **Fix every free parameter before looking.** The first pass at the maker
   question produced a **+$14.7k "improvement" that was purely an artifact of
   choosing the fill window after seeing the P&L** — a $23k swing across
   5/15/30/60m windows.

---

## Traps, each paid for once

- **A universe-wide gain does not transfer to a selected book.** A parameter was
  significant across 57 symbols (t=+3.83, better on 36/57) and **negative on the
  16 deployed**, because those 16 were *selected under the old parameter*.
  `corr(baseline, improvement) = −0.523` — it helps where the old parameter did
  worst. Evaluate changes on the book you actually trade.

- **A saving conditional on execution is not a saving.** Maker entry saved 3 bp
  on a strategy needing 2. Uncollectable: the saving requires being filled, and
  a resting short limit fails to fill *exactly when price gaps down* — i.e. on
  winners. Winner fill 71.7% vs loser 78.8%, **−7.1pp, z=−3.74**, n=2,882. Price
  the selection, not just the fee.

- **Running N variants and picking the best is selection, not discovery.** 8
  shadows on one tick stream spanned +$78k to +$2k. The best looks compelling;
  it is the max of 8 draws from a mean-zero distribution. Pre-register the
  variant count and correct for multiplicity, or run one.

- **Correlated candidates don't add bets.** 37 configs collapsed to a
  participation ratio of **1.9** — about *two* independent bets. Each extra cell
  raised the multiple-testing haircut, so DSR fell from 0.658 to **0.001**. More
  searching made the answer worse, not better.

- **Exit labels lie if inferred.** 7 of 24 "winners" carried `outcome: TARGET`
  on max-hold force-closes that never touched target. P&L was unaffected; every
  win-rate-by-outcome analysis was subtly wrong for months. Write the exit
  reason from the code path that performs the exit. One enum, one writer, one
  test.

- **Look-ahead hides in the window edge.** A trailing z-score must exclude the
  current observation (`range(max(0, i-W), i)`, exclusive upper bound). A
  left-edge-inclusive window silently leaks the present into its own baseline.

---

## What "closed" should mean

A branch is closed when a **pre-registered verdict** says so — not when it feels
exhausted. This matters because the feeling is unreliable: this repo was
declared exhausted while C4 sat flagged-and-untested in a synthesis doc, and a
weekly drift job was still firing against a destroyed server three days after
the project "closed."

**Check running state, not documentation.** `launchctl list`, `crontab -l`, CI
schedules. Prose warnings do not disarm executing schedulers.

---

## Cross-references

- `scripts/quant_honesty.py` — the portable module (`--selftest`)
- `scripts/backtest_overfit_analysis.py` — PBO (CSCV) + Deflated Sharpe, tested
- `results/viability_frontier_2026-07-27.md` — the law, with measured constants
- `results/v2_lessons_and_design_2026-08-04.md` — L1–L8, the long form
- `results/btceth_regime_subbook_prereg_2026-08-07.md` + its verdict — a
  complete worked example of the pre-registration cycle, start to finish
