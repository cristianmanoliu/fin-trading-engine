# Candidate #4 — Taker-flow imbalance (v1) — VERDICT: FEE-DEAD / NO-GO (2026-06-10)

**Type:** verdict (pre-registered design applied to data → mechanical reject).
**Candidate:** #4 of the 20-candidate orthogonal search (`results/strategy_candidates_2026-06-10.md`).
**Script:** `scripts/taker_flow_study.py`.
**Data:** 100% local — klines field 9 (`taker_buy_volume`) in all 57 local 1m CSVs. Zero fetch.

## The bet

Taker-buy / total-volume per bar = aggressor imbalance. Sustained taker-buy pressure (lifting offers) should lead price up; sustained taker-sell should lead down. Rolling z-score of the centered imbalance; trade the extremes directionally on a forward horizon.

## Pre-registration (frozen before run)

1H bars (1m resampled), imbalance = taker_buy_vol/total_vol − 0.5, 24-bar rolling z, top/bottom 10% z → LONG/SHORT, forward horizons H ∈ {1, 4, 24} bars, 15 bp round-trip cost, screen pass = 2× = 30 bp gross move. Honesty: median + win-rate + drop-top-5% + by-year.

## Result (full 57-symbol universe, n=501,826)

| H (bars) | n | gross bp | mean_net bp | med_net bp | win | t | drop5 bp | screen |
|---|---|---|---|---|---|---|---|---|
| 1 | 501826 | **0.96** | −15.82 | −18.26 | 39% | −94.77 | −30.79 | DEAD |
| 4 | 501826 | **2.33** | −16.48 | −18.15 | 44% | −51.01 | −45.38 | DEAD |
| 24 | 501826 | **7.13** | −16.31 | −17.02 | 48% | −20.33 | −86.34 | DEAD |

By-year (best horizon H=1): negative every single year 2020–2026, win 35–43%.

## Verdict: FEE-DEAD — NO-GO. No engine mode.

The gross forward move on extreme taker-flow z-score is **1–7 bp** across all horizons — an order of magnitude below the 15 bp round-trip cost, and far below the 30 bp screen. There is essentially **no forward predictive power**: the aggressor side at a bar's close is *contemporaneous* with that bar's move, not a leading indicator of the next bar(s). The directional trade just pays cost for noise (net ≈ −16 bp every cell), and the negative win rate (39%) confirms the extreme-z bars mean-revert if anything.

This is the cleanest no-go of Phase 0 — not a regime artifact (#8), not a tail-mirage (#11), just flat-zero signal at enormous n (t = −94 on the no-edge). Taker-flow imbalance derived from the kline stream carries no exploitable forward information at the 1H horizon.

## Note on v2

The plan lists a `#4-v2` unlocked by `scripts/download_metrics.sh` (the dedicated taker buy/sell **ratio** metric from the futures metrics archive, distinct from kline field 9). v1 being this dead lowers the prior on v2, but they are different series — v2 stays in Phase 2, evidence-downgraded by this result.

## Gates not reached

Walk-forward / overfit / corr-to-LIVE not run — a 1 bp gross signal under a 15 bp cost floor is dead at the screen.

## Cross-references

- Spec: `results/strategy_candidates_2026-06-10.md` (#4), v2 in Phase 2 plan
- Honesty lens (median + tail + by-year): `results/settlement_drift_verdict_2026-06-10.md` (#11)
