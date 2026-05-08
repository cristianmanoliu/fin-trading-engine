# STAGE-promotion execution runbook — locked decision rule (2026-05-08)

**Status:** LOCKED 2026-05-08. Activates at each STAGE-up promotion event. The completion review (`forward_paper_completion_review_decision_rule_2026-05-08.md`) gates the FIRST promotion (paper → STAGE_1); this runbook locks the EXECUTION sequence for every promotion (paper → STAGE_1, STAGE_1 → STAGE_2, STAGE_2 → STAGE_3, STAGE_3 → STAGE_4).

## Question

Per `real_money_protocol_decision_rule_2026-05-08.md` the four-stage protocol parameterizes:
- STAGE_1: $100/trade after forward-paper deploy criteria + 30d clean drift detector
- STAGE_2: $300/trade after ≥50 STAGE_1 trades + 30d + slip stable
- STAGE_3: $500/trade after ≥100 cumulative + 60d at STAGE_2 + slip/drift stable
- STAGE_4: $1000/trade after ≥200 cumulative + 90d at STAGE_3

Each promotion is a high-stakes moment: the trading engine must (1) verify the gate, (2) update config, (3) deploy, (4) monitor for execution-quality regression, (5) rollback if needed. Without a locked runbook each promotion is improvised — exactly when stakes are 3× higher than the prior stage and exactly when haste produces the wrong outcome.

The locked rule below makes each promotion mechanical: same procedure each time, parameterized only by stage.

## Pre-promotion gate (universal across stages)

Before ANY promotion's execution sequence begins, the corresponding GATE must be GREEN:

| Promotion | Gate document |
|---|---|
| paper → STAGE_1 | `results/forward_paper_completion_review_decision_rule_2026-05-08.md` (full A/B/C audit) |
| STAGE_N → STAGE_N+1 (N=1,2,3) | this rule's "Stage-N completion review" — defined below |

The gate produces an artifact (`results/<gate>_<date>.md`) that the runbook references in Phase 4 documentation. **No promotion proceeds without an explicit GREEN gate verdict.**

## Per-stage parameters

The runbook is the same six phases (HALT-OPTIONAL → CONFIG → DEPLOY → VERIFY → MONITOR → DOCUMENT) regardless of stage. The parameters differ:

| Parameter | STAGE_1 | STAGE_2 | STAGE_3 | STAGE_4 |
|---|:---:|:---:|:---:|:---:|
| Per-trade stake | $100 | $300 | $500 | $1000 |
| Pre-promotion required cumulative trades | ≥150 (forward-paper gate) | ≥50 at STAGE_1 | ≥100 cumulative since STAGE_1 | ≥200 cumulative |
| Pre-promotion required calendar days at prior stage | 60d forward-paper net-positive | 30d STAGE_1 | 60d STAGE_2 | 90d STAGE_3 |
| Pre-promotion completion review | full A/B/C (forward_paper_completion_review) | abbreviated (slip + drift stable) | abbreviated + cost-stack | abbreviated + cost-stack + concentration |
| Engine HALT before config change | NOT REQUIRED (engine state preserved) | NOT REQUIRED | NOT REQUIRED | RECOMMENDED |
| Post-promotion verification window | 24h intensive | 24h intensive | 48h intensive | 72h intensive |
| Post-promotion drift wrapper cadence | weekly (per locked rule) | weekly | every 5 days for 30d, then weekly | every 3-4 days for 30d, then weekly |
| First-N-trade monitoring | first 3 trades manually verified | first 5 | first 5 | first 10 |
| Telegram alert on first-trade fill anomaly | ENABLED | ENABLED | ENABLED | ENABLED |
| Rollback path | revert config to paper Stub | downgrade stake to STAGE_N-1 | same | same |
| Cooling period after AMBER on prior stage's review | 30d before re-attempt | 30d | 60d | 60d |
| Daily-loss circuit breaker (Gate B from executor architecture) | M=10 ($1000) | M=12 ($3600) | M=12 ($6000) | M=15 ($15000) |

The stage-N completion review (for promotions OTHER than paper → STAGE_1) is abbreviated:

### STAGE_1 → STAGE_2 abbreviated review

Required GREEN on:
- Realized fee bps cumulative at STAGE_1 ≤ 11 bp (Section A1 from completion review)
- Realized slip bps on losers cumulative at STAGE_1 ≤ 15 bp (A2)
- Drift detector wrapper history at STAGE_1: zero firings OR firings investigated and dismissed per playbook
- Empirical NET PnL at STAGE_1 ≥ 0 (positive realized $)
- Per-symbol concentration ≤ 30% at STAGE_1
- No HARD per-symbol pauses during STAGE_1
- ≥50 STAGE_1 trades, ≥30 calendar days at STAGE_1

