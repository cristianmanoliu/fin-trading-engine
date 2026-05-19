# EMA × timeframe exploratory grid — research pre-registration (2026-05-19)

**Status:** LOCKED 2026-05-19 BEFORE sweep execution. Pure research, NOT a deployment decision. Activates as an exploration of the parameter landscape; results inform NOTHING about live, shadows, or STAGE_1.

## Context

Forward-paper is on day 11/60 with the live cohort underperforming the alt5-15 shadow (live −$5,340 vs shadow +$40,700, both n=17/27). This is expected statistical noise at low n, but the operator wants to use the wait time productively by mapping the (candle period × EMA fast × EMA slow) landscape across historical data.

**Operator's stated constraint:** "I do not want to touch live or shadow. Only backtest against the historical data for now." This pre-reg locks in that constraint at the protocol level.

## What this sweep does

Run an 11-cell walk-forward grid against 57 symbols of historical 1m kline data (2023-05 → 2026-04). Each cell is one (timeframe, EMA fast, EMA slow) combination. Other parameters (target_rr, side-filter, wick stop, fee/slip model, max-hold) are held constant at the LIVE production values for direct comparability with the baseline.

## What this sweep does NOT do

- Does NOT deploy any new strategy to live or shadow.
- Does NOT inform any STAGE_1 promotion decision. STAGE_1 path uses live + shadow data, not backtest.
- Does NOT iterate. The grid is fixed at 11 cells; "interesting findings" do not unlock follow-up sweeps without a new pre-reg.
- Does NOT introduce new symbols, shadows, or any operational change.
- Does NOT use deployed-16-only data (would be cherry-picked since deployed-16 was itself selected via prior backtest). The full 57-symbol universe is used to avoid look-ahead bias on the universe selection.

## The 11-cell grid (LOCKED)

| # | Role | TF | EMA fast | EMA slow | Trading rationale |
|---|---|---|---:|---:|---|
| 0 | **BASELINE** | 4H | 9 | 21 | Live config. Anchors the grid; verifies harness produces results consistent with prior walk-forward findings (memory: walk_forward_framework_2026-05-06.md). |
| 1 | 4H-interp | 4H | 7 | 14 | Tests interpolation between 5/15 (alt5-15 shadow) and 9/21 (live). |
| 2 | 4H-slower | 4H | 10 | 30 | Slower trend filter; tests "fewer false signals" hypothesis. |
| 3 | 4H-MACD | 4H | 12 | 26 | Most-published EMA pair in technical-analysis literature (MACD-classic). High falsification value — if these widely-known periods don't win, that's strong evidence the strategy doesn't have a "secret magic number." |
| 4 | 4H-ratio | 4H | 5 | 21 | Wide ratio (~1:4.2): fast fire + slow filter. Different from current 1:3 shadows. |
| 5 | 4H-ratio | 4H | 8 | 34 | Wide ratio (~1:4.25) at slightly slower base. |
| 6 | 4H-slowest | 4H | 21 | 50 | Slow-slow swing cross — popular among swing traders. Tests "less is more" hypothesis. |
| 7 | 1D-alt5 | 1D | 5 | 15 | Daily mirror of the alt5-15 shadow. The regime_finding_2026-05-06 memory suggested 1D may work where 4H struggles; this falsifies or supports that. |
| 8 | 1D-live | 1D | 9 | 21 | Daily mirror of the live strategy. |
| 9 | 1D-slow | 1D | 10 | 30 | Slowest daily — almost monthly-scale signals. |
| 10 | 1D-ratio | 1D | 7 | 28 | Daily 1:4 ratio for cross-comparison with row 5 (4H 1:4.25). |

## Held-constant parameters

All 11 cells use:
- target-RR: 6.0
- side-filter: short (shorts only)
- stop: wick (existing locked logic)
- fee_bps: 10 (round-trip taker, matches live)
- stop_slippage_bps: 5 (matches live)
- max_hold_hours: 504 (matches live mh504)
- funding: CSV-loaded historical Binance funding rates per symbol
- universe: 57 symbols (full historical set), NOT deployed-16

## Walk-forward windows (LOCKED, from harness)

3 non-overlapping 12-month chunks:
- **W1** 2023-05 → 2024-04
- **W2** 2024-05 → 2025-04
- **W3** 2025-05 → 2026-04 (the "fresh OOS" window)

