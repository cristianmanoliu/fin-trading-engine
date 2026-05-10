# PROMOTE-closure template — locked decision rule (2026-05-10)

**Status:** LOCKED 2026-05-10. Activates at every stage promotion in the real-money progression: paper→STAGE_1, STAGE_1→STAGE_2, STAGE_2→STAGE_3, STAGE_3→STAGE_4. The paper→STAGE_1 instance simultaneously serves as milestone-1's PROMOTE-side closure artifact.

## Question

`forward_paper_outcome_resolution_decision_rule_2026-05-10.md` produces a PROMOTE verdict mechanically when all gates clear. `stage_promotion_runbook_decision_rule_2026-05-08.md` locks the 6-phase execution sequence. But neither rule specifies what artifact the operator WRITES at the moment of PROMOTE — what evidence gets frozen, what counterfactuals get audited, what risks get explicitly accepted, what monitoring cadence ARMs for the next stage.

`postmortem_template_decision_rule_2026-05-08.md` covers the symmetric KILL-side: when something breaks, an 8-section template makes the analysis structured and comparable. Promotion deserves the same discipline. Without it:

- Promotion artifacts vary in depth and rigor across stages.
- Cross-promotion comparison is difficult — was STAGE_1's evidence quality comparable to STAGE_2's? Hard to answer without consistent format.
- The asymmetric cognitive trap of success ("we knew it would work") goes uncaught; confirmation bias overrules the explicit counterfactual that postmortems force on the kill side.
- Risk acceptance becomes implicit. At paper→STAGE_1, real money is now exposed to the unmodeled risks documented in CLAUDE.md `## Known unmodeled risks`. The operator's explicit signature that they ACCEPT these risks at THIS specific stake size is a forensic record for future-self.

The locked template below makes every promotion produce the same set of structured facts. The discipline: make the format mechanical, leave the substance free.

## When the closure is required vs recommended

| Trigger | Required? | Window |
|---|:---:|---|
| paper → STAGE_1 promotion (milestone-1 closure on PROMOTE side) | **REQUIRED** | within 7 days |
| STAGE_1 → STAGE_2 promotion | **REQUIRED** | within 7 days |
| STAGE_2 → STAGE_3 promotion | **REQUIRED** | within 7 days |
| STAGE_3 → STAGE_4 promotion | **REQUIRED** | within 7 days |
| STAGE_N → STAGE_N (no change; gates cleared but operator declined to promote) | RECOMMENDED | within 14 days |
| WATCH verdict that resolves to PROMOTE within 4 weeks | **REQUIRED** | within 7 days of the eventual PROMOTE |
| OPERATOR_REVIEW verdict that ultimately becomes PROMOTE | **REQUIRED** | within 7 days of the eventual PROMOTE |

The "STAGE_N → STAGE_N declined" case (gates clear but operator holds) is interesting: it's a non-event in the mechanical sense but a decision point worth recording. RECOMMENDED, not required — the operator's reasoning for holding is the substance; format discipline is secondary.

## Locked template structure

The closure artifact is written to `results/promote_closure_<date>_<from>_to_<to>.md` — e.g., `results/promote_closure_2026-09-09_paper_to_STAGE_1.md`. Required structure:

