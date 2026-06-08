# Alt-Breadth Regime Gate — Walk-Forward Verdict

- **Date:** 2026-06-08
- **Anchor:** Equal-weight alt-breadth index (16 deployed non-BTC symbols, daily close returns compounded, base=100). Built by `tasks/breadth_anchor_build.sh`.
- **Prior experiment:** `results/btc_regime_gate_backtest_verdict_2026-06-07.md` (BTC anchor, POSITIVE)
- **Verdict:** **NEGATIVE** — Crit 3 fails (parameter instability). Research-grade. No deployment implication.

## Result

Same harness as BTC experiment: expanding-window walk-forward, full-57 universe,
live cost stack (`--signal-tf 4H --fee-bps 10 --stop-slippage-bps 5 --exact-fills
--include-boundary --max-hold-hours 504 --funding-csv-dir`). Gate flips
SHORT/LONG/FLAT on the breadth anchor. Baseline = always-short on same episode windows.

```
fold  pick(X,Y,Z)         gate_OOS  baseline_OOS
2023  (5,30,14)            +198,585      -112,654
2024  (5,30,14)            +226,568       +84,577
2025  (5,30,7)             +351,474      +249,554
2026  (5,30,30)             -18,222       -15,898
----------------------------------------------------------------
OOS gate total:           +758,405
OOS baseline total:       +205,579
OOS gate per-year:  [198585, 226568, 351474, -18222]
picks:              [(5,30,14), (5,30,14), (5,30,7), (5,30,30)]
```

| Criterion | Result | Detail |
|---|---|---|
| 1. Beat baseline OOS | **PASS** | gate +$758k vs baseline +$206k |
| 2. No consecutive losing years (OOS) | **PASS** | only 2026 negative (−$18k), tiny |
| 3. Parameter stability | **FAIL** | 3 distinct picks across 4 folds |

## Why it fails Crit 3

X and Y lock cleanly: every fold picks X=5, Y=30. The short-activation signal
("BTC/alts down ≥5% over 30 days → short") is stable. Z (long-activation lookback)
is not: it selects 14, 14, 7, 30 across the four folds — no convergence. The fold
picks differ only on Z. This means the long-regime activation has no reproducible
sweet spot in this anchor; the LONG episodes carry no stable signal.

## What this tells us vs the BTC experiment

| Dimension | BTC anchor (POSITIVE) | Breadth anchor (NEGATIVE) |
|---|---|---|
| OOS gate total | +$700k | +$758k |
| Baseline OOS | −$77k | +$206k |
| X pick | 5 (all folds) | 5 (all folds) |
| Y pick | 30 (all folds) | 30 (all folds) |
| Z pick | 30 (all folds) | 14/14/7/30 (unstable) |
| Verdict | POSITIVE | NEGATIVE |

BTC price is a **cleaner regime signal than the alts themselves** for this harness.
Counterintuitive but real: BTC is lower-frequency and smoother; the equal-weight
breadth index is noisier, producing more frequent and shorter regime episodes that
destabilize the long-activation threshold.

The short-gate signal (X=5, Y=30) is confirmed by both experiments independently —
that is the robust finding. The long-gate (Z) is noise in both.

## Implications

1. **BTC short-gate remains the actionable finding.** Breadth doesn't improve on it.
2. **Long-gate is not deployable** from either experiment. Both show Z instability or
   equivalent — the EMA strategy likely has no validated long-side edge to unlock.
3. **Breadth anchor is not a drop-in replacement** for BTC. More episodes, shorter
   median duration, noisier Z selection.
4. **Shadow grid with breadth anchor: cancelled.** Running shadows under a
   NEGATIVE-verdict anchor answers the wrong question. If shadow gates are tested,
   use BTC anchor + short-gate-only (Z irrelevant, treat all LONG episodes as FLAT).

## Reproduce

```bash
bash tasks/breadth_anchor_build.sh                      # build BREADTH-1d.csv
bash tasks/breadth_regime_gate_grid.sh                  # 36 combos × gate/baseline (~6h, JOBS=4)
python3 tasks/walk_forward.py --resdir tasks/breadth_regime_gate_results
```
Outputs in `tasks/breadth_regime_gate_results/` (gitignored, regenerable).
