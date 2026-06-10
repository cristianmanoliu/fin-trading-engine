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

- [x] **#21 RSI/MACD deep validation** — DONE 2026-06-10. **BOTH NO-GO, ledger item
      CLOSED.** Verdict `results/batch3_21_rsi_macd_verdict_2026-06-10.md` (commit
      55fb7ca). RSI total +10.5% vs baseline but 3/7 years, corr 0.844 — same regime
      bet, no diversifier. MACD below baseline on every axis.
- [x] **#23 BTC→alt lead-lag** — DONE 2026-06-10. **NO-GO, 0/12 cells.** Verdict
      `results/btc_leadlag_verdict_2026-06-10.md` (8282a4b). First run t=37 was a
      resample left-edge-label look-ahead (documented trap); corrected: 2-6bp gross
      vs 15bp cost, t≈0. Fully arbed.
- [x] **#22 ETH/BTC RV pair** — DONE 2026-06-10. **NO-GO.** Verdict
      `results/ethbtc_rv_verdict_2026-06-10.md` (commit in history). Grid-mean Sharpe
      −0.157 vs 0.8 bar; ratio trends but too slow for 30bp pair costs; MR side loses.
- [x] **#25 Failed-pump cascade short** — DONE 2026-06-10. **PASS locked bar →
      MARGINAL LEAD (milestone-2, not deployable).** Verdict
      `results/failed_pump_verdict_2026-06-10.md` (fdbd619). Primary cell n=515:
      mean +2.38%, median +6.74%, t=2.36, drop5 +0.75%, 5/7 yrs, holds ex-2025.
      Funding-join bug (Series-reindex trap) found + fixed mid-run.
- [x] **#24 Vol-event two-sided breakout** — DONE 2026-06-10. **NO-GO, 0/3 gate
      families.** Verdict `results/vol_event_breakout_verdict_2026-06-10.md`. Means
      positive (settlement +45bp, 6/7 yrs) but median −31…−62bp, drop5 negative
      everywhere, whipsaw 29-36% — lottery-ticket profile. Vol-timing expression
      space (directional AND direction-free) now closed.
- [x] **Batch synthesis** — DONE. `results/batch3_synthesis_2026-06-10.md` + Telegram
      sent.

## Rules

- Verdict doc + atomic commit per candidate, including NO-GOs.
- No new cells, no post-hoc thresholds; deviations documented in verdict docs.
- Big raw numbers → audit confounds (min-universe, fantasy compounding, optimistic costs)
  before believing.

## Review

**BATCH CLOSED 2026-06-10, same-day.** 4 NO-GO (#21 RSI/MACD ledger settled, #22 RV,
#23 lead-lag, #24 two-sided breakout) · 1 PASS-on-locked-bar (#25 failed-pump cascade
short → MARGINAL LEAD, milestone-2 only, NOT deployable: thin drop5 cushion, 2026
negative, tiny capacity, daily-bar sim).

Durable outputs: two new audit traps documented (resample left-edge look-ahead;
pd.Series-reindex silent empty join), #24 closes the vol-timing expression space from
the direction-free side, #25 joins #15/#16 as the third milestone-2 lead.

Standing recommendation unchanged: **operate-and-wait** on forward-paper (inside
60d/150-trade power floor).