```markdown
# PROMOTE closure — <from> → <to> (<date>)

**Closure completion date:** <YYYY-MM-DD>
**Days since PROMOTE verdict:** <integer> (must be ≤ 7 for required closures)
**Author(s):** <name(s)>
**Trigger:** `results/decision_snapshots/<date>-resolution.txt` exit code 4 (PROMOTE) OR `<override-artifact>`
**Stage transition:** <from-stage> → <to-stage>
**Stake size at new stage:** $<N>/trade per `real_money_protocol_decision_rule_2026-05-08.md`

## 1. Timeline

A chronological reconstruction with timestamps:

- T_first_eligible: <UTC ts when ALL gates first simultaneously cleared>
- T_verdict: <UTC ts when forward_paper_resolution.py / stage_promotion_check.py emitted PROMOTE>
- T_operator_acknowledge: <UTC ts operator confirmed verdict via Telegram CRITICAL response or manual artifact>
- T_runbook_phase_1: <UTC ts stage_promotion.sh Phase 1 fired>
- T_runbook_phase_6: <UTC ts stage_promotion.sh Phase 6 completed (artifact written)>
- T_first_trade_new_stage: <UTC ts first trade executed at new stake size>

The timeline anchors all subsequent analysis to specific events, not narrative gestures. The gap between T_first_eligible and T_verdict is the WEEKLY-CRON-CADENCE artifact (gates may have cleared mid-week but verdict only fires Sunday). The gap between T_verdict and T_operator_acknowledge is the OPERATOR-AVAILABILITY artifact (cron fires automatically; operator response time is human). Document both gaps.

## 2. Mechanism confirmation

**The question:** what evidence supports that the strategy's alpha is real and operating as the milestone-1 thesis predicted?

Required to answer (with specific numbers, not narrative):

- **Net PnL at promotion:** <$N over <D> days>. Compare to `real_money_protocol_decision_rule_2026-05-08.md` STAGE_<from>_to_STAGE_<to>'s "expected PnL" envelope (pro-rated honest-annual × elapsed-fraction × 0.60 lower bound). If outside envelope, document direction (above = pleasant surprise; below = was the gate ALMOST not met?).
- **Hit rate (WR%):** <X%> at <n> trades. Compare to backtest WR (20.6% for the deployed strategy). If divergent by >5pp, surface as a section-5 counterfactual.
- **Per-symbol contribution distribution:** top symbol contributed <X%> of cumulative NET (must be ≤40% per locked criterion). List top 3 contributors with their contributions and trade counts.
- **Cost stack at promotion:** realized fee bps = <X>, slip bps on losers = <Y>. Compare to modeled (10 / 5). Note paper-vs-real-fills caveat per CLAUDE.md if at paper→STAGE_1 (Stub fills = modeled by construction; the gate is plumbing, not signal — see `gate_informationality_2026-05-10.md`).
- **Drift detector trajectory:** weekly exit codes for the last 4 weeks: [0, 0, 0, 0] is clean; any 1 in the window must be explicitly explained.
- **BTC-HODL benchmark beat:** strategy NET vs $32k notional BTC-HODL over the same window: <$N delta>. Locked criterion: must beat HODL.

Answer all bullets in 2-4 sentences each with the specific numbers. Vague answers ("the strategy worked") are insufficient.

## 3. Pre-registered hypothesis evaluation

**The question:** did the milestone-1 thesis hold, or did one of the documented caveats activate?

CLAUDE.md `## Known unmodeled risks` documents the 3 still-open caveats:
- REST polling lag (10s) adverse on entry + stop
- Funding-CSV staleness drift
- No real-money execution test (closes at STAGE_1)

Required to answer:
- Which caveats were ACTIVATED in the forward-paper window (i.e., observed in the data)? Quantify the impact.
- Which caveats remained UNTESTED (i.e., didn't materialize, so we don't know if mitigation is sufficient)? List explicitly so they roll forward.
- Which NEW caveats surfaced that weren't on the original list? Document explicitly so future stages pre-register against them.

For paper→STAGE_1 specifically: caveat 3 (no real-money execution test) was closed by Layer 2 testnet + Layer 3 shadow parity. Document the Layer 2/3 PASS verdicts inline.

## 4. Validation latency

**The question:** how long was the strategy PROMOTE-eligible before the rule caught up?

Required:
- T_first_eligible (when ALL gates were simultaneously satisfied — may require backward-walking the cumulative metrics)
- T_verdict (when the rule actually fired PROMOTE)
- Latency = T_verdict − T_first_eligible: <X days>

