# Auto-kill execution — locked decision rule (2026-05-08)

**Status:** LOCKED 2026-05-08, before any kill has occurred. Activates when any kill trigger fires. Closes the operational loop alongside:
- `results/drift_firing_investigation_decision_rule_2026-05-08.md` (when does TRIAGE-C escalate to auto-kill)
- `results/drift_wrapper_cron_decision_rule_2026-05-08.md` (cadence of detection)
- `results/real_money_protocol_decision_rule_2026-05-08.md` (kill criteria definitions)

## Question

When a kill trigger fires, what is the EXACT mechanical sequence the operator executes? The kill criteria are documented (CLAUDE.md `## Kill the strategy` and the real-money protocol), the detection mechanism is built (`scripts/run_drift_check.sh`), the investigation is locked (the firing playbook), but the EXECUTION — what commands run, in what order, with what verification gates — is undocumented. Without this lock, the first real kill is improvised under maximum stress, and improvisation under stress is exactly when destructive mistakes happen (wrong engines stopped, journal data lost, position state contaminated).

## Trigger taxonomy

The five trigger types and which kill tier they map to by default:

| # | Trigger | Source | Default tier |
|---|---|---|:---:|
| 1 | Drift wrapper exit code 4 (two firings ≥7d apart, OR drift+threshold match) | `run_drift_check.sh` | HARD |
| 2 | TRIAGE-C investigation concludes auto-kill | `drift_firing_investigation` playbook | HARD |
| 3 | `forward_paper_status.sh` verdict = KILL on a non-fee/non-slip criterion | threshold-based | HARD |
| 4 | Real-money protocol locked kill criterion fires (20% drawdown, single-sym>50%, 3 consecutive net-loss days >5× stake) | `real_money_protocol` rule | HARD |
| 5 | Operational anomaly (unrecoverable engine error, exchange spec change, prolonged data-feed outage) | manual/post_deploy_check | SOFT |

The default is HARD for strategy-degradation triggers and SOFT for operational triggers. Operator MAY downgrade a HARD to SOFT or upgrade SOFT to HARD with explicit documented rationale. Any escalation to/from HARD requires the rationale to be locked in the kill artifact (Phase 4 below).

## Kill tiers

### SOFT KILL (Tier 1)

**Definition:** Engines paused, no new positions opened. Existing positions either ride to natural close OR are closed at market — operator decides per the open-position rule (Phase 2). Resume to ACTIVE state allowed once the trigger condition is verified resolved.

**Use cases:** Operational triggers (engine bug, exchange downtime, suspected confounder under investigation). The hypothesis is "the strategy is sound but external context is broken" — fix the context, resume.

**Reversibility:** Engines can be re-enabled via `systemctl start paper-live@*.service`. State (journals, indicators, recovery) is preserved.

### HARD KILL (Tier 2)

**Definition:** Engines stopped, journals archived to `archive/<reason>_<date>/`, systemd units disabled (not just stopped), milestone closed for further deployment of THIS strategy. Resumption requires a new milestone with new pre-registration.

**Use cases:** Strategy-degradation triggers (drift, drawdown, concentration). The hypothesis is "the strategy is broken on its own merits" — research a fix, don't resume the broken version.

**Reversibility:** Possible only via explicit milestone-restart with re-deployment from clean source. Old journals are preserved in archive but not active.

## Locked execution sequence

The six phases below are MANDATORY for both tiers. Variations between SOFT and HARD are flagged inline.

### Phase 1: HALT (5-10 min)

**Goal:** stop new positions from opening across the entire fleet.

```bash
# All 16 engines via the systemd template
ssh root@178.105.24.230 'systemctl stop paper-live@*.service'

# Verify all stopped
ssh root@178.105.24.230 'systemctl list-units "paper-live@*.service" --state=active --no-legend | wc -l'
# Expected: 0
```

**Verification gate:** the line-count from the second command MUST be `0` before Phase 2 begins. If non-zero, identify the holdouts and resolve (typically `systemctl kill paper-live@<sym>.service` if a graceful stop hangs).

**Tier difference:** SOFT uses `stop`; HARD uses `stop` AND prepares for `disable` in Phase 3. Either way, the immediate halt is identical.

### Phase 2: OPEN POSITIONS (15-30 min, paper) / (30-60 min, real-money)

**Goal:** decide and execute the disposition of each currently-open position.