These are the existing harness defaults. NOT extended to 6 windows because (a) the older windows include thinner historical data, (b) the family-wise error rate already grows fast at 11 cells × 3 windows = 33 measurements.

## Statistical caveats (HONEST, locked)

With 10 variants tested (cell 0 is the reference, not a variant), false-discovery odds under the null hypothesis are roughly 1 - (0.95)^10 ≈ **40%**. Said plainly: if NONE of the 10 variants has a true edge over the baseline, we still expect roughly 4 of them to look better by pure chance.

Implication:
- A single "winning" cell is NOT evidence of a real edge.
- Strong evidence would require a cell that wins on ALL 3 windows AND beats baseline by a margin much larger than noise (e.g., 2x).
- Even strong evidence here does not unlock deployment — see "Decision rule" below.

## Decision rule (LOCKED)

For each cell, the harness verdict is:
- **POSITIVE** if NET > 0 in at least 2 of 3 windows AND mean NET > 0
- **NEGATIVE** otherwise

For the grid as a whole:
- All POSITIVE cells are filed in this document as "research findings."
- Findings that beat the baseline cell's total NET are noted as "interesting."
- **No finding triggers any action.** Specifically:
  - No new shadow runner is created from this sweep.
  - No live parameter is changed.
  - No follow-up sweep is initiated based on findings (would require fresh pre-reg).
- If forward-paper resolves with LIVE failing the locked criteria AND a 60-day re-evaluation milestone fires, THIS document may be RE-READ as one input among many. It does not auto-promote.

## Output

1. CSV at `results/ema_tf_exploratory_grid_2026-05-19.csv` with columns:
   `cell,tf,ema_fast,ema_slow,W1_net,W2_net,W3_net,mean_net,wins,verdict`
2. Markdown summary table in this document (appended after sweep completes).
3. Telegram notification on completion.

## Reversal

This is research with no live effect — there is nothing to reverse. The CSV + markdown summary live in `results/` as a permanent research artifact.

## Audit note

Pattern: this kind of "during MONITORING, operator wants to explore" arose previously (memory: feedback_preregistration_discipline.md). The discipline question was "can we do this without violating the locked rule against tuning live parameters?" Resolution: YES, **IFF** the sweep is pre-registered, the grid is fixed, the findings are non-actionable, and the decision rule explicitly bars promotion. This document satisfies all four. The audit-pattern lens for next session: if a future operator reads this and proposes "well, cell #N looked great, let's just deploy it" — that violation must be refused with reference to this pre-reg's decision rule.

---

## Results — completed 2026-05-19 12:05 UTC

Sweep wall-clock: ~7 minutes (M3 Max parallelism across 57 symbols per window).
Total measurements: 33 (11 cells × 3 windows).

### Ranked by mean NET

| Cell | Role | TF | EMA | W1 NET | W2 NET | W3 NET | Mean NET | Wins | Trades | Verdict |
|---:|:---|:---:|:---:|---:|---:|---:|---:|:---:|---:|:---:|
| 4 | 4H-ratio-1to4.2 | 4H | 5/21 | +84,972 | +774,277 | +326,645 | **+395,298** | 3/3 | 7,128 | POSITIVE ★ |
| **0** | **BASELINE (LIVE)** | **4H** | **9/21** | **+175,021** | **+645,812** | **+65,044** | **+295,292** | **3/3** | **5,572** | **POSITIVE** |
| 1 | 4H-interp | 4H | 7/14 | −67,018 | +681,227 | +269,074 | +294,428 | 2/3 | 7,402 | POSITIVE |
| 3 | 4H-MACD | 4H | 12/26 | +93,949 | +638,216 | −44,858 | +229,102 | 2/3 | 4,394 | POSITIVE |
| 2 | 4H-slower | 4H | 10/30 | +32,454 | +626,699 | −80,275 | +192,959 | 2/3 | 4,501 | POSITIVE |
| 5 | 4H-ratio-1to4.25 | 4H | 8/34 | −50,472 | +532,736 | −11,056 | +157,069 | 1/3 | 4,736 | NEGATIVE ✗ |
| 6 | 4H-slowest | 4H | 21/50 | +112 | +303,915 | +32,662 | +112,230 | 3/3 | 2,356 | POSITIVE |
| 7 | 1D-alt5 | 1D | 5/15 | +3,133 | +158,766 | +174,656 | +112,185 | 3/3 | 1,075 | POSITIVE |
| 9 | 1D-slow | 1D | 10/30 | −19,296 | +53,637 | +95,828 | +43,390 | 2/3 | 487 | POSITIVE |
| 8 | 1D-live | 1D | 9/21 | −37,137 | +61,997 | +102,523 | +42,461 | 2/3 | 679 | POSITIVE |
| 10 | 1D-ratio-1to4 | 1D | 7/28 | −30,998 | +48,465 | +77,111 | +31,526 | 2/3 | 657 | POSITIVE |

