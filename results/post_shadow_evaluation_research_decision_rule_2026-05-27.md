# Post-Shadow-Evaluation Research Sweep — PRE-REGISTERED Decision Rule

**Status:** LOCKED 2026-05-27, BEFORE any shadow has been evaluated against `shadow_promotion_decision_rule_2026-05-27.md`.

**Author note:** written with cold head while live = -$19,406 and current shadows are mid-flight. Locks the shape of future research IF current shadows fail promotion. Prevents the failure mode "shadows look bad → invent more sweeps to escape conclusion."

## Why pre-register

The temptation 12 days from now will be: "current shadows didn't pan out, let's just deploy more variants and see what sticks." This is the garden-of-forking-paths trap. Three failure modes pre-empted:

1. **Inventing new sweeps in response to disappointing data** — discipline failure.
2. **Lowering the deploy-bar for new shadows because the prior bar's outcomes were underwhelming** — bar-creep.
3. **Treating "more shadows" as the answer when the answer is "n needs more time"** — over-action in the face of variance.

## Activation conditions (ALL must hold to invoke this rule)

1. At least one shadow has reached n ≥ 63 closed trades
2. `shadow_promotion_decision_rule_2026-05-27.md` has been applied per its terms
3. NO shadow passed all 9 promotion gates (Gate 9 = anti-cluster, added 2026-05-27 in commit `4393e86`)
4. Live strategy is NOT in auto-kill state (`auto_kill_execution_decision_rule` did NOT fire)
5. Live is also NOT yet promoted to STAGE_1 (still STAGE_0 paper)

If any of (1)-(5) is false, this rule does NOT activate. Specifically:
- Auto-kill takes precedence (live broken → fix or kill, not search for alternatives)
- A passing shadow → execute the promotion rule, this rule sleeps
- STAGE_1 active → real-money protocol governs, not research expansion
- All shadows still n < 63 → wait, do nothing

## What this rule does

Specifies the SHAPE of the next research tier: which sweeps run, in what order, with what selection criteria. Activates as cascaded TIERS. Each tier runs ONLY if the previous tier produced zero deploy-worthy results.

## The cascade (LOCKED)

### TIER 1 — Cat-B exit mechanism stress (compute ~3h)

**Hypothesis:** different exit mechanism (trail-stop, MLTP) might reduce variance enough that shadow performance becomes statistically distinguishable from live even without higher mean NET.

**4 cells (LOCKED, no expansion):**

| # | Variant | Engine params | Description |
|---|---------|---------------|-------------|
| 1 | trail-interval-2R | `--trailing-stop-mode --trail-interval-r 2.0` | Lock (N-1)R after every +NR favorable, step=2R |
| 2 | trail-interval-3R | `--trailing-stop-mode --trail-interval-r 3.0` | Same, step=3R |
| 3 | MLTP mid-3R-half | `--multi-level-tp-mode --mid-r 3.0 --mid-frac 0.5` | Close 50% at +3R, raise stop to BE on remainder, hold remainder to 6R |
| 4 | MLTP mid-4R-half | `--multi-level-tp-mode --mid-r 4.0 --mid-frac 0.5` | Same, partial at +4R |

**Note on engine capability (audited 2026-05-28):** the engine's trailing-stop has no separate "engagement threshold" parameter — engagement is implicit at +1R (first ratchet step). The original "trail-stop 3R/4R = engage at +N R" semantics in this rule's lock draft were not implementable. Cells 1-2 use the actual `--trail-interval-r` semantics. MLTP supports single mid-level only (`MidR` + `MidFrac`, fires at most once per position). The original "33%-at-2R + 33%-at-4R" two-tier scale-out is not implementable without engine changes. Cells 3-4 use single-mid variants.

**Prior research note:** 2026-05-19 sweeps (`trailing_stop_sweep_2026-05-19.csv` + `mltp_exit_sweep_2026-05-19.csv`) already produced raw NET numbers for these exact engine cells at fee=10 + slip=5. Re-running is still required because gates C (stress fee=15 + slip=15) and D (anti-cluster) were NOT applied in 2026-05-19. Use prior CSVs as the cached `mean_NET_raw` per cell; new sweep work is the stress + cluster runs only.

All run with LIVE entry config (EMA 9/21 mh504 short 4H), realistic costs (fee=10 + slip=5). Walk-forward 3 windows (W1/W2/W3 per harness, via `scripts/trailing_stop_sweep.sh` + `scripts/mltp_exit_sweep.sh` which wrap `walk_forward.sh`). Universe = 57 historical symbols (NOT deployed-16; avoids look-ahead).

**Deploy-as-shadow criteria (ALL must hold):**

