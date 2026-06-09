# Overfit-gate expansion: does adding today's 3 new candidates help or hurt the family? (pre-reg + verdict, 2026-06-09)

**Status:** LOCKED BEFORE seeing the expanded DSR. This is the decision-grade test of the
question "we need more research" — run on the overfit gate, not raw NET.

## Why this, not another raw-NET sweep

Today produced FOUR cells that beat the live config on mean NET (RSI +79%, Momentum +76%,
MACD +18%, Purgatory-4H +57%). Judging by mean NET, each looks like a candidate. But the
2025-26 bear window (W3) inflates every aggressive short trigger — their W3 is 8-13× the live
EMA's. The correct arbiter is the **overfit gate** (PBO/DSR), which docks the edge for the
size of the search. The clean 34-config gate already returned FRAGILE (DSR 0.658 < 0.95).

This test asks the question directly: **when we add the new candidates we found today to the
search surface, does the family-level DSR go UP (real new edge that survives the haircut) or
DOWN (the candidates are just more trials = the search is self-defeating)?**

## What this does

Rebuilds the overfit returns matrix on the **fixed binary** with the 34 original configs PLUS
3 new modes tested today:
- `momentum_5m` (`--momentum-mode --signal-tf 5m`)
- `purgatory_4H` (`--purgatory-mode --ema 5/9`)
- `pdh_pdl` (`--pdh-pdl-break-mode`)

= 37 configs × 57 sym × 64 months. Then re-runs `scripts/backtest_overfit_analysis.py`.
Matrix: `results/overfit_returns_matrix_2026-06-09_expanded37.csv`.

(Note: momentum_5m and purgatory cells fire their own mode under the matrix-gen flag pattern —
verified the appended mode flag overrides EMAMode + base periods.)

## Pre-registered prediction

**Strong prior: DSR goes DOWN (or flat), PBO stays ≈ coin-flip.** Adding N candidates to a CSCV
search can only lower the deflated Sharpe of the best config unless a candidate has a genuinely
higher, regime-robust Sharpe. The 3 new candidates all share the live class's 2025-26-bear
concentration → they do NOT add a regime-robust stream → DSR(37) ≤ DSR(34)=0.658, still ≪0.95.

**Falsifiable:** if DSR(37) > 0.95 OR a new config becomes modal IS-best with LOW correlation
to the bear window, that would be a real finding (a candidate worth a fresh promotion pre-reg).

## Decision rule (LOCKED)

| Result | Conclusion |
|---|---|
| DSR(37) ≤ DSR(34), still <0.95 | **Confirmed: more research is self-defeating.** Each candidate widens the haircut the live edge already fails. STOP backtest search; forward-paper is the arbiter. |
| DSR(37) > 0.95 OR a low-correlation new modal config | A real candidate exists → fresh promotion pre-reg + correlation analysis. (Not expected.) |

**No deployment regardless.** This is a meta-test of the search itself.

---

## Results — completed 2026-06-09

Expanded matrix: `results/overfit_returns_matrix_2026-06-09_expanded37.csv` (64 months × 37
configs, fixed binary). Gate vs the clean 34-config baseline:

| Metric | Clean-34 | **Expanded-37** | Direction |
|---|---:|---:|---|
| PBO | 0.4698 | **0.5302** | worse |
| **DSR(N)** | **0.658** | **0.0010** | **collapsed to ≈0** |
| SR0 (Sharpe bar to clear) | 0.178 | **0.6047** | bar jumped 3.4× |
| degradation slope | −0.87 | −0.9462 | worse |
| PSR | 0.969 | 0.969 | unchanged (LIVE in isolation) |
| modal IS-best | confl_13_34 | confl_13_34 | unchanged |
| **PR (independent bets)** | **1.8** | **1.9** | still ≈2 of N |

### Verdict — DECISIVE: more candidates DESTROY the family confidence (prediction confirmed)

Adding the 3 candidates we found today (`momentum_5m`, `purgatory_4H`, `pdh_pdl`) drove
**DSR from 0.658 to 0.0010** — effectively zero. Mechanism: `momentum_5m` and `purgatory`
have *higher in-sample Sharpe* than LIVE (they over-fire in the 2025-26 bear window), so they
raise SR0 — the max-of-N Sharpe bar a config must clear to not be search-noise — from 0.178 to
**0.6047**, far above LIVE's actual 0.79-annualized / 0.23-monthly. Once you honestly account
for the candidates the search produced, **the live edge is indistinguishable from the best of
37 random tries.**

The participation ratio is the why: **PR ≈ 1.9 — the 37 configs collapse to ~2 independent
return streams.** Every config except `side_both` (which includes longs) is >0.64 correlated
with LIVE; most are >0.9. The "breadth" of a large config search on this universe is an
illusion — it is ~2 bets ("short the downtrend", and a faint long-inclusive variant) wearing
37 costumes.

### This is the quantified answer to "we need more research / check many combinations"

A broad indicator/param grid cannot help, and this proves it numerically, not rhetorically:
1. **The candidates are correlated copies** (PR ≈ 1.9 / 37), so N grid cells ≈ 2 effective bets.
2. **Each cell raises the haircut** (SR0 0.178→0.60; DSR 0.658→0.001), so searching *lowers*
   confidence in the edge you have.
3. The only way out is a **structurally uncorrelated** return stream (cross-sectional L/S,
   carry, a new data axis) — not another function of the same 57 price paths. The overnight
   batch (`scripts/overnight_research_batch.sh`) tests the grids anyway (stored as documented
   no-gos) AND the one new axis (cross-sectional dollar-neutral).

**Conclusion: backtest signal/param search is closed — now with decision-grade proof (DSR→0,
PR≈2). Forward-paper is the only arbiter. Stored per directive: negative results are results.**
