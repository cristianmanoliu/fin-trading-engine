# Honest-annual pro-rate gate — pre-registered decision rule

**Status:** LOCKED 2026-05-24, BEFORE forward-paper resolution.
**Scope:** defines the exact arithmetic for the deploy criterion "Live PnL ≥ 60% of pro-rated honest-annual ($69k/yr × elapsed × 0.60)" and its analogous STAGE_3→STAGE_4 form, resolving every ambiguity in how `elapsed` is computed, what counts toward `live_pnl_usd`, and what baseline is used.

---

## Why this rule exists

`real_money_protocol_decision_rule_2026-05-08.md` states the criterion but leaves three ambiguities open:

1. **`elapsed` definition**: calendar days vs trading days; floor vs ceiling; which reference timestamp closes the window.
2. **`live_pnl_usd` numerator**: whether in-flight unrealized PnL counts; whether paper trades on shadow engines count; which journal directory is authoritative.
3. **Baseline ($69k/yr)**: whether to update it if backtest parameters change during forward-paper; whether it applies per-cohort or fleet-wide.

Without locking these now, the resolution moment becomes a design exercise under maximum emotional pressure. This rule makes each ambiguity a mechanical lookup.

---

## Canonical definitions

### `elapsed` (calendar days since forward-paper start)

```
elapsed = floor( (t_freeze − t0) / 86400 )
```

- `t0 = 2026-05-05T20:06:00Z` — forward-paper start timestamp (CLAUDE.md).
- `t_freeze` — the freeze timestamp defined by `resolution_data_freeze_decision_rule_2026-05-24.md §Freeze moment`. For the STAGE_0→STAGE_1 gate, `t_freeze` is the resolution-freeze timestamp (PROMOTE verdict). For ongoing real-money stage gates, `t_freeze` is the stage-transition timestamp.
- **Floor** (integer division): avoids inflating the denominator on a partial-day window. A freeze at hour 23 on day 59 reports `elapsed = 59`, not 60.
- **Seconds denominator (86400)**: every day is treated as exactly 86400 seconds. Leap seconds are ignored; they shift `elapsed` by at most 1 second in the denominator and cannot move the floor result.
- **Cross-reference**: the `elapsed` value stored in `results/freeze/<t_freeze>/forward_paper_resolution.txt` IS the canonical value for the STAGE_0→STAGE_1 gate. Do not recompute from a different source.

### Annual rate (the $69k baseline)

```
annual_usd = 69_000
```

- Source: `docs/findings/2026-05-05-pm.md` "honest annual" recalculation (train-only-shortlist, slip=25bp, 16 deployed symbols). This is the **frozen** baseline.
- **Not updated** during forward-paper if backtest parameters change. The baseline was locked before forward-paper started; changing it mid-cohort is the exact post-hoc optimization this rule prevents.
- **Fleet-wide, not per-cohort**: the $69k figure covers all 16 live symbols at $1k stake. It is not divided by cohort count.
- If the strategy were to add symbols mid-cohort (which CLAUDE.md forbids), this rule does NOT auto-scale. Symbol count is frozen.

### Pro-rated target

```
target_usd = annual_usd × (elapsed / 365.25) × fraction
           = 69_000 × (elapsed / 365.25) × fraction
```

- **365.25**: accounts for leap years over a multi-year horizon. Using 365 vs 365.25 shifts the result by < 0.07% — negligible — but 365.25 is the conventional annualization denominator and is locked here.
- `fraction`:
  - STAGE_0→STAGE_1 gate: **0.60** ("≥60% of pro-rated honest-annual")
  - STAGE_3→STAGE_4 gate: **0.50** ("≥50% of pro-rated honest-annual") per `real_money_protocol_decision_rule_2026-05-08.md §STAGE_3→STAGE_4`

### `live_pnl_usd` (the numerator)

```
live_pnl_usd = sum of pnl_usd across all CLOSED trades
               in the LIVE cohort journal
               from t0 to t_freeze (inclusive of both endpoints)
```

**Inclusions:**
- All closed trades (entry + exit both recorded) in the **live cohort** journal (`/var/log/paper-live/journal/live/SYMBOL-YYYY-MM.jsonl`) for all 16 deployed symbols.
- PnL is **realized only**: `pnl_usd` field from the `CLOSE` journal event. No mark-to-market.

**Exclusions:**
- In-flight positions open at `t_freeze`. Unrealized PnL is noise; the gate tests realized edge. Per `resolution_data_freeze_decision_rule_2026-05-24.md §Recovered / in-flight position handling`.
- Shadow-cohort journals (alt5-15-336, alt5-15-504, bb20). Those are research cohorts, not the deployed live cohort.
- Trades with `entry_ts < t0` (pre-forward-paper; none expected but the filter is explicit).

**Source of truth**: the `live_pnl_usd` value in `results/freeze/<t_freeze>/forward_paper_status.txt` (fresh run at freeze) IS the canonical numerator. The `scripts/forward_paper_status.sh` script computes this from the journal cache. Do not recompute from a different script or path.

