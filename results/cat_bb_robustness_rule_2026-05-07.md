# Pre-registered decision rule for BB(20, 2.0) shadow robustness check
**Written: 2026-05-07, BEFORE running BB(14) and BB(30) variant backtests.**

## Purpose

BB(20, 2.0) was deployed as a shadow on VPS today (2026-05-07) based on:
- Cat A literal verdict: WEAK (4/6 wins + mean > 0)
- D2 Sharpe verdict: SUPPORTIVE (Sharpe 0.892 = +81.5% vs baseline 0.49)

Both verdicts came from a SINGLE in-sample observation of the (20, 2.0) parameter set. The same skepticism we applied to D1 (which failed D2a out-of-sample) should apply to BB. **This test is the BB-equivalent of D2a:** does the BB mechanism replicate at neighboring parameter values, or was (20, 2.0) parameter-lucky?

## What we're testing

Two parameter variants of BB breakdown, holding all other settings identical to the deployed BB(20, 2.0):
- 4H signal TF, shorts-only, target_rr=6.0, max_hold_hours=504
- fee=10bp, slip=15bp, funding=CSV, universe-57

| Variant | BB Period | Std Mult | Mode flag                                        |
|---------|-----------|----------|--------------------------------------------------|
| **BB14**| 14        | 2.0      | `--bollinger-mode --bollinger-period 14 --bollinger-std-mult 2.0` |
| **BB30**| 30        | 2.0      | `--bollinger-mode --bollinger-period 30 --bollinger-std-mult 2.0` |

Period varied along ONE axis (14 = faster, 30 = slower than deployed 20). Std mult held constant. **One axis of variation = one test**, minimizing multi-comparison cost.

## Baseline

`results/walk_forward_4H_short_6w_ema9-21_mh504_2026-05-06.txt`:
- Sum: +$922,615  Mean: +$153,769  σ: $313k  Sharpe: 0.49  Wins: 4/6

In-sample reference (BB20):
- Sum: +$1,499,722  Mean: +$249,953  σ: $280k  Sharpe: 0.892  Wins: 4/6

## Per-variant decision rule

For each variant (BB14, BB30) independently:

| Outcome                                                | Per-variant verdict | 
|--------------------------------------------------------|---------------------|
| Sharpe ≥ baseline × 1.50 (≥ 0.74) AND sum > baseline (>$922k) | **STRONG**          |
| Sharpe ≥ baseline × 1.25 (≥ 0.62) AND sum > 0          | **ROBUST**          |
| Sharpe > baseline AND sum > 0 (any improvement)        | **WEAK**            |
| Sharpe ≤ baseline OR sum ≤ 0                           | **FAIL**            |

## Combined BB(20, 2.0) shadow deploy decision matrix

| BB14 verdict | BB30 verdict | BB(20, 2.0) shadow fate                                                       |
|--------------|--------------|-------------------------------------------------------------------------------|
| ROBUST or stronger | ROBUST or stronger | **STAYS** — BB mechanism robust across periods, BB(20) confirmed     |
| One ROBUST+, one WEAK | (any combo) | **STAYS** but flagged — moderate confidence; monitor at 30-day checkpoint |
| WEAK + WEAK         | — | **STAYS but flagged** — both variants positive but small Sharpe deltas; uncertain robustness |
| One STRONG/ROBUST + one FAIL | — | **PULLED** — mechanism is parameter-fragile; BB(20) likely parameter-lucky          |
| One WEAK + one FAIL | —          | **PULLED** — fragile mechanism, marginal evidence                             |
| FAIL + FAIL         | —          | **PULLED** — BB(20, 2.0) was almost certainly parameter-lucky                 |

**Pull execution if required:**
- Edit `deploy/systemd/paper-live@.service` to remove `,bb20:bb:20-2.0-504` from --shadow flag
- `./deploy/sync.sh` + ssh cp + daemon-reload + restart all 16 deployed engines
- Run `post_deploy_check.sh` to verify clean restart
- Bb20 shadow journals already-written are preserved (orphaned trades may exist; documented)

## Pre-registered hypothesis

PRIOR EXPECTATION: most likely **both ROBUST or one STRONG + one ROBUST.** Reasoning:
- BB(20) is the canonical parameter — neighboring values 14 and 30 are within typical practitioner usage range
- If BB mechanism is real, period sensitivity should be modest
- If both variants drop to baseline-equivalent Sharpe (like D1 → D2a), then BB(20) was lucky and the deploy was misjudged

Less likely but possible: divergent (one STRONG, one FAIL) — would suggest BB has non-monotonic period sensitivity, which would itself be a red flag for fragility.

## Multi-comparison context

This is the **12th and 13th candidates** tested against the same baseline.
But these are CONFIRMATION tests, not discovery tests:
- We're not asking "is there another winner?"  
- We're asking "is the existing deployed winner robust?"
- So multi-comparison impact is conceptually different

That said, we ARE consuming additional degrees of freedom. The thresholds above are deliberately strict (ROBUST requires +25% Sharpe, not just any positive delta) to compensate.

## Commitments NOT to do regardless of results

1. NOT test additional period values (e.g., 17, 25, 40) after seeing BB14/BB30 results.
2. NOT vary std-mult after seeing period-variation results.
3. NOT lower the FAIL threshold to "rescue" BB(20) if both variants fail.
4. NOT switch back to sum-gate alone to "rescue" BB(20).
5. The matrix above is binding — execute the deploy decision it produces, including the PULL action if required.
6. If PULL is required, do NOT skip the post_deploy_check.sh re-validation.
