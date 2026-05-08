# Postmortem template — locked decision rule (2026-05-08)

**Status:** LOCKED 2026-05-08. Activates at every HARD kill (per `auto_kill_execution_decision_rule_2026-05-08.md` Phase 6) and is REQUIRED for every kill artifact within 7 days. Recommended (not required) for STAGE-promotion rollbacks per the runbook's Phase 6 documentation.

## Question

The auto-kill execution rule's Phase 6 says: "Convert the kill into milestone-2 planning input. The kill is data; the postmortem is what makes it useful." The doc lists five required outputs (mechanism diagnosis, hypothesis evaluation, milestone-2 implication, backlog re-prioritization, detection-latency analysis) but the EXACT structure is not locked.

Without a locked template:
- Postmortems vary in depth and rigor across operators.
- Cross-postmortem comparison is difficult — was kill A's mechanism the same as kill B's? Hard to answer if formats differ.
- Required outputs are ambiguous — "mechanism diagnosis" is broad; what specifically must be answered?
- The 7-day deadline becomes elastic if the structure isn't clear (operator delays "until I know what to write").

The locked template below makes every postmortem produce the same set of structured facts. The discipline: make the format mechanical, leave the analysis substantive.

## When the postmortem is required vs recommended

| Trigger | Required? | Window |
|---|:---:|---|
| HARD KILL (any tier from `auto_kill_execution_decision_rule`) | **REQUIRED** | within 7 days |
| SOFT KILL extending beyond 30 days (becomes effectively permanent) | **REQUIRED** | within 7 days of the 30-day mark |
| STAGE-promotion rollback (per `stage_promotion_runbook` Phase 6 rollback path) | RECOMMENDED | within 14 days |
| Per-symbol HARD pause (delisting / permanent removal) | **REQUIRED** | within 7 days |
| Per-symbol SOFT pause that extends beyond 30 days | RECOMMENDED | within 14 days |
| Drift firing dismissed via TRIAGE-A or B (no kill) | NOT REQUIRED (firing artifact suffices) | n/a |

The asymmetric "required" assignment reflects that HARD events (kill, promotion-rollback, permanent symbol removal) are the high-information data points where postmortem value is highest. SOFT events are usually lower-information but become "effectively-HARD" if extended.

## Locked template structure

The postmortem is appended to the originating artifact (e.g., `results/kill_<date>_<reason>.md` or `results/stage_promotion_<date>_<from>_to_<to>.md`) under a `## Postmortem` heading. Required structure:

```markdown
## Postmortem

**Postmortem completion date:** <YYYY-MM-DD>
**Days since trigger event:** <integer> (must be ≤ 7 for required postmortems)
**Author(s):** <name(s)>

### 1. Timeline

A chronological reconstruction with timestamps:

- T0: <trigger event UTC ts> — <what fired>
- T0+0..30 min: <Phase 1 actions>
- T0+30..N min: <Phase 2/3/4 actions>
- T0+N..M hours: <stabilization period>
- T0+M..L hours: <follow-on actions>

The timeline anchors all subsequent analysis to specific events, not narrative gestures.

### 2. Mechanism diagnosis

**The question:** what specifically broke, in mechanical terms?

Required to answer:
- Was the strategy's edge GONE (the underlying alpha disappeared) or REGIME-CONDITIONED (works again when regime returns) or COST-DOMINATED (the alpha is intact but realized costs ate it) or CONFOUNDED (the kill trigger fired due to non-strategy cause we caught too late)?
- If GONE: at what point in time did the data first show the degradation? (look back at the cumulative NET trajectory; identify the inflection)
- If REGIME-CONDITIONED: which regime feature changed? (vol, funding, correlation, volume) at what magnitude?
- If COST-DOMINATED: which cost component diverged from model? (fee, slip on losers, funding, slip on winners) by how much?
- If CONFOUNDED: what was the proximate non-strategy cause? (exchange spec change, data integrity, operational anomaly)

Answer in 2-4 sentences with specific numbers (NET delta, cost delta, regime metric values). Vague answers ("the strategy stopped working") are insufficient.

### 3. Pre-registered hypothesis evaluation

**The question:** which milestone-1 caveat was the proximate cause?

CLAUDE.md `## Known unmodeled risks` and `## Forward-paper go/no-go criteria` document the caveats:
- 6s-10s REST polling lag (live-only, unmodeled)
- Funding-CSV staleness drift
- Real-money execution untested (pre-real-money only)
- Train-only-shortlist look-ahead inflation (already quantified)
- Walk-forward CI regime-variance dominance

