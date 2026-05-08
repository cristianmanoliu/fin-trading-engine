# Per-symbol pause/kill — locked decision rule (2026-05-08)

**Status:** LOCKED 2026-05-08, before any per-symbol action has been taken. Complements the fleet-wide auto-kill rule (`results/auto_kill_execution_decision_rule_2026-05-08.md`) by handling cases where the appropriate response is "pause ONE engine" rather than "end the milestone."

## Question

When does the operator pause or kill a SINGLE engine without affecting the rest of the fleet, and when does that action escalate to fleet-wide? Without a locked rule:
- **Drift toward reshuffling:** the operator might quietly remove a "losing" symbol mid-milestone, which is exactly the look-ahead-driven selection the milestone discipline forbids ("Don't reshuffle the deployed-32" — CLAUDE.md `## Things to NOT do during forward-paper`). Once you start, the discipline is broken.
- **Drift toward over-killing:** an isolated symbol-level issue (exchange halt, regulatory action) could trigger an unnecessary full-fleet kill, ending the milestone for a fixable cause.

The lock distinguishes ALLOWED operational/regulatory pauses from FORBIDDEN performance-driven removals.

## The discipline tension — resolved

CLAUDE.md `## Things to NOT do during forward-paper` says:
> Don't reshuffle the deployed-32. Look-ahead is in backtest test_NET, not forward data.

**This rule prohibits PERFORMANCE-DRIVEN symbol changes.** Removing a symbol because it's been a recent loser introduces survivor bias into the remaining set's apparent performance — exactly the look-ahead pattern the discipline forbids.

**This rule does NOT prohibit OPERATIONAL/REGULATORY pauses.** A symbol whose exchange has halted trading, or whose regulatory status has changed, has a non-performance cause. Pausing it does not contaminate the fleet's performance signal — the symbol is removed from the data not because it lost money, but because its market structure changed.

**The mechanical distinction:** is the cause attributable to (a) the strategy's interaction with the symbol's price action, or (b) external context unrelated to strategy performance?

- (a) → reshuffling forbidden, pause forbidden
- (b) → operational pause allowed, with documented justification

## Trigger taxonomy

The five trigger types and which response category they map to:

| # | Trigger | Cause type | Action allowed |
|---|---|:---:|:---:|
| 1 | Binance halts trading on the symbol (announced) | (b) external | PAUSE — operational |
| 2 | Regulatory action on the underlying token (delisting risk, sanctions) | (b) external | PAUSE — regulatory |
| 3 | Per-engine restart-loop / config error (engine unhealthy, not strategy) | (b) external | PAUSE — operational |
| 4 | Symbol shows >50% PnL concentration over a rolling window | (a) performance | **FORBIDDEN** — fleet-level kill is correct response |
| 5 | Symbol's specific funding rate has gone unmodelable (e.g., funding-CSV is structurally broken for one symbol) | (b) external | PAUSE — data integrity |

The distinction matters most for trigger #4: it is an INDICATOR of fleet-level concern but is NOT a justification for per-symbol removal. Per-symbol removal here would be reshuffling.

## Locked decision rule

### When per-symbol pause is ALLOWED

ALL of the following must be true:
1. The trigger maps to category (b) external cause per the taxonomy above.
2. The cause is **documented** (link to Binance announcement, regulatory notice, error log signature, etc.).
3. The cause is **verifiable**: an independent second source can confirm it.
4. The pause is **time-bounded**: SOFT (resumable when cause resolves) per the auto-kill execution rule. If the cause turns out to be permanent (e.g., delisting), escalate to per-symbol HARD: archive that symbol's journal under `archive/<symbol>_<reason>_<date>/` and remove from the active deployed list with an explicit `results/per_symbol_kill_<date>_<symbol>.md` artifact.
5. The fleet's deployed-set FOR ANALYSIS PURPOSES is unchanged — when computing forward-paper criteria, the paused symbol is treated as "would have traded but for external cause," not as "removed from set." Concretely: trade-count and net-PnL are computed across the originally-deployed-16 set; the paused symbol contributes whatever its journal had through the pause point, plus zero from then on.

### When per-symbol pause is FORBIDDEN

