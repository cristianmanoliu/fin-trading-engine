# Option B (wider-stop variant) — **ALREADY ANSWERED, NOT RE-RUN**

`results/v2_lessons_and_design_2026-08-04.md` §3 proposed Option B: *"A 3.5%
stop instead of 1.75% halves fee burden… run it against the 5y data at 10bp
with drop-top-5% honesty reporting."*

**This work was already done, 9 days before the design doc proposed it, across
three pre-registered verdicts with a held-out test split. It is not re-run
here.** Re-running would spend DSR budget to re-derive a settled answer, and
the doc's own caveat — *"pre-register it as a single hypothesis… or it becomes
cell 86 of an overfit sweep"* — argues against paying that cost twice.

The design doc failed to cross-reference these. That is the error being
corrected.

---

## The existing verdicts

| Doc | Arm | Verdict |
|---|---|---|
| `atr_stop_cost_geometry_verdict_2026-07-26.md` | `--atr-stop-mult 1.5` | **PARTIAL** — mechanism real, edge not established (p=0.32, median symbol −$808, top-3 = 79% of gain) |
| `atr1_0_holdout_verdict_2026-07-27.md` | `--atr-stop-mult 1.0` | **NOT ACTIONABLE** — universe-wide significant (p<0.001) but **negative on the deployed-16 in both periods** |
| `deployed16_direct_sweep_verdict_2026-07-27.md` | 15 arms | **LIVE CONFIG NOT BEATEN**; "the ATR direction is closed for this book" |

The mechanism is `--atr-stop-mult M`: stop = `entry ± ATR(14) × M`, replacing
the wick stop. Its own code comment states the Option B rationale verbatim:
*"Wider ATR-scaled stops reduce implicit leverage and per-trade fees on
tight-stop signals."*

## What was established

**1. The fee mechanism is real and is arithmetic, not a statistical claim.**
Fees fell ~65% on **57/57 symbols in both train and test**. It follows
deterministically from `notional = stake / stop_width`. Option B's premise is
correct.

**2. It does not rescue the book.** Applying only the mechanical fee cut to the
live trades moved breakeven coverage from **0.31× to 0.86×** — a large, real
improvement that still lands short. Wider stops make the strategy *nearly*
break-even, not profitable.

**3. The decisive failure is a selection interaction.** atr1.0 was universe-wide
significant (+$335,855, t=+3.83, p<0.001, beats baseline on 36/57) yet
**negative on the deployed-16 in both independent periods** (TRAIN −$26,050,
TEST −$23,808; 10 of 16 symbols worse).

`corr(baseline NET, atr1.0 improvement) = −0.523`. ATR stops help the symbols
the wick stop handles *worst* and hurt those it handles *best*. The deployed-16
were selected by walk-forward **under the wick-stop config**, so the universe
gain is largely regression toward the mean — and on a book already selected for
wick-stop performance, the same mechanism runs in reverse.

> *A universe-wide parameter improvement cannot be assumed to transfer to a book
> that was itself selected under the old parameter.*

That is the generalizable lesson, and it belongs in the v2 rules.

## Consequence for v2

Option B is **closed**. Combined with Option A's NO-GO
(`maker_execution_verdict_2026-08-05.md`), both authorized v2 routes for this
strategy class are now resolved negative:

- **Option A** — maker entry: adverse selection, −6.3 to −7.1pp winner-vs-loser
  fill gap at every window. Cannot collect the 3bp.
- **Option B** — wider stops: fee mechanism real (~65% cut) but insufficient
  (0.86× breakeven) and negative on the deployed book.

Note they fail for *independent* reasons and do not compose into a rescue: A
fails on fills, B fails on transfer. Stacking a route that cannot collect its
saving onto one that lands short of breakeven does not reach breakeven.

**Both v2 options for the perp EMA class are exhausted.** What remains untested
is a cheaper *taker* venue — the §1 break-even arithmetic (8bp needed, 10bp
paid) still flips the sign with no adverse selection, because a market order
always fills. That is a venue-access question, not a strategy question, and
venue access is blocked (`project_binance_futures_region_block`).

## Correction to the v2 design doc

`v2_lessons_and_design_2026-08-04.md` §3 Option B should be read as superseded
by this file. Its factual claims were right (cost is inversely proportional to
stop distance; the quartile data shows wide stops were not systematically
worse) — but the conclusion "this is a backtest question requiring zero
infrastructure" overlooked that the backtest had already been run to a
held-out, deployed-book-specific negative result.

**Process lesson for v2:** before proposing a research direction, grep
`results/` for it. The catalog is 58+ documents; the answer was three files
away.
