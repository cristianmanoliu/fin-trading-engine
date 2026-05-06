# Cat A entry-mechanism completion: MACD + Bollinger verdict

**Date:** 2026-05-07
**Decision rule:** `results/cat_a_decision_rule_2026-05-06.md` (pre-registered 2026-05-06 EOS).
**Baseline:** `results/walk_forward_4H_short_6w_ema9-21_mh504_2026-05-06.txt` — EMA 9/21 mh504 4H short, 6-window: sum +$922k, mean +$154k, 4/6 wins.

## Per-window NETs (universe-57, slip=15bp, fee=10bp, mh504)

| Window               | Baseline (EMA 9/21) | Bollinger (20, 2.0) | MACD (12, 26, 9) |
|----------------------|--------------------:|--------------------:|-----------------:|
| W-2 (2020-05→2021-04)| -275,493            | +141,312            | +142,391         |
| W-1 (2021-05→2022-04)| +459,660            | +520,424            | +330,878         |
| W0  (2022-05→2023-04)| +128,734            | +237,925            | -232,116         |
| W1  (2023-05→2024-04)| +80,564             | -25,382             | -424,265         |
| W2  (2024-05→2025-04)| +564,922            | +646,942            | +85,060          |
| W3  (2025-05→2026-04)| -35,772             | -21,499             | -59,766          |
| **Sum**              | **+922,615**        | **+1,499,722**      | **-157,818**     |
| **Mean**             | **+153,769**        | **+249,953**        | **-26,303**      |
| **Wins**             | **4/6**             | **4/6**             | **3/6**          |

## Stage 1 verdict (literal application of pre-registered rule)

| Strategy  | Wins | Mean   | Rule row hit                     | Verdict        |
|-----------|------|--------|----------------------------------|----------------|
| MACD      | 3/6  | −$26k  | Wins 3/6 OR mean ≤ 0             | **REJECTED**   |
| Bollinger | 4/6  | +$250k | Wins 4/6 AND mean > 0            | **WEAK**       |

## Stage 2 (correlation diagnostic — applies to WEAK only)

Pearson r of per-window NET vs baseline:
- **Bollinger vs Baseline: r = 0.832**  →  **REDUNDANT** (≥ 0.60 threshold)
- MACD vs Baseline: r = 0.290 (would be DIVERSIFIER, but Stage 1 rejected)

Bollinger and baseline move together. The +$577k cumulative outperformance is
concentrated in W-2 (+$417k single-window swing where baseline lost −$275k in
COVID-rally year — 7/37 sym profitable for baseline vs 23/37 for Bollinger).

## Final action

- **MACD: REJECT** — do not deploy in any role.
- **Bollinger: REJECT for shadow deploy** — qualifies as WEAK on Stage 1 but
  REDUNDANT on Stage 2. Per the rule's pre-registered hypothesis, the shadow
  slot is for testing DIVERSIFIERS, not duplicating the same regime edge with
  a different trigger. A high-r, single-window-driven outperformance is the
  exact pattern the framework was designed to flag.

## Observations (not part of the decision)

- All four Cat A candidates rejected for shadow deploy:
  - VWAP fade — REFUTED 2026-05-06
  - PDH/PDL break — REFUTED 2026-05-06
  - RSI cross-50 — Inconclusive 2026-05-06
  - MACD bearish cross — REJECTED 2026-05-07
  - Bollinger breakdown — WEAK + REDUNDANT 2026-05-07
- Pre-registered prior expectation matched: "Most likely outcomes: Both clear
  Stage 1 WEAK or STRONG, but with high correlation (r > 0.5) to baseline →
  not deploy-worthy as diversifiers."
- Conclusion: at this TF/cost stack, momentum-trigger choice is noise — same
  pattern as the EMA-pair sweep on 2026-05-06 (5/15 vs 9/21 within robust
  region was statistically chance).

## Commitments observed

1. Pre-registered rule applied LITERALLY — no goalpost-moving.
2. Did not redefine "win" beyond per-window NET > 0.
3. Did not exclude windows for being atypical.
4. Did not test additional MACD/BB parameter variants after seeing results.

## What this leaves

- Live deploy unchanged: 16 engines on EMA 9/21 mh504 + shadow (5/15 mh336, 5/15 mh504).
- No real-money allocation.
- Forward-paper validation continues per `CLAUDE.md ## Forward-paper go/no-go criteria`.
- Cat B (different exit framework) and Cat C (alt asset class / volatility regime gating)
  remain unexplored and would be the next-session candidates if more sweep
  capacity is desired. But the Cat A pattern is informative: changing the trigger
  doesn't move the needle once the exit framework / cost stack is fixed.