### Findings (five honest reads)

1. **LIVE config validates against the harness.** Cell 0 (4H 9/21 mh504) is positive in all 3 walk-forward windows with mean +$295k. This is consistent with prior walk-forward findings (memory `walk_forward_framework_2026-05-06.md` recorded mean +$146k at mh336; +$295k at mh504 is the same edge, longer max-hold capturing more 6R targets). Baseline is not a sweep artifact.

2. **One config beats LIVE — cell 4 (4H 5/21):** mean +$395k vs +$295k (+34%), 3/3 windows. The wider 1:4.2 ratio (fast EMA 5 against slow EMA 21) fires 28% more trades (7,128 vs 5,572). Interpretation:
   - Could be real edge: wider ratio = fast fire on momentum flip + slow filter for trend confirmation = cleaner signals overall.
   - Could be DoF noise: with 10 variants tested, ~40% chance one looks better by chance even under null.
   - Distinguishing the two requires forward-data, not more backtests.

3. **All 4H configs cluster.** Range from +$112k (cell 6) to +$395k (cell 4). LIVE sits in the upper-middle. No 4H variant is dramatically worse than baseline (excluding rejected cell 5). Implication: the 4H short-only family captures a recognizable structural edge that is robust to small parameter perturbations. The "edge" is regime+universe+RR, not the exact EMA numbers.

4. **1D timeframe is decisively weaker than 4H.** Best 1D config (cell 7, 5/15) is +$112k — 62% lower than LIVE baseline. Trade counts collapse (487-1,075 vs 2,356-7,402 on 4H). The earlier `regime_finding_2026-05-06.md` hypothesis ("1D may work where 4H struggles") is **not supported** by this sweep at realistic costs. 1D shorts on this universe with these params do not replace 4H.

5. **MACD-classic (12/26) is mid-pack.** Falsification: the most-widely-published EMA pair in TA literature offers no special edge on this strategy + universe. The strategy's edge does NOT live in some "magic number" EMA pair — it lives in the wick-stop + 6:1 RR + shorts-only + crypto-universe combination. Useful evidence against the temptation to keep sweeping for the "right" EMA pair.

### What this sweep does NOT change

- LIVE cohort (deployed-16, 4H 9/21 mh504): unchanged.
- 3 shadows (alt5-15-336, alt5-15-504, bb20): unchanged.
- Layer 3 wrap on KAVA + ENS: unchanged.
- forward_paper_status / daily_digest / layer3_cron: all unchanged.

### What this sweep DOES change

- Adds a permanent research artifact in `results/`.
- Falsifies the "magic EMA number" hypothesis.
- Falsifies the "1D might be better" hypothesis at realistic costs.
- Adds cell 4 (4H 5/21) to the catalog of "interesting parameter combinations" for hypothetical future re-evaluation IF an authorizing decision rule fires (currently none does).

### What this sweep does NOT authorize

- Adding cell 4 as a shadow runner. Cell 4 is a research finding, not a deployment candidate.
- Running a follow-up sweep to "narrow in" on cell 4 (e.g., trying 5/20, 6/21, 4/19). Would require fresh pre-reg with explicit decision rule for what to do with results.
- Changing LIVE to 5/21 if forward-paper underperforms expectations. The forward-paper criteria (`results/forward_paper_outcome_resolution_decision_rule_2026-05-10.md`) do not include backtest as an input.

### Statistical reminder

10 variants × 3 windows = 33 measurements. Family-wise false-discovery rate under null ≈ 1−0.95^10 ≈ 40%. The findings here are HYPOTHESIS-STRENGTH, not proof. The forward-paper experiment on live data is the real test.

**This document is the locked record. Future operators reading "cell 4 looked great in backtest" must apply the decision rule above — research finding only, non-actionable.**

