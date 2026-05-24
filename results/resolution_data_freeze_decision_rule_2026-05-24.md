# Resolution data-freeze — pre-registered decision rule

**Status:** LOCKED 2026-05-24, BEFORE any forward-paper resolution event has occurred.
**Scope:** the snapshot-and-bundle procedure that executes the moment forward-paper resolves (KILL or PROMOTE), defining what data is captured, what timestamp closes the dataset, and what artifact bundle locks the resolution state for all downstream artifacts.

---

## Freeze trigger (when this fires)

Exactly one of:
- `forward_paper_resolution.py` exits 1 PROMOTE on a weekly_audit Sunday run
- `forward_paper_resolution.py` exits 4 KILL on any run (including mid-week manual invocation)
- Operator manually invokes the freeze procedure after an OPERATOR_REVIEW verdict (exit 3) that the operator has resolved in writing to PROMOTE or KILL

Does NOT fire on: CONTINUE (0), WATCH (2), or unresolved OPERATOR_REVIEW.

### Gate-ordering rule

If `n ≥ 150` trades hit before `day ≥ 60` (or vice versa), the clock does NOT reset. The remaining gate evaluates at its natural completion; freeze fires only when BOTH gates are satisfied.

**Hysteresis guard:** if `n_current` momentarily dips below 150 between weekly runs (e.g. position-recovery edge case where a recovered trade re-opens), the `n` gate is considered held. The gate is `n_closed_ever ≥ 150 within the 60-day window`, not `n_current ≥ 150`. Calendar days are monotonic — no analogous guard needed.

---

## Freeze moment (t_freeze definition)

`t_freeze` = the UTC timestamp recorded in the `forward_paper_resolution.py` snapshot output at the moment the resolution verdict was emitted (`dt.datetime.utcnow()` at script execution).

For operator-manual invocations: `date -u +%Y-%m-%dT%H:%M:%SZ` at the moment the operator triggers the freeze.

`t_freeze` is LOCKED as the directory name `results/freeze/<YYYY-MM-DDTHHMMSSZ>/` and is referenced by every downstream artifact (cohort_outcome_join verdict, promote_closure, postmortem). It cannot be retroactively adjusted.

---

## Canonical snapshot bundle (what gets frozen)

All 12 components are captured into `results/freeze/<t_freeze>/` at the moment of freeze. Each is a **fresh run at `t_freeze`**, NOT a copy of a prior weekly cron snapshot (rationale: cron snapshots may be up to 7 days stale; the canonical bundle must reflect the exact resolution moment).

| File | Source | Notes |
|---|---|---|
| `forward_paper_status.txt` | fresh `scripts/forward_paper_status.sh` | per-cohort dashboard at freeze |
| `forward_paper_resolution.txt` | the resolution verdict output itself | includes exit code, verdict reason, all gate values |
| `realized_cost_trajectory.txt` | fresh `scripts/realized_cost_trajectory.py` | fee/slip trend through freeze |
| `lag_summary.txt` | fresh `scripts/lag_summary.sh` | fleet-wide lag at freeze |
| `drift_check.txt` | fresh `scripts/run_drift_check.sh` | see drift-history-as-of-freeze §below |
| `signal_context_inspect.txt` | fresh `scripts/signal_context_inspect.py` | cohort/symbol record counts at freeze |
| `signal_journal_reconcile.txt` | fresh `scripts/signal_journal_reconcile.py` | per-cohort gap state at freeze |
| `journal_cache/` | full rsync of VPS `/var/log/paper-live/journal/` | all cohorts: live + shadow subdirs |
| `signal_context_cache/` | full rsync of VPS signal-context dir | all cohorts |
| `git_head.txt` | `git rev-parse HEAD && git status` | code state at freeze |
| `vps_inventory.txt` | `systemctl list-units paper-live@* --state=active` | which engines were alive |
| `freeze_manifest.json` | auto-generated | all-files index with sizes + sha256 checksums |

### Drift-history-as-of-freeze

`forward_paper_resolution.py` reads the tail of `drift_check_history.jsonl` — at the resolution moment that tail entry may be the prior Sunday's run (up to 7 days stale). The canonical bundle requires a **fresh drift detector run at `t_freeze`** (recorded in `drift_check.txt`) to capture the actual drift state at resolution.

If the fresh drift check exits 2 INSUFFICIENT (n still below n_min=30): recorded in manifest as `drift_state: INSUFFICIENT`, freeze proceeds. The drift state at freeze is INFORMATIONAL — it does NOT re-veto a PROMOTE or KILL verdict already issued by `forward_paper_resolution.py`.

If the fresh drift check exits 4 AUTO-KILL: this fires a Telegram CRITICAL regardless of the resolution verdict and requires explicit operator acknowledgment before the promote-closure artifact is written.

---

## Recovered / in-flight position handling

