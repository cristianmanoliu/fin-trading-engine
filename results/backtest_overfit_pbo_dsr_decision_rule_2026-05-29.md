# Backtest-Overfitting Quantification (PBO + Deflated Sharpe) — PRE-REGISTERED Decision Rule

**Status:** LOCKED 2026-05-29, BEFORE generating any returns matrix or running any analysis. Committed as a standalone change prior to code/data.
**Author:** Cristian Manoliu
**Context:** Forward-paper is mid-flight (day ~21/60) and compute is idle. This is an
ADVISORY robustness analysis on the EXISTING live edge — it spends zero forward-paper
degrees of freedom and invents zero new configs. It is NOT a kill trigger.

## Why this analysis

The LIVE config (4H EMA 9/21, shorts-only, 6:1 RR, mh504, slip=5/fee=10) was **selected**
as the survivor of a 6-month search spanning 50+ analyses and 31+ documented structural
perturbations (`results/research_synthesis_2026-05-19.md`). Selection across many trials
inflates the apparent edge of the winner — the same mechanism that already cut the honest
annual from the $129k deployed-claim to ~$69k (train-only-shortlist look-ahead). This
analysis puts a number on the *residual* selection/overfitting risk in the selected config
using two purpose-built estimators.

## Claim under test

After accounting for the number of configurations searched, LIVE's backtest edge is
statistically distinguishable from the best of N lucky draws under the null of zero skill.

## Method

Two estimators computed on a per-config monthly-NET returns matrix.

### Returns matrix M

- **Columns N = 40 documented configs** — every cell of the seven `results/*_2026-05-19.csv`
  sweeps (11 EMA×tf + 5 confluence + 6 vol + 3 side + 6 MLTP + 6 trail + 3 alt-signal).
  Column 0 = LIVE baseline.
- **Rows T ≈ 64** — calendar months 2020-12 → 2025-04. Each cell = portfolio monthly NET =
  equal-weight sum across the **57-symbol `universe` group**.
- **Universe = full 57, NOT deployed-16** — a shortlist would re-inject the train-only-
  shortlist look-ahead the project already caught. Equal-weight, no selection.
- Each config is run as a **continuous 5y backtest per symbol** (concatenated monthly CSVs,
  per the no-month-segmentation invariant — segmenting inflates ~5%), slip=5/fee=10,
  funding=historical. Per-trade journals (`--journal-dir`) are reduced to monthly NET by
  bucketing each closed trade into its **exit month**.
- **Ragged history is expected:** not all 57 symbols exist in 2020-12 (e.g. LDO from 2023-06,
  AAVE from 2024-03). A symbol with no data in a month contributes nothing that month; the
  equal-weight aggregate is over symbols live that month. Early months therefore span fewer
  symbols (less diversified, higher variance) — this is a faithful property of the opportunity
  set, NOT a bug, and the same matrix feeds every config identically so it cannot bias the
  cross-config comparison.

### Estimator 1 — Probability of Backtest Overfitting (PBO) via CSCV

Bailey, Borwein, López de Prado & Zhu (2017).

- Partition the T rows into **S = 16** disjoint blocks (~4 months each).
- Enumerate all **C(16,8) = 12,870** train/test combinations.
- Per combination: in-sample-best config `n* = argmax` monthly Sharpe on the train blocks;
  `ω` = out-of-sample relative rank of `n*` on the test blocks; `λ = logit(ω) = ln(ω/(1-ω))`.
- **PBO = fraction of combinations with `λ ≤ 0`** (the IS-best lands at or below the OOS median).
- Also report: the `λ` distribution; the **performance-degradation slope** = OLS slope of
  (OOS Sharpe of `n*`) regressed on (IS Sharpe of `n*`) across the 12,870 combinations — slope
  near 1 = no degradation, slope ≤ 0 = the in-sample winners systematically lose OOS; and the
  OOS probability-of-loss for `n*`.

### Estimator 2 — Deflated Sharpe Ratio (DSR) + PSR

Bailey & López de Prado (2014). All Sharpes per-period (monthly); annualize only for display.

- `SR̂` = LIVE monthly Sharpe; `γ3`, `γ4` = skew, kurtosis of LIVE monthly returns; T ≈ 64.
- `SR0 = √Var({SRₙ}) · [ (1−γ)·Z⁻¹(1−1/N) + γ·Z⁻¹(1−1/(N·e)) ]`, where γ = 0.5772
  (Euler–Mascheroni), `Var({SRₙ})` is taken over the 40 configs' monthly Sharpes, Z⁻¹ is the
  standard-normal inverse-CDF.
