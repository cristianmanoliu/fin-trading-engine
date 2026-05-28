# Shadow Promotion Decision Rule — PRE-REGISTERED

**Status:** LOCKED 2026-05-27, before any shadow has reached the n≥63 power floor.
**Author:** written with cold head — drafted while live = -$19,406 and shadow
alt5-15-* shows +$15-20k same window. Mechanism diagnosis already in hand
(`docs/findings/2026-05-27-pm.md`). Locking the rule NOW prevents motivated
reasoning at evaluation time.

## Why pre-register

When n≥63 hits ~2026-06-08 the pressure to act on visible-but-not-proven
shadow outperformance will be high. Three temptations:

1. **Promote the shadow that looks best post-hoc** — survivorship bias on a
   surface-level winner that hasn't been pressure-tested.
2. **Lower the n threshold** — "n=50 is close enough."
3. **Add metrics to break ties** — proliferating criteria until a desired
   shadow passes.

Each is a path to swapping a tested-but-unlucky strategy for an
untested-and-lucky one. Pre-register today, apply mechanically later.

## Scope

This rule covers ONLY shadow → live promotion of variants that share the
SAME exit framework as the deployed live config (wick stop, fixed RR=6
target, max-hold cap, fee/slip/funding identical, side-filter SHORT,
signal-tf 4H, deployed-16 universe). I.e., the EMA-period and timeframe
shadows already deployed (alt5-15-336, alt5-15-504, alt5-21-504,
alt7-14-504, alt10-30-504, alt12-26-504, alt21-50-504) plus bb20.

This rule does NOT cover:
- Switching to a different entry mechanism (PDH/PDL, RSI, etc.) — that's
  Cat-A research-phase, separate rule (`cat_a_decision_rule_2026-05-06.md`)
- Adding new symbols
- Adjusting exit framework parameters
- Real-money STAGE_1+ decisions (separate rule:
  `real_money_protocol_decision_rule_2026-05-08.md`)

## What we're testing

Per shadow at evaluation time, we have:
- LIVE n trades, WR, realized PnL, mean MFE, mean MAE
- SHADOW same metrics (each cohort)
- A drift detector exit code (CLEAN / INVESTIGATION / AUTO-KILL)

The promotion question: **does any shadow's realized advantage clear the
statistical bar to justify swapping the live config?**

## Gate criteria (ALL must hold to promote a shadow to live)

A shadow is eligible for promotion ONLY when ALL 9 of:

1. **Shadow n ≥ 63 closed trades** (power floor; ±10pp CI on WR per
   binomial bounds — CLAUDE.md power floor)
2. **Live n ≥ 63 closed trades** (same data quantum for comparison)
3. **Live cumulative PnL trajectory is documented** (the live floor)
4. **Two-proportion z-test of WR (live vs shadow) p < 0.001**
   (Bonferroni-corrected for 7 shadow comparisons → α_family=0.007 / 7 = 0.001).
   Significance threshold borrowed from drift-detector calibration.
5. **Shadow cumulative PnL ≥ 1.5 × |live cumulative PnL|** OR
   **shadow cumulative PnL ≥ +$20k AND live cumulative PnL ≤ 0**
   (one of: large effect size, OR live is negative and shadow is meaningfully
   positive)
6. **Mechanism-confirmed**: shared-day shared-symbol comparison shows
   shadow advantage is NOT solely on shared days (would be impossible —
   strategy outputs identical on same-bar same-signal — but re-run as
   robustness check)
7. **Cost-stack honest**: realized fees ≤ 12bp, realized slip ≤ 20bp
   (same as forward-paper deploy gate)
8. **No shadow-vs-shadow ambiguity**: if two shadows both meet gates 1-7,
   pick the one with HIGHER cumulative PnL only when their PnL difference
   ≥ 1 standard error of the mean PnL delta (i.e., distinguishable signal,
   not noise between alternatives)