### STAGE_2 → STAGE_3 abbreviated review

All STAGE_1→STAGE_2 checks PLUS:
- Cost-stack divergence: realized fee/slip within ±15% of modeled (A5/A6 GREEN threshold)
- Trade distribution: ≥2 distinct calendar quarters represented in STAGE_1+STAGE_2 cumulative trades
- ≥100 cumulative trades since STAGE_1, ≥60 calendar days at STAGE_2

### STAGE_3 → STAGE_4 abbreviated review

All STAGE_2→STAGE_3 checks PLUS:
- Single-symbol concentration over the last 90d ≤ 25% (stricter than STAGE_2)
- BTC-HODL Δ over the STAGE_3 window > 0
- ≥200 cumulative trades, ≥90 calendar days at STAGE_3
- Independent operator review (Section C7 from full completion review)

## Locked execution sequence

Six phases; same shape across all promotions, parameterized per the table above.

### Phase 1: GATE VERIFICATION (15-30 min)

```bash
# Verify the gate document exists and reports GREEN.
GATE_DOC="results/forward_paper_completion_review_<date>.md"  # or stage-N review
# Confirm composite verdict line says ALL_GREEN
grep "Composite verdict:" "$GATE_DOC"
# Expected: contains "ALL_GREEN"

# Confirm no HARD KILL artifact has been created since the gate verdict
ls -la results/kill_*.md 2>/dev/null
# Expected: empty or all timestamps before the gate
```

**Verification gate:** the `Composite verdict: ALL_GREEN` line MUST be present AND no kill artifacts MUST exist newer than the gate doc. If either fails: ABORT promotion. Do not proceed to Phase 2.

### Phase 2: CONFIG PREPARATION (30-45 min)

Compose the config change locally (do NOT deploy yet):

```bash
# For paper → STAGE_1: switch executor to binance_live
# For STAGE_N → STAGE_N+1: increase stake_usd in the config

cd /opt/trading-engine  # or local repo path
# Update configs/<symbol>.yaml or the systemd ExecStart parameters
# Specifically: stake_usd parameter changes per the table above

# For paper → STAGE_1 ONLY: also change executor type:
#   executor: binance_live  (was: stub)
#   binance_api_key: from /etc/paper-live/env (already populated)
#   binance_api_secret: from /etc/paper-live/env

# Generate diff for explicit review
git diff configs/ deploy/
```

**Verification gate:** operator MANUALLY reviews the diff line-by-line. Required:
- ONLY the stake_usd change (and executor change at STAGE_1)
- NO other parameter changes
- NO accidentally-modified comments or whitespace
- Diff is clean

If any unexpected change appears: revert and re-do the config preparation.

### Phase 3: DEPLOY (15-30 min)

**STAGE_4 ONLY: HALT engines first** (recommended for the largest size step):

```bash
# STAGE_4 only — graceful halt to ensure clean transition
ssh root@178.105.24.230 'systemctl stop paper-live@*.service'
ssh root@178.105.24.230 'systemctl list-units "paper-live@*.service" --state=active --no-legend | wc -l'
# Expected: 0
```

**All stages: deploy via existing redeploy mechanism**:

```bash
./deploy/redeploy.sh all
```

**Verification gate (immediate):** post-deploy check must pass strictly:

```bash
STRICT=1 ./scripts/post_deploy_check.sh
# Expected: exit code 0 (HEALTHY)
```

If `post_deploy_check` reports ANY warning at STRICT=1: ROLLBACK (Phase 6) immediately. Do not proceed.

### Phase 4: FIRST-TRADE VERIFICATION (until first N trades close)

The most-likely-place for executor bugs to surface is the FIRST trade at the new stage. Manual verification is mandatory.

For each of the first N trades (per the table; N=3 for STAGE_1, N=10 for STAGE_4):

```bash
# When a trade opens, watch the engine log
ssh root@178.105.24.230 "tail -f /var/log/paper-live/<symbol>.log"

# Verify in the log:
# - "signal generated" line with expected entry price
# - "order submitted" line (BinanceLive only) with matching params
# - "fill received" line within 5s of submission
# - Fill price within 10 bps of signal entry price
# - Journal "open" event with all expected fields
```

```bash
# When the trade closes, verify:
# - Fill price within 10 bps of stop or target as appropriate
# - Journal "close" event has fee_usd, slip_usd, notional_usd populated
# - Realized fee_bps in the close event = (fee_usd / notional_usd) * 10000
#   should be within ±20% of the modeled 10 bp
# - Outcome (TARGET / STOP / TIME) matches the log narrative
```

