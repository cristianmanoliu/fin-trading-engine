# Drift firing investigation — locked decision rule (2026-05-08)

**Status:** LOCKED 2026-05-08, before any drift firing has occurred. Applies mechanically when triggered. Co-locked with:
- `results/drift_detector_calibration_verdict_2026-05-07.md` (operating point: α_family=0.001, N_LIVE=50)
- `results/drift_detector_time_to_detection_verdict_2026-05-08.md` (cadence: weekly; single firing = investigation, two firings ≥7d apart = auto-kill candidate)
- `results/drift_wrapper_cron_decision_rule_2026-05-08.md` (phased cron deployment by STAGE)

## Question

When `scripts/run_drift_check.sh` returns **exit code 1** (single drift firing — investigation-grade per the locked time-to-detection rule), what does the operator do? The wrapper records the firing in history but does not prescribe a response. Without a locked playbook the operator's first reaction is improvised:
- **Over-reaction risk:** treat noise as kill, prematurely terminate the strategy.
- **Under-reaction risk:** dismiss real degradation as noise, accumulate losses for weeks before the second firing fires the rule.
- **Inconsistency risk:** different decisions on equivalent firings depending on operator mood, time pressure, or familiarity with the codebase.

The cost of inconsistency scales with real-money exposure. At STAGE_3+ ($500-$1k/trade) a delayed response can cost thousands; locking the playbook today (when stakes are zero) prevents that.

## Locked decision rule — three triage tiers

The depth of investigation scales with the strength of the firing. Tier classification is **deterministic**, not judgment-based:

### TRIAGE-A — light path (~30 min)

**Trigger:** ONE metric fires (n_metrics_with_drift = 1) AND its p-value satisfies p > α_bonferroni / 10 (i.e., the metric fired marginally — within 10× of the Bonferroni threshold, not deeply below it).

**Required cross-checks:**
1. Operational state: `./scripts/post_deploy_check.sh STRICT=1` — exit 0 required.
2. Forward-paper threshold: `./scripts/forward_paper_status.sh` — verdict not "KILL", realized fee bps ≤ 12, slip bps ≤ 25.
3. Confounder check: `data/funding/*.csv` last-modified date < 14 days (no funding-CSV staleness).

**Decision rule:**
- All three checks clean → classify as likely noise. Document briefly. Continue per locked weekly cadence. The rule's two-firings-≥7d gate is the safety net: if this was real degradation, it will fire again in 7+ days and trigger auto-kill.
- ANY check anomalous → escalate to TRIAGE-B (treat as if multi-metric).

### TRIAGE-B — full path (~1-2 hours)

**Trigger:** Multiple metrics fired (n_metrics_with_drift ≥ 2) OR single-metric fired with p < α_bonferroni / 10 (deeply below threshold) OR escalated from TRIAGE-A by an anomalous cross-check.

