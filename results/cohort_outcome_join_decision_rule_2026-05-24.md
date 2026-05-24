# Cohort-outcome join — pre-registered decision rule

**Status:** LOCKED 2026-05-24, BEFORE writing any consumer code and BEFORE any signal-context records are joined to journal close events.
**Scope:** post-resolution analysis that joins signal-context sidecar records to journal close events to produce per-cohort outcome distributions and a 4-verdict comparison tree.

---

## What is being pre-registered

The methodology for comparing 4 cohorts (live + 3 shadows) on closed-trade outcomes using the signal-context stream (`results/signal_context_cache/`) and journal close events (`results/journal_cache/`).

**Cohort definitions** — same 4 cohorts as `signal_context_consumer_decision_rule_2026-05-24.md`:
- `live` — deployed-16 engines, 4H EMA9/21 short-only 6:1 RR mh504
- `alt5-15-336` — shadow, EMA5/15 mh336
- `alt5-15-504` — shadow, EMA5/15 mh504
- `bb20` — shadow, Bollinger mode

**Join key:**
1. Signal-context record `(symbol, ts)` → journal OPEN event `(symbol, ts)` within ±60s tolerance (signals are emitted on candle close; journal writes are asynchronous — 60s covers normal IO jitter without crossing candle boundaries at 4H resolution).
2. Matched OPEN → journal CLOSE event via `(symbol, side, open_ts)` exact match.
3. Unmatched signal-context records (no journal open within ±60s) → counted in gap tally, excluded from outcome distribution.

**Per-cohort outcome metrics computed:**
- `n_closed` — closed-trade count
- `WR` — win rate (positive PnL closes / n_closed)
- `mean_pnl_per_trade` — mean PnL in USD per closed trade (**PRIMARY comparison metric**)
- `mean_mfe_r`, `mean_mae_r` — mean favorable and adverse excursion in R units
- `total_net_usd` — reported for transparency; NOT a verdict input (unequal n across cohorts)
- `sharpe_annualized` — annualized Sharpe ratio on per-trade PnL series (**SECONDARY metric**)
- `max_symbol_concentration_pct` — single-symbol share of absolute total NET (**SECONDARY metric**)

**Population minimums:**
- Per-cohort: n_closed ≥ 50; cohorts below this are EXCLUDED from the comparison (exit code 1 INSUFFICIENT).
- Per-symbol/cohort cell: n_closed ≥ 5; cells below this are omitted from per-symbol breakdown; cohort aggregate is still computed.

---

## Comparison verdicts (locked)

PRIMARY metric: `mean_pnl_per_trade`. Rationale: cohorts have unequal n (bb20 ~2× live emission rate under the position-already-open guard); total NET is not comparable across unequal n, while per-trade mean is well-defined and bootstrap-CI-compatible.

For each shadow cohort vs live:

| Verdict | Condition |
|---|---|
| **DOMINATE** | shadow `mean_pnl/trade` > live (98.3% bootstrap CI excludes 0) AND shadow Sharpe > live Sharpe AND shadow max-concentration ≤ 50% → shadow documented as **milestone-2 candidate** per `milestone2_runbook_decision_rule_2026-05-10.md`. NOT auto-promoted; must clear that doc's pre-registered launch criteria. |
| **MATCH** | shadow `mean_pnl/trade` within ±20% of live AND bootstrap CI includes 0 AND Sharpe within ±0.2 → no action; parameter perturbation added no edge |
| **UNDERPERFORM** | shadow `mean_pnl/trade` < live by > 20% AND 98.3% CI excludes 0 on the loss side → falsifies shadow's parameter choice; document in postmortem (KILL path) or promote-closure §5 (PROMOTE path) |
| **CONTRADICTION** | `mean_pnl/trade` higher but Sharpe lower (or vice versa), or CI includes 0 on the primary but secondary metrics diverge → OPERATOR_REVIEW; no mechanical verdict; written rationale required in closure artifact |

---

## Family-wise α correction

3 shadow × 1 primary comparison = 3 independent tests.
Bonferroni correction: α_family = 0.05, per-test α = 0.0167, two-sided 98.3% CI.

Statistical test: **paired-window bootstrap** (B = 10 000 resamples) on `(shadow − live)` per-trade-PnL deltas matched by signal-emit week.

Rationale for paired-by-week: cohorts trade overlapping calendar at different rates; weekly pairing controls for shared market-regime effects without requiring per-trade timestamp matching (infeasible — cohorts emit signals at different times). Assumes weekly windows are approximately independent over the 6-month forward-paper horizon.

Bonferroni chosen over Holm-Bonferroni for operational simplicity (N=3; power difference is negligible).

---

## Gap-impact handling

If `signal_journal_reconcile.py` reports gap_pct > 30% for a cohort (signals emitted but no matching journal open):