**Path A — natural close:** existing positions remain in journal as "open"; on a future engine restart they would recover via `Stub.RecoverFromJournal()`. For HARD kill, this means the next actor (a postmortem analyst, a milestone-2 dev) inherits the open positions. Path A is acceptable for SOFT kill but PROBLEMATIC for HARD kill — the milestone is over but state is dangling.

**Path B — synthetic close at current price (paper):** for each open position in each cohort:
```bash
# Identify open positions
./scripts/forward_paper_status.sh

# For each open position, manually emit a synthetic close event to the journal
# (operator script: write a `close` JSONL line with outcome="KILL_CLOSE",
# pnl_usd computed at current price, and journal_path matching the cohort)
```

**Path C — real-money market close:** ONLY at STAGE_1+. Send Binance market-close orders for each open position. Verify fills. Record actual realized P&L.

**Locked rule:**
- Tier SOFT, paper: Path A (natural close) — acceptable since resumption is intended.
- Tier HARD, paper: Path B (synthetic close) — REQUIRED. Dangling open positions in archived journals contaminate any future analysis.
- Tier SOFT, real-money: Path A or B, operator judgment. Bias toward A unless specific risk requires B.
- Tier HARD, real-money: Path C — REQUIRED. Real-money exposure cannot be left dangling in archive.

**Verification gate:** after Path B/C execution, re-run `forward_paper_status.sh`. The "Open positions" section MUST show `0` open across all cohorts.

### Phase 3: ARCHIVE (10-15 min)

**Goal:** move active state to archive and disable engines so accidental restart doesn't resurrect the strategy.

```bash
# Generate the archive slug
REASON="drift_autokill"  # or "drawdown_kill", "concentration_kill", "operational_pause"
DATE=$(date -u +%Y-%m-%d)
ARCHIVE_DIR="/var/log/paper-live/journal/archive/${REASON}_${DATE}"

# Move ALL active journal files (live + shadow/*)
ssh root@178.105.24.230 "
  mkdir -p ${ARCHIVE_DIR}
  cd /var/log/paper-live/journal
  mv *-*.jsonl ${ARCHIVE_DIR}/  2>/dev/null || true
  mv shadow ${ARCHIVE_DIR}/     2>/dev/null || true
"

# Verify the active dir is empty (only `archive/` should remain)
ssh root@178.105.24.230 'ls -la /var/log/paper-live/journal/ | grep -vE "^total|archive|^d.*\.\$" | wc -l'
# Expected: 0

# Tier-specific:
# HARD: disable systemd units so a server reboot does not auto-restart
ssh root@178.105.24.230 '
  for sym in $(./lib/symbols.sh deployed lower); do
    systemctl disable paper-live@${sym}.service
  done
  systemctl disable paper-live-watchdog.timer
  systemctl disable paper-live-digest.timer
'

# SOFT: leave units enabled (systemctl start will resume after fix)
```

**Verification gates:**
- Archive dir created and contains all the journal files
- Active dir empty (Phase 2 verification + Phase 3 move)
- HARD: `systemctl is-enabled paper-live@<any>.service` returns `disabled`

### Phase 4: DOCUMENT (30-60 min)

**Goal:** capture full kill context. The document IS the postmortem-input.

**Required artifact:** `results/kill_<YYYY-MM-DD>_<reason_slug>.md`

**Required fields:**

```markdown
# Kill — <YYYY-MM-DD> — <reason>

**Tier:** SOFT | HARD
**Trigger type:** <1-5 from taxonomy>
**Trigger detail:** <specific rule that fired, with values>
**Detection time (UTC):** <timestamp>
**Halt time (UTC):** <timestamp> (Phase 1 complete)
**Archive complete (UTC):** <timestamp> (Phase 3 complete)

## State at kill

- n_trades closed across all cohorts: <integer>
- Cumulative NET PnL: $<integer>
- Days elapsed since first close: <integer>
- Realized fee bps (cumulative): <number>
- Realized slip bps on losers: <number>
- Single-symbol max contribution: <symbol> = X% of |NET|
- Recent drift firings (last 60d): <list with timestamps>

## Open positions disposition

- Path: A (natural) | B (paper synthetic) | C (real-money market)
- Per-position outcome:
  - <cohort>/<symbol>: closed at <price> on <ts>, pnl=<usd>

## Cross-checks done

- [ ] post_deploy_check.sh: clean | <anomalies>
- [ ] forward_paper_status.sh: verdict = <text>
- [ ] Recent code/deploy events: none | <list>
- [ ] Recent exchange changes: none | <list>

## Action taken

<freeform: what commands ran, in what order, any deviations from the locked playbook>

## Postmortem placeholder

<to be filled in within 7 days of kill — analysis of WHY, mechanism diagnosis, milestone-2 implications>
```

