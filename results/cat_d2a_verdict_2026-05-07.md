# Cat D2a out-of-sample verdict: D1 mechanism does NOT replicate

**Date:** 2026-05-07
**Decision rule:** `results/cat_d2a_decision_rule_2026-05-07.md` (pre-registered).
**D1 baseline (in-sample):** Sharpe 0.629, +28% vs baseline 0.49 — verdicted SUPPORTIVE-UNDERPOWERED under D2 rule.

## D2a result (1D EMA 5/13 confluence)

Universe-57, 4H short, EMA 9/21 entry, mh504, slip=15bp, fee=10bp.
Same setup as D1 but with **DIFFERENT 1D bias periods (5/13 vs D1's 9/21)** — out-of-sample for the Sharpe rule.

| Window               | NET     | Trades | WR      | Profitable    |
|----------------------|--------:|-------:|--------:|---------------|
| W-2 (2020-05→2021-04)| -59,912 | 231    | 12.99%  | 12/34         |
| W-1 (2021-05→2022-04)| +257,037| 618    | 22.49%  | 30/45         |
| W0  (2022-05→2023-04)| -11,790 | 721    | 16.78%  | 18/51         |
| W1  (2023-05→2024-04)| +160,305| 792    | 19.82%  | 37/57         |
| W2  (2024-05→2025-04)| +118,944| 899    | 19.47%  | 31/57         |
| W3  (2025-05→2026-04)| -70,276 | 1015   | 17.24%  | 27/56         |
| **Sum**              | **+394,308** | 4,276 | — | — |
| **Mean**             | **+65,718** | — | — | — |
| **σ across windows** | **$133k** | — | — | — |
| **Sharpe**           | **0.494** | — | — | — |
| **Wins**             | **3/6**   | — | — | — |

## D2a verdict per pre-registered rule

| Metric                          | D2a Value | Threshold        | Pass |
|---------------------------------|-----------|------------------|------|
| Sum > 0                         | +$394k    | > 0              | ✓    |
| Sharpe > baseline (0.492)       | 0.494     | > 0.492          | ✓ (barely — +0.002 = +0.3%) |
| Sharpe > baseline × 1.25 (0.615)| 0.494     | > 0.615          | ✗    |

**Verdict: NEUTRAL** (Sharpe > baseline AND sum > 0, but Sharpe improvement only +0.3% — well within noise band of ±0.43 SE at n=6).

## D1 deployment decision (per D2a rule's matrix)

| D1 verdict (in-sample)            | D2a verdict (out-of-sample) | Combined deploy decision |
|-----------------------------------|------------------------------|--------------------------|
| SUPPORTIVE-UNDERPOWERED            | **NEUTRAL**                  | **DO NOT deploy D1**     |

The matrix's literal text: *"D1 was likely noise; do NOT deploy D1."*

## Mechanism interpretation

D1 with 1D EMA 9/21 had Sharpe 0.629 (+28%). D2a with 1D EMA 5/13 has Sharpe 0.494 (+0.3%). Same mechanism, different parameters → wildly different Sharpe outcomes. This is the **textbook signature of a fragile signal** that won't replicate forward.

If the 1D bias filter were capturing real structural information, modest parameter variations should preserve directional improvement (maybe smaller magnitude, but same sign). Instead D2a's Sharpe collapsed back to baseline. D1's +28% Sharpe was almost certainly statistical noise from a fortunate parameter choice.

This is exactly why the pre-registered D2 rule REQUIRED an out-of-sample test before deployment. The test functioned as designed — caught a noise signal that would have wasted a shadow slot (and 180+ days of forward observation) on a non-replicable mechanism.

## Per-window comparison D1 vs D2a

| Window | D1 (9/21) | D2a (5/13) | Δ        | Same sign? |
|--------|----------:|-----------:|---------:|------------|
| W-2    | -49,358   | -59,912    | -10,554  | ✓ both negative |
| W-1    | +310,827  | +257,037   | -53,790  | ✓ both positive |
| W0     | +8,427    | -11,790    | -20,217  | ✗ flipped       |
| W1     | +247,591  | +160,305   | -87,286  | ✓ both positive |
| W2     | +252,571  | +118,944   | -133,627 | ✓ both positive |
| W3     | -97,754   | -70,276    | +27,478  | ✓ both negative |

Same sign in 5/6 windows — so the **direction** of the signal is consistent. But the **magnitude** is consistently weaker for D2a (smaller positives, smaller mitigation of negatives). This pattern is consistent with: "the slower 9/21 filter happens to have caught more profitable trade-clusters, while the faster 5/13 filter is noisier/less effective." Neither is structurally superior — both are within the noise band of baseline.

## Final actions

- **D1 (1D EMA 9/21 confluence): NOT DEPLOYED.** Out-of-sample test failed to confirm.
- **BB (Bollinger 20, 2.0): SHADOW DEPLOYED on VPS** as `bb20:bb:20-2.0-504`. (Independent of D2a — BB was confirmed by both literal Cat A rule and D2 Sharpe rule.) Status verified via `post_deploy_check.sh`.
- **Live deploy unchanged.** EMA 9/21 mh504 4H short remains the live strategy on 16 paper engines.
- **Real-money allocation: still ZERO.** Shadow journals from BB will accumulate forward-paper data; real-money escalation gated on the existing forward-paper criteria in `CLAUDE.md`.

## Commitments observed

1. Pre-registered D2a rule applied LITERALLY — even though D2a's Sharpe technically beat baseline by 0.3%, that's within the rule's NEUTRAL band, not SUPPORTIVE.
2. Did NOT lower the Sharpe threshold to push D2a into SUPPORTIVE.
3. Did NOT relax the deployment matrix to deploy D1 anyway.
4. Did NOT propose D2b (third parameter variation: 21/55? 7/15?) to keep searching.
5. The required out-of-sample-confirmation safeguard was applied AND honored its negative finding.

## Cumulative search summary (across two sessions)

**11 candidates tested against the same baseline:**

| Cat | Candidate | Verdict (LITERAL rule) | Deployed? |
|---|---|---|---|
| A | VWAP fade | REJECTED | No |
| A | PDH/PDL break | REJECTED | No |
| A | RSI cross-50 | Inconclusive | No |
| A | MACD cross | REJECTED | No |
| A | **BB breakdown** | **WEAK (literal) + D2 SUPPORTIVE** | **YES — shadow deployed 2026-05-07** |
| B1 | Trail 1R | REJECTED | No |
| B2 | MLTP 3R/50% | NEUTRAL | No |
| D1 | 1D conf 9/21 | NEUTRAL (sum) / SUPPORTIVE-UNDERPOWERED (Sharpe), refuted by D2a | No |
| E1 | Vol < 120% | NEUTRAL | No |
| D2a | 1D conf 5/13 | NEUTRAL | No (was confirmation test) |

**Result: 1 of 11 candidates qualifies for shadow deploy.** That candidate (BB) survives BOTH the literal Cat A rule and the D2 Sharpe rule — multi-rule confirmation gives reasonable confidence. All other candidates rejected.

The cumulative search exhaustion is now substantial. Future strategy work should
either:
- (a) Wait for forward-paper data (BB shadow + EMA shadows) to accumulate decision-grade evidence on the existing candidates
- (b) Test fundamentally different strategy types requiring infrastructure changes (Cat F portfolio sizing, carry/basis trades)
- (c) Iterate on cost stack (lower-fee VIP tier, different exchange) to potentially open up previously-marginal strategies

**Real-money allocation: ZERO** until forward-paper criteria pass per `CLAUDE.md`.
