# Cat G — F1 ∧ F2 Composite Filter — PRE-REGISTERED Decision Rule

**Status:** Locked before any composite backtest runs. Same discipline as
F1, F2, sample-split bootstrap, kill-bar calibration, drift-detector
calibration, edge stability.

This pre-registration is the **next-milestone hypothesis** explicitly
called out in both predecessor verdicts:

- F1 verdict: "Combining them is explicitly forbidden under the current
  pre-registrations but is the natural next-milestone hypothesis."
- F2 verdict: "Combining with F1 — that's a separate composite hypothesis,
  requires its own pre-registration in the next milestone."

The pre-registration is committed today (2026-05-07) so the rule is
locked in time. Execution is permitted as soon as the next milestone
opens — that is, when the user explicitly chooses to investigate G.

## Question

F1 (funding cross) and F2 (co-cross filter) each landed at WALK-FORWARD
CANDIDATE in their individual tests (alive-but-modest, +$18k/yr and
+$49k/yr respectively, both r<0.5 vs deployed). They detect orthogonal
mechanisms: F1 catches positioning extremes, F2 catches regime-shift
moments.

**The composite hypothesis under test:** when both conditions co-occur,
the deployed strategy's signal quality is highest. Filtering deployed
signals to only those where BOTH F1 and F2 fire at the candle-close
moment should yield higher NET-per-trade — the joint mechanism is
"extreme positioning during a regime shift."

## Composite design (LOCKED — D out of {A, B, C, D})

**Design D: F1 ∧ F2 as a FILTER on the deployed strategy.**

For each deployed-strategy signal (4H short EMA9/21 with mh504, RR6,
side-filter=short, fee=10/slip=5/funding=historical):

- Keep signal **only if both** of the following hold at the candle close:
  - **F1 condition**: 8h funding rate × 3 × 10000 > +30 bp/day (overcrowded
    longs reverting; locked threshold from cat_f1)
  - **F2 condition**: ≥5 other symbols in deployed-16 had a same-direction
    EMA cross within ±60 seconds of this candle close (locked K and
    tolerance from cat_f2)

- Discard otherwise.

Backtest the filtered signal stream through the same cost stack as the
predecessors. Compare to UNFILTERED deployed (A1's 2,210 trades / +$130k/yr).

### Why Design D and not A/B/C

- **A (F1 ∧ F2 as standalone signal generator)**: testable but mechanism
  story is murky. F1 alone is sparse (~150-200 fires/year). Intersecting
  with F2 would slash to single digits — not statistically resolvable.
- **B (F1 ∨ F2 in parallel)**: not a joint test, just two strategies running.
  Doesn't test the mechanism interaction we care about.
- **C (F1 ∨ F2 as filter on deployed)**: more permissive, similar to D but
  weaker. If the joint mechanism is real, the intersection is the cleaner
  test.
- **D (F1 ∧ F2 as filter on deployed)**: tests "joint mechanism amplifies
  deployed's edge" directly. Has known reference (deployed unfiltered).
  Lower trade count but each trade is the cleanest expression of the
  composite mechanism. **Selected.**

## Method

Walk-forward 6-window evaluation, identical setup to F1 and F2:

| Window | Range |
|:---:|:---|
| W-2 | 2020-05 → 2021-04 |
| W-1 | 2021-05 → 2022-04 |
| W0  | 2022-05 → 2023-04 |
| W1  | 2023-05 → 2024-04 |
| W2  | 2024-05 → 2025-04 |
| W3  | 2025-05 → 2026-04 (true-OOS, may be partial) |

Per window:
- Compute filtered NET (G), unfiltered NET (A1's deployed strategy),
  filtered trade count, filtered NET-per-trade
- Aggregate across windows for the verdict

Universe locked at deployed-16. No symbol sweeping permitted.

## Decision rule (LOCKED)

| Tier | Conditions (ALL must hold) |
|---|---|
| **DEPLOY-CANDIDATE**     | ≥5/6 windows positive AND mean annual NET ≥ $50k/yr AND filtered NET ≥ unfiltered NET |
| **SHADOW DEPLOY**        | ≥4/6 windows positive AND mean annual NET ≥ $30k/yr AND filtered NET/trade ≥ 1.5× unfiltered |
| **WALK-FORWARD CANDIDATE** | ≥3/6 windows positive AND mean > 0 AND filtered NET/trade > unfiltered |
| **REJECT**               | else |

## What is NOT permitted

- ❌ No sweeping the F1 threshold (locked at 30 bp/day per cat_f1)
- ❌ No sweeping the F2 K threshold (locked at 5 per cat_f2)
- ❌ No sweeping the time tolerance (locked at ±60s per cat_f2)
- ❌ No alternative composite operators — D is locked
- ❌ No re-running on different universe (deployed-16 only)
- ❌ No alternative cost stack (10/5/historical only)
- ❌ No alternative max-hold or RR
- ❌ No "saving" REJECT outcomes by relaxing decision tier conditions
- ❌ No combining with other Cat F findings (e.g., F1 ∧ F2 ∧ slip-cliff)

A REJECT outcome is the verdict; it is NOT a license to sweep thresholds
looking for a flattering composite. If REJECT, the F1 ∧ F2 hypothesis is
falsified at this design and may be re-tested only as a NEW hypothesis
(e.g., different operator or different gating mechanism) with its own
fresh pre-registration.

## Pre-registered priors

| Outcome | Prior |
|---|---:|
| DEPLOY-CANDIDATE         | 5%  |
| SHADOW DEPLOY            | 20% |
| WALK-FORWARD CANDIDATE   | 35% |
| REJECT                   | 40% |

Reasoning: F1 alone was modest ($18k/yr, 3/6 wins); F2 filtered alone was
modest ($49k/yr, 4/6 wins). The intersection is mechanically appealing
(joint conditions = highest quality) but mathematically restrictive — F1
fires ~150-200 times/year and F2 retains 33% of trades, so the composite
filter probably keeps 10-30 trades/year on the universe. Low trade count
means high per-trade variance; statistical resolution is the bottleneck,
not mechanism reality.

The 40% prior on REJECT reflects this resolution risk. The 35% on
WALK-FORWARD CANDIDATE captures the most likely outcome: real but small
edge, swamped by variance at the joint operating point.

## When to execute

This pre-registration is committed today but the analysis is **not run
in the current milestone.** Execution is conditional on the user opening
a new milestone (e.g., post-forward-paper-validation or via explicit
ask). When that happens, the rule fires mechanically with no further
design choices.

The user may ALSO choose never to execute — pre-registration doesn't
obligate execution. Its purpose is to lock the design *if* the
hypothesis is tested, so post-hoc tuning is impossible.

## Files

- `results/cat_g_f1xf2_composite_decision_rule_2026-05-07.md` — this file
- `scripts/cat_g_f1xf2_composite.sh` (or .py) — to be added when executed
- `results/cat_g_f1xf2_composite_<exec_date>.txt` — raw output (when run)
- `results/cat_g_f1xf2_composite_verdict_<exec_date>.md` — verdict (when run)

## Cross-references

- `results/cat_f1_funding_cross_decision_rule_2026-05-07.md` — F1 pre-reg
- `results/cat_f1_funding_cross_verdict_2026-05-07.md` — F1 verdict (WFC)
- `results/cat_f2_cocross_confluence_decision_rule_2026-05-07.md` — F2 pre-reg
- `results/cat_f2_cocross_confluence_verdict_2026-05-07.md` — F2 verdict (WFC)
- `results/bootstrap_ci_verdict_2026-05-07.md` — A1 reference for unfiltered NET
