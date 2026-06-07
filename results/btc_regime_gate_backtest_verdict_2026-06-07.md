# BTC-Anchored Regime Gate — Walk-Forward Verdict

- **Date:** 2026-06-07
- **Decision rule (pre-registered):** `results/btc_regime_gate_backtest_decision_rule_2026-06-07.md`
- **Verdict:** **POSITIVE** (all 3 locked criteria pass) — but research-grade, **NOT a promotion trigger**. See §Caveats + §Disposition.
- **Scope:** Backtest only. No live/shadow/VPS change made or implied.

## Result

Expanding-window walk-forward, full-57 universe, live cost stack
(`--signal-tf 4H --fee-bps 10 --stop-slippage-bps 5 --exact-fills
--include-boundary --max-hold-hours 504 --funding-csv-dir`). Gate flips
SHORT/LONG/FLAT on a BTC momentum anchor (built from local BTC 1m → daily).
Baseline = always-short on the SAME episode windows (apples-to-apples
force-close geometry).

```
fold  pick(X,Y,Z)         gate_OOS  baseline_OOS
2023  (5,30,30)            +264,536      -551,877
2024  (5,30,30)            +271,231      +304,326
2025  (5,30,30)            +177,314      +188,142
2026  (5,30,30)             -12,586       -17,365
----------------------------------------------------------------
OOS gate total:           +700,494
OOS baseline total:        -76,774
OOS gate per-year:  [264536, 271231, 177314, -12586]
picks:              [(5,30,30), (5,30,30), (5,30,30), (5,30,30)]
```

| Criterion | Result | Detail |
|---|---|---|
| 1. Beat baseline OOS | **PASS** | gate +$700k vs baseline −$77k |
| 2. No consecutive losing years (OOS) | **PASS** | only 2026 negative (−$12.6k), and tiny |
| 3. Parameter stability | **PASS** | all 4 folds independently picked the SAME (5,30,30) |

## Why this is more than a curve-fit

1. **Parameter stability is the anti-overfit signature.** All four expanding-window
   folds, fit independently on different year spans, converged on the *same*
   (X=5, Y=30, Z=30). Overfitting produces fold-to-fold parameter churn; this is
   the opposite.
2. **The winning combo is #1-by-fit AND best-OOS** — not a lucky low-ranked pick
   that happened to do well out-of-sample.
3. **There is a plateau, not a spike.** For the 2023 fold, the top-6 combos ranked
   by in-sample fit are ALL positive in OOS 2023, and every one beats its
   short-baseline (baselines −$188k to −$552k). The edge is robust across a
   neighborhood of parameters, not a single knife-edge cell.

## What the edge actually is (be honest)

The gate's OOS advantage is **concentrated in 2023**: it beats baseline by ~+$816k
in 2023 alone (gate +$265k vs baseline −$552k). In 2024 and 2025 the gate slightly
*underperforms* baseline; 2026 is a wash. So the mechanism is precisely:
**"don't be short during the 2023 alt-bull."** 2023 is the year the original
analysis (`results/regime_analysis_2026-06-07.txt`) flagged as filter-hostile —
always-short lost there. The BTC anchor correctly flips to LONG/FLAT through the
2023 grind-up and avoids the bloodbath. That is a real, economically-sensible
regime call — but the headline +$700k rests heavily on one regime episode.

This directly answers the open question from the 06-07 entry-selection finding:
the EMA cross has no *per-trade* skill, but **conditioning DIRECTION on a slow
market-regime anchor does carry out-of-sample information** — the alpha lives in
the regime layer, exactly where r=+0.72 (P&L vs alt-breadth) said it would.

## Caveats (why this is NOT yet deployable)

1. **2023-concentrated.** Strip 2023 and the gate ≈ baseline. One more alt-bull
   like 2023 is where it earns its keep; if BTC/alt divergence breaks the anchor in
   the *next* bull (the 2025 cautionary case: BTC flat, alts crashed), it may not.
2. **Episode-length confound.** (5,30,30) makes the FEWEST, LONGEST regime episodes.
   Short episodes can't trade (4H/504h strategy needs days to prime + hold). The fit
   may be partly selecting "the config that lets the strategy trade at all," not
   purely "the best regime timer." The plateau mitigates but doesn't eliminate this.
3. **Gross of switching friction.** Full-57, no cost modeled for actually flipping a
   live book short↔long at each regime boundary (the harness force-closes at the
   boundary price — optimistic vs real flip slippage/funding).
4. **BTC is a noisy proxy for alt-breadth** (the real r=+0.72 driver). Anchor #2
   (breadth-swappable, per spec §4) is the next experiment, not yet run.

## Disposition (per spec §9 + locked protocol)

**Write the finding (this doc), index it, and STOP for a SEPARATE promotion
discussion. No live/shadow/VPS change in this work.** Per the project's locked
discipline, a positive backtest is the *start* of a productionization conversation
(Approach B: in-engine time-varying side filter → shadow), not an auto-promote —
and the forward-paper / drift protocol is untouched (the 06-07 drift KILL was
already adjudicated HOLD via the cross-check 9 censoring gate; this is orthogonal).

**Recommended next steps (for that separate discussion):**
1. Re-run with **alt-breadth anchor** (anchor #2) to test the BTC-proxy caveat — the
   single most informative follow-up.
2. Model switching cost (flip slippage + funding on regime boundaries).
3. If both survive: Approach B (productionize the gate in-engine) → 8-shadow slot,
   research-only, promotion-LOCKED, accruing forward calendar-time like every other
   shadow.

## Reproduce

```bash
bash tasks/btc_anchor_build.sh                 # BTC 1m → daily anchor
bash tasks/regime_gate_grid.sh                 # 36 combos × gate/baseline (~1.6h, JOBS=12)
python3 tasks/walk_forward.py                  # verdict table → summary.txt
```
Harness: `tasks/regime_label.py` (causal labeler, 6 unit tests),
`tasks/regime_gate_backtest.sh` (episode-slicing driver, parallel),
`tasks/regime_gate_grid.sh`, `tasks/walk_forward.py` (4 unit tests). Zero engine
changes. Outputs in `tasks/regime_gate_results/` (gitignored, regenerable).
