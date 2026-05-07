# Drift Detector Time-to-Detection — VERDICT 2026-05-08

**Pre-registered:** `results/drift_detector_time_to_detection_decision_rule_2026-05-08.md`
(committed `428d61d` — before any simulation ran).

**Verdict tier (mechanical application of the locked rule):** **NEEDS_TUNING — too-noisy-null**

## Headline finding

The drift detector catches degradation **fast** (median 26 days under
"dead", far better than the 60-day bar) but **fires too often** under
null when run sequentially every day (cumulative FP=28% by 365 days,
exceeds the 20% bar). Operationally important: at the deployed daily-
cadence assumption, the detector produces ~1 false alarm every 3.5 years
on average — fine for advisory use, too noisy for auto-kill.

## Result vs locked decision rule

### Cumulative fire rate (% of paths fired by day X)

| Scenario | 30d | 60d | 90d | 180d | 365d |
|---|---:|---:|---:|---:|---:|
| **null** (FP) | 22.1% | 26.6% | 27.5% | 27.9% | **28.0%** |
| deg30 | 84.3% | 100.0% | 100.0% | 100.0% | 100.0% |
| deg50 | 85.5% | 100.0% | 100.0% | 100.0% | 100.0% |
| dead  | 83.1% | 100.0% | 100.0% | 100.0% | 100.0% |

### Detection-day distribution (paths that fired within 365d)

| Scenario | P25 | **P50 (median)** | P75 | P90 | Censored |
|---|---:|---:|---:|---:|---:|
| null  | 23d | 26d | 30d | 36d | **720/1000** |
| deg30 | 22d | 25d | 29d | 32d | 0/1000 |
| deg50 | 23d | 26d | 29d | 32d | 0/1000 |
| **dead**  | 23d | **26d** | 29d | 32d | 0/1000 |

### Locked-rule evaluation

| Band | Required | Observed | Status |
|---|---|---|:---:|
| FP(null, 365d) ≤ 20% | ≤ 20% | **28.0%** | ✗ |
| Median detection(dead) ≤ 60d | ≤ 60d | **26d** | ✓ |

→ **NEEDS_TUNING — too-noisy-null**

## Findings

### 1. Detection speed is excellent — far exceeds the bar

Under any of deg30/deg50/dead scenarios, **100% of paths fire by day 60**
and **84-86% fire by day 30**. Median detection day is uniformly **26d**
across all three degradation scenarios. The 60-day TP bar was easily
cleared.

This means: if the strategy genuinely degrades on day 0 of forward-
paper deployment, we'll know within a month with very high confidence.
That's a strong operational property.

### 2. The detector cannot tell deg30 from deg50 from dead

All three degradation scenarios produce essentially the same detection
distribution (median 25-26d, P90 32d). The detector is a binary
"working/not-working" signal, not a graded "degradation magnitude"
signal. This is fine for kill purposes but means it doesn't
discriminate severity.

### 3. Sequential testing inflates FP from 12% (single-shot) to 28% (365-day daily)

The drift detector calibration verdict at fixed N=50, α=0.001 found
FP=12.1%. Running daily over 365 days inflates this to cumulative 28%.

**Most FP accrues in the first 30 days** (22.1% by day 30, only +6pp
more over the next 11 months). Once a path "survives" the early window
under null, subsequent tests on the now-large sample are highly
correlated with prior tests and add little incremental FP.

### 4. The sequential-FP problem is bounded by run cadence

The simulation locked **daily** testing — the worst-case operational
cadence. Running less often reduces sequential testing inflation:

- Daily (365 tests/year): 28% FP measured
- Weekly (52 tests/year): not measured, but bounded above by daily; likely 10-15%
- Monthly (12 tests/year): not measured; likely 6-10%
- Per-N (every 50 new trades ≈ 42 days, 8-9 tests/year): not measured; likely 8-12%

These are conjectures — not pre-registered, not measured. But they
suggest the FP problem is largely a function of test cadence, not of
the detector itself.

## Pre-registered priors vs actual outcome

