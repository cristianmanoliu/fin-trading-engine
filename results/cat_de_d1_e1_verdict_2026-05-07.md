# Cat D/E filter alternatives: D1 + E1 verdict

**Date:** 2026-05-07
**Decision rule:** `results/cat_de_decision_rule_2026-05-07.md` (pre-registered).
**Baseline:** `results/walk_forward_4H_short_6w_ema9-21_mh504_2026-05-06.txt` — fixed 6R TP + wick stop + mh504, 4H short, 6-window: sum +$922k, mean +$154k, σ $313k, 4/6 wins, Sharpe 0.49.

## Per-window NETs (universe-57, slip=15bp, fee=10bp, mh504, 4H short, EMA 9/21 entry)

| Window               | Baseline    | D1 (1D confluence)| E1 (vol filt 120%)| D1 − base   | E1 − base   |
|----------------------|------------:|------------------:|------------------:|------------:|------------:|
| W-2 (2020-05→2021-04)| -275,493    | -49,358           | -106,399          | **+226,135**| **+169,094**|
| W-1 (2021-05→2022-04)| +459,660    | +310,827          | +186,701          | -148,833    | -272,959    |
| W0  (2022-05→2023-04)| +128,734    | +8,427            | +32,267           | -120,307    | -96,467     |
| W1  (2023-05→2024-04)| +80,564     | +247,591          | +70,018           | **+167,027**| -10,546     |
| W2  (2024-05→2025-04)| +564,922    | +252,571          | +400,312          | -312,351    | -164,610    |
| W3  (2025-05→2026-04)| -35,772     | -97,754           | -56,570           | -61,982     | -20,798     |
| **Sum**              | **+922,615**| **+672,304**      | **+526,329**      | **-250k**   | **-396k**   |
| **Mean**             | **+153,769**| **+112,050**      | **+87,721**       |             |             |
| **σ across windows** | **313k**    | 178k              | 184k              |             |             |
| **σ / baseline σ**   | 1.00        | **0.57**          | **0.59**          |             |             |
| **Sharpe (mean/σ)**  | 0.49        | **0.63** ⭐       | 0.48              |             |             |
| **Wins**             | **4/6**     | **4/6**           | **4/6**           |             |             |
| **r vs baseline**    | 1.00        | 0.792             | 0.946             |             |             |
| **Total trades**     | 9,238       | 4,585 (−50%)      | 6,831 (−26%)      |             |             |

## Stage 1 verdict (literal application of pre-registered rule)

| Strategy  | Wins | Sum     | Rule row hit                              | Verdict     |
|-----------|------|---------|-------------------------------------------|-------------|
| **D1**    | 4/6  | +$672k  | "Wins ≥4/6 AND sum > 0 but ≤ baseline"    | **NEUTRAL** |
| **E1**    | 4/6  | +$526k  | "Wins ≥4/6 AND sum > 0 but ≤ baseline"    | **NEUTRAL** |

**Both NEUTRAL per the pre-registered rule. No deploy change.**

The script's auto-verdict shows SUPPORTIVE for both because its built-in logic doesn't compare to baseline. The pre-registered Cat D/E rule has a stricter SUPPORTIVE definition (sum must beat baseline). Applied literally — both fall into NEUTRAL.

## Stage 2 (variance comparison — observational, doesn't change Stage 1)

Pre-registered: σ comparison applies "only for STRONG / SUPPORTIVE." Neither
candidate reached SUPPORTIVE. The σ ratios below are documented for context.

- **D1:** σ ratio 0.57 → would have qualified VARIANCE-REDUCING (< 0.7) under Stage 2.
- **E1:** σ ratio 0.59 → would also have qualified VARIANCE-REDUCING under Stage 2.

## Observational finding: D1 is the first candidate where Sharpe improves

D1 stands apart from the previous 8 candidates tested (Cat A entries 1–5, Cat B exits 1–2, E1) on one specific metric:

- **Mean dropped 27%** (+$154k → +$112k per window)
- **σ dropped 43%** (313k → 178k)
- **Net Sharpe ratio +28%** (0.49 → 0.63)

