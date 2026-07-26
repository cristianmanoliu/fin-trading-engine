# ATR Stop Sizing — Cost-Geometry Arc — VERDICT: PARTIAL (mechanism real, edge not established)

**Status:** Overnight research arc, 2026-07-26. Train/test split enforced *before* any
result was viewed. **This is a research-only finding. It changes nothing live, and it does
NOT constitute a promotion candidate.**
**Author:** Claude (session-generated), for operator review 2026-07-27
**Harness:** `scripts/cost_geometry_sweep.sh` (commit `5bf21c7`)
**Method:** continuous per-symbol 5y backtest (concatenated monthly CSVs, no month
segmentation), 57-symbol universe, `--fee-bps 10 --stop-slippage-bps 5`, historical funding,
`--exact-fills --include-boundary --pessimistic-ambiguous`.
**Split:** TRAIN 2020-01 → 2023-12 (selection). TEST 2024-01 → 2025-04 (**held out; opened
once, after parameters were fixed**).

## Why this and not a signal search

The prior session established (`stop_distance_cost_filter_verdict_2026-07-26.md`) that live
forward-paper is **gross-positive (+$3,489)** and dies to **$11,238 of costs** at **67× average
leverage**. Position size is `stake / |entry − stop|`, so a tight wick stop mechanically forces
huge notional, and fees are charged on notional.

That makes stop *construction* — not the signal — the lever with a real mechanism behind it.
Searching for new signals was explicitly declined: at N≈39 trials already spent, DSR requires
~1.8–2.1 annualized Sharpe to clear, and nothing in 125 result docs exceeds 0.79.

## Variants tested (TRAIN)

`--atr-stop-mult M` replaces the wick stop with `entry ± ATR(14) × M`.

| Variant | Trades | WR | Gross | Fees | NET | NET/trade |
|---|---:|---:|---:|---:|---:|---:|
| baseline (wick) | 5,014 | 18.7% | $783,785 | $270,278 | **$463,687** | $92.5 |
| **atr1.5** | 3,991 | 21.7% | $570,655 | $90,580 | **$518,167** | **$129.8** |
| atr2.5 | 3,129 | 30.3% | $138,791 | $41,915 | $131,460 | $42.0 |
| atr2.5 + trail | 3,892 | 28.1% | $141,388 | $53,524 | $106,744 | $27.4 |
| atr3.5 | 2,765 | 36.2% | −$32,109 | $26,191 | −$26,504 | −$9.6 |
| atr5.0 | 2,458 | 42.1% | −$91,960 | $16,274 | −$79,910 | −$32.5 |

Non-monotone: widening past 1.5× destroys gross faster than it saves fees. **Only atr1.5 was
carried to TEST** (plus atr2.5 as a control).

## Held-out TEST (2024-01 → 2025-04)

| Variant | Trades | WR | Gross | Fees | NET | NET/trade |
|---|---:|---:|---:|---:|---:|---:|
| baseline | 2,433 | 21.7% | $945,756 | $129,957 | $794,069 | $326.4 |
| **atr1.5** | 1,948 | 26.4% | $909,648 | $46,646 | **$887,278** | **$455.5** |
| atr2.5 | 1,474 | 36.3% | $519,267 | $21,075 | $520,724 | $353.3 |

**atr1.5 beat baseline out-of-sample by +$93,209** (NET/trade +40%). The direction found on
TRAIN replicated on TEST.

## Decomposition — what is real vs. what is noise

This is the core result. The aggregate win splits cleanly into a **mechanical** component and
a **statistical-noise** component.

| Component | TRAIN | TEST | Verdict |
|---|---|---|---|
| **Fee reduction** | −66%, lower on **57/57** symbols | −64%, lower on **57/57** symbols | **MECHANICAL — universal, replicates exactly** |
| **Gross change** | −27%, higher on 27/57 (47%) | −4%, higher on 26/57 (46%) | **COIN FLIP — no edge effect** |

Significance of the aggregate NET improvement on TEST:

| Test | Value |
|---|---:|
| Paired per-symbol delta, mean | +$1,635 |
| Paired per-symbol delta, sd | $12,369 |
| **Paired t** | **0.998** |
| **Sign-flip permutation p** | **0.3197** |
| Median symbol delta | **−$808** |
| Top-1 symbol share of delta | 28% |
| Top-3 symbol share of delta | 79% |
| Delta excluding top-3 winners | +$19,992 |

