# Backtest-Overfitting Quantification — VERDICT

> ℹ️ **Input-contamination note (2026-06-09) — verdict UNCHANGED (reinforced).** The matrix
> below had 2 of 34 columns (`macd_12_26_9`, `rsi_14`) generated on the phantom-long-buggy
> binary (fixed `d1d0fae`); the other 32 are EMA-signal variants and were clean (the `LIVE`
> column is byte-identical, Sharpe 0.2273). Re-running on a clean matrix
> (`overfit_returns_matrix_2026-06-09_postfix.csv`) moved the metrics the *wrong* way for the
> strategy: PBO 0.47→**0.52**, DSR(34) 0.642→**0.658** (still ≪0.95), slope −0.87→−0.94, and
> the contaminated `rsi_14` stopped being the modal IS-best config (phantom inflation
> removed). **FRAGILE stands and is reinforced on clean data.** See
> `results/alt_signals_phantom_corrected_verdict_2026-06-09.md` §3.

**Status:** Mechanical application of the LOCKED decision rule
(`results/backtest_overfit_pbo_dsr_decision_rule_2026-05-29.md`, commit `cd7e4e8` + pre-data
amendments). No thresholds altered after seeing data.
**Date:** 2026-05-30
**Data:** `results/overfit_returns_matrix_2026-05-29.csv` — 64 months (2020-01 → 2025-04) ×
34 distinct configs, 57-symbol universe, equal-weight monthly NET, slip=5/fee=10, historical
funding, `--exact-fills --include-boundary --pessimistic-ambiguous`. Entry-month pairing
(open-event exchange ts). No all-zero config columns.
**Raw output:** `results/backtest_overfit_pbo_dsr_2026-05-29.txt`

## Measured values

| Estimator | Value | Band |
|---|---:|:---|
| PBO | 0.4698 | healthy (<0.50) — but ≈ coin-flip |
| **DSR (N=34)** | **0.6420** | **ALARM (<0.90)** |
| DSR (N=68) | 0.5694 | — |
| DSR (N=102) | 0.5286 | — |
| PSR (benchmark 0) | 0.9690 | — |
| Search haircut (PSR − DSR@34) | 0.3270 | large |
| Degradation slope | −0.8737 | **ALARM (<0)** |
| IS-best concentration | 0.4082 (modal `rsi_14`) | < 0.5 → slope NOT excluded |
| LIVE is IS-best in | 1.13% of folds | LIVE almost never the in-sample optimum |
| OOS prob-of-loss (n*) | 0.0904 | — |
| LIVE monthly Sharpe | 0.2273 (annualized 0.787) | modest |
| skew / kurtosis | +0.422 / 3.427 | positive skew (helps DSR) |

DSR stays **below 0.95 at every examined N** (0.642 at N=34, declining to 0.529 at N=102) — the
edge does not clear the search-haircut bar at any trial count.

## Verdict: FRAGILE

Mechanical application of the combined-verdict rule:
- ROBUST requires PBO<0.50 **AND** DSR(34)>0.95 **AND** slope≥0.5 → fails (DSR 0.642, slope −0.87).
- FRAGILE triggers on PBO>0.75 **OR** DSR(34)<0.90 **OR** slope<0 → **two triggers fire**:
  DSR(34)=0.642 < 0.90, and slope=−0.87 < 0 (concentration 0.408 < 0.5, so the slope is in the
  interpretable regime — NOT excluded by the degeneracy caveat).

**Effect on keep-going confidence: DOWN.**

## Interpretation (honest)

1. **LIVE's edge is real in isolation but does not survive the multiple-testing haircut.**
   PSR = 0.969 → LIVE's standalone monthly Sharpe (0.227, ~0.79 annualized) is individually
   significant at ~97%. But DSR(34) = 0.642 → once you dock it for having searched 34 configs,
   only 64% confidence the true Sharpe exceeds the luck-benchmark SR0 (monthly 0.183 ≈ 0.63
   annualized). LIVE's annualized Sharpe (0.79) is barely above what searching 34 configs would
   produce by chance (~0.63). The 0.327 PSR−DSR gap **is** the quantified search-inflation — the
   same phenomenon that cut honest-annual $129k→$69k, now measured directly.

2. **The selection procedure overfits.** PBO = 0.47 ≈ coin-flip: in-sample ranking has almost no
   predictive power for out-of-sample. Slope = −0.87: in-sample winners systematically
   underperform out-of-sample. Both indict naive optimization over this config space.

3. **Crucial nuance — LIVE is almost never the in-sample winner (1.1% of folds; modal is
   `rsi_14` at 41%).** So PBO and slope primarily indict the *in-sample-maximizing selection
   procedure* (which would pick `rsi_14`/others), NOT LIVE specifically — LIVE was chosen by
   walk-forward + judgment, not in-sample maximization. The metric that bears *directly* on LIVE
   is the DSR, and it is also unfavorable (0.642): LIVE's modest edge is mostly eaten by the
   breadth of search.

## What this does and does NOT mean

- **Does NOT mean LIVE loses money.** Its standalone Sharpe is positive and individually
  significant (PSR 0.97). FRAGILE = the *backtest edge does not clearly survive the search-tax*,
  not "the strategy is unprofitable."
- **Does NOT auto-kill or change the live config.** This is advisory. Forward-paper resolution
  (`forward_paper_outcome_resolution_decision_rule_2026-05-10.md`) and the drift detector remain
  the only decision-grade gates.
- **DOES raise the importance of forward-paper.** The backtest edge is search-fragile, so the
  unbiased forward/shadow data is now the *decisive* evidence — exactly the case the
  shadow-before-commit discipline was built for. Do not let a positive backtest substitute for it.
- **DOES strengthen the milestone-2 case** for exploring genuinely different *mechanisms* (with
  economic theses + plateau + overfit gating) rather than promoting this parameter family.

## Comparison to pre-registered priors

Priors were ROBUST 55% / MARGINAL 35% / FRAGILE 10%. The outcome landed in the FRAGILE tail —
driven by a modest LIVE Sharpe meeting a large 34-trial haircut. Prediction #2 (PSR ≫ DSR) and
#3 (positive skew, +0.42) held; prediction #1 (PBO ~0.2–0.4) was too optimistic (0.47); the edge
did NOT clear DSR 0.95 even at N=34, contrary to prediction #3's hope.

## Caveats

- Backtest-only. Says nothing about live-fill realism (Layer 2/3 + forward-paper).
- T = 64 months; CSCV assumes block-level near-independence (4-month blocks).
- Degradation slope interpreted per the pre-data caveat: concentration 0.408 < 0.5, so slope is
  diagnostic here (not the degenerate single-dominator regime).
