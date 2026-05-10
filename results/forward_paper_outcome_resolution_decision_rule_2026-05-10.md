# Forward-paper outcome resolution — locked decision rule (2026-05-10)

**Status:** LOCKED 2026-05-10, before forward-paper data forces an
ambiguous-outcome heuristic call. Closes the resolution gap that the
existing locked rules left open:

- `results/real_money_protocol_decision_rule_2026-05-08.md` defines
  kill criteria + STAGE_1 promotion gates (sharp-extremes locked).
- `results/auto_kill_execution_decision_rule_2026-05-08.md` defines
  HOW to execute a kill once triggered.
- `results/drift_detector_calibration_verdict_2026-05-07.md` makes
  drift detector the decision-grade kill mechanism.
- `results/drift_detector_time_to_detection_verdict_2026-05-08.md`
  locks weekly cadence + two-firings-7d-apart auto-kill rule.
- `results/kill_bar_recal_verdict_2026-05-07.md` makes threshold gates
  in `forward_paper_status.sh` advisory-only.

What's NOT locked: **the outcome at the calendar gate (60d) when at
least one input is ambiguous.** This rule fills that gap mechanically
so the operator's first day-60 decision is a rule application, not an
emotional improvisation.

## Why this rule exists now (2026-05-10)

Forward-paper started 2026-05-08. Day-60 lands ~2026-07-07. At the
fleet rate of 1.18 trades/day, expected n at day 60 ≈ 70 trades. The
locked promotion gate requires ≥150 trades AND ≥60 days; trades is
the binding gate. Earliest STAGE_1 reach ~2026-09-13.

This means **at day 60, the calendar gate trips but the trade gate
does not.** The locked promotion rule says "wait for both." The
locked kill rule says "fires only on specific bad signals." But what
about the operator psychologically at day 60 with n=70, NET=-$2k,
drift clean, costs at modeled? The locked rules say "keep waiting"
but the operator has no MECHANICAL framing for what KEEP WAITING
looks like — and 2026-05-10's emotional reaction to the n=9 numbers
showed that without a rule, the operator's gut weighs.

Locking the rule before the data forces it preserves discipline.

## Inputs the rule consumes

All inputs are computable from existing tooling:

1. `n_trades_live` — live cohort closed trades. From
   `forward_paper_status.sh` "Trades closed" row, live cohort.
2. `n_days_live` — calendar days since live cohort's first close. Same source.
3. `live_pnl_usd` — net live PnL. Same source.
4. `live_wr_pct` — live win rate. Same source.
5. `drift_state` — last `scripts/run_drift_check.sh` exit code.
   Read from `results/drift_check_history.jsonl` tail entry.
6. `drift_history_age_days` — days since last drift run. Catches
   silent cron death (per the audit pattern).
7. `realized_fee_bps`, `realized_slip_bps` — from journal cost
   decomposition. `forward_paper_status.sh` summary lines.
8. `single_sym_pct` — max-symbol concentration % of abs PnL.
9. `hodl_delta_usd` — strategy minus BTC-HODL benchmark.
10. `single_sym_pct_kill` — single-symbol > 50% (from real_money_protocol kill criterion).

## Verdict taxonomy

Each combination of inputs maps to exactly ONE of these five verdicts:

| Verdict | Action | Telegram tier |
|---|---|:---:|
| **CONTINUE** | Keep monitoring; no action | none (silent) |
| **WATCH** | Investigate flagged metric WITHIN normal cadence; no kill | INFO (next weekly digest only) |
| **PROMOTE** | All gates met; proceed to STAGE_1 per protocol | INFO (operator confirmation gate) |
| **KILL** | Locked kill criterion fired | CRITICAL |
| **OPERATOR_REVIEW** | Genuinely ambiguous; needs operator + locked-rule cross-check | WARN |

The taxonomy is intentionally coarse. Day-by-day variance must NOT
produce verdict thrash. Operator's mental load is reduced when the
verdict at day-N is one of five well-defined states.

## The decision tree (locked, mechanical)

Evaluated in the order below. **First match wins** — once a verdict is
emitted, no further rule applies.

