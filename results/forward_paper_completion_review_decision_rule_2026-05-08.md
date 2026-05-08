# Forward-paper completion review — locked decision rule (2026-05-08)

**Status:** LOCKED 2026-05-08. Activates when `scripts/forward_paper_status.sh` first reports overall verdict `DEPLOY-READY`. Sits between mechanical-criteria-pass and STAGE_1 promotion as the qualitative audit gate.

## Question

The forward-paper status script gives a mechanical `WAITING / DEPLOY-READY / KILL` verdict from a fixed set of thresholds (trade count ≥150, days ≥60, WR ≥14%, single-sym ≤40%, fee ≤12bp, slip ≤25bp on losers, BTC-HODL Δ ≥ 0). When it says `DEPLOY-READY`, the obvious next step is STAGE_1 promotion.

But "deploy-ready by the locked thresholds" is NECESSARY, not sufficient. The thresholds are a low-pass filter on the raw data; they don't tell you:
- Are the realized cost numbers BELIEVABLE relative to the modeled values?
- Does the empirical PnL trajectory match the walk-forward CI's prediction shape, or did we get a lucky path within a still-broken distribution?
- Are there anomalies (single-day outliers, symbol-specific patterns, regime correlation) that warrant a pause despite headline pass?
- Is the n>=150 sample distributed across enough independent calendar weeks, or concentrated in a single regime?

Without a locked review, the operator either RUSHES past the gate (treats green-light as conclusive) or STALLS (uncertainty-paralyzed about whether to trust the headline). Both are worse than a mechanical review checklist.

## Position in the operational chain

```
forward_paper_status.sh DEPLOY-READY
            │
            ▼
   THIS REVIEW (qualitative audit)
            │
            ├─ ALL_GREEN ────► STAGE_1 promotion (per real-money protocol)
            │
            ├─ AMBER ────────► resolve specific concerns, re-run review
            │
            └─ RED ──────────► HARD KILL (per auto-kill execution rule); milestone close
```

## Locked review checklist — three sections

The review is in three sections, each with locked verdict criteria. ALL three must produce GREEN before the gate opens.

### Section A — Cost realization audit

Goal: verify the realized fees + slippage are within the cost-stack ENVELOPE the strategy was validated against.

| # | Check | GREEN threshold | AMBER threshold | RED threshold |
|---|---|---|---|---|
| A1 | Realized round-trip fee bps (cumulative) | ≤ 11 bp | 11-12 bp | > 12 bp |
| A2 | Realized stop-side slip bps on losers (cumulative) | ≤ 15 bp | 15-25 bp | > 25 bp |
| A3 | Realized fee bps trend over last 30 days vs prior period | flat or improving | rising < 1 bp/30d | rising ≥ 1 bp/30d |
| A4 | Realized slip bps trend over last 30 days vs prior period | flat or improving | rising < 2 bp/30d | rising ≥ 2 bp/30d |
| A5 | Modeled-vs-realized fee divergence | actual within ±10% of modeled | within ±20% | > ±20% |
| A6 | Modeled-vs-realized slip divergence (losers only) | actual within ±30% of modeled | within ±50% | > ±50% |

**Rationale:**
- A1/A2 are STRICTER than the deploy-gate thresholds (12bp / 25bp) because the deploy gate is a PASS/FAIL filter; the review wants ROBUSTNESS room. Slipping past the deploy gate at 11.9bp realized fee is technically pass but leaves zero margin for STAGE_2/3+ when notional grows.
- A3/A4 catch SLOW DEGRADATION patterns. If fees/slip are creeping up, the trajectory matters as much as the cumulative.
- A5/A6 catch MODEL DIVERGENCE. The locked kill criterion is anchored on the 10bp fee + 5bp slip MODEL; if reality has diverged materially from the model, the kill criterion may be miscalibrated.

**Verdict for Section A:** worst-of all checks. If any check is RED, the section is RED. AMBER allowed in ≤2 of A1-A6 with explicit reason; ≥3 AMBER is RED.

### Section B — Empirical-vs-prediction shape audit

Goal: verify the empirical PnL trajectory is within the WALK-FORWARD CI's predicted distribution, not a lucky path within a broken distribution.

