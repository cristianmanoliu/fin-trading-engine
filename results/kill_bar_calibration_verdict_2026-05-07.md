# Kill-Bar Calibration — VERDICT 2026-05-07

**Pre-registered:** `results/kill_bar_calibration_decision_rule_2026-05-07.md`
(committed `e76e5c0` — before any Monte Carlo runs).

**Verdict tier (mechanical application of the locked rule):** **NEEDS_RECALIBRATION**

## The headline finding

The forward-paper kill bar wired today (`scripts/forward_paper_status.sh`)
is **substantially mis-calibrated against the strategy's natural variance**.
Four of five testable criteria fail at the 90-day horizon under the locked
acceptable bands.

In plain language: if we deployed the strategy 1,000 times and it performed
exactly as the historical 5y backtest predicted, the current kill bar would
incorrectly KILL roughly **30-40% of those deployments** purely from
sampling variance — not because anything was actually wrong.

## Per-criterion results (90-day horizon, B=1,000 paths)

| Criterion | FP(null) | TP(deg50) | TP(dead) | Bands | Status |
|---|---:|---:|---:|:---:|:---:|
| KILL_PNL    | **31.7%** | 31.5% | 50.6% | ≤20 / ≥50 / ≥80 | ✗ NEEDS_RECAL |
| KILL_WR     | 0.6%      | 0.4%  | 0.3%  | ≤20 / ≥50 / ≥80 | ✗ NEEDS_RECAL |
| KILL_SYM    | **41.4%** | 41.0% | 100.0% | ≤20 / ≥50 / ≥80 | ✗ NEEDS_RECAL |
| KILL_HODL_C | **34.3%** | 37.0% | 63.0% | ≤20 / ≥50 / ≥80 | ✗ NEEDS_RECAL |
| KILL_HODL_W | 21.4%     | 20.1% | 28.3% | ≤20 / ≥50 / ≥80 | ≈ BORDERLINE |

(KILL_FEE and KILL_SLIP were excluded from this analysis — they only fire
on realized-vs-modeled cost divergence, which is identical-by-construction
in this simulation.)

## What's actually going on

### KILL_PNL — net negative at horizon

The strategy historically has 32% of 60-day windows and 32% of 90-day
windows with negative aggregate net PnL. This is normal variance for a
~14% breakeven WR strategy with 6:1 RR — most trades lose, a small
fraction win big, so any subset of trades has a thick negative tail.

**Killing on net-negative 60d would kill ~1 in 3 healthy deployments.**

Under "dead" (mean shifted to zero), KILL_PNL fires only 50.6% — barely
better than chance. The criterion has poor TP/FP separation overall.

### KILL_WR — win rate below 14%

WR is rarely evaluable at 60-90d (only 0.6-0% of paths reach the 150-trade
gate). When it does fire, it almost never trips: 0.6% under null and 0.3%
under dead.

This is because WR variance at small N is dominated by binomial noise
around the historical 20.6% mean — values below 14% are 2-3σ events. For
WR to discriminate "dead" from "alive," we'd need either much larger N
(thousands of trades, not 150) or a much higher kill threshold (e.g., 18%).

**WR is essentially uninformative as a kill criterion at the deploy
horizon.**

### KILL_SYM — single-symbol concentration > 40%

Single-symbol concentration above 40% is **the historical norm at 90d**,
not an outlier — fires in 41% of all randomly-chosen 90d windows under
null. With 16 symbols and high per-trade variance (one big winner can
swing aggregates), the 40% bar is too tight.

It DOES fire 100% under "dead," but only because dividing-by-near-zero
makes any nonzero symbol contribution look concentrated. The criterion
has perfect discrimination only when the denominator collapses, which is
a degenerate case.

### KILL_HODL_C — cumulative HODL delta < $0

The strategy underperforms BTC HODL ($32k notional) in 34% of historical
60-day windows. Under "dead," it underperforms in 63% — better than null
but not by enough to be a reliable kill signal.

The benchmark notional asymmetry (CLAUDE.md says $32k, current deployment
is $16k) is part of the issue: at $32k notional the bar is intentionally
high, which is fine for a deploy criterion but produces high FP if used
as a kill criterion.