Required to map: which (if any) of these was the proximate cause of the kill? If a NEW risk surfaced (one not on the documented list), document it explicitly so future milestones can pre-register against it.

### 4. Detection latency

**The question:** how long was the strategy actually broken before the rule caught it?

Required:
- First detectable degradation timestamp (from the cumulative NET inflection identified in Section 2).
- Drift wrapper exit-1 fire timestamps in the relevant window.
- Drift wrapper exit-4 (or auto-kill trigger) fire timestamp.
- Latency from first-detectable to first-flagged: <X days>.
- Latency from first-flagged to kill-executed: <Y days>.

Compare to the locked rule's calibration claims. The drift detector calibration verdict (`drift_detector_calibration_verdict_2026-05-07.md`) found median detection day = 26d under deg30/deg50/dead. If real-world latency materially exceeded the simulation envelope, document the gap — milestone-2 may need recalibration.

### 5. Milestone-2 implication

**The question:** what does this kill rule out, and what does it leave open?

Required:
- Strategy class implication: does this kill rule out the entire EMA-cross-on-shorts mechanism, or only the specific 9/21 / 4H / 504-mh parameter set? (be specific — "EMA cross is dead" is too broad; "9/21 EMA cross at 4H with 504h max-hold on the deployed-16 universe is dead under the cost regime that emerged" is appropriate)
- Strategy backlog re-prioritization: which entries in `strategy_backlog_milestone2_2026-05-08.md` are now MORE relevant given the kill mechanism? (e.g., if the kill was cost-dominated, A2 ATR-targeted-sizing rises in priority because it equalizes per-trade cost contribution)
- Strategy backlog DEMOTION: which entries are now LESS relevant? (e.g., if the kill was regime-conditioned, regime-conditioned entries like A1 vol-regime-filter are more relevant; regime-blind entries are less)

### 6. What we'd do differently

**The question:** with hindsight, what would the operator have done differently?

