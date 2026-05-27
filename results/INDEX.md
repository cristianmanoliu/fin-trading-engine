# results/ index — decision rules, verdicts, and operational pre-registrations

63 markdown artifacts (including INDEX) as of 2026-05-24, organized by lifecycle stage and category.

**Three artifact types:**
- `*_decision_rule_*.md` — pre-registered: rule LOCKED before data is observed
- `*_verdict_*.md` — mechanical: rule applied to data, produces ACCEPT/REJECT
- Synthesis / state docs (no _rule / _verdict suffix) — narrative or operational

The asymmetric ratio (rules → verdicts) reflects today's heavy pre-registration phase: 10 operational pre-regs locked on 2026-05-08 + 3 pre-regs locked on 2026-05-10 (LIMBO outcome resolution + milestone-2 launch runbook + PROMOTE-closure template) await activation under future operational events.

---

## Charter — why this directory exists

Every analytical claim and every operational decision in the project is pre-registered: the rule is LOCKED before any data is examined or any event occurs. This is not bureaucracy — it's the only way to keep results trustworthy under the volume of analyses the project has run (50+ across 6 months) and under the operational stress of forward-paper / real-money progression.

The discipline against which this directory was designed:

1. **No post-hoc strategy selection.** Once data is observed, picking the "winning" subset and reporting only that is exactly how published quant research generates spurious results. Pre-registration banks the alpha cost up front.

2. **No improvised operational responses.** A drift firing, a kill trigger, a STAGE-promotion event — these all happen under maximum stress. The locked playbook converts stress into mechanical execution.

3. **No undocumented threshold drift.** Every numerical threshold (12 bp fee, 25 bp slip, 90s heartbeat, 7-day firing pair, $100/$300/$500/$1000 stakes) is locked in a specific document with rationale. Changing them requires a documented migration trigger.

4. **No silent contract changes.** Code changes during a milestone are subject to Section C8 of the completion review (pure bug fixes OK; behavior changes contaminate data). Pre-registrations themselves are subject to migration triggers — locked but updatable through a documented process.

