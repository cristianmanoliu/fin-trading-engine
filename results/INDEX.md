# results/ index — decision rules, verdicts, and operational pre-registrations

53 markdown artifacts as of 2026-05-08, organized by lifecycle stage and category.

**Three artifact types:**
- `*_decision_rule_*.md` — pre-registered: rule LOCKED before data is observed
- `*_verdict_*.md` — mechanical: rule applied to data, produces ACCEPT/REJECT
- Synthesis / state docs (no _rule / _verdict suffix) — narrative or operational

The asymmetric ratio (rules → verdicts) reflects today's heavy pre-registration phase: 9 operational pre-regs locked on 2026-05-08 await activation under future operational events.

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

### Stage 2 — Forward-paper → real-money gateway

Sits between mechanical DEPLOY-READY verdict and STAGE_1 promotion. The discipline payoff moment.

- **`forward_paper_completion_review_decision_rule_2026-05-08.md`** — qualitative audit gate; three sections (cost realization / empirical-vs-prediction shape / anomaly + qualitative); composite GREEN/AMBER/RED; required artifact + AMBER resolution path

### Stage 3 — Real-money execution (STAGE_1 through STAGE_4)

The 4-stage protocol: $100 → $300 → $500 → $1000. Each promotion is a high-stakes moment with locked execution and verification.

- **`real_money_protocol_decision_rule_2026-05-08.md`** — stage criteria (the master rule); locked kill criteria (drift firing, slip>30bp, 3-consecutive-net-loss-days, single-sym>50%, 20% drawdown, unrecoverable error)
- **`real_money_executor_architecture_decision_rule_2026-05-08.md`** — design for the executor that replaces Stub at STAGE_1; 5-component decomposition (BinanceLive / OrderRouter / PositionReconciler / SafetyGates / KillSwitch); F1-F5 failure-mode taxonomy; three-layer testing strategy
- **`stage_promotion_runbook_decision_rule_2026-05-08.md`** — six-phase execution sequence per promotion; per-stage parameter table; rollback path

### Stage 4 — Kill / pause / postmortem

When something goes wrong, these locks govern the response.

- **`auto_kill_execution_decision_rule_2026-05-08.md`** — six-phase fleet-wide kill (HALT / OPEN-POSITIONS / ARCHIVE / DOCUMENT / NOTIFY / POSTMORTEM); SOFT vs HARD tiers; verification gates between phases
- **`per_symbol_pause_decision_rule_2026-05-08.md`** — single-engine pause without violating "Don't reshuffle the deployed-32"; mechanical attribution (a) performance (FORBIDDEN) vs (b) external (ALLOWED with documented source + independent verification)
- **`postmortem_template_decision_rule_2026-05-08.md`** — locked 8-section structure for postmortem appended to every kill artifact; mechanism diagnosis taxonomy (GONE / REGIME-CONDITIONED / COST-DOMINATED / CONFOUNDED); 7-day window

### Stage 5 — Next milestone

Pre-registered now; activates when current milestone closes.

- **`strategy_backlog_milestone2_2026-05-08.md`** — 8 mechanism ideas (A1 vol-regime / A2 ATR-sizing / B1 RSI-extremum / B2 BB-squeeze / C1 funding-extremum / D1 session-filter / F1 multi-TF-ensemble / G1 tick-imbalance) with locked verdict criteria, prioritization rubric, recommended top-5 selection

---

## Index by category (alternative view)

### Locked decision rules (rule pre-registered, awaits data)
26 files. The pre-registration backbone of the project.

### Verdicts (rule applied to data)
22 files. Each verdict mechanically applies its corresponding decision rule to specific data.

### Synthesis / state docs
5 files. Narrative summaries that don't fit the rule/verdict pattern (e.g., `holy_grail_synthesis`, `cost_stack_sensitivity`).

### Locked 2026-05-08 (today)
The 9 operational pre-regs locked today, organized by what they govern:

- **WHEN to detect:** drift_wrapper_cron
- **HOW to triage:** drift_firing_investigation
- **HOW to kill (fleet):** auto_kill_execution
- **HOW to pause (one symbol):** per_symbol_pause
- **HOW to build (executor):** real_money_executor_architecture
- **HOW to gate (paper→real):** forward_paper_completion_review
- **HOW to promote (each stage):** stage_promotion_runbook
- **HOW to learn (after kill):** postmortem_template
- **WHAT to research (next):** strategy_backlog_milestone2

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