This latency is mechanically bounded by the weekly cron cadence (≤7 days) for the normal case. If the latency exceeds 7 days, document why — was a gate transiently un-met during the window, did the cron fail to fire, was there an operator-mute period?

Compare to the symmetric KILL-side detection latency (`drift_detector_calibration_verdict_2026-05-07.md` median 26d under any degradation). The PROMOTE-side bound is tighter because the gates are CLEAR-OR-NOT-CLEAR not statistical; document if this asymmetry created any timing pathology.

## 5. Counterfactual / non-promote evidence

**The question:** what evidence existed AGAINST promotion that the operator/rule overrode or de-weighted?

This is the section that catches confirmation bias. Required even when answer is "none" — explicit acknowledgement is data.

Required to surface:
- Threshold criteria that PASSED but only marginally (within 10% of cliff). Each near-miss is documented as a soft signal that may not persist.
- Threshold criteria that FAILED on prior weeks but cleared at PROMOTE moment. Document the trajectory — did the failing metric trend correctly (mean-reversion) or did it stochastically clear (lucky window)?
- Per-symbol concentration trends: was the top-symbol contributing 38% at promotion but trending toward 45%? Document if so.
- Any drift_check INVESTIGATION-grade fires (exit 1) in the window. Each must be either (a) cross-checked clean via the threshold-band investigation per drift_firing_investigation rule, OR (b) explicitly documented as overridden with reason.
- Operator qualitative concerns held but ultimately deferred. "The drift detector said clean but the per-symbol distribution feels off to me" is the kind of signal that vanishes if not recorded at the moment of decision.

If the answer is "no counterfactual evidence" — explicit positive statement to that effect. "I have searched and there is no evidence against promotion" is a different artifact than the absence of the section.

## 6. Risk acceptance ledger

**The question:** what risks does this stage now actively expose, and does the operator explicitly accept them at this stake size?

The structure: every CLAUDE.md `## Known unmodeled risks` entry + every section-3 NEW caveat gets a row in this table. The operator signs for each:

| Risk | Pre-promotion state | Stake at this stage | Operator accepts? | Mitigation if any | Reversal threshold |
|---|---|---|:---:|---|---|
| REST polling lag | Untested at real money | $<N>/trade | YES / NO | <e.g., "10s lag empirically <2bp slip impact at paper">  | <e.g., "if measured slip from REST lag exceeds 10bp over 30 trades, pause"> |
| Funding-CSV staleness | Capped at 7d; net ≈ $0 | $<N>/trade | YES / NO | <weekly refresh post_deploy §9> | <"if funding net exceeds modeled by 3σ, investigate"> |
| Real-money execution untested | Closing at this stage | $<N>/trade | YES / NO | <Layer 2 PASS + Layer 3 PASS> | <"if first 10 real-money fills show >25bp slip vs paper, halt"> |
| <new caveat from section 3> | <state> | $<N>/trade | YES / NO | <mitigation> | <threshold> |

A NO in the "accepts" column halts the promotion sequence. This is the operator's explicit pre-commitment. Future-self reading this artifact can audit whether the risk acceptance was calibrated — the table format makes the audit mechanical.

## 7. Stage transition mechanics

**The question:** what concrete operational changes happen at this PROMOTE moment?

Required to document each transition mechanically:

- **Executor flip:** which engine(s) flip from `--executor stub` (or `binance_live_testnet` for Layer 2) to `--executor binance_live`? List by symbol. For paper→STAGE_1, this is the first time real money is exposed.
- **Credentials provisioning:** confirm `BINANCE_API_KEY` / `BINANCE_API_SECRET` with the locked permission set (futures-trade only, IP-allowlisted, position-size-limited, no withdrawal) are in `/etc/paper-live/env` on the VPS.
- **Stake size change:** stake_usd config update applied to which engines.
- **Monitoring cadence change:** drift detector cadence (weekly), per_symbol_pause threshold tightening, post_deploy_check STRICT-mode toggle, any alert-tier escalations.
- **Telegram alert tier changes:** at paper→STAGE_1, certain INFO events may now be WARN-worthy (a single losing trade losing $100 is INFO-noise during paper, but the first real-money loss should be WARN to confirm operator awareness even if the loss is small).
- **Watchdog timer state:** systemd watchdog timers for each engine reviewed and tightened if needed for the new stage.
- **Documentation updates queued:** what CLAUDE.md / OPERATOR_HANDBOOK.md sections need to update post-PROMOTE? List them; they're not required to be done WITHIN the artifact but should be queued.

