# The Viability Frontier — the one-line law every strategy in this venue must obey

**Status:** Measurement synthesis, 2026-07-27. **Zero new trials** — every constant below was
already measured in committed docs. This spends nothing against the DSR budget; it is a
*constraint*, not a candidate. Written so that any future idea can be screened in 30 seconds
before a single backtest is run.

## The law

A strategy is viable if and only if:

```
E[gross R per trade]  >  cost per trade in R  =  (fee_rt + slip + funding) × L × 10⁻⁴
```

where `L = notional / stake = 1 / stop_width` is the implied leverage forced by risk-based
sizing. **Cost is deterministic. Gross is a random variable.** That asymmetry is the entire
story of this project.

## Measured constants (live forward-paper, 118 trades)

| Term | Value | Certainty |
|---|---:|---|
| Mean leverage L | 67× | measured exactly |
| Cost per trade | **0.0952 R** (fee 0.0670 + slip 0.0282) | measured to the basis point |
| Gross per trade | **+0.0296 R** | SE = 0.223 R → 95% CI **[−0.41, +0.47]** |

The cost term is known to four decimal places. The gross term's confidence interval is
**15× wider than the cost it must beat.** The certainty-equivalent of (uncertain gross −
certain cost) is negative regardless of the point estimate.

## Every tested signal class against the frontier (deployed-16, TRAIN, live costs)

| Class | Backtest gross R | Cost R | Backtest verdict | Live gross R |
|---|---:|---:|---|---:|
| EMA 9/21 (live) | +0.309 | 0.052 | viable on paper | **+0.030** |
| Bollinger | +0.135 | 0.031 | viable on paper | — |
| MACD | +0.124 | 0.051 | marginal | — |
| RSI | +0.102 | 0.041 | marginal | — |
| Momentum | +0.054 | 0.037 | marginal | — |
| PDH/PDL | −0.010 | 0.112 | dead | — |
| VWAP-dev | −0.083 | 0.069 | dead | — |

Several classes clear the line *in backtest*. The only one with live data collapsed from
+0.309 to +0.030 the moment it met the market — while its cost term stayed exactly where the
model put it. **Backtest gross is the mirage term; cost is the reliable term.** This is why 85
trials found "winners" and zero survived: the frontier's left side is soft, its right side is
granite.

## The three levers, each pinned

1. **Cut fees** (maker/venue port): 10bp → 2–4bp puts cost at 0.02–0.04 R. Live gross
   (+0.03 R) lands *on* the boundary — inside its own noise band. Proving it clears requires
   ~24,000 trades ≈ **55 years** at 1.2 trades/day. An edge you cannot sign is an edge you
   cannot size (Kelly ≈ 0).
2. **Cut leverage** (wider stops): tested exhaustively — the ATR curve on the deployed-16 is
   monotone worse; gross collapses faster than cost falls. Closed.
3. **Raise gross** (better signal): 6 months, ~85 trials, PBO 0.52. Nothing has ever exceeded
   +0.03 R live or ~0.79 annualized Sharpe anywhere, against a luck-bar now ≈ 1.24. Closed.

## The 30-second screening rule (the reusable artifact)

Before building ANY future candidate for a perp venue:

1. Estimate its typical stop width `w` → leverage `L = 1/w`.
2. Cost R ≈ `(fee_rt + 5bp) × L × 10⁻⁴`.
3. **Require backtest gross ≥ 3× cost R** before writing a line of code — because live gross
   historically arrives at ~1/10 of backtest gross while cost arrives at 1.0× of model.

Applied retroactively, this rule rejects Option C, Purgatory, PDH/PDL, and VWAP-dev *a
priori*, and flags the live config as boundary-marginal — which is exactly what forward-paper
then spent three months confirming.

## What a viable strategy must look like (spec for future work)

- Gross ≥ 0.15 R/trade at L ≤ 30 (stop ≥ 3.3%), sustained out-of-sample; **or**
- Maker execution ≤ 3bp round-trip with gross ≥ 0.05 R (requires venue where operator can
  trade + live queue validation — Kraken path, post-quiz, post-verdict); **or**
- An instrument with ≥10× lower cost per unit of risk. Cash-equity cross-sectional portfolios
  run at ~1× leverage and 1–2bp — cost per unit risk two orders of magnitude below this venue.
  That is `fin-equity-lab`, already designed 2026-06-11, M0 (IBKR + EODHD verification) not
  yet started.

## Cross-references

- `stop_distance_cost_filter_verdict_2026-07-26.md` — the live cost decomposition (gross
  +$3,489 / costs $11,238 / breakeven fee 0.20bp).
- `deployed16_direct_sweep_verdict_2026-07-27.md`, `deployed16_alt_signals_verdict_2026-07-27.md`
  — the 27 arms that closed levers 2 and 3 on the traded book.
- `backtest_overfit_pbo_dsr_verdict_2026-05-29.md` — why lever 3 worsens with more search.
