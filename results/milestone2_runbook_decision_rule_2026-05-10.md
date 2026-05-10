# Milestone-2 launch <abbr title="A locked execution sequence for a high-stakes operation. Same procedure each time, parameterized by stage. Prevents improvisation under stress.">runbook</abbr> — locked <abbr title="The locked criterion BEFORE data is observed. Mechanical when applied — produces a verdict (ADOPT/REJECT/HOLD/CONTRADICTION). Pre-registration discipline prevents post-hoc story-fitting.">decision rule</abbr> (2026-05-10)

> 📖 **New to the jargon?** Hover over <abbr title="Hover over me to see this glossary tooltip.">underlined terms</abbr> for a one-line explanation, or read `docs/GLOSSARY.md` for the full plain-English reference.

**Status:** LOCKED 2026-05-10. Activates when milestone-2 trigger fires (paper→<abbr title="First stage of paper-to-real-money: $100/trade. Stages 1-4 ($100→$300→$500→$1000) gradually scale exposure as live history accumulates per real_money_protocol_decision_rule_2026-05-08.md.">STAGE_1</abbr> promotion artifact written, OR HARD KILL artifact written for the deployed strategy). Until then, `scripts/milestone2_launch.sh` refuses to run.

This rule sits BETWEEN two upstream locks:
- `results/strategy_backlog_milestone2_2026-05-08.md` — locks the 8 candidate ideas + their per-idea verdict criteria + the top-5 selection (A2, A1, C1, B2, D1).
- `results/real_money_protocol_decision_rule_2026-05-08.md` — defines the milestone-2 trigger conditions.

What's NOT yet locked (and what this rule fills): **the order in which the top-5 candidates are tested, the α-correction mechanics, and the rollback path if any verdict surprises mid-sequence.** Without this lock, milestone-2 launch is improvised under the same emotional pressure as STAGE promotion — exactly when discipline matters.

## Why this rule exists now (2026-05-10)

Milestone 1 closed with 11 <abbr title="Locking the rule (threshold, gate, decision) BEFORE looking at the data. Prevents post-hoc story-fitting.">pre-registrations</abbr> and 5 mechanism-class verdicts. Milestone 2's backlog locked 8 ideas on 2026-05-08 with the explicit instruction "Do NOT execute any of these before milestone 2 begins."

The "Do NOT execute" guard is enforced by `scripts/milestone2_launch.sh`'s trigger check. The "what to run, in what order, against what α" is THIS rule. Both must be in place before milestone-2 starts; otherwise the trigger fires and the operator improvises.

Lock the rule before the data forces a heuristic call. Same discipline as <abbr title="Trading the strategy with real market data and exchange feeds, but fake money. Validates the engine without risking capital.">forward-paper</abbr> outcome resolution.

## Trigger condition (binding gate)

The launch script refuses to run unless ONE of these is true:

| Trigger | Detection mechanism |
|---|---|
| paper → STAGE_1 promotion completed | `results/stage_promotion_<date>.md` exists with `Composite verdict: ALL_GREEN` for paper→STAGE_1 |
| Deployed strategy killed | `results/kill_<date>.md` exists |
| Operator override (escape hatch) | `MILESTONE2_OVERRIDE=YES_I_UNDERSTAND` env set; script logs the override invocation to `results/milestone2_override_<date>.md` for audit |

**The override exists for the case where milestone 1 closes by a path the locked triggers don't anticipate** (e.g., voluntary strategy retirement after a different mechanism class becomes available externally). It MUST produce an audit artifact; the script enforces this by refusing the override unless `MILESTONE2_OVERRIDE_REASON` is also set with non-empty text.

## Locked execution sequence

Top-5 picks per the backlog: **A2 (ATR-targeted sizing), A1 (vol-regime filter), C1 (funding extremum reversal), B2 (BB squeeze release), D1 (session filter)**.

A2 is tested FIRST because it's structurally orthogonal — a position-sizing change that composes with any entry mechanism. If A2 ADOPT, every subsequent candidate (A1/C1/B2/D1) is evaluated on TOP of A2-sized baseline. If A2 REJECT, candidates run against constant-stake baseline (current live config).

Order is mechanical:

