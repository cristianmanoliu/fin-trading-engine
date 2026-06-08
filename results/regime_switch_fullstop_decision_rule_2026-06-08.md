# BTC Regime-Switch (Full-Stop) Test — Design

**Date:** 2026-06-08
**Status:** Pre-registered design. Research-only. No live/shadow/VPS change.
**Supersedes nothing.** Follow-up to the SHORT_ONLY regime-gate sweep
(`results/shadow_regime_gate_shortonly_verdict_2026-06-08.md`), which came back
NEGATIVE/degenerate for the live config and every 504h cohort.

---

## 1. Why this exists (the problem)

The deployed strategy and all 8 shadows are **always-short trend-followers**. They
print in down/choppy years and bleed in sustained bull years. The structural risk
the operator will not tolerate is **back-to-back negative years** — two consecutive
red years would breach risk tolerance before any third-year recovery.

The SHORT_ONLY BTC regime-gate (tested 2026-06-08) was meant to fix this by sitting
out non-bearish regimes. It **failed degenerately**: it blocked *new entries* during
non-SHORT days but let already-open 504h (21-day) shorts ride straight through those
days. Gate P&L == always-short baseline to the penny. The gate never separated
because positions spanned the carve-outs.

**This test adds the missing mechanism: force-close on regime exit.** When BTC leaves
a SHORT regime, every open position is closed and new entries halt — the strategy goes
fully flat through bull regimes. This is the only mechanically distinct option from the
degenerate gate, and it directly targets the consecutive-red-year failure mode.

**Intended outcome:** a pre-registered, DoF-disciplined backtest answering — *does a
full-stop regime switch eliminate back-to-back negative years across the strategy
class?* PASS → a real candidate mechanism for the operator's stated risk tolerance.
FAIL → the strategy class cannot avoid consecutive red years via regime timing, and
the operator's fork narrows to "size it down as a down-market specialist" or "drop the
class."

---

## 2. The hypothesis (singular)

> A full-stop BTC regime switch — force-close all open positions and halt new entries
> when BTC exits a SHORT regime (after a 3-day dwell guard) — flips Criterion 2
> (no back-to-back negative OOS years) from FAIL to PASS for a **majority** of the
> 9-cohort strategy class.

One hypothesis, judged across 9 cohorts as a robustness panel. **Not** 9 separate
shots — no single-cohort cherry-pick, no promotion off one passing cohort.

---

## 3. Mechanism

Reuses existing causal machinery — no engine changes:
- `tasks/regime_label.py` — causal SHORT/LONG/FLAT labeler (closes up to day *i* only).
- `tasks/regime_gate_backtest.sh` — episode-slicing driver (slices each symbol's 1m
  history at regime boundaries, runs `bin/backtest` per episode, sums P&L).
- `tasks/walk_forward.py` — 4-fold expanding-window walk-forward + criteria.

New behavior layered on top:

1. **Regime collapse to binary.** LONG and FLAT both collapse to **OFF**. The strategy
   is never long (long side dropped as unstable noise — breadth verdict 2026-06-08).
   So the regime is SHORT (trade) vs OFF (flat).

2. **Min-dwell guard, D=3 (fixed).** An OFF stretch must persist **≥3 consecutive days**
   before it counts as a confirmed regime exit. 1-2 day head-fakes are ignored (the
   regime stays effectively SHORT). This prevents whipsaw force-close+re-entry fee
   churn — the failure mode that killed Option C. D is **fixed at 3**, pre-registered,
   NOT swept (saves a degree of freedom).

3. **Force-close on confirmed exit.** On a SHORT→(confirmed-OFF) transition, every open
   position is force-closed at that day's price — same mechanism as the existing
   max-hold force-close, fired by regime instead of clock.

4. **Entry halt during OFF.** No new entries open while the regime is OFF.

5. **Resume in SHORT.** When the regime returns to SHORT, the live strategy trades
   normally.

**Mechanical contrast with the degenerate gate:** SHORT episodes become **hard-bounded
trading windows**. A position cannot survive past the end of its SHORT episode. Today's
gate let positions span episode gaps — that is exactly why it was degenerate; this test
removes that.

---

## 4. Parameters & grid

| Param | Values | Notes |
|-------|--------|-------|
| X (BTC threshold %) | {5, 10, 15, 20} | SHORT fires if BTC trailing-return over Y days ≤ −X% |
| Y (lookback days) | {7, 14, 30} | trailing-return window |
| D (min-dwell days) | **3 (fixed)** | confirmed-OFF persistence before force-close |

- Grid per cohort: 4×3 = **12 (X,Y) cells** × walk-forward. ~⅓ of today's run (Z/LONG
  axis dropped — no long side).
- Anchor: `data/anchor/BTCUSDT-1d.csv` (BTC was the cleaner anchor; breadth was NEGATIVE).
- Cost stack: live config — `--signal-tf 4H --fee-bps 10 --stop-slippage-bps 5
  --exact-fills --include-boundary`, per-cohort EMA/max-hold flags.

---

## 5. Cohorts (robustness panel)