### Rule 1: KILL precedes everything

Any of these fires KILL regardless of other state:

- `drift_state` indicates two firings ≥7 days apart (drift wrapper exit 4)
- `single_sym_pct >= 50` (real_money_protocol locked criterion)
- `realized_slip_bps > 30` sustained 30 trades (locked criterion)
- 3 consecutive days each net-loss > 5× stake (locked criterion)
- 20% drawdown over 60d window (locked criterion)
- Layer 3 verdict = THRESHOLD or SIGNAL_DIV after pre-promotion test (locked)
- Engine error unrecoverable (operational SOFT KILL escalates to HARD per real_money_protocol)

If any fires → KILL. Execution per `auto_kill_execution_decision_rule_2026-05-08.md`.

### Rule 2: Drift cron silently died → OPERATOR_REVIEW

`drift_history_age_days > 14` is a hard signal that the weekly cron
isn't firing. Without drift monitoring, the decision-grade kill
mechanism is unarmed. The operator must investigate (launchd, plist,
permissions, network) before any other forward-paper interpretation
holds weight. Until investigated, treat as OPERATOR_REVIEW —
**absence of drift firings is NOT confirmation of strategy health
when monitoring itself is dark.**

### Rule 3: Insufficient data → CONTINUE

If `n_trades_live < 50`, NO verdict other than CONTINUE applies.
Drift detector requires N=50 minimum (locked operating point);
threshold gates are advisory-only at any n; statistical power for
PnL/WR claims is essentially zero below 50. Anything else risks
over-fitting to startup variance.

This is the rule that handles 2026-05-10's emotional reaction to
n=9. **n=9 is CONTINUE. Period.**

### Rule 4: PROMOTE (locked from real_money_protocol)

ALL of these must be true:

- `n_trades_live >= 150`
- `n_days_live >= 60`
- `live_pnl_usd > 0`
- `live_wr_pct >= 14` (breakeven floor)
- `single_sym_pct < 40` (concentration cap)
- `realized_fee_bps <= 12` (cost cap)
- `realized_slip_bps <= 25` (cost cap)
- `hodl_delta_usd > 0` (BTC-HODL benchmark beat)
- `drift_state` = exit 0 (CLEAN) on the most recent firing

If all true → PROMOTE. Operator must additionally pass the staged
deployment protocol (Layer 2 testnet + Layer 3 7d shadow parity)
before flipping --executor binance_live. Promotion requires explicit
operator confirmation; this rule emits the GO signal, the operator
executes the actual flip per the staged protocol.

### Rule 5: WATCH (one or two soft signals)

If `n_trades_live >= 50` and exactly ONE OR TWO of these soft
signals fire:

- `drift_state` = exit 1 (single firing — investigation tier per
  drift_detector_time_to_detection_verdict_2026-05-08).
- `live_pnl_usd < 0` AND `n_days_live >= 30` AND `live_pnl_usd >= -2 × abs(MIN_TRADES × stake × 0.10)` (i.e., losses bounded at 2× the symmetric "10% of nominal-trade-deck" reasonability check; not at the locked 20% drawdown cliff).
- `realized_fee_bps > 11` AND `realized_fee_bps <= 12` (in the slack window approaching the cap).
- `realized_slip_bps > 22` AND `realized_slip_bps <= 25` (in the slack window approaching the cap).
- `hodl_delta_usd < 0` AND single 30d window (not two consecutive).
- `single_sym_pct >= 40` AND `single_sym_pct < 50` (between concentration cap and kill).

Each is a flag worth INVESTIGATING but not killing. Multiple flags
firing simultaneously DOES NOT escalate to KILL — that's what the
sharp criteria in Rule 1 are for. Investigation cadence: weekly
digest, not real-time.

### Rule 6: OPERATOR_REVIEW (genuinely ambiguous)

If `n_trades_live >= 50` and ANY of these hold:

- THREE OR MORE soft signals from Rule 5 fire simultaneously.
- `n_days_live > 90` AND `n_trades_live < 100` (significantly
  below the expected fleet rate; either the strategy fires
  rarer than backtest predicted, or symbols are dropping
  out — both warrant investigation before continuing).