1. Cohort comparison runs but is FLAGGED in output (exit code 2 GAP_FLAGGED).
2. Operator MUST compute live-cohort outcome restricted to the SAME signal-emit calendar weeks as the flagged shadow (matched-window subset).
3. Re-run comparison on the matched-window subset.
4. If verdict flips between full-sample and matched-window → verdict is CONTRADICTION (gap biased the result), mark the cohort INCONCLUSIVE.
5. If verdict holds in both samples → FLAGGED verdict stands.

The 30% threshold is based on the `alt5-15-504` bb20 reconcile snapshot (54.5% gap is by design — position-already-open rejections; live gap was 6.5%). Shadow cohorts with structural high gap require this additional step.

---

## What this rule does NOT allow

Forbidden after forward-paper resolution (post-hoc analysis shapes that contaminate verdicts):

- **Per-symbol cherry-picking** — "BNB shadow beat live, deploy BNB-only shadow." Symbol-level verdicts are FORBIDDEN. Only cohort-aggregate verdicts feed into DOMINATE/MATCH/UNDERPERFORM/CONTRADICTION.
- **Time-window slicing post-hoc** — "shadow beat live in the last 30 days." Full-sample only. No sub-window comparisons unless triggered by the gap-impact §5 matched-window protocol.
- **Metric-of-the-day** — PRIMARY metric is `mean_pnl/trade`, locked. Running with `total_net` as the decision metric because it gives a different verdict is FORBIDDEN.
- **Adding metrics after observation** — the metric set is frozen at {mean_pnl/trade, Sharpe, max-concentration}. Adding drawdown, Calmar, or WR as a tiebreaker after seeing data is FORBIDDEN.
- **Running before resolution** — the comparison MUST NOT be run until the forward-paper KILL postmortem or PROMOTE closure artifact exists. Invoking the consumer before resolution → exit 5 PRE_RESOLUTION.

---

## Exit codes (for `scripts/cohort_outcome_join.py`)

| Code | Meaning |
|---|---|
| 0 | CLEAN — all cohorts met n≥50, full comparison rendered, verdicts mechanical |
| 1 | INSUFFICIENT — ≥1 cohort below n=50; partial comparison with EXCLUDED markers |
| 2 | GAP_FLAGGED — comparison rendered but ≥1 cohort gap_pct > 30%; matched-window re-run required per §5 |
| 3 | INPUT_ERROR — signal-context cache or journal cache missing or empty |
| 4 | SCHEMA_WARN — required field (symbol, ts, pnl_usd, outcome) missing in ≥1 record |
| 5 | PRE_RESOLUTION — invoked before forward-paper resolution artifact exists (FORBIDDEN by rule) |

Exit code 0 with missing caches is FORBIDDEN (missing-input → silent-success fail-open).

---

## Audit-lens compliance (per `docs/AUDIT_LENS.md`)

Applied before code is written:

1. **Missing-input → silent-success**: empty caches → exit 3; pre-resolution invocation → exit 5. Neither maps to 0 CLEAN.
2. **Writer-equals-model**: ACTIVELY DISCLAIMED. Verdict thresholds (±20%, ±0.2 Sharpe, 50% concentration, 30% gap, n=50 floor) are LOCKED in this document BEFORE any data is observed. The consumer reads two external writers (signal-context emitter + journal executor) and applies locked thresholds — it cannot tautologically produce MATCH by construction.
3. **Sibling-bug propagation**: `load_cohort` and `load_journal_closes` MUST be imported from `signal_context_inspect.py` and `signal_journal_reconcile.py`, not reimplemented. Schema constants derived by grep from `pkg/strategy/signal_context.go` + `pkg/execution/stub.go`, pinned by `journal_schema_test.go:TestJournalEntry_FieldSetIsPinned`.
4. **Telegram-tier dual-sense**: N/A — one-shot at-resolution analysis, not wired to cron.
5. **Operator-action-path**: ON one. Verdict feeds into milestone-2 candidate selection AND into promote_closure/postmortem §5 (counterfactual audit). Every input-failure shape maps to a distinct exit code (3/4/5) that prevents false CLEAN.

---

## When this becomes decision-relevant

Strictly AFTER forward-paper resolution: either the KILL postmortem per `postmortem_template_decision_rule_2026-05-08.md` OR the PROMOTE closure per `promote_closure_template_decision_rule_2026-05-10.md` must exist first.

Consumer script `scripts/cohort_outcome_join.py` is to be written at resolution, NOT during MONITORING. This rule governs that script.

---

## Files this rule will govern

- `scripts/cohort_outcome_join.py` — to be written post-resolution
- `results/cohort_outcome_join_verdict_<date>.md` — snapshot written at resolution
- Reads from: `results/signal_context_cache/` + `results/journal_cache/` (both gitignored, both populated by existing fetch scripts)