**Verification gate per trade:**
- All log lines present in expected order
- Fill price sanity (within 10 bps of expected)
- Journal cost fields populated and within ±20% of modeled
- Outcome matches narrative

If ANY verification fails on any of the first N trades: HALT (Phase 5 SOFT-pause) and investigate. Re-promote only after root cause identified and fixed.

### Phase 5: MONITORING WINDOW (24h / 24h / 48h / 72h per stage)

For the duration specified per the table, the operator runs intensified monitoring:

```bash
# Daily during the window:
./scripts/post_deploy_check.sh
./scripts/forward_paper_status.sh
./scripts/run_drift_check.sh  # may run more often than weekly during window
```

Required checks each day:
- All engines active (post_deploy_check)
- No new ERROR-level events
- No unexpected position drift (PositionReconciler stability — STAGE_1+ only)
- forward_paper_status: no kill criterion firing
- Drift wrapper: no exit-1 firing yet (or if fired, investigation in progress per playbook)

If ANY check fails during the monitoring window: SOFT pause + root-cause investigation. Resume at the same stage only after fix.

### Phase 6: DOCUMENT (30-45 min)

**Required artifact:** `results/stage_promotion_<YYYY-MM-DD>_<from>_to_<to>.md`

```markdown
# STAGE promotion — <YYYY-MM-DD> — <from> → <to>

**From:** paper | STAGE_1 | STAGE_2 | STAGE_3
**To:** STAGE_1 | STAGE_2 | STAGE_3 | STAGE_4
**New stake_usd:** $<value>
**Operator(s):** <name(s)>

## Pre-promotion gate
- Gate document: <link>
- Verdict: ALL_GREEN
- Verified at: <timestamp>

## Phase-by-phase log

### Phase 1: gate verification
- Gate doc: <link>
- Verified clean: yes/no
- Notes: <text>

### Phase 2: config preparation
- Diff reviewed: yes/no
- Diff link: <commit hash or inline>

### Phase 3: deploy
- Deploy timestamp: <UTC>
- post_deploy_check verdict: HEALTHY/WARN
- Notes: <text>

### Phase 4: first-N-trade verification
For each of first N trades:
- Trade <i>: <symbol> <side> entry=<price> filled=<price> outcome=<TARGET/STOP/TIME>
  realized fee_bps=<value> slip_bps=<value>
  verification: PASS / FAIL / N/A (still open)

### Phase 5: monitoring window
- Window start: <date>
- Window end: <date>
- Daily check anomalies: <count + brief description>
- Resolution: <text>

## Outcome

- Promotion successful: yes / partial (rollback) / failed
- Active stage at end of window: <stage>
- Cumulative trades at end of window: <n>
- Cumulative NET at end of window: $<value>
- Realized fee_bps over window: <value>
- Realized slip_bps over window: <value>

## Cross-references
- forward_paper_status.sh output before promotion: <linked>
- post_deploy_check output post-deploy: <linked>
- First-N-trade journal entries: <linked>
- Telegram alerts during window: <count>
```

**CLAUDE.md updates:**
- "Strategy status" line: update to reflect new stage and stake.
- "Today's findings" entry: brief note + link to artifact.
- (STAGE_4 only) noteworthy milestone — full progression complete.

## Rollback path

If Phase 4 or Phase 5 verification fails, the rollback path:

```bash
# 1. Revert the config change
git revert <Phase 2 commit>  # or manual edit per Phase 2 reverse

# 2. Redeploy
./deploy/redeploy.sh all

# 3. Verify
STRICT=1 ./scripts/post_deploy_check.sh

# 4. Document the rollback in the promotion artifact (Phase 6 section)
# 5. SOFT pause flag (operational trigger #3 in per_symbol_pause rule, applied fleet-wide):
#    cooling period before re-attempt is the cooling-period from the per-stage table
```

The rollback returns engines to the PRIOR STAGE (or paper if STAGE_1 fails). It does NOT proceed to milestone-2 (HARD KILL is a different decision per auto-kill rule).

## Edge cases — pre-locked

### Promotion criteria fire mid-monitoring-window of the previous promotion

E.g., STAGE_1 promotion is in its 24h monitoring window; while waiting, STAGE_2 trade-count criterion fires. Locked rule: complete the prior promotion's monitoring window FIRST. STAGE_2 promotion does not proceed until STAGE_1 monitoring window expires GREEN.

### Drift wrapper fires exit-1 during monitoring window

Per the firing investigation playbook, exit-1 = TRIAGE-A or B. The promotion is paused for the duration of the investigation (which is at most a few hours for TRIAGE-A; longer for B). If investigation concludes "noise / dismissed," promotion's monitoring window resumes from the pause point. If investigation escalates to TRIAGE-C or auto-kill: ROLLBACK.