The walk-forward CI from milestone 1 (`results/bootstrap_ci_verdict_2026-05-07.md`): mean +$130k/yr with 95% CI [−$111k, +$372k] (walk-forward, regime-variance dominant). Trade-level block bootstrap CI was tighter [+$42k, +$220k] but the verdict says "anchor expectations to walk-forward, not bootstrap — bootstrap underestimates per-quarter regime swings."

Pro-rated to 60 days at $130k/yr mean: ~$21.4k. At ≥150 trades and 60+ days, the empirical realized NET should:

| # | Check | GREEN | AMBER | RED |
|---|---|---|---|---|
| B1 | Empirical realized NET PnL (pro-rated to elapsed days at mean) | within ±50% of pro-rated mean | within ±80% | outside ±80% |
| B2 | Empirical realized NET sign vs walk-forward CI sign | matches expected sign | n/a | inverted (loss when positive expected) |
| B3 | Empirical NET within 95% walk-forward CI envelope (pro-rated) | within | n/a | outside |
| B4 | Per-quarter calendar distribution of trades | ≥3 distinct calendar quarters with ≥30 trades each | ≥2 quarters | trades concentrated in 1 quarter |
| B5 | Win-rate empirical vs backtest-predicted (20.6%) | within ±3pp | within ±5pp | > ±5pp divergence |
| B6 | MFE/MAE distribution shape vs backtest reference | KS test p > 0.05 | p ∈ [0.01, 0.05] | p < 0.01 |

**Rationale:**
- B1 catches "lucky-path" — if forward NET is at the 90th percentile of walk-forward CI, that's good but suspect; revert toward mean is plausible.
- B2 is a hard sanity check. If realized NET is NEGATIVE while the walk-forward expected POSITIVE, the strategy is broken and the review fails immediately.
- B3 is the formal CI containment test.
- B4 catches concentration: 150 trades in a single regime is NOT 150 independent observations. The walk-forward CI was derived across regimes; matching that requires regime spread.
- B5 catches structural shifts. WR was central to the strategy's edge derivation; large divergence implies the underlying mechanism has shifted.
- B6 uses MFE/MAE distribution shape (already computable via the existing drift detector code) to detect shape-of-distribution shifts beyond just mean.

**Verdict for Section B:** RED if any of B2/B3 is RED. Otherwise, worst-of all checks following the section A rule.

### Section C — Anomaly + qualitative audit

Goal: catch the things checklists miss — the "this looks weird" gut-check, made mechanical.

| # | Check | GREEN | AMBER | RED |
|---|---|---|---|---|
| C1 | Single-symbol PnL contribution | ≤ 30% | 30-40% | > 40% |
| C2 | Single-day PnL contribution | ≤ 15% | 15-25% | > 25% |
| C3 | Drift detector last 30-day history | clean (no firings) | 1 firing investigated and dismissed | 1 firing pending OR ≥2 firings |
| C4 | post_deploy_check anomaly count over the last 60 days | ≤ 2 (any tier) | 3-5 | > 5 |
| C5 | per_symbol_pause artifacts created during forward-paper | none | 1-2 (all SOFT) | ≥3 OR any HARD |
| C6 | Funding cost realization vs $0 prior | within ±$10k cumulative across fleet | within ±$30k | > ±$30k |
| C7 | Independent operator review | another reviewer confirms data inspection | reviewed by self only | not reviewed |
| C8 | git log of pkg/ + cmd/ during forward-paper | only documented bug fixes | code changes with documented test verification | undocumented behavior changes |

