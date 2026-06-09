# Momentum (5m-candle) mode — first short-only walk-forward (pre-reg + verdict, 2026-06-09)

**Status:** LOCKED BEFORE run. Completeness pass on the phantom-fix contamination audit.

## Scope note — this is the ONE of the 3 untouched modes that can take a clean test

Of the 8 contaminated modes, after MACD/RSI (flipped→candidate), Bollinger (corrected
−$1.69M), PDH/PDL (REFUTED −$127k), and VWAP (both-sides, bug-immune), three were never
filed with a short-only verdict: **Momentum, Absorption, Breakout.**

- **Absorption + Breakout** are NOT standalone modes — they are the *default* `EntryDetector`
  path (the legacy PDH/PDL 5m level-reaction strategy, `min_rr`-based, both fire together off
  daily levels). Forcing them into the live 4H / fixed-6:1 / short harness produces a config
  that was never the strategy and never had a verdict — a meaningless number, not a
  correction. **Out of scope** (no contaminated finding to fix; not a 4H/6:1 candidate).
- **Momentum** (`momentum_mode`) IS a clean standalone toggle (enter on 5m momentum candles
  aligned with 4H bias). It had no CLI flag or harness branch (added 2026-06-09:
  `--momentum-mode` + `p4_fresh_oos.sh`/`walk_forward.sh` `MOMENTUM_MODE`). This is its
  **first** short-only walk-forward — exploratory, not a re-run of a prior verdict.

## What this does

2-cell walk-forward, fixed binary, production cost model (slip=5/fee=10/mh504/4H/short/
57 sym / 3 OOS windows): Cell 0 BASELINE (EMA 9/21), Cell 1 Momentum (`--momentum-mode`).

## Pre-registered prediction

Momentum is a high-frequency 5m trigger (the original "Option A"). Strong prior: like the
other momentum triggers (MACD/RSI/PDH) it over-fires and dilutes per-trade edge; most likely
**loses to baseline**. Because it post-dates the fix it emits 0 phantom longs by construction
(verified in smoke test: `--side-filter short` → only SHORT signals). Falsifiable candidate
trigger: beats baseline ≥10% on ≥2/3 windows (would still require fresh pre-reg + overfit
gate; charter freeze binds).

## Decision rule (LOCKED)

| Result | Action |
|---|---|
| Loses ≥10% OR fails walk-forward | File as exploratory NEGATIVE. Close completeness pass. |
| Beats baseline ≥10% on ≥2/3 windows | File as CANDIDATE-pending-fresh-pre-reg (same as MACD/RSI). Still NO deploy. |

**No deployment regardless of result.** Exploratory only.

---

## Results — completed 2026-06-09

| Cell | W1 | W2 | W3 | Mean | Wins | Trades | vs LIVE |
|---|---:|---:|---:|---:|:---:|---:|:---:|
| BASELINE EMA 9/21 | +175,021 | +645,812 | +65,044 | +$295,292 | 3/3 | 5,572 | reference |
| **Momentum 5m** | +82,509 | +592,159 | +882,251 | **+$518,973** | 3/3 | 12,639 | **+76%** |

### Verdict — POSITIVE, trips the candidate rule (but read the pattern below)

Momentum beats baseline +76% on 3/3 windows → by the locked rule it is a WALK-FORWARD
CANDIDATE. **But it is the THIRD high-frequency short trigger to flip/beat baseline on the
fixed binary, and the pattern matters more than the individual number:**

| Trigger (post-fix) | Mean vs LIVE | Wins | Trades | W3 (2025-26 bear) |
|---|---:|:---:|---:|---:|
| RSI-14 | +79% | 3/3 | 9,416 | +$667k |
| **Momentum 5m** | **+76%** | **3/3** | **12,639** | **+$882k** |
| MACD 12/26/9 | +18% | 2/3 | 8,531 | +$768k |
| LIVE EMA 9/21 | reference | 3/3 | 5,572 | +$65k |

All three over-fire (2–3× baseline trade count) and all three make their biggest gains in
**W3 (2025-26)** — exactly the bear window where *any* aggressive short trigger profits. The
live EMA's W3 is only +$65k; the momentum triggers' W3 is 10–13× that. **This is most likely
a regime/test-geometry artifact, not three independent edges:** a single favorable bear
window (W3) inflating every over-firing short signal, plus the multiple-comparison surface
that the overfit gate (clean DSR 0.658) already flags. The honest interpretation is that
high-frequency short triggers as a CLASS look good in this 3-window walk-forward — which is
a statement about the windows, not a discovery of edge.

**Candidate status (per locked rule) does NOT mean deployable.** It means "would require a
fresh pre-reg with an explicit correlation-vs-baseline check + overfit gate + independent
walk-forward before any shadow." Given (a) the clean overfit verdict still FRAGILE, (b) the
shared W3-windfall fingerprint, (c) the charter freeze to ~2026-08-06, Momentum is logged as
a candidate but is **not pursued now**. If pursued post-freeze, the first question is its
return correlation with RSI/MACD (likely high → not a diversifier, same as the other
momentum triggers).

### Per-year concentration (same crash-dependence fingerprint as RSI/LIVE?)

`tasks/verify_momentum_concentration.sh` (57-sym, 5y). Total $875,068 / 19,957 trades.
Symbol breadth fine (max 12.6%, 41 pos/16 neg — not one-symbol-driven). But per-year is the
**same crash-dependent fingerprint as RSI/LIVE, more extreme:**

| Year | NET | % of total |
|---|---:|---:|
| 2020 | −250,110 | −28.6% |
| 2021 | −492,268 | −56.3% |
| 2022 | +784,181 | +89.6% |
| 2023 | −624,670 | −71.4% |
| 2024 | +436,494 | +49.9% |
| 2025 | +1,021,441 | +116.7% |

Makes everything in 2022 + 2025 (crashes), **bleeds catastrophically in 2020-21 bull (−85%
combined) and 2023.** This is the live class's back-to-back-red-in-bulls flaw, amplified.
Confirms Momentum is the same always-short crash bet, not a diversifier — closing the
question the +76% headline raised.

### Absorption + Breakout — OUT OF SCOPE (not a correction, not a candidate)

These are not standalone modes: they are the legacy default `EntryDetector` path (PDH/PDL 5m
level-reaction, `min_rr`-based, fire together off daily levels). They had no short-only 4H/6:1
verdict to correct, and forcing them through the live harness (fixed 6:1, 4H signal) would
produce a config that never existed and means nothing. They are excluded from the
contamination audit by construction. If the legacy level-reaction strategy is ever revisited,
it must be tested in its OWN frame (5m, `min_rr`, PDH/PDL levels) — not this one.

### What this closes

The phantom-fix contamination audit + completeness pass is now fully done:
- **Flipped to candidate (non-deployable):** RSI +79%, Momentum +76%, MACD +18%
- **Stays refuted:** PDH/PDL −$127k
- **Stays a non-promotable shadow loss:** Bollinger/bb20 −$1.69M
- **Bug-immune (no re-run):** VWAP (both-sides)
- **Out of scope (no filed verdict / wrong frame):** Absorption, Breakout

Net for the strategy-class decision: **three momentum triggers beat the live EMA in
backtest, but they share one bear-window windfall, fail the clean overfit gate as a family,
and over-fire — so they reinforce, not weaken, the conclusion that backtest search is
exhausted and forward-paper is the only remaining arbiter.** No deployable edge surfaced.