### First-N-trade verification: trade is still open at end of monitoring window

The verification requires N CLOSED trades. If trades aren't closing fast enough (4H signal-tf is slow), the monitoring window can extend to accommodate. Locked extension: up to 2× the per-stage window (so STAGE_1's 24h could extend to 48h max).

### Promotion fires at a market event boundary (e.g., week-of-massive-news)

Locked rule: do NOT promote during a window of unusually-large macro events (CPI, FOMC, listed-token announcement). The operator's judgment applies — defer the promotion by 24-48h to a calmer window. Document the deferral in the artifact.

### Operator is unavailable for the full monitoring window

Locked rule: promotion requires operator ATTENTION during the monitoring window (running daily checks). If the operator can't commit, defer. Half-attention monitoring is worse than full-attention deferral.

### STAGE_4 is reached: what happens at the top?

STAGE_4 is the TERMINAL stage in the protocol. No STAGE_5 exists; the promotion playbook does not extend further. Once at STAGE_4 stable, the strategy operates indefinitely (subject to the kill criteria). Future milestones might define a STAGE_5 (different mechanism, different size step) — that's a milestone-N concern.

### Multiple STAGE-up criteria fire same-day

Possible at STAGE_3: cumulative-trade criterion and time-at-stage criterion both fire on the same day. Single promotion event covers both. The runbook executes once; the artifact references both criteria.

## Alternatives considered (rejected)

### Single full-A/B/C completion review at every stage

**Considered:** apply the rigorous A/B/C audit at every promotion (not just paper → STAGE_1).

**Rejected because:** Sections A and B require ≥150 trades / 60+ days for the bootstrap-CI shape check. STAGE_2 requires only 50 STAGE_1 trades / 30 days — too few for the full audit's statistical power. Abbreviated reviews (this rule's section above) are calibrated to what the data supports at each stage.

### Skip rollback path; treat each promotion as committed

**Considered:** simplify by removing the rollback option.

**Rejected because:** the verification phases exist precisely to catch problems. If we catch a problem, we MUST be able to back out — otherwise the verification is performative. Rollback is a critical safety net.

### Combine Phase 4 + Phase 5 (first-N-trade + monitoring)

**Considered:** simpler operational sequence.

**Rejected because:** they have different acceptance criteria. Phase 4 verifies INDIVIDUAL trade execution quality (per-trade granularity). Phase 5 verifies AGGREGATE behavior over time. Rolling them together would obscure single-trade pathology with aggregate noise.

### No documentation requirement for STAGE_2-4 (only STAGE_1)

**Considered:** STAGE_1 is the big deal; subsequent promotions are routine.

**Rejected because:** STAGE_3-4 carry the largest dollar exposure. The audit trail at the high-stakes end is exactly where retrospective analysis is most valuable. Every promotion produces an artifact.

## Migration triggers

The rule re-opens for design when:

1. **First actual STAGE-promotion** — review whether parameters held up. If verification took materially longer than budgeted, recalibrate windows.
2. **First rollback occurs** — review whether the rollback path was clean. If recovery took >2 hours or required manual intervention, document and tighten.
3. **STAGE_5 introduced** (future milestone) — extend the table.
4. **Multi-strategy fleet** — promotion is per-strategy; rule needs cross-strategy coordination if STAGE thresholds differ between strategies.
5. **Real-money executor architecture activates** — Phase 4 verification details should be tested against the actual `BinanceLive` implementation; if the implementation differs from the locked design, the runbook's verification commands need updating.

## Cross-references

- `results/real_money_protocol_decision_rule_2026-05-08.md` — defines the STAGE criteria
- `results/forward_paper_completion_review_decision_rule_2026-05-08.md` — gate for paper → STAGE_1
- `results/real_money_executor_architecture_decision_rule_2026-05-08.md` — defines BinanceLive that activates at STAGE_1
- `results/auto_kill_execution_decision_rule_2026-05-08.md` — fallback if monitoring window catches a fatal issue
- `results/per_symbol_pause_decision_rule_2026-05-08.md` — rollback uses operational SOFT pause semantics
- `results/drift_firing_investigation_decision_rule_2026-05-08.md` — referenced if drift fires during monitoring window
- `scripts/post_deploy_check.sh` — Phase 3 verification gate
- `scripts/forward_paper_status.sh` — Phase 5 daily check
- `scripts/run_drift_check.sh` — Phase 5 cadence-controlled check
- `deploy/redeploy.sh` — Phase 3 deployment mechanism
- `CLAUDE.md ## Strategy status` — updated by Phase 6