9 cohorts, each a separate (X,Y) grid + walk-forward:

| cohort | config |
|--------|--------|
| live | 9/21 EMA, 504h (deployed) |
| alt5-15-336 | 5/15 EMA, 336h |
| alt5-15-504 | 5/15 EMA, 504h |
| alt5-21-504 | 5/21 EMA, 504h |
| alt7-14-504 | 7/14 EMA, 504h |
| alt10-30-504 | 10/30 EMA, 504h |
| alt12-26-504 | 12/26 EMA, 504h |
| alt21-50-504 | 21/50 EMA, 504h |
| bb20 | Bollinger 20/2.0σ, 504h |

---

## 6. Success criteria (locked)

Evaluated per cohort on the 4 OOS walk-forward folds (2023, 2024, 2025, 2026).
2026 is a partial calendar year — flagged but included.

- **Crit 2 (PRIMARY gate): no back-to-back negative OOS years.** "Negative" = net
  P&L < $0 for that calendar year (after fees+slippage). Same definition as the
  existing `walk_forward.py:no_consecutive_losing`.
- **Crit 3 (param stability):** ≤2 distinct (X,Y) picks across the 4 folds, same X.
- **Crit 1 (beat always-short baseline total): REPORTED, NOT a gate.** A switch that is
  flatter and lower-total but never back-to-back-red is a WIN by the operator's stated
  risk tolerance. We explicitly trade some upside for survivability.

### Panel-level PASS rule

Let **F** = the set of cohorts whose always-short baseline FAILS Crit 2 (has back-to-back
red OOS years). These are the only cohorts the switch can *help* — they are the
denominator. The switch **PASSES the class** iff:

> it flips Crit 2 FAIL→PASS on **a strict majority of F** (> |F|/2), AND every flipped
> cohort also satisfies Crit 3, AND no flipped cohort's switch introduces a NEW
> back-to-back-red pair that the baseline lacked.

Edge cases, locked:
- Baseline for each cohort = always-short over the **same episode boundaries** (so
  force-close geometry matches; isolates the switch effect).
- A cohort whose baseline ALREADY passes Crit 2 is **excluded from F** (not in the
  denominator) — the switch has nothing to fix there. Reported as "baseline-clean",
  but it must NOT regress (if the switch breaks a previously-clean cohort into
  back-to-back-red, that is a FAIL flag for the mechanism, reported prominently).
- **If F is empty** (no cohort has back-to-back red at baseline): the premise is void —
  the strategy class does not actually exhibit the failure mode on OOS data. Report this
  as "no problem to solve" and STOP; the switch is unnecessary, not validated.
- **No single-cohort promotion.** One cohort flipping means nothing. The fraction
  flipped-of-F is the verdict.

---

## 7. What we measure & report

Per cohort, per (X,Y):
- OOS per-year net P&L (switch) and (always-short baseline)
- Crit 2 pass/fail for switch AND baseline (the flip)
- Crit 3 pass/fail, picks
- Crit 1 total-P&L delta (switch − baseline), reported

Panel summary table:
- cohort | baseline Crit2 | switch Crit2 | in F? | flipped? | regressed? | Crit3 | total Δ
- Headline: **flipped-of-F fraction** (e.g. "5/7 failing cohorts flipped to clean") —
  the verdict. Plus any regressions on baseline-clean cohorts (a red flag).

Verdict doc: `results/regime_switch_fullstop_verdict_<date>.md`, indexed in
`results/INDEX.md`.

---

## 8. Deferred-conditional follow-ups (pre-registered, NOT run unless force-close passes)

Named now so they cannot become post-hoc fishing. Run **only if** the force-close panel
passes (≥5/9), as "can we keep the red-year fix and recover more total P&L?":

- **(a) Close-if-profitable exit:** on regime exit, close green positions, let underwater
  ones run to stop/target.
- **(b) Tighten-stops-on-exit:** on regime exit, ratchet stop to breakeven instead of
  force-closing.

If force-close FAILS the panel, (a) and (b) are NOT run — they are weaker variants of
the same mechanism and near-certain to fail too. DoF preserved.

---

## 9. Out of scope (locked)

- No engine changes. No live/shadow/VPS interaction. Research-only.
- No real-money implication. Forward-paper go/no-go resolves independently per
  `results/real_money_protocol_decision_rule_2026-05-08.md`.
- No promotion of any cohort off this test. A PASS produces a *candidate mechanism* for
  a separate productionization discussion, not a deploy.
- LONG side stays dropped. This test never goes long; OFF = flat.

---

## 10. Anti-overfitting discipline (why this is DoF-safe)

- One hypothesis (force-close), not three (variants deferred-conditional).
- D fixed at 3, not swept.
- Z/LONG axis dropped (12 cells/cohort vs today's 36).
- 9 cohorts as a **corroborating panel** (majority-flip), not 9 independent shots.
- Crit 3 (param stability) still gates the passing cohorts — an unstable (X,Y) that
  happens to avoid back-to-back-red in-sample does NOT count.
- Pre-registered before running. Verdict mechanically follows the rule.