**Required cross-checks (B inherits A's three plus):**
4. Per-metric breakdown: open the per-run log at `results/drift_runs/<ts>.log`. Identify which metrics fired (pnl_per_trade, mfe_r, mae_r, loss_only_pnl, win_only_pnl, WR). Note their direction (Δ sign).
5. Directional consistency: do the firing metrics tell a single coherent story? E.g., low pnl_per_trade + low MFE + high MAE = "strategy degraded" (consistent). Or contradictory (e.g., low pnl + high WR = unclear).
6. Symbol concentration: re-parse the journal — is one symbol responsible for >50% of the recent loss/win contribution? Concentration-driven firings may be symbol-specific, not strategy-wide.
7. Recent operational events: `git log --since="14 days ago" -- pkg/ cmd/ deploy/` — any code change that could explain a behavioral shift? `ssh root@VPS "journalctl -u 'paper-live@*.service' --since='14 days ago' | grep -E 'restart|crash|recover'"` — any restart/recovery cluster?
8. Exchange-side changes: check Binance announcements (binance.com/en/support/announcement) for the past 14 days for fee schedule, contract spec, or funding-rate-formula changes.

**Decision rule (B):**
- Cross-checks all clean (no confounder, no operational anomaly, no recent code change, no exchange change) AND directional consistency holds → this is a real signal of possible degradation. Document fully. **Increase wrapper cadence to every 3-4 days for 2 weeks** (operator runs `./scripts/run_drift_check.sh` more frequently). If a second firing occurs in that 2-week window AND it's ≥7d after this one, the auto-kill rule fires per the locked rule.
- One or more confounders found → fix the confounder (refresh funding CSVs / restart engines / await exchange spec stabilization), document the fix, run wrapper again same week. The new run replaces this one in the rule's history if the confounder is the proximate cause.
- Directional inconsistency → flag as ambiguous, document, continue per cadence.

### TRIAGE-C — deep path (~2-4 hours)

**Trigger:** All of: ≥3 metrics fired with directional consistency AND `forward_paper_status.sh` verdict is "KILL" or contains a single-criterion FAIL AND operational state shows ≥1 anomaly.

This is essentially "second firing without yet being 7 days apart" — a strong-signal proximate-to-auto-kill state.

**Required actions (C inherits B's plus):**
9. Convene a structured pause: stop processing any pending STAGE-promotion (per `real_money_protocol_decision_rule_2026-05-08.md`).
10. Audit per-symbol P&L over the last 30 days; consider position-reducing the worst-contributing symbol(s) pre-emptively.
11. Run `./scripts/run_drift_check.sh` again in 24-48 hours, not 7 days, to confirm or refute the signal. (Note: this consumes a small amount of FP budget — the time-to-detection verdict's "weekly only" rule applies to NORMAL operation; in TRIAGE-C the increased cadence is justified by the higher-stakes context.)
12. Pre-draft the auto-kill execution checklist: which engines stop, which configs archive, which journal directories rotate to `archive/`. Do not execute yet — but be ready.

**Decision rule (C):**
- Second firing within 24-48h confirms → auto-kill candidate even before the formal 7d window. Apply the kill execution checklist; archive engines; convene retrospective.
- Re-test clean → likely first-firing was noise inflated by the cross-check anomaly. Document the divergence between the two runs as a data point for future calibration. Resume cadence.

## Per-firing documentation

Every firing investigation produces a file:

```
results/drift_firings/<YYYY-MM-DD>-<TRIAGE-A|B|C>-<context_slug>.md
```

Where `<context_slug>` is a short symbol-or-mechanism identifier (e.g., "imxusdt-mae", "general-pnl"). One file per firing event.

**Required fields:**

```markdown
# Drift firing investigation — <date>

**Triage tier:** A | B | C
**Wrapper exit code:** 1 (single firing) | 4 (auto-kill candidate)
**Metrics fired:** <list with p-values>
**n_live at firing:** <integer>

## Cross-checks
- Operational state (post_deploy_check): clean | anomaly
- Forward-paper threshold: clean | flag
- Funding-CSV freshness: ≤14 days | stale
- (B+) Per-metric directional consistency: consistent | mixed | contradictory
- (B+) Symbol concentration: balanced | <symbol> = X% of recent contribution
- (B+) Recent code/deploy events: none | <list>
- (B+) Exchange-side changes: none | <list>
- (C only) STAGE-promotion paused: yes/N/A

## Decision

**Verdict:** continue per cadence | fix-confounder-then-retest | intensify-cadence | auto-kill-candidate

**Rationale (1-3 sentences):** <text>

**Next wrapper run scheduled:** <YYYY-MM-DD>

## Audit trail
- Run log: `results/drift_runs/<ts>.log`
- Cross-check transcripts: <inline or linked>
```

The completed file is committed to the repo. The audit trail is the safety net against operator drift across firings.

## Time budget

Tier-A within 24h of firing (real money: within 8h).
Tier-B within 48h of firing (real money: within 24h).
Tier-C within 4h of firing (regardless of money status — high-stakes context).

A firing whose investigation expires the budget without resolution escalates one tier (A→B, B→C). Stale investigations (where market state has moved on) are useless and the data point is degraded.

## Edge cases — pre-locked

### STAGE_1+ real money active

Same playbook applies but time budgets halve (Tier-A ≤ 8h, Tier-B ≤ 24h, Tier-C ≤ 4h unchanged). Real-money exposure makes delayed response materially more costly.

### Multiple firings within 7 days (NOT ≥7d apart)

The auto-kill rule (`results/drift_detector_time_to_detection_verdict_2026-05-08.md`) requires firings ≥7 days apart. Two firings in the same week do NOT trip auto-kill mechanically — but they ARE a stronger signal than a single firing. Treat the second-within-7d as TRIAGE-B at minimum (escalate from TRIAGE-A even if it would normally classify as light), and document explicitly that the auto-kill clock does not start until the next ≥7d-apart firing.

### Wrapper exit code 3 (ERROR)

This is a TOOLING failure, not a strategy signal. Do NOT apply this playbook. Instead:
- Read the run log to identify the failure mode (SSH timeout, jq parse error, missing reference data, network failure).
- Fix the underlying issue.
- Re-run the wrapper. The wrapper's history index records exit-3 entries — these do not count as drift firings for the rule.

### Wrapper exit code 2 (INSUFFICIENT)

Insufficient live trades (n_live < 30). No firing. Skip the playbook entirely. The wrapper will return INSUFFICIENT until enough closes accumulate. This is the expected state during early forward-paper.

### A confounder is fixed mid-investigation

If a confounder is identified and fixed (e.g., funding CSVs refreshed, engine restarted to load the fix), the investigation can re-run the wrapper to see if the firing persists. If the post-fix run is CLEAN, the original firing is "confounded — root cause documented" and does not count toward the auto-kill rule. If the post-fix run STILL fires, both firings are real (and they're within hours of each other, so the second firing is BENIGN-but-corroborating, not auto-kill-triggering).

### Symbol-specific concentration finding

If a single symbol contributes >50% of recent loss (per cross-check 6), document and consider symbol-level intervention (pause that engine pending review). The strategy-wide drift firing is not invalidated, but the proximate cause is symbol-specific. Consider whether one bad symbol explains the firing without strategy-wide degradation.

## Alternatives considered (rejected)

### Always run TRIAGE-B regardless of firing strength

**Considered:** lock a single procedure, no triage tiers, every firing gets the full investigation.

**Rejected because:** opportunity cost — if 80% of single-marginal firings are noise (a plausible base rate given α_family=0.001 with multi-symbol traffic), spending 1-2 hours on each is a tax on real work. TRIAGE-A's 30 min is the right cost for likely-noise firings; the safety net is the auto-kill rule (any real degradation will fire again).

### Always run TRIAGE-C regardless of firing strength

**Considered:** maximally cautious — every firing pauses STAGE promotions, tightens cadence, pre-drafts kill checklist.

**Rejected because:** alert fatigue + operational drag. STAGE promotion is gated on monthly criteria; pausing for every false-alarm firing would delay real promotions for weeks. The rigor frame already includes the auto-kill rule as the safety net; over-applying it is the opposite of mechanical-rule-application.

### Operator-judgment-driven (no locked tiers)

**Considered:** rely on operator experience.

**Rejected because:** this is exactly what the pre-registration discipline is designed against. Different operators (or the same operator on different days) make different judgment calls under stress. Mechanical rules eliminate that variance.

## Migration triggers

The rule re-opens for design when:

1. **First firing actually occurs** — review whether the playbook held up in practice. If TRIAGE-A took longer than 30 min in real conditions, recalibrate. If a triage classification turned out wrong (TRIAGE-A was actually a real degradation), tighten the classification thresholds.
2. **Promotion to STAGE_3** — real-money exposure crosses $500/trade. Time budgets may need further halving; cross-check 8 (exchange-side) becomes mandatory.
3. **Detector calibration changes** — if `live_vs_backtest_drift.py`'s α_family or N_MIN changes, the playbook's references to those values need updating.
4. **Wrapper exit-code semantics change** — any change to `run_drift_check.sh` exit codes (currently 0/1/2/3/4) requires updating the playbook's trigger conditions.

## Cross-references

- `results/drift_detector_calibration_verdict_2026-05-07.md` — operating point + locked rule
- `results/drift_detector_time_to_detection_verdict_2026-05-08.md` — cadence + auto-kill rule
- `results/drift_wrapper_cron_decision_rule_2026-05-08.md` — phased cron deployment
- `results/real_money_protocol_decision_rule_2026-05-08.md` — STAGE promotion criteria (referenced by TRIAGE-C step 9)
- `scripts/run_drift_check.sh` — emits the exit codes this rule responds to
- `scripts/forward_paper_status.sh` — cross-check 2 + 7
- `scripts/post_deploy_check.sh` — cross-check 1
- `CLAUDE.md ## Forward-paper go/no-go criteria → Kill mechanism` — operational doctrine