**Rationale:**
- C1/C2 catch concentration risk distinct from the cumulative-PnL filter; even if the cumulative looks healthy, single-source dominance is a concern.
- C3 verifies the drift detector itself ran cleanly (or any firings were properly dismissed) over the relevant window.
- C4/C5 quantify the operational-noise floor. A few SOFT pauses for legitimate causes are normal; many indicates the fleet was struggling.
- C6 catches funding cost reality vs the "net ≈ $0 in steady state" assumption; significant funding-cost realization suggests the regime was different from backtest.
- C7 is the social check. Solo-operator review can rationalize; an independent reviewer (even a junior one) catches fresh-eye issues.
- C8 catches mid-milestone code changes. Pure bug fixes with tests are FINE (today's pass is allowed, for example); strategy/behavior changes during the milestone contaminate the data.

**Verdict for Section C:** worst-of all checks.

## Composite verdict

| Section A | Section B | Section C | Composite |
|:---:|:---:|:---:|---|
| GREEN | GREEN | GREEN | **ALL_GREEN** — proceed to STAGE_1 |
| GREEN | GREEN | AMBER | AMBER — resolve C-checks then re-run |
| GREEN | AMBER | GREEN | AMBER — investigate B-checks then re-run |
| AMBER | GREEN | GREEN | AMBER — re-evaluate A-checks at +30 days for trend |
| Any AMBER | Any AMBER | Any AMBER | AMBER — multiple-section investigation |
| Any RED | * | * | **RED** — HARD KILL, milestone close |
| * | Any RED | * | **RED** — HARD KILL |
| * | * | C2 ≤ AMBER, others GREEN | continues per row above |
| * | * | C2 RED (single-day >25%) | **RED** — HARD KILL (lucky-day artifact) |

**The single-RED rule:** any RED in any section produces composite RED. The review is a CONJUNCTIVE filter — it should be hard to pass.

## AMBER resolution path

When the composite is AMBER, the operator must:

1. Identify which specific check(s) flagged.
2. Investigate the underlying cause (cross-checking against `drift_firings/`, `per_symbol_pause/`, journal data, recent git log).
3. Decide one of:
   - **CHECK_PASSES_ON_REVIEW** — the threshold flagged but the underlying cause is benign and documented; advance to GREEN with explicit operator note.
   - **CHECK_REQUIRES_TIME** — the cause is operational + recoverable; defer review by N days (where N is the time required for the cause to clear), then re-run.
   - **CHECK_REQUIRES_FIX** — code/operational fix needed; deploy fix, wait for fix to be reflected in fresh data (≥30 days), re-run.

Each AMBER resolution is documented in the review artifact (see below).

## Required artifact

**File:** `results/forward_paper_completion_review_<YYYY-MM-DD>.md`

Each completion review (initial AND any re-runs after AMBER resolution) produces a new artifact with the date in the filename. The historical sequence IS the audit trail.

**Required fields:**

```markdown
# Forward-paper completion review — <YYYY-MM-DD>

**Trigger:** forward_paper_status.sh reported DEPLOY-READY at <timestamp>
**Reviewer(s):** <operator name(s)>
**Composite verdict:** ALL_GREEN | AMBER | RED

## Section A: Cost realization audit
| Check | Value | Threshold | Verdict |
|---|---|---|---|
| A1 ... | ... | ... | GREEN/AMBER/RED |
[etc.]

## Section B: Empirical-vs-prediction shape
[same table format]

## Section C: Anomaly + qualitative
[same table format]

## AMBER resolutions (if any)
- C-N: <description of issue>; resolution: CHECK_PASSES_ON_REVIEW | REQUIRES_TIME (deferred to <date>) | REQUIRES_FIX (fix: <description>); operator note: <text>

## Composite verdict and decision
[ALL_GREEN → STAGE_1 promotion / AMBER → re-run on <date> / RED → HARD KILL]

## Cross-references
- forward_paper_status.sh output at trigger time: <inline or linked>
- Related drift_firings/ artifacts: <list>
- Related per_symbol_pause/ artifacts: <list>
- git log of behavior-changing commits in pkg/ + cmd/ since first close: <list>
```

## Edge cases — pre-locked

### Review fires the SAME day forward_paper_status first reports DEPLOY-READY

This is the expected normal case. The locked rule is: do not promote to STAGE_1 within 24h of first DEPLOY-READY verdict. The 24h cooling period prevents rushing on a same-day verdict that might flip to AMBER on a single-day data update.

### Review composite goes back and forth between AMBER and GREEN

If two consecutive reviews (≥30 days apart) flip GREEN→AMBER→GREEN, treat the second GREEN with skepticism. The flipping suggests the data is at the edge of multiple thresholds. The locked rule: require THREE CONSECUTIVE green reviews ≥30 days apart before STAGE_1 promotion if any prior review was AMBER on Section A or B.

### Review composite is GREEN but the OPERATOR has a gut concern

The locked rule honors gut concerns within bounded form: operator may flag a "qualitative concern" in the artifact under Section C C7 (Independent operator review). Concrete concerns are documented and addressed; vague concerns become the basis for adding new mechanical checks to FUTURE reviews (so future reviews are more rigorous than this one).

### Multiple reviewers disagree

If the second reviewer (C7) disagrees with the first reviewer's verdict, escalate to AMBER pending resolution. The disagreement itself is data; document the specific point of disagreement and resolution.

### forward_paper_status.sh logic changes during forward-paper

A pure bug-fix in the script (e.g., today's commit `383f98d` adding open-position visibility — additive, doesn't change verdict logic) is FINE per Section C C8. A change to the THRESHOLDS or VERDICT LOGIC is NOT — it would invalidate prior verdicts and require restarting the review's "first DEPLOY-READY" trigger from the change point. Strict version-pinning of the script during forward-paper is implicit in the milestone discipline.

### Walk-forward CI assumption invalidated mid-milestone

If new analytical work (e.g., a Cat F2 mechanism analysis) materially changes the walk-forward CI mean or shape during forward-paper, Section B's pro-rated mean target updates. Document the update with a reference to the new analysis, and treat any prior review verdicts as invalid (must re-run with new B targets).

### STAGE_1 promotion blocked by the kill mechanism mid-review

If a drift firing or threshold criterion fires DURING the review process (e.g., review starts at GREEN, takes 4 days, on day 3 drift wrapper exit 1 fires), the review is paused. The drift firing investigation playbook activates. If the firing escalates to auto-kill, this review is terminated as RED. If the firing is dismissed via the playbook, this review resumes from where it paused.

## Alternatives considered (rejected)

### Use only forward_paper_status.sh's mechanical verdict

**Considered:** the script's threshold gate is sufficient; no need for a separate review.

**Rejected because:** the script applies a low-pass filter; the qualitative concerns (single-day outliers, regime concentration, model-vs-realized divergence, gut-check) are NOT in the threshold logic. Mechanical thresholds are necessary but not sufficient for a real-money decision.

### Single-pass review (no AMBER, just GREEN/RED)

**Considered:** simplification — either everything is fine or the milestone dies.

**Rejected because:** AMBER is real. Most "almost-ready" cases are fixable with bounded operator effort (a fix, a wait period, a documented dismissal). Forcing them into binary GREEN/RED would mean either rushing AMBER cases as GREEN (loses rigor) or treating them as RED (kills milestones unnecessarily).

### Defer to operator gestalt review

**Considered:** the operator inspects the data and decides intuitively.

**Rejected because: ** intuition under stress is unreliable. The whole point of this rule is to mechanize the review so a tired operator at the deploy moment can't rationalize past concerns. Locked thresholds + locked artifact format ensures every review produces the same set of facts.

### Tighter thresholds (e.g., GREEN at ≤8 bp fee instead of ≤11 bp)

**Considered:** be more conservative.

**Rejected because:** thresholds tighter than the deploy gate's 12 bp would make the review almost always AMBER, defeating its purpose. The chosen GREEN thresholds (~10% margin from deploy gate) leave room without being so tight that real data fails.

## Migration triggers

The rule re-opens for design when:

1. **First completion review actually executes** — review whether the locked thresholds held up. If GREEN/AMBER/RED distribution was lopsided (e.g., everything came out AMBER), recalibrate thresholds.
2. **STAGE_1 → STAGE_2 promotion** — the equivalent review for higher-stage transitions may have stricter thresholds (smaller margin from gate; more required quarters in B4; etc.).
3. **Walk-forward CI updated** — Section B targets must be re-derived.
4. **forward_paper_status.sh logic changes** — affected sections re-derive.
5. **Multi-strategy fleet** — review is per-strategy; this rule's structure may need clarification on cross-strategy dependencies.

## Cross-references

- `scripts/forward_paper_status.sh` — the mechanical verdict source
- `results/bootstrap_ci_verdict_2026-05-07.md` — walk-forward CI used in Section B
- `results/real_money_protocol_decision_rule_2026-05-08.md` — STAGE_1 promotion is what this review gates
- `results/drift_firing_investigation_decision_rule_2026-05-08.md` — referenced by Section C C3
- `results/per_symbol_pause_decision_rule_2026-05-08.md` — referenced by Section C C5
- `results/auto_kill_execution_decision_rule_2026-05-08.md` — RED verdict triggers this rule
- `CLAUDE.md ## Forward-paper go/no-go criteria` — the operational doctrine this review extends
