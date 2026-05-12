# Pre-registration amendment — BTC-HODL benchmark notional

**Date locked**: 2026-05-12
**Amends**: CLAUDE.md "## Forward-paper go/no-go criteria" — Deploy
criterion #5 ("Live PnL beats BTC HODL with $32k notional over the same
window")
**Resolves**: T13a operator decision from `docs/findings/2026-05-11-pm.md`

## What changes

Deploy criterion #5 notional default: **$32,000 → $16,000**.

Implementation sites updated in lockstep:

- `scripts/stage_promotion_check.py:BENCHMARK_NOTIONAL` default
- `scripts/forward_paper_status.sh:BENCHMARK_NOTIONAL` default
- `scripts/test_criterion_coverage.py:LOCKED['BENCHMARK_NOTIONAL']`
- Criterion-name display strings ("$32k notional" → "$16k notional")
- CLAUDE.md text

Operator-visible override path (`BENCHMARK_NOTIONAL` env var) is
preserved on both the gate and the dashboard for what-if analysis.

`scripts/kill_bar_calibration.py` and `scripts/kill_bar_recal.py` are
**deliberately NOT updated**: they produced the 2026-05-07 kill-bar
calibration verdicts at the historical $32k notional. Changing those
constants would invalidate the historical record retroactively.

## Why this is an amendment, not a relaxation

The locked spec lived at $32k because that was the notional when the
deployed-32 fleet shipped (32 symbols × $1k stake). Between fleet ship
(2026-05-04) and pre-reg lock (2026-05-08), the fleet shrank to
deployed-16 (16 symbols × $1k = $16k). The locked spec was NOT updated
when the fleet shrank — it carried forward from drafting.

Implication of leaving the criterion at $32k:
- Strategy trades $16k notional in altcoins.
- "Beat the benchmark" requires beating a 2× larger BTC-HODL position.
- Strategy is implicitly being asked to outperform 2× capital
  deployment.
- This is not an apples-to-apples comparison; it's a 2× higher bar
  driven by stale drafting, not by any locked decision.

The pre-registration-discipline principle (locked rules don't change
mid-cohort to make the gate easier) applies to substantive criteria,
not to drafting errors discovered before the gate fires. The drafting
error here is unambiguous: the spec was written one day after the
fleet downsized, and the carryover is documented in the drift findings
from 2026-05-11.

Apples-to-apples comparison: "$16k in this altcoin strategy vs $16k in
BTC-HODL over the same window." The bar this sets is *whether the
alpha thesis holds at all*, not whether it holds against 2× notional.

## What stays locked

- The COMPARISON itself stays: cumulative live PnL must beat cumulative
  BTC-HODL P&L over the same window, both at equal notional.
- The rolling-window kill criterion ("two consecutive 30-day windows
  underperform BTC-HODL by >$5k each") stays. The $5k threshold was
  pre-registered against $32k notional but is independent of notional
  per the absolute-dollar formulation; it doesn't auto-rescale to $16k.
  An open question for a separate decision: should the absolute
  threshold scale with notional? Out of scope for this amendment.
- All other deploy criteria (≥150 trades, ≥60 days, ≤12bp fee, ≤20bp
  slip, ≥60% pro-rated annual, ≤40% single-symbol concentration) are
  unchanged.

## Forward-paper data continuity

Forward-paper started 2026-05-05 with the deployed-16 fleet. The
notional change is retroactive in the sense that historical-PnL
comparison gets re-evaluated against the lower benchmark — but the
deployed strategy was always $16k, so the strategy-side P&L is
unaffected. Only the benchmark-side calculation changes.

Operator may continue to inspect the $32k comparison via
`BENCHMARK_NOTIONAL=32000 ./scripts/forward_paper_status.sh` and the
same env override on `stage_promotion_check.py` — useful for the
sanity check "would the gate have fired under the original $32k bar?"
in a post-promotion retrospective.

## Effective immediately

The default value change takes effect the next time any of the
benchmarking tools runs. Cron-driven `weekly_audit.sh` will pick it up
on the next firing (2026-05-17 09:00 UTC unless invoked manually).

## Lineage

- 2026-05-04: deployed-16 fleet selected (B3 cross-validation verdict)
- 2026-05-07: deployed-32 → deployed-16 reduction operational (live)
- 2026-05-08: real_money_protocol pre-reg locked with $32k carryover
- 2026-05-11 PM: T13a surfaces the spec/reality mismatch
- 2026-05-12: this amendment resolves the question to $16k.
