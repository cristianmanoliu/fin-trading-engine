# Drift detector dry-run validation — 2026-05-22

**Status:** Pre-staged operational confidence check. NOT a decision. NOT a research finding. NOT a pre-registration.

Forward-paper day 14/60, n_live=19/150. Decision-grade drift detector floor is n_min=30. ETA n_live ≥ 30 ≈ 2026-05-30 at observed 1.36 trades/day rate. This document captures a dry-run of the detector's behavior against synthetic 30-trade journals so that when the floor is reached and the wrapper fires for real, the operator (you) has zero ambiguity about what the output means.

## Why dry-run before the gate goes live

Operational confidence ≠ decision making. The gate cannot fire correctly the first time it runs in earnest if the operator hasn't seen its CLEAN vs DRIFT outputs side-by-side. Pre-staging this in a low-emotional-stake context (mid-MONITORING, no decision pressure) prevents misreading the first real firing under load.

This is the same logic as `auto_kill_execution_decision_rule_2026-05-08.md` — pre-register the mechanical response before the trigger fires.

## Dry-run scenarios

Two synthetic 30-trade journals built locally in `/tmp/drift_dryrun/`:

### Scenario A: matched distribution (CLEAN expected)

Synthesized with WR=22.2%, win_pnl_mean=$5,129, loss_pnl_abs=$1,065, mfe_r=2.29 — matching the backtest reference `results/hod_journals/2026-05-07-mfe/` (n=2,210).

Observed dry-run (RNG-realized: WR 23.3%, PnL +$11,084):

```
metric              live mean        Δ       t/z      p     status
pnl_per_trade       +369.48 $        +58.59  -0.12  0.9051  ✓ ok
mfe_r                 +2.29 R         +0.00  -0.00  0.9962  ✓ ok
mae_r                 +0.88 R         -0.07  +1.29  0.1954  ✓ ok
loss_pnl_abs       +1,077.64 $       +12.30  -0.63  0.5272  ✓ ok
win_pnl            +5,124.30 $        -4.82  +0.04  0.9697  ✓ ok
WR                     23.3 %        +1.12pp +0.15  0.8839  ✓ ok
```

**Wrapper exit: 0 (CLEAN).**

### Scenario B: empirical-LIVE distribution (DRIFT expected)

Synthesized to match observed forward-paper: WR=10.5%, win_pnl_mean=$5,917, loss_pnl_abs=$1,137, mfe_r_win=6, mfe_r_loss=0.7. Models what the detector will see if the next 11 trades land in the same shape as the current 19.

Observed dry-run (RNG-realized: WR 6.7%, PnL −$20,041):

```
metric              live mean        Δ        t/z       p     status
pnl_per_trade       -668.05 $       -978.94  +2.94   0.0033  ✓ ok
mfe_r                 +1.05 R         -1.25  +4.61   0.0000  ★ DRIFT
mae_r                 +1.03 R         +0.09  -2.10   0.0362  ✓ ok
loss_pnl_abs       +1,139.51 $       +74.17  -6.43   0.0000  ★ DRIFT
win_pnl            +5,932.47 $      +803.35  -9.95   0.0000  ★ DRIFT
WR                      6.7 %       -15.55pp -2.04   0.0412  ✓ ok
```

**Wrapper exit: 1 (INVESTIGATION — single firing).**

3 of 6 metrics tripped Bonferroni-corrected α (per-test α = 0.001/6 ≈ 0.000167).

## Operational insights surfaced by the dry-run

### 1. WR alone does NOT carry drift detection at n=30

Even at WR 6.7% (vs backtest 22.2% — a 15.5pp gap), the WR 2-prop z-test p=0.0412, which is **above** the Bonferroni floor of 0.000167. WR is the noisiest single metric at small n. Distribution-shape metrics (mfe_r, win_pnl, loss_pnl_abs) carry the signal.