**CLAUDE.md updates:**
- "Strategy status" line: change "**Live:**" to "**KILLED <date>:**" with link to the kill artifact.
- "Today's findings" entry: add a HARD-KILL or SOFT-KILL note with rationale.
- (HARD only) "Forward-paper go/no-go criteria" → mark all locked criteria as superseded by the milestone close.

**Real-money protocol updates (if applicable):** if STAGE_X is active at kill time, mark the protocol as STAGE_X_KILLED and update `real_money_protocol_decision_rule` cross-reference.

### Phase 5: NOTIFY (5 min)

**Goal:** ensure stakeholders know the kill happened. For solo-operator projects this is a documentation safeguard ("future-me reads this and understands why state is what it is"); for teams it's a comms requirement.

**Mechanism:**
- Telegram alert via `pkg/notify/telegram.go` (already-wired infrastructure):
  ```bash
  # Inline ad-hoc — no new code needed; tag = KILL_<TIER>
  ```
- (HARD only) commit and push the kill artifact: `git add results/kill_<date>_<reason>.md && git commit -m "KILL <date>: <reason>" && git push`
- (Real-money active only) operator updates external position tracker (broker dashboard, accountant, etc.) within 24h.

### Phase 6: POSTMORTEM (within 7 days)

**Goal:** convert the kill into milestone-2 planning input. The kill is data; the postmortem is what makes it useful.

**Required outputs (added to the kill artifact, the "Postmortem placeholder" section):**

1. **Mechanism diagnosis:** what went wrong? Was the strategy edge actually gone, or was it a regime-conditioned failure (works again when regime returns), or was it a confounder we didn't catch in the firing playbook?
2. **Pre-registered hypothesis evaluation:** which of the milestone-1 caveats was the proximate cause? (e.g., funding-CSV staleness drift, slip cliff exceeded, regime change)
3. **Milestone-2 implication:** does this kill rule out a strategy-class entirely (e.g., "EMA-cross on shorts is dead") or only the specific parameter set (e.g., "9/21 EMA at 4H is dead, but 5/15 EMA might still work")?
4. **Backlog re-prioritization:** which entries in `strategy_backlog_milestone2_2026-05-08.md` are now MORE relevant given the kill mechanism? Which are LESS?
5. **Detection latency:** how long was the strategy actually broken before the rule caught it? Is the locked rule's TP=100% claim still valid empirically, or did real-world latency exceed the deg30/deg50/dead simulation envelopes?

## Edge cases — pre-locked

### Ambiguous trigger

If two operators (or operator + automated trigger) disagree on whether to kill: operator with stake authority decides. Default to HALT (Phase 1 only) pending agreement. A halted-but-not-killed engine is a SOFT pause; document and resolve within 24h or escalate to HARD.

### Mid-promotion kill

If the kill triggers DURING a STAGE-promotion event (e.g., wrapper fires while promoting STAGE_1 → STAGE_2): kill takes precedence. Promotion is paused. New STAGE assignment after resumption (if applicable) starts from the OLD STAGE, not the in-progress one.

### Engines won't gracefully stop

If `systemctl stop` hangs (>60s): escalate to `systemctl kill --signal=SIGKILL paper-live@<sym>.service`. Document as anomaly in Phase 4 ("ENGINE WOULD NOT STOP GRACEFULLY"). The engine state at SIGKILL may be inconsistent — assume position state is corrupt and treat that engine's open positions as "operator must manually reconcile from journal."

### Multiple triggers fire simultaneously

Document all triggers in Phase 4 (the artifact has space for multiple). The kill executes once; the trigger taxonomy field lists all. If triggers map to different tiers (e.g., one HARD + one SOFT), HARD wins.

### Archive disk space exhausted

If the archive operation fails for disk-space reasons, abort Phase 3 and escalate. DO NOT delete journal data to make room — those journals are the kill audit trail. Archive to a fresh disk or a new mount; document the diversion in Phase 4.

