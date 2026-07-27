# results/ index — decision rules, verdicts, and operational pre-registrations

125 markdown artifacts (including INDEX) as of 2026-06-11, organized by lifecycle stage and category. Docs through 2026-05-24 are organized in the lifecycle/category sections below; later docs are in `## Additions 2026-05-24 → 2026-06-11` at the bottom.

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
- `backtest_overfit_pbo_dsr_decision_rule_2026-05-29.md` + `_verdict_*.md` — PBO (CSCV) + Deflated Sharpe on the 34-config search (**FRAGILE**: DSR(34)=0.64<0.95 — LIVE's edge doesn't clear the multiple-testing haircut; PSR=0.97 so significant in isolation; advisory only, not a kill)

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

#### Regime timing (entry-selection follow-up, 2026-06-07)
- `btc_regime_gate_backtest_decision_rule_2026-06-07.md` — pre-reg for a BTC-anchored time-varying direction gate (SHORT/LONG/FLAT). Tests whether conditioning direction on a BTC momentum anchor beats hardcoded always-short OUT-OF-SAMPLE (walk-forward, full-57, live costs). Locked success criteria incl. "no consecutive losing years" + parameter-stability gate. Research-only; no live/shadow change in scope. Working copy: `docs/superpowers/specs/2026-06-07-btc-regime-gate-backtest-design.md`.
- `btc_regime_gate_backtest_verdict_2026-06-07.md` — **POSITIVE** (all 3 criteria). Walk-forward OOS gate +$700k vs always-short baseline −$77k; all 4 folds independently picked the SAME (X=5,Y=30,Z=30) — anti-overfit signature; top-6-by-fit combos all positive OOS (plateau, not spike). Edge is 2023-concentrated ("don't be short in the alt-bull"). Confirms direction-on-regime carries OOS signal where per-trade entry does not. Caveats: 2023-dependent, episode-length confound, gross-of-switching-cost, BTC-proxy-for-breadth. **NOT a promotion trigger** — earns a separate productionization discussion. Forward-paper/drift protocol untouched.
- `breadth_regime_gate_backtest_verdict_2026-06-08.md` — **NEGATIVE** (Crit 3 fails). Equal-weight alt-breadth anchor (16 deployed symbols). OOS gate +$758k vs baseline +$206k (raw P&L better than BTC), but Z (long-activation lookback) unstable across folds (14/14/7/30). X=5, Y=30 locks in all folds — short-gate signal independently confirmed. Long-gate is noise. BTC is cleaner anchor than alts-themselves (smoother, fewer episodes, stable Z). Shadow grids under breadth anchor cancelled. Next: shadow grids with BTC anchor + short-gate-only.
- `shadow_regime_gate_shortonly_verdict_2026-06-08.md` — **8/9 NEGATIVE/degenerate.** SHORT_ONLY sweep (LONG dropped) over live 9/21 + 8 shadows. Live + ALL 504h cohorts degenerate: gate==baseline to the cent (FLAT carve-outs never separate from always-short — 504h positions span them). live $356,329, alt5-15-504 $323,217, alt7-14-504 $326,572, alt5-21-504 $270,664, alt10-30-504 $133,559, alt12-26-504 $23,901, alt21-50-504 −$55,703, bb20 $0 — all gate==base. Only **alt5-15-336 (tight 336h hold) POSITIVE** (+$1.43M vs +$207k), likely multiple-comparisons survivor. The +$700k BTC POSITIVE was ~50% LONG flip; strip LONG → gate adds $0. **Lever is max-hold, not the gate.** (alt10-30-504 was a false-POSITIVE from an FP-dust crit1 bug, fixed commit 590ef03 — crit1 now needs ≥$1 margin.)
- `regime_switch_fullstop_decision_rule_2026-06-08.md` — **pre-reg** for a BTC regime-SWITCH (full-stop): force-close all positions + halt entries when BTC exits SHORT regime (3-day dwell guard), vs the degenerate SHORT_ONLY gate that let 504h shorts span the carve-outs. Targets operator intolerance of back-to-back negative years. 9-cohort robustness panel; PASS = flip Crit2 FAIL→PASS on strict majority of failing-at-baseline set F (no single-cohort promotion); Crit1 reported-not-gated (trade upside for survivability). Force-close primary; close-if-profitable + tighten-stops deferred-conditional. D=3 fixed. Research-only. Working copy: `docs/superpowers/specs/2026-06-08-regime-switch-fullstop-design.md`.
- `regime_switch_fullstop_verdict_2026-06-08.md` — **VOID ("no problem to solve").** F (cohorts whose always-short baseline FAILS Crit2) = 0/9 → the deployed strategy class has NO back-to-back negative years on the OOS window (2023-2026), even with no switch. Per-year baseline reds cluster at 2023 + 2026 with 2024/2025 always green → never adjacent. The consecutive-red bull years (2020-2021) are in-sample, not scored OOS. Switch is NON-degenerate (live gate-check: switch −$299k vs continuous baseline +$1.29M, $1.59M apart — unlike the degenerate SHORT_ONLY gate) but a NET COST: Crit1=NO for all 9 (switch lowers total P&L every cohort, cutting profitable non-bear exposure; e.g. live switch +$564k vs baseline +$736k, Δ−$173k). bb20 switch=$0 (Bollinger fires too rarely under episode-slicing). All folds pick (5,30). Deferred follow-ups NOT triggered (force-close didn't PASS). **Closes the regime-timing thread**: BTC-gate (LONG-flip artifact) + breadth-gate (Z unstable) + full-stop-switch (no OOS problem to fix) — the always-short class already clears the consecutive-red bar OOS; a regime overlay only reduces returns.

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
- **`post_shadow_evaluation_research_decision_rule_2026-05-27.md`** — cascaded 3-tier research sweep pre-reg, activates ONLY if current shadows fail promotion gates AND live not killed. TIER 1: Cat-B exits (4 cells); TIER 2: symbol-universe (3 cells); TIER 3: ensembles (3 cells). Each tier runs sequentially, only if prior rejected. Anti-cluster gate codifies 2026-05-27 lesson (variant rejected if backtest "edge" disappears when 2 best days removed). Max 1 new shadow per execution. Explicit anti-discovery commitments prevent garden-of-forking-paths

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
- **`milestone2_addendum_overfit_gate_and_mechanisms_2026-05-30.md`** — PROPOSED (non-actionable). Adds a batch overfit-gate (PBO/DSR at N=grid-size, closing the within-idea param-search hole the N=5 Bonferroni misses; from the 2026-05-29 FRAGILE finding) + 3 new mechanism ideas not in the 8 (H1 delta-neutral funding *carry* ≠ C1 signal / H2 cross-sectional momentum / H3 failed-pump cascade short). Ratified at milestone-2 launch via the runbook's migration triggers.

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

---

## Additions 2026-05-24 → 2026-06-11 (catch-up, appended 2026-06-11)

Note: `*_verdict_*` files for 2026-05-07/08 rules are covered by the `+ _verdict_*.md` shorthand in the sections above and are not re-listed.

### 2026-05-19 research arc (6 pre-registered walk-forward sweeps — research-only, non-actionable)
- `research_synthesis_2026-05-19.md` — INDEX for the arc; cross-cutting findings
- `ema_tf_exploratory_grid_2026-05-19.md` — EMA periods × timeframe grid (source of the 5 shadows added 2026-05-26)
- `vol_filter_sweep_2026-05-19.md`, `side_filter_validation_2026-05-19.md`, `mltp_exit_sweep_2026-05-19.md`, `trailing_stop_sweep_2026-05-19.md`, `confl_1d_bias_sweep_2026-05-19.md`, `alt_signals_retest_2026-05-19.md` — the individual sweeps
- `layer3_enablement_2026-05-19.md` — Layer 3 wrap enablement on KAVA/ENS (operational)
- `testnet_multi_symbol_extension_2026-05-19.md` — testnet scope extension (operational)

### 2026-05-22 → 2026-05-24 monitoring locks
- `drift_detector_dryrun_2026-05-22.md` — detector dry-run
- `binomial_monitor_decision_rule_2026-05-24.md` — binomial WR monitor lock
- `signal_context_capture_descriptive_2026-05-24.md` + `signal_context_consumer_decision_rule_2026-05-24.md` — C2 sidecar capture + consumer lock
- `signal_journal_reconcile_decision_rule_2026-05-24.md` + `_descriptive_2026-05-24.md` — signal/journal reconcile lock
- `w7_2026_backtest_decision_rule_2026-05-24.md` — W7 window lock

### 2026-05-29 → 2026-05-30 drift-censoring thread + ops
- `backtest_overfit_pbo_dsr_verdict_2026-05-29.md` — PBO/DSR overfit screen verdict
- `drift_detector_censoring_blindspot_finding_2026-05-30.md` — mid-censoring blind spot (feeds cross-check 9)
- `telegram_env_systemd_fix_runbook_2026-05-30.md` — ops runbook

### 2026-06-09 phantom-bug recheck batch (post side-filter class bug d1d0fae)
- `alt_signals_phantom_corrected_verdict_2026-06-09.md`, `momentum_recheck_2026-06-09.md`, `pdh_pdl_phantom_recheck_2026-06-09.md`, `purgatory_recheck_2026-06-09.md`, `overfit_expansion_2026-06-09.md`

### 2026-06-10 orthogonal-20 search (CLOSED — no survivors)
- `orthogonal_search_synthesis_2026-06-10.md` — synthesis/INDEX for the search
- Verdicts: `btc_leadlag`, `coinbase_premium`, `cross_sectional`, `cross_sectional_carry`, `crossvenue_spread`, `dvol_vrp`, `ethbtc_rv`, `listing_drift`, `live_overlay`, `macro_event`, `oi_lsratio`, `settlement_drift`, `taker_flow`, `vol_event_breakout` (all `*_verdict_2026-06-10.md`) + `final_four_verdict_2026-06-10.md`
- `strategy_candidates_2026-06-10.md`, `strategy_candidates_batch2_2026-06-10.md` — candidate pre-regs

### 2026-06-10 batch-3 search (CLOSED — FINAL 5/5 NO-GO)
- `strategy_candidates_batch3_2026-06-10.md` — pre-reg (#21–#25)
- `batch3_synthesis_2026-06-10.md` — synthesis; `batch3_21_rsi_macd_verdict_2026-06-10.md` — RSI/MACD ledger settled
- `failed_pump_verdict_2026-06-10.md` + `failed_pump_followup_decision_rule_2026-06-10.md` — #25 autopsy (killed by F6 full-history re-run)

### 2026-06-10 operational / milestone prep
- `venue_scouting_2026-06-10.md` — EEA venue port scouting (Kraken presumptive) + full-57 exact-live-config economics addendum
- `milestone2_btceth_subbook_design_2026-06-10.md` — BTC/ETH sub-book design (gate sign: complacency, VRP-z<0)

### Earlier strays (pre-05-24, previously unindexed)
- `btc_hodl_notional_amendment_2026-05-12.md` — HODL benchmark notional amendment ($16k)
- `kill_deploy_fail_taxonomy_2026-05-12.md`, `partial_canon_resolution_2026-05-12.md`, `time_anchor_resolution_2026-05-12.md` — 05-12 resolution docs

### 2026-07-13 venue port
- `docs/superpowers/specs/2026-07-13-venue-port-kraken-design.md` — **pre-reg** for Kraken Futures executor port. Locked 2026-07-13; activates at PROMOTE (~2026-08-25). Decides: venue (Kraken, Payward Europe), book (16 symbols unchanged, Binance klines), executor architecture (KrakenLive = Binance arch rule + 5 deltas), Layer 2/3-equivalent validation gates (demo shadow primary, prod micro-orders fallback), venue fallback ladder (OKX EU → Bybit EU → Hyperliquid). Supersedes "Binance only" venue clause in the staging protocol.

### 2026-07-26 cost decomposition
- `stop_distance_cost_filter_verdict_2026-07-26.md` — **REJECT** (no pre-reg opened; hypothesis falsified same-session). Durable finding: live forward-paper is **gross +$3,489**, destroyed by $11.2k costs at **67× avg leverage**; **breakeven fee = 0.20 bp**, so no reachable fee schedule (incl. maker/maker) makes the current config profitable. The suggested tight-stop filter fails a mechanism test (`corr(stop_dist, gross)` = +0.011; tightest quintile is the *best* on NET) and a permutation test (**p = 0.601**). Worked example of the PBO-0.52 prior.

- `viability_frontier_2026-07-27.md` — **THE LAW.** One-line viability constraint: E[gross R] > (fee+slip)×L×1e-4. Live measured: cost 0.0952R (certain), gross +0.0296R (95% CI [−0.41,+0.47] — 15× wider than the cost it must beat). All three levers pinned: maker fees → boundary-marginal + 55y to prove; wider stops → monotone worse; better signal → 85 trials, PBO 0.52. Includes the 30-second screening rule (require backtest gross ≥ 3× cost R) that retroactively rejects Option C/Purgatory/PDH/VWAP a priori, and the spec any future viable strategy must meet. Zero new trials — pure measurement synthesis.
- `deployed16_alt_signals_verdict_2026-07-27.md` — **NOTHING BEATS LIVE.** Direct deployed-16 test of "maybe not shorts?" and "other indicators?". Longs **−$111,118**, both-sides $314,710 (vs short-only $378,951). All 6 alternative signals lose on TRAIN (MACD $170k, RSI $168k, BB $148k, momentum $77k, VWAP −$71k, PDH/PDL **−$470,833** on $318k of fees). The one TEST "winner" (RSI +$10,997) loses significantly on TRAIN (−$210,586, p=0.006, 3/16) and is p=0.865 on TEST — sign flip = noise. Search closed on the traded book.
- `deployed16_direct_sweep_verdict_2026-07-27.md` — **LIVE CONFIG NOT BEATEN.** 15 arms swept directly on the deployed-16 (8 ATR multipliers, 3 max-holds, vol-filter, 1D-confluence, trailing stop). Live config ranks **2/15**; the only arm above it (`mh336`, +3.8%) is noise (t=0.82, p=0.42, flips negative dropping top-3). **Wick stop beats all 8 ATR multipliers and produces the highest gross of any arm** — the mirror image of the universe-57 result, confirming the selection-interaction effect. Useful negative: the live parameterization is not leaving money on the table via any existing CLI lever.
- `atr1_0_holdout_verdict_2026-07-27.md` — **NOT ACTIONABLE.** atr1.0 is universe-wide held-out significant (+$335,855, t=3.83, p<0.001, 36/57, survives dropping top-5) BUT **negative on the deployed-16 in BOTH periods** (TRAIN −$26,050 / 5-of-16; TEST −$23,808 / 6-of-16). Cause: `corr(baseline NET, improvement) = −0.523` — atr1.0 helps symbols the wick stop handles worst and hurts those it handles best; the deployed-16 were selected *under the wick stop* (mean rank 22.9 vs 31.4). **Selection-interaction effect.** No shadow, no promotion. General lesson: a universe-wide parameter win does not transfer to a book selected under the old parameter.
- `atr_stop_cost_geometry_verdict_2026-07-26.md` — **PARTIAL**. Train(2020-23)/held-out-test(2024-25.04) on `--atr-stop-mult`. atr1.5 beat baseline on train (+$54k) AND replicated out-of-sample (+$93k, NET/trade $326→$456). Decomposition: **fee cut −65% on 57/57 symbols both periods = MECHANICAL**; **gross change coin-flip (46-47%) = no edge effect**. Aggregate win NOT significant (paired t=0.998, sign-flip p=0.32, median symbol −$808, top-3 = 79% of delta). On the live book: moves **0.31× → 0.86× of breakeven** — large, real, still a loss. No promotion, no live change; shadow-engine is the only legitimate vehicle. Harness `scripts/cost_geometry_sweep.sh`.

### Amendments to existing locks
- `real_money_executor_architecture_decision_rule_2026-05-08.md` — **Addendum 2026-06-11**: Gate A basis notional → per-trade risk; Gate B cap → 10× stake (migration trigger #1, testnet integration data; see `docs/findings/2026-06-11.md`)