- `DSR = Φ( (SR̂ − SR0)·√(T−1) / √(1 − γ3·SR̂ + ((γ4−1)/4)·SR̂²) )`.
- `PSR` = same formula with `SR0 = 0`. The gap `PSR − DSR` IS the multiple-testing haircut.
- **N-sensitivity curve:** recompute `SR0` and `DSR` at **N ∈ {40, 80, 120}** (the matrix stays
  40 columns; only the trial-count in `SR0` varies, modeling undocumented/ad-hoc search).
  Report the DSR(N) curve and the N at which DSR crosses 0.95.

## Decision rule (LOCKED — mechanical, ADVISORY)

| Estimator | Healthy | Middle | Alarm |
|:---|:---|:---|:---|
| **PBO** | < 0.50 | 0.50–0.75 | > 0.75 |
| **DSR (N=40)** | > 0.95 | 0.90–0.95 | < 0.90 |
| **Degradation slope** | ≥ 0.5 | 0–0.5 | < 0 |

Combined verdict:

| Verdict | Condition | Effect on keep-going confidence |
|:---|:---|:---|
| **ROBUST** | PBO < 0.50 AND DSR(40) > 0.95 AND slope ≥ 0.5 | UP — edge survives the search-haircut |
| **MARGINAL** | any one estimator in its middle band, none in alarm | NEUTRAL — plausibly real but search-inflated |
| **FRAGILE** | PBO > 0.75 OR DSR(40) < 0.90 OR slope < 0 | DOWN — strong overfitting signature; document prominently |

## Pre-registered predictions (written before running)

1. **PBO < 0.5** — LIVE survived 31 perturbations and uniquely won W3 (05-19), so the IS-best
   should usually hold its OOS rank. Expect ~0.2–0.4.
2. **PSR ≫ DSR** — the search haircut is the whole point; expect a visible gap.
3. **Positive skew helps DSR** — shorts-only 6:1 has rare-big-win positive skew; the `−γ3·SR̂`
   term in the DSR denominator raises DSR vs a naive Sharpe. DSR may clear 0.95 at N=40 despite
   lumpy monthly returns.
4. **DSR degrades with N** — by N=120 it may slip below 0.95; the crossing-N quantifies how much
   undocumented search the edge can absorb before becoming indistinguishable from luck.
5. **Degradation slope positive but < 1** — some OOS shrinkage, not a collapse.

## What is NOT permitted under this pre-registration

- No changing S, the universe, the cost stack, the trial set, the metric, or T after seeing results.
- No swapping the performance metric (monthly Sharpe) post-hoc for a flattering one.
- No re-running CSCV under a different block scheme to find a lower PBO.
- No extending the matrix beyond the 40 documented configs. Inventing configs = a fresh search
  = DoF burn, and requires its own pre-registration.
- A FRAGILE outcome updates confidence DOWN; it is not a license to retry with different settings.

## Pre-registered priors

| Combined verdict | Prior |
|---|---:|
| ROBUST | 55% |
| MARGINAL | 35% |
| FRAGILE | 10% |

Reasoning: the 05-19 robustness result + the unique W3 win argue against gross overfitting; but
the documented $129k→$69k look-ahead haircut and the cost-model fragility lesson keep a
non-trivial MARGINAL/FRAGILE mass.

## Files

- `results/backtest_overfit_pbo_dsr_decision_rule_2026-05-29.md` — this file
- `cmd/backtest --journal-dir` — additive flag that enables matrix generation (to be added)
- `scripts/overfit_matrix_gen.sh` — runs the 40 configs × 57 symbols, reduces journals (to be added)
- `results/overfit_returns_matrix_2026-05-29.csv` — the T×N matrix (generated)
- `scripts/backtest_overfit_analysis.py` — CSCV + DSR/PSR + N-curve (to be added)
- `results/backtest_overfit_pbo_dsr_2026-05-29.txt` — raw analysis output (generated)
- `results/backtest_overfit_pbo_dsr_verdict_2026-05-29.md` — mechanical verdict applying this rule

## Scope

Research-only. ADVISORY confidence-calibration on the backtest edge. Does NOT change the live
config; does NOT auto-kill. Forward-paper resolution
(`forward_paper_outcome_resolution_decision_rule_2026-05-10.md`) and the drift detector remain
the only decision-grade gates. Filed as a calibration artifact for milestone-2 strategy design.