- `n_days_live >= 60` AND `live_pnl_usd > 0` AND `live_pnl_usd < (0.30 × pro_rated_expected_pnl)` where
  `pro_rated_expected_pnl = $69k × n_days_live / 365` (slow-bleed: positive but
  <30% of pro-rated expectation; not a kill but the locked rule
  explicitly defers the promote-or-extend call to operator).
- Live cohort meets PROMOTE criteria but a shadow cohort has
  significantly outperformed (live PnL ≥ 0 but bb20 / alt5 PnL >
  2× live PnL with comparable trade count): the locked rule says
  promote LIVE, but operator may consider whether this milestone
  should close + a new milestone with the better cohort opens.
- Confidence interval check: simple z-test on `live_pnl_usd` against
  bootstrap CI's lower bound from real_money_protocol. If observed
  PnL falls below CI lower bound after n>=150, the backtest distribution
  may no longer apply — operator review needed (drift detector should
  have caught this; if it didn't, that's the OPERATOR_REVIEW signal).

OPERATOR_REVIEW is NOT a kill. It's a "rule application produces no
clear answer" outcome that demands the operator cross-check the
drift detector, examine the source data manually, and document a
written rationale before either CONTINUE-ing or escalating.

### Rule 7: Default → CONTINUE

If no rule above fires, the verdict is CONTINUE. Wait. Re-evaluate
weekly per the locked drift cadence.

## Mechanical implementation (deferred)

This rule is intentionally NOT yet wired into automation. Operator
runs it manually at the natural cadence trigger points (day 30, 60,
90, 150). When forward-paper crosses n=50 (the first point any rule
beyond CONTINUE could fire), a future commit may add
`scripts/forward_paper_resolution.py` that consumes the inputs above
and emits the verdict mechanically. Current state is "rule locked, no
script yet" — the lock itself is the constraint.

## Telegram tier mapping (for future automation)

When the resolution script ships, exit code → Telegram tier:

| Verdict | Exit code | Telegram tier |
|---|:---:|---|
| KILL | 4 | CRITICAL "STOP THE PROTOCOL — kill criterion fired: <which>" |
| OPERATOR_REVIEW | 3 | WARN "Forward-paper review needed: <which rule>" |
| WATCH | 2 | INFO "Flagged metric: <which>; no kill, weekly investigation" |
| PROMOTE | 1 | INFO "All gates met — STAGE_1 promotion eligible" |
| CONTINUE | 0 | (no Telegram alert; output to operator dashboard only) |

Audit-pattern dual sense: the future script's exit-code taxonomy must
NOT collapse a KILL trigger into the same tier as CONTINUE on a
parser/exception failure. Apply the lens (lessons from
audit_pattern_2026-05-09) when implementing.

## What this rule is NOT

- Not a substitute for the drift detector. Drift remains the
  decision-grade kill mechanism.
- Not a hot-path: evaluated weekly during the operator's natural
  cadence (post-cron review of `forward_paper_status.sh` +
  `realized_cost_trajectory.py` + `forward_paper_trajectory.py`).
- Not a permission to override locked criteria. If Rule 1 or Rule 4
  fires, the action is mechanical. OPERATOR_REVIEW exists for the
  middle ground; it does NOT extend to relitigating sharp criteria.
- Not a substitute for written rationale. OPERATOR_REVIEW outcomes
  MUST be documented in a session findings doc before any action.

## Lock-in commitment

This rule is locked 2026-05-10 BEFORE the data forces an ambiguous
call. Modifications mid-milestone require:

1. Explicit identified gap that this rule's branches don't cover.
2. Documented rationale in a new locked-rule doc that supersedes
   this one (date-stamped + linked from this doc).
3. The new rule applies prospectively only — not retroactively to
   already-resolved cases.

The discipline is the asset. The audit pattern memo at 34 fail-opens
shows the lens working. This rule extends the lens to behavioral
rules: pre-register the response BEFORE the data, so the data can't
force a bad heuristic call under emotional load.