---

## Gate evaluation (STAGE_0→STAGE_1)

The gate passes (PASS) if and only if:

```
live_pnl_usd  ≥  69_000 × (elapsed / 365.25) × 0.60
```

Computed at `t_freeze`. Inputs taken from `results/freeze/<t_freeze>/forward_paper_status.txt`.

**Example (illustrative, not predictive):**
- `elapsed = 127` days
- target = 69,000 × (127 / 365.25) × 0.60 = 69,000 × 0.3476 × 0.60 ≈ **$14,387**
- If `live_pnl_usd ≥ $14,387`: gate PASSES

**Negative PnL**: if `live_pnl_usd < 0`, the gate fails trivially (negative < positive target). The separate "≥60 calendar days net-positive in dollar terms" criterion also fails. Both failures are recorded independently in the resolution artifact.

---

## Gate evaluation (STAGE_3→STAGE_4)

```
live_pnl_usd  ≥  69_000 × (elapsed / 365.25) × 0.50
```

Here `elapsed` = `floor((t_stage3_to_4 − t0) / 86400)` where `t_stage3_to_4` is the stage-transition timestamp at the moment STAGE_3→STAGE_4 is being evaluated. `t0` is unchanged (forward-paper start).

`live_pnl_usd` at this gate includes all closed live-cohort trades from `t0` through the evaluation moment, including STAGE_1, STAGE_2, and STAGE_3 trades — cumulative, not stage-segmented. Rationale: the pro-rate compares cumulative realized edge vs elapsed time; segmenting would allow a bad STAGE_1 to be hidden by a good STAGE_3.

---

## What does NOT qualify as a definition dispute

If the resolution moment produces a value that is within 2% of the threshold in either direction, the operator MAY NOT apply judgment to round, adjust, or reinterpret. The mechanical formula is the gate. A 0.1% miss is a FAIL; a 0.1% pass is a PASS.

The only permitted overrides:
- **Journal corruption**: if `scripts/journal_validate` exits non-zero at freeze, the gate is BLOCKED (cannot evaluate) until a clean journal is produced. This is exit code 1 PARTIAL in the freeze bundle.
- **`t0` dispute**: if a journal recovery event restored a trade with `entry_ts < t0`, the trade is still included if its CLOSE event falls within `[t0, t_freeze]`. Entry timestamp does not govern inclusion; close timestamp does. Rationale: the strategy was live at `t0`; a recovered trade that closed after `t0` contributed to the forward-paper outcome.

---

## Relationship to other locked rules

| Rule | How this rule depends on it |
|---|---|
| `resolution_data_freeze_decision_rule_2026-05-24.md` | Source of `t_freeze`, canonical journal cache path, in-flight exclusion rule |
| `real_money_protocol_decision_rule_2026-05-08.md` | Provides the fraction constants (0.60 / 0.50) and stage structure |
| `forward_paper_outcome_resolution_decision_rule_2026-05-10.md` | Provides `forward_paper_resolution.txt` that triggers the freeze |
| `btc_hodl_notional_amendment_2026-05-12.md` | BTC-HODL gate (separate criterion, co-evaluated at same freeze) |

---

## Audit-lens compliance

1. **Missing-input → silent-success**: if `forward_paper_status.txt` is absent from the freeze bundle (exit 1 PARTIAL in freeze), this gate cannot be evaluated and the resolution script records it as BLOCKED, not PASS. No silent success possible.
2. **Writer-equals-model**: this rule computes NO verdict — it defines arithmetic. `forward_paper_resolution.py` reads `live_pnl_usd` from `forward_paper_status.txt` (already pre-registered writer) and applies the formula above. No new writer introduced.
3. **Sibling-bug propagation**: the `live_pnl_usd` value is always sourced from `forward_paper_status.sh` output (existing, tested tool). This rule does NOT reimplement journal aggregation.
4. **Telegram-tier**: no direct alert triggered by this rule — the resolution verdict (PROMOTE/KILL) triggers the alert via `forward_paper_outcome_resolution_decision_rule_2026-05-10.md`. Gate PASS/FAIL is recorded in `forward_paper_resolution.txt` verbatim.

---

## Implementation site

`scripts/forward_paper_resolution.py` — the gate check already reads `live_pnl_usd` from `forward_paper_status.txt`. The formula constants (`HONEST_ANNUAL_USD = 69_000`, `PRORATE_FRACTION_STAGE01 = 0.60`, `PRORATE_FRACTION_STAGE34 = 0.50`, `T0 = datetime(2026, 5, 5, 20, 6, 0)`, denominator `365.25`) should match this doc exactly. Any discrepancy between the script and this doc is a BUG in the script, not a conflict to be resolved by judgment.

---

## When this becomes decision-relevant

At the resolution moment for STAGE_0→STAGE_1, and again at the STAGE_3→STAGE_4 evaluation. Until then, `forward_paper_status.sh` shows running values for monitoring only — not verdict-grade inputs.