ANY of the following makes pause forbidden — fleet-wide kill is the correct response (or no action, depending on severity):
1. The trigger is performance-based (the symbol is losing money).
2. The trigger has no documented external cause.
3. The justification reads "this symbol was always going to underperform" — that is reshuffling-via-rationalization.
4. Multiple symbols with similar losses are NOT also being paused — selection bias. Either ALL bad performers get paused (which IS reshuffling) or NONE do.

## Execution sequence

For an ALLOWED pause:

### Phase 1: HALT the single engine

```bash
# Identify the symbol (lowercase for systemd unit name)
SYM=imxusdt  # example

# Stop the single engine
ssh root@178.105.24.230 "systemctl stop paper-live@${SYM}.service"

# Verify it stopped
ssh root@178.105.24.230 "systemctl is-active paper-live@${SYM}.service"
# Expected: inactive
```

### Phase 2: Open positions on this symbol

The single-engine pause should NOT close other engines' open positions. The per-symbol Stub may have its own open position(s) across cohorts (live + 3 shadows). Apply the same logic as fleet-wide auto-kill Phase 2:

- Paper, SOFT (cause is fixable, intent is resume): natural close (Path A).
- Paper, HARD (cause is permanent, e.g., delisting): synthetic close at last available price (Path B).
- Real-money, any tier: market close (Path C).

The other 15 engines continue undisturbed.

### Phase 3: Document

**Required artifact:** `results/per_symbol_pause_<YYYY-MM-DD>_<symbol>.md`

```markdown
# Per-symbol pause — <date> — <SYMBOL>

**Tier:** SOFT (resumable) | HARD (permanent — implies milestone-2 deployed-set adjustment)
**Trigger type:** <1, 2, 3, or 5 from taxonomy>
**Cause:** <text>
**Documentation source:** <URL or quote>
**Independent verification:** <text>
**Pause time (UTC):** <timestamp>

## State at pause

- Cumulative trades on this symbol: <n>
- Cumulative NET on this symbol: $<usd>
- Open positions across cohorts at pause: <count by cohort>
- Open-position disposition: A natural | B synthetic | C market

## Resumption criteria (SOFT only)

- Cause resolved: <specific verifiable condition>
- Verification method: <how to confirm>
- Expected timeframe: <date or "unknown">

## Discipline check

- [ ] Trigger is category (b) external (NOT performance-driven)
- [ ] Cause is documented with source
- [ ] Cause is independently verified
- [ ] Other symbols with similar losses are NOT being paused (no selection bias)
- [ ] Fleet analysis treats paused symbol as "would have traded" not "removed"
```

### Phase 4: Update operational tooling

`scripts/lib/symbols.sh` (the canonical deployed-list source) MUST NOT be edited for SOFT pauses — the symbol is still deployed, just temporarily silent. For HARD pauses, edit MAY happen per migration trigger #1 below.

`scripts/forward_paper_status.sh` and `scripts/post_deploy_check.sh` automatically inherit the deployed list. For a SOFT pause, the paused engine will show up as `inactive` in post_deploy_check — this is EXPECTED and not a warning.

For SOFT pause, add a short note in CLAUDE.md `## Strategy status` flagging the paused symbol with a link to the artifact. Remove the note when the pause is resumed.

### Phase 5: Resume (SOFT only)

When the resumption criterion fires:

```bash
ssh root@178.105.24.230 "systemctl start paper-live@${SYM}.service"
./scripts/post_deploy_check.sh
```

Verify the engine is firing ticks within 5 minutes of restart. Update the artifact's "Resumption" section with the actual resume timestamp.

## Edge cases — pre-locked

### Multiple symbols hit by the same external cause

E.g., Binance announces simultaneous delisting of 3 symbols in our deployed set. PAUSE all three, document them in a single artifact (`per_symbol_pause_<date>_multi.md`), and verify NONE of the three are being selectively paused based on performance — they should all hit the trigger criterion identically.

### Performance trigger AFTER an operational pause has been documented

If a symbol is operationally paused, then while paused its remaining cohorts (the shadow strategies on that symbol) start producing data we'd consider performance-relevant: this is a confound. Document that the paused-symbol's data from the pause-point onward is COMPROMISED for analysis purposes.