| Phase | Candidate | Inherits from | Verdict criterion source | Estimated runtime |
|:---:|---|---|---|---|
| 1 | A2 | constant-stake baseline | strategy_backlog §A2 | ~2h compute |
| 2 | A1 | A2 verdict (ADOPT or REJECT determines baseline) | strategy_backlog §A1 | ~2h compute |
| 3 | C1 | A2 verdict (independent strategy; A2 sizing applies if ADOPT) | strategy_backlog §C1 + Cat X replication | ~3h compute (Bybit replication adds time) |
| 4 | B2 | A2 verdict | strategy_backlog §B2 | ~2h compute |
| 5 | D1 | A2 verdict | strategy_backlog §D1 | ~1h compute |

**Total estimated runtime:** ~10h compute + ~5h verdict-writing = ~2 calendar days at full operator focus, or ~5 days at reasonable cadence.

**Mid-sequence pause condition:** If any phase produces a verdict that contradicts a Stage-0 (milestone 1) verdict — e.g., A1 vol-regime conditioning produces a Sharpe gain that should retroactively invalidate the milestone-1 assumption that "selection adds variance not edge" — STOP. Do not proceed to the next phase. Document the contradiction in `results/milestone2_contradiction_<date>.md` and convene a rule-update review BEFORE running phase N+1.

## Family-wise <abbr title="Alpha — the maximum chance you're willing to accept of 'seeing a real result when there isn't one.' Smaller is stricter. α=0.05 = 5% false-positive rate; α=0.01 = 1%.">α</abbr> budget

Per strategy_backlog §discipline contract:
> Family-wise α correction at execution: if N ideas are tested in a milestone, each individual α = α_family / N. With α_family = 0.05 and N = 5, individual α = 0.01.

**Per-criterion α application:**