9. **Anti-cluster gate** (explicit codification of 2026-05-27 lesson —
   shadow alt5-15-* apparent +$15,579 collapsed to -$31,754 once 2 best
   calendar days were excluded). Compute over the shadow's full forward-paper
   trade set:
   - `cumulative_PnL_raw` = standard sum across all closed trades
   - `cumulative_PnL_robust` = sum after removing all trades that closed
     on the 2 calendar days (UTC) with highest per-day PnL contribution
   Both must hold:
   - `cumulative_PnL_robust > 0`
   - `cumulative_PnL_robust > 0.4 × cumulative_PnL_raw`
   A shadow whose apparent edge disappears when 2 best calendar days are
   removed is REJECTED. The 0.4 threshold mirrors the research-phase
   anti-cluster gate (`post_shadow_evaluation_research_decision_rule_2026-05-27.md`).
   This is STRICTER than the line-170 rejection bullet ("one trade >40%
   of PnL"), which only catches concentrated single trades; cluster
   patterns spread the same effect across N trades on 1-2 days and would
   pass the single-trade test.

If gates 1-8 are met but gate 8 reveals two equally-good shadows,
**defer** to the variant with LONGER backtest history support (i.e.,
the 5-year backtest already tested it more times). If tied there too,
**defer to next 30 trades**.

## Promotion mechanics (locked sequence)

If a shadow passes ALL 8 gates, the swap is:

### Phase A — pre-flight

```bash
# Verify all gates pass — write evaluation doc first
results/shadow_promotion_eval_<YYYY-MM-DD>.md

# Required fields: per-gate evaluation, p-value, effect size, mechanism
# Snapshot of all shadow journals at decision moment (immutable record)
```

### Phase B — atomic swap (NO partial state)

```bash
# 1. Run scripts/post_deploy_check.sh — must be all-green
# 2. Stop one symbol as canary
ssh root@178.105.24.230 'systemctl stop paper-live@<canary>.service'

# 3. Update deploy/systemd/paper-live@.service ExecStart — change live EMA
#    pair from 9-21 to <promoted> (e.g., 5-15). DO NOT remove shadow lines.
#    Old "live" config moves into --shadow list under label "alt9-21-504"
#    (track what was deprecated).

# 4. ./deploy/sync.sh && systemctl daemon-reload && start canary
# 5. Verify canary signals fire under new config (compare to expected
#    EMA cross from BT signal log on same window)

# 6. Roll fleet via ./deploy/redeploy.sh all
# 7. Run scripts/post_deploy_check.sh — must remain all-green

# 8. Run scripts/run_drift_check.sh under the NEW baseline (regenerate
#    BT baseline using promoted config). Forward-paper window n resets.
```

### Phase C — promotion record

Required artifact: `results/shadow_promotion_<YYYY-MM-DD>_<old>_to_<new>.md`

```markdown
# Shadow promotion — <YYYY-MM-DD>

**Old live config:** EMA <9/21>, mh<504>
**New live config:** EMA <X/Y>, mh<Z>
**Reason:** <which gates triggered, with p-values>

## Evidence

- Live n=<N>, WR=<W>%, PnL=<P>
- Promoted shadow n=<N>, WR=<W>%, PnL=<P>
- Two-proportion z-test: z=<Z>, p=<P>
- Effect size: <PnL ratio, MFE delta>
- Mechanism: <shared-day analysis result>

## Forward-paper reset

Forward-paper observation n RESETS for the new config. Per
`real_money_protocol_decision_rule_2026-05-08.md` the STAGE_1 ETA
recomputes from this date forward.

## Old config disposition

The deprecated EMA <9/21> moves to --shadow list as "alt9-21-504"
(symmetry — what was live becomes a shadow we now compare against).
```

### Phase D — post-promotion validation

After 14 calendar days at new config:
- Re-run shared-day mechanism test (was the advantage stable?)
- Run drift detector against NEW baseline (catch any same-period regime
  noise that masqueraded as edge)
- If new config performs ≤ deprecated old config over 14d, **revert**
  via Phase B in reverse.

## Locked rejection criteria (DO NOT promote)

A shadow with apparent edge but failing ANY gate above is NOT promoted.
Specifically:

- **If shadow n ≥ 63 but live n < 63** — wait for live to catch up
  (the comparison is not yet apples-apples)
- **If p < 0.001 but effect size < 1.5× |live PnL|** — significance
  without meaningful economic impact; the cost-and-risk of swap is not
  justified
- **If two shadows both pass and are statistically indistinguishable** —
  the choice is noise, swap creates churn without expected edge
- **If a single dramatic event drives the shadow advantage** (one trade
  >40% of shadow PnL) — that's variance, not signal

## Commitments NOT to do regardless of results

1. NOT promote a shadow before n_live ≥ 63 + n_shadow ≥ 63
2. NOT lower the p-value threshold below 0.001
3. NOT redefine "winner" beyond cumulative PnL after p-value gate clears
4. NOT promote multiple shadows simultaneously (one swap at a time;
   re-evaluate after 14d)
5. NOT promote a shadow without writing the evaluation doc + promotion
   record artifacts (Phase A + C)
6. NOT skip the 14-day post-promotion validation gate (Phase D)
7. NOT keep promoted shadow if its 14-day forward performance is worse
   than the deprecated config's last 14d

## Edge cases anticipated

**bb20 underperforming:** bb20 (Bollinger Bands 20/2.0) has WR 4.8% and
PnL -$30,633 over 19 days. It's clearly the worst-performing shadow.
**Demoting bb20 is NOT covered by this rule.** Removing a shadow is a
separate question (loses cross-comparison data; cheap to keep running).
If bb20 hits n≥63 and confirms persistent underperformance, the rule for
demotion is: **leave it running as a control**. We need the
underperforming reference point.

**Cat-A long-EMA shadows (10/30, 12/26, 21/50) still 0-2 trades:** these
will not be evaluable until at least n=20-30 trades each, which at observed
rates is months away. They are deliberately slow indicators; this rule
applies whenever ANY shadow first reaches n=63, then re-applies
independently for each subsequent shadow that reaches n=63.

**Live config itself fires drift auto-kill before any shadow promotes:**
auto-kill protocol takes precedence (the existing
`auto_kill_execution_decision_rule_2026-05-08.md` is the higher-priority
rule). In that case ALL shadows pause too — kill is total. After kill,
shadow data may still inform the next-generation strategy (separate
milestone).

## What this rule does NOT solve

- **Choosing initial live config** — already done at deploy time (EMA
  9/21 mh504 per 6-window walk-forward validation)
- **Adding new shadows** — not a promotion question
- **Real-money STAGE_x transitions** — that rule lives in
  `real_money_protocol_decision_rule_2026-05-08.md`
- **Regime detection / strategy ON/OFF gating** — out of scope

## Expected outcome distribution (predictions at lock time)

These are predictions I make NOW, before evaluation, to test my
calibration later:

- P(any shadow passes all 9 gates by 2026-08-12 STAGE_1 ETA): ~25% (lock-time prior was "8 gates"; Gate 9 added 2026-05-27 in commit `4393e86` — prior not re-calibrated, treat as stale)
- P(specifically alt5-15-* passes all gates): ~20% (highest-prior given
  current trajectory)
- P(bb20 ever passes): ~2%
- P(slow-EMA shadows (10/30, 21/50, 12/26) reach n=63 by 2026-09-30): ~30%
- P(live 9/21 keeps running unchanged until 2026-08-12): ~50%
- P(live config gets auto-killed before any promotion): ~25%

If actual outcomes deviate dramatically from these priors, the deviation
is data for next milestone (selection-process calibration).

## Cross-references

- `auto_kill_execution_decision_rule_2026-05-08.md` — what to do if
  drift fires (higher priority than promotion)
- `real_money_protocol_decision_rule_2026-05-08.md` — STAGE_1 gates
  (shadow promotion does NOT trigger STAGE_1 advance; STAGE_0
  forward-paper window restarts post-promotion)
- `cat_a_decision_rule_2026-05-06.md` — original Cat-A research-phase
  decision rule (different scope: entry mechanism replacement, not
  parameter variant)
- `docs/findings/2026-05-27-pm.md` — investigation that prompted this
  pre-registration

## Decision log

| Date | Event | Action |
|------|-------|--------|
| 2026-05-27 | Rule locked | (this doc) |
| 2026-05-27 | Amendment: explicit Gate 9 (anti-cluster) added pre-eval | Closes parity gap with `post_shadow_evaluation_research_decision_rule_2026-05-27.md` |
| TBD | First shadow reaches n=63 | Apply rule, write eval doc |
| TBD | Auto-kill fires | Rule SUSPENDED (auto-kill takes precedence) |