**The +$93k is NOT statistically significant (p = 0.32).** The median symbol gets slightly
*worse*. Three symbols carry 79% of the gain.

### Correct reading

- **The fee cut is real and is not a statistical claim at all.** 57/57 symbols in both
  periods, ~65% reduction. It follows deterministically from `notional = stake / stop_width`.
  This does not need a p-value; it is arithmetic.
- **The claim "ATR stops improve the edge" is NOT supported.** Gross is a coin flip in both
  periods. The higher win rate (18.7% → 21.7% → 26.4%) is the expected consequence of wider
  stops, not evidence of better signal.
- **Net effect: same edge, ~one-third the toll.**

## Application to the live book

Applying only the *mechanical* fee reduction to live's realized 118 trades (gross held
unchanged, since no gross improvement is claimed):

| Scenario | Gross | Fees | Slippage | NET |
|---|---:|---:|---:|---:|
| LIVE actual | +$3,489 | −$7,909 | −$3,329 | **−$7,749** |
| + ATR1.5 fee cut (−64%) | +$3,489 | −$2,847 | −$3,329 | −$2,687 |
| + slippage scales with notional | +$3,489 | −$2,847 | −$1,198 | **−$557** |

Breakeven gross required:

| Cost reduction | Gross needed | Have | Ratio |
|---|---:|---:|---:|
| 0% (today) | $11,238 | $3,489 | **0.31×** |
| 64% (ATR1.5) | $4,046 | $3,489 | **0.86×** |

**This closes the gap from 0.31× to 0.86× of breakeven — a large, real improvement that still
lands short.** The live book does not become profitable; it becomes *nearly* break-even.

## Verdict: PARTIAL

- **Mechanism CONFIRMED** — stop width drives cost, and ATR sizing cuts fees ~65% universally.
- **Edge improvement NOT ESTABLISHED** — p = 0.32, median symbol negative, top-3 concentrated.
- **Does not rescue the live config** — 0.86× breakeven is still a loss.

**No promotion. No live change. No parameter migration.** Per the standing rule, the forward-
paper verdict (~2026-08-25) is unaffected and remains the arbiter for the current config.

## Honest caveats

1. **This is one more trial against the DSR budget.** N grows; the haircut widens for
   everything. Recorded so the cost is paid explicitly rather than hidden.
2. **ATR1.5 was the best of 5 variants on TRAIN.** Best-of-5 selection inflates the TEST
   result even though TEST was held out — the *direction* replicated, but the *magnitude*
   should be treated as an upper bound.
3. **The backtest universe is 57 symbols; live trades 16.** The live-book projection above
   assumes the fee mechanism transfers, which is safe (it is arithmetic), but the gross
   assumption (unchanged) is the optimistic half of a coin flip.
4. **Slippage scaling with notional is assumed, not measured.** The −$557 row is the
   optimistic bound; −$2,687 is the defensible one.

## What would make this actionable

The fee mechanism is worth carrying forward *if* the strategy survives to a venue port,
because it compounds with maker fees (the other lever, `project_fee_reduction_venue_port`).
It is **not** worth acting on now: it does not flip the live book positive, and the verdict
is close.

If it is ever tested live, the correct vehicle is a **shadow engine** (`--atr-stop-mult 1.5`),
which costs zero incremental REST weight and spends no forward-paper degrees of freedom —
per the operator's own stated plan of shadowing anything promising for months before capital.

## Reproduction

```bash
# TRAIN
VARIANT=atr1.5 EXTRA="--atr-stop-mult 1.5" START_YEAR=2020 END_YEAR=2023 END_YEAR_MONTH=12 \
  bash scripts/cost_geometry_sweep.sh results/cg_train/atr1.5.txt
# TEST
VARIANT=atr1.5 EXTRA="--atr-stop-mult 1.5" START_YEAR=2024 END_YEAR=2025 END_YEAR_MONTH=04 \
  bash scripts/cost_geometry_sweep.sh results/cg_test/atr1.5.txt
```

Raw per-symbol outputs: `results/cg_train/`, `results/cg_test/`.

## Cross-references

- `results/stop_distance_cost_filter_verdict_2026-07-26.md` — the cost decomposition that
  motivated this, and the *rejected* threshold-filter version of the same intuition.
- `results/backtest_overfit_pbo_dsr_verdict_2026-05-29.md` — PBO 0.52 / DSR(34) 0.658.
- `docs/RESEARCH_BACKLOG.md` — closed ledger; this arc does not reopen it.