Each item: confirmed-done / pending / not-applicable + timestamp of confirmation.

## 8. Reversal criteria for new stage

**The question:** under what conditions does the operator REVERT to the previous stage?

Reversal is distinct from KILL — kill stops the strategy entirely; reversal de-promotes back to the prior stage's stake size while keeping the strategy live. Reversal preserves the trajectory.

Required:
- Cross-reference `auto_kill_execution_decision_rule_2026-05-08.md` for the HARD/SOFT KILL triggers (these always supersede; reversal is a SOFTER option below kill).
- Cross-reference `real_money_protocol_decision_rule_2026-05-08.md` for the locked per-stage kill triggers (e.g., STAGE_1 kill at "3 consecutive days each net-loss >5× stake"). These also trigger reversal-or-kill — document which.
- NEW thresholds specific to this stage transition: e.g., "if STAGE_1's first 30 days NET < honest-annual × (30/365) × 0.40, reverse to paper to investigate." Each new threshold gets its own pre-registration artifact OR is documented as an action item in section 10.

The reversal path differs from the original promotion path: stage_promotion.sh handles the forward direction; reversal is documented in `stage_promotion_runbook` Phase 6 (rollback). Confirm the rollback path is operational.

## 9. Next-stage monitoring cadence

**The question:** what monitoring ARMs at this PROMOTE moment that wasn't armed before?

Locked items per stage (cross-ref `real_money_protocol_decision_rule_2026-05-08.md`):
- **paper→STAGE_1:** drift detector daily (was weekly), per_symbol_pause check daily, post_deploy_check STRICT=1 weekly, BTC-HODL benchmark weekly, Telegram CRITICAL on any real-money exception
- **STAGE_1→STAGE_2:** drift detector cadence remains daily, additional check on per-symbol max-loss tightening
- **STAGE_2→STAGE_3:** weekly drift check (relax from daily) IF no firings in 60 days, monthly STAGE-rollback review
- **STAGE_3→STAGE_4:** weekly drift check, quarterly STAGE_4-specific kill-criteria review

Specify each cadence that ARMs + its first-fire timestamp. Confirm each is operationally wired (cron / launchd / systemd timer) not just intended.

## 10. Action items

**The question:** what concrete changes propagate FORWARD from this promotion into the next stage or next milestone?

Required: 2-5 specific action items, each with:
- Owner (operator role; for solo project, "operator")
- Trigger condition (when does this action fire)
- Acceptance criterion (what does "done" look like)
- Cross-reference (which existing pre-reg, if any, this action modifies)

Example items (paper→STAGE_1 instance):
- "Update milestone-1 closure section of CLAUDE.md to reflect STAGE_1 operational state. Trigger: within 7d of promotion. Acceptance: PR merged updating the strategy-status block. Modifies: CLAUDE.md ## Strategy status."
- "Activate the SECOND-pass cost-stack gate (now informationally non-null because writer flipped from Stub to binance_live). Trigger: after first 10 STAGE_1 fills. Acceptance: realized_cost_trajectory.py shows non-zero variance across the 10 fills; if variance is implausibly low investigate writer plumbing. Modifies: CLAUDE.md ## Forward-paper go/no-go criteria paper-mode-caveat."
- "Pre-register the STAGE_1→STAGE_2 gate criteria explicitly in a new decision_rule artifact. Trigger: before day 30 at STAGE_1. Acceptance: artifact locked + cross-referenced from this closure."

