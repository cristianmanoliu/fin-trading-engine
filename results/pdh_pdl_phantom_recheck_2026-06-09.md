# PDH/PDL break — phantom-fix re-check (pre-reg + verdict, 2026-06-09)

**Status:** LOCKED BEFORE re-run. Contamination-audit closure. The only remaining
short-only non-EMA mode whose REFUTED verdict was produced on the phantom-long-buggy binary
(`d1d0fae` fix). Companion to `alt_signals_phantom_corrected_verdict_2026-06-09.md` (MACD/RSI).

## Scope justification — why PDH/PDL is the ONLY mode left to re-run

Of the 8 contaminated entry modes (Bollinger/RSI/MACD/Momentum/VWAP/PDH-PDL/Absorption/
Breakout), the side-filter phantom bug only mis-fires under `--side-filter short|long`
(never `both`, where longs are legitimate). Audit of filed verdicts:

| Mode | Filed verdict | Test side | Contaminated? | Disposition |
|---|---|---|---|---|
| RSI cross-50 | Inconclusive 05-06 | 4H short | YES | DONE — flipped to CANDIDATE +79% |
| MACD cross | REJECTED 05-07 | 4H short | YES | DONE — flipped to CANDIDATE +18% |
| Bollinger (bb20) | WEAK→shadow | 4H short | YES | DONE — corrected −$1.69M (shadow) |
| **PDH/PDL break** | **REFUTED 05-06** | **4H short** | **YES** | **THIS DOC — re-run** |
| VWAP-fade | REFUTED 05-06 | **both-sides** (`vwap_decision_rule_2026-05-06.md` ll.14-21) | **NO** (bug-immune) | clean, no re-run |
| Momentum | none (only "trigger=noise" collective) | — | n/a | no short-only verdict to correct |
| Absorption / Breakout | none under 4H/6:1/short | legacy 5m min_rr | n/a | no short-only verdict to correct |

So the contamination audit closes after this one mode.

## What this re-check does

2-cell walk-forward, fixed binary, production cost model (slip=5/fee=10/mh504/4H/short/
57 sym / 3 OOS windows W1 2023-05→2024-04, W2 2024-05→2025-04, W3 2025-05→2026-04):
- Cell 0: BASELINE (LIVE EMA 9/21 cross)
- Cell 1: PDH/PDL break (`--pdh-pdl-break-mode`)

## Pre-registered prediction

PDH/PDL is a momentum-breakdown trigger. Like MACD/RSI it likely fired phantom longs
under the buggy binary (PDH break = bullish, would have emitted LONG under short-only).
Removing them will change the number. **Prior:** PDH/PDL was qualitatively REFUTED (high
correlation to baseline / worse timing); on the fixed binary it most likely (a) still loses
to baseline, or (b) flips positive like MACD/RSI but remains non-diversifying (same
crash-dependent short-only profile). Falsifiable candidate trigger: beats baseline ≥10% on
≥2/3 windows → WALK-FORWARD CANDIDATE (still requires fresh pre-reg + overfit gate to
promote; charter freeze binds to ~2026-08-06).

## Decision rule (LOCKED)

| Result | Action |
|---|---|
| Loses to baseline ≥10% OR fails walk-forward | Confirm REFUTED at clean cost. Close audit. |
| Beats baseline ≥10% on ≥2/3 windows | File as CANDIDATE-pending-fresh-pre-reg (same status as MACD/RSI). Still NO deploy (overfit gate + freeze). |

**No deployment from this re-check regardless of result.** Identical discipline to the
MACD/RSI correction.

---

## Results — completed 2026-06-09

| Cell | W1 | W2 | W3 | Mean | Wins | Trades | vs LIVE |
|---|---:|---:|---:|---:|:---:|---:|:---:|
| BASELINE EMA 9/21 | +175,021 | +645,812 | +65,044 | **+$295,292** | 3/3 | 5,572 | reference |
| **PDH/PDL break** (post-fix) | **−419,690** | +3,201 | +34,325 | **−$127,388** | 2/3¹ | 11,028 | **−143%** |

¹ wins=2/3 counts windows with net>0 (W2/W3 marginally positive), but **mean is −$127k** —
fails the mean>0 half of the candidate rule → verdict NEGATIVE.

### Verdict — REFUTED confirmed at clean cost (did NOT flip)

Unlike MACD/RSI (whose phantom-long removal *raised* mean NET because real bear-window
losses were being stripped to reveal an underlying short edge), PDH/PDL on the fixed binary
is **decisively negative**: mean −$127,388, **−143% vs baseline**, dominated by a −$419,690
W1 collapse, on 11,028 trades (2× baseline — an over-firing momentum trigger with no
underlying short edge). The 2026-05-06 REFUTED verdict **holds and is quantified** at the
production cost model. PDH/PDL is not a candidate.

### Contamination detail (side-count, pre vs post binary, full 57-sym)

Full 57-sym side-count (`tasks/pdh_sidecount.sh`, pre-binary `d1d0fae^` vs post):

| Binary | LONG opens | SHORT opens |
|---|---:|---:|
| **PRE-FIX** | **16,624** | 15,889 |
| **POST-FIX** | **0** | 18,405 |

PDH/PDL was **heavily contaminated** — 16,624 phantom LONG opens under `--side-filter short`
(more than RSI or MACD), ~51% of pre-fix trades. (An earlier 3-symbol probe coincidentally
hit symbols with no bias=Long PDH break and showed 0/0 — the full universe is definitive.)
The fix zeroes them. **And PDH/PDL is STILL REFUTED at −$127k after removing all 16k phantom
longs** — the strongest possible refutation: even stripping a massive phantom-long inflation
can't lift it above baseline, because the underlying short signal over-fires (18,405 shorts,
3.3× baseline) with no edge. Contrast MACD/RSI, where stripping the phantom longs *revealed*
a real short edge that beat baseline.

### Decision rule applied

Result = "loses to baseline ≥10% / fails walk-forward" → **confirm REFUTED, close audit.**
No deployment. No candidate status.

### What this closes

This was the last contaminated short-only non-EMA mode. With MACD/RSI re-verified (flipped
to candidate, `alt_signals_phantom_corrected_verdict_2026-06-09.md`), Bollinger corrected
(−$1.69M), VWAP confirmed bug-immune (both-sides), and Momentum/Absorption/Breakout having
no filed short-only verdict, **the phantom-fix contamination audit is COMPLETE.** Net
outcome: 2 modes flip to (non-deployable) candidates (MACD/RSI), 1 stays refuted (PDH/PDL),
1 stays a non-promotable shadow loss (bb20). No new deployable edge surfaced; the overfit
gate (clean DSR 0.658) and charter freeze continue to bind.