**Implication for ~05-30 read:** if you reach n=30 with similar low WR but distributions matching backtest (~6R wins, ~1R losses), the detector likely CLEAN. The current LIVE state qualitatively matches that pattern — wins land at ~$5.9k (≈6R), losses at ~$1.1k (≈1R). Stops/wins behaving by design.

### 2. The drift signal would come from mfe_r if it comes at all

Among the 3 tripped metrics in Scenario B, the synthesized mfe_r distribution was the most divergent (−1.25R mean, p<0.0001). This was engineered: synth losses had mfe_r=0.7 (matching DOTUSDT's 2.82R outlier-skewed observed pattern). Backtest reference shows mean mfe_r=2.29R.

**Implication:** if our actual 30-trade window has fewer "high-MFE" excursions per loss than backtest, mfe_r will trip first. If observed MFE distribution matches backtest, the detector likely CLEAN regardless of WR.

### 3. Single firing ≠ kill

Per `kill_bar_recal_verdict_2026-05-07.md` + `drift_detector_time_to_detection_verdict_2026-05-08.md`:

- Exit 1 (INVESTIGATION) is **not** an auto-kill
- Auto-kill (exit 4) requires **two firings ≥7 days apart** OR drift+threshold combination
- Operator response to exit 1: cross-check forward-paper status, investigate, do NOT halt

## Reference: detector internals (validated by dry-run)

- Backtest ref: `results/hod_journals/2026-05-07-mfe/` (n=2,210, sanity floor 500)
- Live source: VPS journal at `/var/log/paper-live/journal/` (tar+ssh fetch)
- Statistical: Welch t (continuous metrics) + 2-prop z (WR), Bonferroni-corrected across 6 tests
- α_family = 0.001, α_per_test ≈ 0.000167
- N_MIN = 30 (hard floor; exit 2 below)
- Exit codes: 0 CLEAN / 1 INVESTIGATION / 2 INSUFFICIENT / 3 ERROR / 4 AUTO-KILL / 5 HISTORY_CORRUPT

## When ~05-30 arrives

1. Sunday weekly_audit.sh auto-fires drift_check (scheduled via `deploy/drift-check.launchd.plist`)
2. If n_live ≥ 30 at that point: get exit 0/1/2/3 directly
3. If exit 0 (CLEAN): MONITORING continues. Next gate is forward-paper deploy criteria (150 trades, 60 days, ≥60% PnL of pro-rated honest-annual, beats BTC HODL @ $16k notional)
4. If exit 1 (INVESTIGATION): cross-check `scripts/forward_paper_status.sh` per the firing investigation playbook. Single firing is investigation-grade, not kill
5. If exit 4 (AUTO-KILL): triggers `auto_kill_execution_decision_rule_2026-05-08.md` Phase 1 HALT (paper-only at STAGE_0, no real money to close yet)

## What this document is NOT

- NOT a pre-registration. The detector calibration is locked elsewhere.
- NOT a decision. ~05-30 is days away; current state is MONITORING.
- NOT a forecast. RNG-realized scenarios are illustrative, not predictive of actual ~05-30 outcome.
- NOT an excuse to fire the detector ad-hoc. Cadence remains weekly (Sun) per locked rule. Mid-week firing is operator-authorized only.

## Cross-references

- `results/drift_detector_calibration_verdict_2026-05-07.md` — α=0.001 calibration
- `results/drift_detector_time_to_detection_verdict_2026-05-08.md` — weekly cadence
- `results/kill_bar_recal_verdict_2026-05-07.md` — advisory thresholds vs decision-grade
- `results/real_money_protocol_decision_rule_2026-05-08.md` — STAGE_0 → STAGE_1 criteria
- `results/auto_kill_execution_decision_rule_2026-05-08.md` — Phase 1-6 kill execution
- `docs/OPERATOR_HANDBOOK.md ## Scenario playbook` — drift firing + PROMOTION READY scenarios

## Dry-run artifacts

`/tmp/drift_dryrun/` — synthetic journals + test history. Ephemeral, will be wiped by OS. Not committed.