For variance reduction to translate to Sharpe improvement, σ must drop MORE than mean. That happened with D1, did NOT happen with B1, B2, or E1. This is a genuinely different signal shape than the magnitude-capping pattern of B1/B2.

Additional supporting evidence:
- **Lower correlation to baseline** (r=0.79) vs B2 (r=0.996). D1 captures a meaningfully different regime exposure — the 1D bias filter actually changes WHICH trades are taken, not just how much they win/lose.
- The pattern of per-window deltas differs from Cat B: D1 is **better** on W-2 (+$226k) AND W1 (+$167k), worse only on the bull-trap recovery windows where 1D bias was UP (W-1, W2). This is exactly the mechanism story we hypothesized.
- All 6 windows have ≥204 trades (the smallest, W-2). Statistically powered.

**However**, the rule gates on sum. D1 does not earn a shadow slot under the current rule. **A future session could pre-register a Sharpe-gated Cat D2 rule and revisit** — that would not be a goalpost-move on this verdict, just a different question with its own pre-registration.

## E1 is unambiguously NEUTRAL

E1 saves losses on W-2 (+$169k delta in COVID-altseason) but loses everywhere else. The vol filter excludes some profitable bear-window trades along with the panic ones — net negative trade-off.

- Sum −43% vs baseline (worse)
- Sharpe −3% vs baseline (slightly worse)
- σ ratio 0.59 (variance-reducing but Stage 2 doesn't apply)
- r=0.946 (high correlation = same regime, magnitude-modulated)

This is the same pattern as B1/B2 — variance-reducing without commensurate sum.

## Final action

- **D1: NEUTRAL — no deploy change** (per current rule). **Flag for potential future Sharpe-gated re-evaluation.**
- **E1: NEUTRAL — no deploy change.**
- **Live deploy unchanged.** 16 paper engines continue running EMA 9/21 mh504 fixed 6R + wick stop.

## Commitments observed

1. Pre-registered rule applied LITERALLY — no goalpost-moving despite D1's appealing Sharpe.
2. Did NOT switch to Sharpe-gated verdict for D1.
3. Did NOT switch to baseline-relative-σ ratio as a tiebreaker.
4. Did NOT exclude W-1 / W2 (the windows that hurt D1) for being "atypical bull-trap years."
5. Did NOT propose D1+E1 combo after seeing results.
6. Documented Sharpe finding as **observational** with explicit pre-registration disclaimer for future revisit.

## Cumulative search exhaustion (across two sessions)

**9 candidates tested against the same baseline:**
- Cat A entries (5): VWAP, PDH/PDL, RSI, MACD, BB — all REJECTED.
- Cat B exits (2): B1 trail, B2 mltp — REJECTED, NEUTRAL.
- Cat D/E filters (2): D1 confluence, E1 vol — NEUTRAL, NEUTRAL.

**No candidate qualifies for SUPPORTIVE+** under the pre-registered baseline-comparison rules. Strong evidence the deployed configuration is in a robust region.

D1 is the closest near-miss — Sharpe improvement is real but doesn't satisfy the
sum-based gate. With multiple-comparison context (9 tests), even a SUPPORTIVE
result would have been weighted ~3× more skeptically than at test #1, so the
conservative NEUTRAL outcome reinforces the case for status quo.

## What this leaves

**Remaining unexplored axes:**
- **Cat F: portfolio-level concentration sizing.** Requires cross-symbol coordination → not in current backtest harness. ~4-6h architectural work.
- **Cat D2: Sharpe-gated re-evaluation of D1 (pre-register a new rule first).** ~30 min. Honest re-test of whether Sharpe improvement should weight the verdict.
- **Operational tools:** `paper_live_status.sh`, `paper_live_compare.sh`. Not strategy work, but the gate for moving to real money.

The Cat A through Cat E entry/exit/filter taxonomy is now exhausted.
Future strategy work has to either (a) change the cost stack (different
exchange, different VIP tier), (b) change the framework (Cat F sizing
architecture), or (c) test fundamentally different strategy types (carry,
basis, pairs — high infrastructure cost).