At `t_freeze`, positions still OPEN in the journal are treated as follows:

- **Reported separately** in `forward_paper_status.txt` under an `open_positions` column. NOT mixed into closed-trade aggregates.
- **Excluded from pro-rate numerator**: in-flight unrealized PnL DOES NOT count toward `live_pnl_usd` in any promotion gate comparison. Only closed trades count. Rationale: unrealized PnL is mark-to-market noise; promotion criteria must reflect realized edge.
- **`elapsed` definition**: `floor((t_freeze − t0) / 86400)` integer days. `t0 = 2026-05-05T20:06:00Z` (per CLAUDE.md: forward-paper start). Floor avoids inflating the denominator on a partial-day freeze. This is the canonical `elapsed` value referenced by `honest_annual_prorate_decision_rule` (to be written separately).

---

## Snapshot selection for backward-references

Any rule that references "the resolution snapshot" means EXACTLY `results/freeze/<t_freeze>/`. Specifically:

- `cohort_outcome_join_decision_rule_2026-05-24.md` §5 "matched calendar weeks": weeks are bounded by `[t0, t_freeze]`.
- `promote_closure_template_decision_rule_2026-05-10.md` §2 "required links": the `forward_paper_status.sh snapshot at trigger moment` = `results/freeze/<t_freeze>/forward_paper_status.txt`.
- `postmortem_template_decision_rule_2026-05-08.md` "forward_paper_status output at trigger time" = same canonical bundle path.

Prior `results/forward_paper_snapshots/*.txt` files are REFERENCE-ONLY (historical monitoring trail). They MUST NOT be used as the verdict-input source for any post-resolution analysis. The lex-greatest snapshot pattern in `forward_paper_resolution.py:117` continues to drive the live verdict; the freeze copies that snapshot's content + a fresh re-run into the canonical bundle.

---

## Exit codes (for `scripts/resolution_freeze.sh`)

| Code | Meaning |
|---|---|
| 0 | CLEAN — all 12 bundle components written, manifest checksums verified |
| 1 | PARTIAL — bundle written but ≥1 component fresh-run failed; manifest records FAIL per component |
| 2 | PRE_RESOLUTION — invoked before PROMOTE (1) or KILL (4) verdict; freeze not authorized |
| 3 | INPUT_ERROR — VPS unreachable, rsync failed, or local disk insufficient for journal_cache mirror |
| 4 | ALREADY_FROZEN — `results/freeze/` already contains a `<t>` directory; freeze is one-shot; requires operator-touched sentinel file to override |
| 5 | MANIFEST_FAIL — bundle written but checksum verification failed; partial-copy corruption suspected |

Exit code 0 with missing bundle components is FORBIDDEN.

---

## Audit-lens compliance (per `docs/AUDIT_LENS.md`)

Applied before code is written:

1. **Missing-input → silent-success**: every fresh-run component failure → manifest FAIL marker (NOT silent omission). Exit 1 PARTIAL is distinct from 0 CLEAN. Pre-resolution invocation → exit 2 (NOT 0). Already-frozen → exit 4 (NOT 0).
2. **Writer-equals-model**: ACTIVELY DISCLAIMED. The freeze computes NO verdict — it ONLY captures outputs from external writers into a canonical bundle. All verdict logic stays in `forward_paper_resolution.py` (already pre-registered and pattern-locked).
3. **Sibling-bug propagation**: the freeze script MUST call existing scripts (`signal_context_fetch.sh`, `journal_fetch.sh`, `signal_context_inspect.py`, `signal_journal_reconcile.py`, `forward_paper_status.sh`, `lag_summary.sh`, `run_drift_check.sh`, `realized_cost_trajectory.py`) and NOT reimplement their data-gathering. Schema constants derived from the same grep-based sources as the existing tools.
4. **Telegram-tier dual-sense**: freeze completion → CRITICAL (KILL path) or INFO (PROMOTE path). Any component failure → CRITICAL regardless of path. The CRITICAL/INFO distinction is on the resolution event, not on the freeze health — freeze health always fires CRITICAL on failure.
5. **Operator-action-path**: ON one (feeds every downstream closure artifact). Every input-failure shape maps to a distinct exit code; manifest is checksummed to prevent silent partial-copy corruption.

---

## When this becomes decision-relevant

Fires AT the moment of resolution. Until then, no execution — the rule is locked NOW so the resolution moment does not become a design exercise under maximum stress.

---

## Files this rule will govern

- `scripts/resolution_freeze.sh` — to be written when resolution is imminent (NOT now during MONITORING)
- `results/freeze/<t_freeze>/` — canonical bundle directory (gitignored)
- Referenced by: `cohort_outcome_join_decision_rule_2026-05-24.md`, `promote_closure_template_decision_rule_2026-05-10.md`, `postmortem_template_decision_rule_2026-05-08.md`