| Criterion type | How α=0.01 enters |
|---|---|
| <abbr title="Return divided by volatility. Higher = more profit per unit of stomach-churn. 1.0 is OK, 2.0 is good, 3.0+ is exceptional.">Sharpe ratio</abbr> threshold | <abbr title="Pretend the data you have IS the population, resample it many times, see how much your number wiggles. The 2.5%/97.5% wiggle extremes are your confidence interval.">Bootstrap CI</abbr> at α=0.01 (1% / 99%) — must clear baseline by margin specified in backlog (e.g., A1 requires 1.2× baseline) |
| NET PnL > 0 | <abbr title="Split history into many train/test windows that march forward through time. Mean across windows = expected performance; spread = regime risk.">Walk-forward</abbr> 6-window mean must clear $0 with bootstrap 99% CI excluding zero |
| Cat X replication (C1 only) | Bybit-replicated NET sign must match Binance NET sign on ≥4 of 6 windows |
| Concentration (≤ 40%) | Computed on cumulative empirical distribution; no statistical test (it's a structural gate) |

**Why <abbr title="When testing many ideas at once, divide your significance threshold by the count. Tests 5 things at α=0.05 → each must clear α=0.01.">Bonferroni</abbr> and not Holm/BH:** the candidates are pre-specified in advance, and any single rejection is a deploy-grade decision (we're going to allocate real money differently). The conservatism of Bonferroni matches the asymmetric loss function — false positive (deploy a bad change) is much costlier than false negative (skip a real edge that re-surfaces in milestone 3).

## Compute environment

**Required:**
- Cleanly compiled `cmd/backtest` binary at the milestone-2 trigger commit hash
- All 16 deployed-symbol historical CSVs in `data/` for at least 2020-01 → 2025-04
- All 16 funding-CSV files in `data/funding/` (post-loader-fix; verify with `LastTS` check ≥ 2025-04)
- For C1 only: Bybit historical CSVs in `data/bybit/` per Cat X replication template

**Logged outputs per phase:**
- Raw backtest stdout: `results/m2_phase{N}_{candidate}_raw_<date>.txt`
- Verdict artifact: `results/m2_phase{N}_{candidate}_verdict_<date>.md`
- Bootstrap CI artifact (where applicable): `results/m2_phase{N}_{candidate}_ci_<date>.json`

## Per-phase verdict template

Each phase MUST produce a verdict artifact with these sections:

1. **Phase header** — candidate name, locked criterion source, baseline used (A2-sized vs constant-stake), trigger commit hash
2. **Mechanical verdict** — ADOPT / REJECT / HOLD / CONTRADICTION (the four-state output per backlog)
3. **Numerical evidence** — primary metric (Sharpe / NET / etc) with bootstrap 99% CI; secondary metrics; concentration; cost stack
4. **Pre-registration compliance** — copy-paste of the exact criterion from the backlog with a checkbox per sub-criterion
5. **Rollback assessment** — if ADOPT, what changes vs current live config? if REJECT, are there sub-results worth carrying to milestone 3?
6. **Operator sign-off** — explicit acknowledgement that the verdict was applied mechanically per the locked criterion, with no <abbr title="Looking at the data, then picking the threshold/strategy that 'works.' Generates spurious results, even with honest intent. Pre-registration is the antidote.">post-hoc</abbr> threshold adjustment

## Rollback path

Two distinct rollback scenarios:

### Scenario A: Mid-sequence kill (any phase)

A phase produces verdict CONTRADICTION (verdict directly contradicts a milestone-1 lock).

1. STOP the launch script (it does this automatically — the script's exit code 5 on CONTRADICTION halts the loop).
2. Write `results/milestone2_contradiction_<date>.md` documenting which milestone-1 verdict is contradicted and how.
3. Convene the rule-update review: re-read the contradicted milestone-1 verdict in light of the new evidence. Either (a) update the milestone-1 verdict (rare — requires affirmative justification why the new finding overrides the prior n=5y conclusion) or (b) update the milestone-2 candidate's interpretation (more common — usually the contradiction is a regime-conditional finding that doesn't invalidate the milestone-1 conclusion).
4. Document the resolution. Re-launch the script from phase N+1 with `MILESTONE2_RESUME=phaseN+1`.

### Scenario B: All-phases-complete with ADOPT verdicts inconsistent with deployed strategy

The cleanest exit. All five phases run; verdicts are mechanical; some ADOPTs propose changes to the deployed config.

1. Each ADOPT proposal goes through `stage_promotion_runbook_decision_rule_2026-05-08.md`'s six-phase execution sequence, parameterized as a config update at the current STAGE_N.
2. ADOPTs are applied ONE AT A TIME — never bundle multiple milestone-2 changes into a single config update. The next change waits for ≥30 days of post-rollout monitoring + <abbr title="Statistical test that compares live performance against backtest expectation, Bonferroni-corrected at α_family=0.001. Single firing = investigation; two firings 7+ days apart = auto-kill candidate. The decision-grade kill mechanism.">drift detector</abbr> clean.
3. The order of post-milestone-2 rollouts mirrors the test order (A2 first if ADOPTed; then A1; etc.).

## Audit-lens compliance for the launch script

Per `docs/AUDIT_LENS.md` lens-as-design-tool, `scripts/milestone2_launch.sh` MUST ship with:

- Distinct exit codes per failure shape: 0 ALL_VERDICTS_WRITTEN, 1 PHASE_FAILED_VERDICT_INCONCLUSIVE, 2 TRIGGER_NOT_DETECTED, 3 ENV_OR_INPUT_ERROR, 4 PHASE_FAILED_RUNTIME_ERROR, 5 CONTRADICTION_HALTED
- No `|| true` swallowing exit codes anywhere in the orchestration loop
- Trigger detection that fails NOISILY when neither STAGE_1 promotion nor kill artifact is present (not "(no trigger yet)" silent default)
- Per-phase regression test in `scripts/test_milestone2_launch.sh` covering: missing data, missing binary, partial-completion resume, override-without-reason, contradiction-mid-sequence
- Telegram CRITICAL alert on exit codes 1, 4, 5; INFO alert on 0; WARN on 2 (operator misconfiguration)

## Cross-references

- `results/strategy_backlog_milestone2_2026-05-08.md` — the candidate catalog this runbook executes
- `results/real_money_protocol_decision_rule_2026-05-08.md` — the trigger conditions
- `results/stage_promotion_runbook_decision_rule_2026-05-08.md` — the post-verdict rollout playbook
- `docs/AUDIT_LENS.md` — the design discipline the launch script must follow
- `scripts/realistic_sweep.sh` / `scripts/p4_oos_persistence.sh` — the runner-script patterns this harness inherits

## Migration triggers

This rule is locked. It updates only on:

1. **Backlog update.** If `strategy_backlog_milestone2_2026-05-08.md` is updated to include a 9th idea or remove one of the top-5, this rule updates the execution sequence + α budget mechanically.
2. **Trigger semantics change.** If `real_money_protocol_decision_rule_2026-05-08.md`'s milestone-2 trigger conditions change (e.g., to add a third trigger type), this rule updates the trigger-detection table.
3. **Compute environment failure mode.** If a backtest run reveals a missing-data or missing-binary failure mode the launch script doesn't handle gracefully, this rule's "audit-lens compliance" section grows the regression-test list.

NO OTHER UPDATES are allowed without an explicit migration document in `results/migration_<date>.md`.
