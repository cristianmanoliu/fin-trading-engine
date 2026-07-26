# Finding — Forward-paper resolution fired KILL (2026-07-26)

**Status: KILL verdict recorded. Milestone should be marked KILLED.**
**Operator action still required — see "What was NOT done" at the bottom.**

Locked rule: `results/forward_paper_outcome_resolution_decision_rule_2026-05-10.md`
Kill criteria: `results/real_money_protocol_decision_rule_2026-05-08.md` §Kill criteria
Artifacts: `results/decision_snapshots/2026-07-26-{kill,promote,resolution,rehearsal,lag}.txt`

## The verdict

The 06:00 UTC automated resolution emitted:

```
Live cohort:   n=116 trades / 78 days / pnl=$-5571.0
>>> VERDICT: KILL  (Rule 1)
```

**Real-money allocation was and is ZERO** (STAGE_0, Stub executor). No capital
was lost. The −$5,572 is paper.

## Which criteria fired

| # | criterion | threshold | actual | status |
|---|---|---|---|---|
| 1 | Drift detector confirmed fire (≥2 firings ≥7d apart) | <2 or <7d apart | PAIR: 2026-05-27 ↔ 2026-06-07 | **KILL** |
| 2 | Slip >30bp over last 30 losers | ≤30bp | 5.00bp | CONTINUE |
| 3 | 3 consecutive days each loss >5× stake | <3 days | no qualifying run | OPERATOR-VERIFY |
| 4 | Single-symbol >50% PnL | ≤50% | 9.4% (1000SHIBUSDT) | CONTINUE |
| 5 | Unrecoverable engine/exchange error | operator-verify | (deferred) | DEFERRED |
| 6 | Drawdown >20% over 60d | ≤20% | **63.5%** (peak $34,926 → trough $12,766) | **KILL** |

Two independent criteria fired. Either alone is sufficient under the locked rule.

## Verification performed before recording this

The 63.5% figure was checked against the code rather than taken at face value,
because a headline number that large is exactly the kind that turns out to be a
metric artifact (cf. the 3e16 benchmark in `fin-insider-edge` the same week).

`scripts/kill_protocol_check.py:check_drawdown` measures drawdown against **peak
cumulative PnL**, not account equity, and explicitly returns PENDING when
`peak <= 0`. Here peak was genuinely positive: cumulative PnL climbed to
**+$34,926**, fell to **+$12,766**, and now sits at **−$5,572**. The give-back
is real. The denominator is peak-profit, so "63.5%" means "gave back 63.5% of
peak paper profit" — severe, and correctly within the criterion's intended shape.

Promotion check independently agrees the run is not viable:

| gate | required | actual |
|---|---|---|
| Net-positive cumulative PnL | >$0 | **−$5,572** |
| PnL ≥60% pro-rated annual | ≥$8,841 | −$5,572 |
| Beats BTC-HODL ($16k notional) | >$0 vs HODL | strategy −$5,572 vs HODL −$3,147 (**Δ −$2,425**) |
| Drift clean ≥30d | ≥30d | 0d |

The strategy is net-negative on 78 days of forward paper and **loses to simply
holding BTC by $2,425**. Trade count (116) never reached the 150 floor.

## The HOLD-vs-KILL tension, and why KILL stands

There is an apparent contradiction that must be recorded, because a future
session will otherwise hit it and be confused:

- The **manual weekly drift cross-check** logged **HOLD — 12th consecutive
  censoring-benign reading** on 2026-07-26 (commit `3aa4d2d`).
- The **automated kill-protocol check** logged **KILL** the same morning.

Both are correct about different things:

1. The manual protocol carries a **standing prohibition** on running
   `run_drift_check.sh`, because "it mutates `drift_check_history.jsonl` and
   would manufacture a false 7d pair." The launchd cron
   (`weekly_audit.sh:142`) runs it every Sunday regardless, appending
   `DRIFT_FIRED` rows. Kill criterion #1 reads that file.

   **CORRECTION (added after auditing the file — the first version of this
   finding overstated the contamination).** The firing pair criterion #1
   actually keyed on is **2026-05-27 ↔ 2026-06-07**, and auditing every row by
   timestamp shows:

   - `2026-05-27T17:44:44Z` — **MANUAL** (not a 06:0x Sunday cron slot)
   - `2026-05-30T19:31:42Z` — **MANUAL**, also `DRIFT_FIRED`
   - `2026-06-07T06:00:05Z` — cron

   So a ≥7-day-apart pair (05-27 ↔ 06-07, and independently 05-27 ↔ 05-30 is
   <7d but 05-30 ↔ 06-07 is not) exists from **operator-initiated runs**, not
   only cron ones. Further, `git log -S "standing prohibition"` shows the
   prohibition text first entered the repo on **2026-07-25** — *after* both
   firings. The cron was not violating a rule that existed at the time, and it
   did not fabricate the pair.

   **Revised reading: criterion #1 is real, not an artifact.** The cron is still
   worth fixing (it writes decision-grade state on a schedule, which is fragile
   and makes future audits ambiguous), but it does not undermine this verdict.
2. **Criterion #6 is independent of all of that.** It reads trade PnL, not
   drift history. It fires on its own.
3. The automated detector's fire is on `loss_pnl_abs`: live losses average
   $1,110 vs backtest $1,065 — **+$45, or +4%**. Statistically significant
   (t=−6.61) only because losses are tightly clustered. That is exactly the
   "censoring-benign" reading the operator has diagnosed 12 times. Taken alone
   it would not justify a kill.

**Conclusion: the kill stands, and now on firmer ground than first stated.**
Criterion #1 turns out to be genuine (the pair includes manual runs and predates
the prohibition), and criterion #6 plus the plain economics — net-negative,
losing to HODL by $2,425, 63.5% give-back of peak — fire independently of it.
Even discarding criterion #1 entirely, the verdict is unchanged.

The manual HOLD readings are not wrong either: they assess whether the *drift
mechanism* indicates structural breakdown, and "censoring-benign" is a fair
reading of a +4% loss-size gap. But a strategy can be free of mechanism drift
and still simply lose money, which is what criterion #6 and the promotion gates
measure. Both instruments were reporting accurately about different questions.

## Why this was always the likely outcome

Context from the locked docs, not hindsight:

- Honest projection was ~$69k/yr at slip=25bp, with a walk-forward 95% CI of
  **[−$111k, +$372k]** — the interval always included substantial loss.
- Breakeven WR ≈ 14.3% against a backtest WR of 20.6% — only 6.3pp of cushion.
  Live WR came in at **12.4%**, below breakeven.
- `docs/RESEARCH_BACKLOG.md` had already closed the strategy-class search:
  every positive backtest finding was already deployed as a shadow.

## Consequences under the locked rule

Per `real_money_protocol_decision_rule_2026-05-08.md` §"When kill fires":

- Stop opening new positions
- Let existing positions close at their stops/targets — **do NOT panic-close**
- Mark the milestone **KILLED**
- **No automatic resumption.** Restarting requires a fresh next-milestone
  pre-registration with a revised protocol reflecting what the kill taught.

Also now moot: the Kraken/OKX-EU/Hyperliquid **executor port**. CLAUDE.md
recorded that port as the default real-money path if forward-paper promoted,
with the explicit note "a KILL verdict makes it moot." It is moot. Do not start
it.

## What was NOT done (deliberately)

**The 16 VPS engines were left RUNNING.** Stopping them is an outward-facing,
hard-to-reverse action on live infrastructure and was not authorized. It is also
not urgent: the executor is Stub (paper), so every additional trade costs
nothing but disk. The locked rule's "stop opening new positions" should be
executed by the operator when convenient:

```bash
ssh root@178.105.24.230 'systemctl stop "paper-live@*.service"'
ssh root@178.105.24.230 'systemctl disable "paper-live@*.service"'
```

Open positions should be allowed to close naturally first if the operator wants
trade-level economics preserved (the rule prefers this over mass closure).

**Recommended follow-up, needing operator judgement:**

1. Fix or disable the drift cron's mutation of `drift_check_history.jsonl`.
   It is writing rows the manual protocol explicitly forbids, and it
   contaminated kill criterion #1. Either make `run_drift_check.sh` append-only
   to a separate cron-scoped file, or point the launchd job at a read-only mode.
2. Decide whether a next milestone is worth pre-registering at all. The
   strategy-class search is documented as closed, the venue is region-blocked
   for real money (Binance EEA), and the honest CI always straddled zero.