### KILL_HODL_W — two consecutive 30d windows underperform by >$5k

This was the criterion I built today. It's the only one in the borderline
zone — FP(null) = 21.4% (just over the 20% bar). Bad news: TP(dead) is
only 28.3%, meaning **the criterion fails to detect dead strategies most
of the time.**

The reason: the kill requires *two* monthly windows underperforming HODL
by *>$5k each.* Under "dead" the strategy gives back HODL's drift, but
monthly drift on $32k notional has high variance — adjacent monthly
deltas don't both stay below -$5k 72% of the time.

The threshold of $5k against a $32k notional is too tight relative to
BTC's monthly volatility.

## Diagnostic context

Trade-count distribution under all scenarios at each horizon:

| Horizon | Mean trades | WR-eligibility (≥150 trades) |
|:---:|---:|---:|
| 60d  | 70  | 0.0% |
| 90d  | 105 | 0.6% |
| 120d | 137 | 48% |
| 180d | 210 | 89% |

**The CLAUDE.md "≥150 trades AND ≥60 days" deploy gate is internally
inconsistent.** At the historical trade rate (1.18/day), 60 days yields
only ~70 trades on average. To hit 150 trades requires ~127 days. The
two thresholds need to be reconciled.

## Pre-registered priors vs actual outcome

| Outcome | Prior | Actual |
|---|---:|:---:|
| WELL_CALIBRATED      | 30% | |
| NEEDS_RECALIBRATION  | 50% | **★** |
| BORDERLINE           | 20% | |

The 50% prior captured the verdict tier correctly. But the *magnitude*
of mis-calibration is worse than expected — I anticipated borderline
issues on perhaps one or two criteria, not 4-of-5 failing badly.

The remaining criterion (KILL_HODL_W) is borderline rather than
calibrated. So **zero of five criteria are well-calibrated**.

## What this UPDATES — and what it does NOT

### Updates (high confidence)

- The kill bar in `forward_paper_status.sh` produces too many false
  positives to be relied on for a real-money kill decision at 60d/90d
- The "≥150 trades AND ≥60 days" deploy gate is internally inconsistent
  with the historical trade rate
- WR as a kill criterion is uninformative at deployment horizons
- Single-sym 40% threshold is too tight for the actual fleet variance

### Does NOT update (per the locked rule)

The locked rule explicitly forbids fishing for replacement thresholds in
the same session as the calibration analysis. Specifically:

- ❌ Don't sweep KILL_SYM threshold {40, 50, 60, 70} to find one with
  acceptable FP rate now
- ❌ Don't reframe KILL_PNL as "net-negative across multiple horizons"
- ❌ Don't relax KILL_HODL_W to "one window underperforming"
- ❌ Don't change the benchmark notional from $32k mid-flight

Each of these is a separate hypothesis requiring its own pre-registration
in a follow-up milestone.

## What's NOT permitted under the original pre-registration

The following moves remain forbidden until a fresh pre-registration:

- Re-run with different B
- Re-run with different time horizons
- Re-run with different scenario definitions
- Tweak the bands post-hoc

## Recommendation for next session (not part of this verdict)

The verdict mechanically says NEEDS_RECALIBRATION. The follow-up question
is: do we recalibrate, or do we accept the kill bar will produce many
false-positive kills and use other signals (drift detector, manual
intervention) to confirm before actually killing?

Both are legitimate paths. The cost of recalibration is another
pre-registered analysis (~1 session). The cost of accepting the
mis-calibration is operating with a noisy bar that needs human
adjudication for every kill firing.

Either path requires the user's judgment. This verdict is the input,
not the decision.

## Files

- `results/kill_bar_calibration_decision_rule_2026-05-07.md` — pre-registration
- `results/kill_bar_calibration_2026-05-07.txt` — raw output
- `scripts/kill_bar_calibration.py` — analyzer
- `scripts/forward_paper_status.sh` — the kill bar being calibrated

## Status update

The full forward-paper kill bar from `forward_paper_status.sh` is logged
as **MIS-CALIBRATED, AWAITING RECALIBRATION DECISION**. The infrastructure
exists, the criteria match CLAUDE.md verbatim, but the calibration is the
issue — not the implementation. Implementation is correct.