### Operational pause that becomes permanent

If a SOFT pause's resumption criterion can't be met within 30 days (e.g., delisting becomes permanent), escalate to HARD per-symbol kill:
- Archive that symbol's journal to `archive/<symbol>_permanent_<date>/`
- Update CLAUDE.md `## Strategy status` to remove from deployed list
- Edit `scripts/lib/symbols.sh` `deployed` list (this is the ONE permitted edit during a milestone, with explicit per-symbol HARD justification)
- Update the milestone's deployed-set documentation to note the legitimate removal
- Future analysis treats the symbol's pre-removal data as in-sample but no post-removal data is generated

### Performance trigger fires but operator wants to PAUSE anyway

This is the discipline-violation case. The locked rule says NO. If the operator wants to override, they MUST:
1. Document the trigger as performance-driven in the artifact.
2. Document a structured justification — what new information would justify breaking discipline.
3. Acknowledge in writing that this is a discipline violation and the milestone's results are now contaminated for the affected symbol going forward.
4. Proceed at their own audit risk.

The locked rule cannot prevent a determined override; it can only document that the override happened.

### Single symbol's engine is in restart-loop but the cause is unclear

Default to SOFT pause with trigger=#3 (operational). Resume when the engine state is clean (post_deploy_check shows it healthy on restart). If the restart-loop is caused by the SYMBOL'S DATA (e.g., a malformed kline causes a parser crash), document the parser bug — the pause is operational and resumption depends on the parser fix, not on the symbol's price action.

## Alternatives considered (rejected)

### Allow performance-driven per-symbol pauses with a "research mode" justification

**Considered:** the operator might want to pause a high-loss symbol to "study why it's failing" without ending the milestone.

**Rejected because:** "studying" mid-milestone is exactly the look-ahead pattern. The right time to study is in the postmortem after kill, or in milestone 2 with new pre-registration. Allowing it during the milestone breaks discipline.

### Always escalate to fleet-wide kill (no per-symbol path)

**Considered:** simplicity — every issue is a fleet-wide kill.

**Rejected because:** an exchange halt on ONE token is an external event that doesn't degrade the strategy on the other 15. Forcing a fleet-wide kill in that case is over-reaction with cost (lost milestone time, postmortem effort). Per-symbol pause is the right granularity for external causes.

### Operator-judgment on the (a) vs (b) cause attribution

**Considered:** rely on operator wisdom to distinguish performance-cause from external-cause.

**Rejected because:** under stress, operators rationalize. A losing symbol can be reframed as "having unique microstructure issues" if you squint. The locked rule requires DOCUMENTED external cause with INDEPENDENT VERIFICATION — these are mechanical tests that survive bias.

## Migration triggers

The rule re-opens for design when:

1. **First HARD per-symbol kill occurs** — e.g., delisting becomes permanent. Validates the migration path from SOFT to HARD; verify the deployed-list edit step is clean.
2. **First operator override of a performance trigger** — re-evaluate whether the discipline was properly applied; if the override turned out justified, weaken the rule's blanket prohibition; if not, tighten the documentation requirements.
3. **STAGE_3 promotion** — Phase 2C (real-money market close on per-symbol pause) becomes routine; the rule may need a dedicated real-money sub-section for partial-position handling.
4. **Multi-strategy fleet** — if architecture grows beyond 1-live-3-shadow, the per-symbol pause may need to specify which COHORTS pause vs continue (currently the rule treats the whole engine — all 4 cohorts — atomically).

## Cross-references

- `results/auto_kill_execution_decision_rule_2026-05-08.md` — fleet-wide kill (this rule's superset response)
- `results/drift_firing_investigation_decision_rule_2026-05-08.md` — TRIAGE-B step 6 (symbol concentration check) flags candidates for review but does NOT itself trigger a per-symbol pause
- `results/strategy_backlog_milestone2_2026-05-08.md` — entry C2 (BTC dominance regime) and the heterogeneity finding are about SELECTION rules for milestone 2, not mid-milestone reshuffling
- `CLAUDE.md ## Things to NOT do during forward-paper` — the discipline contract this rule reconciles with
- `scripts/lib/symbols.sh` — the deployed list (edit only on HARD per-symbol kill)
