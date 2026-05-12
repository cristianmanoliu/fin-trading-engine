# KILL-vs-DEPLOY-FAIL taxonomy resolution

**Date locked**: 2026-05-12
**Resolves**: T13d operator decision from `docs/findings/2026-05-11-pm.md`
**Affects**: `scripts/forward_paper_status.sh` overall-verdict labeling +
the operator's mental model bridge to `auto_kill_execution_decision_rule_2026-05-08.md`

## The question

`scripts/forward_paper_status.sh` routed several deploy-criterion FAIL
states to messages prefixed `KILL —` despite CLAUDE.md having no
corresponding kill criteria:

- `s_fee == FAIL` (realized fee > 12bp) → "KILL — realized cost exceeds
  kill threshold"
- Generic disjunction (`s_pnl`, `s_wr`, `s_sym`, `s_hodl_cumul`,
  `s_slip` all FAIL) → "KILL — at least one criterion failed"

CLAUDE.md's kill criteria section lists only TWO explicit slip/HODL-
related kills:
- Realized slip > 25bp (historical cliff edge)
- Two consecutive 30-day windows underperforming BTC-HODL by >$5k each

Plus `scripts/live_vs_backtest_drift.py` exit code 1 (decision-grade
drift) — which the dashboard doesn't directly evaluate.

The mismatch caused operator-misleading messaging. At Layer 2 testnet
activation (when real Binance fees replace the modeled flat-rate Stub
costs), a thin-liquidity symbol's realized fee could legitimately land
at 13-15bp and trigger the "KILL — realized cost exceeds kill threshold"
message — which an operator following the dashboard would map to the
auto_kill_execution flow per the threshold-fire condition.

## Resolution

Five-class verdict taxonomy, with precedence in this order:

| # | Verdict | When | Operator action |
|---|---|---|---|
| 1 | **KILL — advisory** | s_slip_kill==FAIL (slip > 25bp) OR s_hodl_window==FAIL (2 consec 30d under HODL) | Confirm via drift detector before acting — these are CLAUDE.md kill criteria but advisory only; decision-grade is the drift detector |
| 2 | **DEPLOY-FAIL** | Any deploy criterion FAIL with NO kill criterion firing (fee>12bp, slip 20-25bp, pnl/wr/sym/cumul-HODL FAIL) | Investigate; do NOT auto-kill; promotion blocked until resolved |
| 3 | **WAITING (insufficient data)** | < 60d OR < 150 trades | Continue accumulation |
| 4 | **DEPLOY-READY** | All locked deploy criteria PASS | Reach for the STAGE_1 promotion checklist + drift detector |
| 5 | **WAITING** | Catch-all for intermediate non-failing non-passing states | No action |

Specific message strings:

- `KILL — advisory: realized slip exceeds 25bp historical kill edge`
- `KILL — advisory: two consecutive 30d windows underperform BTC-HODL`
- `DEPLOY-FAIL — realized fee exceeds 12bp deploy threshold (investigate; no fee kill criterion)`
- `DEPLOY-FAIL — realized slip in 20-25bp band (above deploy threshold, below 25bp kill cliff)`
- `DEPLOY-FAIL — investigate criteria failing (PnL / WR / single-sym / cumul-HODL)`
- `WAITING (insufficient data)`
- `DEPLOY-READY — all criteria met`
- `WAITING` (catch-all)

## Why this matters

The auto_kill_execution flow has a stake: real-money close-all is
operator-driven but Telegram messaging tied to dashboard verdicts.
Pre-D4 a 14bp fee at Layer 2 would have rendered "KILL —" on the
dashboard, surfacing in `paste-ready` operator snapshots like
`scripts/daily_status.sh`, and an operator following the dashboard
literally would have invoked the kill protocol — when the actual
decision is "deploy gate fails on cost; investigate why fees came back
higher than the modeled 10bp; decide whether to promote, retune, or
delay."

The five-class taxonomy makes the dashboard's verdict line directly
inform the right next action without requiring the operator to
mentally translate between dashboard verdicts and CLAUDE.md kill
criteria.

## What stays unchanged

- All `s_*` per-criterion PASS/FAIL/PENDING/INSUFFICIENT statuses.
- All threshold values.
- The kill criteria themselves (slip > 25bp, 2-consec-30d HODL).
- `scripts/live_vs_backtest_drift.py` decision-grade kill — unaffected.
- `scripts/forward_paper_resolution.py` LIMBO rule — unaffected
  (consumes structured numeric inputs, not the verdict string).
- The auto_kill_execution flow itself — unaffected; this is messaging
  alignment, not flow logic.

## Verbatim invariants for cron consumers

Three scripts grep for KILL/DEPLOY-READY/WAITING substrings:
- `scripts/daily_status.sh` (paste-ready snapshot)
- `scripts/weekly_audit.sh` (cron)
- `scripts/forward_paper_resolution.py` (LIMBO rule synthesis)

None grep for the SPECIFIC pre-D4 strings ("realized cost exceeds kill
threshold" / "at least one criterion failed") — only for the verdict-
class prefix. So the post-D4 taxonomy preserves the prefix grammar
(KILL / DEPLOY-READY / WAITING / DEPLOY-FAIL is the new class) without
breaking those consumers, EXCEPT for any operator-script that pinned
the exact post-bug language. None found in the repo.

## Test pins

`scripts/test_forward_paper_status.py::SlipThresholdSeparationTest`:
- Existing `test_slip_in_deploy_fail_band_does_not_fire_kill` renamed
  in spirit to pin "DEPLOY-FAIL — realized slip in 20-25bp band"
  message + assert no `KILL —` substring in the verdict line.
- Existing `test_slip_above_kill_threshold_fires_kill_cost_branch`
  renamed to `..._fires_advisory_kill` + asserts the new
  "KILL — advisory: realized slip exceeds 25bp" message.
- NEW: `test_fee_fail_routes_to_deploy_fail_not_kill` — fixture with
  realized fee 15bp + slip 5bp asserts the verdict is DEPLOY-FAIL on
  fee, NOT KILL.

## Forward-paper data continuity

Zero numeric change. Pure messaging alignment. The dashboard's per-
criterion PASS/FAIL outputs are byte-identical; only the overall
verdict line label changes.

## Lineage

- 2026-05-09: T13c first fixed s_slip dashboard/gate misalignment
  (HIGH severity — slip 22bp PASS on dashboard vs FAIL in gate).
- 2026-05-09: T13d caught the T13c regression where s_slip served
  two semantics; introduced s_slip_kill for kill-classification.
  Surfaced the "s_fee FAIL → KILL message" pre-existing labeling
  issue but deferred to a separate operator decision.
- 2026-05-11 PM: T13d surfaces this as operator decision #4.
- 2026-05-12: D4 resolution — five-class taxonomy with explicit
  KILL/DEPLOY-FAIL separation.
