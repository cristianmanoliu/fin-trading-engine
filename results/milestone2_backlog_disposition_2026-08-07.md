# Milestone-2 backlog — disposition (2026-08-07)

**Trigger for this document:** the operator asked "are you sure nothing left to
explore" after C4 closed. Re-checking *state* rather than re-asserting the
claim surfaced a genuine loose end: `results/strategy_backlog_milestone2_2026-05-08.md`
locks **8 candidate ideas** (top-5 picks: A2, C1, A1, B2, D1) with the
instruction "Do NOT execute before milestone 2 begins," and
`results/milestone2_runbook_decision_rule_2026-05-10.md` says its launch
trigger fires on **PROMOTE *or* HARD KILL** — not PROMOTE alone.

That matters: the forward-paper run ended in neither. It was **terminated early
by operator decision** at day 88 of ~112, 134 of a pre-registered 150 trades.
No promotion artifact, no HARD KILL artifact. So the milestone-2 trigger never
fired and `scripts/milestone2_launch.sh` still refuses to run — the backlog is
neither activated nor formally closed.

This document closes it. **No new trials were run.** Every disposition below is
either an existing verdict or an arithmetic screen.

---

## Disposition of the top-5

| # | Idea | Status | Basis |
|---|---|---|---|
| **A2** | ATR-targeted position sizing | **CLOSED** | Tested as #10 vol-sizing overlay: NO-GO — live $-risk sizing is *already* vol-aware, the overlay hurts (`live_overlay_verdict_2026-06-10.md`). Also re-answered from the cost side by the ATR-stop sweeps (`atr_stop_cost_geometry_verdict_2026-07-26.md`, `atr1_0_holdout_verdict_2026-07-27.md`). |
| **A1** | Realized-vol regime filter | **CLOSED** | The regime-overlay family was tested to exhaustion: gate (`btc_regime_gate_backtest_verdict_2026-06-07.md`), breadth (`breadth_regime_gate_backtest_verdict_2026-06-08.md`), and full-stop switch — **F=0/9, VOID**. Short-only, the gate is degenerate: it adds **$0**. |
| **C1** | Funding-rate extremum reversal | **CLOSED** | The carry family closed as a unit: #8 cross-sectional carry MARGINAL (~0.4 Sharpe), #13 cross-venue spread NO-GO (dead-arbed), #3 downgraded **NO-BUILD** (`crossvenue_spread_verdict_2026-06-10.md`). |
| **B2** | Bollinger squeeze release | **CLOSED** | BB was the only non-EMA shadow and ran live for the whole forward-paper window. Additionally contaminated by the phantom-long bug (`project_sidefilter_phantom_bug`) — the reported bull-year figure was ≈ **−$1.69M** corrected. |
| **D1** | Session filter (UTC session) | **CLOSED — see below.** Never tested. Closed on arithmetic, not on a verdict. |

## D1 — the only genuinely untested item, and why it still fails

D1 is unlike the other four: it is a **cost-side** filter (restrict trading to
sessions with better realized slippage), and cost-side is exactly the lever the
viability frontier says can matter. So it deserved an actual check rather than
a hand-wave.

It fails on a ceiling argument, no backtest required.

D1 can do two things: **remove trades**, and **reduce slippage** on those it
keeps. It cannot raise gross R per trade. So its absolute ceiling is *every
remaining trade executes at zero slippage with gross unchanged*:

| Scenario | cost R | gross R | ratio (need ≥ 3.0) | pass |
|---|---:|---:|---:|:--:|
| Live, as actually run | 0.1000 | 0.0296 | **0.30** | ✗ |
| **D1 at its perfect ceiling** (slip → 0) | 0.0667 | 0.0296 | **0.44** | ✗ |
| D1 + fees also zero (impossible) | 0.0000 | 0.0296 | ∞ | ✓ |

*(`scripts/quant_honesty.py::screen`, stop_pct 1.5%, L=66.7×.)*

**Even perfect execution leaves D1 at 0.44 against a required 3.0 — short by
~7×.** Slippage was only 5 bp of the 15 bp cost; the fee term (10 bp on
notional, twice) is untouched by *when* you trade. And the real effect is far
below the ceiling, because removing sessions also removes trades, which cuts
gross and worsens the statistical power that was already the binding problem.

This is the frontier doing its job: a 30-second arithmetic screen retires a
locked backlog item that would otherwise have cost a day of backtesting to
reject.

## The remaining three (G1, B1, F1)

Deferred to "milestone 3" by the backlog's own ranking as LOW priority. They
are entry-mechanism variants on the perp EMA class, which is closed at N≈85
trials, PBO 0.52, DSR 0.001, participation ratio ≈1.9. Each additional cell
*widens* the multiple-testing haircut the live edge already failed. **Closed by
the class closure, not individually tested.**

## Status of the milestone-2 machinery

- `scripts/milestone2_launch.sh` refuses to run without a PROMOTE or HARD KILL
  artifact. Neither exists and neither will — the run was terminated by
  operator decision. The guard is correct and should be left alone.
- `results/milestone2_runbook_decision_rule_2026-05-10.md` stays locked and
  unexecuted, as the record of a launch sequence that never activated.
- **The backlog is now dispositioned rather than dangling.** Nothing in it is an
  open lead.

## Process note

This is the second time in two days that "the repo is exhausted" was asserted
and then falsified by checking state instead of re-reading prose — first C4
(flagged in a synthesis, unblocked when the book closed), now this backlog
(locked behind a trigger that could never fire, so it never surfaced as either
open or closed). Both were found by grepping for *forward-looking language*
(`milestone-2`, `future milestone`, `revisit`, `if the project ever`) rather
than by asking whether the project felt done.

The pattern worth keeping: **a lead parked behind a condition that later becomes
unreachable never announces itself.** It is not in a TODO list and not in a
failing test — it just sits, indefinitely, looking settled.

## Cross-references

- Backlog being dispositioned: `results/strategy_backlog_milestone2_2026-05-08.md`
- Launch runbook (locked, never fired): `results/milestone2_runbook_decision_rule_2026-05-10.md`
- Screening rule applied to D1: `results/viability_frontier_2026-07-27.md`, `scripts/quant_honesty.py`
- The prior loose end, same shape: `results/btceth_regime_subbook_verdict_2026-08-07.md`