This section is not blame — it's pattern-extraction. Required:
- One specific decision in the timeline that, in hindsight, was wrong. (If everything was correct given the information available, document THAT — it's a calibration confirmation.)
- One specific check or threshold that, if it had been in place, would have caught the issue earlier. (If no such check is plausible, document THAT.)
- One specific milestone-2 design implication. (E.g., "Future milestones should require X check at Y cadence.")

### 7. Action items for milestone 2

**The question:** what concrete changes propagate from this kill into milestone 2?

Required: 2-5 specific action items, each with:
- Owner (operator role; for solo project, "operator")
- Trigger condition (when does this action fire; e.g., "before milestone-2 deploy")
- Acceptance criterion (what does "done" look like)
- Cross-reference (which existing pre-reg, if any, this action modifies)

Example items:
- "Add a Section A8 to the completion review: realized-funding-cost cumulative ≤ $X. Trigger: before next forward-paper review. Acceptance: PR merged updating completion-review pre-reg with A8 threshold + the new threshold validated against milestone-1's funding cost data. Modifies: forward_paper_completion_review_decision_rule."
- "Recalibrate drift detector's deg-scenario simulations to include the cost-divergence pattern observed here. Trigger: before milestone-2 deploy. Acceptance: new calibration verdict with the additional scenario. Modifies: drift_detector_calibration_verdict."

Action items are what convert the postmortem from REFLECTION to ACCOUNTABILITY.

### 8. Cross-references and supporting data

Required links:
- Originating artifact (kill / promotion-rollback / per-symbol-pause)
- forward_paper_status.sh output at trigger time
- Drift wrapper run logs in the relevant window
- Per-symbol pause artifacts in the relevant window
- Recent git log entries (pkg/ + cmd/) covering the milestone window
- Any external references (Binance announcements, regulatory notices, market reports)
```

## Postmortem timing — locked

- **Required postmortems:** complete within 7 days of the trigger event. Late completion is itself a process failure to flag in the next milestone's preparation.
- **Recommended postmortems:** complete within 14 days. Late completion reduces value but doesn't trigger process-failure flag.

7-day window rationale: this is the period where the kill is freshest. Operator memory is intact, market state is still close to the trigger context, the data hasn't been displaced by subsequent events. After 7 days, recall quality degrades and the analysis becomes archeology.

## Edge cases — pre-locked

### Kill trigger is genuinely ambiguous (TRIAGE-C escalation that wasn't clearly justified)

Section 2 (mechanism diagnosis) MAY answer "ambiguous — the kill was triggered by the rule but no single mechanism is identifiable." Document why this is the conclusion (specific data points that suggest multiple mechanisms or none). Section 5 (milestone-2 implication) becomes more conservative — don't rule out anything that ambiguity doesn't support ruling out.

### Postmortem completion exceeds 7-day window

The postmortem is still required. Document the late completion explicitly under "Author note: this postmortem was completed N days post-trigger; delay was caused by [reason]." Future milestone-2 preparation must include a process-failure check for late completion.

### Multiple postmortems overlap (e.g., kill happens during promotion-rollback)

Single artifact covers both, with the more-severe trigger as the primary classification. The postmortem section addresses both events.

### Postmortem analysis suggests the locked rule itself was wrong

Section 6 (what we'd do differently) and Section 7 (action items) document this. The action item modifies the relevant pre-reg per its migration trigger. Locked rules are not immutable — they're locked AGAINST stress-driven re-design but updateable via documented postmortem-driven feedback.

### Data needed for the postmortem is missing

If the trigger event corrupted state (e.g., archive disk full, journal lost), document the data gap explicitly. Sections that can't be answered with available data note "DATA UNAVAILABLE — see Section 6 for action item to ensure data preservation in future kills." This is itself a milestone-2 input.

## Alternatives considered (rejected)

### Free-form postmortems

**Considered:** trust the operator to write what's relevant.

**Rejected because:** structured templates produce consistent data. Cross-postmortem comparison (was kill A's cause similar to kill B's?) is impossible without consistent format. The structure is not bureaucracy — it's the difference between data and notes.

### Required postmortem ONLY for HARD KILL

**Considered:** simplification.

**Rejected because:** SOFT kills that extend beyond 30 days are effectively HARD (the strategy hasn't run for a month — that's a milestone interruption). Per-symbol HARD pauses (delistings) are similarly milestone-impacting. The required vs recommended distinction is calibrated to information value, not to the immediate trigger label.

### Defer all postmortems to "end of milestone" instead of 7-day window

**Considered:** batch the analysis at milestone close.

**Rejected because:** memory degrades. End-of-milestone postmortem of an event that happened 60 days ago is materially worse than a 7-day-fresh postmortem. The 7-day window costs operator time but pays in analysis quality.

## Migration triggers

The rule re-opens for design when:

1. **First actual postmortem completion** — review whether the structure produced useful analysis. If a section was repeatedly empty across multiple postmortems, recalibrate.
2. **Multiple kills cluster** — 3+ kills in a short window may benefit from a meta-postmortem (cross-postmortem analysis) which this rule doesn't currently specify.
3. **Postmortem suggests the locked rule itself was wrong** (per the edge case) — the action items modify the relevant pre-reg per its migration trigger.
4. **Multi-strategy fleet** — postmortems may need cross-strategy framing.

## Cross-references

- `results/auto_kill_execution_decision_rule_2026-05-08.md` — Phase 6 references this template
- `results/stage_promotion_runbook_decision_rule_2026-05-08.md` — Phase 6 documentation on rollback can use this template
- `results/per_symbol_pause_decision_rule_2026-05-08.md` — HARD pauses require this template
- `results/strategy_backlog_milestone2_2026-05-08.md` — Section 5 directly feeds backlog re-prioritization
- `results/drift_detector_calibration_verdict_2026-05-07.md` — Section 4 (detection latency) compares to this calibration
- `CLAUDE.md ## Known unmodeled risks` — Section 3 maps proximate causes to documented caveats
