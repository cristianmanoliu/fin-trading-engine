# Cat B exit-framework: B1 + B2 verdict

**Date:** 2026-05-07
**Decision rule:** `results/cat_b_decision_rule_2026-05-07.md` (pre-registered).
**Baseline:** `results/walk_forward_4H_short_6w_ema9-21_mh504_2026-05-06.txt` — fixed 6R TP + wick stop + mh504, 4H short, 6-window: sum +$922k, mean +$154k, σ $313k, 4/6 wins.

## Per-window NETs (universe-57, slip=15bp, fee=10bp, mh504, 4H short, EMA 9/21 entry)

| Window               | Baseline    | B1 trailing 1R | B2 mltp 3R 50% | B1 − base   | B2 − base   |
|----------------------|------------:|---------------:|---------------:|------------:|------------:|
| W-2 (2020-05→2021-04)| -275,493    | -136,277       | -243,083       | +139,216    | +32,410     |
| W-1 (2021-05→2022-04)| +459,660    | +37,406        | +316,247       | **-422,254**| -143,413    |
| W0  (2022-05→2023-04)| +128,734    | -60,899        | +20,144        | -189,633    | -108,590    |
| W1  (2023-05→2024-04)| +80,564     | +909           | +48,435        | -79,655     | -32,129     |
| W2  (2024-05→2025-04)| +564,922    | +175,813       | +405,593       | **-389,109**| -159,329    |
| W3  (2025-05→2026-04)| -35,772     | -176,598       | -83,221        | -140,826    | -47,449     |
| **Sum**              | **+922,615**| **-159,646**   | **+464,115**   | **-1,082k** | **-458k**   |
| **Mean**             | **+153,769**| **-26,607**    | **+77,352**    |             |             |
| **σ across windows** | **313k**    | 128k           | 244k           |             |             |
| **Wins**             | **4/6**     | **3/6**        | **4/6**        |             |             |
| **σ / baseline σ**   | 1.00        | 0.41           | 0.78           |             |             |
| **r vs baseline**    | 1.00        | 0.887          | **0.996**      |             |             |

## Stage 1 verdict (literal application of pre-registered rule)

| Strategy  | Wins | Sum     | Rule row hit                     | Verdict                |
|-----------|------|---------|----------------------------------|------------------------|
| **B1**    | 3/6  | −$160k  | "Wins 3/6 OR sum < 0"            | **REJECTED** (Inconclusive — both clauses true) |
| **B2**    | 4/6  | +$464k  | "Wins ≥4/6 AND sum > 0 but ≤ baseline" | **NEUTRAL** (no improvement; do not change deploy) |

## Stage 2 — variance comparison (observational only; doesn't change Stage 1 verdict)

Pre-registered: σ comparison applies "only for STRONG / SUPPORTIVE" candidates.
Neither B1 nor B2 reached SUPPORTIVE. The σ values below are documented for
context, not used to override Stage 1.

- **B1:** σ ratio 0.41 → would have been VARIANCE-REDUCING (< 0.7) had B1 passed Stage 1.
- **B2:** σ ratio 0.78 → NEUTRAL band (between 0.7 and 1.3).

## Mechanism interpretation

The per-window delta column tells a clean story for both candidates:

- **On baseline-WINNING windows (W-1, W2):** B1 and B2 give back $389k–$422k vs baseline. Both exit logics cap tail captures — trail forces premature exit at locked-R, scale-out banks half the position at 3R missing the 3R→6R move on the half-leg.
- **On baseline-LOSING windows (W-2, W3):** Both candidates marginally reduce losses but don't reverse them. The improvement (≤$139k on W-2) is small relative to the loss they cause on big-win windows.

Net: both candidates trade upside variance for downside variance asymmetrically — they reduce both, but reduce upside MORE. Result is lower expected sum without commensurate downside protection. Classic over-managed exit pattern: defending profits costs more than it saves.

The high correlation (B2 r=0.996, B1 r=0.887) confirms these are the SAME regime exposure as baseline, just with magnitude-capped P&L. They're not capturing a different edge — they're attenuating the existing one.

## Final action

- **B1: REJECT** for any deployment role.
- **B2: NEUTRAL — no deploy change.** Despite qualifying as positive in 4/6 windows, it underperforms baseline by $458k cumulative. Pre-registered rule says NEUTRAL = "no deploy change."
- **Live deploy unchanged.** 16 paper engines continue running EMA 9/21 mh504 fixed 6R + wick stop.

## Commitments observed

1. Pre-registered rule applied LITERALLY — no goalpost-moving, no metric-switching.
2. Did NOT switch verdict to "SUPPORTIVE" for B2 by redefining "baseline" downward.
3. Did NOT use Sharpe / risk-adjusted-return as a tiebreaker (rule says sum is the gate at Stage 1).
4. Did NOT exclude W-1 or W2 (the windows that hurt the candidates) for being "atypical bull-trap" or similar.
5. Did NOT test B1 with different trail intervals (0.5R, 2R, 3R) after seeing 1R failed.
6. Did NOT test B2 with different mid-R splits (2R/4R, 60/40 fractions) after seeing 50/50 underperformed.
7. Did NOT propose B1+B2 combo — that's a separate test for a future session with its own pre-registered rule.

## What this leaves

**Cumulative search exhaustion across two sessions:**
- Cat A entry triggers (2026-05-06 + 2026-05-07): 5 candidates tested, ALL REJECTED.
  - VWAP fade, PDH/PDL break, RSI cross-50, MACD cross, Bollinger breakdown.
- Cat B exit framework (2026-05-07): 2 candidates tested, BOTH REJECTED (B1) or NEUTRAL (B2).
  - Trailing stop 1R, multi-level TP 50% at 3R.

**Conclusion:** at this trigger / TF / cost stack, neither entry-trigger choice nor exit-framework choice meaningfully improves on the deployed configuration. The current strategy occupies a robust region — the kind of region where small parameter changes don't move the needle. This is consistent with the 2026-05-06 EMA-pair sweep finding (5/15 vs 9/21 within robust region was statistically chance).

The remaining unexplored axes:
- Multi-TF confluence (Cat D1) — not yet tested.
- Concentration / portfolio-level sizing (Cat F) — not yet tested.
- Entirely different cost stacks (lower fees with VIP, different exchange) — outside backtest scope.

These are next-session candidates if more sweep capacity is warranted. None require changes to the live deploy.

## Observation: variance reduction without sum

Both B1 and B2 reduced σ relative to baseline (0.41× and 0.78× respectively). For a Sharpe-targeting investor, B2's σ reduction of 22% with only a 50% sum reduction would be on the right side of the Sharpe ratio if absolute σ-per-dollar matters more than absolute dollar return.

But the rule's pre-registered Stage 1 gate is dollar sum, not Sharpe. We committed to that gate before seeing results, and we apply it. If a future session has a deliberate decision rule that gates on Sharpe instead, B2 might revisit. That's a re-pre-registration question, not a goalpost-move.