### Wrapper exit 4 fires but operator disagrees

This is a "rule says kill, operator says don't kill" case. Locked rule: the operator MAY override the auto-kill exit code, but MUST document the override rationale in a `results/drift_firings/<date>-override.md` artifact within 24h. The override is a one-shot — the next exit-4 from the wrapper triggers a kill regardless. Persistent override (operator overrides 2+ exit-4s) re-opens this rule for design.

### Kill happens during ongoing milestone-2 research execution

If milestone-2 backtest scripts are running on the operator's machine when the kill triggers: kill the engines first (Phase 1 latency), then decide whether to abort the backtest. The backtest doesn't affect live state, so it can be allowed to complete.

## Alternatives considered (rejected)

### Auto-execute the kill via cron when wrapper exits 4

**Considered:** if the rule already says exit 4 = auto-kill candidate, just execute the kill mechanically.

**Rejected because:** kills are high-stakes, hard-to-reverse actions. The operator MUST be in the loop for a kill, even when the trigger is mechanical. The wrapper firing exit 4 is the rule recommending a kill; the operator confirming and executing is the safety gate. (Compare: the firing-investigation playbook's TRIAGE-A safety net is "if real degradation, will fire again in 7+ days." The auto-kill execution does not have a similar safety net — once executed, it's done.)

### Single-tier (HARD only) — every kill is permanent

**Considered:** simplify by removing the SOFT/HARD distinction.

**Rejected because:** operational triggers (engine bug, exchange downtime) are routine and reversible. Forcing them into HARD KILL means every operational hiccup ends the milestone — wasteful and demoralizing. SOFT tier preserves resumption for fixable causes.

### Skip Phase 4 documentation; rely on git history

**Considered:** git log is enough, no need for a structured artifact.

**Rejected because:** git messages are not structured for postmortem (no required fields, easy to omit). Phase 4's locked artifact format ensures every kill produces the same set of facts, which is what makes cross-kill analysis tractable.

### Skip Phase 6 postmortem (defer indefinitely)

**Considered:** focus on execution; postmortem is "research for milestone 2."

**Rejected because:** the 7-day window is the period where the kill is freshest and most analyzable. Deferring means the diagnosis quality degrades (operator memory fades, market regime moves on). Lock the 7-day window now so milestone-2 inputs are high-quality.

## Migration triggers

The rule re-opens for design when:

1. **First actual kill occurs.** Review whether the playbook held up. If a phase took materially longer than the time budget, recalibrate. If a verification gate was unclear, tighten it.
2. **STAGE_3+ promotion** (real-money $500-$1k/trade). Phase 2C (real-money market close) becomes a routine concern; the playbook may need a dedicated real-money sub-section.
3. **First operator override of an exit-4** (per the edge case). Document and verify the override rule is calibrated correctly.
4. **Persistent overrides** (2+ exit-4 overrides without a HARD kill). Indicates the wrapper's exit-4 threshold is mis-calibrated for current operating context — re-open both this rule AND the wrapper's calibration.
5. **Multi-strategy fleet** (future milestone). Currently 1 live + 3 shadow; if the architecture grows to multi-strategy live (Strategy A + Strategy B both real), the kill rule becomes per-strategy and the playbook needs clarification on whether one strategy's kill affects the other.

## Cross-references

- `results/drift_firing_investigation_decision_rule_2026-05-08.md` — the investigation playbook that may escalate to this rule
- `results/drift_wrapper_cron_decision_rule_2026-05-08.md` — cadence of the wrapper that emits exit 4
- `results/drift_detector_calibration_verdict_2026-05-07.md` — operating point underlying exit-code semantics
- `results/real_money_protocol_decision_rule_2026-05-08.md` — kill criteria definitions for real-money triggers
- `scripts/run_drift_check.sh` — the wrapper this rule responds to
- `scripts/forward_paper_status.sh` — Phase 4 cross-check
- `scripts/post_deploy_check.sh` — Phase 4 cross-check
- `pkg/execution/stub.go` — `Stub.RecoverFromJournal` is the function that would resurrect Path-A open positions on a future engine restart
- `pkg/notify/telegram.go` — Phase 5 notify mechanism
- `CLAUDE.md ## Forward-paper go/no-go criteria` — operational doctrine
- `CLAUDE.md ## Strategy status` — what gets updated in Phase 4
