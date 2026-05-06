# Pre-registered decision rule for Cat D2: Sharpe-gated re-evaluation
**Written: 2026-05-07, AFTER observing D1 Sharpe = 0.63 in the Cat D/E sweep.**

## Disclosure: data-snooping risk

**This rule is being written with knowledge of D1's observed Sharpe (0.63 vs
baseline 0.49).** The thresholds below are chosen on principled grounds
(industry-standard Sharpe improvement bands, statistical power at n=6),
not engineered around D1's specific value. But the choice to ASK the
question "what if we used Sharpe instead of sum?" was prompted by D1's
result — so the framing itself is informed by data.

**Mitigation:** thresholds set BELOW where D1's Sharpe might let it pass
trivially, AND flag any positive verdict explicitly as "needs out-of-
sample confirmation via a new D2 variant test."

## What we're testing

Re-evaluate already-run candidates from Cat A/B/D/E under a **Sharpe-based
gating rule** instead of the dollar-sum gate used previously. Same data,
different decision criterion. The premise: when capital is risk-budgeted
(as ours is — fixed $1k risked per trade), Sharpe (mean/σ) better captures
deployment desirability than absolute dollar sum.

The candidate of immediate interest: **D1 (1D bias confluence)** — only
candidate observed to improve Sharpe meaningfully in prior runs.

## Sample-size context: Sharpe SE at n=6

The standard error of an estimated Sharpe at n=6 windows is approximately:

    SE(Sharpe) ≈ sqrt((1 + Sharpe²/2) / n) ≈ sqrt(1.12 / 6) ≈ 0.43

So Sharpe estimates at n=6 carry ~±0.43 noise. Differences of <0.4 between
two candidates are NOT statistically distinguishable. This means:
- Any rule that fires on small Sharpe deltas (e.g., 10-15%) is firing on noise.
- A meaningful improvement at n=6 needs Sharpe delta ≥ 1.0 SE ≈ 0.4 to clear noise.
- Thresholds below adjust for this — STRONG requires very large delta; SUPPORTIVE explicitly flagged as underpowered.

## Decision rule

Dual gate: candidate must improve Sharpe AND maintain positive sum.

| Outcome                                                          | Verdict        | Action                                              |
|------------------------------------------------------------------|----------------|-----------------------------------------------------|
| Sharpe ≥ baseline × 2.00 (i.e., +100%) AND sum > 0               | **STRONG**     | Replace deploy + 90 days observation                |
| Sharpe ≥ baseline × 1.50 (+50%) AND sum > 0                      | **SUPPORTIVE** | Shadow deploy + 90 days observation (high confidence) |
| Sharpe ≥ baseline × 1.25 (+25%) AND sum > 0                      | **SUPPORTIVE-UNDERPOWERED** | Shadow deploy + 180 days observation; flag as statistically noisy |
| Sharpe > baseline (any improvement) AND sum > 0                  | **NEUTRAL**    | No deploy change (improvement within noise band)    |
| Sharpe ≤ baseline OR sum ≤ 0                                     | **REJECTED**   | Reject                                              |

**Reasoning for thresholds:**
- **+100% (STRONG):** Very large effect, clears n=6 SE comfortably. Approximately 2× baseline = 0.98 absolute, or +0.5 above baseline absolute = >1 SE.
- **+50% (SUPPORTIVE):** Moderate effect, marginal statistical clearance. About 0.5 SE — borderline distinguishable.
- **+25% (UNDERPOWERED):** Small effect, well within noise band. Triggers a longer observation window (180 days vs 90) to compensate for noise risk.
- **<25%:** Improvement is ≤ baseline + 0.1 SE — indistinguishable from chance. NEUTRAL or REJECTED.

## Stage 2: out-of-sample confirmation

Any candidate verdicted SUPPORTIVE or stronger under D2 must, before live
deployment, pass an OUT-OF-SAMPLE test in a future session:
- A NEW D2 variant (different parameters or different filter mechanism) tested under the same rule
- Confirms the Sharpe-gated framework isn't just rewarding D1's specific noise pattern
- Required for SUPPORTIVE-UNDERPOWERED → "deploy as shadow" requires also a new variant test before any real-money allocation

This is a self-imposed safeguard against the data-snooping concern.
The shadow deploy is the in-sample placeholder; the out-of-sample test
is the decision-grade evidence.

## Pre-registered hypothesis

Most likely outcome: **D1 verdicts as SUPPORTIVE-UNDERPOWERED.** Its
observed +28% Sharpe is below the SUPPORTIVE threshold (+50%) and well
below STRONG (+100%). The UNDERPOWERED tag triggers shadow deploy with
LONG observation window (180 days) and a required out-of-sample test.

Other prior candidates:
- B1, B2: B1 has negative sum (rejected on second gate), B2 has lower Sharpe than baseline (rejected). Expect both REJECTED.
- E1: ≈ baseline Sharpe — rejected.
- MACD, BB, others: mostly rejected on sum.

If ALL candidates verdict NEUTRAL or REJECTED under Sharpe rule: the
deployed configuration is robust on Sharpe metric too. Status quo holds.

If D1 alone passes SUPPORTIVE-UNDERPOWERED: trigger an out-of-sample
D2 variant test (next session) before any deployment change.

If 2+ candidates pass: multiple-comparison concern compounds. Treat
extra-skeptically.

## Commitments NOT to do regardless of results

1. NOT raise OR lower the Sharpe thresholds after seeing per-candidate results.
2. NOT switch back to sum-gating to "validate" a Sharpe-passing candidate.
3. NOT deploy real money based on this re-evaluation alone — even SUPPORTIVE
   triggers shadow deploy + out-of-sample variant test, NOT direct deployment.
4. NOT add candidates to this re-evaluation that weren't already run.
5. The "out-of-sample variant test" required for any positive verdict will be
   pre-registered with its OWN rule (not assumed to inherit D2's).
6. NOT redefine Sharpe (e.g., switch to Sortino, Calmar) to break ties.

## What this leaves

If D1 verdicts SUPPORTIVE-UNDERPOWERED: deploy as shadow today; queue
out-of-sample D2 variant for next session (e.g., 1D EMA 5/13 confluence
or 1D EMA 21/55). The variant test, under its own pre-registered rule,
becomes the deploy decision-gate.

If all candidates fail under D2: status quo holds. Cumulative search has
now exhausted both sum-gated AND Sharpe-gated re-evaluation. Strong
evidence for current deployment.