The cost of this discipline is real (writing pre-regs takes time, the operator can't optimize on the fly). The benefit is that the project's claims and operational decisions are AUDITABLE — for future-self, for collaborators, and for any retrospective analysis.

---

## Index by lifecycle stage

### Stage 0 — Milestone 1 research (2026-05-06 → 2026-05-07)

Backtest investigation set on 5y × 16-57 symbols. Pre-registered analyses establish the deployed strategy's expected NET, validate the edge, and close the strategy-class search.

#### Cumulative-claim validation
- `bootstrap_ci_verdict_2026-05-07.md` — block bootstrap CI on annual NET (mean +$130k/yr; walk-forward 95% [-$111k, +$372k]); regime variance dominates trade-level autocorrelation
- `sample_split_bootstrap_decision_rule_2026-05-07.md` + `_verdict_*.md` — train/test independence check (ROBUST_BOTH)
- `edge_stability_decision_rule_2026-05-07.md` + `_verdict_*.md` — edge stable across regimes (STABLE_EDGE)

#### Strategy categories (Cat A through Cat X)
- Cat A: `cat_a_decision_rule_2026-05-06.md`, `cat_a_macd_bb_verdict_2026-05-07.md`
- Cat B: `cat_b_decision_rule_2026-05-07.md`, `cat_b_b1_b2_verdict_2026-05-07.md`
- Cat BB (Bollinger): `cat_bb_robustness_rule_2026-05-07.md`, `cat_bb_robustness_verdict_2026-05-07.md`
- Cat D2: `cat_d2_sharpe_gated_rule_2026-05-07.md`, `cat_d2_d1_sharpe_verdict_2026-05-07.md`, `cat_d2a_decision_rule_2026-05-07.md`, `cat_d2a_verdict_2026-05-07.md`
- Cat DE: `cat_de_decision_rule_2026-05-07.md`, `cat_de_d1_e1_verdict_2026-05-07.md`
- Cat F1 (funding cross): `cat_f1_funding_cross_decision_rule_2026-05-07.md` + `_verdict_*.md`
- Cat F2 (co-cross): `cat_f2_cocross_confluence_decision_rule_2026-05-07.md` + `_verdict_*.md`
- Cat G (F1×F2 composite): `cat_g_f1xf2_composite_decision_rule_2026-05-07.md`, `cat_g_f1xf2_composite_verdict_2026-05-08.md` (REJECT)
- Cat G' (universe-57): `cat_g_prime_universe57_decision_rule_2026-05-08.md` + `_verdict_*.md` (REJECT)
- Cat X (Bybit cross-exchange): `cat_x_bybit_replication_decision_rule_2026-05-08.md` + `_verdict_*.md` (ROBUST)
- Synthesis: `holy_grail_synthesis_2026-05-07.md` — strategy-class search summary

#### Cost / fee modeling
- `cost_stack_sensitivity_2026-05-07.md` — sensitivity at fee=6/8/10/12/15 bp
- `slip_cliff_verdict_2026-05-07.md` — slippage cliff at ~81bp (kill criterion 25bp has substantial margin)
- `mfe_mae_verdict_2026-05-07.md` — MFE/MAE distribution analysis from journal data
- `hod_decomposition_verdict_2026-05-07.md` — hold-out journal decomposition
- `b3_features_verdict_2026-05-07.md` — feature audit; symbol heterogeneity per mean_price + mean_daily_vol_m

#### Window selection
- `6window_decision_rule_2026-05-06.md` — walk-forward window selection
- `vwap_decision_rule_2026-05-06.md` — VWAP signal-tf

### Stage 1 — Forward-paper monitoring (2026-05-05 → ongoing)

Live paper-trading; weekly drift detection; threshold cross-checks. The kill rules are advisory until forward-paper completes.

#### Drift detection (the decision-grade kill mechanism)
- `kill_bar_calibration_decision_rule_2026-05-07.md` + `_verdict_*.md` — threshold-based kill bar (NEEDS_RECALIBRATION)
- `kill_bar_recal_decision_rule_2026-05-07.md` + `_verdict_*.md` — recalibration attempt (NO_KILL_BAR — statistical impossibility at 90d due to distribution overlap)
- `drift_detector_calibration_decision_rule_2026-05-07.md` + `_verdict_*.md` — distribution-based detector (DETECTOR_VIABLE; α_family=0.001, N_LIVE=50)
- `drift_detector_time_to_detection_decision_rule_2026-05-08.md` + `_verdict_*.md` — sequential-FP analysis (NEEDS_TUNING; daily inflates FP to 28%/year; locked weekly cadence)
- **`drift_wrapper_cron_decision_rule_2026-05-08.md`** — phased deployment (manual through STAGE_2; VPS systemd timer required at STAGE_3+)
- **`drift_firing_investigation_decision_rule_2026-05-08.md`** — three-tier triage A/B/C with locked cross-checks per tier

#### Operator alerting layer
- **`telegram_alert_design_decision_rule_2026-05-08.md`** — 3-tier alert system (INFO/WARN/CRITICAL) with locked rate limits, mute-hour semantics, per-event tier assignments, and aggregation behavior. Activates when first non-startup/shutdown alert fires; binds every other locked rule that references "Telegram alert" to a consistent format and cadence

#### Shadow promotion
- **`shadow_promotion_decision_rule_2026-05-27.md`** — locked 8-gate criteria for swapping a shadow variant (alt5-15-*, alt5-21, alt7-14, alt10-30, alt12-26, alt21-50, bb20) into the live config. Activates when any shadow first reaches n=63 (~2026-06-08 earliest for alt5-15-*). Two-proportion z-test p<0.001 (Bonferroni for 7 shadows), shadow PnL ≥ 1.5× |live PnL| or +$20k with live ≤ 0, mechanism-confirmed via shared-day analysis, atomic-swap Phases A-D with 14d post-promotion validation gate. Auto-kill protocol takes precedence — if drift fires, this rule pauses

### Stage 2 — Forward-paper → real-money gateway

Sits between mechanical DEPLOY-READY verdict and STAGE_1 promotion. The discipline payoff moment.

- **`forward_paper_completion_review_decision_rule_2026-05-08.md`** — qualitative audit gate; three sections (cost realization / empirical-vs-prediction shape / anomaly + qualitative); composite GREEN/AMBER/RED; required artifact + AMBER resolution path
- **`forward_paper_outcome_resolution_decision_rule_2026-05-10.md`** — five-verdict mechanical decision tree (CONTINUE / WATCH / PROMOTE / KILL / OPERATOR_REVIEW) for the LIMBO case (calendar gate met but trade gate not, or any genuinely ambiguous outcome); locks the response BEFORE the data forces a heuristic call. Rule 3 explicitly handles 2026-05-10's emotional reaction to n=9 by mandating CONTINUE below n=50
- **`cohort_outcome_join_decision_rule_2026-05-24.md`** — post-resolution analysis methodology: joins signal-context sidecar records to journal close events, computes per-cohort outcome distributions across 4 cohorts (live + 3 shadows), and applies a locked 4-verdict tree (DOMINATE / MATCH / UNDERPERFORM / CONTRADICTION) with family-wise Bonferroni correction (α_family=0.05, N=3). PRIMARY metric: mean PnL/trade (unequal-n safe). DOMINATE → milestone-2 candidate only (not auto-promote). Forbids per-symbol cherry-picking, post-hoc time-window slicing, and invocation before resolution artifact exists (exit 5 PRE_RESOLUTION).
- **`resolution_data_freeze_decision_rule_2026-05-24.md`** — locks the snapshot-and-bundle procedure at the resolution moment: 12-file canonical bundle into `results/freeze/<t_freeze>/` (all fresh runs, NOT prior cron copies), `t_freeze` definition (UTC at verdict-emit, floor-day `elapsed` for pro-rate), in-flight position exclusion from numerator, gate-ordering hysteresis (`n_ever ≥ 150` not `n_current`), drift-history-as-of-freeze (fresh check required; INSUFFICIENT does not re-veto verdict), and 6 exit codes including exit-4 ALREADY_FROZEN one-shot guard. Upstream of `honest_annual_prorate_decision_rule` (defines canonical `elapsed`) and `cohort_outcome_join_decision_rule_2026-05-24.md` (defines calendar bounds for matched-window analysis).
- **`honest_annual_prorate_decision_rule_2026-05-24.md`** — locks the exact arithmetic for the "≥60% of pro-rated honest-annual" gate (STAGE_0→STAGE_1) and "≥50%" (STAGE_3→STAGE_4): `elapsed = floor((t_freeze − t0) / 86400)`, baseline frozen at $69k/yr, denominator 365.25, `live_pnl_usd` = realized closed live-cohort trades only (no shadow journals, no unrealized). Resolves all definition ambiguities in `real_money_protocol_decision_rule_2026-05-08.md`. Downstream of `resolution_data_freeze_decision_rule_2026-05-24.md` (canonical `elapsed` and journal source).

### Stage 3 — Real-money execution (STAGE_1 through STAGE_4)

The 4-stage protocol: $100 → $300 → $500 → $1000. Each promotion is a high-stakes moment with locked execution and verification.

- **`real_money_protocol_decision_rule_2026-05-08.md`** — stage criteria (the master rule); locked kill criteria (drift firing, slip>30bp, 3-consecutive-net-loss-days, single-sym>50%, 20% drawdown, unrecoverable error)
- **`real_money_executor_architecture_decision_rule_2026-05-08.md`** — design for the executor that replaces Stub at STAGE_1; 5-component decomposition (BinanceLive / OrderRouter / PositionReconciler / SafetyGates / KillSwitch); F1-F5 failure-mode taxonomy; three-layer testing strategy
- **`stage_promotion_runbook_decision_rule_2026-05-08.md`** — six-phase execution sequence per promotion; per-stage parameter table; rollback path
- **`promote_closure_template_decision_rule_2026-05-10.md`** — 11-section closure artifact written at each PROMOTE moment (paper→STAGE_1 + each intra-real-money stage transition); symmetric to `postmortem_template` on the KILL side; section 5 forces explicit counterfactual / non-promote-evidence audit to defeat success-bias; section 6 risk-acceptance ledger requires operator's explicit accept of each known unmodeled risk at the new stake size; 7-day completion window

### Stage 4 — Kill / pause / postmortem

When something goes wrong, these locks govern the response.

- **`auto_kill_execution_decision_rule_2026-05-08.md`** — six-phase fleet-wide kill (HALT / OPEN-POSITIONS / ARCHIVE / DOCUMENT / NOTIFY / POSTMORTEM); SOFT vs HARD tiers; verification gates between phases
- **`per_symbol_pause_decision_rule_2026-05-08.md`** — single-engine pause without violating "Don't reshuffle the deployed-32"; mechanical attribution (a) performance (FORBIDDEN) vs (b) external (ALLOWED with documented source + independent verification)
- **`postmortem_template_decision_rule_2026-05-08.md`** — locked 8-section structure for postmortem appended to every kill artifact; mechanism diagnosis taxonomy (GONE / REGIME-CONDITIONED / COST-DOMINATED / CONFOUNDED); 7-day window

### Stage 5 — Next milestone

Pre-registered now; activates when current milestone closes.

- **`strategy_backlog_milestone2_2026-05-08.md`** — 8 mechanism ideas (A1 vol-regime / A2 ATR-sizing / B1 RSI-extremum / B2 BB-squeeze / C1 funding-extremum / D1 session-filter / F1 multi-TF-ensemble / G1 tick-imbalance) with locked verdict criteria, prioritization rubric, recommended top-5 selection
- **`milestone2_runbook_decision_rule_2026-05-10.md`** — locked 5-phase execution sequence (A2 → A1 → C1 → B2 → D1), family-wise α=0.01 (Bonferroni at α_family=0.05, N=5), per-phase verdict template, mid-sequence rollback paths, audit-lens compliance contract for `scripts/milestone2_launch.sh`. Bridges the strategy backlog with the milestone-2 trigger conditions in `real_money_protocol`.

---

## Index by category (alternative view)

### Locked decision rules (rule pre-registered, awaits data)
35 files. The pre-registration backbone of the project. (+1 telegram_alert_design 2026-05-08 + 1 forward_paper_outcome_resolution 2026-05-10 + 1 milestone2_runbook 2026-05-10 + 1 promote_closure_template 2026-05-10 + signal_context_consumer/signal_journal_reconcile/binomial_monitor/cohort_outcome_join/resolution_data_freeze/honest_annual_prorate 2026-05-24.)

### Verdicts (rule applied to data)
22 files. Each verdict mechanically applies its corresponding decision rule to specific data.

### Synthesis / state docs
5 files. Narrative summaries that don't fit the rule/verdict pattern (e.g., `holy_grail_synthesis`, `cost_stack_sensitivity`).

### Locked 2026-05-08 (operational hardening day)
10 operational pre-regs locked, organized by what they govern:

- **WHEN to detect:** drift_wrapper_cron
- **HOW to triage:** drift_firing_investigation
- **HOW to alert:** telegram_alert_design — binds every "Telegram alert" reference in the other 9 to a consistent tier (INFO/WARN/CRITICAL), rate limit, and mute-hour policy
- **HOW to kill (fleet):** auto_kill_execution
- **HOW to pause (one symbol):** per_symbol_pause
- **HOW to build (executor):** real_money_executor_architecture
- **HOW to gate (paper→real):** forward_paper_completion_review
- **HOW to promote (each stage):** stage_promotion_runbook
- **HOW to learn (after kill):** postmortem_template
- **WHAT to research (next):** strategy_backlog_milestone2

### Locked 2026-05-10 (LIMBO resolution + milestone-2 launch prep + PROMOTE-closure)
3 pre-regs locked, all filling rule→tool gaps before the events they govern:

- **HOW to resolve (calendar gate met, trade gate not):** forward_paper_outcome_resolution — five-verdict tree (CONTINUE / WATCH / PROMOTE / KILL / OPERATOR_REVIEW) implemented mechanically by `scripts/forward_paper_resolution.py`, wired into `weekly_audit.sh` stage 6
- **HOW to execute (milestone-2 candidate sweep):** milestone2_runbook — locked A2 → A1 → C1 → B2 → D1 sequence with family-wise α=0.01, per-phase verdict template, mid-sequence rollback paths. Implemented mechanically by `scripts/milestone2_launch.sh` with trigger gate (refuses to run until paper→STAGE_1 promotion or kill artifact present)
- **HOW to document (PROMOTE moment):** promote_closure_template — 11-section closure artifact for every stage promotion (paper→STAGE_1 + each intra-real-money transition). Symmetric to postmortem_template on the kill side. The 6th section (risk-acceptance ledger) makes the operator explicitly sign for each known unmodeled risk at the new stake size; the 5th (counterfactual) forces explicit "evidence against promotion" review to defeat success-bias.

---

## Cross-reference graph (how the locks compose)

```
                        forward-paper accumulating
                                 │
                                 ▼
  drift_wrapper_cron ────────► run_drift_check.sh
                                 │
                                 ├─ exit 0/2/3 ─► continue
                                 │
                                 ├─ exit 1 (single firing)
                                 │  └─► drift_firing_investigation
                                 │       (TRIAGE A/B/C)
                                 │       └─► continue OR escalate
                                 │
                                 └─ exit 4 (auto-kill candidate)
                                    │
                                    ▼
  forward_paper_completion_review (paper→STAGE_1 only)
                                    │
                                    ▼
  stage_promotion_runbook ─────► STAGE_N+1
                                    │
            (also-fires-on-trigger) │
                                    ▼
  real_money_executor_architecture ◄──── STAGE_1+ activates
                                    │
                                    ▼
  auto_kill_execution (or per_symbol_pause if granular)
                                    │
                                    ▼
  postmortem_template (within 7 days)
                                    │
                                    ▼
  strategy_backlog_milestone2 ◄─── action items feed
```

---

## CLAUDE.md anchor

The full `CLAUDE.md ## Forward-paper go/no-go criteria` section is the operational doctrine. The locks here are the mechanical implementations of that doctrine.

When in doubt during a real operational event: **find the locked rule, apply mechanically, document the application.** The discipline IS the process.