| Outcome | Prior | Actual |
|---|---:|:---:|
| DEPLOYABLE_AS_IS | 55% | |
| NEEDS_TUNING — too-slow-dead | 25% | |
| **NEEDS_TUNING — too-noisy-null** | **20%** | **★** |

The 20% prior on noisy-null was correct in tier. The magnitude (28% vs
the 20% bar) is moderate — not catastrophic, fixable by reducing
cadence or further tightening α.

## What this updates

### Locks in (high confidence)

- **The drift detector catches degradation fast** at the deployed
  α=0.001 operating point: median 26 days under any of deg30/deg50/dead.
- **Cumulative FP under daily sequential testing is 28% by day 365** —
  exceeds the locked acceptable bar of 20%.
- **The detector does NOT discriminate degradation severity** — same
  detection time across deg30, deg50, dead.

### Operational implications

The detector at α=0.001 deployed daily produces ~1 false alarm every
3.5 years on average (28% / year ≈ once per 3.6 years of daily
running). For PAPER-trading monitoring this is acceptable — a false
alarm just triggers investigation, no real-money loss.

For REAL-MONEY auto-kill, 28% per year is too noisy. Two paths:

- **Reduce test cadence**: run weekly or per-N instead of daily.
  Estimated FP drops to 8-15% per year.
- **Tighten α**: e.g., α=0.0001 reduces FP at the cost of slightly
  longer detection. Requires fresh pre-registration in a future
  milestone.

The locked rule explicitly forbids re-tuning in this milestone:

> If NEEDS_TUNING, the verdict identifies WHICH bar fails. These are
> diagnostics, not solutions. Re-tuning is a separate fresh
> pre-registration in a future milestone.

### Updates the deploy path

CLAUDE.md says "drift detector returns exit code 1 = decision-grade
kill, no further confirmation needed." Given the 28% daily-cadence FP,
this needs nuancing. The honest update:

- **Single drift firing**: investigation-grade signal. Cross-check
  against forward-paper PnL trajectory and base rates before acting.
- **Two consecutive drift firings 7+ days apart**: stronger signal —
  joint probability under null is much lower than 28%.
- **Drift firing + threshold-kill firing simultaneously**: very strong
  signal.

Or operationally: **don't cron the detector daily.** Run weekly.

## What's NOT permitted (per the locked rule)

- ❌ Re-running with α=0.0001
- ❌ Re-running with weekly cadence
- ❌ Re-running with different scenarios
- ❌ Reframing the bands

The verdict stands as NEEDS_TUNING — too-noisy-null. Re-tuning requires
a fresh pre-registration in a future milestone.

## Files

- `results/drift_detector_time_to_detection_decision_rule_2026-05-08.md` — pre-reg
- `results/drift_detector_time_to_detection_2026-05-08.txt` — raw output
- `scripts/drift_detector_time_to_detection.py` — analyzer

## Status update

Drift detector time-to-detection is logged as **NEEDS_TUNING —
too-noisy-null** at α=0.001 daily cadence. Detection speed is
EXCELLENT (median 26d, 100% by 60d under any degradation). Sequential
FP under daily cadence is 28%/year — too noisy for auto-kill but
acceptable for advisory use.

Operational recommendation (not part of the verdict, NOT
pre-registered, but flagged for the user):

- **Run the drift detector weekly or per-N**, not daily, to bound
  sequential FP.
- **Treat single firings as investigation-grade**, not auto-kill.
- **Two firings 7+ days apart, OR firing + threshold-kill match**, as
  strong signals.

A future milestone may pre-register a re-tuning (α=0.0001 or different
cadence). Within this milestone, the verdict stands.

## End of milestone backtest investigation set

Per the pre-registration's explicit commitment:

> No further analyses on the 2020-2025 historical dataset within this
> research milestone after the verdict for this analysis lands.

Today's backtest investigation set is now closed. Next data flow:
forward-paper accumulation, real-money small-tranche prep, operational
monitoring. The historical dataset has produced everything it can on
this strategy class.
