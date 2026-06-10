# Executing plan: batch-3 candidates #21–#25 (2026-06-10 PM)

**Pre-reg (LOCKED, committed before first run):** `results/strategy_candidates_batch3_2026-06-10.md`
**Prior batch:** 20-candidate orthogonal search CLOSED (`results/orthogonal_search_synthesis_2026-06-10.md`); its plan + review archived in git history of this file (commit `6bfba2c` era).

This batch = 1 open ledger item (#21, the corrected MACD/RSI CANDIDATE from
`alt_signals_phantom_corrected_verdict_2026-06-09.md`) + 4 untested strategy STRUCTURES
(two-sided payoff, cross-asset conditioning, 2-asset RV, event-cascade short).
Research-only; live config / VPS / forward-paper untouched. Default outcome NO-GO;
honesty block mandatory (median + drop-top-5% + by-year + correct-sign + selection
correction across locked grid).

## Candidates (execution order locked in pre-reg)

- [ ] **#21 RSI/MACD deep validation** — per-trade journals 3 cells (baseline / MACD-12/26/9
      / RSI-14), deployed-20 universe, 2020→2026-06 continuous, live cost model (fee=10,
      slip=5, mh504, 4H, short, funding CSV) → honesty block + 6 yearly windows + monthly
      corr-to-baseline. Settles the `RESEARCH_BACKLOG.md` open CANDIDATE item.
- [ ] **#23 BTC→alt lead-lag** — event study only; screen: |cond. fwd return| > 15bp with
      |t|>3 at ≥1 horizon. Grid 2 z-thresholds × 3 horizons. Fail screen → NO-GO, no build.
- [ ] **#22 ETH/BTC RV pair** — trend k∈{7,14,30} + MR z∈{1.5,2.0,2.5}; 30bp pair RT +
      per-leg funding. Bar: grid-mean ann.Sharpe > 0.8 AND best cell passes honesty block.
- [ ] **#25 Failed-pump cascade short** — de-survivorship 732-perp daily klines; pump
      P∈{25%,50%}, fail = close < prior low, short next open, stop above pump high, 7d max;
      70bp RT + funding bleed. Primary cell P=25%.
- [ ] **#24 Vol-event two-sided breakout** — gates: macro events / OI-z spike / settlement
      EXTREME_LOW; brackets ±0.75×ATR(4H,14), stop = opposite bracket, +2R or 24h; 15bp on
      filled leg. Need ≥2/3 gate families positive.
- [ ] **Batch synthesis** — `results/batch3_synthesis_2026-06-10.md` + Telegram summary to
      operator.

## Rules

- Verdict doc + atomic commit per candidate, including NO-GOs.
- No new cells, no post-hoc thresholds; deviations documented in verdict docs.
- Big raw numbers → audit confounds (min-universe, fantasy compounding, optimistic costs)
  before believing.

## Review

(to be filled at batch close)