## 11. Cross-references and supporting data

Required links:
- Originating verdict artifact (`results/decision_snapshots/<date>-resolution.txt`)
- stage_promotion.sh run log
- forward_paper_status.sh snapshot at trigger moment
- forward_paper_resolution.py output at trigger moment
- realized_cost_trajectory.py snapshot at trigger moment
- Drift detector run log for last 4 weeks pre-promotion
- BTC-HODL benchmark output at trigger moment
- Layer 2 testnet PASS verdict (paper→STAGE_1 only) — `results/layer2_smoke_<date>.md`
- Layer 3 shadow parity PASS verdict (paper→STAGE_1 only) — `results/layer3_verdict_<date>.md`
- Real-money credentials provisioning audit trail (NOT the credentials themselves — just the audit that they were provisioned with the locked permission set)
- Recent git log entries (pkg/ + cmd/) covering the milestone window
- Any external references (regulatory announcements, exchange spec changes)
```

## Closure timing — locked

- **Required closures:** complete within 7 days of the PROMOTE verdict. Late completion is itself a process failure to flag in the next milestone's preparation.
- **Recommended closures:** complete within 14 days. Late completion reduces value but doesn't trigger process-failure flag.

7-day window rationale: this is the period where the PROMOTE evidence is freshest. Operator memory is intact, market state is still close to the trigger context, the gating-metric snapshots haven't been overwritten by subsequent weekly cron runs. After 7 days, recall quality degrades and the analysis becomes archeology — and worse, the next stage's first-week data starts contaminating the closure's "as-of-PROMOTE" snapshot.

## Edge cases — pre-locked

### PROMOTE verdict fires but operator declines to promote

The PROMOTE verdict is mechanical; the operator retains override authority. If the operator declines:
1. Write the closure artifact as RECOMMENDED-not-required.
2. Section 5 (counterfactual) becomes the PRIMARY section — document explicitly why the operator overrode the mechanical verdict.
3. The next weekly cron will re-fire PROMOTE; document whether the override is one-shot (re-evaluate next week) or persistent (operator wants to hold for N weeks; lock that hold-duration here).
4. Sections 7-9 (transition mechanics, reversal, monitoring) are SKIPPED because no transition happened.

### WATCH verdict that becomes PROMOTE within 4 weeks

Treat as a single closure artifact covering both the WATCH period and the eventual PROMOTE. Section 1 (timeline) extends back to the WATCH verdict. Section 5 (counterfactual) addresses what the WATCH-period soft signals were and how they resolved.

### Closure analysis suggests the locked verdict rule was wrong

Section 6 has no direct analog here — it's analogous to postmortem section 6 ("what we'd do differently"). If the operator's section-5 counterfactual reveals that the mechanical PROMOTE was based on evidence that, in hindsight, shouldn't have qualified — the action item is to update `forward_paper_outcome_resolution_decision_rule_2026-05-10.md` per its migration triggers. Promotion does NOT roll back automatically; the locked rule's verdict is taken as authoritative even when retrospectively questioned. The update applies to FUTURE promotions.

### First real-money trade fails / partial-fills / executes at unexpected price

This is a STAGE_1-specific edge case. Section 7 (stage transition mechanics) gains an addendum: document the first 5 real-money fills with full detail (signal timestamp, order placement, fill timestamp, slippage observed, exchange response codes). This 5-fill snapshot is the LAST CHANCE to catch a binance_live executor plumbing issue before broad deployment.

### Closure data is missing

If the trigger event corrupted state (e.g., archive disk full, journal lost), document the data gap explicitly. Sections that can't be answered with available data note "DATA UNAVAILABLE — see Section 10 for action item to ensure data preservation in future promotions." This is itself a future-stage input.

## Alternatives considered (rejected)

### Free-form promotion notes

**Considered:** trust the operator to write what's relevant.

**Rejected because:** structured templates produce consistent data. Cross-promotion comparison (was STAGE_1 evidence comparable to STAGE_2?) is impossible without consistent format. The structure is not bureaucracy — it's the difference between data and notes. And the success-side cognitive trap (confirmation bias under positive outcome) is MORE acute than the kill-side trap (where the bias is to under-blame); a free-form artifact under success bias would systematically under-document counterfactuals.

### Skip closure for "obvious" promotions

**Considered:** if all gates clear by wide margin, skip the artifact.

**Rejected because:** "wide margin" is a post-hoc judgment subject to the success-bias the closure exists to counteract. The closure is the discipline; skipping it for "easy wins" is exactly when the discipline matters most. The format makes the time-cost small (~30-60 min) even in the easy-win case.

### Defer to "end of milestone" instead of 7-day window

**Considered:** batch the analysis at milestone close.

**Rejected because:** the milestone CLOSURE on PROMOTE is itself a milestone-1 closing event. There's no later milestone-close to defer to (it IS the close). For STAGE_N→STAGE_N+1 promotions, deferring to "end of milestone" means the closure artifact for STAGE_1 might land at the milestone-2 close — by which time STAGE_2 and STAGE_3 may also have happened and the analysis becomes archeology. The 7-day discipline preserves the per-stage analytic resolution.

### Closure only for paper→STAGE_1, not for intra-real-money stage promotions

**Considered:** paper→STAGE_1 is the BIG transition; the rest are tweaks.

**Rejected because:** each STAGE promotion is a 3× stake increase ($100→$300→$500→$1000). At STAGE_3→STAGE_4 the per-trade exposure is 10× the initial commit. The risk-acceptance ledger (section 6) is materially different at each stage; making the operator re-sign at every promotion is exactly the discipline that prevents stake-creep. The format makes the time-cost similar across stages even though substance varies.

## Migration triggers

The rule re-opens for design when:

1. **First actual closure completion** — review whether the structure produced useful analysis. If a section was repeatedly empty across multiple closures, recalibrate. If a section was repeatedly insufficient (operator filled in but wished for more granularity), expand.
2. **Multiple promotions cluster** — if STAGE_1, STAGE_2, STAGE_3 all happen within 90 days, the closure structure may need a "cumulative across recent promotions" section that doesn't currently exist.
3. **Closure suggests the verdict rule itself was wrong** — per the edge case above, action items modify the relevant pre-reg per its migration trigger.
4. **Real-money operation reveals a risk class not on the original caveat list** — section 6's ledger format extends to cover the new class. The first such addition is an interesting forensic moment in itself.
5. **Multi-strategy fleet** — if the project ever runs >1 strategy simultaneously, the closure artifact needs cross-strategy framing analogous to the postmortem's multi-strategy migration trigger.

## Cross-references

- `results/postmortem_template_decision_rule_2026-05-08.md` — symmetric template for the KILL side; section structure mirrors this one's substance
- `results/forward_paper_outcome_resolution_decision_rule_2026-05-10.md` — produces the PROMOTE verdict this artifact closes
- `results/stage_promotion_runbook_decision_rule_2026-05-08.md` — 6-phase execution sequence the closure documents
- `results/real_money_protocol_decision_rule_2026-05-08.md` — locks the per-stage stake sizes + reversal criteria
- `results/real_money_executor_architecture_decision_rule_2026-05-08.md` — Layer 2/3 gate definitions referenced in section 11 supporting data
- `results/drift_detector_calibration_verdict_2026-05-07.md` — section 4's symmetric reference for the KILL-side latency baseline
- `CLAUDE.md ## Known unmodeled risks` — section 3 maps activation status; section 6 ledgers operator-accept
- `CLAUDE.md ## Forward-paper go/no-go criteria` — section 2's reference for the locked threshold values
- Future: `scripts/stage_promotion.sh` (T2a) — implementing orchestrator the closure documents the run of