A. Variant POSITIVE in ≥2/3 windows AND mean NET > 0
B. Variant mean NET ≥ 0.8 × LIVE mean NET (small concession for variance trade-off)
C. **Stress test:** rerun at fee=15bp + slip=15bp. Variant remains POSITIVE in ≥2/3 windows. This bakes in the today-lesson that cost-stack honesty matters.
D. **Anti-cluster test:** remove the 2 best calendar days from each window's trades; variant mean NET still > 0 in ≥2/3 windows. Today's lesson: 2-day cluster drove apparent alt5-15-* outperformance. This guards against deploying variants whose backtest edge is similarly concentrated.
E. Variant is DIFFERENT enough from current shadows that adding it informs new ground (operational test: cross-correlation of per-window NET vs each existing shadow < 0.7)

If any cell passes A+B+C+D+E → deploy AS SHADOW under `shadow_promotion_decision_rule` (will require n=63 forward-paper before live consideration). Maximum 1 new shadow per execution of this tier.

### TIER 2 — Symbol-universe robustness (compute ~2h)

**Activates only if TIER 1 yields zero deploy-worthy cells.**

**Hypothesis:** maybe the LIVE strategy edge exists on a subset of the deployed-16 universe but the noisy symbols are masking it. NOT a hypothesis to change live universe (deployed-16 is locked) but to inform shadow design.

**3 cells (LOCKED):**

| # | Variant | Description |
|---|---------|-------------|
| 1 | LIVE config, top-8 contributing symbols only | Subset deployed-16 to top-8 by backtest contribution |
| 2 | LIVE config, bottom-8 only (placebo) | Same but bottom-8 contributors. Should be NEGATIVE — pre-registered placebo. |
| 3 | LIVE config, equal-weight 16 | Normalize stake per symbol-contribution rather than fixed $1k |

**Deploy-as-shadow criteria:** same A-E as Tier 1.

If cell 1 dramatically outperforms cell 3 by symbol selection alone (>2x mean NET), that's evidence the deployment universe could be optimized. Action: pre-register a separate decision rule for universe re-selection (NOT covered here — universe changes are a separate locked-rule class).

### TIER 3 — Combined-strategy ensemble (compute ~4h)

**Activates only if TIERS 1 AND 2 yield zero deploy-worthy cells.**

**Hypothesis:** individually-weak strategies may compose into a lower-variance ensemble (modern portfolio theory applied to strategies).

**3 cells (LOCKED):**

| # | Variant | Description |
|---|---------|-------------|
| 1 | LIVE + alt5-15-504 50/50 stake split | Both fire independently, stake halved |
| 2 | LIVE + bb20 50/50 (negative-correlation diversifier) | bb20 underperforms; if r<0 with LIVE, halving stakes increases Sharpe |
| 3 | Weighted ensemble by backtest Sharpe | Allocate stake proportional to per-strategy backtest Sharpe |

**Deploy-as-shadow criteria:**

A. Ensemble POSITIVE in ≥2/3 windows
B. Ensemble Sharpe (mean / std of per-window NET) > 1.2 × LIVE Sharpe
C. Stress test (fee=15bp + slip=15bp) holds
D. Anti-cluster test holds
E. Implementation simplicity check: can be expressed as **post-hoc journal arithmetic** on already-running shadow journals (multiply each per-trade PnL by stake-weight, sum). The `--shadow` flag runs each variant at full stake on identical ticks; stake-split semantics are applied at evaluation time, not in the engine. If a cell requires actual engine-level position sizing changes → defer to milestone 2

**Audit note 2026-05-28:** `--shadow` flag spec is `label:ema_fast-ema_slow-max_hold` only — it cannot encode stake fractions or per-strategy weighting. This is fine: ensemble PnL is computed at evaluation time as `Σ weight_i × journal_PnL_i`, no engine change needed.

## Cost budget (LOCKED)

- Wall-clock per tier: ≤ 4h (matches existing sweep infrastructure throughput)
- Maximum new shadows added across all tiers: **1**
- Maximum REST poll load increase: 120/min (1 new symbol shadow)
- Compute: existing `walk_forward.sh` harness wrapped by `mltp_exit_sweep.sh` + `trailing_stop_sweep.sh` (TIER 1) + symbol-list overrides via `SYMBOLS=` env (TIER 2 + 3); no new tooling. `realistic_sweep.sh` is **5y continuous, NOT walk-forward** — do not substitute
- Manual analysis time per tier: ≤ 2h. If exceeded, halt the tier and document why.

## Anti-discovery commitments (LOCKED)

These are pre-committed to before any sweep runs:

1. **One tier at a time.** Do not run TIER 2 before TIER 1 is fully evaluated and rejected. Do not run TIER 3 before TIER 2.
2. **No follow-on sweeps within a tier.** Each tier's cell list is FIXED at lock time. "Just one more variant to test" = NO.
3. **No criteria modification.** If TIER 1 yields a cell that passes A-D but fails E (similarity test) → reject. Don't loosen E to admit it.
4. **No metric substitution.** PnL-mean-NET is the primary. Sharpe, hit-rate, max-drawdown are SECONDARY. A cell that wins on Sharpe but loses on NET is REJECTED.
5. **If all 3 tiers produce zero deploy-worthy results:** the search is OVER for this milestone. Document the exhaustion as a verdict. Next action: wait for more live data, or wait for Milestone 2 scope.
6. **No re-running tiers within 90 days.** If TIER 1 rejects everything, you cannot re-run TIER 1 with "slightly different cells" until at least 90 days have elapsed and new live data informs the design.

## Anti-cluster gate detail (lesson from 2026-05-27)

Today's investigation revealed that alt5-15-336 shadow's apparent +$15,579 cumulative PnL collapsed to -$31,754 when 2 best calendar days (2026-05-16 + 2026-05-17, 8 trades, +$47,333) were excluded. The "edge" was a 48h market-wide event.

To prevent this from contaminating shadow selection:

For each candidate variant, compute:
- `mean_NET_raw` = standard per-window mean NET
- `mean_NET_robust` = mean NET after removing the 2 best calendar days from EACH window's trade set

The variant must satisfy:
- `mean_NET_raw > 0` in ≥2/3 windows
- `mean_NET_robust > 0` in ≥2/3 windows
- `mean_NET_robust > 0.4 × mean_NET_raw` (the "edge" is at least 40% non-cluster)

A variant whose backtest "edge" disappears when 2 best days are removed is REJECTED. This is the explicit codification of today's lesson.

## Required artifacts per tier

When a tier runs, the operator must produce:

1. `results/post_shadow_tier_<N>_sweep_<YYYY-MM-DD>.md` — sweep pre-reg (cells locked, harness invocation locked) BEFORE running
2. `results/post_shadow_tier_<N>_verdict_<YYYY-MM-DD>.md` — sweep results + criteria evaluation per cell + accept/reject verdict
3. `results/post_shadow_tier_<N>_deploy_<YYYY-MM-DD>.md` — IF a cell deploys, the new shadow's parameter spec + activation date + activation drift_check_history snapshot

## Cross-references

- `shadow_promotion_decision_rule_2026-05-27.md` — gate-evaluation rule that, when failing, triggers THIS rule
- `auto_kill_execution_decision_rule_2026-05-08.md` — supersedes this rule if live is killed
- `real_money_protocol_decision_rule_2026-05-08.md` — supersedes if STAGE_1 active
- `cat_a_decision_rule_2026-05-06.md` — original Cat-A research (entry mechanism); already REJECTED
- `cat_b_decision_rule_2026-05-07.md` — original Cat-B research (exits); informs TIER 1
- `ema_tf_exploratory_grid_2026-05-19.md` — confirms EMA-period is noise dimension; this rule deliberately does NOT re-sweep EMA pairs
- `research_synthesis_2026-05-19.md` — establishes "LIVE config survives 31+ perturbations"; this rule respects that finding by searching ORTHOGONAL dimensions (exits, universe, ensemble) NOT MORE EMA PAIRS

## Decision log

| Date | Event | Action |
|------|-------|--------|
| 2026-05-27 | Rule locked | (this doc) |
| 2026-05-28 | Audit + patches (mirror of auto-kill audit). Fixed: TIER 1 cells aligned to engine capability (trail-engage-threshold + 2-tier MLTP don't exist); cost-budget script ref corrected (`walk_forward.sh` not `realistic_sweep.sh`); activation-condition gate count 8→9; TIER 3 criterion E clarified as post-hoc journal arithmetic. Cells now executable end-to-end | Commit TBD |
| TBD (≥ 2026-06-08) | First shadow reaches n=63, promotion rule applied | If REJECTED: this rule may activate |
| TBD | TIER 1 sweep runs (if activated) | Pre-reg, run, verdict, accept/reject |
| TBD | TIER 2 sweep runs (only if TIER 1 yields zero) | Same |
| TBD | TIER 3 sweep runs (only if TIER 2 yields zero) | Same |
| TBD | Search exhausted (all 3 tiers reject) | Document; wait for milestone 2 |

## Predictions at lock time (calibration check for later)

These priors are pre-committed:

- P(this rule activates by 2026-09-01): ~60%
- P(TIER 1 produces 1 deploy-worthy cell | rule activates): ~25%
- P(TIER 2 produces 1 deploy-worthy cell | TIER 1 rejected): ~15%
- P(TIER 3 produces 1 deploy-worthy cell | TIER 2 rejected): ~20%
- P(all 3 tiers exhaust | rule activates): ~50%
- P(any TIER-deployed shadow eventually passes shadow_promotion gates): ~30%

If deviations from these priors are substantial, that's data for next-milestone meta-design (the search-process is also subject to selection effects we should track).
